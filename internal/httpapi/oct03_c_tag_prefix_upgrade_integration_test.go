package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/database"
)

func oct03CRequirePrefixIndex(t *testing.T, f test013Fixture, present bool) {
	t.Helper()
	state, err := database.InspectCatalogTagPrefixIndex(f.ctx, f.db)
	if err != nil || state.Exists != present || (present && (!state.Matches || !state.Ready || !state.Valid)) {
		t.Fatalf("tag prefix index present=%v state=%+v err=%v", present, state, err)
	}
}

func TestOCT03CTagPrefixUpgradePreservesOldDataAndIndexesIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	oct03CRequirePrefixIndex(t, f, true)
	if _, err := f.db.Exec(f.ctx, `with entity as (
		insert into catalog_entities(identity_key,entity_type,status) values('oct03-upgrade:tag','tag','active') returning id)
		insert into catalog_tags(entity_id,registry,canonical_id) select id,'minecraft:item','oct03:preserved' from entity`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `drop index concurrently public.idx_catalog_tags_canonical_prefix_registry`); err != nil {
		t.Fatal(err)
	}
	// Existing generation 168 remains runnable, but startup is not a migration
	// that silently applies this new concurrent index to an installed database.
	if err := database.Migrate(f.ctx, f.db); err != nil {
		t.Fatal(err)
	}
	oct03CRequirePrefixIndex(t, f, false)
	name := f.db.Config().ConnConfig.Database
	persisted := 0
	persist := func(metadata database.CatalogTagPrefixIndexBackup) error {
		if metadata.Change != "20261003_01" || metadata.Database != name || metadata.Generation != 168 || metadata.PriorState.Exists {
			t.Fatalf("unexpected prior metadata: %+v", metadata)
		}
		persisted++
		return nil
	}
	if err := database.ApplyCatalogTagPrefixIndex(f.ctx, f.db, name+"_wrong", false, persist); err == nil || persisted != 0 {
		t.Fatalf("wrong target mutated metadata: err=%v persisted=%d", err, persisted)
	}
	if err := database.ApplyCatalogTagPrefixIndex(f.ctx, f.db, name, false, func(database.CatalogTagPrefixIndexBackup) error {
		return errors.New("synthetic metadata persistence failure")
	}); err == nil {
		t.Fatal("metadata failure was ignored")
	}
	oct03CRequirePrefixIndex(t, f, false)
	if err := database.ApplyCatalogTagPrefixIndex(f.ctx, f.db, name, false, persist); err != nil {
		t.Fatal(err)
	}
	oct03CRequirePrefixIndex(t, f, true)
	var beforeOID, afterOID uint32
	if err := f.db.QueryRow(f.ctx, `select 'public.idx_catalog_tags_canonical_prefix_registry'::regclass::oid`).Scan(&beforeOID); err != nil {
		t.Fatal(err)
	}
	if err := database.ApplyCatalogTagPrefixIndex(f.ctx, f.db, name, false, persist); err != nil {
		t.Fatal(err)
	}
	var rows, generation, retained int
	if err := f.db.QueryRow(f.ctx, `select 'public.idx_catalog_tags_canonical_prefix_registry'::regclass::oid,
		(select count(*) from catalog_tags where canonical_id='oct03:preserved'),
		(select generation from schema_metadata where singleton),
		(select count(*) from pg_index where indrelid='public.catalog_tags'::regclass
		 and (indexrelid='public.idx_catalog_tags_canonical_registry_folded'::regclass or indisunique))`).Scan(&afterOID, &rows, &generation, &retained); err != nil {
		t.Fatal(err)
	}
	if persisted != 1 || beforeOID != afterOID || rows != 1 || generation != 168 || retained < 2 {
		t.Fatalf("repeat altered installation: metadata=%d sameOID=%v rows=%d generation=%d retained=%d", persisted, beforeOID == afterOID, rows, generation, retained)
	}
	// Generation mismatch must refuse even a valid named index, with no writes.
	if _, err := f.db.Exec(f.ctx, `update schema_metadata set generation=167 where singleton`); err != nil {
		t.Fatal(err)
	}
	if err := database.ApplyCatalogTagPrefixIndex(f.ctx, f.db, name, false, persist); err == nil || persisted != 1 {
		t.Fatalf("wrong generation was accepted: err=%v metadata=%d", err, persisted)
	}
}

