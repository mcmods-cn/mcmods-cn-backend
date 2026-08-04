package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestValidateUsernameSupportsUnicodeAndVisibleSpecialCharacters(t *testing.T) {
	t.Parallel()
	for _, username := range []string{"树苗分类员", "玩家✨-01", "123456789"} {
		if err := validateUsername(username); err != nil {
			t.Errorf("validateUsername(%q) error = %v", username, err)
		}
	}
}

func TestEvaluatePermissionUsesResolvedDenyRules(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/permission?code=admin.users.read", nil)
	claims := security.Claims{
		PermissionRules: []security.PermissionRule{
			{Code: "admin.*", Allow: false, Priority: directUserPermissionPriority},
		},
	}
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, claims))
	response := httptest.NewRecorder()
	new(Server).evaluatePermission(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("evaluatePermission() status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Allowed bool  `json:"allowed"`
		Value   int32 `json:"value"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Allowed || payload.Value != 0 {
		t.Fatalf("evaluatePermission() = %#v, expected explicit deny", payload)
	}
}

func TestValidateUsernameRejectsUnsafeOrOversizedValues(t *testing.T) {
	t.Parallel()
	for _, username := range []string{"", "two words", "line\nbreak", "admin", strings.Repeat("界", 33)} {
		if err := validateUsername(username); err == nil {
			t.Errorf("validateUsername(%q) accepted invalid username", username)
		}
	}
}
