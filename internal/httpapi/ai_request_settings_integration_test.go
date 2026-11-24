package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"mcmods-cn-backend/internal/config"
)

func TestAIProviderAdmissionUsesCurrentSettingsIntegration(t *testing.T) {
	ctx := context.Background()
	pool := oct02AITestPool(t)
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"key\":\"title\",\"text\":\"Example\"}]}"}}],"usage":{"prompt_tokens":12,"completion_tokens":4}}`))
	}))
	defer provider.Close()
	const key = "fixture-settings-key-at-least-32-characters"
	server := &Server{db: pool, cfg: config.Config{SettingsEncryptionKey: key}}
	worker := NewAIWorker(pool, nil, key)
	cfg := defaultAIConfig()
	cfg.Providers[0].Enabled, cfg.Providers[0].BaseURL, cfg.Providers[0].APIKey = true, provider.URL, "fixture"
	cfg.Models[0].Enabled = true
	cfg.TaskModels[1].ModelKey = "openai/" + cfg.Models[0].Model
	save := func() {
		t.Helper()
		sealed, err := server.sealSystemSetting(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('ai.config',$1::jsonb)
			on conflict(key) do update set value=excluded.value`, sealed); err != nil {
			t.Fatal(err)
		}
	}
	prepare := func() preparedAITask {
		t.Helper()
		p, err := worker.prepareTask(ctx, aiTaskI18nTranslation, "openai", cfg.Models[0].Model,
			[]byte(`{"sourceLocale":"en-US","targetLocale":"zh-CN","items":[{"key":"title","text":"Example"}]}`))
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	save()
	old := prepare()
	cfg.Quotas[0].RequestLimit = 1
	save()
	if _, _, err := worker.executePreparedTask(ctx, old); !errors.Is(err, errAIExecutionPolicy) {
		t.Fatalf("request prepared under the previous budget returned %v", err)
	}
	var reserved int
	if err := pool.QueryRow(ctx, `select count(*) from ai_task_logs where event=$1`, aiRequestBudgetEvent).Scan(&reserved); err != nil || reserved != 0 || calls.Load() != 0 {
		t.Fatalf("stale settings reserved=%d calls=%d err=%v", reserved, calls.Load(), err)
	}
	if _, _, err := worker.executePreparedTask(ctx, prepare()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := worker.executePreparedTask(ctx, prepare()); err == nil || calls.Load() != 1 {
		t.Fatalf("current one-request budget calls=%d err=%v", calls.Load(), err)
	}
}
