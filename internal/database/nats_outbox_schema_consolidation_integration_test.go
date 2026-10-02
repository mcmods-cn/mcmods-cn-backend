package database

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestNATSOutboxFinalShapeInstallsDirectlyIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the consolidated outbox schema")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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
	var publicGenerationBefore int
	if err = pool.QueryRow(ctx, `select generation from public.schema_metadata where singleton`).Scan(&publicGenerationBefore); err != nil {
		t.Fatal(err)
	}
	if err = InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	cleaned := false
	defer func() {
		if !cleaned {
			if dropErr := DropEphemeralSchema(context.Background(), pool); dropErr != nil {
				t.Errorf("drop ephemeral schema: %v", dropErr)
			}
		}
	}()

	rows, err := pool.Query(ctx, `select column_name,is_nullable,column_default
		from information_schema.columns where table_schema=current_schema() and table_name='nats_outbox'
		order by ordinal_position`)
	if err != nil {
		t.Fatal(err)
	}
	type columnFact struct{ nullable, defaultValue string }
	columns := make(map[string]columnFact)
	for rows.Next() {
		var name, nullable string
		var defaultValue *string
		if err = rows.Scan(&name, &nullable, &defaultValue); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		value := ""
		if defaultValue != nil {
			value = *defaultValue
		}
		columns[name] = columnFact{nullable: nullable, defaultValue: value}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	if len(columns) != 20 {
		t.Fatalf("nats_outbox columns=%d want=20: %v", len(columns), columns)
	}
	for name, defaultFragment := range map[string]string{
		"event_type": "''::text", "schema_version": "1", "occurred_at": "now()", "trace_id": "''::text",
		"status": "'pending'::text", "available_at": "now()", "locked_by": "''::text",
		"attempts": "0", "max_attempts": "12", "last_error": "''::text", "updated_at": "now()",
	} {
		fact, exists := columns[name]
		if !exists || fact.nullable != "NO" || !strings.Contains(fact.defaultValue, defaultFragment) {
			t.Errorf("column %s=%+v", name, fact)
		}
	}
	for _, nullable := range []string{"published_at", "locked_at"} {
		if fact := columns[nullable]; fact.nullable != "YES" {
			t.Errorf("column %s nullable=%q want YES", nullable, fact.nullable)
		}
	}

	var constraintDefinition string
	if err = pool.QueryRow(ctx, `select pg_get_constraintdef(oid) from pg_constraint
		where conrelid='nats_outbox'::regclass and conname='nats_outbox_status_check'`).Scan(&constraintDefinition); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"pending", "publishing", "published", "failed", "dead"} {
		if !strings.Contains(constraintDefinition, "'"+status+"'") {
			t.Errorf("status constraint is missing %q: %s", status, constraintDefinition)
		}
	}

	indexRows, err := pool.Query(ctx, `select indexname,indexdef from pg_indexes
		where schemaname=current_schema() and tablename='nats_outbox'`)
	if err != nil {
		t.Fatal(err)
	}
	indexes := make(map[string]string)
	for indexRows.Next() {
		var name, definition string
		if err = indexRows.Scan(&name, &definition); err != nil {
			indexRows.Close()
			t.Fatal(err)
		}
		indexes[name] = strings.ToLower(definition)
	}
	if err = indexRows.Err(); err != nil {
		indexRows.Close()
		t.Fatal(err)
	}
	indexRows.Close()
	for name, fragments := range map[string][]string{
		"idx_nats_outbox_pending":        {"(available_at, id)", "status = any", "published_at is null"},
		"idx_nats_outbox_status_created": {"(status, created_at)"},
		"idx_nats_outbox_aggregate":      {"(aggregate_type, aggregate_id, id)"},
	} {
		definition := indexes[name]
		for _, fragment := range fragments {
			if !strings.Contains(definition, fragment) {
				t.Errorf("index %s is missing %q: %s", name, fragment, definition)
			}
		}
	}

	var eventType, status string
	var schemaVersion, attempts, maxAttempts int
	if err = pool.QueryRow(ctx, `insert into nats_outbox(event_id,subject,aggregate_type,aggregate_id,payload)
		values('schema-direct','test.subject','test','schema-direct','{}')
		returning event_type,schema_version,status,attempts,max_attempts`).
		Scan(&eventType, &schemaVersion, &status, &attempts, &maxAttempts); err != nil {
		t.Fatal(err)
	}
	if eventType != "" || schemaVersion != 1 || status != "pending" || attempts != 0 || maxAttempts != 12 {
		t.Fatalf("outbox defaults eventType=%q schema=%d status=%q attempts=%d max=%d",
			eventType, schemaVersion, status, attempts, maxAttempts)
	}
	if _, err = pool.Exec(ctx, `insert into nats_outbox(event_id,subject,aggregate_type,aggregate_id,payload,status)
		values('schema-invalid','test.subject','test','schema-invalid','{}','unknown')`); err == nil {
		t.Fatal("nats_outbox accepted a status outside the final state machine")
	}

	if err = DropEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	cleaned = true
	var publicGenerationAfter int
	if err = pool.QueryRow(ctx, `select generation from public.schema_metadata where singleton`).Scan(&publicGenerationAfter); err != nil {
		t.Fatal(err)
	}
	if publicGenerationAfter != publicGenerationBefore {
		t.Fatalf("public generation changed from %d to %d", publicGenerationBefore, publicGenerationAfter)
	}
}
