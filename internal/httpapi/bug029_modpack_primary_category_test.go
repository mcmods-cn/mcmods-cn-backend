package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestBUG029ModpackPrimaryCategoryUsesAuthoritativeWhitelist(t *testing.T) {
	base := bug029ValidModpackRequest()
	for category := range allowedModpackCategories {
		request := base
		request.PrimaryCategory = category
		if err := normalizeAndValidateModpackRequest(&request); err != nil {
			t.Errorf("registered primary category %q was rejected: %v", category, err)
		}
	}

	defaulted := base
	defaulted.PrimaryCategory = ""
	if err := normalizeAndValidateModpackRequest(&defaulted); err != nil || defaulted.PrimaryCategory != "adventure" {
		t.Fatalf("empty primary category defaulted to %q with error %v", defaulted.PrimaryCategory, err)
	}

	invalid := base
	invalid.PrimaryCategory = "orphan_category"
	if err := normalizeAndValidateModpackRequest(&invalid); err == nil {
		t.Fatal("unregistered modpack primary category was accepted")
	}
}

func TestBUG029CreateModpackRejectsInvalidPrimaryCategoryBeforeDatabaseAccess(t *testing.T) {
	payload := bug029ValidModpackRequest()
	payload.PrimaryCategory = "orphan_category"
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/modpacks", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{
		Subject:         29,
		PermissionRules: []security.PermissionRule{{Code: "modpack.create", Allow: true, Priority: 100}},
	}))
	response := httptest.NewRecorder()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("invalid primary category reached database-backed creation: %v", recovered)
		}
	}()
	(&Server{}).createModpack(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("create status=%d want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}

func bug029ValidModpackRequest() createModpackRequest {
	return createModpackRequest{
		SiteID:           "bug029-pack",
		PrimaryName:      "BUG-029 pack",
		DefaultLocale:    "en-US",
		Environment:      "bothRequired",
		PrimaryCategory:  "adventure",
		OfficialStatus:   "active",
		SourceStatus:     "open",
		License:          "MIT",
		SubmissionMethod: "manual",
		Compatibilities: []modLoaderCompatibilityPayload{{
			Loader: "Fabric", Versions: []string{"1.21.1"},
		}},
	}
}
