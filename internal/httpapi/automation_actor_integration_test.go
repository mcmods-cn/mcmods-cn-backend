package httpapi

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/systemactor"
)

func TestAutomationActorUsesSeededRBACWithoutManufacturedClaims(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the seeded automation identity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	claims, err := (&Server{db: pool}).automationActor(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Username != systemactor.AutobotUsername || claims.Email != systemactor.AutobotEmail || claims.Subject <= 0 {
		t.Fatalf("unexpected automation actor: %+v", claims)
	}
	for _, required := range []string{"project.create", "project.edit", "project.no-review", "content.no-review"} {
		if !claimsAllow(claims, required) {
			t.Fatalf("resolved automation actor is missing %q", required)
		}
	}
	if percent := antiAbuseRateLimitPercent(claims, "review.submit"); percent != 1000 {
		t.Fatalf("review.submit rate allowance = %d%%, want 1000%%", percent)
	}
}
