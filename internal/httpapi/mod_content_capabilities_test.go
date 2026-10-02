package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestModContentCapabilitiesFollowWriteAuthority(t *testing.T) {
	identity := modIdentityRecord{UniqueID: "abc234567"}
	denied := modContentCapabilitiesFor(security.Claims{Subject: 41}, identity)
	if denied.ManageLayout || denied.CreateResource || denied.EditResource {
		t.Fatalf("ordinary authenticated user received capabilities: %#v", denied)
	}
	allowed := modContentCapabilitiesFor(security.Claims{Subject: 42, PermissionRules: []security.PermissionRule{{
		Code: "project.edit.abc234567", Allow: true,
	}}}, identity)
	if !allowed.ManageLayout || !allowed.CreateResource || !allowed.EditResource {
		t.Fatalf("scoped editor did not receive capabilities: %#v", allowed)
	}
}

func TestModContentResponsesExposeTheSharedCapabilitySet(t *testing.T) {
	raw, err := os.ReadFile("mod_content_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	resourceRaw, err := os.ReadFile("mod_content_resource_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(raw)+string(resourceRaw), `"capabilities": modContentCapabilitiesFor(currentClaims(r), identity)`); got != 2 {
		t.Fatalf("shared capability response wiring count=%d, want 2", got)
	}
}

func TestModContentCapabilityResponsesCannotBeSharedAcrossUsers(t *testing.T) {
	response := httptest.NewRecorder()
	markModContentCapabilityResponse(response)
	if got := response.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("cache control=%q", got)
	}
	vary := strings.Join(response.Header().Values("Vary"), ",")
	if !strings.Contains(vary, "Authorization") || !strings.Contains(vary, "Cookie") {
		t.Fatalf("vary=%q", vary)
	}
}

func TestUnauthorizedModContentEntryAndWriteStayClosedIntegration(t *testing.T) {
	db := openGlobalCatalogTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `create temp table mods(id bigint primary key,project_code text not null,slug text not null,submitted_by bigint);
		insert into mods(id,project_code,slug,submitted_by) values(71,'abc234567','capability-test',42)`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: db}
	identity := modIdentityRecord{ID: 71, UniqueID: "abc234567", SiteID: "capability-test"}
	if capabilities := modContentCapabilitiesFor(security.Claims{Subject: 99}, identity); capabilities.EditResource {
		t.Fatal("unauthorized detail capability exposed an edit entrance")
	}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/mods/capability-test/content-resources/res234567", strings.NewReader("{}"))
	request.SetPathValue("siteId", "capability-test")
	request.SetPathValue("resourceId", "res234567")
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{Subject: 99}))
	response := httptest.NewRecorder()
	server.modContentResource(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("direct unauthorized resource edit status=%d body=%s", response.Code, response.Body.String())
	}
}
