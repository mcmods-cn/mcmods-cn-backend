package httpapi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

var errAuditMsgReviewTempIdentityChanged = errors.New("original notification TEMP identity changed; refuse DROP")
var errAuditMsgReviewTempConnectionUnavailable = errors.New("original sole idle TEMP connection unavailable; refuse reconnect or DROP")

type auditMsgReviewTempIdentity struct {
	BackendPID, SchemaOID, TableOID, TableNamespace, OwnerOID, RoleOID int64
	Persistence                                                        string
}

func (identity auditMsgReviewTempIdentity) valid() bool {
	return identity.BackendPID > 0 && identity.SchemaOID > 0 && identity.TableOID > 0 &&
		identity.TableNamespace == identity.SchemaOID && identity.OwnerOID > 0 &&
		identity.OwnerOID == identity.RoleOID && identity.Persistence == "t"
}

func readAuditMsgReviewTempIdentity(ctx context.Context, conn *pgxpool.Conn) (auditMsgReviewTempIdentity, error) {
	var identity auditMsgReviewTempIdentity
	err := conn.QueryRow(ctx, `select pg_catalog.pg_backend_pid()::bigint,
 pg_catalog.pg_my_temp_schema()::bigint,c.oid::bigint,c.relnamespace::bigint,
 c.relowner::bigint,pg_catalog.to_regrole(current_user::text)::oid::bigint,c.relpersistence::text
 from pg_catalog.pg_class c where c.oid=pg_catalog.to_regclass('pg_temp.notifications')`).Scan(
		&identity.BackendPID, &identity.SchemaOID, &identity.TableOID, &identity.TableNamespace,
		&identity.OwnerOID, &identity.RoleOID, &identity.Persistence)
	return identity, err
}

func captureAuditMsgReviewTempIdentity(ctx context.Context, pool *pgxpool.Pool) (auditMsgReviewTempIdentity, error) {
	conn, err := acquireAuditMsgReviewOriginalIdleConnection(ctx, pool)
	if err != nil {
		return auditMsgReviewTempIdentity{}, err
	}
	defer conn.Release()
	identity, err := readAuditMsgReviewTempIdentity(ctx, conn)
	if err != nil {
		return identity, err
	}
	if !identity.valid() {
		return identity, errAuditMsgReviewTempIdentityChanged
	}
	return identity, nil
}

func cleanupAuditMsgReviewTempTable(ctx context.Context, pool *pgxpool.Pool, original auditMsgReviewTempIdentity) error {
	if !original.valid() {
		return errAuditMsgReviewTempIdentityChanged
	}
	conn, err := acquireAuditMsgReviewOriginalIdleConnection(ctx, pool)
	if err != nil {
		return err
	}
	defer conn.Release()
	return dropAuditMsgReviewTempOnOriginalConnection(ctx, conn, original)
}

func acquireAuditMsgReviewOriginalIdleConnection(ctx context.Context, pool *pgxpool.Pool) (*pgxpool.Conn, error) {
	// The scale pool has exactly one connection. AcquireAllIdle never creates
	// a replacement connection; no idle original is an explicit cleanup error.
	idle := pool.AcquireAllIdle(ctx)
	if len(idle) != 1 {
		for _, conn := range idle {
			conn.Release()
		}
		return nil, errAuditMsgReviewTempConnectionUnavailable
	}
	return idle[0], nil
}

