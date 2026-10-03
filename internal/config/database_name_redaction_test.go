package config

import (
	"strings"
	"testing"
)

func TestInvalidDatabaseNameDoesNotExposeConnectionCredentials(t *testing.T) {
	_, err := (DBConfig{URL: "postgres://synthetic-user:synthetic-password@127.0.0.1:5432/%GG?password=synthetic-query-secret"}).EffectiveName()
	if err == nil {
		t.Fatal("invalid database URL was accepted")
	}
	for _, secret := range []string{"synthetic-user", "synthetic-password", "synthetic-query-secret"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("invalid connection URL was included in the reset validation diagnostic")
		}
	}
}
