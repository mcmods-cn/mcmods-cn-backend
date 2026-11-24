package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestUnacknowledgedProjectMirrorRetainsOnlyItsOwnDelayedCleanupIntegration(t *testing.T) {
	f := newTEST016FileFixture(t)
	ctx := f.ctx
	const oldKey, replacement = "project/attempt-one.jar", "project/attempt-two.jar"
	received := make(chan struct{})
	release := make(chan struct{})
	var denyDelete atomic.Bool
	denyDelete.Store(true)
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, oldKey) && denyDelete.Swap(false) {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("<Error><Code>AccessDenied</Code><Message>test object policy</Message></Error>"))
			return
		}
		if r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, oldKey) {
			// Store the bytes through the same OSS fixture, then hold its response.
			f.provider.Config.Handler.ServeHTTP(httptest.NewRecorder(), r)
			close(received)
			<-release
			w.WriteHeader(http.StatusOK)
			return
		}
		f.provider.Config.Handler.ServeHTTP(w, r)
	}))
	defer local.Close()
	defer close(release)
	cfg := f.server.ossConfigFromSettings(ctx)
	cfg.Endpoint = local.URL
	client := newOSSClient(cfg, local.URL, true)
	body := []byte("normal mirror bytes")
	put := func(requestCtx context.Context, key string) error {
		conn, err := f.db.Acquire(requestCtx)
		if err != nil {
			return err
		}
		defer conn.Release()
		spooled, err := spoolProviderFile(bytes.NewReader(body), providerProjectFile{SizeBytes: int64(len(body))}, int64(len(body)))
		if err != nil {
			return err
		}
		defer spooled.Close()
		return putSpooledProjectMirror(requestCtx, conn, client, cfg, key, spooled)
	}
	requestCtx, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- put(requestCtx, oldKey) }()
	select {
	case <-received:
	case err := <-done:
		t.Fatalf("transfer returned before receipt was held: %v", err)
	case <-requestCtx.Done():
		t.Fatal(requestCtx.Err())
	}
	var expiryID int64
	var delayed bool
	if err := f.db.QueryRow(ctx, `select id,next_attempt_at>now()+interval '23 hours' from oss_object_deletion_outbox
		where object_key=$1 and status='pending' and attempts=0`, oldKey).Scan(&expiryID, &delayed); err != nil || !delayed {
		t.Fatalf("bytes reached storage without a durable delayed cleanup: %v", err)
	}
	var lockedID int64
	err := f.db.QueryRow(ctx, `select id from oss_object_deletion_outbox where id=$1 for update nowait`, expiryID).Scan(&lockedID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("in-flight upload did not hold its cleanup receipt: %v", err)
	}
	stop()
	if err = <-done; err == nil {
		t.Fatal("acknowledgement was withheld but upload reported success")
	}
	if err = put(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	cleanup := NewOSSDeletionWorker(f.server.cfg, f.db)
	if _, err = cleanup.claim(ctx); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("an uncertain or active transfer was selected too early: %v", err)
	}
	if _, err = f.db.Exec(ctx, `update oss_object_deletion_outbox set next_attempt_at=now() where id=$1`, expiryID); err != nil {
		t.Fatal(err)
	}
	job, err := cleanup.claim(ctx)
	if err != nil || job.ID != expiryID {
		t.Fatalf("wrong expired transfer selected: %v", err)
	}
	if err = cleanup.deleteObject(ctx, job); err == nil {
		t.Fatal("provider rejected deletion but it was reported complete")
	}
	if dead, persistErr := cleanup.recordFailure(ctx, job, err); persistErr != nil || !dead {
		t.Fatalf("provider rejection was not retained: dead=%v err=%v", dead, persistErr)
	}
	var publicID, failureClass string
	if err = f.db.QueryRow(ctx, `select public_id,failure_class from oss_object_deletion_outbox where id=$1 and status='dead'`, expiryID).Scan(&publicID, &failureClass); err != nil || failureClass != "authorization" {
		t.Fatalf("unrecovered deletion class=%q err=%v", failureClass, err)
	}
	grantTEST044Permissions(t, ctx, f.db, f.userIDs[f.reviewer], "admin.config.write")
	f.require(t, f.reviewer, http.MethodPost, "/api/v1/admin/infrastructure/oss-deletions/"+publicID+"/replay", nil, http.StatusAccepted)
	if job, err = cleanup.claim(ctx); err != nil || job.ID != expiryID {
		t.Fatalf("replay selected another transfer: %v", err)
	}
	if err = cleanup.deleteObject(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err = cleanup.complete(ctx, job); err != nil {
		t.Fatal(err)
	}
	objects := f.objects()
	if _, exists := objects[oldKey]; exists || objects[replacement] != sha256Hex(body) {
		t.Fatalf("expiry removed the wrong bytes; remaining=%v", objects)
	}
}
