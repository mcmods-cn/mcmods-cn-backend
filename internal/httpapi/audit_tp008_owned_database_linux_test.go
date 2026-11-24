package httpapi

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/tools/testing/postgrestarget"
)

// An owned database keeps public-qualified OSS receipt proofs and all business
// tables in the same actual migrated namespace. No base/public data is edited.
func newAuditTP008OwnedDatabase(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
	t.Helper()
	if os.Getenv("MCMODS_AUDIT_PG_LOCK") == "" {
		t.Fatal("explicit task PostgreSQL DDL lock is required")
	}
	cfg, err := postgrestarget.ParseSingleLoopback(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	scope := auditT002DatabaseScope{Prefix: "audit_tp008_native_"}
	unlock := auditTP001CredentialDDLLock(t)
	identity, err := createAuditT002OwnedDatabase(ctx, admin, scope)
	unlock()
	if err != nil {
		admin.Close()
		t.Fatalf("TP008 child creation could not confirm its identity: %v", err)
	}
	var pool *pgxpool.Pool
	cleanup := func() {
		defer admin.Close()
		cleanupCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if pool != nil {
			if err := closeAuditT002OwnedPool(cleanupCtx, pool); err != nil {
				t.Errorf("TP008 owned pool did not close: %v", err)
				return
			}
			pool = nil
		}
		unlock := auditTP001CredentialDDLLock(t)
		defer unlock()
		if err := dropAuditT002OwnedDatabase(cleanupCtx, admin, scope, identity); err != nil {
			t.Errorf("TP008 owned database cleanup could not be confirmed: %v", err)
		}
	}
	scoped := cfg.Copy()
	scoped.ConnConfig.Database = identity.Name
	scoped.MaxConns, scoped.MinConns = 12, 1
	pool, err = pgxpool.NewWithConfig(ctx, scoped)
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	unlock = auditTP001CredentialDDLLock(t)
	err = database.Initialize(ctx, pool)
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
