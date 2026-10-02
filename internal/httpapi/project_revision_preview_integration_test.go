package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestProjectRevisionPreviewUsesExactReviewScopeAndTypedSnapshotIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 with an owned test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	poolCfg, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolCfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := database.DropEphemeralSchema(context.Background(), pool); err != nil {
			t.Error(err)
		}
	}()
	if _, err = pool.Exec(ctx, `insert into users(id,username,email,password_hash) values
 (99380001,'preview-owner','preview-owner@example.invalid','test-only'),
 (99380002,'preview-reviewer','preview-reviewer@example.invalid','test-only'),
 (99380003,'preview-other','preview-other@example.invalid','test-only');
 insert into oss_files(bucket,endpoint,object_key,category,source,original_name,content_type,size_bytes,sha256,uploader_id,status,scan_status)
 values('preview-test','https://preview-storage.invalid','private/preview.png','user/private','user','preview.png','image/png',128,repeat('a',64),99380001,'active','clean')`); err != nil {
		t.Fatal(err)
	}
	cfg := ossConfigPayload{Enabled: true, Region: "cn-hangzhou", Bucket: "preview-test", Endpoint: "https://oss-cn-hangzhou.aliyuncs.com", PublicEndpoint: "https://preview-storage.invalid", AccessKeyID: "synthetic-key", AccessKeySecret: "synthetic-test-secret"}
	server := &Server{db: pool, cfg: config.Config{SettingsEncryptionKey: "synthetic-preview-settings-key-at-least-32-characters"}, mux: http.NewServeMux()}
	server.routes()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := security.EncryptSetting(server.cfg.SettingsEncryptionKey, raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb) on conflict(key) do update set value=excluded.value`, string(sealed)); err != nil {
		t.Fatal(err)
	}
	const icon = "https://preview-storage.invalid/private/preview.png"
	invoke := func(revisionID string, claims security.Claims) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/content-revisions/"+revisionID, nil)
		req.SetPathValue("revisionId", revisionID)
		req = req.WithContext(context.WithValue(ctx, claimsContextKey, claims))
		response := httptest.NewRecorder()
		server.projectRevisionPreview(response, req)
		return response
	}
	reviewer := func(projectID string) security.Claims {
		return security.Claims{Subject: 99380002, PermissionRules: []security.PermissionRule{{Code: "project.review." + projectID, Allow: true, Priority: 100}}}
	}
	var lastRevision string
	var lastTargetID int64
	for index, kind := range []string{"mod", "modpack", "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon"} {
		t.Run(kind, func(t *testing.T) {
			slug := fmt.Sprintf("preview-target-%d", index)
			var targetID int64
			var publicID string
			aggregate := "simple_project"
			switch kind {
			case "mod":
				aggregate = "mod"
				publicID = fmt.Sprintf("prev%05d", index)
				err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by) values($1,$2,'Preview target','pending',99380001) returning id`, publicID, slug).Scan(&targetID)
			case "modpack":
				aggregate = "modpack"
				err = pool.QueryRow(ctx, `insert into modpacks(slug,primary_name,review_status,submitted_by) values($1,'Preview target','pending',99380001) returning id,public_id`, slug).Scan(&targetID, &publicID)
			default:
				err = pool.QueryRow(ctx, `insert into simple_projects(project_type,slug,primary_name,review_status,submitted_by) values($1,$2,'Preview target','pending',99380001) returning id,public_id`, kind, slug).Scan(&targetID, &publicID)
			}
			if err != nil {
				t.Fatal(err)
			}
			createRevision := func(actorID int64, version int) string {
				t.Helper()
				var internalID int64
				var revisionID string
				if err := pool.QueryRow(ctx, `insert into content_revisions(entity_type,entity_id,aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash,created_by)
    values($1,$2,$3,$4,$5,jsonb_build_object('projectType',$1::text,'primaryName','Visible title','iconUrl',$6::text,'privateInternalMetadata','must-be-omitted'),'synthetic-hash',$7) returning id,public_id`, kind, targetID, aggregate, publicID, version, icon, actorID).Scan(&internalID, &revisionID); err != nil {
					t.Fatal(err)
				}
				if _, err := pool.Exec(ctx, `insert into change_requests(entity_type,entity_id,aggregate_type,aggregate_key,proposed_revision_id,status,submitted_by) values($1,$2,$3,$4,$5,'pending',$6)`, kind, targetID, aggregate, publicID, internalID, actorID); err != nil {
					t.Fatal(err)
				}
				return revisionID
			}
			revisionID := createRevision(99380001, 1)
			lastRevision = revisionID
			lastTargetID = targetID
			request := httptest.NewRequest(http.MethodGet, "/api/v1/content-revisions/"+revisionID, nil)
			_, pattern := server.mux.Handler(request)
			if pattern != "GET /api/v1/content-revisions/{revisionId}" {
				t.Fatal("preview route not registered")
			}
			response := invoke(revisionID, reviewer(publicID))
			if response.Code != http.StatusOK {
				t.Fatalf("legitimate reviewer returned %d: %s", response.Code, response.Body.String())
			}
			if response.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal("private review preview is cacheable")
			}
			var envelope struct {
				Data struct {
					ID, EntityType, ProjectID, Status string
					Snapshot                          map[string]json.RawMessage
				}
			}
			if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Data.ID != revisionID || envelope.Data.EntityType != kind || envelope.Data.ProjectID != publicID || envelope.Data.Status != "pending" {
				t.Fatal("preview target identity changed")
			}
			if _, ok := envelope.Data.Snapshot["privateInternalMetadata"]; ok {
				t.Fatal("unrecognized snapshot field escaped typed whitelist")
			}
			if !strings.Contains(strings.ToLower(string(envelope.Data.Snapshot["iconUrl"])), "x-oss-signature") {
				t.Fatal("legitimate pending image not signed")
			}
			for _, claims := range []security.Claims{
				{Subject: 99380002}, reviewer("other0001"),
				{Subject: 99380001, PermissionRules: []security.PermissionRule{{Code: "project.review." + publicID, Allow: true, Priority: 100}}},
			} {
				if response := invoke(revisionID, claims); response.Code != http.StatusForbidden {
					t.Fatalf("ordinary/cross-project/self reviewer returned %d", response.Code)
				}
			}
			// A reviewer may inspect a malicious proposal, but its foreign file stays private.
			if _, err = pool.Exec(ctx, `update change_requests set status='rejected' where proposed_revision_id=(select id from content_revisions where public_id=$1)`, revisionID); err != nil {
				t.Fatal(err)
			}
			malicious := createRevision(99380003, 2)
			response = invoke(malicious, reviewer(publicID))
			if response.Code != http.StatusOK {
				t.Fatalf("malicious proposal preview returned %d", response.Code)
			}
			if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if string(envelope.Data.Snapshot["iconUrl"]) != `""` {
				t.Fatal("foreign-uploader snapshot gained a signed URL")
			}
			if _, err = pool.Exec(ctx, `update change_requests set status='rejected' where proposed_revision_id=(select id from content_revisions where public_id=$1)`, malicious); err != nil {
				t.Fatal(err)
			}
			if response = invoke(malicious, reviewer(publicID)); response.Code != http.StatusNotFound {
				t.Fatalf("closed revision returned %d", response.Code)
			}
			if _, err = pool.Exec(ctx, `update change_requests set status='pending' where proposed_revision_id=(select id from content_revisions where public_id=$1)`, revisionID); err != nil {
				t.Fatal(err)
			}
			// The review request must also identify the exact immutable revision target.
			if _, err = pool.Exec(ctx, `update change_requests set aggregate_key='mismatched-target' where proposed_revision_id=(select id from content_revisions where public_id=$1)`, revisionID); err != nil {
				t.Fatal(err)
			}
			if response = invoke(revisionID, reviewer(publicID)); response.Code != http.StatusNotFound {
				t.Fatal("mismatched request target remained readable")
			}
			if _, err = pool.Exec(ctx, `update change_requests set aggregate_key=$2 where proposed_revision_id=(select id from content_revisions where public_id=$1)`, revisionID, publicID); err != nil {
				t.Fatal(err)
			}
		})
	}
	global := security.Claims{Subject: 99380002, PermissionRules: []security.PermissionRule{{Code: "content.review", Allow: true, Priority: 100}}}
	// Normal hard deletion is prevented by the revision-to-route FK. In this
	// owned schema only, suppress the cleanup trigger to simulate a historical
	// orphaned route after resource deletion; the preview must still fail closed.
	if _, err = pool.Exec(ctx, `alter table simple_projects disable trigger user`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `delete from simple_projects where id=$1`, lastTargetID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `alter table simple_projects enable trigger user`); err != nil {
		t.Fatal(err)
	}
	if response := invoke(lastRevision, global); response.Code != http.StatusNotFound {
		t.Fatalf("deleted target returned %d", response.Code)
	}
	if response := invoke("bad", global); response.Code != http.StatusBadRequest {
		t.Fatal("malformed revision was accepted")
	}
	if response := invoke(lastRevision, security.Claims{}); response.Code != http.StatusUnauthorized {
		t.Fatal("anonymous preview was accepted")
	}
}
