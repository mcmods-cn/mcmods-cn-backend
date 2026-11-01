package progression

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	appconfig "mcmods-cn-backend/internal/config"
)

type auditT002ScopeIdentityRow struct{ scan func(...any) error }

func (row auditT002ScopeIdentityRow) Scan(values ...any) error { return row.scan(values...) }

type auditT002ScopeGuardAdmin struct {
	identity auditT002DatabaseIdentity
	exists   bool
	queryErr error
	sql      []string
	drops    int
}

func (admin *auditT002ScopeGuardAdmin) QueryRow(_ context.Context, statement string, _ ...any) pgx.Row {
	admin.sql = append(admin.sql, statement)
	return auditT002ScopeIdentityRow{scan: func(values ...any) error {
		if admin.queryErr != nil {
			return admin.queryErr
		}
		if !admin.exists {
			return pgx.ErrNoRows
		}
		*values[0].(*uint32) = admin.identity.OID
		*values[1].(*string) = admin.identity.Owner
		*values[2].(*string) = admin.identity.Marker
		return nil
	}}
}

func (admin *auditT002ScopeGuardAdmin) Exec(_ context.Context, statement string, _ ...any) (pgconn.CommandTag, error) {
	admin.sql = append(admin.sql, statement)
	if statement == "drop database "+(pgx.Identifier{admin.identity.Name}).Sanitize() {
		admin.drops++
		admin.exists = false
		return pgconn.NewCommandTag("DROP DATABASE"), nil
	}
	return pgconn.CommandTag{}, fmt.Errorf("unexpected cleanup statement")
}

var auditT002ScopeTestScope = auditT002DatabaseScope{Prefix: "audit_t002_progression_"}

func auditT002ScopeGuardIdentity() auditT002DatabaseIdentity {
	return auditT002DatabaseIdentity{Name: "audit_t002_progression_1234567890", OID: 12345, Owner: "owned_test_role", Marker: auditT002ScopeTestScope.markerPrefix() + strings.Repeat("a", 32)}
}

func TestAuditT002ScopedDatabaseCleanupIdentityMismatchHasZeroDropAndNoTermination(t *testing.T) {
	original := auditT002ScopeGuardIdentity()
	for _, field := range []string{"oid", "owner", "nonce", "missing", "query-failure", "unsafe-name", "unknown-oid", "unknown-owner", "invalid-nonce", "cross-scope-name", "cross-scope-marker"} {
		t.Run(field, func(t *testing.T) {
			expected, current := original, original
			admin := &auditT002ScopeGuardAdmin{identity: current, exists: true}
			switch field {
			case "oid":
				admin.identity.OID++
			case "owner":
				admin.identity.Owner = "another_task_owned_role"
			case "nonce":
				admin.identity.Marker = auditT002ScopeTestScope.markerPrefix() + strings.Repeat("b", 32)
			case "missing":
				admin.exists = false
			case "query-failure":
				admin.queryErr = &pgconn.PgError{Code: "XX001", Message: "owned metadata fault"}
			case "unsafe-name":
				expected.Name = "unrelated_database"
			case "unknown-oid":
				expected.OID = 0
			case "unknown-owner":
				expected.Owner = ""
			case "invalid-nonce":
				expected.Marker = "unverified"
			case "cross-scope-name":
				expected.Name = "another_task_1234567890"
			case "cross-scope-marker":
				expected.Marker = "mcmods-testdb-owned:another_task_:" + strings.Repeat("a", 32)
			}
			before, present := admin.identity, admin.exists
			if err := dropAuditT002OwnedDatabase(context.Background(), admin, auditT002ScopeTestScope, expected); !errors.Is(err, errAuditT002DatabaseOwnership) {
				t.Fatalf("ownership mismatch was not rejected precisely: %v", err)
			}
			if admin.drops != 0 || admin.identity != before || admin.exists != present {
				t.Fatal("mismatched identity changed or removed the substitute resource")
			}
			for _, statement := range admin.sql {
				lower := strings.ToLower(statement)
				if strings.Contains(lower, "drop database") || strings.Contains(lower, "force") || strings.Contains(lower, "terminate_backend") {
					t.Fatal("ownership rejection reached a destructive or termination statement")
				}
			}
		})
	}
	admin := &auditT002ScopeGuardAdmin{identity: original, exists: true}
	if err := dropAuditT002OwnedDatabase(context.Background(), admin, auditT002ScopeTestScope, original); err != nil || admin.drops != 1 || admin.exists || len(admin.sql) != 3 {
		t.Fatalf("ordinary exact-identity DROP and absence confirmation: drops=%d exists=%t sql=%d err=%v", admin.drops, admin.exists, len(admin.sql), err)
	}
}

