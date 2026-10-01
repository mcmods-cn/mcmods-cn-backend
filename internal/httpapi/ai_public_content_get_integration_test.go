package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type publicTranslationSubject struct {
	ID                   int64
	PublicID, EntityType string
}

func publicTranslationFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, entityType string, id int64) publicTranslationSubject {
	t.Helper()
	fixture := publicTranslationSubject{EntityType: entityType}
	var err error
	if entityType == "mod" {
		if id == 0 {
			err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status) values(new_public_id(),'public-ai-'||gen_random_uuid()::text,'Synthetic source','approved') returning id,project_code`).Scan(&fixture.ID, &fixture.PublicID)
		} else {
			err = pool.QueryRow(ctx, `insert into mods(id,project_code,slug,primary_name,review_status) values($1,new_public_id(),'public-ai-'||gen_random_uuid()::text,'Synthetic source','approved') returning id,project_code`, id).Scan(&fixture.ID, &fixture.PublicID)
		}
	} else {
		err = pool.QueryRow(ctx, `insert into catalog_entities(id,identity_key,entity_type) values($1,'audit:public-ai:'||gen_random_uuid()::text,$2) returning id,public_id`, id, entityType).Scan(&fixture.ID, &fixture.PublicID)
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into content_localizations(subject_type,subject_id,locale,name,summary,content_markdown,provenance,review_status,revision_no) values($1,$2,'zh-CN','源名称','源简介','源正文','human','approved',1)`, fixture.EntityType, fixture.ID); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func publicTranslationGET(ctx context.Context, client *http.Client, baseURL, publicID, locale string) (contentResolutionResponse, error) {
	var result struct {
		Data contentResolutionResponse `json:"data"`
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/content/"+publicID+"?locale="+locale, nil)
	if err != nil {
		return result.Data, err
	}
	response, err := client.Do(r)
	if err != nil {
		return result.Data, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return result.Data, fmt.Errorf("public content GET status=%d", response.StatusCode)
	}
	if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
		return result.Data, err
	}
	if response.Header.Get("Content-Language") != result.Data.ResolvedLocale {
		return result.Data, fmt.Errorf("fallback Content-Language disagrees with response locale")
	}
	return result.Data, nil
}

func publicTranslationWave(t *testing.T, ctx context.Context, client *http.Client, baseURL string, fixture publicTranslationSubject, locale, expectedStatus string, count int) string {
	t.Helper()
	results := make(chan contentResolutionResponse, count)
	errors := make(chan error, count)
	var group sync.WaitGroup
	for range count {
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := publicTranslationGET(ctx, client, baseURL, fixture.PublicID, locale)
			if err != nil {
				errors <- err
				return
			}
			results <- result
		}()
	}
	group.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	uid := ""
	for result := range results {
		if result.Translation.Status != expectedStatus || result.Localization == nil {
			t.Errorf("GET status=%s source=%v expected=%s", result.Translation.Status, result.Localization != nil, expectedStatus)
		}
		if result.Translation.TaskID != nil {
			if uid != "" && uid != *result.Translation.TaskID {
				t.Error("same source refresh returned distinct tasks")
			}
			uid = *result.Translation.TaskID
		}
	}
	return uid
}

func publicTranslationTask(t *testing.T, ctx context.Context, pool *pgxpool.Pool, uid string) (aiTaskMessage, contentTranslationTaskPayload, string) {
	t.Helper()
	var msg aiTaskMessage
	var payload contentTranslationTaskPayload
	var raw []byte
	var key string
	if err := pool.QueryRow(ctx, `select id,task_uid,task_type,payload,concurrency_key from ai_tasks where task_uid=$1`, uid).Scan(&msg.TaskID, &msg.TaskUID, &msg.TaskType, &raw, &key); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	return msg, payload, key
}

func publicTranslationCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture publicTranslationSubject, expected int) {
	t.Helper()
	var tasks, outbox int
	if err := pool.QueryRow(ctx, `select count(*),count(o.id) from ai_tasks t left join nats_outbox o on o.aggregate_id=t.task_uid where t.payload->>'publicId'=$1`, fixture.PublicID).Scan(&tasks, &outbox); err != nil || tasks != expected || outbox != expected {
		t.Fatalf("task/outbox=%d/%d expected=%d error=%v", tasks, outbox, expected, err)
	}
}

