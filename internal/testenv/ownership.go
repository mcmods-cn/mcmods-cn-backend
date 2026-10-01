// Package testenv verifies disposable test resources before integration tests
// or setup commands can connect to services. An opt-in flag alone is not proof.
package testenv

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
)

// ValidateOwnedDatabaseTarget accepts only the cluster created by
// scripts/test-services.sh, including independently enabled NATS/search tests
// and service configurations used by database integration tests. Errors
// deliberately omit connection strings and credentials.
func ValidateOwnedDatabaseTarget(cfg config.Config, override string) error {
	directory := os.Getenv("MCMODS_TEST_STATE")
	if !filepath.IsAbs(directory) || !strings.HasPrefix(filepath.Base(directory), "mcmods-test-services.") {
		return fmt.Errorf("MCMODS_TEST_STATE must identify an owned test-services directory")
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil || resolved != directory {
		return fmt.Errorf("test-services directory must exist without symlinks")
	}
	var identity struct {
		Directory string `json:"directory"`
		UID       int    `json:"uid"`
		Database  string `json:"database"`
	}
	data, err := os.ReadFile(filepath.Join(directory, "identity.json"))
	if err != nil || json.Unmarshal(data, &identity) != nil || identity.Directory != directory || identity.UID != os.Getuid() {
		return fmt.Errorf("test-services ownership identity does not match")
	}
	marker, err := os.ReadFile(filepath.Join(directory, "owned-directory"))
	if err != nil || strings.TrimSpace(string(marker)) != directory {
		return fmt.Errorf("test-services ownership marker does not match")
	}
	if cfg.Env != "test" || cfg.DB.URL != "" || cfg.DB.Host != "127.0.0.1" || cfg.DB.Port != "55432" || cfg.DB.Name != identity.Database || cfg.DB.Name != "mcmods_audit" || cfg.DB.User != "mcmods_audit" || cfg.DB.ResetOnStart {
		return fmt.Errorf("refusing database target outside the owned isolated test cluster")
	}
	password, err := os.ReadFile(filepath.Join(directory, "pg-password"))
	if err != nil || cfg.DB.Password == "" || cfg.DB.Password != strings.TrimSpace(string(password)) {
		return fmt.Errorf("database credential does not match the owned test cluster")
	}
	if override != "" {
		parsed, err := pgxpool.ParseConfig(override)
		if err != nil {
			return fmt.Errorf("invalid isolated test database override")
		}
		connection := parsed.ConnConfig
		if connection.Host != cfg.DB.Host || connection.Port != 55432 || connection.Database != cfg.DB.Name || connection.User != cfg.DB.User || connection.Password != cfg.DB.Password || connection.TLSConfig != nil || len(connection.Fallbacks) != 0 {
			return fmt.Errorf("test database override does not match the owned isolated cluster")
		}
	}
	return validateOwnedServices(directory, cfg)
}

func exactServiceURL(value, scheme, host string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == scheme && parsed.Host == host && parsed.User == nil && parsed.Path == "" && parsed.RawQuery == "" && !parsed.ForceQuery && parsed.Fragment == "" && parsed.Opaque == "" && value == scheme+"://"+host
}

func validateOwnedServices(directory string, cfg config.Config) error {
	// A valid PostgreSQL identity is not authorization to connect other services.
	if cfg.Redis.Enabled {
		data, err := os.ReadFile(filepath.Join(directory, "redis.conf"))
		password := ""
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "requirepass ") {
				password = strings.TrimPrefix(line, "requirepass ")
			}
		}
		if err != nil || cfg.Redis.Addr != "127.0.0.1:56379" || cfg.Redis.Username != "" || cfg.Redis.DB != 0 || password == "" || cfg.Redis.Password != password || !strings.HasPrefix(string(data), "bind 127.0.0.1\nport 56379\nprotected-mode yes\n") {
			return fmt.Errorf("Redis target or credential does not match the owned test service")
		}
	}
	natsTestURL := os.Getenv("MCMODS_TEST_NATS_URL")
	if cfg.NATS.Enabled || os.Getenv("MCMODS_RUN_NATS_INTEGRATION") == "1" || natsTestURL != "" {
		if (cfg.NATS.Enabled && (!exactServiceURL(cfg.NATS.URL, "nats", "127.0.0.1:54222") || cfg.NATS.Username != "" || cfg.NATS.Password != "" || cfg.NATS.Token != "")) || ((os.Getenv("MCMODS_RUN_NATS_INTEGRATION") == "1" || natsTestURL != "") && !exactServiceURL(natsTestURL, "nats", "127.0.0.1:54222")) {
			return fmt.Errorf("NATS target does not match the owned test service")
		}
		data, err := os.ReadFile(filepath.Join(directory, "nats.conf"))
		expected := "host: \"127.0.0.1\"\nport: 54222\npid_file: \"" + filepath.Join(directory, "nats.pid") + "\"\njetstream { store_dir: \"" + filepath.Join(directory, "jetstream") + "\" }\n"
		if err != nil || string(data) != expected {
			return fmt.Errorf("NATS configuration does not match the owned test directory")
		}
	}
	searchURL, searchKey := os.Getenv("MCMODS_TEST_TYPESENSE_URL"), os.Getenv("MCMODS_TEST_TYPESENSE_KEY")
	searchScope := os.Getenv("MCMODS_RUN_TYPESENSE_INTEGRATION") == "1" || searchURL != "" || searchKey != ""
	if cfg.Typesense.Enabled || searchScope {
		data, err := os.ReadFile(filepath.Join(directory, "typesense-key"))
		key := strings.TrimSpace(string(data))
		if err != nil || key == "" || (cfg.Typesense.Enabled && (!exactServiceURL(cfg.Typesense.URL, "http", "127.0.0.1:58108") || cfg.Typesense.APIKey != key)) || (searchScope && (!exactServiceURL(searchURL, "http", "127.0.0.1:58108") || searchKey != key)) {
			return fmt.Errorf("Typesense target or credential does not match the owned test service")
		}
	}
	return nil
}

// IntegrationEnabled gates every independent integration scope before tests can
// connect. Test target variables also require ownership even without an opt-in.
func IntegrationEnabled() bool {
	for _, name := range []string{"MCMODS_RUN_DB_INTEGRATION", "MCMODS_RUN_NATS_INTEGRATION", "MCMODS_RUN_TYPESENSE_INTEGRATION"} {
		if os.Getenv(name) == "1" {
			return true
		}
	}
	for _, name := range []string{"MCMODS_TEST_DATABASE_URL", "MCMODS_TEST_NATS_URL", "MCMODS_TEST_TYPESENSE_URL", "MCMODS_TEST_TYPESENSE_KEY"} {
		if os.Getenv(name) != "" {
			return true
		}
	}
	return false
}
