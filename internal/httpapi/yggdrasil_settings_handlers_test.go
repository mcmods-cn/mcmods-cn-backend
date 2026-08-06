package httpapi

import (
	"encoding/json"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func TestYggdrasilSettingsRequestDecodesEmbeddedFields(t *testing.T) {
	var request updateYggdrasilSettingsRequest
	err := json.Unmarshal([]byte(`{
		"enabled":true,
		"publicBaseUrl":"https://auth.example.com/yggdrasil/",
		"textureBaseUrl":"https://auth.example.com/yggdrasil/textures/",
		"serverName":"Example",
		"tokenTtlHours":24,
		"maxTokens":5,
		"joinTtlSeconds":30,
		"textureMaxBytes":1048576,
		"rotatePrivateKey":true
	}`), &request)
	if err != nil {
		t.Fatal(err)
	}
	if !request.Enabled || request.ServerName != "Example" || request.TokenTTLHours != 24 || !request.RotatePrivateKey {
		t.Fatalf("unexpected decoded request: %#v", request)
	}
}

func TestNormalizeYggdrasilConfig(t *testing.T) {
	cfg := config.YggdrasilConfig{
		Enabled:           true,
		PublicBaseURL:     " https://auth.example.com/yggdrasil ",
		TextureBaseURL:    "https://textures.example.com/content",
		ServerName:        " Example ",
		TrustedProxyCIDRs: []string{"10.0.0.0/8", "10.0.0.0/8", "127.0.0.1"},
		TokenTTL:          24 * time.Hour,
		MaxTokens:         10,
		JoinTTL:           30 * time.Second,
		TextureMaxBytes:   1024 * 1024,
	}
	if err := normalizeYggdrasilConfig(&cfg, "development"); err != nil {
		t.Fatal(err)
	}
	if cfg.PublicBaseURL != "https://auth.example.com/yggdrasil/" || cfg.ServerName != "Example" || len(cfg.TrustedProxyCIDRs) != 2 {
		t.Fatalf("unexpected normalized config: %#v", cfg)
	}
}

func TestGeneratedYggdrasilPrivateKeyRoundTrip(t *testing.T) {
	encoded, err := generateYggdrasilPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	key, err := parseYggdrasilPrivateKey(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if key.N.BitLen() < 2048 {
		t.Fatalf("generated signing key is too small: %d", key.N.BitLen())
	}
}