func TestOCT03CTagPrefixUpgradeRefusesDriftAndRecoversInterruptedBuildIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	name := f.db.Config().ConnConfig.Database
	if _, err := f.db.Exec(f.ctx, `drop index concurrently public.idx_catalog_tags_canonical_prefix_registry`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `create index idx_catalog_tags_canonical_prefix_registry on catalog_tags(canonical_id)`); err != nil {
		t.Fatal(err)
	}
	persisted := 0
	persist := func(database.CatalogTagPrefixIndexBackup) error { persisted++; return nil }
	if err := database.ApplyCatalogTagPrefixIndex(f.ctx, f.db, name, true, persist); err == nil || persisted != 0 {
		t.Fatalf("definition drift was overwritten: err=%v persisted=%d", err, persisted)
	}
	if _, err := f.db.Exec(f.ctx, `drop index concurrently public.idx_catalog_tags_canonical_prefix_registry`); err != nil {
		t.Fatal(err)
	}
	writer, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback(context.Background())
	if _, err := writer.Exec(f.ctx, `with entity as (
		insert into catalog_entities(identity_key,entity_type,status) values('oct03-invalid:tag','tag','active') returning id)
		insert into catalog_tags(entity_id,registry,canonical_id) select id,'minecraft:item','oct03:blocked' from entity`); err != nil {
		t.Fatal(err)
	}
	buildCtx, stopBuild := context.WithCancel(f.ctx)
	defer stopBuild()
	buildDone := make(chan error, 1)
	go func() {
		_, err := f.db.Exec(buildCtx, `create index concurrently idx_catalog_tags_canonical_prefix_registry
			on public.catalog_tags(lower(canonical_id) text_pattern_ops,registry,entity_id)`)
		buildDone <- err
	}()
	// Hold an actual writer until PostgreSQL exposes the active concurrent
	// build. No catalog flags are manually forged to simulate invalidity.
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var active bool
		if err := f.db.QueryRow(f.ctx, `select exists(select 1 from pg_stat_progress_create_index
			where index_relid=to_regclass('public.idx_catalog_tags_canonical_prefix_registry'))`).Scan(&active); err != nil {
			t.Fatal(err)
		}
		if active {
			break
		}
		select {
		case err := <-buildDone:
			t.Fatalf("concurrent build terminated before the writer barrier: %v", err)
		case <-deadline.C:
			t.Fatal("concurrent build did not reach its writer barrier")
		case <-tick.C:
		}
	}
	if err := database.ApplyCatalogTagPrefixIndex(f.ctx, f.db, name, true, persist); err == nil || !strings.Contains(err.Error(), "active build") || persisted != 0 {
		t.Fatalf("active build was not protected: err=%v metadata=%d", err, persisted)
	}
	stopBuild()
	if err := <-buildDone; err == nil {
		t.Fatal("controlled build cancellation unexpectedly succeeded")
	}
	// pgx returning context cancellation does not prove the PostgreSQL backend
	// has processed its cancellation yet. Keep the writer locked until the
	// active build has disappeared, or releasing it could finish the index
	// before the cancellation reaches the server.
	cancelDeadline := time.NewTimer(5 * time.Second)
	defer cancelDeadline.Stop()
	for {
		var active bool
		if err := f.db.QueryRow(f.ctx, `select exists(select 1 from pg_stat_progress_create_index
			where index_relid=to_regclass('public.idx_catalog_tags_canonical_prefix_registry'))`).Scan(&active); err != nil {
			t.Fatal(err)
		}
		if !active {
			break
		}
		select {
		case <-cancelDeadline.C:
			t.Fatal("PostgreSQL did not observe the controlled build cancellation")
		case <-tick.C:
		}
	}
	if err := writer.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	state, err := database.InspectCatalogTagPrefixIndex(f.ctx, f.db)
	if err != nil || !state.Exists || !state.Matches || state.Valid {
		t.Fatalf("cancelled PostgreSQL build did not leave the expected invalid artifact: %+v err=%v", state, err)
	}
	if err := database.ApplyCatalogTagPrefixIndex(f.ctx, f.db, name, false, persist); err == nil || persisted != 0 {
		t.Fatalf("invalid artifact was silently repaired: err=%v metadata=%d", err, persisted)
	}
	if err := database.ApplyCatalogTagPrefixIndex(f.ctx, f.db, name, true, persist); err != nil {
		t.Fatal(err)
	}
	oct03CRequirePrefixIndex(t, f, true)
	if persisted != 1 {
		t.Fatalf("explicit repair metadata=%d want1", persisted)
	}
}

