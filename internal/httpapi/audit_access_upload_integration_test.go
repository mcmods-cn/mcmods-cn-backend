package httpapi

import (
	"net/http"
	"testing"
)

func TestAuditCMP005ActualSourceHashBeforeRegistrationFullHTTPIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	raw := []byte("synthetic upload original bytes")
	wrong := append([]byte(nil), raw...)
	wrong[0] = 'X'
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "audit-source.zip", "application/octet-stream", "audit-hash", raw)
	f.put(t, ticket, wrong) // matching declared metadata and byte length, different actual bytes
	f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 422)
	var registered int
	if err := f.db.QueryRow(f.ctx, `select count(*) from oss_files where object_key=$1 and status='active'`, ticket.ObjectKey).Scan(&registered); err != nil || registered != 0 {
		t.Fatalf("invalid source registered=%d err=%v", registered, err)
	}
	correct := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "audit-correct.zip", "application/octet-stream", "audit-hash", raw)
	if !correct.UploadRequired {
		t.Fatal("unverified bytes entered deduplication")
	}
	f.put(t, correct, raw)
	file := decodeTEST022Data[struct {
		ID string `json:"id"`
	}](t, f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", correct.completion(), 201))
	f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", correct.completion(), 200)
	var verified bool
	if err := f.db.QueryRow(f.ctx, `select source_hash_verified from oss_files where public_id=$1`, file.ID).Scan(&verified); err != nil || !verified {
		t.Fatalf("source verified=%v err=%v", verified, err)
	}
	reusable := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "audit-reuse.zip", "application/octet-stream", "audit-hash", raw)
	if reusable.UploadRequired {
		t.Fatal("verified same-owner source was not reusable")
	}
	foreign := f.ticket(t, f.otherEditor, "/api/v1/users/me/oss/uploads/presign", "audit-foreign.zip", "application/octet-stream", "audit-hash", raw)
	if !foreign.UploadRequired {
		t.Fatal("source hash reuse crossed ownership")
	}
	if _, err := f.db.Exec(f.ctx, `update oss_files set source_hash_verified=false where public_id=$1`, file.ID); err != nil {
		t.Fatal(err)
	}
	legacy := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "audit-legacy.zip", "application/octet-stream", "audit-hash", raw)
	if !legacy.UploadRequired {
		t.Fatal("legacy hash assertion was trusted without byte evidence")
	}
	// Repeating an ordinary completion may verify a legacy object using its still
	// available immutable source; converted legacy objects need separate diagnosis.
	f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", correct.completion(), 201)
	if err := f.db.QueryRow(f.ctx, `select source_hash_verified from oss_files where public_id=$1`, file.ID).Scan(&verified); err != nil || !verified {
		t.Fatalf("legacy reverify=%v err=%v", verified, err)
	}
}
