package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/config"
)

// These cases exercise the mounted HTTP handlers and real PostgreSQL. Object
// storage is the existing synthetic provider; no vendor execution is asserted.
func auditP2NormalImportUpload(t *testing.T, f auditFixGRFixture, raw []byte, source, name, contentType string) string {
	t.Helper()
	digest := sha256.Sum256(raw)
	base := "/api/v1/mods/audit-gr-" + f.modCode + "/export-imports/uploads/"
	if source != "mcmods_exporter" {
		base = "/api/v1/mods/audit-gr-" + f.modCode + "/catalog-imports/uploads/"
	}
	ticket := decodeTEST022Data[test037Ticket](t, f.require(t, f.editor, http.MethodPost, base+"presign", map[string]any{
		"originalName": name, "contentType": contentType, "source": source, "sizeBytes": len(raw), "sha256": hex.EncodeToString(digest[:]),
	}, 200))
	request, err := http.NewRequestWithContext(f.ctx, ticket.Method, ticket.URL, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range ticket.Headers {
		request.Header.Set(key, value)
	}
	response, err := f.provider.Client().Do(request)
	if err != nil {
		t.Fatal("synthetic normal upload request failed")
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("normal upload status=%d", response.StatusCode)
	}
	file := decodeTEST022Data[map[string]any](t, f.require(t, f.editor, http.MethodPost, base+"complete", ticket.completion(), 201))
	id, ok := file["id"].(string)
	if !ok || id == "" {
		t.Fatal("normal completed upload has no public file identity")
	}
	return id
}

func TestAuditP2GR005ExactInt32BoundaryBothImportHandlersHTTPPostgresIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	for _, userID := range []int64{2147483647, 2147483648} {
		t.Run(fmt.Sprint(userID), func(t *testing.T) {
			if _, err := f.db.Exec(f.ctx, `select setval(pg_get_serial_sequence('users','id'),$1,false)`, userID); err != nil {
				t.Fatal(err)
			}
			actual, _, token := createTEST044User(t, f.ctx, f.db, config.Load(), fmt.Sprintf("p2-boundary-%d", userID), "UTC")
			if actual != userID {
				t.Fatalf("actual generated identity=%d want=%d", actual, userID)
			}
			if _, err := f.db.Exec(f.ctx, `update users set created_at=now()-interval '30 days',email_verified=true where id=$1`, userID); err != nil {
				t.Fatal(err)
			}
			grantTEST044Permissions(t, f.ctx, f.db, userID, "project.edit."+f.modCode, "user.file.single_limit.64", "user.file.daily_limit.256", "user.file.total_limit.256")
			actor := f
			actor.editor = token
			for _, source := range []string{"mcmods_exporter", "iconrenderer"} {
				t.Run(source, func(t *testing.T) {
					var file, path string
					body := map[string]any{"targetVersionPublicId": f.versionPublic}
					if source == "mcmods_exporter" {
						file = auditP2NormalImportUpload(t, actor, auditFixGRZip(t, map[string]string{"registries/items.json": fmt.Sprintf(`{"registry":"items","entries":[{"id":"example:boundary_%d","namespace":"example","names":{"en-US":"Boundary"}}]}`, userID)}), source, "boundary.zip", "application/zip")
						path = "/export-imports"
					} else {
						file = auditP2NormalImportUpload(t, actor, []byte(fmt.Sprintf(`{"registerName":"example:boundary_%d","name":"Boundary"}`, userID)), source, "boundary.json", "application/json")
						path = "/catalog-imports"
						body["source"] = source
					}
					body["ossFileId"] = file
					before := f.number(t, `select count(*) from user_activity_events where user_id=$1`, userID)
					job := decodeTEST022Data[modExportJobResponse](t, actor.require(t, token, http.MethodPost, "/api/v1/mods/audit-gr-"+f.modCode+path, body, 202))
					var createdBy, uploadedBy, owner int64
					var status string
					if err := f.db.QueryRow(f.ctx, `select j.created_by,p.uploaded_by,o.uploader_id,j.status from catalog_import_jobs j join catalog_import_packages p on p.id=j.package_id join oss_files o on o.id=p.archive_file_id where j.id=$1`, job.ID).Scan(&createdBy, &uploadedBy, &owner, &status); err != nil {
						t.Fatal(err)
					}
					if createdBy != userID || uploadedBy != userID || owner != userID || status != "queued" {
						t.Fatalf("actor propagation=%d/%d/%d status=%s", createdBy, uploadedBy, owner, status)
					}
					if after := f.number(t, `select count(*) from user_activity_events where user_id=$1`, userID); after != before+1 {
						t.Fatalf("actual bigint activity events before=%d after=%d", before, after)
					}
					if n := f.number(t, `select count(*) from nats_outbox where aggregate_type='mod_export_job' and aggregate_id=$1`, job.ID); n != 1 {
						t.Fatalf("job intent rows=%d want=1", n)
					}
					// A different normal editor has no ownership of this source.
					beforeJobs := f.number(t, `select count(*) from catalog_import_jobs`)
					beforePackages := f.number(t, `select count(*) from catalog_import_packages`)
					beforeActivity := f.number(t, `select count(*) from user_activity_events`)
					beforeOutbox := f.number(t, `select count(*) from nats_outbox`)
					rejector, rejection := f.otherEditor, 403
					if userID == 2147483648 {
						rejector, rejection = f.editor, 400 // same-project editor, foreign file
					}
					f.require(t, rejector, http.MethodPost, "/api/v1/mods/audit-gr-"+f.modCode+path, body, rejection)
					if f.number(t, `select count(*) from catalog_import_jobs`) != beforeJobs || f.number(t, `select count(*) from catalog_import_packages`) != beforePackages || f.number(t, `select count(*) from user_activity_events`) != beforeActivity || f.number(t, `select count(*) from nats_outbox`) != beforeOutbox {
						t.Fatal("denied import changed job/package/activity/outbox")
					}
				})
			}
		})
	}
}