func TestOCT03CTagPrefixUpgradeCLIUsesExplicitTargetAndPrivateMetadataIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	name := f.db.Config().ConnConfig.Database
	binary := filepath.Join(t.TempDir(), "db-index-repair")
	// ConnString preserves the original parsed text, while this fixture changes
	// Config.Database to its nonce child. Build the explicit owned child target
	// from the effective fields instead of accidentally handing the parent URL
	// to the CLI's exact-database guard.
	connection := f.db.Config().ConnConfig
	if connection.TLSConfig != nil || (connection.Host != "127.0.0.1" && connection.Host != "localhost" && connection.Host != "::1") {
		t.Fatal("CLI fixture requires its own non-TLS loopback child database")
	}
	childURL := (&url.URL{Scheme: "postgres", Host: net.JoinHostPort(connection.Host, strconv.Itoa(int(connection.Port))),
		User: url.UserPassword(connection.User, connection.Password), Path: "/" + name, RawQuery: "sslmode=disable"}).String()
	build := exec.CommandContext(f.ctx, "go", "build", "-o", binary, "../../cmd/db-index-repair")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build reviewed CLI: %v: %s", err, output)
	}
	run := func(withURL bool, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(f.ctx, binary, args...)
		for _, variable := range os.Environ() {
			if !strings.HasPrefix(variable, "MCMODS_INDEX_REPAIR_DATABASE_URL=") {
				cmd.Env = append(cmd.Env, variable)
			}
		}
		if withURL {
			cmd.Env = append(cmd.Env, "MCMODS_INDEX_REPAIR_DATABASE_URL="+childURL)
		}
		return cmd.CombinedOutput()
	}
	if output, err := run(false); err == nil || !strings.Contains(string(output), "explicit MCMODS_INDEX_REPAIR_DATABASE_URL is required") {
		t.Fatalf("CLI used implicit deployment configuration: err=%v", err)
	}
	backup := filepath.Join(t.TempDir(), "prior-metadata.json")
	if _, err := run(true, "-apply", "-confirm-database", name+"_wrong", "-backup", backup); err == nil {
		t.Fatal("CLI accepted a different configured database")
	}
	if _, err := os.Stat(backup); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("wrong target wrote metadata: %v", err)
	}
	if _, err := f.db.Exec(f.ctx, `drop index concurrently public.idx_catalog_tags_canonical_prefix_registry`); err != nil {
		t.Fatal(err)
	}
	if output, err := run(true); err != nil || strings.TrimSpace(string(output)) != `{"exists":false,"matches":false,"ready":false,"valid":false}` {
		t.Fatalf("CLI inspection failed: err=%v output=%s", err, output)
	}
	if output, err := run(true, "-apply", "-confirm-database", name, "-backup", backup); err != nil {
		t.Fatalf("CLI exact-target apply failed: %v output=%s", err, output)
	}
	stat, err := os.Stat(backup)
	if err != nil || stat.Mode().Perm() != 0600 {
		t.Fatalf("metadata is not private: stat=%v err=%v", stat, err)
	}
	raw, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	var metadata database.CatalogTagPrefixIndexBackup
	if err := json.Unmarshal(raw, &metadata); err != nil || metadata.Database != name || metadata.Generation != 168 || metadata.PriorState.Exists {
		t.Fatalf("CLI persisted wrong prior metadata: %+v err=%v", metadata, err)
	}
	oct03CRequirePrefixIndex(t, f, true)
	if output, err := run(true, "-apply", "-confirm-database", name, "-backup", backup); err != nil {
		t.Fatalf("valid index repeat was not idempotent: %v output=%s", err, output)
	}
	if _, err := f.db.Exec(f.ctx, `drop index concurrently public.idx_catalog_tags_canonical_prefix_registry`); err != nil {
		t.Fatal(err)
	}
	if _, err := run(true, "-apply", "-confirm-database", name, "-backup", backup); err == nil {
		t.Fatal("CLI overwrote pre-change metadata before a new mutation")
	}
	oct03CRequirePrefixIndex(t, f, false)
}

