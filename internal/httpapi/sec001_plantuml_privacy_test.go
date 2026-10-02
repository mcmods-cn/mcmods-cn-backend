package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSEC001PlantUMLDefaultsToDisabledSameOriginProxy(t *testing.T) {
	config := defaultMarkdownConfig()
	if config.PlantUML {
		t.Fatal("PlantUML must be disabled by default until a trusted same-origin proxy is deployed")
	}
	if config.PlantUMLServer != "/plantuml" {
		t.Fatalf("PlantUML server = %q, want fixed same-origin proxy path", config.PlantUMLServer)
	}
}

func TestSEC001StoredExternalPlantUMLServerFailsClosed(t *testing.T) {
	config := defaultMarkdownConfig()
	config.PlantUML = true
	config.PlantUMLServer = "https://www.plantuml.com/plantuml"

	config = normalizeMarkdownConfig(config)
	if config.PlantUML {
		t.Fatal("legacy external PlantUML server must disable rendering")
	}
	if config.PlantUMLServer != "/plantuml" {
		t.Fatalf("normalized PlantUML server = %q, want /plantuml", config.PlantUMLServer)
	}
}

func TestSEC001PlantUMLConfigurationAcceptsOnlyFixedProxyPath(t *testing.T) {
	for _, value := range []string{"", "/plantuml", "/plantuml/"} {
		if err := validatePlantUMLProxyPath(value); err != nil {
			t.Errorf("validatePlantUMLProxyPath(%q) error = %v", value, err)
		}
	}
	for _, value := range []string{
		"https://www.plantuml.com/plantuml",
		"http://127.0.0.1:8080/plantuml",
		"//www.plantuml.com/plantuml",
		"/plantuml?target=external",
		"/plantuml#external",
		"/other-service",
	} {
		if err := validatePlantUMLProxyPath(value); err == nil {
			t.Errorf("validatePlantUMLProxyPath(%q) unexpectedly succeeded", value)
		}
	}
}

func TestSEC001MarkdownUpdateRejectsExternalPlantUMLBeforePersistence(t *testing.T) {
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config/markdown", bytes.NewBufferString(`{
		"plantUML":true,
		"plantUMLServer":"https://www.plantuml.com/plantuml"
	}`))
	response := httptest.NewRecorder()

	(&Server{}).updateMarkdownConfig(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}
