package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestModRevisionHistoryPaginatesOneHundredThousandRowsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify Mod revision history scale")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	var submitterID, modID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('history_scale_submitter','history-scale@example.invalid','test-only',true) returning id`).Scan(&submitterID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values('history01','history-scale','History scale','approved',$1) returning id`, submitterID).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `alter table content_revisions disable trigger trg_content_revisions_metrics`); err != nil {
		t.Fatal(err)
	}
	fixtureStarted := time.Now()
	if _, err = pool.Exec(ctx, `insert into content_revisions(
		entity_type,entity_id,aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash,created_by,source,created_at)
		select 'mod',$1,'mod','history01',series,
			jsonb_build_object('siteId','history-scale','primaryName','Revision '||series::text),
			'history-scale-'||series::text,$2,'test',timestamptz '2026-01-01 00:00:00+00'+series*interval '1 second'
		from generate_series(1,100000) series`, modID, submitterID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into change_requests(
		entity_type,entity_id,aggregate_type,aggregate_key,proposed_revision_id,status,reason,submitted_by,submitted_at)
		select revision.entity_type,revision.entity_id,revision.aggregate_type,revision.aggregate_key,revision.id,
			'approved','revision '||revision.revision_no::text,$1,revision.created_at
		from content_revisions revision where revision.aggregate_type='mod' and revision.aggregate_key='history01'`, submitterID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into review_events(change_request_id,event_type,actor_id,actor_snapshot,note,created_at)
		select request.id,'approved',$1,'history_scale_submitter','approved '||revision.revision_no::text,revision.created_at
		from change_requests request join content_revisions revision on revision.id=request.proposed_revision_id
		where request.aggregate_type='mod' and request.aggregate_key='history01'`, submitterID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `analyze content_revisions; analyze change_requests; analyze review_events`); err != nil {
		t.Fatal(err)
	}
	t.Logf("100k fixture loaded in %s", time.Since(fixtureStarted))

	type historyPage struct {
		Items      []modRevisionResponse `json:"items"`
		Limit      int                   `json:"limit"`
		HasMore    bool                  `json:"hasMore"`
		NextCursor string                `json:"nextCursor"`
	}
	server := &Server{db: pool}
	loadPage := func(target string) (historyPage, int) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.SetPathValue("siteId", "history-scale")
		request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{}))
		response := httptest.NewRecorder()
		server.modRevisionHistory(response, request)
		if response.Code != http.StatusOK {
			return historyPage{}, response.Code
		}
		var envelope struct {
			Data historyPage `json:"data"`
		}
		if decodeErr := json.Unmarshal(response.Body.Bytes(), &envelope); decodeErr != nil {
			t.Fatal(decodeErr)
		}
		return envelope.Data, response.Code
	}

	first, status := loadPage("/api/v1/mods/history-scale/revisions?limit=50")
	if status != http.StatusOK || first.Limit != 50 || len(first.Items) != 50 || !first.HasMore || first.NextCursor == "" ||
		first.Items[0].Version != 100000 || first.Items[49].Version != 99951 {
		t.Fatalf("first page status=%d page=%#v", status, first)
	}
	second, status := loadPage("/api/v1/mods/history-scale/revisions?limit=50&cursor=" + first.NextCursor)
	if status != http.StatusOK || len(second.Items) != 50 || second.Items[0].Version != 99950 || second.Items[49].Version != 99901 {
		t.Fatalf("second page status=%d page=%#v", status, second)
	}
	firstIDs := make(map[string]struct{}, len(first.Items))
	for _, item := range first.Items {
		firstIDs[item.ID] = struct{}{}
	}
	for _, item := range second.Items {
		if _, duplicate := firstIDs[item.ID]; duplicate {
			t.Fatalf("revision %s was duplicated across cursor pages", item.ID)
		}
	}

	deepCursor := encodeModRevisionHistoryPageCursor(modRevisionHistoryPageCursor{
		Version: modRevisionHistoryCursorVersion,
		Scope:   modRevisionHistoryPageScope("history01", 50), RevisionNo: 51,
	})
	deep, status := loadPage("/api/v1/mods/history-scale/revisions?limit=50&cursor=" + deepCursor)
	if status != http.StatusOK || len(deep.Items) != 50 || deep.HasMore || deep.NextCursor != "" ||
		deep.Items[0].Version != 50 || deep.Items[49].Version != 1 {
		t.Fatalf("deep page status=%d page=%#v", status, deep)
	}
	foreignCursor := encodeModRevisionHistoryPageCursor(modRevisionHistoryPageCursor{
		Version: modRevisionHistoryCursorVersion,
		Scope:   modRevisionHistoryPageScope("another01", 50), RevisionNo: 51,
	})
	if _, status = loadPage("/api/v1/mods/history-scale/revisions?limit=50&cursor=" + foreignCursor); status != http.StatusBadRequest {
		t.Fatalf("foreign cursor returned %d; want 400", status)
	}

	request, err := parseModRevisionHistoryPageRequest(map[string][]string{
		"limit": {"50"}, "cursor": {deepCursor},
	}, "history01")
	if err != nil {
		t.Fatal(err)
	}
	query, arguments := modRevisionHistoryPageSQL("history01", pendingReviewVisibility{}, request)
	planRows, err := pool.Query(ctx, "explain (analyze,buffers,format text) "+query, arguments...)
	if err != nil {
		t.Fatal(err)
	}
	planLines := make([]string, 0)
	for planRows.Next() {
		var line string
		if err = planRows.Scan(&line); err != nil {
			planRows.Close()
			t.Fatal(err)
		}
		planLines = append(planLines, line)
	}
	if err = planRows.Err(); err != nil {
		planRows.Close()
		t.Fatal(err)
	}
	planRows.Close()
	plan := strings.Join(planLines, "\n")
	t.Logf("deep cursor plan:\n%s", plan)
	if !strings.Contains(plan, "idx_content_revisions_aggregate") || !strings.Contains(plan, "idx_review_events_request_created") ||
		strings.Contains(plan, "Seq Scan on content_revisions") || strings.Contains(plan, "Seq Scan on review_events") || strings.Contains(plan, "SubPlan") {
		t.Fatalf("deep cursor did not use the bounded aggregate index without correlated subplans:\n%s", plan)
	}
}
