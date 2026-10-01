package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestYggdrasilSettingsWriteContractExcludesRuntimeFields(t *testing.T) {
	accepted := `{"enabled":false,"publicBaseUrl":"http://127.0.0.1:8080/api/yggdrasil/","textureBaseUrl":"http://127.0.0.1:8080/api/yggdrasil/textures/","serverName":"Synthetic contract","trustedProxyCidrs":[],"tokenTtlHours":360,"maxTokens":10,"joinTtlSeconds":30,"textureMaxBytes":2097152,"privateKeyBase64":"","rotatePrivateKey":false}`
	var request updateYggdrasilSettingsRequest
	if err := decodeJSON(httptest.NewRequest(http.MethodPut, "/api/v1/admin/config/yggdrasil", strings.NewReader(accepted)), &request); err != nil {
		t.Fatal("frontend writable contract was rejected", err)
	}
	for _, field := range []string{"hasPrivateKey", "persistentPrivateKey", "available", "disabledReason"} {
		t.Run(field, func(t *testing.T) {
			value := "true"
			if field == "disabledReason" {
				value = `"synthetic"`
			}
			body := strings.TrimSuffix(accepted, "}") + `,"` + field + `":` + value + `}`
			var invalid updateYggdrasilSettingsRequest
			if err := decodeJSON(httptest.NewRequest(http.MethodPut, "/api/v1/admin/config/yggdrasil", strings.NewReader(body)), &invalid); err == nil {
				t.Fatal("read-only runtime field unexpectedly accepted")
			}
		})
	}
}
