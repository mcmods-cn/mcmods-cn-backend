package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestBUG033ProjectCompatibilityWritesUseAuthoritativeMinecraftVersionsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify project Minecraft-version authority")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	catalog := `{"versions":[{"code":"1.21.1","type":"release"}],"commonVersions":["1.21.1"],"loaders":[{"code":"Fabric","name":"Fabric","versions":["1.21.1"]}]}`
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values($1,$2::jsonb)
		on conflict(key) do update set value=excluded.value`, minecraftVersionsSettingKey, catalog); err != nil {
		t.Fatal(err)
	}
	var userID, projectInternalID, routeID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'bug033',true) returning id`, fmt.Sprintf("bug033-%d", time.Now().UnixNano()),
		fmt.Sprintf("bug033-%d@example.invalid", time.Now().UnixNano())).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	const projectID = "b033m0001"
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values($1,'bug033-project','BUG033 project','approved',$2) returning id`, projectID, userID).Scan(&projectInternalID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, projectInternalID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	project := projectFileContext{ProjectType: "mod", ProjectID: projectID, ProjectInternalID: projectInternalID,
		SiteID: "bug033-project", ReviewStatus: "approved"}

	_, ossPublicID := insertBUG032OSSFile(t, ctx, pool, project, userID, "unknown-version.jar")
	fileBody := fmt.Sprintf(`{"ossFileId":%q,"versionName":"1.0.0","releaseChannel":"release","gameVersions":["future-typo"],"loaders":["fabric"]}`, ossPublicID)
	fileRequest := httptest.NewRequest(http.MethodPost, "/api/v1/projects/mod/b033m0001/files", bytes.NewBufferString(fileBody))
	fileRequest = fileRequest.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: userID}))
	fileResponse := httptest.NewRecorder()
	server.createProjectFile(fileResponse, fileRequest, project)
	if fileResponse.Code != http.StatusBadRequest {
		t.Fatalf("unknown-version project file status=%d body=%s, want 400", fileResponse.Code, fileResponse.Body.String())
	}
	var fileCount int
	if err = pool.QueryRow(ctx, `select count(*) from project_files where project_type='mod' and project_internal_id=$1`, projectInternalID).Scan(&fileCount); err != nil {
		t.Fatal(err)
	}
	if fileCount != 0 {
		t.Fatalf("unknown-version upload persisted %d project files", fileCount)
	}

	changelogBody := `{"eventAt":"2026-08-23T00:00:00Z","minecraftVersions":["future-typo"],"projectVersion":"1.0.0","defaultLocale":"en-US","localizations":[{"locale":"en-US","bodyMarkdown":"Release"}]}`
	changelogRequest := httptest.NewRequest(http.MethodPost, "/api/v1/projects/mod/b033m0001/changelog", bytes.NewBufferString(changelogBody))
	changelogRequest = changelogRequest.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: userID}))
	changelogResponse := httptest.NewRecorder()
	server.createProjectChangelog(changelogResponse, changelogRequest, projectChangelogTarget{RouteID: routeID, EntityType: "mod", PublicID: projectID, Name: "BUG033 project", CanEdit: true})
	if changelogResponse.Code != http.StatusBadRequest {
		t.Fatalf("unknown-version changelog status=%d body=%s, want 400", changelogResponse.Code, changelogResponse.Body.String())
	}

	worker := &ProjectAutomationWorker{server: server}
	job := projectAutomationJob{RouteID: routeID, InternalID: projectInternalID, ProjectType: "mod", ProjectPublicID: projectID,
		SourceType: "modrinth", ActorID: userID}
	result, err := worker.syncChangelogs(ctx, job, []projectAutomationRelease{
		{ID: "unknown-release", Version: "2.0.0", PublishedAt: time.Now().UTC(), GameVersions: []string{"future-provider"}, Body: "Unknown only", URL: "https://example.invalid/unknown"},
		{ID: "mixed-release", Version: "2.1.0", PublishedAt: time.Now().UTC(), GameVersions: []string{"1.21.1", "future-provider"}, Body: "Mixed", URL: "https://example.invalid/mixed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["skippedUnknownMinecraftVersions"] != 1 {
		t.Fatalf("automation result=%#v, want one unknown-only release skipped", result)
	}
	var storedVersions []string
	var bindingMetadata []byte
	if err = pool.QueryRow(ctx, `select entry.minecraft_versions,binding.metadata
		from external_release_bindings binding join project_changelogs entry on entry.public_id=binding.changelog_public_id
		where binding.external_release_id='mixed-release'`).Scan(&storedVersions, &bindingMetadata); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(storedVersions, []string{"1.21.1"}) {
		t.Fatalf("mixed external release persisted versions=%#v, want only authoritative code", storedVersions)
	}
	var metadata map[string]any
	if err = json.Unmarshal(bindingMetadata, &metadata); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(metadata["rawMinecraftVersions"], []any{"1.21.1", "future-provider"}) ||
		!reflect.DeepEqual(metadata["unmappedMinecraftVersions"], []any{"future-provider"}) {
		t.Fatalf("external release metadata=%#v, want raw and unmapped provider labels", metadata)
	}
	var unknownBindingCount int
	if err = pool.QueryRow(ctx, `select count(*) from external_release_bindings where external_release_id='unknown-release'`).Scan(&unknownBindingCount); err != nil {
		t.Fatal(err)
	}
	if unknownBindingCount != 0 {
		t.Fatalf("unknown-only release created %d bindings", unknownBindingCount)
	}
	var changelogInternalID int64
	if err = pool.QueryRow(ctx, `select id from project_changelogs where object_route_id=$1 limit 1`, routeID).Scan(&changelogInternalID); err != nil {
		t.Fatal(err)
	}
	reviewTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reviewSnapshot := projectChangelogSnapshot{EventAt: time.Now().UTC(), MinecraftVersions: []string{"removed-after-review"},
		ProjectVersion: "reviewed", DefaultLocale: "en-US", Localizations: []projectChangelogLocalization{{Locale: "en-US", BodyMarkdown: "Reviewed"}}}
	applyErr := applyProjectChangelogSnapshotTx(ctx, reviewTx, changelogInternalID, routeID, 0, userID, reviewSnapshot)
	_ = reviewTx.Rollback(ctx)
	if !errors.Is(applyErr, errUnknownMinecraftVersionCodes) {
		t.Fatalf("review-time publication error=%v, want unknown-version rejection", applyErr)
	}

	insertBUG033CleanMirror(t, ctx, pool, routeID, "unknown-file", "unknown.jar", []string{"future-provider"})
	insertBUG033CleanMirror(t, ctx, pool, routeID, "mixed-file", "mixed.jar", []string{"1.21.1", "future-provider"})
	worker.promoteCleanMirrors(ctx)
	var unknownMirrorStatus string
	if err = pool.QueryRow(ctx, `select status from mirrored_project_files where external_file_id='unknown-file'`).Scan(&unknownMirrorStatus); err != nil {
		t.Fatal(err)
	}
	if unknownMirrorStatus != "review" {
		t.Fatalf("unknown-only mirror status=%q want review", unknownMirrorStatus)
	}
	if err = pool.QueryRow(ctx, `select project_file.game_versions from project_files project_file
		join mirrored_project_files mirror on mirror.oss_file_id=project_file.oss_file_id where mirror.external_file_id='mixed-file'`).Scan(&storedVersions); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(storedVersions, []string{"1.21.1"}) {
		t.Fatalf("mixed mirror persisted versions=%#v, want only authoritative code", storedVersions)
	}
}

func insertBUG033CleanMirror(t *testing.T, ctx context.Context, pool *pgxpool.Pool, routeID int64, externalID, fileName string, versions []string) {
	t.Helper()
	metadata, err := json.Marshal(providerProjectFile{ID: externalID, Source: "modrinth", DisplayName: fileName, FileName: fileName,
		VersionName: "1.0.0", ReleaseChannel: "release", GameVersions: versions, Loaders: []string{"Fabric"}})
	if err != nil {
		t.Fatal(err)
	}
	var ossID int64
	if err = pool.QueryRow(ctx, `insert into oss_files(object_key,category,source,original_name,content_type,size_bytes,source_size_bytes,sha256,status,scan_status)
		values($1,'project/download/automated','project_auto_update',$2,'application/java-archive',128,128,$3,'active','clean') returning id`,
		fmt.Sprintf("bug033/%d/%s", time.Now().UnixNano(), fileName), fileName, "sha-"+externalID).Scan(&ossID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mirrored_project_files(project_route_id,source_type,external_file_id,file_sha256,byte_size,oss_file_id,status,metadata)
		values($1,'modrinth',$2,$3,128,$4,'scanning',$5::jsonb)`, routeID, externalID, "sha-"+externalID, ossID, metadata); err != nil {
		t.Fatal(err)
	}
}