func dropAuditMsgReviewTempOnOriginalConnection(ctx context.Context, conn *pgxpool.Conn, original auditMsgReviewTempIdentity) error {
	if !original.valid() {
		return errAuditMsgReviewTempIdentityChanged
	}
	current, err := readAuditMsgReviewTempIdentity(ctx, conn)
	if errors.Is(err, pgx.ErrNoRows) {
		return errAuditMsgReviewTempIdentityChanged
	}
	if err != nil {
		return err
	}
	if !current.valid() || current != original {
		return errAuditMsgReviewTempIdentityChanged
	}
	// No IF EXISTS, CASCADE, reconnect, server termination or broad namespace
	// cleanup. The one held connection proves and drops only its original table.
	if _, err := conn.Exec(ctx, "drop table pg_temp.notifications"); err != nil {
		return fmt.Errorf("drop proved own notification TEMP: %w", err)
	}
	var pid, schema int64
	var absent bool
	if err := conn.QueryRow(ctx, `select pg_catalog.pg_backend_pid()::bigint,
 pg_catalog.pg_my_temp_schema()::bigint,pg_catalog.to_regclass('pg_temp.notifications') is null`).Scan(&pid, &schema, &absent); err != nil {
		return err
	}
	if pid != original.BackendPID || schema != original.SchemaOID || !absent {
		return errors.New("own notification TEMP absence not confirmed on original backend")
	}
	return nil
}

type auditMsgReviewTempSQLTrace struct{ drops atomic.Int64 }

func (trace *auditMsgReviewTempSQLTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(data.SQL)), "drop ") {
		trace.drops.Add(1)
	}
	return ctx
}

func (*auditMsgReviewTempSQLTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func newAuditMsgReviewTempCleanupConnection(t *testing.T, ctx context.Context) (*pgxpool.Pool, *pgxpool.Conn, *auditMsgReviewTempSQLTrace) {
	t.Helper()
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 for own TEMP cleanup PostgreSQL regression")
	}
	cfg, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	trace := &auditMsgReviewTempSQLTrace{}
	cfg.MaxConns, cfg.MinConns, cfg.ConnConfig.Tracer = 1, 1, trace
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		pool.Close()
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `create temporary table notifications(id bigint primary key,body text not null);
 insert into notifications values(1,'own exact notification');
 create temporary table audit_msg_review_keep(id bigint primary key,body text not null);
 insert into audit_msg_review_keep values(1,'other own TEMP is retained')`); err != nil {
		conn.Release()
		pool.Close()
		t.Fatal(err)
	}
	return pool, conn, trace
}

func TestAuditMsgReviewTempCleanupRejectsChangedIdentityWithoutDropIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, conn, trace := newAuditMsgReviewTempCleanupConnection(t, ctx)
	defer pool.Close()
	defer conn.Release()
	original, err := readAuditMsgReviewTempIdentity(ctx, conn)
	if err != nil || !original.valid() {
		t.Fatalf("original own TEMP proof: %+v/%v", original, err)
	}
	t.Run("busy_original_no_reconnect", func(t *testing.T) {
		before := pool.Stat().NewConnsCount()
		if err := cleanupAuditMsgReviewTempTable(ctx, pool, original); !errors.Is(err, errAuditMsgReviewTempConnectionUnavailable) {
			t.Fatalf("specific original-connection error not returned: %v", err)
		}
		if trace.drops.Load() != 0 || pool.Stat().NewConnsCount() != before {
			t.Fatal("busy original caused a DROP or a new connection")
		}
	})
	for _, field := range []string{"backend", "schema", "table", "owner", "persistence"} {
		t.Run(field, func(t *testing.T) {
			changed := original
			switch field {
			case "backend":
				changed.BackendPID++
			case "schema":
				changed.SchemaOID++
				changed.TableNamespace = changed.SchemaOID
			case "table":
				changed.TableOID++
			case "owner":
				changed.OwnerOID++
				changed.RoleOID = changed.OwnerOID
			case "persistence":
				changed.Persistence = "p"
			}
			before := trace.drops.Load()
			if err := dropAuditMsgReviewTempOnOriginalConnection(ctx, conn, changed); !errors.Is(err, errAuditMsgReviewTempIdentityChanged) {
				t.Fatalf("specific identity mismatch not returned: %v", err)
			}
			current, err := readAuditMsgReviewTempIdentity(ctx, conn)
			var body string
			if scanErr := conn.QueryRow(ctx, `select body from pg_temp.notifications where id=1`).Scan(&body); scanErr != nil {
				t.Fatal(scanErr)
			}
			if err != nil || current != original || body != "own exact notification" || trace.drops.Load() != before {
				t.Fatalf("mismatch changed own TEMP or sent DROP: proof=%+v/%v body=%q drops=%d/%d", current, err, body, trace.drops.Load(), before)
			}
		})
	}
	if err := dropAuditMsgReviewTempOnOriginalConnection(ctx, conn, original); err != nil {
		t.Fatal(err)
	}
}

