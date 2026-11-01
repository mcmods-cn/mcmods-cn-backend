package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func auditRawURLDeadline(t *testing.T, location string, seconds int64) time.Time {
	t.Helper()
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatal("invalid synthetic presigned URL")
	}
	started, err := time.Parse("20060102T150405Z", parsed.Query().Get("x-oss-date"))
	if err != nil {
		t.Fatal("signed URL has no real v4 signature time")
	}
	duration, err := strconv.ParseInt(parsed.Query().Get("x-oss-expires"), 10, 64)
	if err != nil || duration != seconds {
		t.Fatalf("real signature duration=%d expected%d err=%v", duration, seconds, err)
	}
	return started.Add(time.Duration(duration) * time.Second)
}

func auditRawRequireCapability(t *testing.T, f test037Fixture, ticket test037Ticket, seconds int64, count int64) time.Time {
	t.Helper()
	var expiry time.Time
	var actor, grants int64
	var kind, upload string
	if err := f.db.QueryRow(f.ctx, `select max_expires_at,last_actor_id,grant_kind,upload_id,grant_count from oss_upload_capabilities where bucket=$1 and endpoint=$2 and object_key=$3`, f.cfg.Bucket, f.cfg.Endpoint, ticket.ObjectKey).Scan(&expiry, &actor, &kind, &upload, &grants); err != nil {
		t.Fatal(err)
	}
	var signedMaximum time.Time
	locations := []string{ticket.URL}
	if ticket.Method == "MULTIPART" {
		if kind != "multipart" || upload != ticket.Multipart.UploadID || len(ticket.Multipart.Parts) < 2 {
			t.Fatal("multipart grant lost its actual upload identity/parts")
		}
		locations = nil
		for _, part := range ticket.Multipart.Parts {
			locations = append(locations, part.URL)
		}
	} else if kind != "put" || upload != "" {
		t.Fatal("ordinary PUT grant was mislabeled as multipart")
	}
	for _, location := range locations {
		deadline := auditRawURLDeadline(t, location, seconds)
		if deadline.After(signedMaximum) {
			signedMaximum = deadline
		}
	}
	// SDK Expiration retains subsecond precision; its actual signed URL is
	// second-granular. The database never truncates the authority below it.
	if expiry.Before(signedMaximum) || expiry.Sub(signedMaximum) > time.Second || actor != f.userIDs[f.editor] || grants != count {
		t.Fatalf("durable actual deadline/actor/count mismatch: grants=%d expected%d actorMatches=%v deadlineCoversAllParts=%v", grants, count, actor == f.userIDs[f.editor], !expiry.Before(signedMaximum))
	}
	return expiry
}

func TestAuditGR001InitialUserAdminAndMultipartPersistActualCapabilityBeforeDisclosureIntegration(t *testing.T) {
	for _, scope := range []string{"user", "admin", "multipart"} {
		t.Run(scope, func(t *testing.T) {
			f := newTEST037Fixture(t)
			base := "/api/v1/users/me/oss/uploads/"
			if scope == "admin" {
				base = "/api/v1/admin/oss/uploads/"
				grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "oss.write")
			}
			raw := []byte("synthetic actual capability " + scope)
			if scope == "multipart" {
				raw = bytes.Repeat([]byte{'m'}, int(ossMultipartThreshold))
			}
			ticket := f.ticket(t, f.editor, base+"presign", "capability.zip", "application/zip", "test037-capability", raw)
			expiry := auditRawRequireCapability(t, f, ticket, 600, 1)
			if scope != "admin" {
				var reservation time.Time
				if err := f.db.QueryRow(f.ctx, `select expires_at from oss_user_upload_quota_reservations where user_id=$1 and object_key=$2`, f.userIDs[f.editor], ticket.ObjectKey).Scan(&reservation); err != nil || !reservation.Equal(expiry) {
					t.Fatal("quota reservation did not use the actual last SDK expiry")
				}
			}
			if scope == "multipart" {
				var session time.Time
				if err := f.db.QueryRow(f.ctx, `select expires_at from oss_multipart_sessions where owner_id=$1 and object_key=$2 and upload_id=$3`, f.userIDs[f.editor], ticket.ObjectKey, ticket.Multipart.UploadID).Scan(&session); err != nil || !session.Equal(expiry) {
					t.Fatal("multipart session did not persist every returned part's maximum expiry")
				}
			}
		})
	}
}

