package config

import (
	"strings"
	"testing"
	"time"
)

func TestDeploymentURLDelimitersCannotSwallowPathsOrChangeBrowserOrigin(t *testing.T) {
	for _, marker := range []string{"?", "#", "?#"} {
		cfg := validActivityTestConfig()
		cfg.FrontendOrigin = "https://ui.example.test" + marker
		cfg.Typesense = TypesenseConfig{Enabled: true, URL: "https://search.example.test/base" + marker,
			APIKey: "synthetic", CollectionPrefix: "test", Timeout: time.Second}
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "FRONTEND_ORIGIN") || !strings.Contains(err.Error(), "TYPESENSE_URL") {
			t.Errorf("empty URL component %q passed validation for a deployment endpoint", marker)
		}
	}
	cfg := validActivityTestConfig()
	cfg.FrontendOrigin = "https://ui.example.test"
	cfg.Typesense = TypesenseConfig{Enabled: true, URL: "https://search.example.test/base%3F%23",
		APIKey: "synthetic", CollectionPrefix: "test", Timeout: time.Second}
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid browser origin and escaped Typesense path: %v", err)
	}
}
