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
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestBUG038RejectedMirrorFailsRunAndCanBeRescannedIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify automated mirror scan transitions")
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

	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values($1,$2::jsonb)
		on conflict(key) do update set value=excluded.value`, minecraftVersionsSettingKey,
		`{"versions":[{"code":"1.21.1","type":"release"}],"commonVersions":["1.21.1"],"loaders":[{"code":"Fabric","name":"Fabric","versions":["1.21.1"]}]}`); err != nil {
		t.Fatal(err)
	}
	var reviewerID int64
	stamp := time.Now().UnixNano()
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'bug038',true) returning id`, fmt.Sprintf("bug038-%d", stamp),
		fmt.Sprintf("bug038-%d@example.invalid", stamp)).Scan(&reviewerID); err != nil {
		t.Fatal(err)
	}
	var projectID, routeID int64
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		values('b038m0001','bug038-project','BUG038 project','approved') returning id`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, projectID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}

	providerFile := providerProjectFile{
		ID: "bug038-file", Source: "modrinth", DisplayName: "BUG038 release", FileName: "bug038.jar",
		VersionName: "1.0.0", ReleaseChannel: "release", GameVersions: []string{"1.21.1"}, Loaders: []string{"Fabric"},
		SizeBytes: 128, SHA1: "1111111111111111111111111111111111111111", DirectURL: "https://cdn.example.test/bug038.jar",
	}
	metadata, err := json.Marshal(providerFile)
	if err != nil {
		t.Fatal(err)
	}
	var ossID int64
	var ossPublicID string
	if err = pool.QueryRow(ctx, `insert into oss_files(object_key,category,source,original_name,content_type,size_bytes,source_size_bytes,sha256,status,scan_status)
		values($1,'project/download/automated','project_auto_update','bug038.jar','application/java-archive',128,128,$2,'quarantined','rejected')
		returning id,public_id`, fmt.Sprintf("bug038/%d/bug038.jar", stamp), "sha-bug038-file").Scan(&ossID, &ossPublicID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mirrored_project_files(project_route_id,source_type,external_file_id,file_sha256,byte_size,oss_file_id,status,metadata)
		values($1,'modrinth',$2,$3,128,$4,'scanning',$5::jsonb)`, routeID, providerFile.ID, "sha-bug038-file", ossID, metadata); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool, cfg: config.Load()}
	worker := &ProjectAutomationWorker{server: server}
	if err = worker.promoteCleanMirrors(ctx); err != nil {
		t.Fatal(err)
	}
	var mirrorStatus string
	if err = pool.QueryRow(ctx, `select status from mirrored_project_files where external_file_id=$1`, providerFile.ID).Scan(&mirrorStatus); err != nil {
		t.Fatal(err)
	}
	if mirrorStatus != "failed" {
		t.Errorf("rejected mirror status=%q, want failed", mirrorStatus)
	}

	ossConfig := ossConfigPayload{
		Enabled: true, Region: "cn-test", Endpoint: "https://oss.example.test", PublicEndpoint: "https://public.example.test",
		Bucket: "bug038-bucket", AccessKeyID: "bug038-key", AccessKeySecret: "bug038-secret", UseCName: true, Prefix: "mcmods",
	}
	sealed, err := server.sealSystemSetting(ossConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb)
		on conflict(key) do update set value=excluded.value`, sealed); err != nil {
		t.Fatal(err)
	}
	job := projectAutomationJob{RouteID: routeID, InternalID: projectID, ProjectType: "mod", ProjectPublicID: "b038m0001", SourceType: "modrinth"}
	result, mirrorErr := worker.mirrorFiles(ctx, job, modImportConfig{}, []providerProjectFile{providerFile})
	if !errors.Is(mirrorErr, errProjectMirrorScanRejected) || result["scanFailures"] != 1 || result["needsReview"] != 1 {
		t.Errorf("rejected mirror run result=%v error=%v, want scanFailures=1/needsReview=1/error", result, mirrorErr)
	}

	updateScan := func(status, note string) {
		t.Helper()
		payload, marshalErr := json.Marshal(map[string]string{"status": status, "note": note})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		request := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/oss/files/"+ossPublicID+"/scan", bytes.NewReader(payload))
		request.SetPathValue("publicId", ossPublicID)
		request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: reviewerID}))
		response := httptest.NewRecorder()
		server.updateOSSFileScanStatus(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s rescan status=%d body=%s", status, response.Code, response.Body.String())
		}
	}
	updateScan("clean", "false positive cleared")
	if err = worker.promoteCleanMirrors(ctx); err != nil {
		t.Fatal(err)
	}
	var projectFileStatus string
	if err = pool.QueryRow(ctx, `select mirror.status,project_file.status from mirrored_project_files mirror
		join project_files project_file on project_file.oss_file_id=mirror.oss_file_id where mirror.external_file_id=$1`,
		providerFile.ID).Scan(&mirrorStatus, &projectFileStatus); err != nil {
		t.Fatal(err)
	}
	if mirrorStatus != "ready" || projectFileStatus != "active" {
		t.Errorf("clean rescan mirror/project file=%s/%s, want ready/active", mirrorStatus, projectFileStatus)
	}

	updateScan("pending", "late rescan started")
	if err = pool.QueryRow(ctx, `select mirror.status,project_file.status from mirrored_project_files mirror
		join project_files project_file on project_file.oss_file_id=mirror.oss_file_id where mirror.external_file_id=$1`,
		providerFile.ID).Scan(&mirrorStatus, &projectFileStatus); err != nil {
		t.Fatal(err)
	}
	if mirrorStatus != "scanning" || projectFileStatus != "processing" {
		t.Errorf("pending rescan mirror/project file=%s/%s, want scanning/processing", mirrorStatus, projectFileStatus)
	}
	result, mirrorErr = worker.mirrorFiles(ctx, job, modImportConfig{}, []providerProjectFile{providerFile})
	if mirrorErr != nil || result["awaitingScan"] != 1 || result["scanFailures"] != 0 {
		t.Errorf("pending mirror run result=%v error=%v, want awaitingScan=1/scanFailures=0/success", result, mirrorErr)
	}

	updateScan("rejected", "late signature rejection")
	if err = pool.QueryRow(ctx, `select mirror.status,project_file.status from mirrored_project_files mirror
		join project_files project_file on project_file.oss_file_id=mirror.oss_file_id where mirror.external_file_id=$1`,
		providerFile.ID).Scan(&mirrorStatus, &projectFileStatus); err != nil {
		t.Fatal(err)
	}
	if mirrorStatus != "failed" || projectFileStatus != "rejected" {
		t.Errorf("late rejection mirror/project file=%s/%s, want failed/rejected", mirrorStatus, projectFileStatus)
	}
	result, mirrorErr = worker.mirrorFiles(ctx, job, modImportConfig{}, []providerProjectFile{providerFile})
	if !errors.Is(mirrorErr, errProjectMirrorScanRejected) || result["scanFailures"] != 1 {
		t.Errorf("late rejected mirror run result=%v error=%v, want scanFailures=1/error", result, mirrorErr)
	}

	updateScan("clean", "late rejection cleared")
	if err = worker.promoteCleanMirrors(ctx); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select mirror.status,project_file.status from mirrored_project_files mirror
		join project_files project_file on project_file.oss_file_id=mirror.oss_file_id where mirror.external_file_id=$1`,
		providerFile.ID).Scan(&mirrorStatus, &projectFileStatus); err != nil {
		t.Fatal(err)
	}
	if mirrorStatus != "ready" || projectFileStatus != "active" {
		t.Errorf("second clean rescan mirror/project file=%s/%s, want ready/active", mirrorStatus, projectFileStatus)
	}
	result, mirrorErr = worker.mirrorFiles(ctx, job, modImportConfig{}, []providerProjectFile{providerFile})
	if mirrorErr != nil || result["existing"] != 1 || result["scanFailures"] != 0 {
		t.Errorf("recovered mirror run result=%v error=%v, want existing=1/scanFailures=0/success", result, mirrorErr)
	}
}
