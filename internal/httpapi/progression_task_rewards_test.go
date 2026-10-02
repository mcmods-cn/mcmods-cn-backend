package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTaskPayloadRejectsDuplicateCurrencyRewardKeys(t *testing.T) {
	for _, body := range []string{
		`{"rewards":{"currencies":{"diamond":1,"diamond":2}}}`,
		`{"rewards":{"currencies":{"Gold":1," gold ":2}}}`,
	} {
		var payload taskPayload
		if err := json.Unmarshal([]byte(body), &payload); !errors.Is(err, errTaskCurrencyRewardDuplicate) {
			t.Fatalf("expected duplicate currency error for %s, got %v", body, err)
		}
	}
}

func TestSaveTaskRejectsDuplicateCurrencyRequestBeforeDatabaseAccess(t *testing.T) {
	body := `{"code":"visit","name":"Visit","description":"","icon":"","translations":{},"refreshPeriod":"daily","condition":{"action":"view","objectType":"mod","metric":"count","target":1},"rewards":{"experience":0,"currencies":{"diamond":1,"diamond":2}},"status":"active"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/tasks", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	(&Server{}).saveTask(recorder, request, "")
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "task currency reward codes must be unique") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestNormalizeTaskCurrencyRewardsRejectsProgrammaticCollision(t *testing.T) {
	_, err := normalizeTaskCurrencyRewards(map[string]any{"Gold": 1, " gold ": 2})
	if !errors.Is(err, errTaskCurrencyRewardDuplicate) {
		t.Fatalf("expected duplicate currency error, got %v", err)
	}
}
