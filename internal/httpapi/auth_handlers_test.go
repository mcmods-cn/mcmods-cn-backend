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

func TestEvaluatePermissionUsesResolvedRules(t *testing.T) {
	const code = "admin.users.read"
	admin := security.PermissionRule{Code: "admin.*", Allow: true, Priority: 10}
	for _, tc := range []struct {
		name    string
		code    string
		rules   []security.PermissionRule
		allowed bool
		value   int32
	}{
		{"wide denial", code, []security.PermissionRule{{Code: "admin.*", Allow: false, Priority: directUserPermissionPriority}}, false, 0},
		{"read permission", code, []security.PermissionRule{{Code: code, Allow: true, Priority: 10}}, true, 0},
		{"specific denial", code, []security.PermissionRule{admin, {Code: code, Allow: false, Priority: 20}}, false, 0},
		{"global administrator", code, []security.PermissionRule{admin}, true, maxPermissionValue},
		{"limited storage", "user.file.total_limit", []security.PermissionRule{admin, {Code: "user.file.total_limit.*", Allow: false, Priority: 20}, {Code: "user.file.total_limit.100", Allow: true, Priority: 30}}, true, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/permissions/evaluate?code="+tc.code, nil)
			request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{PermissionRules: tc.rules}))
			response := httptest.NewRecorder()
			new(Server).evaluatePermission(response, request)
			var payload struct {
				Data struct {
					Permission string
					Allowed    *bool
					Value      *int32
				}
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			got := payload.Data
			if response.Code != http.StatusOK || got.Permission != tc.code || got.Allowed == nil || *got.Allowed != tc.allowed || got.Value == nil || *got.Value != tc.value {
				t.Fatalf("permission %s: status=%d response=%s, want allowed=%t value=%d", tc.code, response.Code, response.Body.String(), tc.allowed, tc.value)
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
