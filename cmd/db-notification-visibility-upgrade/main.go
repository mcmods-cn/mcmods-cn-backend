// Explicit, additive migration. The default invocation is read-only.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

type visibilityQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}
type visibilityInspection struct {
	ColumnsPresent          int  `json:"columns_present"`
	ColumnsCorrect          int  `json:"columns_correct"`
	LeaseTablePresent       bool `json:"lease_table_present"`
	LeaseColumnsCorrect     bool `json:"lease_columns_correct"`
	LeaseConstraintsCorrect bool `json:"lease_constraints_correct"`
	TriggerCorrect          bool `json:"trigger_correct"`
	Ready                   bool `json:"ready"`
}

func inspectVisibilitySchema(ctx context.Context, query visibilityQuery) (visibilityInspection, error) {
	var result visibilityInspection
	err := query.QueryRow(ctx, `with expected(table_name,column_name,type_oid,not_null) as (values
 ('notifications','revision_xid','pg_catalog.xid8'::regtype::oid,true),
 ('notification_receipts','read_revision_xid','pg_catalog.xid8'::regtype::oid,false),
 ('notification_read_watermarks','visibility_snapshot','pg_catalog.pg_snapshot'::regtype::oid,false)
 ), actual as (
 select expected.*,attribute.atttypid,attribute.attnotnull from expected
 left join pg_attribute attribute on attribute.attrelid=to_regclass('public.'||expected.table_name)
 and attribute.attname=expected.column_name and attribute.attnum>0 and not attribute.attisdropped
 ) select count(atttypid)::int,count(*) filter(where atttypid=type_oid and attnotnull=not_null)::int,
 to_regclass('public.realtime_connection_leases') is not null,
 (select count(*)=4 from (values ('lease_id','pg_catalog.text'::regtype::oid),('user_id','pg_catalog.int8'::regtype::oid),
 ('session_hash','pg_catalog.text'::regtype::oid),('expires_at','pg_catalog.timestamptz'::regtype::oid)) expected(name,type_oid)
 join pg_attribute attribute on attribute.attrelid=to_regclass('public.realtime_connection_leases') and attribute.attname=expected.name
 and attribute.atttypid=expected.type_oid and attribute.attnotnull and not attribute.attisdropped),
 exists(select 1 from pg_constraint primary_key join pg_attribute primary_attribute
 on primary_attribute.attrelid=primary_key.conrelid and primary_attribute.attname='lease_id'
 where primary_key.conrelid=to_regclass('public.realtime_connection_leases') and primary_key.contype='p'
 and primary_key.conkey=array[primary_attribute.attnum]::smallint[])
 and exists(select 1 from pg_constraint foreign_key join pg_attribute local_attribute
 on local_attribute.attrelid=foreign_key.conrelid and local_attribute.attname='user_id'
 join pg_attribute remote_attribute on remote_attribute.attrelid=foreign_key.confrelid and remote_attribute.attname='id'
 where foreign_key.conrelid=to_regclass('public.realtime_connection_leases') and foreign_key.contype='f'
 and foreign_key.confrelid=to_regclass('public.users') and foreign_key.confdeltype='c'
 and foreign_key.conkey=array[local_attribute.attnum]::smallint[] and foreign_key.confkey=array[remote_attribute.attnum]::smallint[]),
 exists(select 1 from pg_trigger trigger_row join pg_proc routine on routine.oid=trigger_row.tgfoid
 join pg_namespace routine_namespace on routine_namespace.oid=routine.pronamespace
 where trigger_row.tgrelid=to_regclass('public.notifications') and trigger_row.tgname='trg_notifications_revision'
 and trigger_row.tgenabled='O' and trigger_row.tgtype=23 and trigger_row.tgqual is null and not trigger_row.tgisinternal
 and routine_namespace.nspname='public' and routine.proname='stamp_notification_revision' and routine.pronargs=0
 and routine.prorettype='pg_catalog.trigger'::regtype
 and regexp_replace(lower(routine.prosrc),'[[:space:]]','','g')='beginnew.revision_xid:=pg_current_xact_id();returnnew;end')
 from actual`).Scan(&result.ColumnsPresent, &result.ColumnsCorrect, &result.LeaseTablePresent, &result.LeaseColumnsCorrect, &result.LeaseConstraintsCorrect, &result.TriggerCorrect)
	result.Ready = result.ColumnsCorrect == 3 && result.LeaseColumnsCorrect && result.LeaseConstraintsCorrect && result.TriggerCorrect
	return result, err
}

