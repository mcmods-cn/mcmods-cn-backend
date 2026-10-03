package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type oct03CAuthorizationRequestKey struct{}

// The first production transaction remains open while the competing request
// reaches its actual PostgreSQL lock. No substitute implementation is called.
type oct03CAuthorizationBarrier struct {
	firstSQL, secondSQL string
	firstReached        chan struct{}
	secondPID           chan uint32
	release             chan struct{}
	first, second       atomic.Bool
	once                sync.Once
}

func (b *oct03CAuthorizationBarrier) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	request, _ := ctx.Value(oct03CAuthorizationRequestKey{}).(string)
	if request == "first" && strings.Contains(data.SQL, b.firstSQL) && b.first.CompareAndSwap(false, true) {
		close(b.firstReached)
		select {
		case <-b.release:
		case <-ctx.Done():
		}
	}
	if request == "second" && strings.Contains(data.SQL, b.secondSQL) && b.second.CompareAndSwap(false, true) {
		b.secondPID <- conn.PgConn().PID()
	}
	return ctx
}

func (*oct03CAuthorizationBarrier) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (b *oct03CAuthorizationBarrier) allowFirst() { b.once.Do(func() { close(b.release) }) }

func oct03CTraceAuthorizationPool(t *testing.T, f test013Fixture, firstSQL, secondSQL string) (*Server, *oct03CAuthorizationBarrier) {
	t.Helper()
	b := &oct03CAuthorizationBarrier{firstSQL: firstSQL, secondSQL: secondSQL,
		firstReached: make(chan struct{}), secondPID: make(chan uint32, 1), release: make(chan struct{})}
	cfg := f.db.Config().Copy()
	cfg.ConnConfig.Tracer = b
	pool, err := pgxpool.NewWithConfig(f.ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	t.Cleanup(b.allowFirst)
	server := f.origin.Config.Handler.(*Server)
	server.db = pool
	return server, b
}

func oct03CStartAuthorizationRequest(ctx context.Context, label string, handler http.HandlerFunc, token, method, body string, paths map[string]string) <-chan *httptest.ResponseRecorder {
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- performTEST044Request(context.WithValue(ctx, oct03CAuthorizationRequestKey{}, label), handler, token, method, "/oct03_authorization", body, paths)
	}()
	return result
}

func oct03CWaitForAuthorizationLock(t *testing.T, f test013Fixture, b *oct03CAuthorizationBarrier) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	var pid uint32
	select {
	case pid = <-b.secondPID:
	case <-ctx.Done():
		t.Fatal("competing production request did not reach its lock statement")
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := f.db.QueryRow(ctx, `select coalesce((select wait_event_type='Lock' from pg_stat_activity where pid=$1),false)`, pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("competing transaction did not wait for the first authorization mutation")
		}
	}
}

func oct03CWaitForAuthorizationBarrier(t *testing.T, ctx context.Context, reached <-chan struct{}, result <-chan *httptest.ResponseRecorder) {
	t.Helper()
	select {
	case <-reached:
	case response := <-result:
		t.Fatalf("authorization request ended before barrier: status=%d body=%s", response.Code, response.Body.String())
	case <-ctx.Done():
		t.Fatal("production authorization mutation did not reach controlled barrier")
	}
}

func oct03CRequireAuthorizationResponse(t *testing.T, ctx context.Context, response <-chan *httptest.ResponseRecorder, want int) {
	t.Helper()
	select {
	case got := <-response:
		if got.Code != want {
			t.Fatalf("authorization mutation status=%d want=%d body=%s", got.Code, want, got.Body.String())
		}
	case <-ctx.Done():
		t.Fatal("production authorization mutation did not finish")
	}
}

