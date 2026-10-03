package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestOCT02ReusableBlueprintCoverDoesNotReportFailedBindingAsSuccessIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := newOCT02IsolatedDatabase(t, ctx)
	server := &Server{db: pool, cfg: config.Load()}
	var owner, fileID int64
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values('cover-reuse-user','cover@example.invalid','test') returning id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	settings, err := server.sealSystemSetting(ossConfigPayload{Enabled: true, Bucket: "test", Region: "cn-hangzhou", Endpoint: "https://storage.invalid", AccessKeyID: "synthetic-key", AccessKeySecret: "synthetic-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb) on conflict(key) do update set value=excluded.value`, settings); err != nil {
		t.Fatal(err)
	}
	blueprintInternalID, blueprintPublicID, err := server.createPendingBlueprint(ctx, owner, "cover-test.nbt", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	source := "blueprint_cover:" + blueprintPublicID
	category := ossBlueprintTextCategory(blueprintPublicID, "cover")
	objectKey := "mcmods/" + category + "/cover.png"
	digest := strings.Repeat("a", 64)
	if err = pool.QueryRow(ctx, `insert into oss_files(bucket,endpoint,region,object_key,category,source,original_name,content_type,size_bytes,sha256,uploader_id,status,scan_status) values('test','https://storage.invalid','cn-hangzhou',$1,$2,$3,'cover.png','image/png',128,$4,$5,'active','clean') returning id`, objectKey, category, source, digest, owner).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(ossDirectUploadRequest{OriginalName: "cover.png", ContentType: "image/png", SizeBytes: 128, SHA256: digest, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	invoke := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/users/me/oss/uploads/presign", strings.NewReader(string(body)))
		req = req.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: owner}))
		response := httptest.NewRecorder()
		server.createUserOSSDirectUpload(response, req)
		return response
	}
	if _, err = pool.Exec(ctx, `alter table blueprints add constraint oct02_cover_bind_fault check(cover_file_id is null)`); err != nil {
		t.Fatal(err)
	}
	response := invoke()
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("binding DB failure status=%d want500", response.Code)
	}
	if strings.Contains(response.Body.String(), "oct02_cover_bind_fault") {
		t.Fatal("internal SQL constraint leaked")
	}
	var stored *int64
	if err = pool.QueryRow(ctx, `select cover_file_id from blueprints where id=$1`, blueprintInternalID).Scan(&stored); err != nil || stored != nil {
		t.Fatal("failed cover binding changed durable facts")
	}
	if _, err = pool.Exec(ctx, `alter table blueprints drop constraint oct02_cover_bind_fault`); err != nil {
		t.Fatal(err)
	}
	response = invoke()
	if response.Code != http.StatusOK {
		t.Fatalf("reuse retry status=%d body=%s", response.Code, response.Body.String())
	}
	if err = pool.QueryRow(ctx, `select cover_file_id from blueprints where id=$1`, blueprintInternalID).Scan(&stored); err != nil || stored == nil || *stored != fileID {
		t.Fatal("successful reuse did not persist cover")
	}
	if strings.Contains(fmt.Sprint(response.Body.String()), `"uploadRequired":true`) {
		t.Fatal("reuse needlessly requested new upload")
	}
}
