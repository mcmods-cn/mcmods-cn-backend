package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/queue"
	"mcmods-cn-backend/internal/security"
)

// Unlike the targeted temporary-table regressions, this exercises the actual
// routes, JWT/session resolution, permissions, triggers and complete schema.
func TestTEST048GovernanceHTTPPermissionsConcurrencyAndTransactionsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := newTEST018IsolatedDatabase(t, ctx)
	if err := database.SeedRBAC(ctx, pool); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.JWTSecret = "test048-only-jwt-signing-secret"
	cfg.SettingsEncryptionKey = "test048-only-encryption-key-32-bytes"
	cfg.Redis.Enabled, cfg.AntiAbuse.Enabled = false, false
	cfg.NATS = config.NATSConfig{}
	queueClient := queue.New(ctx, cfg.NATS)
	defer queueClient.Close()
	server := NewServer(ctx, cfg, pool, queueClient, nil, nil, nil)
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			t.Errorf("shutdown governance server: %v", err)
		}
	}()
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	client := httpServer.Client()
	client.Timeout = 15 * time.Second
	reporterID, _, reporter := createTEST044User(t, ctx, pool, cfg, "048reporter", "UTC")
	authorID, authorPublicID, author := createTEST044User(t, ctx, pool, cfg, "048author", "UTC")
	reviewerIDs := make([]int64, 2)
	reviewers := make([]string, 2)
	for index := range reviewers {
		reviewerIDs[index], _, reviewers[index] = createTEST044User(t, ctx, pool, cfg, fmt.Sprintf("048reviewer%d", index), "UTC")
	}
	request := func(t *testing.T, token, method, path, body string, expected int) []byte {
		t.Helper()
		status, raw, err := test048HTTPRequest(ctx, client, httpServer.URL, token, method, path, body)
		if err != nil || status != expected {
			t.Fatalf("%s %s status=%d want=%d err=%v body=%s", method, path, status, expected, err, raw)
		}
		return raw
	}
	sealedOSS, err := server.sealSystemSetting(ossConfigPayload{Enabled: true, Region: "cn-test", Endpoint: "https://oss.example.invalid", Bucket: "test048-only", AccessKeyID: "test048-key", AccessKeySecret: "test048-only-secret", UseCName: true, DownloadURLMode: ossDownloadModePresigned})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb) on conflict(key) do update set value=excluded.value`, sealedOSS); err != nil {
		t.Fatal(err)
	}
	assertCount := func(t *testing.T, query string, expected int, args ...any) {
		t.Helper()
		var got int
		if err := pool.QueryRow(ctx, query, args...).Scan(&got); err != nil || got != expected {
			t.Fatalf("facts query=%s got=%d want=%d err=%v", query, got, expected, err)
		}
	}
	reportBody := fmt.Sprintf(`{"targetType":"user","targetId":%q,"reasonCode":"harassment","detail":"test048 public profile evidence"}`, authorPublicID)
	request(t, "", http.MethodPost, "/api/v1/reports", reportBody, http.StatusUnauthorized)
	request(t, reporter, http.MethodPost, "/api/v1/reports", reportBody, http.StatusForbidden)
	grantTEST044Permissions(t, ctx, pool, reporterID, "report.create", "report.view_own")
	grantTEST044Permissions(t, ctx, pool, authorID, "report.create", "report.view_own")
	for index := range reviewers {
		grantTEST044Permissions(t, ctx, pool, reviewerIDs[index], "report.review")
	}
	t.Run("invisible targets and another upload never become report facts", func(t *testing.T) {
		var modID int64
		if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,submitted_by)
			values('test048m1','test048-hidden','Hidden governance fixture',$1) returning id`, authorID).Scan(&modID); err != nil {
			t.Fatal(err)
		}
		var commentPublicID string
		if err := pool.QueryRow(ctx, `insert into comments(target_type,target_id,author_id,body)
			values('mod',$1,$2,'Comment on hidden target') returning public_id`, modID, authorID).Scan(&commentPublicID); err != nil {
			t.Fatal(err)
		}
		request(t, reporter, http.MethodPost, "/api/v1/reports", `{"targetType":"mod","targetId":"test048m1","reasonCode":"malware"}`, http.StatusNotFound)
		request(t, reporter, http.MethodPost, "/api/v1/reports", fmt.Sprintf(`{"targetType":"comment","targetId":%q,"reasonCode":"spam"}`, commentPublicID), http.StatusNotFound)
		request(t, reporter, http.MethodPost, "/api/v1/reports", fmt.Sprintf(`{"targetType":"skin","targetId":%q,"reasonCode":"malware"}`, authorPublicID), http.StatusNotFound)
		request(t, reporter, http.MethodPost, "/api/v1/reports", fmt.Sprintf(`{"targetType":"user","targetId":%q,"reasonCode":"other"}`, authorPublicID), http.StatusBadRequest)
		otherEvidence := newTEST048Evidence(t, ctx, pool, authorID, "other", "clean")
		request(t, reporter, http.MethodPost, "/api/v1/reports", strings.TrimSuffix(reportBody, "}")+fmt.Sprintf(`,"evidenceIds":[%q]}`, otherEvidence), http.StatusBadRequest)
		assertCount(t, `select count(*) from reports`, 0)
		assertCount(t, `select count(*) from report_snapshots`, 0)
		assertCount(t, `select count(*) from report_evidence where report_id is not null`, 0)
		request(t, reporter, http.MethodPost, "/api/v1/reports/evidence/"+otherEvidence+"/access", `{}`, http.StatusForbidden)
		request(t, "", http.MethodPost, "/api/v1/reports/evidence/"+otherEvidence+"/access", `{}`, http.StatusUnauthorized)
	})
	evidence := newTEST048Evidence(t, ctx, pool, reporterID, "own", "clean")
	rejected := newTEST048Evidence(t, ctx, pool, reporterID, "rejected", "rejected")
	request(t, reporter, http.MethodPost, "/api/v1/reports", strings.TrimSuffix(reportBody, "}")+fmt.Sprintf(`,"evidenceIds":[%q]}`, rejected), http.StatusBadRequest)
	request(t, reporter, http.MethodPost, "/api/v1/reports/evidence/"+rejected+"/access", `{}`, http.StatusNotFound)
	created := request(t, reporter, http.MethodPost, "/api/v1/reports", strings.TrimSuffix(reportBody, "}")+fmt.Sprintf(`,"evidenceIds":[%q]}`, evidence), http.StatusCreated)
	var envelope struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created, &envelope); err != nil || envelope.Data.ID == "" {
		t.Fatalf("report identity missing: %s err=%v", created, err)
	}
	reportPublicID := envelope.Data.ID
	path := "/api/v1/admin/reports/" + reportPublicID
	request(t, reporter, http.MethodPost, "/api/v1/reports", reportBody, http.StatusConflict)
	request(t, reporter, http.MethodGet, path, "", http.StatusForbidden)
	request(t, reporter, http.MethodGet, "/api/v1/admin/reports", "", http.StatusForbidden)
	assertCount(t, `select count(*) from report_snapshots where report_id=(select id from reports where public_id=$1)
		and payload->>'id'=$2 and content_sha256 ~ '^[a-f0-9]{64}$'`, 1, reportPublicID, authorPublicID)
	assertCount(t, `select count(*) from report_evidence where public_id=$1 and status='bound' and cleanup_after='infinity'`, 1, evidence)
	request(t, reporter, http.MethodPost, "/api/v1/reports/evidence/"+evidence+"/access", `{}`, http.StatusOK)
	request(t, reviewers[0], http.MethodPost, "/api/v1/reports/evidence/"+evidence+"/access", `{}`, http.StatusForbidden)
	grantTEST044Permissions(t, ctx, pool, reviewerIDs[0], "report.evidence.view")
	request(t, reviewers[0], http.MethodPost, "/api/v1/reports/evidence/"+evidence+"/access", `{}`, http.StatusOK)
	resolveBody := `{"conclusion":"invalid","note":"not upheld","idempotencyKey":"048resolve"}`
	request(t, reviewers[0], http.MethodPost, path+"/resolve", resolveBody, http.StatusConflict)
	request(t, reporter, http.MethodPost, path+"/claim", `{}`, http.StatusForbidden)
	t.Run("two reviewers race and only claimant may resolve", func(t *testing.T) {
		statuses := runTEST048ConcurrentRequests(t, ctx, client, httpServer.URL, reviewers, path+"/claim", `{}`)
		if fmt.Sprint(statuses) != "[200 409]" {
			t.Fatalf("claim statuses=%v", statuses)
		}
		var claimedBy int64
		if err := pool.QueryRow(ctx, `select claimed_by from reports where public_id=$1`, reportPublicID).Scan(&claimedBy); err != nil {
			t.Fatal(err)
		}
		if claimedBy == reviewerIDs[1] {
			reviewers[0], reviewers[1] = reviewers[1], reviewers[0]
			reviewerIDs[0], reviewerIDs[1] = reviewerIDs[1], reviewerIDs[0]
		}
		assertCount(t, `select count(*) from report_assignment_events where report_id=(select id from reports where public_id=$1)`, 1, reportPublicID)
		request(t, reviewers[1], http.MethodPost, path+"/resolve", resolveBody, http.StatusConflict)
		request(t, reviewers[1], http.MethodPost, path+"/takeover", `{"reason":"audited handoff"}`, http.StatusForbidden)
		grantTEST044Permissions(t, ctx, pool, reviewerIDs[1], "report.action.takeover", "report.action.reopen", "report.action.delete", "report.action.ban")
		request(t, reviewers[1], http.MethodPost, path+"/takeover", `{"reason":""}`, http.StatusBadRequest)
		request(t, reviewers[1], http.MethodPost, path+"/takeover", `{"reason":"audited handoff"}`, http.StatusOK)
		request(t, reviewers[0], http.MethodPost, path+"/resolve", resolveBody, http.StatusConflict)
		assertCount(t, `select count(*) from report_assignment_events where report_id=(select id from reports where public_id=$1)
			and action='takeover' and previous_assignee_id=$2 and assignee_id=$3 and reason='audited handoff'`, 1, reportPublicID, reviewerIDs[0], reviewerIDs[1])
	})
	t.Run("invalid punishments and failed outbox roll the whole transaction back", func(t *testing.T) {
		request(t, reviewers[1], http.MethodPost, path+"/resolve", `{"conclusion":"invalid","deleteTarget":true,"idempotencyKey":"invalid-hide"}`, http.StatusBadRequest)
		request(t, reviewers[1], http.MethodPost, path+"/resolve", fmt.Sprintf(`{"conclusion":"invalid","banUserId":%q,"idempotencyKey":"invalid-ban"}`, authorPublicID), http.StatusBadRequest)
		request(t, reviewers[0], http.MethodPost, path+"/resolve", `{"conclusion":"valid","deleteTarget":true,"idempotencyKey":"unprivileged-hide"}`, http.StatusForbidden)
		past := time.Now().Add(-time.Hour).Format(time.RFC3339Nano)
		request(t, reviewers[1], http.MethodPost, path+"/resolve", fmt.Sprintf(`{"conclusion":"valid","banUserId":%q,"banEndsAt":%q,"idempotencyKey":"elapsed-ban"}`, authorPublicID, past), http.StatusBadRequest)
		if _, err := pool.Exec(ctx, `alter table nats_outbox add constraint test048_reject_notification check(event_type <> 'report.resolved') not valid`); err != nil {
			t.Fatal(err)
		}
		request(t, reviewers[1], http.MethodPost, path+"/resolve", resolveBody, http.StatusInternalServerError)
		if _, err := pool.Exec(ctx, `alter table nats_outbox drop constraint test048_reject_notification`); err != nil {
			t.Fatal(err)
		}
		assertCount(t, `select count(*) from report_reviews`, 0)
		assertCount(t, `select count(*) from moderation_actions`, 0)
		assertCount(t, `select count(*) from ban_records`, 0)
		assertCount(t, `select count(*) from reports where public_id=$1 and status='in_review' and claimed_by=$2`, 1, reportPublicID, reviewerIDs[1])
		assertCount(t, `select count(*) from report_evidence where public_id=$1 and status='bound'`, 1, evidence)
	})
	t.Run("concurrent resolution is once only and reopen cannot revive deleted evidence", func(t *testing.T) {
		statuses := runTEST048ConcurrentRequests(t, ctx, client, httpServer.URL, []string{reviewers[1], reviewers[1]}, path+"/resolve", resolveBody)
		if fmt.Sprint(statuses) != "[200 409]" {
			t.Fatalf("resolve statuses=%v", statuses)
		}
		assertCount(t, `select count(*) from report_reviews`, 1)
		assertCount(t, `select count(*) from nats_outbox where event_type='report.resolved'`, 1)
		assertCount(t, `select count(*) from report_evidence where public_id=$1 and status='pending_delete' and cleanup_after>now()`, 1, evidence)
		request(t, reviewers[0], http.MethodPost, path+"/reopen", `{"reason":"new evidence","idempotencyKey":"reopen-no-permission"}`, http.StatusForbidden)
		request(t, reviewers[1], http.MethodPost, path+"/reopen", `{"reason":"new evidence","idempotencyKey":"048reopen"}`, http.StatusOK)
		assertCount(t, `select count(*) from report_evidence where public_id=$1 and status='bound' and cleanup_after='infinity'`, 1, evidence)
		request(t, reviewers[1], http.MethodPost, path+"/claim", `{}`, http.StatusOK)
		request(t, reviewers[1], http.MethodPost, path+"/resolve", strings.ReplaceAll(resolveBody, "048resolve", "048resolve-again"), http.StatusOK)
		if _, err := pool.Exec(ctx, `update report_evidence set cleanup_after=now()-interval '1 hour' where public_id=$1`, evidence); err != nil {
			t.Fatal(err)
		}
		// Hold the report row so the real reopen HTTP transaction waits while
		// the real cleanup worker commits deletion. Reopen must recheck the
		// evidence predicate, not turn deleted metadata back into bound.
		gate, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer gate.Rollback(context.Background())
		if _, err = gate.Exec(ctx, `select id from reports where public_id=$1 for update`, reportPublicID); err != nil {
			t.Fatal(err)
		}
		type reopenedResult struct {
			status int
			raw    []byte
			err    error
		}
		reopened := make(chan reopenedResult, 1)
		go func() {
			status, raw, requestErr := test048HTTPRequest(ctx, client, httpServer.URL, reviewers[1], http.MethodPost, path+"/reopen", `{"reason":"metadata review","idempotencyKey":"048reopen-deleted"}`)
			reopened <- reopenedResult{status, raw, requestErr}
		}()
		waitTEST048ReopenLock(t, ctx, pool)
		worker := &MaintenanceWorker{db: pool, cache: server.cache}
		worker.pruneReportEvidence(ctx)
		assertCount(t, `select count(*) from report_evidence where public_id=$1 and status='deleted' and delete_attempts=1`, 1, evidence)
		assertCount(t, `select count(*) from oss_files where object_key='test048/own.txt' and status='deleted'`, 1)
		if err = gate.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		result := <-reopened
		if result.err != nil || result.status != http.StatusOK {
			t.Fatalf("concurrent reopen status=%d err=%v body=%s", result.status, result.err, result.raw)
		}
		assertCount(t, `select count(*) from report_evidence where public_id=$1 and status='deleted'`, 1, evidence)
		request(t, reporter, http.MethodPost, "/api/v1/reports/evidence/"+evidence+"/access", `{}`, http.StatusNotFound)
	})
	t.Run("valid hide and ban share one rollback and one committed punishment", func(t *testing.T) {
		punishedID, punishedPublicID, punishedToken := createTEST044User(t, ctx, pool, cfg, "048punished", "UTC")
		if _, err := pool.Exec(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
			values('test048m2','test048-public','Public governance target','approved',$1)`, punishedID); err != nil {
			t.Fatal(err)
		}
		created := request(t, reporter, http.MethodPost, "/api/v1/reports", `{"targetType":"mod","targetId":"test048m2","reasonCode":"malware"}`, http.StatusCreated)
		var response struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(created, &response); err != nil || response.Data.ID == "" {
			t.Fatalf("new report response=%s err=%v", created, err)
		}
		punitivePath := "/api/v1/admin/reports/" + response.Data.ID
		request(t, reviewers[1], http.MethodPost, punitivePath+"/claim", `{}`, http.StatusOK)
		body := fmt.Sprintf(`{"conclusion":"valid","deleteTarget":true,"banUserId":%q,"banReasonCode":"malware","banEndsAt":%q,"idempotencyKey":"048punishment"}`, punishedPublicID, time.Now().Add(2*time.Hour).UTC().Format(time.RFC3339Nano))
		if _, err := pool.Exec(ctx, `alter table nats_outbox add constraint test048_reject_punishment check(event_type <> 'report.resolved') not valid`); err != nil {
			t.Fatal(err)
		}
		request(t, reviewers[1], http.MethodPost, punitivePath+"/resolve", body, http.StatusInternalServerError)
		if _, err := pool.Exec(ctx, `alter table nats_outbox drop constraint test048_reject_punishment`); err != nil {
			t.Fatal(err)
		}
		assertCount(t, `select count(*) from mods where project_code='test048m2' and review_status='approved'`, 1)
		assertCount(t, `select count(*) from ban_records where user_id=$1`, 0, punishedID)
		assertCount(t, `select count(*) from user_role_bindings where user_id=$1 and source='governance_ban'`, 0, punishedID)
		assertCount(t, `select count(*) from users where id=$1 and auth_version=1`, 1, punishedID)
		assertCount(t, `select count(*) from report_reviews where report_id=(select id from reports where public_id=$1)`, 0, response.Data.ID)
		assertCount(t, `select count(*) from moderation_actions where target_public_id='test048m2'`, 0)
		request(t, punishedToken, http.MethodGet, "/api/v1/users/me/profile-settings", "", http.StatusOK)
		request(t, reviewers[1], http.MethodPost, punitivePath+"/resolve", body, http.StatusOK)
		request(t, reviewers[1], http.MethodPost, punitivePath+"/resolve", body, http.StatusConflict)
		assertCount(t, `select count(*) from mods where project_code='test048m2' and review_status='rejected'`, 1)
		assertCount(t, `select count(*) from ban_records where user_id=$1 and status='active'`, 1, punishedID)
		assertCount(t, `select count(*) from moderation_actions where target_public_id='test048m2' and action_type='delete'`, 1)
		assertCount(t, `select count(*) from nats_outbox where event_type='ban.created' and aggregate_id=$1`, 1, fmt.Sprint(punishedID))
		request(t, punishedToken, http.MethodGet, "/api/v1/users/me/profile-settings", "", http.StatusUnauthorized)
	})
	t.Run("temporary bans revoke old sessions and public reads cannot leak internal notes", func(t *testing.T) {
		grantTEST044Permissions(t, ctx, pool, reviewerIDs[1], "ban.create", "ban.view_internal", "ban.revoke")
		future := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339Nano)
		banBody := fmt.Sprintf(`{"userId":%q,"reasonCode":"harassment","endsAt":%q,"publicRecordMarkdown":"Public sanction","internalNote":"TEST048-CONFIDENTIAL"}`, authorPublicID, future)
		request(t, reporter, http.MethodPost, "/api/v1/admin/bans", banBody, http.StatusForbidden)
		request(t, reviewers[1], http.MethodPost, "/api/v1/admin/bans", banBody, http.StatusCreated)
		request(t, author, http.MethodGet, "/api/v1/users/me/profile-settings", "", http.StatusUnauthorized)
		fresh := mintTEST048Token(t, ctx, pool, cfg, authorID)
		request(t, fresh, http.MethodGet, "/api/v1/users/me/profile-settings", "", http.StatusOK)
		request(t, fresh, http.MethodPost, "/api/v1/reports", reportBody, http.StatusForbidden)
		var banPublicID string
		var banID int64
		if err := pool.QueryRow(ctx, `select id,public_id from ban_records where user_id=$1`, authorID).Scan(&banID, &banPublicID); err != nil {
			t.Fatal(err)
		}
		publicList := request(t, "", http.MethodGet, "/api/v1/site-affairs/blackroom", "", http.StatusOK)
		publicDetail := request(t, "", http.MethodGet, "/api/v1/site-affairs/blackroom/"+banPublicID, "", http.StatusOK)
		for _, raw := range [][]byte{publicList, publicDetail} {
			if bytes.Contains(raw, []byte("TEST048-CONFIDENTIAL")) || bytes.Contains(raw, []byte("internalNote")) {
				t.Fatalf("public ban DTO leaked internal facts: %s", raw)
			}
		}
		request(t, reporter, http.MethodGet, "/api/v1/admin/bans", "", http.StatusForbidden)
		internal := request(t, reviewers[1], http.MethodGet, "/api/v1/admin/bans", "", http.StatusOK)
		if !bytes.Contains(internal, []byte("TEST048-CONFIDENTIAL")) {
			t.Fatalf("authorized internal DTO lost note: %s", internal)
		}
		request(t, reviewers[1], http.MethodPost, "/api/v1/admin/bans", banBody, http.StatusBadRequest)
		assertCount(t, `select count(*) from ban_records where user_id=$1`, 1, authorID)
		if _, err := pool.Exec(ctx, `insert into user_role_bindings(user_id,role_id,source,source_key)
			select $1,id,'manual','' from roles where code='banned'`, authorID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `update ban_records set starts_at=now()-interval '2 hours',ends_at=now()-interval '1 hour' where id=$1`, banID); err != nil {
			t.Fatal(err)
		}
		worker := &MaintenanceWorker{db: pool, cache: server.cache}
		worker.expireBans(ctx)
		worker.expireBans(ctx)
		assertCount(t, `select count(*) from ban_records where id=$1 and status='expired'`, 1, banID)
		assertCount(t, `select count(*) from user_role_bindings where user_id=$1 and source='governance_ban'`, 0, authorID)
		assertCount(t, `select count(*) from user_role_bindings where user_id=$1 and source='manual' and role_id=(select id from roles where code='banned')`, 1, authorID)
		request(t, fresh, http.MethodGet, "/api/v1/users/me/profile-settings", "", http.StatusOK)
		request(t, author, http.MethodGet, "/api/v1/users/me/profile-settings", "", http.StatusUnauthorized)
		request(t, reviewers[1], http.MethodPost, "/api/v1/admin/bans/"+banPublicID+"/revoke", `{"reason":"already expired"}`, http.StatusConflict)
		publicDetail = request(t, "", http.MethodGet, "/api/v1/site-affairs/blackroom/"+banPublicID, "", http.StatusOK)
		if !bytes.Contains(publicDetail, []byte(`"status":"released"`)) {
			t.Fatalf("expired ban not released: %s", publicDetail)
		}
	})
}

func test048HTTPRequest(ctx context.Context, client *http.Client, origin, token, method, path, body string) (int, []byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, origin+path, strings.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return response.StatusCode, raw, err
}

func waitTEST048ReopenLock(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiting bool
		if err := pool.QueryRow(ctx, `select exists(select 1 from pg_stat_activity where datname=current_database()
			and wait_event_type='Lock' and query like 'update reports set status=''pending''%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("reopen never waited on the held report row")
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-tick.C:
		}
	}
}

func runTEST048ConcurrentRequests(t *testing.T, ctx context.Context, client *http.Client, origin string, tokens []string, path, body string) []int {
	t.Helper()
	start := make(chan struct{})
	type result struct {
		status int
		raw    []byte
		err    error
	}
	results := make(chan result, len(tokens))
	var ready sync.WaitGroup
	ready.Add(len(tokens))
	for _, token := range tokens {
		go func() {
			ready.Done()
			<-start
			status, raw, err := test048HTTPRequest(ctx, client, origin, token, http.MethodPost, path, body)
			results <- result{status, raw, err}
		}()
	}
	ready.Wait()
	close(start)
	statuses := make([]int, 0, len(tokens))
	for range tokens {
		item := <-results
		if item.err != nil {
			t.Errorf("concurrent request %s: %v", path, item.err)
		}
		if item.status != http.StatusOK && item.status != http.StatusConflict {
			t.Errorf("concurrent request %s status=%d body=%s", path, item.status, item.raw)
		}
		statuses = append(statuses, item.status)
	}
	sort.Ints(statuses)
	return statuses
}

func newTEST048Evidence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, uploaderID int64, suffix, scan string) string {
	t.Helper()
	key := "test048/" + suffix + ".txt"
	if _, err := pool.Exec(ctx, `insert into oss_files(object_key,uploader_id,bucket,endpoint,region,size_bytes,source_size_bytes,scan_status)
		values($1,$2,'test048-only','https://oss.example.invalid','test',1,1,'clean')`, key, uploaderID); err != nil {
		t.Fatal(err)
	}
	var publicID string
	if err := pool.QueryRow(ctx, `insert into report_evidence(uploader_id,object_key,original_name,content_type,byte_size,sha256,scan_status)
		values($1,$2,'evidence.txt','text/plain',1,repeat('a',64),$3) returning public_id`, uploaderID, key, scan).Scan(&publicID); err != nil {
		t.Fatal(err)
	}
	return publicID
}

func mintTEST048Token(t *testing.T, ctx context.Context, pool *pgxpool.Pool, cfg config.Config, userID int64) string {
	t.Helper()
	var publicID, username, email string
	var authVersion int64
	if err := pool.QueryRow(ctx, `select public_id,username,email,auth_version from users where id=$1`, userID).Scan(&publicID, &username, &email, &authVersion); err != nil {
		t.Fatal(err)
	}
	claims, err := security.NewClaims(publicID, username, email, authVersion, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into auth_sessions(session_hash,user_id,auth_version,expires_at) values($1,$2,$3,$4)`, security.SessionFingerprint(claims.SessionID), userID, authVersion, time.Unix(claims.ExpiresAt, 0)); err != nil {
		t.Fatal(err)
	}
	token, err := security.SignToken(cfg.JWTSecret, claims)
	if err != nil {
		t.Fatal(err)
	}
	return token
}