type auditT002ScopeBlockingCloser struct{ started, release, finished chan struct{} }

func (closer auditT002ScopeBlockingCloser) Close() {
	close(closer.started)
	<-closer.release
	close(closer.finished)
}

func TestAuditT002ScopedOwnedPoolMustCloseBeforeAnyCleanupQuery(t *testing.T) {
	closer := auditT002ScopeBlockingCloser{make(chan struct{}), make(chan struct{}), make(chan struct{})}
	var once sync.Once
	release := func() { once.Do(func() { close(closer.release) }) }
	defer func() {
		release()
		select {
		case <-closer.finished:
		case <-time.After(2 * time.Second):
			t.Error("owned substitute closer did not finish after its release barrier")
		}
	}()
	admin := &auditT002ScopeGuardAdmin{identity: auditT002ScopeGuardIdentity(), exists: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- cleanupAuditT002OwnedDatabase(ctx, admin, auditT002ScopeTestScope, admin.identity, func(ctx context.Context) error { return closeAuditT002OwnedPool(ctx, closer) })
	}()
	select {
	case <-closer.started:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup did not reach its owned-pool close barrier")
	}
	cancel()
	var err error
	select {
	case err = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled owned-pool close did not return without database cleanup")
	}
	if !errors.Is(err, context.Canceled) || len(admin.sql) != 0 || admin.drops != 0 || !admin.exists {
		t.Fatalf("unclosed owned pool reached database cleanup: err=%v queries=%d drops=%d", err, len(admin.sql), admin.drops)
	}
	release()
	<-closer.finished
}

type auditT002ScopeRecordingAdmin struct {
	pool *pgxpool.Pool
	sql  []string
}

func (admin *auditT002ScopeRecordingAdmin) QueryRow(ctx context.Context, statement string, args ...any) pgx.Row {
	admin.sql = append(admin.sql, statement)
	return admin.pool.QueryRow(ctx, statement, args...)
}

func (admin *auditT002ScopeRecordingAdmin) Exec(ctx context.Context, statement string, args ...any) (pgconn.CommandTag, error) {
	admin.sql = append(admin.sql, statement)
	return admin.pool.Exec(ctx, statement, args...)
}

