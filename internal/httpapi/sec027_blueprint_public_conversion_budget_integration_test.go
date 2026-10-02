package httpapi

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSEC027PublicBlueprintConversionsHaveAtomicGlobalAdmissionIntegration(t *testing.T) {
	pool := newSEC027BlueprintBudgetPool(t)
	const attempts = 24
	const wantAccepted = 16
	start := make(chan struct{})
	results := make(chan error, attempts)
	var ready sync.WaitGroup
	ready.Add(attempts)
	for attempt := range attempts {
		go func() {
			ready.Done()
			<-start
			tx, err := pool.Begin(context.Background())
			if err == nil {
				defer tx.Rollback(context.Background())
				_, err = enqueueBlueprintJobTx(context.Background(), tx, int64(27000+attempt), int64(28000+attempt), "convert", "schem")
			}
			if err == nil {
				// Keep the admission-to-commit gap open: a database-global lock
				// must remain held through the task and Outbox commit.
				_, err = tx.Exec(context.Background(), `select pg_sleep(0.03)`)
			}
			if err == nil {
				err = tx.Commit(context.Background())
			}
			results <- err
		}()
	}
	ready.Wait()
	close(start)
	accepted, rejected := 0, 0
	for range attempts {
		err := <-results
		if err == nil {
			accepted++
			continue
		}
		if err.Error() != "blueprint global active conversion limit reached" {
			t.Fatalf("unexpected global admission error: %v", err)
		}
		rejected++
	}
	if accepted != wantAccepted || rejected != attempts-wantAccepted {
		t.Fatalf("global conversion admission accepted=%d rejected=%d, want %d/%d", accepted, rejected,
			wantAccepted, attempts-wantAccepted)
	}
	var tasks, events int
	if err := pool.QueryRow(context.Background(), `select count(*) from blueprint_jobs`).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `select count(*) from nats_outbox`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if tasks != wantAccepted || events != wantAccepted {
		t.Fatalf("atomic task/outbox facts=%d/%d, want %d/%d", tasks, events, wantAccepted, wantAccepted)
	}
}

func TestSEC027ActiveBlueprintConversionDeduplicationIsDatabaseAtomicIntegration(t *testing.T) {
	pool := newSEC027BlueprintBudgetPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = enqueueBlueprintJobTx(ctx, tx, 27100, 28100, "convert", "litematic"); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = enqueueBlueprintJobTx(ctx, tx, 27100, 28101, "convert", "litematic"); !isUniqueViolation(err) {
		t.Fatalf("duplicate active conversion error=%v, want database unique violation", err)
	}
}

func TestSEC027GlobalConversionAdmissionStaysBoundedAtMillionJobsIntegration(t *testing.T) {
	pool := newSEC027BlueprintBudgetPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `insert into blueprint_jobs(public_id,blueprint_id,operation,target_format,status,created_by)
		select lpad(value::text,9,'0'),value,'convert','schem','completed',value
		from generate_series(1,1000000) value;
		insert into blueprint_jobs(public_id,blueprint_id,operation,target_format,status,created_by)
		select lpad(value::text,9,'0'),value,'convert','schem','queued',value
		from generate_series(1000001,1000016) value;
		analyze blueprint_jobs`); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	rows, err := pool.Query(ctx, "explain (analyze,buffers,format text) "+blueprintGlobalActiveConversionCountSQL,
		maxBlueprintActiveConversionsGlobal)
	if err != nil {
		t.Fatal(err)
	}
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	duration := time.Since(started)
	planText := plan.String()
	usesActiveIndex := strings.Contains(planText, "idx_sec027_queue") || strings.Contains(planText, "idx_sec027_active_operation")
	if duration > 2*time.Second || strings.Contains(planText, "Seq Scan on blueprint_jobs") || !usesActiveIndex {
		t.Fatalf("million-job global admission took %s or missed the active queue index:\n%s", duration, planText)
	}
	t.Logf("million-job global conversion admission: %s\n%s", duration, planText)
}

