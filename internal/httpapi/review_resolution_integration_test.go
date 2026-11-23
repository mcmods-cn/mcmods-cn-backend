package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestAutomaticReviewResolutionIntegration(t *testing.T) {
	if os.Getenv("MCMODS_TEST_DATABASE_URL") == "" && os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to run the automatic review integration test")
	}
	ctx := context.Background()
	db, actorID := newPostgresUserFixture(t, ctx, "automatic-review-resolution")
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	var entityID int64
	var entityType string
	if err = tx.QueryRow(ctx, `select entity_type,internal_id from public_routes
		where entity_type='user' and internal_id=$1`, actorID).Scan(&entityType, &entityID); err != nil {
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
