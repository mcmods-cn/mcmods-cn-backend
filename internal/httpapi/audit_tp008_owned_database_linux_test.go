package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

// An owned database keeps public-qualified OSS receipt proofs and all business
// tables in the same actual migrated namespace. No base/public data is edited.
func newAuditTP008OwnedDatabase(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
	t.Helper()
	if os.Getenv("MCMODS_AUDIT_PG_LOCK") == "" {
		t.Fatal("explicit task PostgreSQL DDL lock is required")
	}
	cfg, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "::1" {
		t.Fatal("TP008 refuses a non-loopback test database")
	}
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	name := "audit_tp008_native_" + hex.EncodeToString(nonce[:8])
	marker := "mcmods-tp008-owned:" + hex.EncodeToString(nonce[:])
	quoted := pgx.Identifier{name}.Sanitize()
	unlock := auditTP001CredentialDDLLock(t)
	_, err = admin.Exec(ctx, "create database "+quoted)
	if err == nil {
		_, err = admin.Exec(ctx, "comment on database "+quoted+" is '"+marker+"'")
	}
	unlock()
	if err != nil {
		admin.Close()
		t.Fatal(err)
	}
	var pool *pgxpool.Pool
	cleanup := func() {
		if pool != nil {
			pool.Close()
			pool = nil
		}
		defer admin.Close()
		cleanupCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		var owned bool
		if e := admin.QueryRow(cleanupCtx, `select datdba=(select oid from pg_roles where rolname=current_user) and shobj_description(oid,'pg_database')=$2 from pg_database where datname=$1`, name, marker).Scan(&owned); e != nil || !owned {
			t.Errorf("TP008 refuses database cleanup: owner/nonce not verified (%v)", e)
			return
		}
		unlock := auditTP001CredentialDDLLock(t)
		defer unlock()
		// Every owned connection must have closed. Never force-drop or terminate an
		// unknown connection to make cleanup appear successful.
		if _, e := admin.Exec(cleanupCtx, "drop database "+quoted); e != nil {
			t.Errorf("TP008 owned database cleanup failed: %v", e)
		}
	}
	scoped := cfg.Copy()
	scoped.ConnConfig.Database = name
	scoped.MaxConns, scoped.MinConns = 12, 1
	pool, err = pgxpool.NewWithConfig(ctx, scoped)
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	unlock = auditTP001CredentialDDLLock(t)
	err = database.Migrate(ctx, pool)
	unlock()
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	if err = database.RequireOSSUploadByteBindingSchema(ctx, pool); err != nil {
		cleanup()
		t.Fatal(err)
	}
	return pool, cleanup
}
