package testenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/config"
)

func ownedFixture(t *testing.T) config.Config {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "mcmods-test-services.synthetic")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	identity, err := json.Marshal(map[string]any{"directory": directory, "uid": os.Getuid(), "database": "mcmods_audit"})
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"identity.json": string(identity), "owned-directory": directory + "\n", "pg-password": "synthetic-password\n"} {
		if err = os.WriteFile(filepath.Join(directory, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("MCMODS_TEST_STATE", directory)
	for _, name := range []string{"MCMODS_RUN_DB_INTEGRATION", "MCMODS_TEST_DATABASE_URL", "MCMODS_RUN_NATS_INTEGRATION", "MCMODS_TEST_NATS_URL", "MCMODS_RUN_TYPESENSE_INTEGRATION", "MCMODS_TEST_TYPESENSE_URL", "MCMODS_TEST_TYPESENSE_KEY"} {
		t.Setenv(name, "")
	}
	return config.Config{Env: "test", DB: config.DBConfig{Host: "127.0.0.1", Port: "55432", Name: "mcmods_audit", User: "mcmods_audit", Password: "synthetic-password", SSLMode: "disable"}}
}

func TestDatabaseOwnershipRejectsUnverifiedTargets(t *testing.T) {
	tests := []struct {
		name   string
		change func(*config.Config)
	}{
		{"production environment", func(c *config.Config) { c.Env = "production" }},
		{"remote host", func(c *config.Config) { c.DB.Host = "db.example.invalid" }},
		{"other port", func(c *config.Config) { c.DB.Port = "5432" }},
		{"other database", func(c *config.Config) { c.DB.Name = "mcmods_real" }},
		{"other user", func(c *config.Config) { c.DB.User = "postgres" }},
		{"other password", func(c *config.Config) { c.DB.Password = "different-private-value" }},
		{"hidden URL override", func(c *config.Config) { c.DB.URL = "postgres://unverified.invalid" }},
		{"reset enabled", func(c *config.Config) { c.DB.ResetOnStart = true }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ownedFixture(t)
			tt.change(&cfg)
			if err := ValidateOwnedDatabaseTarget(cfg, ""); err == nil {
				t.Fatal("unverified target accepted")
			} else if strings.Contains(err.Error(), "different-private-value") {
				t.Fatal("credential leaked in ownership error")
			}
		})
	}
}

func writeOwnedServiceFile(t *testing.T, name, value string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(os.Getenv("MCMODS_TEST_STATE"), name), []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationEnabledIncludesIndependentServiceScopes(t *testing.T) {
	for _, name := range []string{"MCMODS_RUN_DB_INTEGRATION", "MCMODS_TEST_DATABASE_URL", "MCMODS_RUN_NATS_INTEGRATION", "MCMODS_TEST_NATS_URL", "MCMODS_RUN_TYPESENSE_INTEGRATION", "MCMODS_TEST_TYPESENSE_URL", "MCMODS_TEST_TYPESENSE_KEY"} {
		t.Run(name, func(t *testing.T) {
			ownedFixture(t)
			if IntegrationEnabled() {
				t.Fatal("empty environment enabled integrations")
			}
			t.Setenv(name, "1")
			if !IntegrationEnabled() {
				t.Fatal("independent service scope bypassed ownership gate")
			}
		})
	}
}

func TestOwnedNATSTargetRequiresExactLoopbackAndState(t *testing.T) {
	urls := []string{"", "nats://unverified.example.invalid:54222", "nats://127.0.0.1:4222", "nats://localhost:54222", "nats://127.0.0.1:54222,nats://unverified.example.invalid:4222", "nats://user:secret@127.0.0.1:54222", "nats://127.0.0.1:54222/path", "nats://127.0.0.1:54222?servers=remote", "nats://127.0.0.1:54222#remote", "tls://127.0.0.1:54222"}
	for _, target := range urls {
		t.Run(target, func(t *testing.T) {
			cfg := ownedFixture(t)
			t.Setenv("MCMODS_RUN_NATS_INTEGRATION", "1")
			t.Setenv("MCMODS_TEST_NATS_URL", target)
			writeOwnedServiceFile(t, "nats.conf", "host: \"127.0.0.1\"\nport: 54222\n")
			if err := ValidateOwnedDatabaseTarget(cfg, ""); err == nil {
				t.Fatal("unowned NATS target accepted")
			} else if strings.Contains(err.Error(), "secret") {
				t.Fatal("NATS credential appeared in error")
			}
		})
	}
	t.Run("valid test URL with missing state", func(t *testing.T) {
		cfg := ownedFixture(t)
		t.Setenv("MCMODS_TEST_NATS_URL", "nats://127.0.0.1:54222")
		if err := ValidateOwnedDatabaseTarget(cfg, ""); err == nil {
			t.Fatal("database ownership was treated as NATS ownership")
		}
	})
	t.Run("owned NATS scope and zero NATS config", func(t *testing.T) {
		cfg := ownedFixture(t)
		t.Setenv("MCMODS_RUN_NATS_INTEGRATION", "1")
		t.Setenv("MCMODS_TEST_NATS_URL", "nats://127.0.0.1:54222")
		writeOwnedServiceFile(t, "nats.conf", "host: \"127.0.0.1\"\nport: 54222\npid_file: \""+filepath.Join(os.Getenv("MCMODS_TEST_STATE"), "nats.pid")+"\"\njetstream { store_dir: \""+filepath.Join(os.Getenv("MCMODS_TEST_STATE"), "jetstream")+"\" }\n")
		if err := ValidateOwnedDatabaseTarget(cfg, ""); err != nil {
			t.Fatal(err)
		}
	})
}

func TestOwnedTypesenseTargetAndKeyMatchState(t *testing.T) {
	for _, target := range []string{"", "http://unverified.example.invalid:58108", "http://127.0.0.1:8108", "http://localhost:58108", "http://user:secret@127.0.0.1:58108", "http://127.0.0.1:58108/", "http://127.0.0.1:58108?other=1", "http://127.0.0.1:58108#other", "https://127.0.0.1:58108"} {
		t.Run(target, func(t *testing.T) {
			cfg := ownedFixture(t)
			t.Setenv("MCMODS_RUN_TYPESENSE_INTEGRATION", "1")
			t.Setenv("MCMODS_TEST_TYPESENSE_URL", target)
			t.Setenv("MCMODS_TEST_TYPESENSE_KEY", "synthetic-search-key")
			writeOwnedServiceFile(t, "typesense-key", "synthetic-search-key\n")
			if err := ValidateOwnedDatabaseTarget(cfg, ""); err == nil {
				t.Fatal("unowned Typesense target accepted")
			}
		})
	}
	for _, key := range []string{"", "different-search-secret"} {
		t.Run("invalid key "+key, func(t *testing.T) {
			cfg := ownedFixture(t)
			t.Setenv("MCMODS_TEST_TYPESENSE_URL", "http://127.0.0.1:58108")
			t.Setenv("MCMODS_TEST_TYPESENSE_KEY", key)
			writeOwnedServiceFile(t, "typesense-key", "synthetic-search-key\n")
			if err := ValidateOwnedDatabaseTarget(cfg, ""); err == nil {
				t.Fatal("unowned search key accepted")
			} else if strings.Contains(err.Error(), "different-search-secret") {
				t.Fatal("search credential appeared in error")
			}
		})
	}
	t.Run("missing key file", func(t *testing.T) {
		cfg := ownedFixture(t)
		t.Setenv("MCMODS_TEST_TYPESENSE_URL", "http://127.0.0.1:58108")
		t.Setenv("MCMODS_TEST_TYPESENSE_KEY", "synthetic-search-key")
		if err := ValidateOwnedDatabaseTarget(cfg, ""); err == nil {
			t.Fatal("database ownership was treated as Typesense ownership")
		}
	})
	t.Run("owned optional search", func(t *testing.T) {
		cfg := ownedFixture(t)
		t.Setenv("MCMODS_TEST_TYPESENSE_URL", "http://127.0.0.1:58108")
		t.Setenv("MCMODS_TEST_TYPESENSE_KEY", "synthetic-search-key")
		writeOwnedServiceFile(t, "typesense-key", "synthetic-search-key\n")
		if err := ValidateOwnedDatabaseTarget(cfg, ""); err != nil {
			t.Fatal(err)
		}
	})
}

func TestConfiguredServicesCannotEscapeOwnedTarget(t *testing.T) {
	for _, service := range []string{"redis", "nats", "typesense"} {
		t.Run(service, func(t *testing.T) {
			cfg := ownedFixture(t)
			switch service {
			case "redis":
				cfg.Redis = config.RedisConfig{Enabled: true, Addr: "remote.invalid:6379", Password: "private-cache-secret"}
			case "nats":
				cfg.NATS = config.NATSConfig{Enabled: true, URL: "nats://remote.invalid:4222"}
			case "typesense":
				cfg.Typesense = config.TypesenseConfig{Enabled: true, URL: "http://remote.invalid:8108", APIKey: "private-search-secret"}
			}
			if err := ValidateOwnedDatabaseTarget(cfg, ""); err == nil {
				t.Fatal("valid database authorized a different service")
			}
		})
	}
}

func TestConfiguredServicesRequireTheirOwnCredentials(t *testing.T) {
	cfg := ownedFixture(t)
	directory := os.Getenv("MCMODS_TEST_STATE")
	writeOwnedServiceFile(t, "redis.conf", "bind 127.0.0.1\nport 56379\nprotected-mode yes\nrequirepass synthetic-cache-key\n")
	writeOwnedServiceFile(t, "nats.conf", "host: \"127.0.0.1\"\nport: 54222\npid_file: \""+filepath.Join(directory, "nats.pid")+"\"\njetstream { store_dir: \""+filepath.Join(directory, "jetstream")+"\" }\n")
	writeOwnedServiceFile(t, "typesense-key", "synthetic-search-key\n")
	cfg.Redis = config.RedisConfig{Enabled: true, Addr: "127.0.0.1:56379", Password: "synthetic-cache-key"}
	cfg.NATS = config.NATSConfig{Enabled: true, URL: "nats://127.0.0.1:54222"}
	cfg.Typesense = config.TypesenseConfig{Enabled: true, URL: "http://127.0.0.1:58108", APIKey: "synthetic-search-key"}
	if err := ValidateOwnedDatabaseTarget(cfg, ""); err != nil {
		t.Fatal(err)
	}
	for _, service := range []string{"redis", "nats", "typesense"} {
		t.Run(service, func(t *testing.T) {
			changed := cfg
			switch service {
			case "redis":
				changed.Redis.Password = "incorrect-secret"
			case "nats":
				changed.NATS.Token = "incorrect-secret"
			case "typesense":
				changed.Typesense.APIKey = "incorrect-secret"
			}
			if err := ValidateOwnedDatabaseTarget(changed, ""); err == nil {
				t.Fatal("incorrect service credential accepted")
			}
		})
	}
}

func TestDatabaseOwnershipRequiresMatchingOverrideAndMarkers(t *testing.T) {
	cfg := ownedFixture(t)
	valid := "postgres://mcmods_audit:synthetic-password@127.0.0.1:55432/mcmods_audit?sslmode=disable"
	if err := ValidateOwnedDatabaseTarget(cfg, valid); err != nil {
		t.Fatal(err)
	}
	for _, override := range []string{strings.Replace(valid, "55432", "5432", 1), strings.Replace(valid, "/mcmods_audit?", "/unknown?", 1), strings.Replace(valid, "synthetic-password", "unrelated-credential", 1), strings.Replace(valid, "disable", "require", 1)} {
		if err := ValidateOwnedDatabaseTarget(cfg, override); err == nil {
			t.Fatal("unverified legacy test URL accepted")
		}
	}
	directory := os.Getenv("MCMODS_TEST_STATE")
	if err := os.WriteFile(filepath.Join(directory, "owned-directory"), []byte("unrelated"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateOwnedDatabaseTarget(cfg, ""); err == nil {
		t.Fatal("mismatched ownership marker accepted")
	}
	t.Setenv("MCMODS_TEST_STATE", "")
	if err := ValidateOwnedDatabaseTarget(cfg, ""); err == nil {
		t.Fatal("opt-in without owned resources accepted")
	}
}
