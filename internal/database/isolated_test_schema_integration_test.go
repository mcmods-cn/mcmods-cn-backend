package database

import (
	"context"
	"errors"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIsolatedSchemaMarkerFailureRollsBackNamespaceIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database := newBaselineTestDatabase(t, ctx)
	// Restrict the injected failure to the random database created by this test.
	var childName string
	if err := database.QueryRow(ctx, `select current_database()`).Scan(&childName); err != nil {
		t.Fatal(err)
	}
	target, err := url.Parse(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("invalid test database URL")
	}
	target.Path = "/" + childName
	cfg, err := pgxpool.ParseConfig(target.String())
	parentConfig := database.Config().ConnConfig
	if err != nil || cfg.ConnConfig.Database != childName || cfg.ConnConfig.User != parentConfig.User ||
		cfg.ConnConfig.Host != parentConfig.Host || cfg.ConnConfig.Port != parentConfig.Port {
		t.Fatal("fixture URL does not match the created child database")
	}
	if _, err := database.Exec(ctx, `create function reject_test_marker() returns event_trigger language plpgsql
		as $$ begin raise exception 'fixture marker unavailable'; end $$;
		create event trigger reject_test_marker on ddl_command_start when tag in ('COMMENT')
		execute function reject_test_marker()`); err != nil {
		t.Fatal(err)
	}
	fixture, err := NewIsolatedTestSchema(ctx, target.String(), 1)
	if err == nil {
		t.Cleanup(func() { _ = fixture.Close(context.Background()) })
		t.Fatal("schema created despite marker failure")
	}
	var markerError *pgconn.PgError
	if !errors.As(err, &markerError) || markerError.Message != "fixture marker unavailable" {
		t.Fatal("fixture failed before reaching the injected marker error")
	}
	var found bool
	if err := database.QueryRow(ctx, `select exists(select 1 from pg_namespace where starts_with(nspname,'mcmods_audit_'))`).Scan(&found); err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("failed fixture left its new namespace behind without an ownership marker")
	}
}