// Pause only the advisory-lock response, after allowing the server to receive
// the query. Actual pg_locks and socket deadlines determine the cancellation
// interleaving; no database response or lock state is mocked.
type oct03CIndexLockAcknowledgementGate struct {
	armed    atomic.Bool
	pid      chan uint32
	release  chan struct{}
	deadline chan struct{}
	once     sync.Once
}

func (g *oct03CIndexLockAcknowledgementGate) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "select pg_advisory_lock(hashtext('mcmods-cn-schema-migrations'))") && !g.armed.Swap(true) {
		g.pid <- conn.PgConn().PID()
	}
	return ctx
}

func (*oct03CIndexLockAcknowledgementGate) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

type oct03CIndexLockGatedConnection struct {
	net.Conn
	gate *oct03CIndexLockAcknowledgementGate
}

func (c *oct03CIndexLockGatedConnection) Read(bytes []byte) (int, error) {
	if c.gate.armed.Load() {
		<-c.gate.release
	}
	return c.Conn.Read(bytes)
}

func (c *oct03CIndexLockGatedConnection) SetDeadline(deadline time.Time) error {
	err := c.Conn.SetDeadline(deadline)
	if c.gate.armed.Load() && !deadline.IsZero() && deadline.Before(time.Now().Add(time.Second)) {
		c.gate.once.Do(func() { close(c.gate.deadline) })
	}
	return err
}

func TestOCT03CTagPrefixUpgradeCancelledLockAcknowledgementDiscardsSessionIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	gate := &oct03CIndexLockAcknowledgementGate{pid: make(chan uint32, 1), release: make(chan struct{}), deadline: make(chan struct{})}
	var release sync.Once
	unblock := func() { release.Do(func() { close(gate.release) }) }
	defer unblock()
	cfg := f.db.Config().Copy()
	cfg.MaxConns, cfg.MinConns = 1, 0
	cfg.ConnConfig.Tracer = gate
	cfg.ConnConfig.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		return &oct03CIndexLockGatedConnection{Conn: conn, gate: gate}, nil
	}
	pool, err := pgxpool.NewWithConfig(f.ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		unblock()
		pool.Close()
	}()
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- database.ApplyCatalogTagPrefixIndex(ctx, pool, cfg.ConnConfig.Database, false,
			func(database.CatalogTagPrefixIndexBackup) error {
				return errors.New("cancelled operation must not persist metadata")
			})
	}()
	barrier, stop := context.WithTimeout(f.ctx, 5*time.Second)
	defer stop()
	var pid uint32
	select {
	case pid = <-gate.pid:
	case <-barrier.Done():
		t.Fatal("index repair did not reach the lock acknowledgement gate")
	}
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var granted bool
		if err := f.db.QueryRow(barrier, `select exists(select 1 from pg_locks where pid=$1 and locktype='advisory' and granted)`, pid).Scan(&granted); err != nil {
			t.Fatal(err)
		}
		if granted {
			break
		}
		select {
		case <-barrier.Done():
			t.Fatal("server did not actually acquire the paused advisory lock")
		case <-tick.C:
		}
	}
	cancel()
	select {
	case <-gate.deadline:
	case <-barrier.Done():
		t.Fatal("driver did not establish the cancellation deadline before acknowledgement")
	}
	unblock()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled lock response was not reported: %v", err)
		}
	case <-barrier.Done():
		t.Fatal("cancelled index repair did not return")
	}
	for {
		var stillLocked bool
		if err := f.db.QueryRow(barrier, `select exists(select 1 from pg_locks where pid=$1 and locktype='advisory' and granted)`, pid).Scan(&stillLocked); err != nil {
			t.Fatal(err)
		}
		if !stillLocked {
			break
		}
		select {
		case <-barrier.Done():
			t.Fatal("cancelled repair left its session advisory lock behind")
		case <-tick.C:
		}
	}
	var nextPID uint32
	if err := pool.QueryRow(f.ctx, `select pg_backend_pid()`).Scan(&nextPID); err != nil {
		t.Fatal(err)
	}
	if nextPID == pid {
		t.Fatal("cancelled lock connection was reused by the pool")
	}
	if err := database.ApplyCatalogTagPrefixIndex(f.ctx, pool, cfg.ConnConfig.Database, false, func(database.CatalogTagPrefixIndexBackup) error { return nil }); err != nil {
		t.Fatalf("replacement session could not inspect the already valid index: %v", err)
	}
}