func TestOCT03CBUG004ConcurrentRoleTrackReadsCommittedManualBindingIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	actor, target := f.userIDs[f.reviewer], f.userIDs[f.editor]
	grantTEST044Permissions(t, f.ctx, f.db, actor, "permission.write")
	for _, role := range []string{"oct03_low", "oct03_middle", "oct03_high"} {
		if _, err := f.db.Exec(f.ctx, `insert into roles(code,name) values($1,$1)`, role); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.Exec(f.ctx, `insert into permission_role_tracks(code,name) values('oct03_track','Oct03');
		insert into permission_role_track_roles(track_code,role_id,position)
		select 'oct03_track',id,case code when 'oct03_low' then 0 when 'oct03_middle' then 1 else 2 end
		from roles where code=any(array['oct03_low','oct03_middle','oct03_high'])`); err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Microsecond)
	if _, err := f.db.Exec(f.ctx, `insert into user_role_bindings(user_id,role_id,source,source_key,expires_at)
		select $1,id,'manual','',$2 from roles where code='oct03_low'`, target, expiry); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into user_role_bindings(user_id,role_id,source,source_key)
		select $1,id,source,key from roles cross join (values('level_track','oct03-level'),('account_status','oct03-status')) sources(source,key)
		where code='oct03_low'`, target); err != nil {
		t.Fatal(err)
	}
	var targetPublicID string
	if err := f.db.QueryRow(f.ctx, `select public_id from users where id=$1`, target).Scan(&targetPublicID); err != nil {
		t.Fatal(err)
	}
	server, barrier := oct03CTraceAuthorizationPool(t, f, "select role.code,binding.expires_at", "select id from users where id=$1 for update")
	ctx, cancel := context.WithTimeout(f.ctx, 15*time.Second)
	defer cancel()
	defer barrier.allowFirst()
	handler := server.requirePermission("permission.write", server.upgradeUserRoleTrack)
	paths := map[string]string{"id": targetPublicID, "code": "oct03_track"}
	first := oct03CStartAuthorizationRequest(ctx, "first", handler, f.reviewer, http.MethodPost, `{}`, paths)
	oct03CWaitForAuthorizationBarrier(t, ctx, barrier.firstReached, first)
	second := oct03CStartAuthorizationRequest(ctx, "second", handler, f.reviewer, http.MethodPost, `{}`, paths)
	oct03CWaitForAuthorizationLock(t, f, barrier)
	barrier.allowFirst()
	oct03CRequireAuthorizationResponse(t, ctx, first, http.StatusOK)
	oct03CRequireAuthorizationResponse(t, ctx, second, http.StatusOK)
	var role string
	var persistedExpiry time.Time
	if err := f.db.QueryRow(f.ctx, `select role.code,binding.expires_at from user_role_bindings binding join roles role on role.id=binding.role_id
		where binding.user_id=$1 and binding.source='manual' and binding.source_key='' and role.code like 'oct03\_%' escape '\'`, target).Scan(&role, &persistedExpiry); err != nil {
		t.Fatal(err)
	}
	if role != "oct03_high" || !persistedExpiry.Equal(expiry) {
		t.Fatalf("serialized upgrades role=%s expiryPreserved=%v", role, persistedExpiry.Equal(expiry))
	}
	var systemBindings, audits int
	if err := f.db.QueryRow(f.ctx, `select
		(select count(*) from user_role_bindings b join roles r on r.id=b.role_id where b.user_id=$1 and r.code='oct03_low' and b.source in ('level_track','account_status')),
		(select count(*) from permission_audit_logs where target_user_id=$1 and action='upgrade_role_track' and payload->>'track'='oct03_track')`, target).Scan(&systemBindings, &audits); err != nil {
		t.Fatal(err)
	}
	if systemBindings != 2 || audits != 2 {
		t.Fatalf("system bindings=%d audits=%d want=2/2", systemBindings, audits)
	}
}

func TestOCT03CBUG122ConcurrentRoleParentsCannotCommitCycleIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "permission.write")
	if _, err := f.db.Exec(f.ctx, `insert into roles(code,name) values('oct03_a','A'),('oct03_b','B')`); err != nil {
		t.Fatal(err)
	}
	server, barrier := oct03CTraceAuthorizationPool(t, f, "select code,parents from roles order by code", "pg_advisory_xact_lock(hashtext('mcmods-cn-role-graph'))")
	ctx, cancel := context.WithTimeout(f.ctx, 15*time.Second)
	defer cancel()
	defer barrier.allowFirst()
	handler := server.requirePermission("permission.write", server.updateRole)
	first := oct03CStartAuthorizationRequest(ctx, "first", handler, f.reviewer, http.MethodPut,
		`{"name":"A","parents":["oct03_b"]}`, map[string]string{"code": "oct03_a"})
	oct03CWaitForAuthorizationBarrier(t, ctx, barrier.firstReached, first)
	second := oct03CStartAuthorizationRequest(ctx, "second", handler, f.reviewer, http.MethodPut,
		`{"name":"B","parents":["oct03_a"]}`, map[string]string{"code": "oct03_b"})
	oct03CWaitForAuthorizationLock(t, f, barrier)
	barrier.allowFirst()
	oct03CRequireAuthorizationResponse(t, ctx, first, http.StatusOK)
	oct03CRequireAuthorizationResponse(t, ctx, second, http.StatusBadRequest)
	var aParents, bParents []string
	var audits int
	if err := f.db.QueryRow(f.ctx, `select (select parents from roles where code='oct03_a'),
		(select parents from roles where code='oct03_b'),
		(select count(*) from permission_audit_logs where action='update_role' and payload->>'code' in ('oct03_a','oct03_b'))`).Scan(&aParents, &bParents, &audits); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(aParents, []string{"oct03_b"}) || len(bParents) != 0 || audits != 1 {
		t.Fatalf("cycle rejection did not rollback: A=%v B=%v audits=%d", aParents, bParents, audits)
	}
	if err := validateRoleGraph(map[string][]string{"oct03_a": aParents, "oct03_b": bParents}); err != nil {
		t.Fatal(fmt.Errorf("committed graph is invalid: %w", err))
	}
}
