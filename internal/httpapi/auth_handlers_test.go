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
	for _, test := range []struct {
		name        string
		code        string
		rules       []security.PermissionRule
		wantAllowed bool
		wantValue   int32
	}{
		{name: "explicit-deny", code: "admin.users.read", rules: []security.PermissionRule{{Code: "admin.*", Allow: false, Priority: directUserPermissionPriority}}},
		{name: "explicit-allow", code: "admin.users.read", rules: []security.PermissionRule{{Code: "admin.users.read", Allow: true, Priority: 100}}, wantAllowed: true},
		{name: "administrator-specific-deny", code: "admin.users.read", rules: []security.PermissionRule{
			{Code: "admin.*", Allow: true, Priority: 100},
			{Code: "admin.users.read", Allow: false, Priority: directUserPermissionPriority},
		}},
		{name: "numeric-prefix", code: "user.ai.daily_token_limit", rules: []security.PermissionRule{
			{Code: "user.ai.daily_token_limit.12000", Allow: true, Priority: 100},
		}, wantAllowed: true, wantValue: 12000},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/permission?code="+test.code, nil)
			claims := security.Claims{PermissionRules: test.rules}
			request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, claims))
			response := httptest.NewRecorder()
			new(Server).evaluatePermission(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("evaluatePermission() status = %d, body = %s", response.Code, response.Body.String())
			}
			var payload struct {
				Data *struct {
					Permission string `json:"permission"`
					Allowed    bool   `json:"allowed"`
					Value      int32  `json:"value"`
				} `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Data == nil || payload.Data.Permission != test.code {
				t.Fatalf("evaluatePermission() response is missing its data envelope: %s", response.Body.String())
			}
			if payload.Data.Allowed != test.wantAllowed || payload.Data.Value != test.wantValue {
				t.Fatalf("evaluatePermission() = %#v, want allowed=%v value=%d", payload.Data, test.wantAllowed, test.wantValue)
			}
		})
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
