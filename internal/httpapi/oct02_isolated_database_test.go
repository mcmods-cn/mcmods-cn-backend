package httpapi

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// These audit regressions create a complete disposable database. Both switches
// are required before the older fixture helper may load application settings;
// it must never fall back to a developer or production connection by accident.
func newOCT02IsolatedDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	requireOCT02DatabaseIntegration(t)
	return newTEST018IsolatedDatabase(t, ctx)
}

func requireOCT02DatabaseIntegration(t *testing.T) {
	t.Helper()
	target := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	applicationTarget := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" || target == "" || applicationTarget == "" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 and matching explicit owned test/database URLs")
	}
	testConfig, testErr := pgxpool.ParseConfig(target)
	applicationConfig, applicationErr := pgxpool.ParseConfig(applicationTarget)
	if testErr != nil || applicationErr != nil {
		t.Fatal("invalid explicit audit database configuration")
	}
	a, b := testConfig.ConnConfig, applicationConfig.ConnConfig
	if a.Host != b.Host || a.Port != b.Port || a.Database != b.Database || a.User != b.User {
		t.Fatal("audit test database and application database targets must match")
	}
}
