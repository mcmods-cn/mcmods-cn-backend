package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBUG030ServerMutationPayloadRejectsTrustedEvidenceFields(t *testing.T) {
	raw := `{
		"name":"Evidence boundary",
		"minecraftVersions":["1.21.1"],
		"languages":["zh-CN"],
		"primaryTag":"survival",
		"mods":[{"id":"example_mod","version":"1.0","source":"agent","confidence":"exact"}]
	}`
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/servers/abc123def", bytes.NewBufferString(raw))
	request.Header.Set("Content-Type", "application/json")
	var payload updateMinecraftServerRequest
	if err := decodeJSON(request, &payload); err == nil {
		t.Fatal("client mutation payload accepted server-owned source/confidence evidence")
	}
}

func TestBUG030UpdateRejectsForgedEvidenceBeforeDatabaseAccess(t *testing.T) {
	raw := `{
		"name":"Evidence boundary",
		"minecraftVersions":["1.21.1"],
		"languages":["zh-CN"],
		"primaryTag":"survival",
		"mods":[{"id":"example_mod","source":"forge_status","confidence":"exact"}]
	}`
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/servers/abc123def", bytes.NewBufferString(raw))
	request.SetPathValue("serverId", "abc123def")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("forged evidence reached database-backed update: %v", recovered)
		}
	}()
	(&Server{}).updateMinecraftServer(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("update status=%d want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}

func TestBUG030ClientModDTOContainsOnlyIdentifierAndVersion(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "server_catalog_handlers.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "type createServerModRequest struct {")
	if start < 0 {
		t.Fatal("createServerModRequest declaration is missing")
	}
	endOffset := strings.Index(source[start:], "\n}")
	if endOffset < 0 {
		t.Fatal("createServerModRequest declaration is incomplete")
	}
	declaration := source[start : start+endOffset]
	if strings.Contains(declaration, "Source") || strings.Contains(declaration, "Confidence") ||
		strings.Contains(declaration, `json:"source"`) || strings.Contains(declaration, `json:"confidence"`) {
		t.Fatalf("client mod DTO still exposes trusted evidence fields:\n%s", declaration)
	}
}
