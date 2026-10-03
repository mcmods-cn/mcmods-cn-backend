package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOCT02TaskRewardMalformedObjectsAreRejectedBeforePersistence(t *testing.T) {
	for _, rewards := range []string{`null`, `{"experience":1,"currencies":[]}`, `{"experience":1,"currencies":"diamond"}`} {
		t.Run(rewards, func(t *testing.T) {
			defer func() {
				if value := recover(); value != nil {
					t.Fatalf("malformed reward caused panic: %v", value)
				}
			}()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tasks", strings.NewReader(`{"code":"invalid-reward","name":"Invalid","refreshPeriod":"never","condition":{"action":"create","objectType":"mod","metric":"count","target":1},"rewards":`+rewards+`}`))
			response := httptest.NewRecorder()
			(&Server{}).createTask(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("invalid reward status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
