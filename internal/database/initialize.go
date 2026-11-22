package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const schemaGeneration = 169

const applicationSearchPath = "public, pg_temp"

const schemaMetadataDDL = `create table schema_metadata (
	singleton boolean primary key default true check(singleton),
	generation integer not null check(generation>0),
	definition_fingerprint text not null check(definition_fingerprint ~ '^[a-f0-9]{64}$'),
	catalog_fingerprint text not null check(catalog_fingerprint ~ '^[a-f0-9]{64}$'),
	admin_bootstrapped boolean not null,
	installed_at timestamptz not null default now()
)`

// Initialize installs the current schema and its first-boot seed data together.
// An existing installation is checked without rewriting its settings or grants.
func Initialize(ctx context.Context, db *pgxpool.Pool) error {
	return initialize(ctx, db, false)
}

// BootstrapAdmin is an explicit operator action available only until this
// installation's initial administrator has been created once.
func BootstrapAdmin(ctx context.Context, db *pgxpool.Pool) error {
	return initialize(ctx, db, true)
}

func initialize(ctx context.Context, db *pgxpool.Pool, bootstrapAdmin bool) error {
	// Each waiting installer must see the preceding commit after the lock is
	// acquired, even if the database has a stricter default isolation level.
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin database initialization: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `select set_config('search_path',$1,true)`, applicationSearchPath); err != nil {
		return fmt.Errorf("select application schema: %w", err)
	}
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('mcmods-cn-schema-migrations'))`); err != nil {
		return fmt.Errorf("lock database initialization: %w", err)
	}

	var hasMetadata bool
	if err = tx.QueryRow(ctx, `select to_regclass('public.schema_metadata') is not null`).Scan(&hasMetadata); err != nil {
		return fmt.Errorf("inspect database schema: %w", err)
	}
	if hasMetadata {
		if err = checkInstalledSchema(ctx, tx); err != nil {
			return err
		}
		if bootstrapAdmin {
			if err = claimInitialAdmin(ctx, tx); err != nil {
				return err
			}
		}
	} else if err = installEmptySchema(ctx, tx, bootstrapAdmin); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit database initialization: %w", err)
	}
	return nil
}

func checkInstalledSchema(ctx context.Context, tx pgx.Tx) error {
	recorded, err := installedCatalogFingerprint(ctx, tx)
	if err != nil {
		return err
	}
	current, err := catalogFingerprint(ctx, tx, "public")
	if err != nil {
		return err
	}
	if current != recorded {
		return errors.New("database catalog differs from its installed baseline")
	}
	return nil
}

// Repair commands can verify the installed definition before looking at the
// database objects they are meant to restore.
func installedCatalogFingerprint(ctx context.Context, db schemaQueryer) (string, error) {
	var generation int
	err := db.QueryRow(ctx, `select generation from public.schema_metadata
		where singleton and (select count(*) from public.schema_metadata)=1`).Scan(&generation)
	if err != nil {
		return "", fmt.Errorf("read installed schema version: %w", err)
	}
	if generation != schemaGeneration {
		return "", fmt.Errorf("database schema generation %d is incompatible with generation %d", generation, schemaGeneration)
	}
	var definition, recorded string
	if err = db.QueryRow(ctx, `select definition_fingerprint,catalog_fingerprint from public.schema_metadata where singleton`).Scan(&definition, &recorded); err != nil {
		return "", fmt.Errorf("read installed schema fingerprint: %w", err)
	}
	if definition != definitionFingerprint(schemaInstallationStatements()) {
		return "", errors.New("installed schema definition differs from the application's baseline")
	}
	return recorded, nil
}

func claimInitialAdmin(ctx context.Context, tx pgx.Tx) error {
	claimed, err := tx.Exec(ctx, `update public.schema_metadata set admin_bootstrapped=true where singleton and not admin_bootstrapped`)
	if err != nil {
		return fmt.Errorf("claim initial administrator: %w", err)
	}
	if claimed.RowsAffected() != 1 {
		return errors.New("the initial administrator has already been bootstrapped")
	}
	return seedBootstrapAdmin(ctx, tx)
}

func installEmptySchema(ctx context.Context, tx pgx.Tx, bootstrapAdmin bool) error {
	var hasPublic, hasObjects bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from pg_namespace where nspname='public'), exists(
		select 1 from pg_class where relnamespace=to_regnamespace('public')
		union all select 1 from pg_proc where pronamespace=to_regnamespace('public')
		union all select 1 from pg_type where typnamespace=to_regnamespace('public')
		union all select 1 from pg_extension where extnamespace=to_regnamespace('public')
	)`).Scan(&hasPublic, &hasObjects); err != nil {
		return fmt.Errorf("inspect blank application schema: %w", err)
	}
	if !hasPublic || hasObjects {
		return errors.New("database initialization requires an empty public schema or a recognized installation")
	}
	statements := schemaInstallationStatements()
	for index, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("install schema generation %d statement %d: %w", schemaGeneration, index+1, err)
		}
	}
	if err := seedNewDatabase(ctx, tx); err != nil {
		return fmt.Errorf("seed new database: %w", err)
	}
	if bootstrapAdmin {
		if err := seedBootstrapAdmin(ctx, tx); err != nil {
			return fmt.Errorf("seed initial administrator: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, schemaMetadataDDL); err != nil {
		return fmt.Errorf("create schema metadata: %w", err)
	}
	fingerprint, err := catalogFingerprint(ctx, tx, "public")
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `insert into schema_metadata(singleton,generation,definition_fingerprint,catalog_fingerprint,admin_bootstrapped)
		values(true,$1,$2,$3,$4)`, schemaGeneration, definitionFingerprint(statements), fingerprint, bootstrapAdmin); err != nil {
		return fmt.Errorf("record installed schema: %w", err)
	}
	return nil
}
