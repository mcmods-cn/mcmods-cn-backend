package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOCT02RegistrationPreservesPrimaryLanguageAndValidatesSupportedSecondary(t *testing.T) {
	for _, test := range []struct{ name, primary, secondary, message string }{
		{"arbitrary primary remains supported", "pt-BR", "en-US", "邮箱验证码格式不正确"},
		{"supported aliases normalize", "zh-cn", "ja-jp", "邮箱验证码格式不正确"},
		{"unsupported secondary rejected", "en-US", "pt-BR", "content language preference is invalid"},
		{"invalid primary rejected", "12-invalid", "en-US", "content language preference is invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := `{"username":"SyntheticUser","email":"synthetic@example.invalid","password":"synthetic-password","code":"bad","preferredContentLanguage":"` + test.primary + `","secondaryContentLanguage":"` + test.secondary + `"}`
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBufferString(body))
			response := httptest.NewRecorder()
			(&Server{}).register(response, request)
			if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), test.message) {
				t.Fatalf("validation response status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
