package searchindex

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestImportRequiresAnAcknowledgementForEveryDocument(t *testing.T) {
	for _, scenario := range []struct {
		name, response string
		wantError      bool
	}{
		{"empty", "", true},
		{"partial", "{\"success\":true}\n", true},
		{"extra", "{\"success\":true}\n{\"success\":true}\n{\"success\":true}\n", true},
		{"invalid", "{\"success\":true}\nnot-json\n", true},
		{"rejected", "{\"success\":true}\n{\"success\":false}\n", true},
		{"complete", "{\"success\":true}\n{\"success\":true}\n", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, scenario.response)
			}))
			defer server.Close()
			client := New(config.TypesenseConfig{Enabled: true, URL: server.URL, Timeout: time.Second})
			err := client.ImportDocuments(context.Background(), "projects", []map[string]any{{"id": "one"}, {"id": "two"}})
			if (err != nil) != scenario.wantError {
				t.Fatalf("error=%v; want failure=%v", err, scenario.wantError)
			}
		})
	}
}
