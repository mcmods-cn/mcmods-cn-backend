package httpapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOCT02AutomaticActivityRetentionRunsOnItsSingleLockedConnectionIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	configuration := defaultActivityRetentionConfig()
	configuration.Enabled = true
	configuration.Actions = map[string]activityRetentionPolicy{"view": {Enabled: true, AllowDelete: true, RetentionDays: 1, BatchSize: 100}}
	raw, err := json.Marshal(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into system_settings(key,value) values($1,$2::jsonb) on conflict(key) do update set value=excluded.value`, activityRetentionSettingKey, string(raw)); err != nil {
		t.Fatal(err)
	}
	var eventID int64
	if err := f.db.QueryRow(f.ctx, `insert into user_activity_events(user_id,action_id,object_type_id,occurred_at) values($1,$2,7,now()-interval '3 days') returning id`, f.userIDs[f.editor], activityActionIDs["view"]).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	pc := f.db.Config()
	pc.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(f.ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(f.ctx, 2*time.Second)
	NewActivityRetentionWorker(pool).prune(ctx)
	cancel()
	var events, runs, deleted int
	if err := pool.QueryRow(f.ctx, `select (select count(*) from user_activity_events where id=$1),
	 (select count(*) from activity_cleanup_runs where source='automatic' and status='completed'),
	 (select coalesce(sum(deleted_count),0) from activity_cleanup_runs where source='automatic')`, eventID).Scan(&events, &runs, &deleted); err != nil {
		t.Fatal(err)
	}
	if events != 0 || runs != 1 || deleted != 1 {
		t.Fatalf("automatic cleanup events=%d runs=%d deleted=%d, want0/1/1", events, runs, deleted)
	}
	ctx, cancel = context.WithTimeout(f.ctx, 2*time.Second)
	defer cancel()
	NewActivityRetentionWorker(pool).prune(ctx)
	if err := pool.QueryRow(f.ctx, `select count(*) from activity_cleanup_runs where source='automatic'`).Scan(&runs); err != nil || runs != 1 {
		t.Fatalf("automatic retry bypassed interval runs=%d err=%v", runs, err)
	}
}
