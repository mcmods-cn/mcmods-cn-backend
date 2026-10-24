package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAuditFID002QueuePublicIDsRoundTripAboveJSSafeIntegerFullHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "admin.config.read", "admin.config.write")
	var filePublicID, userPublicID string
	if err := f.db.QueryRow(f.ctx, `select public_id from users where id=$1`, f.userIDs[f.editor]).Scan(&userPublicID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `insert into oss_files(id,bucket,endpoint,region,object_key,uploader_id) values(9007199254740993,'fixture','https://example.test','fixture','audit-queue-source',$1) returning public_id`, f.userIDs[f.editor]).Scan(&filePublicID); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ table, route, sql, relation, want, status string }{
		{"oss_object_deletion_outbox", "oss-deletions", `insert into oss_object_deletion_outbox(id,oss_file_id,bucket,endpoint,region,object_key,status,last_replayed_by) values(9007199254740995,9007199254740993,'fixture','https://example.test','fixture','audit-queue-source','dead',$1) returning public_id`, "fileId", filePublicID, "pending"},
		{"oss_rehome_jobs", "oss-rehomes", `insert into oss_rehome_jobs(id,mod_id,status,last_replayed_by) values(9007199254740997,$2,'dead',$1) returning public_id`, "modId", f.modCode, "queued"},
		{"oss_multipart_sessions", "oss-multipart-sessions", `insert into oss_multipart_sessions(id,owner_id,bucket,endpoint,region,object_key,upload_id,original_name,content_type,size_bytes,sha256,category,source,status,expires_at,last_replayed_by) values(9007199254740999,$1,'fixture','https://example.test','fixture','audit-queue-multipart','fixture-upload','fixture.bin','application/octet-stream',1,repeat('a',64),'audit','audit','dead',now()+interval '1 hour',$1) returning public_id`, "ownerId", userPublicID, "cleanup_pending"},
	} {
		var publicID string
		args := []any{f.userIDs[f.editor]}
		if row.table == "oss_rehome_jobs" {
			args = append(args, f.modID)
		}
		if err := f.db.QueryRow(f.ctx, row.sql, args...).Scan(&publicID); err != nil {
			t.Fatal(err)
		}
		path := "/api/v1/admin/infrastructure/" + row.route
		f.require(t, f.editor, http.MethodGet, path, nil, 403)
		page := decodeTEST022Data[struct {
			Items []map[string]json.RawMessage `json:"items"`
		}](t, f.require(t, f.reviewer, http.MethodGet, path, nil, 200))
		if len(page.Items) != 1 {
			t.Fatalf("%s rows=%d", row.route, len(page.Items))
		}
		for field, want := range map[string]string{"id": publicID, row.relation: row.want, "lastReplayedBy": userPublicID} {
			var got string
			if err := json.Unmarshal(page.Items[0][field], &got); err != nil || got != want {
				t.Fatalf("%s %s=%s err=%v", row.route, field, page.Items[0][field], err)
			}
		}
		f.require(t, f.editor, http.MethodPost, path+"/"+publicID+"/replay", nil, 403)
		f.require(t, f.reviewer, http.MethodPost, path+"/9007199254740995/replay", nil, 400)
		f.require(t, f.reviewer, http.MethodPost, path+"/unknown99/replay", nil, 404)
		f.require(t, f.reviewer, http.MethodPost, path+"/"+publicID+"/replay", nil, 202)
		var state string
		var count int
		if err := f.db.QueryRow(f.ctx, `select status,replay_count from `+row.table+` where public_id=$1`, publicID).Scan(&state, &count); err != nil || state != row.status || count != 1 {
			t.Fatalf("%s state=%s count=%d err=%v", row.route, state, count, err)
		}
		f.require(t, f.reviewer, http.MethodPost, path+"/"+publicID+"/replay", nil, 409)
	}
}
