package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// IsolatedTestSchema owns one ordinary schema installed from exactly the
// application's migration statements. Unlike pg_temp, ordinary namespaces are
// searched for SQL functions and support tests with multiple connections.
// It never edits public and refuses cleanup unless both owner and nonce match.
type IsolatedTestSchema struct {
	Pool         *pgxpool.Pool
	owner        *pgxpool.Pool
	name, marker string
}

func NewIsolatedTestSchema(ctx context.Context, connectionString string, maxConnections int32) (*IsolatedTestSchema, error) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		return nil, errors.New("isolated schema requires MCMODS_RUN_DB_INTEGRATION=1")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	name := "mcmods_audit_" + hex.EncodeToString(nonce[:8])
	marker := "mcmods-owned-test:" + hex.EncodeToString(nonce[:])
	owner, err := pgxpool.New(ctx, connectionString)
	if err != nil {
		return nil, err
	}
	fixture := &IsolatedTestSchema{owner: owner, name: name, marker: marker}
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err = owner.Exec(ctx, "create schema "+quoted+" authorization current_user"); err != nil {
		owner.Close()
		return nil, err
	}
	// COMMENT cannot bind SQL parameters; the marker consists only of a fixed
	// prefix and cryptographically generated hexadecimal digits.
	if _, err = owner.Exec(ctx, "comment on schema "+quoted+" is '"+marker+"'"); err != nil {
		owner.Close()
		return nil, err
	}
	complete := false
	defer func() {
		if !complete {
			_ = fixture.Close(context.WithoutCancel(ctx))
		}
	}()
	config, err := pgxpool.ParseConfig(connectionString)
	if err != nil {
		return nil, err
	}
	config.MaxConns = maxConnections
	if config.MaxConns < 1 {
		config.MaxConns = 1
	}
	config.ConnConfig.RuntimeParams["search_path"] = quoted + ",pg_catalog"
	fixture.Pool, err = pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	tx, err := fixture.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	statements := schemaInstallationStatements()
	qualify := ephemeralSchemaFunctionQualifier(name, ephemeralSchemaFunctionNames(statements))
	for index, raw := range statements {
		statement := strings.ReplaceAll(raw, "on public.%I", "on %I")
		statement = strings.ReplaceAll(statement, "namespace_row.nspname='public'", "namespace_row.nspname='"+name+"'")
		statement = qualify(statement)
		if _, err = tx.Exec(ctx, statement); err != nil {
			_ = tx.Rollback(ctx)
			return nil, fmt.Errorf("install actual schema statement %d: %w", index, err)
		}
	}
	if _, err = tx.Exec(ctx, `create table schema_metadata(singleton boolean primary key default true check(singleton),generation integer not null,installed_at timestamptz not null default now());`); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `insert into schema_metadata(singleton,generation) values(true,$1)`, schemaGeneration); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	complete = true
	return fixture, nil
}

func (fixture *IsolatedTestSchema) Close(ctx context.Context) error {
	if fixture.Pool != nil {
		fixture.Pool.Close()
		fixture.Pool = nil
	}
	if fixture.owner == nil {
		return nil
	}
	defer func() { fixture.owner.Close(); fixture.owner = nil }()
	var owned bool
	if err := fixture.owner.QueryRow(ctx, `select nspowner=(select oid from pg_roles where rolname=current_user)
 and obj_description(oid,'pg_namespace')=$2 from pg_namespace where nspname=$1`, fixture.name, fixture.marker).Scan(&owned); err != nil {
		return err
	}
	if !owned {
		return errors.New("refusing schema cleanup: owner or marker mismatch")
	}
	_, err := fixture.owner.Exec(ctx, "drop schema "+pgx.Identifier{fixture.name}.Sanitize()+" cascade")
	return err
}
