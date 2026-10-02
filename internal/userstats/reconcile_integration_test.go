package userstats

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestReconcileExactlyRepairsDriftAndPreservesRetainedActivityIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify exact statistics reconciliation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := database.DropEphemeralSchema(context.Background(), pool); err != nil {
			t.Errorf("drop ephemeral schema: %v", err)
		}
	}()

	var userID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('statistics_reconcile','statistics_reconcile@example.invalid','test-only',true) returning id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	firstEdit := time.Date(2025, time.February, 3, 1, 0, 0, 0, time.UTC)
	deletedView := firstEdit.Add(time.Hour)
	deletedComment := firstEdit.Add(2 * time.Hour)
	deletedAction := firstEdit.Add(24 * time.Hour)
	events := []struct {
		actionID       int16
		objectTypeID   int16
		addedBytes     int
		deletedBytes   int
		occurredAt     time.Time
		retainOnDelete bool
	}{
		{actionID: 1, objectTypeID: 2, addedBytes: 10, deletedBytes: 1, occurredAt: firstEdit},
		{actionID: 3, objectTypeID: 2, occurredAt: deletedView, retainOnDelete: true},
		{actionID: 2, objectTypeID: 8, addedBytes: 20, deletedBytes: 2, occurredAt: deletedComment, retainOnDelete: true},
		{actionID: 4, objectTypeID: 2, deletedBytes: 5, occurredAt: deletedAction, retainOnDelete: true},
	}
	deletedIDs := make([]int64, 0, 3)
	for _, event := range events {
		var eventID int64
		if err = pool.QueryRow(ctx, `insert into user_activity_events(user_id,action_id,object_type_id,markdown_added_bytes,markdown_deleted_bytes,occurred_at)
			values($1,$2,$3,$4,$5,$6) returning id`, userID, event.actionID, event.objectTypeID, event.addedBytes, event.deletedBytes, event.occurredAt).Scan(&eventID); err != nil {
			t.Fatal(err)
		}
		if event.retainOnDelete {
			deletedIDs = append(deletedIDs, eventID)
		}
	}
	if tag, err := pool.Exec(ctx, `delete from user_activity_events where id=any($1::bigint[])`, deletedIDs); err != nil || tag.RowsAffected() != int64(len(deletedIDs)) {
		t.Fatalf("delete retained source events: affected=%d err=%v", tag.RowsAffected(), err)
	}

	firstDate := firstEdit.Format(time.DateOnly)
	secondDate := deletedAction.Format(time.DateOnly)
	staleDate := firstEdit.Add(48 * time.Hour).Format(time.DateOnly)
	if _, err = pool.Exec(ctx, `update user_statistics_daily set
		action_count=99,view_count=99,edit_count=99,create_count=99,delete_count=99,
		markdown_added_bytes=999,markdown_deleted_bytes=999,action_counts='{"bogus":99,"view":77}'::jsonb,
		first_activity_at=$3::timestamptz-interval '1 day',last_activity_at=$3::timestamptz+interval '1 day'
		where user_id=$1 and stat_date=$2::date`, userID, firstDate, firstEdit); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `delete from user_statistics_daily where user_id=$1 and stat_date=$2::date`, userID, secondDate); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into user_statistics_daily(user_id,stat_date,action_count,view_count,edit_count,create_count,delete_count,
			markdown_added_bytes,markdown_deleted_bytes,action_counts,first_activity_at,last_activity_at)
		values($1,$2::date,50,50,0,0,0,500,500,'{"ghost":50}'::jsonb,$3,$3)`, userID, staleDate, firstEdit); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update user_statistics_totals set action_count=999,view_count=999,edit_count=999,create_count=999,delete_count=999,
			markdown_added_bytes=999,markdown_deleted_bytes=999,action_counts='{"bogus":999}'::jsonb,
			first_activity_at=$2::timestamptz-interval '2 days',last_activity_at=$2::timestamptz+interval '2 days',
			last_edit_at=$2::timestamptz+interval '2 days',last_comment_at=$2::timestamptz+interval '2 days' where user_id=$1`, userID, firstEdit); err != nil {
		t.Fatal(err)
	}

	if err = reconcileUser(ctx, pool, userID); err != nil {
		t.Fatal(err)
	}
	if err = reconcileUser(ctx, pool, userID); err != nil {
		t.Fatalf("repeat reconciliation: %v", err)
	}
	var retainedRows int
	var retainedEvents int64
	if err = pool.QueryRow(ctx, `select count(*)::int,coalesce(sum(event_count),0) from user_statistics_retained_actions where user_id=$1`, userID).
		Scan(&retainedRows, &retainedEvents); err != nil || retainedRows != 3 || retainedEvents != 3 {
		t.Fatalf("retained rows=%d events=%d; want 3/3: %v", retainedRows, retainedEvents, err)
	}
	assertDailyStatistics(t, ctx, pool, userID, firstDate, statisticsExpectation{
		actionCount: 3, viewCount: 1, editCount: 1, createCount: 1,
		addedBytes: 30, deletedBytes: 3, actionCounts: map[string]int64{"edit": 1, "view": 1, "create": 1},
		firstAt: firstEdit, lastAt: deletedComment,
	})
	assertDailyStatistics(t, ctx, pool, userID, secondDate, statisticsExpectation{
		actionCount: 1, deleteCount: 1, deletedBytes: 5, actionCounts: map[string]int64{"delete": 1},
		firstAt: deletedAction, lastAt: deletedAction,
	})
	var dailyRows int
	if err = pool.QueryRow(ctx, `select count(*)::int from user_statistics_daily where user_id=$1`, userID).Scan(&dailyRows); err != nil || dailyRows != 2 {
		t.Fatalf("daily rows=%d want 2: %v", dailyRows, err)
	}

	var total statisticsExpectation
	var rawActionCounts []byte
	var lastEditAt, lastCommentAt *time.Time
	if err = pool.QueryRow(ctx, `select action_count,view_count,edit_count,create_count,delete_count,
		markdown_added_bytes,markdown_deleted_bytes,action_counts,first_activity_at,last_activity_at,last_edit_at,last_comment_at
		from user_statistics_totals where user_id=$1`, userID).Scan(
		&total.actionCount, &total.viewCount, &total.editCount, &total.createCount, &total.deleteCount,
		&total.addedBytes, &total.deletedBytes, &rawActionCounts, &total.firstAt, &total.lastAt, &lastEditAt, &lastCommentAt); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(rawActionCounts, &total.actionCounts); err != nil {
		t.Fatal(err)
	}
	wantTotal := statisticsExpectation{
		actionCount: 4, viewCount: 1, editCount: 1, createCount: 1, deleteCount: 1,
		addedBytes: 30, deletedBytes: 8,
		actionCounts: map[string]int64{"edit": 1, "view": 1, "create": 1, "delete": 1},
		firstAt:      firstEdit, lastAt: deletedAction,
	}
	assertStatisticsExpectation(t, total, wantTotal)
	if lastEditAt == nil || !lastEditAt.Equal(firstEdit) || lastCommentAt == nil || !lastCommentAt.Equal(deletedComment) {
		t.Fatalf("recent timestamps edit=%v comment=%v; want %s and %s", lastEditAt, lastCommentAt, firstEdit, deletedComment)
	}
}

