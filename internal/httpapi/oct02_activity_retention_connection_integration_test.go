package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
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

func TestActivityRetentionRespectsRemovedViewOverrideIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "log.read", "log.write")
	const path = "/api/v1/admin/activity-logs/retention"
	cfg := defaultActivityRetentionConfig()
	cfg.Enabled = true
	delete(cfg.Actions, "view")
	f.require(t, f.editor, http.MethodPut, path, cfg, http.StatusOK)
	stored := decodeTEST022Data[struct{ Config activityRetentionConfig }](t, f.require(t, f.editor, http.MethodGet, path, nil, http.StatusOK))
	if _, restored := stored.Config.Actions["view"]; restored {
		t.Error("reading the saved policy restored the removed view override")
	}
	var eventID int64
	if err := f.db.QueryRow(f.ctx, `insert into user_activity_events(user_id,action_id,object_type_id,occurred_at)
		values($1,$2,7,now()-interval '45 days') returning id`, f.userIDs[f.editor], activityActionIDs["view"]).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	NewActivityRetentionWorker(f.db).prune(f.ctx)
	var present bool
	if err := f.db.QueryRow(f.ctx, `select exists(select 1 from user_activity_events where id=$1)`, eventID).Scan(&present); err != nil {
		t.Fatal(err)
	}
	if !present {
		t.Error("automatic retention deleted a view protected by the saved default")
	}
	f.require(t, f.editor, http.MethodPost, "/api/v1/admin/activity-logs/cleanup/preview",
		activityCleanupFilterRequest{Actions: []string{"view"}, To: time.Now().UTC().Format(time.RFC3339)}, http.StatusForbidden)
}
