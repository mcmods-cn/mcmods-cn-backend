package httpapi

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestTEST037ProviderDeleteKeepsLeaseLockedUntilSideEffectCompletesIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	entered, proceed := make(chan struct{}), make(chan struct{})
	var enteredOnce, proceedOnce sync.Once
	resume := func() { proceedOnce.Do(func() { close(proceed) }) }
	defer resume()
	upstream, err := url.Parse(f.provider.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			enteredOnce.Do(func() { close(entered) })
			select {
			case <-proceed:
			case <-r.Context().Done():
				return
			}
		}
		forward := r.Clone(r.Context())
		forward.URL.Scheme, forward.URL.Host, forward.Host = upstream.Scheme, upstream.Host, upstream.Host
		forward.RequestURI = ""
		response, forwardErr := f.provider.Client().Do(forward)
		if forwardErr != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		for key, values := range response.Header {
			w.Header()[key] = append([]string(nil), values...)
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	t.Cleanup(proxy.Close)
	cfg := f.cfg
	cfg.Endpoint, cfg.PublicEndpoint = proxy.URL, proxy.URL
	sealed, err := f.server.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, "update system_settings set value=$1::jsonb where key='oss.aliyun'", sealed); err != nil {
		t.Fatal(err)
	}
	raw := []byte("TEST037 actual blocked provider DELETE\n")
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "inflight.zip", "application/octet-stream", "test037-private", raw)
	f.put(t, ticket, raw)
	f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201)
	f.require(t, f.editor, http.MethodDelete, "/api/v1/users/me/files", map[string]any{"objectKey": ticket.ObjectKey}, 200)
	first := NewOSSDeletionWorker(f.server.cfg, f.db)
	job, err := first.claim(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, "update oss_object_deletion_outbox set locked_at=now()-interval '10 minutes' where id=$1", job.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- first.deleteObject(f.ctx, job) }()
	select {
	case <-entered:
	case <-f.ctx.Done():
		t.Fatal("actual DELETE never reached provider within original fixture budget")
	}
	before := f.facts(t)
	second := NewOSSDeletionWorker(f.server.cfg, f.db)
	if claimed, claimErr := second.claim(f.ctx); !errors.Is(claimErr, pgx.ErrNoRows) {
		t.Fatalf("inflight physical deletion lease was reclaimable: %#v/%v", claimed, claimErr)
	}
	f.unchanged(t, before)
	f.store.mu.Lock()
	object, exists := f.store.objects[ticket.ObjectKey]
	f.store.mu.Unlock()
	if !exists || !bytes.Equal(object.data, raw) {
		t.Fatal("blocked provider fixture did not preserve bytes before release")
	}
	resume()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if err = first.complete(f.ctx, job); err != nil {
		t.Fatal(err)
	}
	f.store.mu.Lock()
	_, exists = f.store.objects[ticket.ObjectKey]
	f.store.mu.Unlock()
	if exists {
		t.Fatal("provider did not execute DELETE after its controlled release")
	}
}
