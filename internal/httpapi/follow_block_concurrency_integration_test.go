package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/security"
)

type followTransactionPause struct {
	armed   atomic.Bool
	reached chan struct{}
	release chan struct{}
}

func (pause *followTransactionPause) TraceQueryStart(ctx context.Context, _ *pgx.Conn, query pgx.TraceQueryStartData) context.Context {
	if strings.EqualFold(strings.TrimSpace(query.SQL), "begin") && pause.armed.CompareAndSwap(true, false) {
		close(pause.reached)
		select {
		case <-pause.release:
		case <-ctx.Done():
		}
	}
	return ctx
}
func (*followTransactionPause) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestOCT02CommittedBlockPreventsInFlightFollowIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	base := newOCT02IsolatedDatabase(t, ctx)
	var actor, target int64
	var actorPublic, targetPublic string
	for _, user := range []struct {
		name   string
		id     *int64
		public *string
	}{{"follow-actor", &actor, &actorPublic}, {"block-target", &target, &targetPublic}} {
		if err := base.QueryRow(ctx, `insert into users(username,email,password_hash) values($1,$2,'unused') returning id,public_id`, user.name, user.name+"@example.invalid").Scan(user.id, user.public); err != nil {
			t.Fatal(err)
		}
	}
	grantTEST044Permissions(t, ctx, base, target, "user.follow.receive")
	pause := &followTransactionPause{reached: make(chan struct{}), release: make(chan struct{})}
	poolConfig := base.Config()
	poolConfig.ConnConfig.Tracer = pause
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		select {
		case <-pause.release:
		default:
			close(pause.release)
		}
		pool.Close()
	}()
	server := &Server{db: pool}
	pause.armed.Store(true)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+targetPublic+"/follow", nil)
	request.SetPathValue("id", targetPublic)
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: actor, PublicSubject: actorPublic}))
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { server.followUser(response, request); close(done) }()
	select {
	case <-pause.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("follow did not reach transaction barrier")
	}
	block := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+actorPublic+"/block", nil)
	block.SetPathValue("id", actorPublic)
	block = block.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: target, PublicSubject: targetPublic}))
	blockResponse := httptest.NewRecorder()
	(&Server{db: base}).userBlock(blockResponse, block)
	if blockResponse.Code != http.StatusOK {
		t.Fatalf("block status=%d: %s", blockResponse.Code, blockResponse.Body.String())
	}
	close(pause.release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("follow remained blocked after release")
	}
	if response.Code != http.StatusForbidden {
		t.Fatalf("stale follow status=%d want403 body=%s", response.Code, response.Body.String())
	}
	var follows, blocks int
	if err = base.QueryRow(ctx, `select (select count(*) from user_follows where follower_id=$1 and followed_id=$2),(select count(*) from user_blocks where blocker_id=$2 and blocked_id=$1)`, actor, target).Scan(&follows, &blocks); err != nil || follows != 0 || blocks != 1 {
		t.Fatalf("relationships follow/block=%d/%d err=%v", follows, blocks, err)
	}
}