func TestAuditGR001CapabilityPersistenceFailureDisclosesNoURLAndRollsBackTicketIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	if _, err := f.db.Exec(f.ctx, `alter table oss_upload_capabilities add constraint reject_owned_capability check(last_actor_id<>`+strconv.FormatInt(f.userIDs[f.editor], 10)+`) not valid`); err != nil {
		t.Fatal(err)
	}
	before := f.facts(t)
	raw := []byte("single synthetic not issued upload")
	digest := sha256.Sum256(raw)
	response := auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/presign", map[string]any{"originalName": "unissued.zip", "contentType": "application/zip", "source": "test037-capability", "sizeBytes": len(raw), "sha256": hex.EncodeToString(digest[:])}, 500)
	if !strings.Contains(string(response), "保存 OSS 上传会话失败") || strings.Contains(string(response), "x-oss-signature") || strings.Contains(string(response), `"url"`) {
		t.Fatal("capability persistence failure exposed signed authority or reached an unrelated boundary")
	}
	f.unchanged(t, before)
	var grants int
	if err := f.db.QueryRow(f.ctx, `select count(*) from oss_upload_capabilities`).Scan(&grants); err != nil || grants != 0 {
		t.Fatal("failed capability persistence left an issued deadline")
	}
}

func TestAuditGR001ResumeExtendsActualCapabilityAndRetiredKeyRejectsWithoutSideEffectsIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	raw := []byte("owned resume without a second raw PUT")
	base := "/api/v1/mods/test013-mod/export-imports/uploads/"
	ticket := f.ticket(t, f.editor, base+"presign", "resume.zip", "application/zip", "mcmods_exporter", raw)
	initial := auditRawRequireCapability(t, f, ticket, 600, 1)
	request := resumeModExportUploadRequest{ObjectKey: ticket.ObjectKey, OriginalName: ticket.OriginalName, ContentType: ticket.ContentType, SizeBytes: ticket.SizeBytes, SHA256: ticket.SHA256}
	resumed := decodeTEST022Data[test037Ticket](t, auditOSSRequire(t, f, f.editor, http.MethodPost, base+"resume", request, 200))
	last := auditRawRequireCapability(t, f, resumed, 3600, 2)
	if !last.After(initial) {
		t.Fatal("normal 60 minute resume failed to extend the real original write authority")
	}
	// The one ordinary initial PUT is followed by actual completion. Export
	// sources use the import terminal service, not the user-file DELETE route.
	// This case proves the resume boundary against that shared transactional
	// tombstone; the actual complete import Worker chain has separate evidence.
	// No returned resumed URL is used to rewrite an object.
	f.put(t, ticket, raw)
	file := decodeTEST022Data[map[string]any](t, auditOSSRequire(t, f, f.editor, http.MethodPost, base+"complete", ticket.completion(), 201))
	var fileID int64
	if err := f.db.QueryRow(f.ctx, `select id from oss_files where public_id=$1`, file["id"]).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if err = f.server.tombstoneOSSFileTx(f.ctx, tx, fileID, "owned-resume-terminal-boundary"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	before := f.facts(t)
	response := auditOSSRequire(t, f, f.editor, http.MethodPost, base+"resume", request, 409)
	if !strings.Contains(string(response), "原始上传已结束") {
		t.Fatal("retired resume failed at an unrelated boundary")
	}
	f.unchanged(t, before)
	var grants int64
	var next time.Time
	if err := f.db.QueryRow(f.ctx, `select grant_count,(select next_attempt_at from oss_object_deletion_outbox where bucket=$1 and endpoint=$2 and object_key=$3 and raw_upload_binding_id is not null) from oss_upload_capabilities where bucket=$1 and endpoint=$2 and object_key=$3`, f.cfg.Bucket, f.cfg.Endpoint, ticket.ObjectKey).Scan(&grants, &next); err != nil || grants != 2 || next.Before(last.Add(time.Second)) {
		t.Fatal("retired resume changed deadline or terminal intent lost the actual longer capability")
	}
	// Same project permission still cannot use another actor's source prefix.
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "project.edit."+f.modCode)
	auditOSSRequire(t, f, f.otherEditor, http.MethodPost, base+"resume", request, 400)
	if err := f.db.QueryRow(f.ctx, `select grant_count from oss_upload_capabilities where object_key=$1`, ticket.ObjectKey).Scan(&grants); err != nil || grants != 2 {
		t.Fatal("foreign resume wrote to the original capability")
	}
}
