package antiabuse

import (
	"fmt"
	"os"
	"testing"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/testenv"
)

// Fail before any test opens a connection or runs migrations against an
// unverified environment. Individual opt-in flags do not authorize real data.
func TestMain(m *testing.M) {
	if testenv.IntegrationEnabled() {
		if err := testenv.ValidateOwnedDatabaseTarget(config.Load(), os.Getenv("MCMODS_TEST_DATABASE_URL")); err != nil {
			fmt.Fprintln(os.Stderr, "refusing unverified integration database:", err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}