func TestAuditT002ScopedActualOwnedDatabaseRejectsMismatchesAndLiveConnectionWithoutForceIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 for actual task-owned database cleanup negatives")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(appconfig.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost" && cfg.ConnConfig.Host != "::1" {
		t.Fatal("owned cleanup regression requires an explicitly configured loopback test PostgreSQL server")
	}
	cfg.MaxConns, cfg.MinConns = 1, 0
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	admin := &auditT002ScopeRecordingAdmin{pool: pool}
	identity, err := createAuditT002OwnedDatabase(ctx, admin, auditT002ScopeTestScope)
	if err != nil {
		pool.Close()
		t.Fatalf("owned substitute creation failed: %v", err)
	}
	var held *pgx.Conn
	dropped := false
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		defer pool.Close()
		if held != nil {
			if err := held.Close(cleanup); err != nil {
				t.Errorf("close this test's explicitly held connection: %v", err)
				return
			}
		}
		if !dropped {
			if err := dropAuditT002OwnedDatabase(cleanup, admin, auditT002ScopeTestScope, identity); err != nil {
				t.Errorf("owned substitute remains for diagnosis: %v", err)
			}
		}
	})
	if actual, exists, err := readAuditT002DatabaseIdentity(ctx, admin, identity.Name); err != nil || !exists || actual != identity {
		t.Fatalf("original OID/owner/nonce were not recorded exactly: %v", err)
	}
	for _, field := range []string{"oid", "owner", "nonce"} {
		t.Run(field, func(t *testing.T) {
			wrong := identity
			switch field {
			case "oid":
				wrong.OID++
			case "owner":
				wrong.Owner += "_mismatch"
			case "nonce":
				wrong.Marker = auditT002ScopeTestScope.markerPrefix() + strings.Repeat("0", 32)
				if wrong.Marker == identity.Marker {
					wrong.Marker = auditT002ScopeTestScope.markerPrefix() + strings.Repeat("1", 32)
				}
			}
			admin.sql = nil
			if err := dropAuditT002OwnedDatabase(ctx, admin, auditT002ScopeTestScope, wrong); !errors.Is(err, errAuditT002DatabaseOwnership) {
				t.Fatalf("actual metadata mismatch returned an unrelated result: %v", err)
			}
			for _, statement := range admin.sql {
				if strings.Contains(strings.ToLower(statement), "drop database") {
					t.Fatal("mismatch issued a DROP")
				}
			}
			if actual, exists, err := readAuditT002DatabaseIdentity(ctx, admin, identity.Name); err != nil || !exists || actual != identity {
				t.Fatal("rejected cleanup changed the actual owned substitute")
			}
		})
	}
	connection := cfg.ConnConfig.Copy()
	connection.Database = identity.Name
	held, err = pgx.ConnectConfig(ctx, connection)
	if err != nil {
		t.Fatal(err)
	}
	var pid int32
	if err = held.QueryRow(ctx, `select pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if _, err = held.Exec(ctx, `create table owned_cleanup_sentinel(id int primary key,value text not null); insert into owned_cleanup_sentinel values(1,'owned exact bytes')`); err != nil {
		t.Fatal(err)
	}
	admin.sql = nil
	err = dropAuditT002OwnedDatabase(ctx, admin, auditT002ScopeTestScope, identity)
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "55006" {
		t.Fatalf("an actual live owned connection must cause exact object_in_use55006: %v", err)
	}
	for _, statement := range admin.sql {
		lower := strings.ToLower(statement)
		if strings.Contains(lower, "force") || strings.Contains(lower, "terminate_backend") || strings.Contains(lower, "cancel_backend") {
			t.Fatal("ordinary cleanup tried to signal a connection")
		}
	}
	var afterPID int32
	var value string
	if err = held.QueryRow(ctx, `select pg_backend_pid(),value from owned_cleanup_sentinel where id=1`).Scan(&afterPID, &value); err != nil || afterPID != pid || value != "owned exact bytes" {
		t.Fatal("failed ordinary DROP terminated or changed the held task-owned connection")
	}
	if actual, exists, err := readAuditT002DatabaseIdentity(ctx, admin, identity.Name); err != nil || !exists || !reflect.DeepEqual(actual, identity) {
		t.Fatal("failed ordinary DROP changed the original ownership fingerprint")
	}
	if err = held.Close(ctx); err != nil {
		t.Fatal(err)
	}
	held = nil
	if err = dropAuditT002OwnedDatabase(ctx, admin, auditT002ScopeTestScope, identity); err != nil {
		t.Fatal(err)
	}
	dropped = true
	t.Logf("T002 actual owned OID=%d owner=%s: three identity mismatches zeroDROP; live connection samePID/data after exact55006; only after its normal close ordinaryDROP+absence confirmed; no force/signals/foreign resource", identity.OID, identity.Owner)
}
