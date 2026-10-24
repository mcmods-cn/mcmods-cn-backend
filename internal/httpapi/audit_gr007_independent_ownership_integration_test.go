package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/config"
)

// These assertions use the mounted server and real PostgreSQL. The attempt is
// claimed and a planned artifact registered through the actual Worker helpers;
// this is not a queue delivery or complete Worker execution claim.
func TestAuditGR007IndependentCreatorMatrixWithLiveAttemptHTTPPostgresIntegration(t *testing.T) {
	for _, source := range []string{"mcmods_exporter", iconRendererImportSource} {
		t.Run(source, func(t *testing.T) {
			f := newAuditFixGRFixture(t, 4*time.Minute)
			base := "/api/v1/mods/audit-gr-" + f.modCode + "/export-imports"
			file := ""
			if source == "mcmods_exporter" {
				file = f.upload(t, auditFixGRZip(t, nil))
			} else {
				file = auditActivityCatalogUpload(t, f)
				base = "/api/v1/mods/audit-gr-" + f.modCode + "/catalog-imports"
			}
			body := map[string]any{"ossFileId": file, "targetVersionPublicId": f.versionPublic}
			if source != "mcmods_exporter" {
				body["source"] = source
			}
			job := decodeTEST022Data[modExportJobResponse](t, f.require(t, f.editor, http.MethodPost, base, body, 202))
			runToken, err := f.server.claimModExportJob(f.ctx, job.ID)
			if err != nil || runToken == "" {
				t.Fatalf("actual claim failed: %v", err)
			}
			cfg := defaultOSSConfig()
			cfg.Bucket = "audit-owned"
			cfg.Endpoint = f.provider.URL
			cfg.Region = "cn-test"
			pixels := test039PNG(t, 16, 16, 97)
			digest := sha256.Sum256(pixels)
			objectKey := "audit-gr007/" + job.ID + "/planned.png"
			files, err := f.server.registerCatalogImportArtifacts(f.ctx, job.ID, runToken, cfg, f.userIDs[f.editor], source, []modExportUploadAsset{{ObjectKey: objectKey, Original: "planned.png", Digest: hex.EncodeToString(digest[:]), ContentType: "image/png", ByteLength: int64(len(pixels)), Data: pixels}})
			if err != nil || len(files) != 1 {
				t.Fatalf("actual planned artifact registration: %v", err)
			}
			snapshot := func() map[string]string {
				t.Helper()
				out := map[string]string{}
				for _, table := range []string{"catalog_import_jobs", "catalog_import_packages", "catalog_import_revisions", "catalog_import_job_logs", "catalog_import_job_artifacts", "oss_files", "oss_object_deletion_outbox", "nats_outbox", "activity_event_outbox", "user_activity_events", "oss_multipart_sessions", "oss_user_quota_usage", "oss_user_daily_quota_usage", "oss_user_upload_quota_reservations"} {
					var raw string
					err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` fact`).Scan(&raw)
					if err != nil {
						t.Fatal(err)
					}
					out[table] = raw
				}
				return out
			}
			unchanged := func(before map[string]string) {
				t.Helper()
				if after := snapshot(); !reflect.DeepEqual(before, after) {
					for table, value := range before {
						if after[table] != value {
							t.Errorf("denied request changed %s", table)
						}
					}
					t.FailNow()
				}
			}
			ownerAction := func(path string, want int) []byte {
				t.Helper()
				before := snapshot()
				req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.origin.URL+path, nil)
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Authorization", "Bearer "+f.editor)
				response, err := f.origin.Client().Do(req)
				if err != nil {
					t.Fatal("local creator request failed")
				}
				raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
				response.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if response.StatusCode == want {
					return raw
				}
				if response.StatusCode != http.StatusTooManyRequests {
					t.Fatalf("creator action status=%d want=%d", response.StatusCode, want)
				}
				var problem struct {
					Code string `json:"code"`
				}
				if json.Unmarshal(raw, &problem) != nil || problem.Code != "rate_limited" {
					t.Fatal("unexpected creator rejection")
				}
				unchanged(before)
				seconds, err := strconv.Atoi(response.Header.Get("Retry-After"))
				if err != nil || seconds < 1 || seconds > 60 {
					t.Fatalf("invalid original quota Retry-After=%q", response.Header.Get("Retry-After"))
				}
				t.Logf("GR007 fixture observes default quota429 with zero task writes; explicit creator retry after Retry-After=%d", seconds)
				select {
				case <-time.After(time.Duration(seconds)*time.Second + 100*time.Millisecond):
				case <-f.ctx.Done():
					t.Fatal(f.ctx.Err())
				}
				return f.require(t, f.editor, http.MethodPost, path, nil, want)
			}
			actor := func(label string, permissions ...string) string {
				t.Helper()
				id, _, token := createTEST044User(t, f.ctx, f.db, config.Load(), fmt.Sprintf("gr007-%s-%d", label, time.Now().UnixNano()), "UTC")
				if _, err := f.db.Exec(f.ctx, `update users set created_at=now()-interval '30 days',email_verified=true where id=$1`, id); err != nil {
					t.Fatal(err)
				}
				if len(permissions) > 0 {
					grantTEST044Permissions(t, f.ctx, f.db, id, permissions...)
				}
				return token
			}
			denyMatrix := func(phase string) {
				t.Helper()
				cases := []struct {
					name, token, path string
					read, write       int
				}{
					{"anonymous", "", base, 401, 401},
					{"ordinary", actor(phase + "-ordinary"), base, 403, 403},
					{"same-project-editor", actor(phase+"-same", "project.edit."+f.modCode), base, 404, 409},
					{"cross-project", actor(phase+"-cross", "project.edit."+f.otherModCode), base, 403, 403},
					{"other-project-own-route", actor(phase+"-wrong-target", "project.edit."+f.otherModCode), "/api/v1/mods/audit-gr-" + f.otherModCode + base[len("/api/v1/mods/audit-gr-"+f.modCode):], 404, 409},
					{"independent-reviewer", actor(phase+"-review", "content.review"), base, 403, 403},
					{"admin-project-editor", actor(phase+"-admin", "admin.*", "project.edit."+f.modCode), base, 404, 409},
				}
				for _, c := range cases {
					t.Run(phase+"/"+c.name, func(t *testing.T) {
						before := snapshot()
						f.require(t, c.token, http.MethodGet, c.path+"/"+job.ID, nil, c.read)
						unchanged(before)
						f.require(t, c.token, http.MethodPost, c.path+"/"+job.ID+"/cancel", nil, c.write)
						unchanged(before)
						f.require(t, c.token, http.MethodPost, c.path+"/"+job.ID+"/retry", nil, c.write)
						unchanged(before)
						if source == "mcmods_exporter" && (c.name == "same-project-editor" || c.name == "admin-project-editor") {
							raw := f.require(t, c.token, http.MethodGet, base+"/active?targetVersionId="+f.versionPublic, nil, 200)
							result := decodeTEST022Data[struct {
								Job *modExportJobResponse `json:"job"`
							}](t, raw)
							if result.Job != nil {
								t.Fatal("active task endpoint disclosed another creator's job")
							}
							unchanged(before)
						}
					})
				}
			}
			before := snapshot()
			read := decodeTEST022Data[modExportJobResponse](t, f.require(t, f.editor, http.MethodGet, base+"/"+job.ID, nil, 200))
			if read.ID != job.ID || read.Status != "validating" {
				t.Fatal("creator cannot read actual claimed attempt")
			}
			unchanged(before)
			denyMatrix("claimed-nonempty-artifact")
			ownerAction(base+"/"+job.ID+"/cancel", 200)
			var status, token, artifact, fileStatus string
			if err = f.db.QueryRow(f.ctx, `select j.status,j.run_token,a.status,o.status from catalog_import_jobs j join catalog_import_job_artifacts a on a.job_id=j.id join oss_files o on o.id=a.oss_file_id where j.id=$1`, job.ID).Scan(&status, &token, &artifact, &fileStatus); err != nil {
				t.Fatal(err)
			}
			if status != "cancelled" || token != "" || artifact != "abandoned" || fileStatus != "deleted" {
				t.Fatalf("creator cancel did not fence and compensate attempt: %s/%t/%s/%s", status, token == "", artifact, fileStatus)
			}
			denyMatrix("cancelled-before-retry")
			beforeOutbox := f.number(t, `select count(*) from nats_outbox where aggregate_id=$1`, job.ID)
			retried := decodeTEST022Data[modExportJobResponse](t, ownerAction(base+"/"+job.ID+"/retry", 202))
			if retried.ID != job.ID || retried.Status != "queued" {
				t.Fatal("creator retry did not preserve task identity")
			}
			var createdBy, uploadedBy, sourceOwner int64
			if err = f.db.QueryRow(f.ctx, `select j.created_by,p.uploaded_by,o.uploader_id from catalog_import_jobs j join catalog_import_packages p on p.id=j.package_id join oss_files o on o.id=p.archive_file_id where j.id=$1`, job.ID).Scan(&createdBy, &uploadedBy, &sourceOwner); err != nil {
				t.Fatal(err)
			}
			if createdBy != f.userIDs[f.editor] || uploadedBy != createdBy || sourceOwner != createdBy {
				t.Fatalf("retry silently reassigned source ownership: %d/%d/%d", createdBy, uploadedBy, sourceOwner)
			}
			if n := f.number(t, `select count(*) from nats_outbox where aggregate_id=$1`, job.ID); n != beforeOutbox+1 {
				t.Fatalf("retry intent count before=%d after=%d", beforeOutbox, n)
			}
			before = snapshot()
			f.require(t, f.editor, http.MethodPost, base+"/"+job.ID+"/retry", nil, 409)
			unchanged(before)
			metadata, _ := json.Marshal(map[string]any{"source": source, "role_cases_per_phase": 7, "phases": 2, "planned_artifact_before_denials": 1, "creator_preserved": createdBy == sourceOwner})
			t.Log("GR007_POSTCONDITIONS " + string(metadata))
		})
	}
}
