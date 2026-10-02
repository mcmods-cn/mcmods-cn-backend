package database

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/config"
)

func TestOCT02LogRedactionForwardUpgradeIntegration(t *testing.T) {
	target := os.Getenv("MCMODS_DB_FUNCTION_REPAIR_TEST_TARGET")
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" || target == "" {
		t.Skip("requires an explicitly confirmed, disposable, exclusively owned PostgreSQL database")
	}
	cfg := config.Load()
	name, err := cfg.DB.EffectiveName()
	if err != nil || name != target {
		t.Fatal("configured target does not match the exclusively owned database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := Connect(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	initial, err := InspectLogRedactionSchema(ctx, pool)
	if err != nil || initial.Database != target || initial.Generation != 168 {
		t.Fatalf("inspect exclusively owned target: %v", err)
	}
	// This test owns the disposable database, not merely a database whose name
	// contains 'test'. Remove only our known marker to reproduce original 168.
	if initial.Present {
		if _, err := pool.Exec(ctx, `alter table public.log_shares drop column redaction_applied_version`); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		cleanup, cancelCleanup := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelCleanup()
		if _, err := pool.Exec(cleanup, `update public.schema_metadata set generation=168 where singleton`); err != nil {
			t.Errorf("restore exclusively owned generation: %v", err)
		}
		if err := ApplyLogRedactionSchemaUpgrade(cleanup, pool, target, func(LogRedactionSchemaState) error { return nil }); err != nil {
			t.Errorf("leave exclusively owned target on current expanded schema: %v", err)
		}
	})
	var ownerID, fileID int64
	if err := pool.QueryRow(ctx, `insert into public.users(username,email,password_hash,status)
		values($1,$2,'not-used','active') returning id`, fmt.Sprintf("oct02-redactor-%d", time.Now().UnixNano()),
		fmt.Sprintf("oct02-redactor-%d@example.test", time.Now().UnixNano())).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `delete from public.users where id=$1`, ownerID); err != nil {
			t.Errorf("remove synthetic redactor owner: %v", err)
		}
		if fileID != 0 {
			if _, err := pool.Exec(context.Background(), `delete from public.oss_files where id=$1`, fileID); err != nil {
				t.Errorf("remove synthetic redactor source: %v", err)
			}
		}
	})
	if err := pool.QueryRow(ctx, `insert into public.oss_files(bucket,endpoint,region,object_key)
		values('fixture','https://oss.example.test','fixture',$1) returning id`, fmt.Sprintf("oct02-redactor-%d.txt", time.Now().UnixNano())).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into public.log_shares(public_code,owner_user_id,source_type,source_file_id,
		status,redaction_version,redaction_counts,expires_at) values
		('oct02rdct001',$1,'file',$2,'ready',1,'{"token":1}',now()+interval '1 day'),
		('oct02rdct002',$1,'file',$2,'ready',2,'{"token":2}',now()+interval '1 day')`, ownerID, fileID); err != nil {
		t.Fatal(err)
	}
	legacy, err := InspectLogRedactionSchema(ctx, pool)
	if err != nil || legacy.Present {
		t.Fatalf("legacy schema was not reproduced: %v", err)
	}
	t.Run("old generation startup fails with explicit upgrade instruction", func(t *testing.T) {
		if err := Migrate(ctx, pool); err == nil || !strings.Contains(err.Error(), "cmd/db-log-redaction-upgrade") {
			t.Fatalf("missing schema did not produce an actionable upgrade instruction: %v", err)
		}
	})
	t.Run("wrong target and generation are rejected before backup", func(t *testing.T) {
		called := false
		backup := func(LogRedactionSchemaState) error { called = true; return nil }
		if err := ApplyLogRedactionSchemaUpgrade(ctx, pool, target+"_wrong", backup); err == nil || called {
			t.Fatal("wrong target was not rejected before DDL/backup")
		}
		if _, err := pool.Exec(ctx, `update public.schema_metadata set generation=167 where singleton`); err != nil {
			t.Fatal(err)
		}
		err := ApplyLogRedactionSchemaUpgrade(ctx, pool, target, backup)
		if _, restoreErr := pool.Exec(ctx, `update public.schema_metadata set generation=168 where singleton`); restoreErr != nil {
			t.Fatal(restoreErr)
		}
		if err == nil || called {
			t.Fatal("wrong generation was not rejected before DDL/backup")
		}
	})
	t.Run("failed metadata persistence leaves legacy schema unchanged", func(t *testing.T) {
		failure := errors.New("synthetic metadata persistence failure")
		if err := ApplyLogRedactionSchemaUpgrade(ctx, pool, target, func(LogRedactionSchemaState) error { return failure }); !errors.Is(err, failure) {
			t.Fatalf("metadata persistence failure was lost: %v", err)
		}
		current, err := InspectLogRedactionSchema(ctx, pool)
		if err != nil || !reflect.DeepEqual(current, legacy) {
			t.Fatalf("failed metadata persistence changed schema: %v", err)
		}
	})
	t.Run("conflicting DDL rolls back the new column", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `alter table public.log_shares add constraint
			log_shares_redaction_applied_version_check check(redaction_version>=1)`); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := pool.Exec(context.Background(), `alter table public.log_shares drop constraint
				log_shares_redaction_applied_version_check`); err != nil {
				t.Errorf("remove exclusively owned conflicting constraint: %v", err)
			}
		}()
		called := false
		err := ApplyLogRedactionSchemaUpgrade(ctx, pool, target,
			func(LogRedactionSchemaState) error { called = true; return nil })
		if err == nil || !called {
			t.Fatal("DDL conflict did not fail after recording original state")
		}
		current, err := InspectLogRedactionSchema(ctx, pool)
		if err != nil || !reflect.DeepEqual(current, legacy) {
			t.Fatalf("failed DDL committed its new column: %v", err)
		}
	})
	t.Run("interrupted validation preserves rows and resumes", func(t *testing.T) {
		name := fmt.Sprintf("oct02_redactor_validate_failure_%d", time.Now().UnixNano())
		function, trigger := pgx.Identifier{"public", name}.Sanitize(), pgx.Identifier{name}.Sanitize()
		if _, err := pool.Exec(ctx, "create function "+function+`() returns event_trigger language plpgsql as $$ begin
			if position('validate constraint log_shares_redaction_applied_version_check' in lower(current_query()))>0 then
				raise exception 'synthetic redactor validation failure'; end if; end $$`); err != nil {
			t.Fatal(err)
		}
		remove := func() {
			if _, err := pool.Exec(context.Background(), "drop event trigger if exists "+trigger+"; drop function if exists "+function+"()"); err != nil {
				t.Errorf("remove exclusively owned validation injector: %v", err)
			}
		}
		defer func() { remove() }()
		if _, err := pool.Exec(ctx, "create event trigger "+trigger+" on ddl_command_start when tag in ('ALTER TABLE') execute function "+function+"()"); err != nil {
			t.Fatal(err)
		}
		var saved LogRedactionSchemaState
		if err := ApplyLogRedactionSchemaUpgrade(ctx, pool, target, func(state LogRedactionSchemaState) error { saved = state; return nil }); err == nil {
			t.Fatal("validation injector did not reject validation")
		}
		if !reflect.DeepEqual(saved, legacy) {
			t.Fatal("pre-change metadata did not describe original 168")
		}
		current, err := InspectLogRedactionSchema(ctx, pool)
		if err != nil || !current.Present || current.Validated {
			t.Fatalf("interrupted validation did not leave resumable schema: %v", err)
		}
		if err := Migrate(ctx, pool); err == nil {
			t.Fatal("backend accepted an unvalidated schema extension")
		}
		remove()
		remove = func() {}
		if err := ApplyLogRedactionSchemaUpgrade(ctx, pool, target, func(LogRedactionSchemaState) error { return nil }); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("same source v1 and v2 identities survive and original rows default to v1", func(t *testing.T) {
		var rows, applied, versionSum, tokenSum int
		if err := pool.QueryRow(ctx, `select count(*)::int,sum(redaction_applied_version)::int,
			sum(redaction_version)::int,sum((redaction_counts->>'token')::int)::int
			from public.log_shares where owner_user_id=$1 and source_file_id=$2`, ownerID, fileID).
			Scan(&rows, &applied, &versionSum, &tokenSum); err != nil {
			t.Fatal(err)
		}
		if rows != 2 || applied != 2 || versionSum != 3 || tokenSum != 3 {
			t.Fatalf("upgrade altered legacy identities/counts: %d/%d/%d/%d", rows, applied, versionSum, tokenSum)
		}
		if _, err := pool.Exec(ctx, `update public.log_shares set redaction_applied_version=2 where public_code='oct02rdct001'`); err != nil {
			t.Fatal("updating applied marker collided with source identity:", err)
		}
		if _, err := pool.Exec(ctx, `update public.log_shares set redaction_applied_version=0 where public_code='oct02rdct001'`); err == nil {
			t.Fatal("invalid applied redactor version bypassed database invariant")
		}
	})
	t.Run("repeat apply and restart are idempotent", func(t *testing.T) {
		if err := ApplyLogRedactionSchemaUpgrade(ctx, pool, target, func(LogRedactionSchemaState) error { return nil }); err != nil {
			t.Fatal(err)
		}
		if err := Migrate(ctx, pool); err != nil {
			t.Fatal(err)
		}
	})
}
