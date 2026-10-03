package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Forward change 20261003_01 adds a pattern-operator index without replacing
// generation-168 data or indexes used by equality and collation-aware ordering.
const CatalogTagPrefixIndexName = "idx_catalog_tags_canonical_prefix_registry"

const catalogTagPrefixIndexSQL = `create index idx_catalog_tags_canonical_prefix_registry
	on catalog_tags(lower(canonical_id) text_pattern_ops,registry,entity_id)`

type CatalogTagPrefixIndexState struct {
	Exists  bool `json:"exists"`
	Matches bool `json:"matches"`
	Ready   bool `json:"ready"`
	Valid   bool `json:"valid"`
}

// This is a private operator metadata record, not a backup of business rows.
type CatalogTagPrefixIndexBackup struct {
	Change     string                     `json:"change"`
	Database   string                     `json:"database"`
	Generation int                        `json:"generation"`
	PriorState CatalogTagPrefixIndexState `json:"prior_state"`
}

func InspectCatalogTagPrefixIndex(ctx context.Context, pool *pgxpool.Pool) (CatalogTagPrefixIndexState, error) {
	return inspectCatalogTagPrefixIndex(ctx, pool)
}

type catalogIndexQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func inspectCatalogTagPrefixIndex(ctx context.Context, db catalogIndexQuery) (CatalogTagPrefixIndexState, error) {
	var state CatalogTagPrefixIndexState
	err := db.QueryRow(ctx, `select true,coalesce(
		idx.relkind='i' and tbl.relname='catalog_tags' and table_ns.nspname='public'
		and access.amname='btree' and not i.indisunique and i.indnkeyatts=3 and i.indnatts=3
		and i.indpred is null and pg_get_expr(i.indexprs,i.indrelid,false)='lower(canonical_id)'
		and i.indkey[0]=0 and i.indkey[1]=registry.attnum and i.indkey[2]=entity.attnum
		and i.indclass[0]=(select oid from pg_opclass where opcnamespace='pg_catalog'::regnamespace and opcname='text_pattern_ops' and opcmethod=access.oid)
		and i.indclass[1]=(select oid from pg_opclass where opcnamespace='pg_catalog'::regnamespace and opcname='text_ops' and opcmethod=access.oid)
		and i.indclass[2]=(select oid from pg_opclass where opcnamespace='pg_catalog'::regnamespace and opcname='int8_ops' and opcmethod=access.oid)
		and i.indcollation[0]=canonical.attcollation and i.indcollation[1]=registry.attcollation and i.indcollation[2]=0
		and i.indoption[0]=0 and i.indoption[1]=0 and i.indoption[2]=0,false),
		coalesce(i.indisready,false),coalesce(i.indisvalid,false)
		from pg_class idx join pg_namespace index_ns on index_ns.oid=idx.relnamespace
		left join pg_index i on i.indexrelid=idx.oid
		left join pg_class tbl on tbl.oid=i.indrelid
		left join pg_namespace table_ns on table_ns.oid=tbl.relnamespace
		left join pg_am access on access.oid=idx.relam
		left join pg_attribute canonical on canonical.attrelid=tbl.oid and canonical.attname='canonical_id' and not canonical.attisdropped
		left join pg_attribute registry on registry.attrelid=tbl.oid and registry.attname='registry' and not registry.attisdropped
		left join pg_attribute entity on entity.attrelid=tbl.oid and entity.attname='entity_id' and not entity.attisdropped
		where index_ns.nspname='public' and idx.relname=$1`, CatalogTagPrefixIndexName).
		Scan(&state.Exists, &state.Matches, &state.Ready, &state.Valid)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("inspect tag prefix index: %w", err)
	}
	return state, nil
}