func TestAIPublicContentGETAutomaticTranslationBoundariesIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	cfg.NATS.OutboxEnabled = true
	var calls atomic.Int64
	var fail atomic.Bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		calls.Add(1)
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"items":[{"key":"name","text":"Translated name"},{"key":"summary","text":"Translated summary"},{"key":"contentMarkdown","text":"Translated content"}]}`}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 11, "completion_tokens": 7}})
	}))
	defer provider.Close()
	settings := aiTestConfig(provider.URL)
	setAITestConfig(t, ctx, pool, cfg, settings)
	server := &Server{db: pool, cfg: cfg}
	// Same registered method/path, optional authentication and actual handler as
	// Server.routes. Only the supplier is replaced; our API/SQL are not mocked.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/content/{publicId}", server.optionalAuth(server.catalogEntityContent))
	api := httptest.NewServer(mux)
	defer api.Close()
	worker := NewAIWorker(pool, nil, cfg.SettingsEncryptionKey)
	fixture := publicTranslationFixture(t, ctx, pool, "mod", 0)
	t.Run("disabled automatic translation preserves fallback and never reserves or calls", func(t *testing.T) {
		if settings.Translation.Enabled {
			t.Fatal("fixture must begin disabled with an otherwise usable provider")
		}
		uid := publicTranslationWave(t, ctx, api.Client(), api.URL, fixture, "en-US", "unavailable", 24)
		if uid != "" || calls.Load() != 0 {
			t.Fatal("disabled public GET produced a task/provider request")
		}
		publicTranslationCounts(t, ctx, pool, fixture, 0)
	})
	settings.Translation.Enabled = true
	setAITestConfig(t, ctx, pool, cfg, settings)
	t.Run("concurrent enabled refresh reserves once and published translation is reused", func(t *testing.T) {
		uid := publicTranslationWave(t, ctx, api.Client(), api.URL, fixture, "en-US", "queued", 24)
		publicTranslationCounts(t, ctx, pool, fixture, 1)
		if uid == "" || calls.Load() != 0 {
			t.Fatal("GET itself called the provider or failed to expose a real queued task")
		}
		msg, payload, key := publicTranslationTask(t, ctx, pool, uid)
		if payload.EntityType != "mod" || payload.EntityID != fixture.ID || payload.SourceRevisionNo != 1 || !strings.HasPrefix(key, "mod:"+fixture.PublicID+":") {
			t.Fatal("task does not carry the actual source/type/public identity")
		}
		raw, _ := json.Marshal(msg)
		if err := worker.handleTask(ctx, raw); err != nil {
			t.Fatal(err)
		}
		publicTranslationWave(t, ctx, api.Client(), api.URL, fixture, "en-US", "ready", 24)
		publicTranslationCounts(t, ctx, pool, fixture, 1)
		if calls.Load() != 1 {
			t.Fatal("published translation refresh called provider again")
		}
		result, err := publicTranslationGET(ctx, api.Client(), api.URL, fixture.PublicID, "en-US")
		if err != nil || result.ResolvedLocale != "en-US" || result.Localization.Name != "Translated name" {
			t.Fatalf("real public target readback=%+v error=%v", result, err)
		}
	})
	for _, status := range []string{"failed", "cancelled"} {
		t.Run("same-version "+status+" is not repaid and changed source can create one fresh task", func(t *testing.T) {
			fixture := publicTranslationFixture(t, ctx, pool, "mod", 0)
			before := calls.Load()
			uid := publicTranslationWave(t, ctx, api.Client(), api.URL, fixture, "en-US", "queued", 8)
			msg, _, _ := publicTranslationTask(t, ctx, pool, uid)
			raw, _ := json.Marshal(msg)
			if status == "failed" {
				fail.Store(true)
				if err := worker.handleTask(ctx, raw); err == nil {
					t.Fatal("real HTTP 503 did not fail the task")
				}
				fail.Store(false)
				before++
			} else {
				r := httptest.NewRequest(http.MethodPost, "/api/v1/admin/ai/tasks/"+uid+"/cancel", nil).WithContext(ctx)
				r.SetPathValue("id", uid)
				response := httptest.NewRecorder()
				server.cancelAITask(response, r)
				if response.Code != http.StatusOK {
					t.Fatalf("cancel=%d", response.Code)
				}
			}
			if repeated := publicTranslationWave(t, ctx, api.Client(), api.URL, fixture, "en-US", status, 24); repeated != uid {
				t.Fatal("same source failed/cancelled task was replaced")
			}
			if err := worker.handleTask(ctx, raw); err != nil || calls.Load() != before {
				t.Fatalf("terminal redelivery called supplier: error=%v calls=%d expected=%d", err, calls.Load(), before)
			}
			publicTranslationCounts(t, ctx, pool, fixture, 1)
			if _, err := pool.Exec(ctx, `update content_localizations set name='新版源名称',revision_no=2,updated_at=clock_timestamp() where subject_type='mod' and subject_id=$1 and locale='zh-CN'`, fixture.ID); err != nil {
				t.Fatal(err)
			}
			newUID := publicTranslationWave(t, ctx, api.Client(), api.URL, fixture, "en-US", "queued", 24)
			msg, snapshot, _ := publicTranslationTask(t, ctx, pool, newUID)
			if newUID == uid || snapshot.SourceRevisionNo != 2 {
				t.Fatal("changed source did not receive a fresh version-specific identity")
			}
			publicTranslationCounts(t, ctx, pool, fixture, 2)
			raw, _ = json.Marshal(msg)
			if err := worker.handleTask(ctx, raw); err != nil || calls.Load() != before+1 {
				t.Fatalf("new source real worker=%v calls=%d expected=%d", err, calls.Load(), before+1)
			}
			publicTranslationWave(t, ctx, api.Client(), api.URL, fixture, "en-US", "ready", 8)
			publicTranslationCounts(t, ctx, pool, fixture, 2)
		})
	}
	t.Run("equal internal IDs in different public resource types cannot share tasks", func(t *testing.T) {
		mod := publicTranslationFixture(t, ctx, pool, "mod", 100000019)
		tag := publicTranslationFixture(t, ctx, pool, "tag", mod.ID)
		modUID := publicTranslationWave(t, ctx, api.Client(), api.URL, mod, "en-US", "queued", 8)
		tagUID := publicTranslationWave(t, ctx, api.Client(), api.URL, tag, "en-US", "queued", 8)
		_, modPayload, modKey := publicTranslationTask(t, ctx, pool, modUID)
		_, tagPayload, tagKey := publicTranslationTask(t, ctx, pool, tagUID)
		if modUID == tagUID || modKey == tagKey || modPayload.EntityID != tagPayload.EntityID || modPayload.EntityType != "mod" || tagPayload.EntityType != "tag" || !strings.HasPrefix(tagKey, "tag:"+tag.PublicID+":") {
			t.Fatal("public identity/type collided at equal numeric IDs")
		}
		publicTranslationCounts(t, ctx, pool, mod, 1)
		publicTranslationCounts(t, ctx, pool, tag, 1)
	})
}

func TestAIPublicContentGETSiteBudgetBoundsDifferentLocalesIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	cfg.NATS.OutboxEnabled = true
	var calls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer provider.Close()
	settings := aiTestConfig(provider.URL)
	settings.Translation.Enabled = true
	settings.Quotas[0].RequestLimit = 2
	setAITestConfig(t, ctx, pool, cfg, settings)
	server := &Server{db: pool, cfg: cfg}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/content/{publicId}", server.optionalAuth(server.catalogEntityContent))
	api := httptest.NewServer(mux)
	defer api.Close()
	fixture := publicTranslationFixture(t, ctx, pool, "mod", 0)
	worker := NewAIWorker(pool, nil, cfg.SettingsEncryptionKey)
	for _, locale := range []string{"en-US", "ja-JP"} {
		uid := publicTranslationWave(t, ctx, api.Client(), api.URL, fixture, locale, "queued", 16)
		msg, _, _ := publicTranslationTask(t, ctx, pool, uid)
		raw, _ := json.Marshal(msg)
		if err := worker.handleTask(ctx, raw); err == nil {
			t.Fatal("controlled provider failure must fail")
		}
	}
	for _, locale := range []string{"fr-FR", "de-DE", "es-ES", "ru-RU"} {
		uid := publicTranslationWave(t, ctx, api.Client(), api.URL, fixture, locale, "unavailable", 16)
		if uid != "" {
			t.Fatal("exhausted public GET budget still enqueued a task")
		}
	}
	for _, locale := range []string{"en-US", "ja-JP"} {
		publicTranslationWave(t, ctx, api.Client(), api.URL, fixture, locale, "failed", 16)
	}
	publicTranslationCounts(t, ctx, pool, fixture, 2)
	if calls.Load() != 2 {
		t.Fatalf("public locale refresh exceeded configured request budget: calls=%d", calls.Load())
	}
}
