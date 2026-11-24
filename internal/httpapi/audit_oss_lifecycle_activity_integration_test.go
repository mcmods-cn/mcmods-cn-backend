package httpapi

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/activity"
)

func TestAuditCMP003OSSReplayCommitsQueueAuditAndExistingActivityHTTPIntegration(t *testing.T) {
	for _, row := range []struct{ table, route, insert, status, logAction string }{
		{"oss_object_deletion_outbox", "oss-deletions", `insert into oss_object_deletion_outbox(id,bucket,endpoint,region,object_key,status) values(9007199254740995,'fixture','https://example.test','fixture','audit-atomic-replay','dead') returning public_id`, "pending", "oss_deletion_replayed"},
		{"oss_rehome_jobs", "oss-rehomes", `insert into oss_rehome_jobs(id,mod_id,status) values(9007199254740997,$1,'dead') returning public_id`, "queued", "oss_rehome_replayed"},
		{"oss_multipart_sessions", "oss-multipart-sessions", `insert into oss_multipart_sessions(id,owner_id,bucket,endpoint,region,object_key,upload_id,original_name,content_type,size_bytes,sha256,category,source,status,expires_at) values(9007199254740999,$1,'fixture','https://example.test','fixture','audit-atomic-multipart','audit-atomic-upload','fixture.bin','application/octet-stream',1,repeat('a',64),'audit','audit','dead',now()+interval '1 hour') returning public_id`, "cleanup_pending", "oss_multipart_cleanup_replayed"},
	} {
		t.Run(row.route, func(t *testing.T) {
			f := newTEST037Fixture(t)
			actor := f.userIDs[f.reviewer]
			grantTEST044Permissions(t, f.ctx, f.db, actor, "admin.config.write")
			var publicID string
			var args []any
			if row.table == "oss_rehome_jobs" {
				args = []any{f.modID}
			}
			if row.table == "oss_multipart_sessions" {
				args = []any{f.userIDs[f.editor]}
			}
			if err := f.db.QueryRow(f.ctx, row.insert, args...).Scan(&publicID); err != nil {
				t.Fatal(err)
			}
			path := "/api/v1/admin/infrastructure/" + row.route + "/" + publicID + "/replay"
			snapshot := func() []string {
				var queue, audit, intents string
				err := f.db.QueryRow(f.ctx, `select (select to_jsonb(job)::text from `+pgx.Identifier{row.table}.Sanitize()+` job where public_id=$1),(select coalesce(jsonb_agg(to_jsonb(entry) order by id),'[]'::jsonb)::text from app_logs entry where action=$2),(select coalesce(jsonb_agg(to_jsonb(intent) order by id),'[]'::jsonb)::text from activity_event_outbox intent where user_id=$3)`, publicID, row.logAction, actor).Scan(&queue, &audit, &intents)
				if err != nil {
					t.Fatal(err)
				}
				return []string{queue, audit, intents}
			}
			auditFixInjectActivityFailure(t, f.test013Fixture, actor, true)
			before := snapshot()
			auditOSSRequire(t, f, f.editor, http.MethodPost, path, nil, 403)
			auditOSSRequire(t, f, f.reviewer, http.MethodPost, path, nil, 500)
			if after := snapshot(); !reflect.DeepEqual(before, after) {
				t.Fatal("failed replay changed queue, security audit or original activity")
			}
			auditFixInjectActivityFailure(t, f.test013Fixture, actor, false)
			var prior int
			if err := f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where user_id=$1 and action_id=$2 and object_type_id=$3`, actor, activity.ActionCreate, activity.ObjectFile).Scan(&prior); err != nil {
				t.Fatal(err)
			}
			auditOSSRequire(t, f, f.reviewer, http.MethodPost, path, nil, 202)
			var status string
			var count, audits, intents int
			var by int64
			if err := f.db.QueryRow(f.ctx, `select status,replay_count,last_replayed_by,(select count(*) from app_logs where action=$2),(select count(*) from activity_event_outbox where user_id=$3 and action_id=$4 and object_type_id=$5) from `+pgx.Identifier{row.table}.Sanitize()+` where public_id=$1`, publicID, row.logAction, actor, activity.ActionCreate, activity.ObjectFile).Scan(&status, &count, &by, &audits, &intents); err != nil || status != row.status || count != 1 || by != actor || audits != 1 || intents != prior+1 {
				t.Fatalf("status/count/actor/audits/intents=%s/%d/%d/%d/%d prior=%d err=%v", status, count, by, audits, intents, prior, err)
			}
			before = snapshot()
			auditOSSRequire(t, f, f.reviewer, http.MethodPost, path, nil, 409)
			if after := snapshot(); !reflect.DeepEqual(before, after) {
				t.Fatal("nondead replay changed facts")
			}
		})
	}
}

// The historical test name is retained for execution-ledger continuity. This
// asserts failed activity grants no ticket/counter and successful issuance keeps
// the existing counter; the separate telemetry-failure case proves ARCH020's
// best-effort counter is not a mandatory transaction participant.
func TestAuditCMP003OSSSignedDownloadCommitsCounterAndExistingActivityHTTPIntegration(t *testing.T) {
	for _, scope := range []string{"user", "admin"} {
		t.Run(scope, func(t *testing.T) {
			f := newTEST037Fixture(t)
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "oss.read")
			source := []byte("owned atomic download ticket")
			ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "ticket.zip", "application/octet-stream", "test037-atomic", source)
			f.put(t, ticket, source)
			auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201)
			route := "/api/v1/users/me/files/presign"
			if scope == "admin" {
				route = "/api/v1/admin/oss/files/presign"
			}
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
			before := auditOSSAllFacts(t, f)
			raw := auditOSSRequire(t, f, f.editor, http.MethodPost, route, map[string]any{"objectKey": ticket.ObjectKey}, 500)
			if !strings.Contains(string(raw), "failed to persist OSS download") {
				t.Fatalf("unrelated download fault: %s", raw)
			}
			if after := auditOSSAllFacts(t, f); !reflect.DeepEqual(before, after) {
				t.Fatal("failed download issued counter or activity")
			}
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
			var prior int
			if err := f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where user_id=$1 and action_id=$2 and object_type_id=$3`, f.userIDs[f.editor], activity.ActionCreate, activity.ObjectFile).Scan(&prior); err != nil {
				t.Fatal(err)
			}
			auditOSSRequire(t, f, f.editor, http.MethodPost, route, map[string]any{"objectKey": ticket.ObjectKey}, 200)
			var downloads, intents int
			if err := f.db.QueryRow(f.ctx, `select (select downloads from oss_download_stats where object_key=(select stored_object_key from oss_upload_byte_bindings where upload_object_key=$1 and status='committed')),(select count(*) from activity_event_outbox where user_id=$2 and action_id=$3 and object_type_id=$4)`, ticket.ObjectKey, f.userIDs[f.editor], activity.ActionCreate, activity.ObjectFile).Scan(&downloads, &intents); err != nil || downloads != 1 || intents != prior+1 {
				t.Fatalf("downloads/intents=%d/%d prior=%d err=%v", downloads, intents, prior, err)
			}
			t.Logf("%s signed capability preserves existing %s/%s classification; one actual counter+intent", scope, fmt.Sprint(activity.ActionCreate), fmt.Sprint(activity.ObjectFile))
		})
	}
}

