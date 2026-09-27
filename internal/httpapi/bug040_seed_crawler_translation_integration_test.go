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

func TestBUG040SeedCrawlerTranslationsReachDraftAndSubmittedProjectIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify seed crawler translation publication")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/project/bug040-seed":
			_, _ = response.Write([]byte(`{"id":"bug040-external","slug":"bug040-seed","title":"English name","description":"English summary","body":"English body","project_type":"mod","status":"approved","client_side":"required","server_side":"required","categories":["utility"],"game_versions":["1.20.1"],"loaders":["fabric"],"license":{"id":"MIT"}}`))
		case "/project/bug040-external/version":
			_, _ = response.Write([]byte(`[]`))
		case "/project/bug040-plugin":
			_, _ = response.Write([]byte(`{"id":"bug040-plugin-external","slug":"bug040-plugin","title":"English plugin","description":"English plugin summary","body":"English plugin body","project_type":"plugin","status":"approved","categories":["utility"],"game_versions":["1.20.1"],"loaders":["paper"],"license":{"id":"MIT"}}`))
		case "/project/bug040-plugin-external/version":
			_, _ = response.Write([]byte(`[]`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer provider.Close()

	aiCalls := 0
	aiProvider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		aiCalls++
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil || len(body.Messages) < 2 {
			t.Errorf("decode AI request: messages=%d err=%v", len(body.Messages), err)
			http.Error(response, "bad request", http.StatusBadRequest)
			return
		}
		prompt := body.Messages[len(body.Messages)-1].Content
		locale := "unknown"
		for _, candidate := range supportedContentLocaleList() {
			if strings.Contains(prompt, `"targetLocale":"`+candidate+`"`) {
				locale = candidate
				break
			}
		}
		nameKey := "primaryName"
		if strings.Contains(prompt, `"key":"name"`) {
			nameKey = "name"
		}
		translated, err := json.Marshal(map[string]any{"items": []map[string]string{
			{"key": nameKey, "text": locale + " name"},
			{"key": "summary", "text": locale + " summary"},
			{"key": "bodyMarkdown", "text": locale + " body"},
		}})
		if err != nil {
			t.Error(err)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": string(translated)}}},
			"usage":   map[string]int{"prompt_tokens": 3, "completion_tokens": 4},
		})
	}))
	defer aiProvider.Close()

	loaded := config.Load()
	poolConfig, err := pgxpool.ParseConfig(loaded.DB.ConnString())
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

	var actorID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('bug040-actor','bug040@example.test','integration-test',true) returning id`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	worker := NewSeedCrawlerWorker(loaded, pool)
	importConfig := defaultModImportConfig()
	importConfig.Modrinth.BaseURL = provider.URL
	importConfig.Modrinth.Token = ""
	sealedImportConfig, err := worker.server.sealSystemSetting(importConfig)
	if err != nil {
		t.Fatal(err)
	}
	aiConfig := aiConfigPayload{
		Providers: []aiProviderConfig{{Code: "bug040", Name: "BUG040", Enabled: true, BaseURL: aiProvider.URL,
			APIKey: "integration-test", Protocol: "openai-compatible"}},
		Models: []aiModelConfig{{Provider: "bug040", Model: "translator", DisplayName: "BUG040 translator", Enabled: true,
			ContextTokens: 8192, MaxOutputTokens: 4096}},
		TaskModels: []aiTaskModelConfig{{TaskType: aiTaskContentTranslation, ModelKey: "bug040/translator",
			TimeoutSeconds: 30}},
	}
	sealedAIConfig, err := worker.server.sealSystemSetting(aiConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value,updated_by) values
		($1,$2::jsonb,$4),('ai.config',$3::jsonb,$4)`, modImportConfigSettingKey, sealedImportConfig, sealedAIConfig, actorID); err != nil {
		t.Fatal(err)
	}
	var crawlerRunID int64
	if err = pool.QueryRow(ctx, `insert into seed_crawler_runs(dry_run) values(false) returning id`).Scan(&crawlerRunID); err != nil {
		t.Fatal(err)
	}
	var candidateID int64
	if err = pool.QueryRow(ctx, `insert into seed_crawler_candidates(first_seen_run_id,last_seen_run_id,external_project_id,project_type,downloads,payload)
		values($1,$1,'bug040-external','mod',1000,'{}') returning id`, crawlerRunID).Scan(&candidateID); err != nil {
		t.Fatal(err)
	}
	actor := security.Claims{Subject: actorID, Username: "bug040-actor", PermissionRules: []security.PermissionRule{{Code: "admin.*", Allow: true, Priority: 100}}}
	status, createErr := worker.createSeedDraft(ctx, candidateID, actor, "mod", seedModrinthHit{
		ProjectID: "bug040-external", Slug: "bug040-seed", Title: "English name", Downloads: 1000,
	}, seedCrawlerRuntimeConfig{AIDailyTokenBudget: 10000, AutoSubmitReview: true})
	if createErr != nil || status != "submitted" {
		t.Fatalf("create translated seed draft = %q, err=%v", status, createErr)
	}

	var draftPayload []byte
	var submittedAt *time.Time
	if err = pool.QueryRow(ctx, `select payload,submitted_at from user_drafts where draft_key='seed-crawler:bug040-external'`).
		Scan(&draftPayload, &submittedAt); err != nil {
		t.Fatal(err)
	}
	var draft map[string]any
	if err = json.Unmarshal(draftPayload, &draft); err != nil {
		t.Fatal(err)
	}
	draftLocalizations, _ := draft["localizations"].([]any)
	if len(draftLocalizations) != len(supportedEditableContentLocales) {
		t.Errorf("submitted draft localizations=%d, want %d", len(draftLocalizations), len(supportedEditableContentLocales))
	}
	if _, deadField := draft["seedTranslations"]; deadField {
		t.Error("submitted draft still stores translations in the unconsumed seedTranslations field")
	}
	if submittedAt == nil {
		t.Error("auto-submitted draft has no submitted_at")
	}

	var publishedLocalizations, snapshotLocalizations, completedTranslations int
	var inputTokens, outputTokens int64
	if err = pool.QueryRow(ctx, `select
		(select count(*)::int from content_localizations localization join mods mod
			on localization.subject_type='mod' and localization.subject_id=mod.id
			where mod.modrinth_project_id='bug040-external'),
		(select jsonb_array_length(revision.snapshot->'localizations') from content_revisions revision join mods mod
			on mod.published_revision_id=revision.id where mod.modrinth_project_id='bug040-external'),
		(select count(*)::int from seed_crawler_translation_tasks where candidate_id=$1 and status='completed'),
		(select coalesce(sum(input_tokens),0) from seed_crawler_translation_tasks where candidate_id=$1),
		(select coalesce(sum(output_tokens),0) from seed_crawler_translation_tasks where candidate_id=$1)`, candidateID).
		Scan(&publishedLocalizations, &snapshotLocalizations, &completedTranslations, &inputTokens, &outputTokens); err != nil {
		t.Fatal(err)
	}
	wantTranslations := len(supportedEditableContentLocales) - 1
	if aiCalls != wantTranslations || completedTranslations != wantTranslations || inputTokens != int64(wantTranslations*3) || outputTokens != int64(wantTranslations*4) {
		t.Errorf("AI work calls/completed/tokens=%d/%d/%d/%d, want %d/%d/%d/%d", aiCalls, completedTranslations,
			inputTokens, outputTokens, wantTranslations, wantTranslations, wantTranslations*3, wantTranslations*4)
	}
	if publishedLocalizations != len(supportedEditableContentLocales) || snapshotLocalizations != len(supportedEditableContentLocales) {
		t.Errorf("published/snapshot localizations=%d/%d, want %d/%d after paid translations", publishedLocalizations,
			snapshotLocalizations, len(supportedEditableContentLocales), len(supportedEditableContentLocales))
	}

	var pluginCandidateID int64
	if err = pool.QueryRow(ctx, `insert into seed_crawler_candidates(first_seen_run_id,last_seen_run_id,external_project_id,project_type,downloads,payload)
		values($1,$1,'bug040-plugin-external','plugin',2000,'{}') returning id`, crawlerRunID).Scan(&pluginCandidateID); err != nil {
		t.Fatal(err)
	}
	pluginStatus, pluginCreateErr := worker.createSeedDraft(ctx, pluginCandidateID, actor, "plugin", seedModrinthHit{
		ProjectID: "bug040-plugin-external", Slug: "bug040-plugin", Title: "English plugin", Downloads: 2000,
	}, seedCrawlerRuntimeConfig{AIDailyTokenBudget: 10000, AutoSubmitReview: true})
	if pluginCreateErr != nil || pluginStatus != "submitted" {
		t.Fatalf("create translated plugin seed draft = %q, err=%v", pluginStatus, pluginCreateErr)
	}
	if err = pool.QueryRow(ctx, `select payload from user_drafts where draft_key='seed-crawler:bug040-plugin-external'`).
		Scan(&draftPayload); err != nil {
		t.Fatal(err)
	}
	draft = nil
	if err = json.Unmarshal(draftPayload, &draft); err != nil {
		t.Fatal(err)
	}
	draftLocalizations, _ = draft["localizations"].([]any)
	if len(draftLocalizations) != len(supportedEditableContentLocales) {
		t.Errorf("plugin draft localizations=%d, want %d", len(draftLocalizations), len(supportedEditableContentLocales))
	}
	if _, deadField := draft["seedTranslations"]; deadField {
		t.Error("plugin draft still stores translations in the unconsumed seedTranslations field")
	}
	if err = pool.QueryRow(ctx, `select
		(select count(*)::int from simple_project_localizations localization join simple_projects project on project.id=localization.project_id
			where project.project_type='plugin' and project.modrinth_project_id='bug040-plugin-external'),
		(select jsonb_array_length(revision.snapshot->'localizations') from content_revisions revision join simple_projects project
			on project.published_revision_id=revision.id where project.project_type='plugin' and project.modrinth_project_id='bug040-plugin-external'),
		(select count(*)::int from seed_crawler_translation_tasks where candidate_id=$1 and status='completed'),
		(select coalesce(sum(input_tokens),0) from seed_crawler_translation_tasks where candidate_id=$1),
		(select coalesce(sum(output_tokens),0) from seed_crawler_translation_tasks where candidate_id=$1)`, pluginCandidateID).
		Scan(&publishedLocalizations, &snapshotLocalizations, &completedTranslations, &inputTokens, &outputTokens); err != nil {
		t.Fatal(err)
	}
	if aiCalls != wantTranslations*2 || completedTranslations != wantTranslations || inputTokens != int64(wantTranslations*3) || outputTokens != int64(wantTranslations*4) {
		t.Errorf("plugin AI work total-calls/completed/tokens=%d/%d/%d/%d, want %d/%d/%d/%d", aiCalls,
			completedTranslations, inputTokens, outputTokens, wantTranslations*2, wantTranslations, wantTranslations*3, wantTranslations*4)
	}
	if publishedLocalizations != len(supportedEditableContentLocales) || snapshotLocalizations != len(supportedEditableContentLocales) {
		t.Errorf("plugin published/snapshot localizations=%d/%d, want %d/%d", publishedLocalizations,
			snapshotLocalizations, len(supportedEditableContentLocales), len(supportedEditableContentLocales))
	}
}