// ApplyCatalogTagPrefixIndex is an explicit additive change. PostgreSQL builds
// the index concurrently outside a transaction; interruption can leave an
// invalid index, so success requires a final definition/ready/valid inspection.
func ApplyCatalogTagPrefixIndex(ctx context.Context, pool *pgxpool.Pool, confirmedDatabase string, repairInvalid bool, persistMetadata func(CatalogTagPrefixIndexBackup) error) error {
	if confirmedDatabase == "" || persistMetadata == nil {
		return errors.New("tag prefix index changes require exact database confirmation and private metadata persistence")
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire tag prefix index connection: %w", err)
	}
	defer conn.Release()
	var priorSearchPath string
	if err := conn.QueryRow(ctx, `select current_setting('search_path')`).Scan(&priorSearchPath); err != nil {
		return fmt.Errorf("read index repair search path: %w", err)
	}
	if _, err := conn.Exec(ctx, `select set_config('search_path','pg_catalog',false)`); err != nil {
		return fmt.Errorf("secure index repair search path: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(cleanup, `select set_config('search_path',$1,false)`, priorSearchPath); err != nil {
			_ = conn.Conn().Close(cleanup)
		}
	}()
	if _, err := conn.Exec(ctx, `select pg_advisory_lock(hashtext('mcmods-cn-schema-migrations'))`); err != nil {
		// Cancellation can race the server obtaining a session lock before its
		// acknowledgement reaches the client. Discard the physical connection
		// even if resetting search_path succeeds; it may already own the lock.
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = conn.Conn().Close(cleanup)
		cancel()
		return fmt.Errorf("lock tag prefix index change: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Discard a connection if the session lock could not be released.
		if _, err := conn.Exec(cleanup, `select pg_advisory_unlock(hashtext('mcmods-cn-schema-migrations'))`); err != nil {
			_ = conn.Conn().Close(cleanup)
		}
	}()
	var actualDatabase string
	var generation int
	if err := conn.QueryRow(ctx, `select current_database(),generation from public.schema_metadata where singleton`).Scan(&actualDatabase, &generation); err != nil {
		return fmt.Errorf("read tag prefix index target: %w", err)
	}
	if actualDatabase != confirmedDatabase || generation != 168 {
		return errors.New("tag prefix index change requires the confirmed database and generation 168")
	}
	state, err := inspectCatalogTagPrefixIndex(ctx, conn)
	if err != nil {
		return err
	}
	if state.Exists {
		if !state.Matches {
			return errors.New("tag prefix index name has a different definition; no change applied")
		}
		if state.Ready && state.Valid {
			return nil
		}
		if !repairInvalid {
			return errors.New("tag prefix index is invalid or not ready; inspect failed build before explicit repair-invalid")
		}
		var building bool
		if err := conn.QueryRow(ctx, `select exists(select 1 from pg_stat_progress_create_index
			where index_relid=to_regclass('public.'||$1))`, CatalogTagPrefixIndexName).Scan(&building); err != nil {
			return fmt.Errorf("inspect active tag prefix build: %w", err)
		}
		if building {
			return errors.New("tag prefix index still has an active build; no change applied")
		}
	}
	if err := persistMetadata(CatalogTagPrefixIndexBackup{Change: "20261003_01", Database: actualDatabase, Generation: generation, PriorState: state}); err != nil {
		return fmt.Errorf("persist prior tag prefix index metadata: %w", err)
	}
	if state.Exists {
		if _, err := conn.Exec(ctx, `drop index concurrently public.idx_catalog_tags_canonical_prefix_registry`); err != nil {
			return fmt.Errorf("remove confirmed invalid tag prefix index: %w", err)
		}
	}
	if _, err := conn.Exec(ctx, `create index concurrently idx_catalog_tags_canonical_prefix_registry
		on public.catalog_tags(lower(canonical_id) text_pattern_ops,registry,entity_id)`); err != nil {
		return fmt.Errorf("build tag prefix index; inspect ready/valid before retry: %w", err)
	}
	state, err = inspectCatalogTagPrefixIndex(ctx, conn)
	if err != nil {
		return err
	}
	if !state.Exists || !state.Matches || !state.Ready || !state.Valid {
		return errors.New("tag prefix index build did not produce the reviewed valid index")
	}
	return nil
}
