package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"mcmods-cn-backend/internal/config"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAutomaticReviewResolutionIntegration(t *testing.T) {
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to run the automatic review integration test")
		}
		databaseURL = config.Load().DB.ConnString()
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	var actorID, entityID int64
	var entityType string
	if err = tx.QueryRow(ctx, `select route.entity_type,route.internal_id,user_account.id
		from users user_account join public_routes route on route.entity_type='user' and route.internal_id=user_account.id
		where user_account.status='active' order by user_account.id limit 1`).Scan(&entityType, &entityID, &actorID); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := json.Marshal(map[string]any{"automaticReviewTest": true})
	created, err := createContentRevisionTx(ctx, tx, createContentRevisionParams{
		EntityType: entityType, EntityID: entityID, AggregateType: "integration_test", AggregateKey: "automatic-review-resolution",
		Snapshot: snapshot, ActorID: actorID, Source: "integration_test", Status: "approved",
	})
	if err != nil {
		t.Fatalf("create revision: %v", err)
	}
	if err = appendReviewResolutionTx(ctx, tx, created.ChangeRequestID, "approved", actorID, "automatic approval", nil); err != nil {
		t.Fatalf("append review resolution: %v", err)
	}
}