func TestAuditMsgReviewTempCleanupRefusesDependencyWithoutCascadeIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, conn, trace := newAuditMsgReviewTempCleanupConnection(t, ctx)
	defer pool.Close()
	defer conn.Release()
	original, err := readAuditMsgReviewTempIdentity(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `create temporary view audit_msg_review_dependency as select id from pg_temp.notifications`); err != nil {
		t.Fatal(err)
	}
	err = dropAuditMsgReviewTempOnOriginalConnection(ctx, conn, original)
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) || pgError.Code != "2BP01" || trace.drops.Load() != 1 {
		t.Fatalf("ordinary DROP must preserve dependency: err=%v/dropAttempts=%d", err, trace.drops.Load())
	}
	current, err := readAuditMsgReviewTempIdentity(ctx, conn)
	var rows int64
	if countErr := conn.QueryRow(ctx, `select count(*) from pg_temp.audit_msg_review_dependency`).Scan(&rows); countErr != nil {
		t.Fatal(countErr)
	}
	if err != nil || current != original || rows != 1 {
		t.Fatalf("failed ordinary DROP changed dependent scope: %+v/%v rows=%d", current, err, rows)
	}
	if _, err := conn.Exec(ctx, `drop view pg_temp.audit_msg_review_dependency`); err != nil {
		t.Fatal(err)
	}
	if err := dropAuditMsgReviewTempOnOriginalConnection(ctx, conn, original); err != nil {
		t.Fatal(err)
	}
}

func TestAuditMsgReviewTempCleanupDropsOwnedTempThenFreshSchemaIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, conn, trace := newAuditMsgReviewTempCleanupConnection(t, ctx)
	defer func() {
		if conn != nil {
			conn.Release()
		}
		pool.Close()
	}()
	original, err := readAuditMsgReviewTempIdentity(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	conn.Release()
	conn = nil
	captured, err := captureAuditMsgReviewTempIdentity(ctx, pool)
	if err != nil || captured != original {
		t.Fatalf("original idle capture changed identity: %+v/%v", captured, err)
	}
	if err := cleanupAuditMsgReviewTempTable(ctx, pool, original); err != nil {
		t.Fatal(err)
	}
	conn, err = acquireAuditMsgReviewOriginalIdleConnection(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	var body string
	if err := conn.QueryRow(ctx, `select body from pg_temp.audit_msg_review_keep where id=1`).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if body != "other own TEMP is retained" || trace.drops.Load() != 1 {
		t.Fatalf("cleanup changed other own TEMP or wrong DROP count: %q/%d", body, trace.drops.Load())
	}
	var installedGeneration int
	if err := conn.QueryRow(ctx, `select generation from public.schema_metadata where singleton`).Scan(&installedGeneration); err != nil {
		t.Fatalf("read the current installed baseline: %v", err)
	}
	conn.Release()
	conn = nil
	pool.Close()
	fresh, err := database.NewIsolatedTestSchema(ctx, config.Load().DB.ConnString(), 1)
	if err != nil {
		t.Fatalf("next actual migrated schema after synchronous own TEMP drop: %v", err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := fresh.Close(cleanup); err != nil {
			t.Errorf("next owned schema cleanup: %v", err)
		}
	}()
	var generation int
	if err := fresh.Pool.QueryRow(ctx, `select generation from schema_metadata where singleton`).Scan(&generation); err != nil || generation != installedGeneration {
		t.Fatalf("next actual schema generation=%d/%v", generation, err)
	}
}