func TestSEC027PublicConversionHandlerExposesStableCapacityAndDeduplicationErrors(t *testing.T) {
	raw, err := os.ReadFile("blueprint_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, required := range []string{
		"maxBlueprintActiveConversionsGlobal", "reserveBlueprintGlobalConversionBudgetTx", "blueprint-conversion-global",
		"BLUEPRINT_GLOBAL_CONCURRENCY_LIMIT", "BLUEPRINT_CONVERSION_ALREADY_QUEUED",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("missing public conversion capacity contract %q", required)
		}
	}
	if !strings.Contains(source, "writeAPIError(w, http.StatusTooManyRequests") || !strings.Contains(source, "writeAPIError(w, http.StatusConflict") {
		t.Fatal("global capacity and active deduplication must map to stable 429/409 responses")
	}
	enqueueStart := strings.Index(source, "func enqueueBlueprintJobTx(")
	if enqueueStart < 0 {
		t.Fatal("cannot locate the transactional conversion producer")
	}
	enqueueEnd := strings.Index(source[enqueueStart:], "func reserveBlueprintGlobalConversionBudgetTx(")
	if enqueueEnd < 0 {
		t.Fatal("cannot locate the transactional conversion producer")
	}
	enqueueBody := source[enqueueStart : enqueueStart+enqueueEnd]
	reserveCall := strings.Index(enqueueBody, "reserveBlueprintGlobalConversionBudgetTx(ctx, tx)")
	insert := strings.Index(enqueueBody, "insert into blueprint_jobs")
	if reserveCall < 0 || insert < 0 || reserveCall >= insert {
		t.Fatal("global admission must precede the task insertion in one transaction")
	}
	helperStart := strings.Index(source, "func reserveBlueprintGlobalConversionBudgetTx(")
	if helperStart < 0 {
		t.Fatal("cannot locate the global conversion reservation helper")
	}
	helperEnd := strings.Index(source[helperStart:], "func reserveBlueprintUserJobBudgetTx(")
	if helperEnd < 0 {
		t.Fatal("cannot locate the global conversion reservation helper")
	}
	helperBody := source[helperStart : helperStart+helperEnd]
	globalLock := strings.Index(helperBody, "blueprint-conversion-global")
	globalCount := strings.Index(helperBody, "blueprintGlobalActiveConversionCountSQL")
	if globalLock < 0 || globalCount < 0 || globalLock >= globalCount {
		t.Fatal("global lock must precede the bounded active conversion count")
	}
}

func TestSEC027OnlyTheActiveOperationConstraintIsReportedAsQueued(t *testing.T) {
	if !isBlueprintActiveJobConflict(&pgconn.PgError{Code: "23505", ConstraintName: "idx_blueprint_jobs_active_operation"}) {
		t.Fatal("active blueprint operation conflict was not classified")
	}
	for _, databaseErr := range []*pgconn.PgError{
		{Code: "23505", ConstraintName: "blueprint_jobs_public_id_key"},
		{Code: "23505", ConstraintName: "unrelated_constraint"},
		{Code: "23514", ConstraintName: "idx_blueprint_jobs_active_operation"},
	} {
		if isBlueprintActiveJobConflict(databaseErr) {
			t.Fatalf("unrelated database error was misclassified: %#v", databaseErr)
		}
	}
}

func newSEC027BlueprintBudgetPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 with isolated PostgreSQL to verify public blueprint conversion admission")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	baseConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(adminPool.Close)
	schemaName := fmt.Sprintf("sec027_blueprint_budget_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schemaName}.Sanitize()
	if _, err = adminPool.Exec(ctx, "create schema "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, dropErr := adminPool.Exec(context.Background(), "drop schema "+quotedSchema+" cascade"); dropErr != nil {
			t.Errorf("drop isolated SEC-027 schema: %v", dropErr)
		}
	})

	scopedConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	scopedConfig.MaxConns = 32
	scopedConfig.ConnConfig.RuntimeParams["search_path"] = schemaName + ",pg_catalog"
	pool, err := pgxpool.NewWithConfig(ctx, scopedConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err = pool.Exec(ctx, `
		create sequence sec027_public_id_seq;
		create function new_public_id() returns text language sql volatile as
			$$select substr(md5(nextval('sec027_public_id_seq')::text),1,9)$$;
		create table blueprint_jobs (
			id bigserial primary key,
			public_id text not null unique default new_public_id(),
			blueprint_id bigint not null,
			operation text not null,
			target_format text not null default '',
			status text not null default 'queued',
			created_by bigint,
			created_at timestamptz not null default now()
		);
		create index idx_sec027_queue on blueprint_jobs(status,created_at);
		create index idx_sec027_creator_active on blueprint_jobs(created_by,id)
			where status in ('queued','processing') and created_by is not null;
		create unique index idx_sec027_active_operation on blueprint_jobs(blueprint_id,operation,target_format)
			where status in ('queued','processing');
		create table nats_outbox (
			id bigserial primary key,
			event_id text not null,
			event_type text not null,
			schema_version integer not null,
			subject text not null,
			aggregate_type text not null,
			aggregate_id text not null,
			trace_id text not null,
			payload jsonb not null,
			occurred_at timestamptz not null,
			status text not null,
			available_at timestamptz not null
		)`); err != nil {
		t.Fatal(err)
	}
	return pool
}
