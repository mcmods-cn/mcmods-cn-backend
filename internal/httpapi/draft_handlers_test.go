package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/security"
)

func TestCompleteUserDraftRequestDecodesAndValidates(t *testing.T) {
	var request completeUserDraftRequest
	if err := json.Unmarshal([]byte(`{
		"draftKey":"mod:new","projectKey":"mod:mekanism","projectTitle":"Mekanism","kind":"mod",
		"title":"Mekanism","editUrl":"/mods/new","targetUrl":"/mods/mekanism",
		"changeRequestId":"abc123xyz","payload":{"primaryName":"Mekanism"}
	}`), &request); err != nil {
		t.Fatalf("decode complete draft request: %v", err)
	}
	normalizeCompleteUserDraftRequest(&request)
	if request.DraftKey != "mod:new" || request.ProjectKey != "mod:mekanism" {
		t.Fatalf("unexpected normalized request: %#v", request)
	}
	if !validDraftRequest(request.upsertUserDraftRequest) || !validCompleteUserDraftRequest(request) {
		t.Fatal("expected complete draft request to be valid")
	}
}

func TestCompleteUserDraftRequiresExactlyOneAuthoritativeReviewTarget(t *testing.T) {
	request := completeUserDraftRequest{
		upsertUserDraftRequest: upsertUserDraftRequest{
			DraftKey: "mod:new", ProjectKey: "mod:mekanism", Kind: "mod", Title: "Mekanism",
			EditURL: "/mods/new", Payload: json.RawMessage(`{"name":"Mekanism"}`),
		},
		ProjectTitle: "Mekanism", TargetURL: "/mods/mekanism",
	}
	if validCompleteUserDraftRequest(request) {
		t.Fatal("authority-free completion was accepted")
	}
	request.ChangeRequestID = "abc123xyz"
	if !validCompleteUserDraftRequest(request) {
		t.Fatal("owned change request should be a valid completion authority")
	}
	request.ReviewTargetType = "server"
	request.ReviewTargetPublicID = "server123"
	if validCompleteUserDraftRequest(request) {
		t.Fatal("change request and server authorities were accepted together")
	}
	request.ChangeRequestID = ""
	if !validCompleteUserDraftRequest(request) {
		t.Fatal("server target should be a valid completion authority")
	}
}

func TestUserDraftListRequestUsesScopedStableCursor(t *testing.T) {
	statusAt := time.Date(2026, 8, 21, 3, 4, 5, 6000, time.UTC)
	cursor := encodeUserDraftCursor(userDraftCursor{Category: userDraftCategoryCompleted, StatusAt: statusAt, ID: 42})
	request, err := parseUserDraftListRequest(url.Values{
		"category": {userDraftCategoryCompleted},
		"limit":    {"17"},
		"cursor":   {cursor},
	})
	if err != nil {
		t.Fatalf("parse valid list request: %v", err)
	}
	if request.Category != userDraftCategoryCompleted || request.Limit != 17 || request.Cursor == nil ||
		request.Cursor.ID != 42 || !request.Cursor.StatusAt.Equal(statusAt) {
		t.Fatalf("unexpected list request: %#v", request)
	}

	if _, err = parseUserDraftListRequest(url.Values{"category": {"all"}}); err == nil {
		t.Fatal("unbounded all-category list was accepted")
	}
	if _, err = parseUserDraftListRequest(url.Values{"category": {userDraftCategoryActive}, "limit": {"51"}}); err == nil {
		t.Fatal("page above the server ceiling was accepted")
	}
	foreignCursor := encodeUserDraftCursor(userDraftCursor{Category: userDraftCategoryCompleted, StatusAt: statusAt, ID: 1})
	if _, err = parseUserDraftListRequest(url.Values{"category": {userDraftCategoryActive}, "cursor": {foreignCursor}}); err == nil {
		t.Fatal("cursor was reusable across list categories")
	}
	malformed := base64.RawURLEncoding.EncodeToString([]byte(`{"category":"active"}`))
	if _, err = parseUserDraftListRequest(url.Values{"category": {userDraftCategoryActive}, "cursor": {malformed}}); err == nil {
		t.Fatal("incomplete cursor was accepted")
	}
}

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
	valid := upsertUserDraftRequest{DraftKey: "community:tutorial:new", ProjectKey: "community:tutorial:new", Kind: "community_post", Title: "Tutorial", EditURL: "/tutorials/new", Payload: json.RawMessage(`{"title":"Tutorial"}`)}
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

func TestDraftPayloadAndPerUserQuotaBudgets(t *testing.T) {
	valid := upsertUserDraftRequest{
		DraftKey: "mod:new", ProjectKey: "mod:new", Kind: "mod", Title: "New mod", EditURL: "/mods/new",
		Payload: json.RawMessage(`{"name":"New mod"}`),
	}
	if !validDraftRequest(valid) {
		t.Fatal("control draft must be valid")
	}
	valid.Payload = json.RawMessage(`{"body":"` + strings.Repeat("x", maximumDraftPayloadBytes) + `"}`)
	if validDraftRequest(valid) {
		t.Fatal("oversized draft payload was accepted")
	}

	policy := userDraftQuotaPolicy{MaximumActive: 2, MaximumCompleted: 2, MaximumBytes: 100}
	if err := evaluateUserDraftQuota(userDraftQuotaUsage{Active: 2}, 10, false, policy); !errors.Is(err, errUserDraftCountQuota) {
		t.Fatalf("new active draft quota error = %v", err)
	}
	if err := evaluateUserDraftQuota(userDraftQuotaUsage{Active: 2, Bytes: 80, ExistingBytes: 30, HasExisting: true}, 40, false, policy); err != nil {
		t.Fatalf("replacement should reserve only its byte delta: %v", err)
	}
	if err := evaluateUserDraftQuota(userDraftQuotaUsage{Completed: 2}, 10, true, policy); !errors.Is(err, errUserDraftCountQuota) {
		t.Fatalf("completed history quota error = %v", err)
	}
	if err := evaluateUserDraftQuota(userDraftQuotaUsage{Bytes: 95}, 10, false, policy); !errors.Is(err, errUserDraftStorageQuota) {
		t.Fatalf("total byte quota error = %v", err)
	}
}

func TestDraftWritePathHasRateAndAtomicStockBudgets(t *testing.T) {
	source, err := os.ReadFile("draft_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, required := range []string{
		"ConsumeRateLimitPolicy", "userDraftSharedWritesPerWindow", "userDraftLocalWritesPerWindow",
		"pg_advisory_xact_lock", "maximumActiveUserDrafts", "maximumCompletedUserDrafts", "maximumUserDraftStorageBytes",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("draft write path is missing %q", required)
		}
	}
}
