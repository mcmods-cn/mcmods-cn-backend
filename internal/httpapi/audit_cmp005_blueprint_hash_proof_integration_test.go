package httpapi

import (
	"net/http"
	"testing"
)

func TestAuditCMP005BlueprintHashReuseRequiresCommittedSourceProofIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	raw, _, err := encodeBlueprint(test036Document(), "nbt")
	if err != nil {
		t.Fatal(err)
	}
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "proof-blueprint.nbt", "application/octet-stream", "blueprint", raw)
	f.put(t, ticket, raw)
	auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201)
	request := map[string]any{"originalName": "proof-blueprint.nbt", "contentType": "application/octet-stream", "source": "blueprint", "category": "test037", "sizeBytes": len(raw), "sha256": ticket.SHA256}
	type reuseResponse struct {
		UploadRequired bool   `json:"uploadRequired"`
		BlueprintID    string `json:"blueprintId"`
		ObjectKey      string `json:"objectKey"`
	}
	reused := decodeTEST022Data[reuseResponse](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/presign", request, 200))
	if reused.UploadRequired || reused.BlueprintID == "" {
		t.Fatal("normally sealed blueprint lost its existing hash reuse")
	}
	// Model an unbound pre-expansion record on this owned test copy. This is
	// legacy compatibility coverage, not a normal-request corruption claim.
	tag, err := f.db.Exec(f.ctx, `delete from oss_upload_byte_bindings where upload_object_key=$1 and status='committed'`, ticket.ObjectKey)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("specific legacy-unbound fixture not established: %v", err)
	}
	var active, marker bool
	if err = f.db.QueryRow(f.ctx, `select status='active',source_hash_verified from oss_files where sha256=$1`, ticket.SHA256).Scan(&active, &marker); err != nil || !active || !marker {
		t.Fatal("legacy fixture lost its original active source-hash marker")
	}
	unbound := decodeTEST022Data[reuseResponse](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/presign", request, 200))
	if !unbound.UploadRequired {
		t.Fatal("unbound legacy hash marker reused a blueprint without byte-binding proof")
	}
	if unbound.ObjectKey == "" || unbound.BlueprintID == "" || unbound.BlueprintID == reused.BlueprintID {
		t.Fatal("legacy-unbound path did not issue a distinct ordinary upload ticket")
	}
	if _, err = f.db.Exec(f.ctx, `select 1`); err != nil {
		t.Fatal(err)
	}
}