type migrationOptions struct {
	Apply                              bool
	ConfirmedDatabase, BackupReference string
}

func runVisibilityMigration(ctx context.Context, pool *pgxpool.Pool, options migrationOptions, output io.Writer) error {
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return errors.New("cannot acquire migration connection")
	}
	defer connection.Release()
	// Pin every statement to the intended schema regardless of caller-provided
	// connection options or a role/database search_path.
	if _, err = connection.Exec(ctx, `select set_config('search_path','public,pg_catalog',false)`); err != nil {
		return errors.New("cannot pin public schema")
	}
	var name string
	var generation int
	if err = connection.QueryRow(ctx, `select current_database(),generation from public.schema_metadata where singleton`).Scan(&name, &generation); err != nil || generation != 168 {
		return errors.New("requires an accessible generation-168 database")
	}
	before, err := inspectVisibilitySchema(ctx, connection)
	if err != nil {
		return errors.New("failed to inspect notification schema")
	}
	after := before
	if options.Apply {
		if options.ConfirmedDatabase != name || options.BackupReference == "" {
			return errors.New("apply requires exact --database and --backup-reference")
		}
		if before.ColumnsPresent != before.ColumnsCorrect || (before.LeaseTablePresent && (!before.LeaseColumnsCorrect || !before.LeaseConstraintsCorrect)) {
			return errors.New("existing notification extension has incompatible types or lease constraints; no changes applied")
		}
		tx, txErr := connection.Begin(ctx)
		if txErr != nil {
			return errors.New("failed to begin migration")
		}
		defer tx.Rollback(context.Background())
		if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('mcmods-cn-schema-migrations'))`); err == nil {
			// Re-inspect under the same migration lock used by application startup.
			locked, inspectErr := inspectVisibilitySchema(ctx, tx)
			if inspectErr != nil {
				err = inspectErr
			} else if locked.ColumnsPresent != locked.ColumnsCorrect || (locked.LeaseTablePresent && (!locked.LeaseColumnsCorrect || !locked.LeaseConstraintsCorrect)) {
				err = errors.New("schema changed to an incompatible type while waiting for migration lock")
			}
		}
		if err == nil {
			for _, statement := range database.NotificationVisibilitySchemaStatements() {
				if _, err = tx.Exec(ctx, statement); err != nil {
					break
				}
			}
		}
		if err == nil {
			after, err = inspectVisibilitySchema(ctx, tx)
			if err == nil && !after.Ready {
				err = errors.New("migration postconditions not satisfied")
			}
		}
		if err == nil {
			err = tx.Commit(ctx)
		}
		if err != nil {
			return errors.New("notification extension failed; transaction rolled back")
		}
		after, err = inspectVisibilitySchema(ctx, connection)
		if err != nil || !after.Ready {
			return errors.New("committed notification extension failed final verification")
		}
	}
	return json.NewEncoder(output).Encode(map[string]any{"database": name, "generation": generation, "before": before, "after": after, "applied": options.Apply, "columns_before": before.ColumnsPresent, "columns_after": after.ColumnsPresent, "historical_unseen_read_state_recoverable": false})
}

func main() {
	apply := flag.Bool("apply", false, "install notification visibility extension")
	confirmed := flag.String("database", "", "exact target database for apply")
	backup := flag.String("backup-reference", "", "existing verified backup reference for apply")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid database configuration")
		os.Exit(1)
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = "public,pg_catalog"
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid database configuration")
		os.Exit(1)
	}
	defer pool.Close()
	if err = runVisibilityMigration(ctx, pool, migrationOptions{*apply, *confirmed, *backup}, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
