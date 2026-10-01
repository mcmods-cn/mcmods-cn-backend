package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func aiFailureMessage(t *testing.T, taskID int64, uid, taskType string) []byte {
	t.Helper()
	raw, err := json.Marshal(aiTaskMessage{TaskID: taskID, TaskUID: uid, TaskType: taskType})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestAITaskTransportFailureDoesNotAutomaticallyPayAgainCoreIntegration(t *testing.T) {
	for _, scenario := range []string{"disconnect", "timeout", "caller_cancel"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, pool, cfg := isolatedAITestDatabase(t)
			var calls atomic.Int32
			started, stopped, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := io.Copy(io.Discard, r.Body); err != nil {
					t.Error(err)
					return
				}
				calls.Add(1)
				close(started)
				defer close(stopped)
				if scenario == "disconnect" {
					connection, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = connection.Close()
					return
				}
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}))
			defer provider.Close()
			defer close(release)
			settings := aiTestConfig(provider.URL)
			settings.Quotas[0].CostLimitCNY = 1
			for i := range settings.TaskModels {
				settings.TaskModels[i].TimeoutSeconds = 1
			}
			setAITestConfig(t, ctx, pool, cfg, settings)
			payload := map[string]any{"sourceLocale": "zh-CN", "targetLocale": "en-US", "items": []map[string]string{{"key": "greeting", "text": "你好"}}}
			taskID, uid := insertAITestTask(t, ctx, pool, aiTaskI18nTranslation, "queued", payload, 100)
			server := &Server{db: pool, cfg: cfg}
			server.cfg.NATS.OutboxEnabled = false
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err = server.enqueueAIOutboxTx(ctx, tx, taskID, uid, aiTaskI18nTranslation); err != nil {
				_ = tx.Rollback(ctx)
				t.Fatal(err)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			worker := NewAIWorker(pool, nil, cfg.SettingsEncryptionKey)
			requestCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			done := make(chan error, 1)
			message := aiFailureMessage(t, taskID, uid, aiTaskI18nTranslation)
			go func() { done <- worker.handleTask(requestCtx, message) }()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if scenario == "caller_cancel" {
				cancel()
			}
			select {
			case err = <-done:
				if err == nil {
					t.Fatal("transport failure was accepted as successful translation")
				}
			case <-time.After(4 * time.Second):
				t.Fatal("provider request did not terminate within its configured timeout")
			}
			select {
			case <-stopped:
			case <-time.After(2 * time.Second):
				t.Fatal("provider HTTP request was not cancelled")
			}
			// A cancelled outer context cannot persist its terminal update. The
			// normal recovery path must fail it without scheduling a second bill.
			if _, err = pool.Exec(ctx, `update ai_tasks set started_at=now()-interval '10 minutes' where id=$1`, taskID); err != nil {
				t.Fatal(err)
			}
			worker.recoverTasks(ctx)
			if err = worker.handleTask(ctx, message); err != nil {
				t.Fatalf("terminal duplicate delivery: %v", err)
			}
			var status string
			var input, output, cost, reserved, reservedCost int64
			if err = pool.QueryRow(ctx, `select status,input_tokens,output_tokens,cost_micros,quota_reserved_tokens,
				(payload->>'quotaReservedCostMicros')::bigint from ai_tasks where id=$1`, taskID).Scan(&status, &input, &output, &cost, &reserved, &reservedCost); err != nil {
				t.Fatal(err)
			}
			if status != "failed" || calls.Load() != 1 || input != 0 || output != 0 || cost != 0 || reserved != 100 || reservedCost != 200 {
				t.Fatalf("unknown usage lost reservation or retried: status=%s calls=%d tokens=%d/%d cost=%d reserved=%d/%d", status, calls.Load(), input, output, cost, reserved, reservedCost)
			}
			tx, err = pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			quota := settings
			quota.Quotas[0].TokenLimit = 100
			if err = reserveAISiteQuotaTx(ctx, tx, quota, 1); !errors.Is(err, errAIQuotaExceeded) {
				t.Fatalf("unknown billed tokens became free: %v", err)
			}
			nextID, nextUID := insertAITestTask(t, ctx, pool, aiTaskI18nTranslation, "queued", payload, 500000)
			if err = server.enqueueAIOutboxTx(ctx, tx, nextID, nextUID, aiTaskI18nTranslation); !errors.Is(err, errAIQuotaExceeded) {
				t.Fatalf("unknown billed cost reservation was released: %v", err)
			}
		})
	}
}

func aiFailureNotificationFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (int64, int64, map[string]any) {
	t.Helper()
	var userID, notificationID int64
	name := "ai-failure-" + randomHex(12)
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values($1,$2,'synthetic') returning id`, name, name+"@example.test").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	var updated time.Time
	if err := pool.QueryRow(ctx, `insert into notifications(recipient_id,kind,title,body,source_locale)
		values($1,'reply_mention','原始标题','你好 {name}','zh-CN') returning id,updated_at`, userID).Scan(&notificationID, &updated); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into notification_translations(notification_id,user_id,locale,title,body)
		values($1,$2,'en-US','Existing title','Existing body {name}')`, notificationID, userID); err != nil {
		t.Fatal(err)
	}
	return userID, notificationID, map[string]any{
		"notificationId": notificationID, "sourceUpdatedAt": updated,
		"sourceLocale": "zh-CN", "targetLocale": "en-US",
		"items": []map[string]string{{"key": "title", "text": "原始标题"}, {"key": "body", "text": "你好 {name}"}},
	}
}

func TestAIInvalidBatchPreservesExistingTranslationsCoreIntegration(t *testing.T) {
	for _, scenario := range []struct{ name, response string }{
		{"empty_item", `{"items":[{"key":"title","text":"New title"},{"key":"body","text":""}]}`},
		{"missing_field", `{"items":[{"key":"title","text":"New title"},{"key":"body"}]}`},
		{"missing_item", `{"items":[{"key":"title","text":"New title"}]}`},
		{"broken_placeholder", `{"items":[{"key":"title","text":"New title"},{"key":"body","text":"Hello"}]}`},
		{"invalid_json", `{untrusted invalid JSON`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, pool, cfg := isolatedAITestDatabase(t)
			var calls atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": scenario.response}}},
					"usage":   map[string]int{"prompt_tokens": 22, "completion_tokens": 11},
				})
			}))
			defer provider.Close()
			setAITestConfig(t, ctx, pool, cfg, aiTestConfig(provider.URL))
			userID, notificationID, payload := aiFailureNotificationFixture(t, ctx, pool)
			taskID, uid := insertAITestTask(t, ctx, pool, aiTaskNotificationTranslation, "queued", payload, 100)
			if _, err := pool.Exec(ctx, `update ai_tasks set created_by=$2 where id=$1`, taskID, userID); err != nil {
				t.Fatal(err)
			}
			worker := NewAIWorker(pool, nil, cfg.SettingsEncryptionKey)
			message := aiFailureMessage(t, taskID, uid, aiTaskNotificationTranslation)
			if err := worker.handleTask(ctx, message); err == nil {
				t.Fatal("invalid batch was accepted")
			}
			var status, title, body string
			var input, output, cost int64
			if err := pool.QueryRow(ctx, `select status,input_tokens,output_tokens,cost_micros from ai_tasks where id=$1`, taskID).Scan(&status, &input, &output, &cost); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRow(ctx, `select title,body from notification_translations where notification_id=$1 and user_id=$2 and locale='en-US'`, notificationID, userID).Scan(&title, &body); err != nil {
				t.Fatal(err)
			}
			if status != "failed" || calls.Load() != 1 || title != "Existing title" || body != "Existing body {name}" || input != 22 || output != 11 || cost != 44 {
				t.Fatalf("invalid batch published or lost actual usage: status=%s calls=%d title=%q body=%q usage=%d/%d/%d", status, calls.Load(), title, body, input, output, cost)
			}
			if err := worker.handleTask(ctx, message); err != nil || calls.Load() != 1 {
				t.Fatalf("failed batch duplicate called provider again: %v calls=%d", err, calls.Load())
			}
		})
	}
}

func TestAIDisabledModelBeforeDeliveryNeverCallsProviderCoreIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer provider.Close()
	settings := aiTestConfig(provider.URL)
	setAITestConfig(t, ctx, pool, cfg, settings)
	taskID, uid := insertAITestTask(t, ctx, pool, aiTaskI18nTranslation, "queued", map[string]any{
		"sourceLocale": "zh-CN", "targetLocale": "en-US", "items": []map[string]string{{"key": "greeting", "text": "你好"}},
	}, 100)
	settings.Models[0].Enabled = false
	setAITestConfig(t, ctx, pool, cfg, settings)
	worker := NewAIWorker(pool, nil, cfg.SettingsEncryptionKey)
	if err := worker.handleTask(ctx, aiFailureMessage(t, taskID, uid, aiTaskI18nTranslation)); err == nil {
		t.Fatal("disabled model was accepted")
	}
	var status string
	if err := pool.QueryRow(ctx, `select status from ai_tasks where id=$1`, taskID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 || status != "failed" {
		t.Fatalf("disabled model called provider: calls=%d status=%s", calls.Load(), status)
	}
}
