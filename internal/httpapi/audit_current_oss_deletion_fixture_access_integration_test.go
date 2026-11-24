package httpapi

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/tools/testing/postgrestarget"
)

// These callers own fresh connection/transaction temp fixtures and create their
// own oss_files first. Copy the actual current migration shape, not a hand-kept
// subset. LIKE copies defaults/checks/indexes but not foreign keys; explicitly
// recreate all three outbox FKs against this connection's owned temp tables.
type accessFixtureSQL interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func installAccessCurrentOSSDeletionFixture(t *testing.T, ctx context.Context, pool accessFixtureSQL) {
	t.Helper()
	if err := database.RequireOSSUploadByteBindingSchema(ctx, pool); err != nil {
		t.Fatalf("current migrated public OSS fixture prerequisite: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		create temp table users(like public.users including all);
		create temp sequence access_fixture_oss_user_ids;
		alter table users alter column id set default nextval('pg_temp.access_fixture_oss_user_ids'::regclass);
		alter sequence pg_temp.access_fixture_oss_user_ids owned by pg_temp.users.id;
		create temp table oss_upload_byte_bindings(like public.oss_upload_byte_bindings including all);
		create temp table oss_object_deletion_outbox(like public.oss_object_deletion_outbox including all);
		create temp sequence access_fixture_oss_deletion_ids;
		alter table oss_object_deletion_outbox alter column id set default nextval('pg_temp.access_fixture_oss_deletion_ids'::regclass);
		alter sequence pg_temp.access_fixture_oss_deletion_ids owned by pg_temp.oss_object_deletion_outbox.id;
		alter table oss_object_deletion_outbox
			add constraint oss_object_deletion_outbox_oss_file_id_fkey foreign key(oss_file_id) references pg_temp.oss_files(id) on delete set null,
			add constraint oss_object_deletion_outbox_last_replayed_by_fkey foreign key(last_replayed_by) references pg_temp.users(id) on delete set null,
			add constraint oss_deletion_raw_binding_fk foreign key(raw_upload_binding_id) references pg_temp.oss_upload_byte_bindings(id) on delete restrict;
	`); err != nil {
		t.Fatalf("install exact owned OSS deletion fixture: %v", err)
	}
	var foreignKeys, externalSequenceDefaults int
	if err := pool.QueryRow(ctx, `select
		(select count(*) from pg_constraint where conrelid='pg_temp.oss_object_deletion_outbox'::regclass and contype='f'
		 and ((conname='oss_object_deletion_outbox_oss_file_id_fkey' and confrelid='pg_temp.oss_files'::regclass and confdeltype='n')
		 or (conname='oss_object_deletion_outbox_last_replayed_by_fkey' and confrelid='pg_temp.users'::regclass and confdeltype='n')
		 or (conname='oss_deletion_raw_binding_fk' and confrelid='pg_temp.oss_upload_byte_bindings'::regclass and confdeltype='r'))),
		(select count(*) from pg_attrdef def join pg_depend dep on dep.classid='pg_attrdef'::regclass and dep.objid=def.oid
		 join pg_class seq on seq.oid=dep.refobjid and dep.refclassid='pg_class'::regclass and seq.relkind='S'
		 where def.adrelid in ('pg_temp.users'::regclass,'pg_temp.oss_upload_byte_bindings'::regclass,'pg_temp.oss_object_deletion_outbox'::regclass)
		 and seq.relnamespace<>pg_my_temp_schema())`).Scan(&foreignKeys, &externalSequenceDefaults); err != nil || foreignKeys != 3 || externalSequenceDefaults != 0 {
		t.Fatalf("owned outbox fixture FKs/external sequence defaults=%d/%d err=%v", foreignKeys, externalSequenceDefaults, err)
	}
	t.Log("actual migrated outbox shape cloned with three owned FKs and zero external sequence defaults")
}

func requireAccessFixturePGError(t *testing.T, err error, code, constraint, message string) {
	t.Helper()
	if findAccessFixturePGError(err, code, constraint, message) == nil {
		t.Fatalf("expected exact PostgreSQL fault code=%s constraint=%q message=%q, got %v", code, constraint, message, err)
	}
	t.Logf("actual expected PostgreSQL fault code=%s constraint=%q", code, constraint)
}

func findAccessFixturePGError(err error, code, constraint, message string) *pgconn.PgError {
	if pgError, ok := err.(*pgconn.PgError); ok && pgError.Code == code &&
		(constraint == "" || pgError.ConstraintName == constraint) && (message == "" || strings.Contains(pgError.Message, message)) {
		return pgError
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, inner := range joined.Unwrap() {
			if found := findAccessFixturePGError(inner, code, constraint, message); found != nil {
				return found
			}
		}
	} else if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return findAccessFixturePGError(wrapped.Unwrap(), code, constraint, message)
	}
	return nil
}

func accessFixtureBusinessRows(t *testing.T, ctx context.Context, pool accessFixtureSQL, tables ...string) string {
	t.Helper()
	var result strings.Builder
	for _, table := range tables {
		var rows string
		if err := pool.QueryRow(ctx, `select coalesce(jsonb_agg(to_jsonb(snapshot_row) order by to_jsonb(snapshot_row)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` as snapshot_row`).Scan(&rows); err != nil {
			t.Fatalf("snapshot owned business table %s: %v", table, err)
		}
		result.WriteString(table + ":" + rows + "\n")
	}
	return result.String()
}

// Maintenance deliberately addresses public tables. Give it a new database,
// not an ordinary-schema fixture whose public tables belong to another scope.
func openAccessOwnedMigratedFixture(t *testing.T, ctx context.Context, connectionString string) *pgxpool.Pool {
	t.Helper()
	parentConfig, err := postgrestarget.ParseSingleLoopback(connectionString)
	if err != nil {
		t.Fatal(err)
	}
	parentConfig.MaxConns = 1
	parent, err := pgxpool.NewWithConfig(ctx, parentConfig)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := createAuditT002OwnedDatabase(ctx, parent, test018DatabaseScope)
	if err != nil {
		parent.Close()
		t.Fatalf("create owned fixture database: %v; no cleanup without original identity", err)
	}
	var pool *pgxpool.Pool
	t.Cleanup(func() {
		defer parent.Close()
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := cleanupAuditT002OwnedDatabase(cleanup, parent, test018DatabaseScope, identity, func(ctx context.Context) error {
			if pool != nil {
				return closeAuditT002OwnedPool(ctx, pool)
			}
			return nil
		}); err != nil {
			t.Errorf("owned fixture database cleanup retained for diagnosis: %v", err)
		}
	})
	scoped := parentConfig.Copy()
	scoped.ConnConfig.Database = identity.Name
	scoped.MaxConns, scoped.MinConns = 1, 1
	pool, err = pgxpool.NewWithConfig(ctx, scoped)
	if err != nil {
		t.Fatal(err)
	}
	if err = database.Initialize(ctx, pool); err != nil {
		t.Fatal(err)
	}
	t.Logf("ACCESS_OWNED_FIXTURE_DATABASE name=%s oid=%d owner=%s nonce=%s maxConns=1", identity.Name, identity.OID, identity.Owner, identity.Marker)
	return pool
}
