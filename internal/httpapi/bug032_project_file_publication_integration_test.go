package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestBUG032ProjectFilePublicationFollowsScanLifecycleIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify scan-gated project-file publication")
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

	userIDs := make([]int64, 2)
	for index := range userIDs {
		if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
			values($1,$2,'bug032',true) returning id`, fmt.Sprintf("bug032-%d-%d", time.Now().UnixNano(), index),
			fmt.Sprintf("bug032-%d-%d@example.invalid", time.Now().UnixNano(), index)).Scan(&userIDs[index]); err != nil {
			t.Fatal(err)
		}
	}
	const projectID = "b032m0001"
	var projectInternalID int64
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values($1,'bug032-project','BUG032 project','approved',$2) returning id`, projectID, userIDs[0]).Scan(&projectInternalID); err != nil {
		t.Fatal(err)
	}
	project := projectFileContext{
		ProjectType: "mod", ProjectID: projectID, ProjectInternalID: projectInternalID,
		SiteID: "bug032-project", ReviewStatus: "approved",
	}
	server := &Server{db: pool}

	pendingOSSID, pendingOSSPublicID := insertBUG032OSSFile(t, ctx, pool, project, userIDs[0], "pending.jar")
	pendingFileID := invokeBUG032ProjectFileCreate(t, ctx, server, project, userIDs[0], pendingOSSPublicID, "Pending release")
	assertBUG032ProjectFileState(t, ctx, pool, pendingFileID, "processing")
	if count := bug032ProjectFileEvents(t, ctx, pool, pendingFileID, "download_added"); count != 0 {
		t.Errorf("pending file emitted %d download_added events, want 0", count)
	}
	page, err := server.internalProjectFilePage(ctx, project, projectFilePageRequest{Source: "internal", Scope: "bug032", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Errorf("pending file was publicly listed: %#v", page.Items)
	}

	invokeBUG032ScanTransition(t, ctx, server, userIDs[1], pendingOSSPublicID, "clean")
	assertBUG032ProjectFileState(t, ctx, pool, pendingFileID, "active")
	if count := bug032ProjectFileEvents(t, ctx, pool, pendingFileID, "download_added"); count != 1 {
		t.Errorf("clean activation emitted %d download_added events, want 1", count)
	}
	page, err = server.internalProjectFilePage(ctx, project, projectFilePageRequest{Source: "internal", Scope: "bug032", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != pendingFileID || page.Items[0].ScanStatus != "clean" {
		t.Errorf("clean file page=%#v, want one downloadable item", page.Items)
	}
	invokeBUG032ScanTransition(t, ctx, server, userIDs[1], pendingOSSPublicID, "pending")
	assertBUG032ProjectFileState(t, ctx, pool, pendingFileID, "processing")
	if count := bug032ProjectFileEvents(t, ctx, pool, pendingFileID, "download_removed"); count != 1 {
		t.Errorf("quarantine transition emitted %d download_removed events, want 1", count)
	}
	page, err = server.internalProjectFilePage(ctx, project, projectFilePageRequest{Source: "internal", Scope: "bug032", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Errorf("quarantined file remained publicly listed: %#v", page.Items)
	}
	invokeBUG032ScanTransition(t, ctx, server, userIDs[1], pendingOSSPublicID, "clean")
	assertBUG032ProjectFileState(t, ctx, pool, pendingFileID, "active")
	var publicationGeneration int
	if err = pool.QueryRow(ctx, `select publication_generation from project_files where public_id=$1`, pendingFileID).Scan(&publicationGeneration); err != nil {
		t.Fatal(err)
	}
	if publicationGeneration != 2 {
		t.Errorf("reactivated file publication generation=%d want 2", publicationGeneration)
	}

	rejectedOSSID, rejectedOSSPublicID := insertBUG032OSSFile(t, ctx, pool, project, userIDs[1], "rejected.jar")
	rejectedFileID := invokeBUG032ProjectFileCreate(t, ctx, server, project, userIDs[1], rejectedOSSPublicID, "Rejected release")
	invokeBUG032ScanTransition(t, ctx, server, userIDs[0], rejectedOSSPublicID, "rejected")
	assertBUG032ProjectFileState(t, ctx, pool, rejectedFileID, "rejected")
	if count := bug032ProjectFileEvents(t, ctx, pool, rejectedFileID, "download_added"); count != 0 {
		t.Errorf("rejected file emitted %d download_added events, want 0", count)
	}

	unsafeOSSID, _ := insertBUG032OSSFile(t, ctx, pool, project, userIDs[0], "unsafe-direct.jar")
	if _, insertErr := pool.Exec(ctx, `insert into project_files(
		project_type,project_internal_id,oss_file_id,display_name,version_name,release_channel,
		game_versions,loaders,file_name,content_type,size_bytes,sha256,uploaded_by,status
	) values('mod',$1,$2,'Unsafe direct','1.0.0','release',array['1.21.1'],array['fabric'],
		'unsafe-direct.jar','application/java-archive',128,'unsafe',$3,'active')`, projectInternalID, unsafeOSSID, userIDs[0]); insertErr == nil {
		t.Error("database accepted an active project file whose OSS scan is pending")
	} else {
		var pgErr *pgconn.PgError
		if !errors.As(insertErr, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "project_files_active_oss_scan_check" {
			t.Errorf("unsafe active insert error=%v, want named 23514 publication check", insertErr)
		}
	}

	_ = pendingOSSID
	_ = rejectedOSSID
}

func insertBUG032OSSFile(t *testing.T, ctx context.Context, pool *pgxpool.Pool, project projectFileContext, uploaderID int64, fileName string) (int64, string) {
	t.Helper()
	var internalID int64
	var publicID string
	objectKey := fmt.Sprintf("bug032/%d/%s", time.Now().UnixNano(), fileName)
	if err := pool.QueryRow(ctx, `insert into oss_files(
		object_key,category,source,original_name,content_type,size_bytes,source_size_bytes,sha256,uploader_id,status,scan_status
	) values($1,$2,'project_file_upload',$3,'application/java-archive',128,128,$4,$5,'active','pending')
	returning id,public_id`, objectKey, ossProjectReleaseCategory(project.ProjectType, project.ProjectID), fileName,
		fmt.Sprintf("sha-%d", time.Now().UnixNano()), uploaderID).Scan(&internalID, &publicID); err != nil {
		t.Fatal(err)
	}
	return internalID, publicID
}

func invokeBUG032ProjectFileCreate(t *testing.T, ctx context.Context, server *Server, project projectFileContext, actorID int64, ossPublicID, displayName string) string {
	t.Helper()
	body := fmt.Sprintf(`{"ossFileId":%q,"displayName":%q,"versionName":"1.0.0","releaseChannel":"release","gameVersions":["1.21.1"],"loaders":["fabric"]}`, ossPublicID, displayName)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/projects/mod/b032m0001/files", bytes.NewBufferString(body))
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: actorID}))
	response := httptest.NewRecorder()
	server.createProjectFile(response, request, project)
	if response.Code != http.StatusCreated {
		t.Fatalf("create project file status=%d body=%s", response.Code, response.Body.String())
	}
	var publicID string
	if err := server.db.QueryRow(ctx, `select project_file.public_id from project_files project_file
		join oss_files oss on oss.id=project_file.oss_file_id where oss.public_id=$1`, ossPublicID).Scan(&publicID); err != nil {
		t.Fatal(err)
	}
	return publicID
}

func invokeBUG032ScanTransition(t *testing.T, ctx context.Context, server *Server, actorID int64, ossPublicID, status string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/oss/files/"+ossPublicID+"/scan", bytes.NewBufferString(`{"status":"`+status+`"}`))
	request.SetPathValue("publicId", ossPublicID)
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: actorID}))
	response := httptest.NewRecorder()
	server.updateOSSFileScanStatus(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("scan transition %q status=%d body=%s", status, response.Code, response.Body.String())
	}
}

func assertBUG032ProjectFileState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, publicID, want string) {
	t.Helper()
	var got string
	if err := pool.QueryRow(ctx, `select status from project_files where public_id=$1`, publicID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("project file %s status=%q want %q", publicID, got, want)
	}
}

func bug032ProjectFileEvents(t *testing.T, ctx context.Context, pool *pgxpool.Pool, publicID, kind string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `select count(*) from project_update_events
		where update_kind=$1 and position($2 in publication_batch_id)>0`, kind, publicID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
