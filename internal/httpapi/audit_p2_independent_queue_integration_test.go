package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestAuditP2FID002DistinctQueueRelationsAboveJSSafeIntegerHTTPPostgresIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "admin.config.read", "admin.config.write")
	owners := []int64{f.userIDs[f.editor], f.userIDs[f.otherEditor]}
	mods := []int64{f.modID, f.otherModID}
	modPublic := []string{f.modCode, f.otherModCode}
	users, files := make([]string, 2), make([]string, 2)
	for i, owner := range owners {
		if err := f.db.QueryRow(f.ctx, `select public_id from users where id=$1`, owner).Scan(&users[i]); err != nil {
			t.Fatal(err)
		}
		if err := f.db.QueryRow(f.ctx, `insert into oss_files(id,bucket,endpoint,region,object_key,uploader_id) values($1,'fixture','https://example.test','fixture',$2,$3) returning public_id`, int64(9007199254741001+i*2), fmt.Sprintf("p2-queue-source-%d", i), owner).Scan(&files[i]); err != nil {
			t.Fatal(err)
		}
	}
	for _, queue := range []struct{ table, route, relation, ready string }{
		{"oss_object_deletion_outbox", "oss-deletions", "fileId", "pending"},
		{"oss_rehome_jobs", "oss-rehomes", "modId", "queued"},
		{"oss_multipart_sessions", "oss-multipart-sessions", "ownerId", "cleanup_pending"},
	} {
		ids := make([]string, 2)
		for i, owner := range owners {
			internal := int64(9007199254741011 + i*2)
			var err error
			switch queue.table {
			case "oss_object_deletion_outbox":
				err = f.db.QueryRow(f.ctx, `insert into oss_object_deletion_outbox(id,oss_file_id,bucket,endpoint,region,object_key,status,last_replayed_by) values($1,$2,'fixture','https://example.test','fixture',$3,'dead',$4) returning public_id`, internal, int64(9007199254741001+i*2), fmt.Sprintf("p2-queue-source-%d", i), owner).Scan(&ids[i])
			case "oss_rehome_jobs":
				err = f.db.QueryRow(f.ctx, `insert into oss_rehome_jobs(id,mod_id,status,last_replayed_by) values($1,$2,'dead',$3) returning public_id`, internal, mods[i], owner).Scan(&ids[i])
			case "oss_multipart_sessions":
				err = f.db.QueryRow(f.ctx, `insert into oss_multipart_sessions(id,owner_id,bucket,endpoint,region,object_key,upload_id,original_name,content_type,size_bytes,sha256,category,source,status,expires_at,last_replayed_by) values($1,$2,'fixture','https://example.test','fixture',$3,$3,'p2.bin','application/octet-stream',1,repeat('a',64),'audit','audit','dead',now()+interval '1 hour',$2) returning public_id`, internal, owner, fmt.Sprintf("p2-multipart-%d", i)).Scan(&ids[i])
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		path := "/api/v1/admin/infrastructure/" + queue.route
		page := decodeTEST022Data[struct {
			Items []map[string]json.RawMessage `json:"items"`
		}](t, f.require(t, f.reviewer, http.MethodGet, path, nil, 200))
		if len(page.Items) != 2 {
			t.Fatalf("%s rows=%d", queue.table, len(page.Items))
		}
		for i, id := range ids {
			var row map[string]json.RawMessage
			for _, item := range page.Items {
				var got string
				if json.Unmarshal(item["id"], &got) == nil && got == id {
					row = item
				}
			}
			if row == nil {
				t.Fatalf("%s missing public identity=%s", queue.table, id)
			}
			want := map[string]string{"fileId": files[i], "modId": modPublic[i], "ownerId": users[i]}[queue.relation]
			for field, expected := range map[string]string{queue.relation: want, "lastReplayedBy": users[i]} {
				var got string
				if err := json.Unmarshal(row[field], &got); err != nil || got != expected {
					t.Fatalf("%s identity=%s field=%s value=%s", queue.table, id, field, row[field])
				}
			}
		}
		f.require(t, f.reviewer, http.MethodPost, path+"/"+ids[1]+"/replay", nil, 202)
		for i, id := range ids {
			var status string
			var replays int
			if err := f.db.QueryRow(f.ctx, `select status,replay_count from `+queue.table+` where public_id=$1`, id).Scan(&status, &replays); err != nil {
				t.Fatal(err)
			}
			want, count := "dead", 0
			if i == 1 {
				want, count = queue.ready, 1
			}
			if status != want || replays != count {
				t.Fatalf("%s targeted replay row=%d state=%s/%d want=%s/%d", queue.table, i, status, replays, want, count)
			}
		}
	}
}