func TestAuditCMP003OSSUploadTicketCommitsReservationBlueprintSessionAndActivityHTTPIntegration(t *testing.T) {
	for _, scope := range []string{"user", "admin", "blueprint", "multipart", "multipart_unavailable_abort", "multipart_abort_deadline"} {
		t.Run(scope, func(t *testing.T) {
			f := newTEST037Fixture(t)
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "oss.write")
			name, size := "atomic-ticket.zip", int64(128)
			if scope == "blueprint" {
				name = "atomic-ticket.nbt"
			}
			if strings.HasPrefix(scope, "multipart") {
				size = ossMultipartThreshold
			}
			route := "/api/v1/users/me/oss/uploads/presign"
			if scope == "admin" {
				route = "/api/v1/admin/oss/uploads/presign"
			}
			body := ossDirectUploadRequest{OriginalName: name, ContentType: "application/octet-stream", SizeBytes: size, SHA256: strings.Repeat("a", 64), Category: "test037", Source: "test037-atomic-ticket"}
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
			before := auditOSSAllFacts(t, f)
			if scope == "multipart_abort_deadline" {
				f.store.waitForAbortDeadline.Store(true)
			} else if scope == "multipart_unavailable_abort" {
				f.store.failAbort.Store(true)
			}
			raw := auditOSSRequire(t, f, f.editor, http.MethodPost, route, body, 500)
			if !strings.Contains(string(raw), "failed to persist OSS upload ticket activity") {
				t.Fatalf("unrelated ticket failure: %s", raw)
			}
			if scope != "multipart_unavailable_abort" && scope != "multipart_abort_deadline" {
				if after := auditOSSAllFacts(t, f); !reflect.DeepEqual(before, after) {
					t.Fatal("unissued ticket committed reservation/blueprint/session/activity")
				}
			} else {
				var cleanup, other, reserved, intents int
				if err := f.db.QueryRow(f.ctx, `select (select count(*) from oss_multipart_sessions where status='cleanup_pending'),(select count(*) from oss_multipart_sessions where status<>'cleanup_pending'),(select count(*) from oss_user_upload_quota_reservations),(select count(*) from activity_event_outbox where user_id=$1)`, f.userIDs[f.editor]).Scan(&cleanup, &other, &reserved, &intents); err != nil || cleanup != 1 || other+reserved+intents != 0 {
					t.Fatalf("explicit cleanup/other/reserved/intents=%d/%d/%d/%d err=%v", cleanup, other, reserved, intents, err)
				}
				f.store.failAbort.Store(false)
				f.store.waitForAbortDeadline.Store(false)
				worker := NewOSSMultipartCleanupWorker(f.server.cfg, f.db)
				worker.drain(f.ctx)
				var aborted int
				if err := f.db.QueryRow(f.ctx, `select count(*) from oss_multipart_sessions where status='aborted'`).Scan(&aborted); err != nil || aborted != 1 {
					t.Fatalf("unissued cleanup aborted=%d err=%v", aborted, err)
				}
			}
			f.store.mu.Lock()
			pending := len(f.store.uploads)
			f.store.mu.Unlock()
			if pending != 0 {
				t.Fatal("unissued provider multipart not aborted or durably recovered")
			}
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
			var prior int
			if err := f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where user_id=$1 and action_id=$2 and object_type_id=$3`, f.userIDs[f.editor], activity.ActionUpload, activity.ObjectFile).Scan(&prior); err != nil {
				t.Fatal(err)
			}
			ticket := decodeTEST022Data[test037Ticket](t, auditOSSRequire(t, f, f.editor, http.MethodPost, route, body, 200))
			var intents, reserved int
			var reservedSource int64
			if err := f.db.QueryRow(f.ctx, `select (select count(*) from activity_event_outbox where user_id=$1 and action_id=$2 and object_type_id=$3),(select count(*) from oss_user_upload_quota_reservations where object_key=$4),coalesce((select source_bytes from oss_user_upload_quota_reservations where object_key=$4),0)`, f.userIDs[f.editor], activity.ActionUpload, activity.ObjectFile, ticket.ObjectKey).Scan(&intents, &reserved, &reservedSource); err != nil || intents != prior+1 {
				t.Fatalf("ticket intents=%d prior=%d err=%v", intents, prior, err)
			}
			wantReserved := 1
			if scope == "admin" {
				wantReserved = 0
			}
			if reserved != wantReserved || (wantReserved == 1 && reservedSource != size) {
				t.Fatalf("reservation=%d source=%d", reserved, reservedSource)
			}
			if scope == "blueprint" {
				var count int
				if err := f.db.QueryRow(f.ctx, `select count(*) from blueprints where owner_id=$1 and original_object_key=$2 and status='uploading' and review_status='pending'`, f.userIDs[f.editor], ticket.ObjectKey).Scan(&count); err != nil || count != 1 {
					t.Fatalf("private pending blueprint=%d err=%v", count, err)
				}
			}
		})
	}
}

func TestAuditCMP003OSSCoverReuseBindingAndActivityCommitTogetherHTTPIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	owner := f.userIDs[f.editor]
	blueprintID, publicID, err := f.server.createPendingBlueprint(f.ctx, owner, "atomic-cover.nbt", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if err = png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "atomic-cover.png", "image/png", "blueprint_cover:"+publicID, data.Bytes())
	f.put(t, ticket, data.Bytes())
	auditOSSInstallTransparentCoverConversion(t, f, ticket)
	auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201)
	if _, err = f.db.Exec(f.ctx, `update blueprints set cover_file_id=null,cover_object_key='' where id=$1`, blueprintID); err != nil {
		t.Fatal(err)
	}
	body := ossDirectUploadRequest{OriginalName: ticket.OriginalName, ContentType: ticket.ContentType, SizeBytes: ticket.SizeBytes, SHA256: ticket.SHA256, Source: ticket.Source}
	auditFixInjectActivityFailure(t, f.test013Fixture, owner, true)
	before := auditOSSAllFacts(t, f)
	raw := auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/presign", body, 500)
	if !strings.Contains(string(raw), "failed to persist reused OSS upload ticket") {
		t.Fatalf("unrelated cover reuse failure: %s", raw)
	}
	if after := auditOSSAllFacts(t, f); !reflect.DeepEqual(before, after) {
		t.Fatal("cover reuse activity failure retained association or changed shared file/quota")
	}
	auditFixInjectActivityFailure(t, f.test013Fixture, owner, false)
	result := decodeTEST022Data[map[string]any](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/presign", body, 200))
	if result["uploadRequired"] != false || result["blueprintId"] != publicID {
		t.Fatal("cover retry did not reuse its source and blueprint")
	}
	var linked int
	if err = f.db.QueryRow(f.ctx, `select count(*) from blueprints where id=$1 and cover_file_id is not null`, blueprintID).Scan(&linked); err != nil || linked != 1 {
		t.Fatalf("recovered cover link=%d err=%v", linked, err)
	}
}

// The owned protocol provider must produce real decodable bytes for the PNG
// conversion requested by production code. It does not emulate real cloud.
func auditOSSInstallTransparentCoverConversion(t *testing.T, f test037Fixture, ticket test037Ticket) {
	t.Helper()
	converted, err := os.ReadFile("testdata/audit-20261024/oss-atomic-transparent-2x2.webp")
	if err != nil {
		t.Fatal(err)
	}
	auditOSSInstallBindingWebPConversion(t, f, ticket, converted)
}
