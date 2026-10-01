package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSaveTaskMissingRewardsReturnsBadRequestCore(t *testing.T) {
	for _, rewards := range []string{"", `,"rewards":null`} {
		t.Run(map[bool]string{true: "null", false: "missing"}[rewards != ""], func(t *testing.T) {
			body := `{"code":"fixture-task","name":"Fixture task","refreshPeriod":"daily","condition":{"action":"edit","objectType":"mod","metric":"count","target":1}` + rewards + `}`
			request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tasks", strings.NewReader(body))
			response := httptest.NewRecorder()
			defer func() {
				if caught := recover(); caught != nil {
					t.Fatalf("missing rewards panicked instead of validation response: %v", caught)
				}
			}()
			(&Server{}).saveTask(response, request, "")
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d, want400: %s", response.Code, response.Body.String())
			}
		})
	}
}
