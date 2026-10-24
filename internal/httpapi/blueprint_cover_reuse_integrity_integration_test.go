package httpapi

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestOCT02ReusableBlueprintCoverDoesNotReportFailedBindingAsSuccessIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	owner := f.userIDs[f.editor]
	blueprintInternalID, blueprintPublicID, err := f.server.createPendingBlueprint(f.ctx, owner, "cover-test.nbt", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err = png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	// Exercise the real verified-byte producer; an unverified hand-inserted file
	// must not bypass CMP005's deduplication gate or stand in for cover binding.
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "cover.png", "image/png", "blueprint_cover:"+blueprintPublicID, encoded.Bytes())
	f.put(t, ticket, encoded.Bytes())
	auditOSSInstallTransparentCoverConversion(t, f, ticket)
	file := decodeTEST022Data[struct {
		ID string `json:"id"`
	}](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201))
	var fileID int64
	if err = f.db.QueryRow(f.ctx, `select id from oss_files where public_id=$1 and source_hash_verified and scan_status in ('clean','trusted_generated')`, file.ID).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `update blueprints set cover_file_id=null,cover_object_key='' where id=$1`, blueprintInternalID); err != nil {
		t.Fatal(err)
	}
	body := ossDirectUploadRequest{OriginalName: ticket.OriginalName, ContentType: ticket.ContentType, SizeBytes: ticket.SizeBytes, SHA256: ticket.SHA256, Source: ticket.Source}
	if _, err = f.db.Exec(f.ctx, `alter table blueprints add constraint oct02_cover_bind_fault check(cover_file_id is null)`); err != nil {
		t.Fatal(err)
	}
	raw := auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/presign", body, 500)
	if !strings.Contains(string(raw), "保存蓝图封面失败") || strings.Contains(string(raw), "oct02_cover_bind_fault") {
		t.Fatal("failed to reach the precise, nonleaking cover binding boundary")
	}
	var stored *int64
	if err = f.db.QueryRow(f.ctx, `select cover_file_id from blueprints where id=$1`, blueprintInternalID).Scan(&stored); err != nil || stored != nil {
		t.Fatal("failed cover binding changed durable facts")
	}
	if _, err = f.db.Exec(f.ctx, `alter table blueprints drop constraint oct02_cover_bind_fault`); err != nil {
		t.Fatal(err)
	}
	raw = auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/presign", body, 200)
	if err = f.db.QueryRow(f.ctx, `select cover_file_id from blueprints where id=$1`, blueprintInternalID).Scan(&stored); err != nil || stored == nil || *stored != fileID {
		t.Fatal("successful reuse did not persist cover")
	}
	if strings.Contains(string(raw), `"uploadRequired":true`) {
		t.Fatal("reuse needlessly requested new upload")
	}
}
