package httpapi

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestProjectUpdateNotificationCountUsesAtomicDeltasAndEventIndexIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to validate project notification count scale")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if _, err = pool.Exec(ctx, `
		create temporary table project_update_notification_tasks (
			event_id bigint primary key,status text not null,next_user_id bigint not null default 0,
			notified_count bigint not null default 0,next_attempt_at timestamptz not null default now(),
			last_error text not null default '',updated_at timestamptz not null default now()
		);
		create temporary table notifications (
			id bigint primary key,recipient_id bigint not null,project_update_event_id bigint
		);
		create unique index uq_notifications_project_update_recipient
			on notifications(recipient_id,project_update_event_id) where project_update_event_id is not null;
		create index idx_notifications_project_update_event
			on notifications(project_update_event_id) where project_update_event_id is not null;
		insert into project_update_notification_tasks(event_id,status) values(42,'processing');
		insert into notifications
		select value,value,case when value<=1000 then 42 else value+100 end
		from generate_series(1,1000000) value;
		analyze notifications
	`); err != nil {
		t.Fatal(err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = updateProjectUpdateNotificationProgress(ctx, tx, 42, 200, 137, false); err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = updateProjectUpdateNotificationProgress(ctx, tx, 42, 263, 63, true); err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	var status string
	var nextUserID, notifiedCount int64
	if err = pool.QueryRow(ctx, `select status,next_user_id,notified_count
		from project_update_notification_tasks where event_id=42`).Scan(&status, &nextUserID, &notifiedCount); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || nextUserID != 263 || notifiedCount != 200 {
		t.Fatalf("progress status=%s next=%d notified=%d", status, nextUserID, notifiedCount)
	}

	rows, err := pool.Query(ctx, `explain (analyze,buffers,format text)
		select count(*) from notifications where project_update_event_id=42`)
	if err != nil {
		t.Fatal(err)
	}
	planLines := make([]string, 0)
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		planLines = append(planLines, line)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	plan := strings.Join(planLines, "\n")
	if !strings.Contains(plan, "idx_notifications_project_update_event") || strings.Contains(plan, "Seq Scan on notifications") {
		t.Fatalf("event reconciliation did not use the event-first index:\n%s", plan)
	}
	t.Logf("1m project notification reconciliation plan:\n%s", plan)
}
