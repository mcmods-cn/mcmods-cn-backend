package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/activity"
)

func auditFixLogShareFacts(t *testing.T, f test037Fixture) map[string]string {
	t.Helper()
	facts := f.facts(t)
	for _, table := range []string{"log_shares", "log_share_entries"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` fact`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		facts[table] = raw
	}
	return facts
}
func auditFixUploadLogShareFile(t *testing.T, f test037Fixture, name, text string) string {
	t.Helper()
	raw := []byte(text)
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", name, "text/plain", "log_share", raw)
	f.put(t, ticket, raw)
	return decodeTEST022Data[struct {
		ID string `json:"id"`
	}](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201)).ID
}
func auditFixNoLogStages(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "mcmods-sanitized-log-batch-") {
			t.Fatalf("owned sanitized stage left behind: %s", filepath.Base(entry.Name()))
		}
	}
}
func TestAuditFixCMP003LogShareBatchActivitySharesAllNewSharesTransaction(t *testing.T) {
	scratch := t.TempDir()
	t.Setenv("TMPDIR", scratch)
	f := newTEST037Fixture(t)
	first := auditFixUploadLogShareFile(t, f, "first.log", "Authorization: Bearer audit-first-secret\nfirst visible line\n")
	second := auditFixUploadLogShareFile(t, f, "second.log", "access_token=audit-second-secret\nsecond visible line\n")
	foreignRaw := []byte("foreign owned log remains private\n")
	foreignTicket := f.ticket(t, f.otherEditor, "/api/v1/users/me/oss/uploads/presign", "foreign.log", "text/plain", "log_share", foreignRaw)
	f.put(t, foreignTicket, foreignRaw)
	foreign := decodeTEST022Data[struct {
		ID string `json:"id"`
	}](t, auditOSSRequire(t, f, f.otherEditor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", foreignTicket.completion(), 201)).ID
	ids := []string{second, "absent001", first, second, foreign}
	body := createFileLogSharesRequest{FileIDs: ids, RetentionDays: 30}
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
	before := auditFixLogShareFacts(t, f)
	raw := auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/log-shares/files", body, 500)
	auditFixRequireSpecificActivityError(t, raw, "LOG_SHARE_BATCH_PERSIST_FAILED")
	auditFixRequireProjectFactsUnchanged(t, before, auditFixLogShareFacts(t, f))
	auditFixNoLogStages(t, scratch)
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
	result := decodeTEST022Data[struct {
		Items []map[string]any `json:"items"`
	}](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/log-shares/files", body, 207))
	if len(result.Items) != 5 {
		t.Fatalf("batch results=%d", len(result.Items))
	}
	for i, item := range result.Items {
		if item["fileId"] != ids[i] {
			t.Fatal("sorting changed stable input mapping")
		}
	}
	if result.Items[4]["status"] != "failed" || result.Items[1]["status"] != "failed" || result.Items[0]["status"] != "ready" || result.Items[2]["status"] != "ready" || result.Items[3]["publicCode"] != result.Items[0]["publicCode"] {
		t.Fatal("partial result/reuse contract changed")
	}
	var shares, entries int
	var text string
	if err := f.db.QueryRow(f.ctx, `select (select count(*) from log_shares),(select count(*) from log_share_entries),(select string_agg(sanitized_text,E'\n') from log_share_entries)`).Scan(&shares, &entries, &text); err != nil || shares != 2 || entries != 2 || strings.Contains(text, "audit-first-secret") || strings.Contains(text, "audit-second-secret") || !strings.Contains(text, "first visible line") || !strings.Contains(text, "second visible line") {
		t.Fatalf("redacted shares/entries=%d/%d err=%v", shares, entries, err)
	}
	if got := auditFixActivityIntentCount(t, f.test013Fixture, f.editor, activity.ActionCreate, activity.ObjectFile); got != 1 {
		t.Fatalf("batch creation events=%d", got)
	}
	auditFixNoLogStages(t, scratch)
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
	before = auditFixLogShareFacts(t, f)
	auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/log-shares/files", body, 207)
	auditFixRequireProjectFactsUnchanged(t, before, auditFixLogShareFacts(t, f))
	auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/log-shares/files", createFileLogSharesRequest{FileIDs: []string{"absent001"}, RetentionDays: 30}, 207)
	auditFixRequireProjectFactsUnchanged(t, before, auditFixLogShareFacts(t, f))
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
}
func TestAuditFixCMP003LogShareReverseBatchesUseSingleSlotsOutsideProviderIO(t *testing.T) {
	scratch := t.TempDir()
	t.Setenv("TMPDIR", scratch)
	f := newTEST037Fixture(t)
	first := auditFixUploadLogShareFile(t, f, "first.log", "first batch log\n")
	second := auditFixUploadLogShareFile(t, f, "second.log", "second batch log\n")
	makePool := func() *pgxpool.Pool {
		t.Helper()
		cfg := f.db.Config().Copy()
		cfg.MaxConns = 1
		cfg.MinConns = 0
		pool, err := pgxpool.NewWithConfig(f.ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		return pool
	}
	one, two := makePool(), makePool()
	f.server.db = one
	otherServer := NewServer(f.ctx, f.server.cfg, two, nil, nil, nil, nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := otherServer.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		otherServer.cache.Close()
	})
	otherOrigin := httptest.NewServer(otherServer)
	t.Cleanup(otherOrigin.Close)
	handler := f.provider.Config.Handler
	reached, release := make(chan struct{}), make(chan struct{})
	var reads atomic.Int32
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	f.provider.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			n := reads.Add(1)
			if n == 2 {
				close(reached)
			}
			if n <= 2 {
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
			}
		}
		handler.ServeHTTP(w, r)
	})
	otherFixture := f.test013Fixture
	otherFixture.origin = otherOrigin
	start := make(chan struct{})
	results := make(chan auditCompletionHTTPResult, 2)
	// Two distinct API instances each own one connection. Both actual source GETs
	// are held before either transaction; reverse order must not deadlock writes.
	for i, fixture := range []test013Fixture{f.test013Fixture, otherFixture} {
		ids := []string{first, second}
		if i == 1 {
			ids = []string{second, first}
		}
		go func(current test013Fixture, order []string) {
			<-start
			results <- <-auditCompletionAsyncJSON(current, f.editor, http.MethodPost, "/api/v1/log-shares/files", createFileLogSharesRequest{FileIDs: order, RetentionDays: 30})
		}(fixture, ids)
	}
	close(start)
	wait, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	select {
	case <-reached:
	case <-wait.Done():
		t.Fatal("both owned source GETs did not reach controlled barrier")
	}
	for _, pool := range []*pgxpool.Pool{one, two} {
		acquire, stop := context.WithTimeout(f.ctx, time.Second)
		slot, err := pool.Acquire(acquire)
		stop()
		if err != nil {
			t.Fatal("provider IO held the API instance's only database slot")
		}
		slot.Release()
	}
	unblock()
	for range 2 {
		select {
		case result := <-results:
			if result.err != nil || result.status != 207 {
				t.Fatalf("reverse batch status=%d err=%v", result.status, result.err)
			}
			var value struct {
				Data struct {
					Items []map[string]any `json:"items"`
				} `json:"data"`
			}
			if err := json.Unmarshal(result.raw, &value); err != nil {
				t.Fatal(err)
			}
			if len(value.Data.Items) != 2 {
				t.Fatal("reverse results omitted items")
			}
		case <-wait.Done():
			t.Fatal("reverse batch did not finish within controlled boundary")
		}
	}
	var shares, events int
	if err := f.db.QueryRow(f.ctx, `select (select count(*) from log_shares),(select count(*) from activity_event_outbox where user_id=$1 and action_id=$2 and object_type_id=$3)`, f.userIDs[f.editor], activity.ActionCreate, activity.ObjectFile).Scan(&shares, &events); err != nil || shares != 2 || events != 1 {
		t.Fatalf("concurrent shares/events=%d/%d err=%v", shares, events, err)
	}
	auditFixNoLogStages(t, scratch)
	before := auditFixLogShareFacts(t, f)
	auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/log-shares/files", createFileLogSharesRequest{FileIDs: []string{second, first}, RetentionDays: 30}, 207)
	if !reflect.DeepEqual(before, auditFixLogShareFacts(t, f)) {
		t.Fatal("completed reverse retry created another event/share")
	}
}

