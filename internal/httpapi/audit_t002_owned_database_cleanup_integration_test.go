package httpapi

import (
	"context"
	"errors"
	"fmt"
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

type test018IdentityRow struct{ scan func(...any) error }

func (row test018IdentityRow) Scan(values ...any) error { return row.scan(values...) }

type test018GuardAdmin struct {
	identity test018DatabaseIdentity
	exists   bool
	queryErr error
	sql      []string
	drops    int
}

func (admin *test018GuardAdmin) QueryRow(_ context.Context, statement string, _ ...any) pgx.Row {
	admin.sql = append(admin.sql, statement)
	return test018IdentityRow{scan: func(values ...any) error {
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

func (admin *test018GuardAdmin) Exec(_ context.Context, statement string, _ ...any) (pgconn.CommandTag, error) {
	admin.sql = append(admin.sql, statement)
	if statement == "drop database "+(pgx.Identifier{admin.identity.Name}).Sanitize() {
		admin.drops++
		admin.exists = false
		return pgconn.NewCommandTag("DROP DATABASE"), nil
	}
	return pgconn.CommandTag{}, fmt.Errorf("unexpected cleanup statement")
}

func test018GuardIdentity() test018DatabaseIdentity {
	return test018DatabaseIdentity{Name: "test018_project_update_1234567890", OID: 12345, Owner: "owned_test_role", Marker: "mcmods-test018-owned:" + strings.Repeat("a", 32)}
}

func TestAuditT002DatabaseCleanupIdentityMismatchHasZeroDropAndNoTermination(t *testing.T) {
	original := test018GuardIdentity()
	for _, field := range []string{"oid", "owner", "nonce", "missing", "query-failure", "unsafe-name", "unknown-oid", "unknown-owner", "invalid-nonce"} {
		t.Run(field, func(t *testing.T) {
			expected, current := original, original
			admin := &test018GuardAdmin{identity: current, exists: true}
			switch field {
			case "oid":
				admin.identity.OID++
			case "owner":
				admin.identity.Owner = "another_task_owned_role"
			case "nonce":
				admin.identity.Marker = "mcmods-test018-owned:" + strings.Repeat("b", 32)
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
			}
			before, present := admin.identity, admin.exists
			if err := dropTEST018OwnedDatabase(context.Background(), admin, expected); !errors.Is(err, errTEST018DatabaseOwnership) {
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
	admin := &test018GuardAdmin{identity: original, exists: true}
	if err := dropTEST018OwnedDatabase(context.Background(), admin, original); err != nil || admin.drops != 1 || admin.exists || len(admin.sql) != 3 {
		t.Fatalf("ordinary exact-identity DROP and absence confirmation: drops=%d exists=%t sql=%d err=%v", admin.drops, admin.exists, len(admin.sql), err)
	}
}

type test018BlockingCloser struct{ started, release, finished chan struct{} }

func (closer test018BlockingCloser) Close() {
	close(closer.started)
	<-closer.release
	close(closer.finished)
}

func TestAuditT002OwnedPoolMustCloseBeforeAnyCleanupQuery(t *testing.T) {
	closer := test018BlockingCloser{make(chan struct{}), make(chan struct{}), make(chan struct{})}
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
	admin := &test018GuardAdmin{identity: test018GuardIdentity(), exists: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- cleanupTEST018OwnedDatabase(ctx, admin, admin.identity, func(ctx context.Context) error { return closeTEST018OwnedPool(ctx, closer) })
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

type test018RecordingAdmin struct {
	pool *pgxpool.Pool
	sql  []string
}

func (admin *test018RecordingAdmin) QueryRow(ctx context.Context, statement string, args ...any) pgx.Row {
	admin.sql = append(admin.sql, statement)
	return admin.pool.QueryRow(ctx, statement, args...)
}

func (admin *test018RecordingAdmin) Exec(ctx context.Context, statement string, args ...any) (pgconn.CommandTag, error) {
	admin.sql = append(admin.sql, statement)
	return admin.pool.Exec(ctx, statement, args...)
}

func TestAuditT002ActualOwnedDatabaseRejectsMismatchesAndLiveConnectionWithoutForceIntegration(t *testing.T) {
	requireOCT02DatabaseIntegration(t)
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
	admin := &test018RecordingAdmin{pool: pool}
	identity, err := createTEST018OwnedDatabase(ctx, admin)
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
			if err := dropTEST018OwnedDatabase(cleanup, admin, identity); err != nil {
				t.Errorf("owned substitute remains for diagnosis: %v", err)
			}
		}
	})
	if actual, exists, err := readTEST018DatabaseIdentity(ctx, admin, identity.Name); err != nil || !exists || actual != identity {
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
				wrong.Marker = "mcmods-test018-owned:" + strings.Repeat("0", 32)
				if wrong.Marker == identity.Marker {
					wrong.Marker = "mcmods-test018-owned:" + strings.Repeat("1", 32)
				}
			}
			admin.sql = nil
			if err := dropTEST018OwnedDatabase(ctx, admin, wrong); !errors.Is(err, errTEST018DatabaseOwnership) {
				t.Fatalf("actual metadata mismatch returned an unrelated result: %v", err)
			}
			for _, statement := range admin.sql {
				if strings.Contains(strings.ToLower(statement), "drop database") {
					t.Fatal("mismatch issued a DROP")
				}
			}
			if actual, exists, err := readTEST018DatabaseIdentity(ctx, admin, identity.Name); err != nil || !exists || actual != identity {
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
	err = dropTEST018OwnedDatabase(ctx, admin, identity)
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
	if actual, exists, err := readTEST018DatabaseIdentity(ctx, admin, identity.Name); err != nil || !exists || !reflect.DeepEqual(actual, identity) {
		t.Fatal("failed ordinary DROP changed the original ownership fingerprint")
	}
	if err = held.Close(ctx); err != nil {
		t.Fatal(err)
	}
	held = nil
	if err = dropTEST018OwnedDatabase(ctx, admin, identity); err != nil {
		t.Fatal(err)
	}
	dropped = true
	t.Logf("T002 actual owned OID=%d owner=%s: three identity mismatches zeroDROP; live connection samePID/data after exact55006; only after its normal close ordinaryDROP+absence confirmed; no force/signals/foreign resource", identity.OID, identity.Owner)
}