func TestAuditP2CONT004NormalizedNumericHTTPRejectsWithoutWritesIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "content.write")
	counts := func() [4]int64 {
		t.Helper()
		var result [4]int64
		for i, table := range []string{"catalog_entities", "content_revisions", "change_requests", "nats_outbox"} {
			if err := f.db.QueryRow(f.ctx, `select count(*) from `+table).Scan(&result[i]); err != nil {
				t.Fatal(err)
			}
		}
		return result
	}
	definitions := []map[string]any{
		{"lightLevel": 16}, {"lightLevel": 1.5}, {"maxStackSize": 0}, {"durability": 1.5}, {"temperature": -1},
		{"lightLevel": "16"}, {"lightLevel": true}, {"maxStackSize": []any{64}},
		{"physical": map[string]any{"lightLevel": 16}}, {"physical": map[string]any{"lightLevel": 1.5}}, {"tool": map[string]any{"durability": 1.5}}, {"fluid": map[string]any{"temperature": -1}},
		{"physical": map[string]any{"lightLevel": "16"}}, {"fluid": map[string]any{"temperature": false}},
	}
	for index, definition := range definitions {
		before := counts()
		body := map[string]any{"kindCode": "minecraft.item", "canonicalId": fmt.Sprintf("audit:p2-invalid-%d", index), "defaultLocale": "en-US", "localizations": []map[string]string{{"locale": "en-US", "name": "Invalid numeric fixture"}}, "definition": definition}
		raw := f.require(t, f.editor, http.MethodPost, "/api/v1/catalog/resources", body, 422)
		var problem struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		if err := json.Unmarshal(raw, &problem); err != nil || problem.Code != "HTTP_422" || !strings.Contains(problem.Error, "numeric property") {
			t.Fatalf("case %d did not reject the specific numeric rule: %s", index, raw)
		}
		if after := counts(); after != before {
			t.Fatalf("case %d illegal numeric definition changed rows before=%v after=%v", index, before, after)
		}
	}
	for index, definition := range []map[string]any{{"lightLevel": 0}, {"lightLevel": 15}, {"temperature": 0}, {"physical": map[string]any{"lightLevel": 15}}, {"fluid": map[string]any{"temperature": 0}}} {
		body := map[string]any{"kindCode": "minecraft.item", "canonicalId": fmt.Sprintf("audit:p2-valid-%d", index), "defaultLocale": "en-US", "localizations": []map[string]string{{"locale": "en-US", "name": "Valid boundary fixture"}}, "definition": definition}
		result := decodeTEST022Data[catalogEditResult](t, f.require(t, f.editor, http.MethodPost, "/api/v1/catalog/resources", body, 202))
		if result.ReviewStatus != "pending" || result.RevisionID == "" || result.ChangeRequestID == "" {
			t.Fatal("valid numeric boundary did not produce a reviewable ordinary-user revision")
		}
		f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+result.RevisionID, map[string]string{"status": "approved", "note": "Independent numeric boundary"}, 200)
	}
}
