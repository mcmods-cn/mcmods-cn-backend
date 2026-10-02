package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestOCT02AIAdminCreateDeduplicatesConcurrentSourceSnapshotsIntegration(t *testing.T) {
	pool := oct02AITestPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `alter table ai_tasks add column priority integer default 0,add column concurrency_key text,add column queued_at timestamptz;
 create table nats_outbox(event_id text,event_type text,schema_version integer,subject text,aggregate_type text,aggregate_id text,trace_id text,payload jsonb,occurred_at timestamptz,status text,available_at timestamptz)`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool, cfg: config.Config{SettingsEncryptionKey: "fixture-settings-key-at-least-32-characters"}}
	cfg := defaultAIConfig()
	cfg.Providers[0].Enabled = true
	cfg.Providers[0].APIKey = "fixture-only"
	cfg.Models[0].Enabled = true
	cfg.Models[0].MaxOutputTokens = 100
	for i := range cfg.TaskModels {
		cfg.TaskModels[i].ModelKey = "openai/gpt-4.1-mini"
	}
	sealed, err := server.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings values('ai.config',$1)`, sealed); err != nil {
		t.Fatal(err)
	}
	invoke := func(text string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"taskType": aiTaskI18nTranslation, "concurrencyKey": "same-client-key", "payload": map[string]any{"targetLocale": "zh-CN", "items": []any{map[string]any{"key": "title", "text": text}}}})
		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/ai/tasks", strings.NewReader(string(body)))
		request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 42}))
		response := httptest.NewRecorder()
		server.createAITask(response, request)
		return response
	}
	var group sync.WaitGroup
	responses := make(chan *httptest.ResponseRecorder, 8)
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() { defer group.Done(); responses <- invoke("source version one") }()
	}
	group.Wait()
	close(responses)
	created, accepted := 0, 0
	id := ""
	for response := range responses {
		if response.Code == http.StatusCreated {
			created++
		} else if response.Code == http.StatusAccepted {
			accepted++
		} else {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
		var result struct {
			Data struct {
				ID string `json:"id"`
			}
		}
		if err = json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if id != "" && id != result.Data.ID {
			t.Fatal("same source admitted more than one task")
		}
		id = result.Data.ID
	}
	if created != 1 || accepted != 7 {
		t.Fatalf("create/reuse=%d/%d, want 1/7", created, accepted)
	}
	if changed := invoke("source version two"); changed.Code != http.StatusCreated {
		t.Fatalf("changed snapshot status=%d body=%s", changed.Code, changed.Body.String())
	}
	var tasks, events int
	if err = pool.QueryRow(ctx, `select (select count(*) from ai_tasks),(select count(*) from nats_outbox)`).Scan(&tasks, &events); err != nil || tasks != 2 || events != 2 {
		t.Fatalf("durable tasks/outbox=%d/%d error=%v", tasks, events, err)
	}
}

func TestOCT02AIBudgetStatsPreserveActualAndUnknownUsageIntegration(t *testing.T) {
	pool := oct02AITestPool(t)
	ctx := context.Background()
	worker := NewAIWorker(pool, nil, "")
	prepared := preparedAITask{provider: aiProviderConfig{Code: "fixture"}, model: aiModelConfig{Model: "fixture", InputPricePerMillion: 2}, reservationTokens: 100}
	id, budget, err := worker.reserveProviderRequestBudget(ctx, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.settleProviderRequestBudget(ctx, id, budget, aiTaskUsage{InputTokens: 7, OutputTokens: 3, CostMicros: 14}); err != nil {
		t.Fatal(err)
	}
	id, budget, err = worker.reserveProviderRequestBudget(ctx, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.settleProviderRequestBudget(ctx, id, budget, aiTaskUsage{}); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	(&Server{db: pool}).adminAIStats(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/ai/stats", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("stats status=%d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Data struct {
			ByStatus      []any `json:"byStatus"`
			ByProvider    []any `json:"byProvider"`
			RequestBudget []struct {
				State          string `json:"state"`
				Requests       int64  `json:"requests"`
				InputTokens    int64  `json:"input_tokens"`
				ReservedTokens int64  `json:"reserved_tokens"`
			} `json:"requestBudget"`
		}
	}
	if err = json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	states := map[string]bool{}
	for _, row := range result.Data.RequestBudget {
		if row.Requests != 1 {
			t.Fatalf("requests=%d", row.Requests)
		}
		if row.State == "settled" {
			states[row.State] = true
			if row.InputTokens != 7 || row.ReservedTokens != 0 {
				t.Fatalf("settled row=%#v", row)
			}
		}
		if row.State == "usage_unknown" {
			states[row.State] = true
			if row.InputTokens != 0 || row.ReservedTokens != 100 {
				t.Fatalf("unknown row=%#v", row)
			}
		}
	}
	if !states["settled"] || !states["usage_unknown"] {
		t.Fatalf("states=%#v", states)
	}
}

func TestOCT02AIResourceTranslationRequiresResourceViewEvenForCachedResultIntegration(t *testing.T) {
	pool := oct02AITestPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `alter table public_routes add column canonical_path text;
 alter table content_localizations add column summary text default '',add column content_markdown text default '',add column source_locale text default '',add column editable boolean default true,add column updated_at timestamptz default now();
 create table blueprints(id bigint,status text,review_status text);create table skin_assets(id bigint,status text,review_status text,visibility text);
 create table catalog_entities(id bigint,public_id text,status text,archived_at timestamptz);create table recipe_layout_templates(entity_id bigint,recipe_type_id bigint);create table recipes(entity_id bigint,recipe_type_id bigint);
 insert into public_routes values(7,'r00000007','resource','/admin/global-resources?publicId=r00000007');
 insert into content_subjects values(7,'resource','en-US');
 insert into content_localizations(subject_id,subject_type,locale,revision_no,review_status,provenance,name) values(7,'resource','zh-CN',1,'approved','human','restricted content')`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	for _, allowed := range []bool{false, true} {
		claims := security.Claims{Subject: 42}
		if allowed {
			claims.PermissionRules = []security.PermissionRule{{Code: "global_resource.view", Allow: true}}
		}
		request := httptest.NewRequest(http.MethodPost, "/api/v1/content/r00000007/translations", strings.NewReader(`{"targetLocale":"zh-CN"}`))
		request.SetPathValue("publicId", "r00000007")
		request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
		response := httptest.NewRecorder()
		server.requestCatalogContentTranslation(response, request)
		want := http.StatusNotFound
		if allowed {
			want = http.StatusOK
		}
		if response.Code != want {
			t.Fatalf("allowed=%v status=%d want=%d body=%s", allowed, response.Code, want, response.Body.String())
		}
		if !allowed && strings.Contains(response.Body.String(), "restricted content") {
			t.Fatal("resource content leaked to unauthorized translation caller")
		}
	}
}
