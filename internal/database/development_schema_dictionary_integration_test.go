package database

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestDevelopmentSchemaDictionariesRejectUnreachableAndInvalidValuesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify development schema dictionaries")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
	if err = InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	if _, err = pool.Exec(ctx, `
		insert into public_routes(id,public_id,entity_type,internal_id)
		values(99164001,'rmap006aa','mod',99164001);
		insert into project_auto_update_settings(id,project_route_id,update_kind,interval_code)
		values(99164002,99164001,'changelog','month')
	`); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `insert into project_auto_update_runs(setting_id,status)
		values(99164002,'failed')`)
	assertPostgresCode(t, err, "23514")

	if _, err = pool.Exec(ctx, `
		insert into catalog_entities(id,identity_key,public_id,entity_type) values
			(99164003,'audit:recipe_type','rdead007a','recipe_type'),
			(99164004,'audit:recipe','rdead007b','recipe');
		insert into recipe_types(entity_id,canonical_id) values(99164003,'audit:recipe_type');
		insert into recipes(entity_id,recipe_type_id,semantic_fingerprint,identity_source)
		values(99164004,99164003,'audit-fingerprint','generated_index')
	`); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"backfill", "split"} {
		_, err = pool.Exec(ctx, `insert into recipe_version_bindings(recipe_id,version_code,source)
			values(99164004,$1,$2)`, "1.0-"+source, source)
		assertPostgresCode(t, err, "23514")
	}
	if _, err = pool.Exec(ctx, `insert into recipe_version_bindings(recipe_id,version_code,source) values
		(99164004,'1.0-import','import'),(99164004,'1.0-editor','editor')`); err != nil {
		t.Fatalf("insert reachable recipe binding sources: %v", err)
	}

	var revisionID int64
	if err = pool.QueryRow(ctx, `insert into content_revisions(
		aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash)
		values('mod','map006',1,'{}'::jsonb,'map006') returning id`).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into project_update_events(
		project_route_id,revision_id,update_kind,publication_batch_id)
		values(99164001,$1,'content_updated','map006-valid')`, revisionID); err != nil {
		t.Fatalf("insert project update event with valid numeric revision: %v", err)
	}
	_, err = pool.Exec(ctx, `insert into project_update_events(
		project_route_id,revision_id,update_kind,publication_batch_id)
		values(99164001,9223372036854775807,'content_updated','map006-invalid')`)
	assertPostgresCode(t, err, "23503")
	var revisionType string
	if err = pool.QueryRow(ctx, `select pg_typeof(revision_id)::text from project_update_events
		where publication_batch_id='map006-valid'`).Scan(&revisionType); err != nil || revisionType != "bigint" {
		t.Fatalf("project update revision type=%q err=%v, want bigint", revisionType, err)
	}
}

func assertPostgresCode(t *testing.T, err error, want string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != want {
		t.Fatalf("postgres error=%v, want SQLSTATE %s", err, want)
	}
}
