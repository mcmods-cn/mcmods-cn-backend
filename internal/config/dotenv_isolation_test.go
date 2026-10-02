package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExplicitTestEnvironmentCanDisableAncestorDotenvLoading(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DB_PASSWORD=synthetic-private-fixture\nGITHUB_TOKEN=synthetic-provider-fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(child)
	t.Setenv("APP_ENV", "test")
	t.Setenv("DB_PASSWORD", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("MCMODS_SKIP_DOTENV", "true")
	if cfg := Load(); cfg.DB.Password != "" || os.Getenv("GITHUB_TOKEN") != "" {
		t.Fatal("isolated config imported a private ancestor dotenv value")
	}
	// Existing local-development loading remains compatible when opted in.
	t.Setenv("MCMODS_SKIP_DOTENV", "false")
	if cfg := Load(); cfg.DB.Password == "" || os.Getenv("GITHUB_TOKEN") == "" {
		t.Fatal("ordinary dotenv loading no longer works")
	}
}
