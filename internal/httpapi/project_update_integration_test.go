package httpapi

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestProjectUpdateEventsMergeWithinDeliveryWindowIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify project update merging against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())

	var routeID int64
	if err = tx.QueryRow(ctx, `insert into public_routes(public_id,entity_type,internal_id,canonical_path)
		values($1,'mod',$2,$3) returning id`, "pit"+randomHex(3), time.Now().UnixNano(), "/mod/project-update-integration").Scan(&routeID); err != nil {
		t.Fatalf("create project route: %v", err)
	}
	if err = enqueueProjectUpdateEventTx(ctx, tx, routeID, 0, 0, "content_updated", []string{"description"}, "integration:first"); err != nil {
		t.Fatalf("enqueue first event: %v", err)
	}
	if err = enqueueProjectUpdateEventTx(ctx, tx, routeID, 0, 0, "download_added", []string{"download_files"}, "integration:second"); err != nil {
		t.Fatalf("merge second event: %v", err)
	}
	if err = enqueueProjectUpdateEventTx(ctx, tx, routeID, 0, 0, "download_added", []string{"download_files"}, "integration:second"); err != nil {
		t.Fatalf("retry second event: %v", err)
	}

	var eventCount, taskCount, outboxCount int
	var sections []string
	var readyInFuture bool
	if err = tx.QueryRow(ctx, `select count(*) from project_update_events where project_route_id=$1`, routeID).Scan(&eventCount); err != nil {
		t.Fatalf("read merged event: %v", err)
	}
	if err = tx.QueryRow(ctx, `select changed_sections from project_update_events where project_route_id=$1`, routeID).Scan(&sections); err != nil {
		t.Fatalf("read merged sections: %v", err)
	}
	if err = tx.QueryRow(ctx, `select count(*),bool_and(next_attempt_at>now()) from project_update_notification_tasks
		where event_id in (select id from project_update_events where project_route_id=$1)`, routeID).Scan(&taskCount, &readyInFuture); err != nil {
		t.Fatalf("read notification task: %v", err)
	}
	if err = tx.QueryRow(ctx, `select count(*) from nats_outbox where aggregate_type='project_update_event'
		and aggregate_id in (select id::text from project_update_events where project_route_id=$1)`, routeID).Scan(&outboxCount); err != nil {
		t.Fatalf("read event outbox: %v", err)
	}
	if eventCount != 1 || taskCount != 1 || outboxCount != 1 || !readyInFuture {
		t.Fatalf("unexpected merge counts: events=%d tasks=%d outbox=%d delayed=%v", eventCount, taskCount, outboxCount, readyInFuture)
	}
	sections = uniqueProjectUpdateSections(sections)
	if len(sections) != 2 || sections[0] != "description" || sections[1] != "download_files" {
		t.Fatalf("merged sections = %v", sections)
	}
}
