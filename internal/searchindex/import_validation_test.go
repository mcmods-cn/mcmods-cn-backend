package searchindex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestImportRequiresOneSuccessfulResultPerDocument(t *testing.T) {
	for _, response := range []string{"", "{\"success\":true}\n", "{\"success\":true}\n{\"success\":true}\n{\"success\":true}\n"} {
		t.Run(response, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(response))
			}))
			defer server.Close()
			client := New(config.TypesenseConfig{Enabled: true, URL: server.URL, APIKey: "synthetic-key", Timeout: time.Second})
			if err := client.ImportDocuments(context.Background(), "projects", []map[string]any{{"id": "mod_1"}, {"id": "mod_2"}}); err == nil {
				t.Fatal("partial or misaligned import response was acknowledged as success")
			}
		})
	}
}