// Occupied active-source slots are legitimate per-item conflicts, not a reason
// to roll back an unrelated accepted file. Maintenance need not have relabeled
// an elapsed ready share, and another creator may still own a processing slot.
func TestAuditFixCMP003LogShareOccupiedSourceKeepsPartial207(t *testing.T) {
	for _, mode := range []string{"expired-ready", "processing"} {
		t.Run(mode, func(t *testing.T) {
			f := newTEST037Fixture(t)
			occupied := auditFixUploadLogShareFile(t, f, "occupied.log", "old visible redacted log\n")
			fresh := auditFixUploadLogShareFile(t, f, "fresh.log", "new visible redacted log\n")
			auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/log-shares/files", createFileLogSharesRequest{FileIDs: []string{occupied}, RetentionDays: 30}, 207)
			state := "ready"
			expiry := time.Now().Add(-time.Hour)
			if mode == "processing" {
				state = "processing"
				expiry = time.Now().Add(time.Hour)
			}
			if _, err := f.db.Exec(f.ctx, `update log_shares set status=$2,expires_at=$3 where source_file_id=(select id from oss_files where public_id=$1)`, occupied, state, expiry); err != nil {
				t.Fatal(err)
			}
			var original string
			if err := f.db.QueryRow(f.ctx, `select row_to_json(s)::text from log_shares s where source_file_id=(select id from oss_files where public_id=$1)`, occupied).Scan(&original); err != nil {
				t.Fatal(err)
			}
			beforeEvents := auditFixActivityIntentCount(t, f.test013Fixture, f.editor, activity.ActionCreate, activity.ObjectFile)
			body := createFileLogSharesRequest{FileIDs: []string{occupied, fresh}, RetentionDays: 30}
			result := decodeTEST022Data[struct {
				Items []map[string]any `json:"items"`
			}](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/log-shares/files", body, 207))
			if len(result.Items) != 2 || result.Items[0]["fileId"] != occupied || result.Items[0]["status"] != "failed" || result.Items[1]["fileId"] != fresh || result.Items[1]["status"] != "ready" {
				t.Fatalf("partial result=%v", result.Items)
			}
			if got := auditFixActivityIntentCount(t, f.test013Fixture, f.editor, activity.ActionCreate, activity.ObjectFile); got != beforeEvents+1 {
				t.Fatalf("new activity=%d before=%d", got, beforeEvents)
			}
			var after string
			if err := f.db.QueryRow(f.ctx, `select row_to_json(s)::text from log_shares s where source_file_id=(select id from oss_files where public_id=$1)`, occupied).Scan(&after); err != nil || after != original {
				t.Fatalf("occupied share changed: %v", err)
			}
			var shares, entries int
			if err := f.db.QueryRow(f.ctx, `select (select count(*) from log_shares),(select count(*) from log_share_entries)`).Scan(&shares, &entries); err != nil || shares != 2 || entries != 2 {
				t.Fatalf("accepted share lost shares=%d entries=%d err=%v", shares, entries, err)
			}
		})
	}
}
