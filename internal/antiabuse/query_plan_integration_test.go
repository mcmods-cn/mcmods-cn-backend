package antiabuse

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

const antiAbusePlanFixtureRows = 100_000

func TestAntiAbuseQueryPlans(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify high-cardinality query plans in a temporary schema")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
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
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	var namespace string
	if err = pool.QueryRow(ctx, `select current_schema()`).Scan(&namespace); err != nil {
		t.Fatal(err)
	}
	if namespace == "public" {
		t.Fatal("query-plan fixture must never be installed in the public schema")
	}
	if _, err = pool.Exec(ctx, `
		insert into users(id,username,email,password_hash,email_verified)
		select generated_id,
			'query_plan_user_' || generated_id,
			'query_plan_user_' || generated_id || '@example.invalid',
			'test-only',true
		from generate_series(1,1000) generated_id;
		select setval(pg_get_serial_sequence('users','id'),1000,true);
		alter table anti_abuse_events disable trigger trg_anti_abuse_daily_stats;
	`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into anti_abuse_events(public_id,user_id,action,outcome,created_at)
		select lpad(to_hex(generated_id),9,'0'),
			1+(generated_id%1000),
			case when generated_id%100=0 then 'comment.create' else 'review.submit' end,
			'allow',now()-(generated_id*interval '1 second')
		from generate_series(1,$1) generated_id`, antiAbusePlanFixtureRows); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into anti_abuse_content_fingerprints(user_id,action,exact_hash,simhash,created_at)
		select 1+(generated_id%1000),'comment.create','hash-' || generated_id,generated_id,
			now()-((generated_id%600)*interval '1 minute')
		from generate_series(1,$1) generated_id`, antiAbusePlanFixtureRows); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into anti_abuse_restrictions(public_id,user_id,actions,mode,source,starts_at,ends_at)
		select lpad(to_hex(generated_id),9,'0'),1+(generated_id%1000),array['comment.create'],
			'cooldown','automatic',now()-(generated_id*interval '1 second'),now()+interval '1 day'
		from generate_series(1,$1) generated_id`, antiAbusePlanFixtureRows); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `
		analyze anti_abuse_events;
		analyze anti_abuse_content_fingerprints;
		analyze anti_abuse_restrictions;
	`); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{
		"anti_abuse_events",
		"anti_abuse_content_fingerprints",
		"anti_abuse_restrictions",
	} {
		var count int
		if err = pool.QueryRow(ctx, "select count(*) from "+table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count < antiAbusePlanFixtureRows {
			t.Fatalf("%s has %d rows; want at least %d", table, count, antiAbusePlanFixtureRows)
		}
	}

	plans := []struct {
		name          string
		query         string
		args          []any
		table         string
		expectedIndex string
		expectedRows  int
	}{
		{
			name:          "risk events by action",
			query:         `select id from anti_abuse_events where action=$1 order by created_at desc,id desc limit 100`,
			args:          []any{"comment.create"},
			table:         "anti_abuse_events",
			expectedIndex: "idx_anti_abuse_events_action_time",
			expectedRows:  100,
		},
		{
			name:          "fingerprints by user/action",
			query:         `select id from anti_abuse_content_fingerprints where action=$1 and user_id=$2 and created_at>now()-interval '24 hours' order by created_at desc,id desc limit 200`,
			args:          []any{"comment.create", int64(1)},
			table:         "anti_abuse_content_fingerprints",
			expectedIndex: "idx_anti_abuse_fingerprint_user_action",
			expectedRows:  100,
		},
		{
			name:          "active user restrictions",
			query:         `select id from anti_abuse_restrictions where user_id=$1 and lifted_at is null and starts_at<=now() and (ends_at is null or ends_at>now()) order by starts_at desc limit 1`,
			args:          []any{int64(1)},
			table:         "anti_abuse_restrictions",
			expectedIndex: "idx_anti_abuse_restrictions_user_active",
			expectedRows:  1,
		},
	}
	for _, plan := range plans {
		text := explainAntiAbusePlan(t, ctx, pool, plan.query, plan.args...)
		if !strings.Contains(text, plan.expectedIndex) {
			t.Errorf("%s did not use %s:\n%s", plan.name, plan.expectedIndex, text)
		}
		if strings.Contains(text, "Seq Scan on "+plan.table) {
			t.Errorf("%s performed a sequential scan on %s:\n%s", plan.name, plan.table, text)
		}
		if !strings.Contains(text, "actual time=") || !strings.Contains(text, "Buffers:") {
			t.Errorf("%s lacks ANALYZE/BUFFERS execution evidence:\n%s", plan.name, text)
		}
		t.Logf("%s (%d-row fixture)\n%s", plan.name, antiAbusePlanFixtureRows, text)
		var measured []byte
		if err = pool.QueryRow(ctx, "explain (analyze, buffers, costs true, format json) "+plan.query, plan.args...).Scan(&measured); err != nil {
			t.Fatal(err)
		}
		if err = validateAntiAbuseMeasuredPlan(measured, plan.table, plan.expectedIndex, plan.expectedRows); err != nil {
			t.Fatalf("%s measured plan rejected: %v\n%s", plan.name, err, measured)
		}
		t.Logf("%s measured JSON baseline: %s", plan.name, measured)
		// Simulate the regression in this temporary-schema transaction only.
		// A successfully executed bad plan must be rejected by the same judge.
		tx, beginErr := pool.Begin(ctx)
		if beginErr != nil {
			t.Fatal(beginErr)
		}
		if _, err = tx.Exec(ctx, `set local enable_indexscan=off; set local enable_indexonlyscan=off; set local enable_bitmapscan=off`); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
		var degraded []byte
		if err = tx.QueryRow(ctx, "explain (analyze, buffers, costs true, format json) "+plan.query, plan.args...).Scan(&degraded); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err = tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if rejection := validateAntiAbuseMeasuredPlan(degraded, plan.table, plan.expectedIndex, plan.expectedRows); rejection == nil {
			t.Fatalf("%s accepted the real forced degraded plan: %s", plan.name, degraded)
		} else {
			t.Logf("%s forced degraded plan correctly rejected: %v; JSON: %s", plan.name, rejection, degraded)
		}
	}
}

func explainAntiAbusePlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, args ...any) string {
	t.Helper()
	rows, err := pool.Query(ctx, "explain (analyze, buffers, costs true, format text) "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	lines := make([]string, 0, 16)
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 {
		t.Fatal("EXPLAIN returned an empty plan")
	}
	return strings.Join(lines, "\n")
}
