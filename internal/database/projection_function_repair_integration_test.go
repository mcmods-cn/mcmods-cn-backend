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

func TestOCT02ProjectionFunctionForwardRepairAndRecovery(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" || os.Getenv("MCMODS_DB_FUNCTION_REPAIR_TEST_TARGET") == "" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 and the exact owned MCMODS_DB_FUNCTION_REPAIR_TEST_TARGET")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, err := Connect(ctx, config.Load())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	original, _, err := InspectProjectionFunctions(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if original.Database != os.Getenv("MCMODS_DB_FUNCTION_REPAIR_TEST_TARGET") {
		t.Fatal("test target is not the explicitly confirmed owned database")
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := pool.Exec(cleanupContext, `update public.schema_metadata set generation=168 where singleton`); err != nil {
			t.Errorf("restore owned test generation: %v", err)
		}
		if err := ChangeProjectionFunctions(cleanupContext, pool, original.Database, &original,
			func(ProjectionFunctionBackup) error { return nil }); err != nil {
			t.Errorf("restore original owned test definitions: %v", err)
		}
	})
	legacySQL, err := os.ReadFile("testdata/oct02_legacy_functions.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range commentPopularityTriggerNames {
		if _, err := pool.Exec(ctx, "drop trigger if exists "+name+" on public.comments"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, string(legacySQL)); err != nil {
		t.Fatal(err)
	}
	legacy, changed, err := InspectProjectionFunctions(ctx, pool)
	if err != nil || changed != 4 {
		t.Fatalf("legacy inspection changes=%d err=%v, want four", changed, err)
	}
	t.Run("wrong target is refused before backup", func(t *testing.T) {
		called := false
		err := ChangeProjectionFunctions(ctx, pool, original.Database+"_wrong", nil,
			func(ProjectionFunctionBackup) error { called = true; return nil })
		if err == nil || called {
			t.Fatal("wrong target was not refused before any backup or DDL")
		}
	})
	t.Run("failed backup leaves definitions unchanged", func(t *testing.T) {
		failure := errors.New("synthetic backup failure")
		err := ChangeProjectionFunctions(ctx, pool, original.Database, nil,
			func(ProjectionFunctionBackup) error { return failure })
		if !errors.Is(err, failure) {
			t.Fatalf("backup failure was lost: %v", err)
		}
		current, _, err := InspectProjectionFunctions(ctx, pool)
		if err != nil || !reflect.DeepEqual(current, legacy) {
			t.Fatalf("failed backup altered definitions: %v", err)
		}
	})
	t.Run("DDL failure rolls back earlier replacements", func(t *testing.T) {
		invalid := legacy
		invalid.Functions = make(map[string]string, len(legacy.Functions))
		for name, definition := range legacy.Functions {
			invalid.Functions[name] = definition
		}
		// The first replacement would change a legacy function. PostgreSQL
		// rejects the second body's syntax, so none may survive the transaction.
		invalid.Functions[projectionFunctionNames[0]] = original.Functions[projectionFunctionNames[0]]
		name := projectionFunctionNames[1]
		body, ok := projectionBackupBody(name, invalid.Functions[name])
		if !ok {
			t.Fatal("legacy fixture cannot be restored")
		}
		invalid.Functions[name] = strings.Replace(invalid.Functions[name], body, "\nBEGIN INVALID SYNTHETIC SYNTAX; END\n", 1)
		backedUp := false
		err := ChangeProjectionFunctions(ctx, pool, original.Database, &invalid,
			func(ProjectionFunctionBackup) error { backedUp = true; return nil })
		if err == nil || !backedUp {
			t.Fatal("synthetic DDL failure did not occur after the required backup")
		}
		current, _, err := InspectProjectionFunctions(ctx, pool)
		if err != nil || !reflect.DeepEqual(current, legacy) {
			t.Fatalf("failed DDL committed earlier replacements: %v", err)
		}
	})
	t.Run("binding DDL failure restores functions and original binding", func(t *testing.T) {
		// Only this explicitly confirmed, disposable database gets the scoped
		// failure injector. It rejects CREATE TRIGGER after replacements began.
		name := fmt.Sprintf("oct02_binding_failure_%d", time.Now().UnixNano())
		function := pgx.Identifier{"public", name}.Sanitize()
		trigger := pgx.Identifier{name}.Sanitize()
		if _, err := pool.Exec(ctx, "create function "+function+"() returns event_trigger language plpgsql as $$ begin raise exception 'synthetic comment binding DDL failure'; end; $$"); err != nil {
			t.Fatal(err)
		}
		defer func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cleanupCancel()
			if _, err := pool.Exec(cleanupCtx, "drop event trigger if exists "+trigger+"; drop function "+function+"()"); err != nil {
				t.Errorf("remove exclusively owned DDL failure injector: %v", err)
			}
		}()
		if _, err := pool.Exec(ctx, "create event trigger "+trigger+" on ddl_command_start when tag in ('CREATE TRIGGER') execute function "+function+"()"); err != nil {
			t.Fatal(err)
		}
		backedUp := false
		err := ChangeProjectionFunctions(ctx, pool, original.Database, nil,
			func(ProjectionFunctionBackup) error { backedUp = true; return nil })
		if err == nil || !backedUp {
			t.Fatal("synthetic binding failure did not stop DDL after backup")
		}
		current, _, err := InspectProjectionFunctions(ctx, pool)
		if err != nil || !reflect.DeepEqual(current, legacy) {
			t.Fatalf("binding failure did not restore original functions and binding: %v", err)
		}
	})
	var saved ProjectionFunctionBackup
	if err := ChangeProjectionFunctions(ctx, pool, original.Database, nil,
		func(backup ProjectionFunctionBackup) error { saved = backup; return nil }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved, legacy) {
		t.Fatal("repair did not preserve all original definitions")
	}
	current, changed, err := InspectProjectionFunctions(ctx, pool)
	if err != nil || changed != 0 || current.Generation != 168 {
		t.Fatalf("repaired inspection changes=%d generation=%d err=%v", changed, current.Generation, err)
	}
	t.Run("repeat application is idempotent", func(t *testing.T) {
		if err := ChangeProjectionFunctions(ctx, pool, original.Database, nil,
			func(ProjectionFunctionBackup) error { return nil }); err != nil {
			t.Fatal(err)
		}
		repeated, _, err := InspectProjectionFunctions(ctx, pool)
		if err != nil || !reflect.DeepEqual(current, repeated) {
			t.Fatalf("repeat repair changed definitions: %v", err)
		}
	})
	t.Run("restore original definitions", func(t *testing.T) {
		if err := ChangeProjectionFunctions(ctx, pool, original.Database, &saved,
			func(ProjectionFunctionBackup) error { return nil }); err != nil {
			t.Fatal(err)
		}
		restored, changed, err := InspectProjectionFunctions(ctx, pool)
		if err != nil || changed != 4 || !reflect.DeepEqual(restored, saved) {
			t.Fatalf("restored changes=%d definitions differ=%t err=%v", changed, !reflect.DeepEqual(restored, saved), err)
		}
	})
	t.Run("wrong generation is refused", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `update public.schema_metadata set generation=167 where singleton`); err != nil {
			t.Fatal(err)
		}
		called := false
		err := ChangeProjectionFunctions(ctx, pool, original.Database, nil,
			func(ProjectionFunctionBackup) error { called = true; return nil })
		if err == nil || called {
			t.Fatal("incompatible generation was not refused before backup or DDL")
		}
		if _, err := pool.Exec(ctx, `update public.schema_metadata set generation=168 where singleton`); err != nil {
			t.Fatal(err)
		}
	})
}
