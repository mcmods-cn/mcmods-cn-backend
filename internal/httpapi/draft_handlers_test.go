package httpapi

import (
	"encoding/json"
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestDraftRetentionSecondsUsesNumericPermission(t *testing.T) {
	claims := security.Claims{PermissionRules: []security.PermissionRule{{Code: "user.draft.retention_seconds.3600", Allow: true, Priority: 10}}}
	if got := draftRetentionSeconds(claims); got != 3600 {
		t.Fatalf("draftRetentionSeconds() = %d, want 3600", got)
	}
	claims.PermissionRules = []security.PermissionRule{{Code: "admin.*", Allow: true, Priority: 10}}
	if got := draftRetentionSeconds(claims); got != maximumDraftRetentionSeconds {
		t.Fatalf("admin draft retention = %d, want %d", got, maximumDraftRetentionSeconds)
	}
}

func TestValidDraftRequestRejectsExternalURLAndNonObjectPayload(t *testing.T) {
	valid := upsertUserDraftRequest{DraftKey: "community:tutorial:new", Kind: "community_post", Title: "Tutorial", EditURL: "/tutorials/new", Payload: json.RawMessage(`{"title":"Tutorial"}`)}
	if !validDraftRequest(valid) {
		t.Fatal("expected valid draft request")
	}
	valid.EditURL = "https://example.com/steal"
	if validDraftRequest(valid) {
		t.Fatal("external edit URL must be rejected")
	}
	valid.EditURL = "/tutorials/new"
	valid.Payload = json.RawMessage(`[]`)
	if validDraftRequest(valid) {
		t.Fatal("non-object payload must be rejected")
	}
}
