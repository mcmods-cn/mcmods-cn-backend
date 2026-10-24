package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type auditAccessHTTPResult struct {
	status int
	body   string
	err    error
}

func auditAccessStartHTTP(f test013Fixture, token, method, path string, body any) <-chan auditAccessHTTPResult {
	done := make(chan auditAccessHTTPResult, 1)
	go func() {
		raw, _ := json.Marshal(body)
		request, err := http.NewRequestWithContext(f.ctx, method, f.origin.URL+path, bytes.NewReader(raw))
		if err != nil {
			done <- auditAccessHTTPResult{err: err}
			return
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		response, err := f.origin.Client().Do(request)
		if err != nil {
			done <- auditAccessHTTPResult{err: err}
			return
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		done <- auditAccessHTTPResult{status: response.StatusCode, body: string(data), err: err}
	}()
	return done
}

func auditAccessReleaseAfterTwoSQLWaiters(t *testing.T, f test013Fixture, lock pgx.Tx) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiters int
		if err := f.db.QueryRow(ctx, `select count(*) from pg_stat_activity where datname=current_database() and pid<>pg_backend_pid() and wait_event_type='Lock'`).Scan(&waiters); err != nil {
			t.Fatal(err)
		}
		if waiters >= 2 {
			if err := lock.Commit(f.ctx); err != nil {
				t.Fatal(err)
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("both requests did not reach the explicit creator-row SQL barrier")
		case <-ticker.C:
		}
	}
}

func TestAuditF001TwoReviewersSerializeCompetingClaimsFullHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	for _, token := range []string{f.editor, f.otherEditor} {
		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[token], "creator.claim")
	}
	for _, token := range []string{f.reviewer, f.denied} {
		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[token], "creator.claim.review")
	}
	var creatorID int64
	var publicID string
	if err := f.db.QueryRow(f.ctx, `insert into creators(kind,name,normalized_name,review_status) values('author','Audit competing identity','audit competing identity','approved') returning id,public_id`).Scan(&creatorID, &publicID); err != nil {
		t.Fatal(err)
	}
	var claims []string
	for _, token := range []string{f.editor, f.otherEditor} {
		item := decodeTEST022Data[struct {
			ID string `json:"id"`
		}](t, f.require(t, token, http.MethodPost, "/api/v1/creators/"+publicID+"/claims", map[string]any{"proofMarkdown": "Synthetic competing claim"}, 201))
		claims = append(claims, item.ID)
	}
	lock, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(f.ctx)
	if _, err = lock.Exec(f.ctx, `select id from creators where id=$1 for update`, creatorID); err != nil {
		t.Fatal(err)
	}
	first := auditAccessStartHTTP(f, f.reviewer, http.MethodPatch, "/api/v1/admin/creator-claims/"+claims[0], map[string]any{"status": "approved"})
	second := auditAccessStartHTTP(f, f.denied, http.MethodPatch, "/api/v1/admin/creator-claims/"+claims[1], map[string]any{"status": "approved"})
	auditAccessReleaseAfterTwoSQLWaiters(t, f, lock)
	counts := map[int]int{}
	for _, done := range []<-chan auditAccessHTTPResult{first, second} {
		result := <-done
		if result.err != nil {
			t.Fatal(result.err)
		}
		counts[result.status]++
		if result.status != 200 && result.status != 409 {
			t.Errorf("review status=%d body=%s", result.status, result.body)
		}
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatalf("competing reviews=%v", counts)
	}
	var approved, rejected int
	if err = f.db.QueryRow(f.ctx, `select count(*) filter(where status='approved'),count(*) filter(where status='rejected') from creator_claims where creator_id=$1`, creatorID).Scan(&approved, &rejected); err != nil || approved != 1 || rejected != 1 {
		t.Fatalf("approved=%d rejected=%d err=%v", approved, rejected, err)
	}
}

func TestAuditF001DuplicateCreationHasOnePendingFactFullHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "creator.claim")
	var creatorID int64
	var publicID string
	if err := f.db.QueryRow(f.ctx, `insert into creators(kind,name,normalized_name,review_status) values('author','Audit duplicate identity','audit duplicate identity','approved') returning id,public_id`).Scan(&creatorID, &publicID); err != nil {
		t.Fatal(err)
	}
	lock, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(f.ctx)
	if _, err = lock.Exec(f.ctx, `select id from creators where id=$1 for update`, creatorID); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/creators/" + publicID + "/claims"
	body := map[string]any{"proofMarkdown": "Synthetic duplicate proof"}
	first := auditAccessStartHTTP(f, f.editor, http.MethodPost, path, body)
	second := auditAccessStartHTTP(f, f.editor, http.MethodPost, path, body)
	auditAccessReleaseAfterTwoSQLWaiters(t, f, lock)
	counts := map[int]int{}
	for _, done := range []<-chan auditAccessHTTPResult{first, second} {
		result := <-done
		if result.err != nil {
			t.Fatal(result.err)
		}
		counts[result.status]++
	}
	if counts[201] != 1 || counts[409] != 1 {
		t.Fatalf("duplicate creations=%v", counts)
	}
	var facts int
	if err = f.db.QueryRow(f.ctx, `select count(*) from creator_claims where creator_id=$1 and status='pending'`, creatorID).Scan(&facts); err != nil || facts != 1 {
		t.Fatalf("facts=%d err=%v", facts, err)
	}
}