type statisticsExpectation struct {
	actionCount  int64
	viewCount    int64
	editCount    int64
	createCount  int64
	deleteCount  int64
	addedBytes   int64
	deletedBytes int64
	actionCounts map[string]int64
	firstAt      time.Time
	lastAt       time.Time
}

func assertDailyStatistics(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID int64, date string, want statisticsExpectation) {
	t.Helper()
	var got statisticsExpectation
	var rawActionCounts []byte
	if err := pool.QueryRow(ctx, `select action_count,view_count,edit_count,create_count,delete_count,
		markdown_added_bytes,markdown_deleted_bytes,action_counts,first_activity_at,last_activity_at
		from user_statistics_daily where user_id=$1 and stat_date=$2::date`, userID, date).Scan(
		&got.actionCount, &got.viewCount, &got.editCount, &got.createCount, &got.deleteCount,
		&got.addedBytes, &got.deletedBytes, &rawActionCounts, &got.firstAt, &got.lastAt); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rawActionCounts, &got.actionCounts); err != nil {
		t.Fatal(err)
	}
	assertStatisticsExpectation(t, got, want)
}

func assertStatisticsExpectation(t *testing.T, got, want statisticsExpectation) {
	t.Helper()
	gotActions, _ := json.Marshal(got.actionCounts)
	wantActions, _ := json.Marshal(want.actionCounts)
	if got.actionCount != want.actionCount || got.viewCount != want.viewCount || got.editCount != want.editCount ||
		got.createCount != want.createCount || got.deleteCount != want.deleteCount || got.addedBytes != want.addedBytes ||
		got.deletedBytes != want.deletedBytes || string(gotActions) != string(wantActions) ||
		!got.firstAt.Equal(want.firstAt) || !got.lastAt.Equal(want.lastAt) {
		t.Fatalf("statistics=%+v actions=%s; want %+v actions=%s", got, gotActions, want, wantActions)
	}
}
