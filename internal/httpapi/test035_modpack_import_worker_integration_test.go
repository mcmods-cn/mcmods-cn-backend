package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	appconfig "mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestTEST035ModpackImportWorkerBoundariesAndFinalStatesIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("TEST-035 exercises an isolated PostgreSQL generation and bounded provider archives")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := newTEST035IsolatedDatabase(t, ctx)

	successArchive := makeTEST035MRPack(t, []map[string]any{
		{
			"path": "mods/trusted-mod.jar",
			"downloads": []string{
				"https://cdn.modrinth.com/data/trusted-project/versions/trusted-version/trusted-mod.jar",
			},
			"env": map[string]string{"client": "required", "server": "required"},
		},
		{
			"path": "mods/attacker-mod.jar",
			"downloads": []string{
				"https://attacker.example/data/victim-project/versions/fake-version/attacker-mod.jar",
			},
			"env": map[string]string{"client": "required", "server": "unsupported"},
		},
	})
	excessFiles := make([]map[string]any, 0, maxModpackIndexFiles+1)
	for index := 0; index <= maxModpackIndexFiles; index++ {
		excessFiles = append(excessFiles, map[string]any{
			"path":      "mods/excess-" + strconv.Itoa(index) + ".jar",
			"downloads": []string{},
			"env":       map[string]string{"client": "required", "server": "required"},
		})
	}
	excessArchive := makeTEST035MRPack(t, excessFiles)

	var provider *httptest.Server
	provider = httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		path := request.URL.Path
		if strings.HasPrefix(path, "/project/") && !strings.HasSuffix(path, "/version") {
			reference := strings.TrimPrefix(path, "/project/")
			team := ""
			if reference == "success" {
				team = "team-success"
			} else if reference == "auxfail" {
				team = "team-fail"
			}
			writeTEST035JSON(t, response, map[string]any{
				"id": "project-" + reference, "slug": reference, "title": "TEST-035 " + reference,
				"description": "Bounded Modrinth import", "body": "TEST-035 body", "project_type": "modpack",
				"categories": []string{"adventure"}, "client_side": "required", "server_side": "required",
				"status": "approved", "team": team, "license": map[string]string{"id": "MIT"},
			})
			return
		}
		if strings.HasPrefix(path, "/project/project-") && strings.HasSuffix(path, "/version") {
			reference := strings.TrimSuffix(strings.TrimPrefix(path, "/project/project-"), "/version")
			archiveURL := provider.URL + "/archives/" + reference + ".mrpack"
			writeTEST035JSON(t, response, []map[string]any{
				{
					"id": "release-old", "name": "Old release", "version_number": "1.0.0", "version_type": "release", "status": "listed",
					"date_published": "2026-01-01T00:00:00Z", "game_versions": []string{"1.20.1"}, "loaders": []string{"fabric"},
					"files": []map[string]any{{"url": archiveURL, "filename": "old-" + reference + ".mrpack", "primary": true}},
				},
				{
					"id": "beta-newer", "name": "Newer beta", "version_number": "3.0.0-beta", "version_type": "beta", "status": "listed",
					"date_published": "2026-03-01T00:00:00Z", "game_versions": []string{"1.21.1"}, "loaders": []string{"fabric"},
					"files": []map[string]any{{"url": archiveURL, "filename": "beta-" + reference + ".mrpack", "primary": true}},
				},
				{
					"id": "release-new", "name": "Newest release", "version_number": "2.0.0", "version_type": "release", "status": "listed",
					"date_published": "2026-02-01T00:00:00Z", "game_versions": []string{"1.21.1"}, "loaders": []string{"fabric"},
					"files": []map[string]any{
						{"url": archiveURL, "filename": "secondary-" + reference + ".mrpack", "primary": false},
						{"url": archiveURL, "filename": "main-" + reference + ".mrpack", "primary": true},
					},
				},
			})
			return
		}
		switch path {
		case "/team/team-success/members":
			writeTEST035JSON(t, response, []map[string]any{{
				"role": "Owner", "user": map[string]string{"username": "test035", "name": "TEST-035 Author"},
			}})
		case "/team/team-fail/members":
			http.Error(response, "TEST035 auxiliary endpoint failed", http.StatusServiceUnavailable)
		case "/archives/success.mrpack":
			response.Header().Set("Content-Type", "application/octet-stream")
			_, _ = response.Write(successArchive)
		case "/archives/excess.mrpack":
			response.Header().Set("Content-Type", "application/octet-stream")
			_, _ = response.Write(excessArchive)
		case "/archives/huge.mrpack":
			response.Header().Set("Content-Type", "application/octet-stream")
			response.Header().Set("Content-Length", strconv.FormatInt(maxModpackArchiveBytes+1, 10))
			response.WriteHeader(http.StatusOK)
		default:
			http.Error(response, "TEST035 unexpected provider path "+path, http.StatusNotFound)
		}
	}))
	defer provider.Close()

	originalTransport := http.DefaultTransport
	http.DefaultTransport = provider.Client().Transport
	defer func() { http.DefaultTransport = originalTransport }()
	temporaryRoot := t.TempDir()
	t.Setenv("TMP", temporaryRoot)
	t.Setenv("TEMP", temporaryRoot)
	t.Setenv("TMPDIR", temporaryRoot)
	if got := os.TempDir(); !strings.EqualFold(filepath.Clean(got), filepath.Clean(temporaryRoot)) {
		t.Fatalf("TEST035 temporary root=%q, want %q", got, temporaryRoot)
	}

	server := &Server{db: pool, cfg: appconfig.Load()}
	importConfig := defaultModImportConfig()
	importConfig.RequestTimeoutSeconds = 5
	importConfig.Modrinth = modImportProviderConfig{Enabled: true, BaseURL: provider.URL}
	importConfig.CurseForge.Enabled = false
	importConfig.GitHub.Enabled = false
	sealed, err := server.sealSystemSetting(importConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values($1,$2::jsonb)
		on conflict(key) do update set value=excluded.value`, modImportConfigSettingKey, sealed); err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values('test035-worker','test035-worker@example.test','not-used','active') returning id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	tasks := []struct {
		id        string
		reference string
	}{
		{"t035ok001", "success"},
		{"t035aux01", "auxfail"},
		{"t035huge1", "huge"},
		{"t035many1", "excess"},
	}
	for _, task := range tasks {
		if _, err = pool.Exec(ctx, `insert into mod_metadata_import_jobs(public_id,user_id,project_type,provider,source_url,status,progress)
			values($1,$2,'modpack','modrinth',$3,'queued',0)`, task.id, userID, "https://modrinth.com/modpack/"+task.reference); err != nil {
			t.Fatal(err)
		}
	}

	if err = server.runModMetadataImport(ctx, "t035ok001"); err != nil {
		t.Fatalf("successful import worker: %v", err)
	}
	var successStatus, successError string
	var successProgress int
	var successResult []byte
	var successFinished bool
	if err = pool.QueryRow(ctx, `select status,progress,error,result,finished_at is not null from mod_metadata_import_jobs where public_id='t035ok001'`).
		Scan(&successStatus, &successProgress, &successError, &successResult, &successFinished); err != nil {
		t.Fatal(err)
	}
	if successStatus != "completed" || successProgress != 100 || successError != "" || !successFinished {
		t.Fatalf("successful task final state=%s/%d/%q/finished=%t", successStatus, successProgress, successError, successFinished)
	}
	var draft createModpackRequest
	if err = json.Unmarshal(successResult, &draft); err != nil {
		t.Fatal(err)
	}
	if draft.DefaultLocale != externalModpackLocale || draft.ModrinthProjectID != "project-success" || draft.ImportSelection == nil {
		t.Fatalf("provider project/locale/selection=%q/%q/%#v", draft.ModrinthProjectID, draft.DefaultLocale, draft.ImportSelection)
	}
	selection := draft.ImportSelection
	if selection.ProjectID != "project-success" || selection.VersionID != "release-new" || selection.VersionName != "Newest release" ||
		selection.FileName != "main-success.mrpack" || selection.ReleaseType != "release" || selection.PublishedAt != "2026-02-01T00:00:00Z" {
		t.Fatalf("non-deterministic release/main selection: %#v", selection)
	}
	if len(draft.Authors) != 1 || draft.Authors[0].Name != "TEST-035 Author" || len(draft.Mods) != 2 {
		t.Fatalf("provider auxiliary/mod projection=%#v/%#v", draft.Authors, draft.Mods)
	}
	if draft.Mods[0].ProviderProjectID != "trusted-project" || draft.Mods[0].ProviderVersionID != "trusted-version" ||
		draft.Mods[1].ProviderProjectID != "" || draft.Mods[1].ProviderVersionID != "" || draft.Mods[1].Identifier != "attacker-mod" {
		t.Fatalf("CDN identity boundary=%#v", draft.Mods)
	}
	assertTEST035NoTemporaryArchives(t, temporaryRoot)

	for _, failure := range []struct {
		id      string
		message string
	}{
		{"t035aux01", "team"},
		{"t035huge1", "too large"},
		{"t035many1", "more than 2000 files"},
	} {
		if err = server.runModMetadataImport(ctx, failure.id); err != nil {
			t.Fatalf("failed job %s returned infrastructure error: %v", failure.id, err)
		}
		var status, message string
		var progress int
		var result []byte
		var finished bool
		if err = pool.QueryRow(ctx, `select status,progress,error,result,finished_at is not null from mod_metadata_import_jobs where public_id=$1`, failure.id).
			Scan(&status, &progress, &message, &result, &finished); err != nil {
			t.Fatal(err)
		}
		if status != "failed" || progress != 25 || !finished || result != nil || !strings.Contains(message, failure.message) {
			t.Fatalf("failed task %s final state=%s/%d/%q/result=%s/finished=%t", failure.id, status, progress, message, result, finished)
		}
		assertTEST035NoTemporaryArchives(t, temporaryRoot)
	}
}

func makeTEST035MRPack(t *testing.T, files []map[string]any) []byte {
	t.Helper()
	index, err := json.Marshal(map[string]any{
		"formatVersion": 1,
		"game":          "minecraft",
		"versionId":     "test035",
		"name":          "TEST-035 pack",
		"dependencies":  map[string]string{"minecraft": "1.21.1", "fabric-loader": "0.16.14"},
		"files":         files,
	})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	entry, err := archive.Create("modrinth.index.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = entry.Write(index); err != nil {
		t.Fatal(err)
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func writeTEST035JSON(t *testing.T, response http.ResponseWriter, value any) {
	t.Helper()
	if err := json.NewEncoder(response).Encode(value); err != nil {
		t.Errorf("write TEST035 provider response: %v", err)
	}
}

func assertTEST035NoTemporaryArchives(t *testing.T, root string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, "mcmods-modpack-import-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("modpack import temporary directories were not removed: %v", matches)
	}
}

func newTEST035IsolatedDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	baseConfig, err := pgxpool.ParseConfig(appconfig.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	databaseName := fmt.Sprintf("test035_modpack_%d", time.Now().UnixNano())
	namePattern := regexp.MustCompile(`^test035_modpack_[0-9]+$`)
	if !namePattern.MatchString(databaseName) {
		adminPool.Close()
		t.Fatalf("refuse unsafe TEST035 database name %q", databaseName)
	}
	quotedName := pgx.Identifier{databaseName}.Sanitize()
	if _, err = adminPool.Exec(ctx, "create database "+quotedName); err != nil {
		adminPool.Close()
		t.Fatal(err)
	}
	var pool *pgxpool.Pool
	t.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if !namePattern.MatchString(databaseName) {
			t.Errorf("refuse unsafe TEST035 cleanup database name %q", databaseName)
		} else if _, dropErr := adminPool.Exec(cleanupCtx, "drop database "+quotedName+" with (force)"); dropErr != nil {
			t.Errorf("drop isolated TEST035 database: %v", dropErr)
		}
		adminPool.Close()
	})
	scopedConfig, err := pgxpool.ParseConfig(appconfig.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	scopedConfig.ConnConfig.Database = databaseName
	scopedConfig.MaxConns, scopedConfig.MinConns = 8, 1
	pool, err = pgxpool.NewWithConfig(ctx, scopedConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}
