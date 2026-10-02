package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	appconfig "mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestTEST043ContentAIWorkerFailureSuccessAndDuplicateDeliveryIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("TEST-043 exercises an isolated PostgreSQL content and AI worker state machine")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := newTEST043IsolatedDatabase(t, ctx)
	cfg := appconfig.Load()
	server := &Server{db: pool, cfg: cfg}

	var providerCalls atomic.Int32
	var providerFailing atomic.Bool
	providerFailing.Store(true)
	provider := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		providerCalls.Add(1)
		if request.Method != http.MethodPost || request.URL.Path != "/v1/chat/completions" {
			http.Error(response, "unexpected TEST-043 provider request", http.StatusNotFound)
			return
		}
		if request.Header.Get("Authorization") != "Bearer test043-provider-key" {
			http.Error(response, "missing TEST-043 provider credential", http.StatusUnauthorized)
			return
		}
		if providerFailing.Load() {
			http.Error(response, "TEST-043 provider unavailable", http.StatusServiceUnavailable)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{
				"content": `{"items":[{"key":"title","text":"Titre traduit"},{"key":"bodyMarkdown","text":"Corps traduit"}]}`,
			}}},
			"usage": map[string]int64{"prompt_tokens": 17, "completion_tokens": 9},
		})
	}))
	defer provider.Close()
	originalTransport := http.DefaultTransport
	http.DefaultTransport = provider.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	aiConfig := defaultAIConfig()
	aiConfig.Providers[0].Enabled = true
	aiConfig.Providers[0].BaseURL = provider.URL
	aiConfig.Providers[0].APIKey = "test043-provider-key"
	aiConfig.Models[0].Enabled = true
	for index := range aiConfig.TaskModels {
		if aiConfig.TaskModels[index].TaskType == aiTaskContentTranslation {
			aiConfig.TaskModels[index].ModelKey = "openai/gpt-4.1-mini"
			aiConfig.TaskModels[index].TimeoutSeconds = 5
		}
	}
	sealedConfig, err := server.sealSystemSetting(aiConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('ai.config',$1::jsonb)
		on conflict(key) do update set value=excluded.value`, sealedConfig); err != nil {
		t.Fatal(err)
	}

	authorID, token := createTEST043User(t, ctx, pool, cfg)
	postID, publicID, sourceRevisionID := createTEST043ApprovedCommunityPost(t, ctx, pool, authorID)
	translationHandler := server.requirePermission("content.translate", server.requestCommunityPostTranslation)
	if response := performTEST043Request(ctx, translationHandler, token, http.MethodPost,
		"/api/v1/community/posts/"+publicID+"/translations", `{"targetLocale":"fr-FR"}`, map[string]string{"id": publicID}); response.Code != http.StatusForbidden {
		t.Fatalf("translation without permission status=%d body=%s", response.Code, response.Body.String())
	}
	grantTEST043Permissions(t, ctx, pool, authorID, "content.translate", "user.ai.daily_token_limit.100000")

	failedTaskUID := requestTEST043Translation(t, ctx, translationHandler, token, publicID, http.StatusAccepted)
	failedTaskID := loadTEST043TaskID(t, ctx, pool, failedTaskUID)
	worker := NewAIWorker(pool, nil, cfg.SettingsEncryptionKey)
	failedMessage := marshalTEST043TaskMessage(t, failedTaskID, failedTaskUID)
	if err = worker.handleTask(ctx, failedMessage); err == nil || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("provider failure error=%v, want HTTP 503", err)
	}
	assertTEST043Task(t, ctx, pool, failedTaskUID, "failed", "HTTP 503", 0, 0)
	assertTEST043Translation(t, ctx, pool, postID, sourceRevisionID, false)
	if calls := providerCalls.Load(); calls != 1 {
		t.Fatalf("provider calls after failure=%d, want 1", calls)
	}
	if err = worker.handleTask(ctx, failedMessage); err != nil {
		t.Fatalf("duplicate failed-task delivery returned error: %v", err)
	}
	if calls := providerCalls.Load(); calls != 1 {
		t.Fatalf("duplicate failed-task delivery called provider %d times, want 1", calls)
	}

	providerFailing.Store(false)
	completedTaskUID := requestTEST043Translation(t, ctx, translationHandler, token, publicID, http.StatusAccepted)
	if completedTaskUID == failedTaskUID {
		t.Fatal("a terminal failed task was reused instead of creating a retryable new task")
	}
	completedTaskID := loadTEST043TaskID(t, ctx, pool, completedTaskUID)
	completedMessage := marshalTEST043TaskMessage(t, completedTaskID, completedTaskUID)
	if err = worker.handleTask(ctx, completedMessage); err != nil {
		t.Fatalf("successful worker delivery: %v", err)
	}
	assertTEST043Task(t, ctx, pool, completedTaskUID, "completed", "", 17, 9)
	assertTEST043Translation(t, ctx, pool, postID, sourceRevisionID, true)
	if calls := providerCalls.Load(); calls != 2 {
		t.Fatalf("provider calls after success=%d, want 2", calls)
	}
	if err = worker.handleTask(ctx, completedMessage); err != nil {
		t.Fatalf("duplicate completed-task delivery returned error: %v", err)
	}
	if calls := providerCalls.Load(); calls != 2 {
		t.Fatalf("duplicate completed-task delivery called provider %d times, want 2", calls)
	}

	resultHandler := server.requirePermission("content.translate", server.communityPostTranslationResult)
	result := performTEST043Request(ctx, resultHandler, token, http.MethodGet,
		"/api/v1/community/translations/"+completedTaskUID, "", map[string]string{"taskId": completedTaskUID})
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), `"status":"completed"`) ||
		!strings.Contains(result.Body.String(), `"title":"Titre traduit"`) || !strings.Contains(result.Body.String(), `"bodyMarkdown":"Corps traduit"`) {
		t.Fatalf("completed translation result status=%d body=%s", result.Code, result.Body.String())
	}
	cachedTaskUID := requestTEST043Translation(t, ctx, translationHandler, token, publicID, http.StatusOK)
	if cachedTaskUID != "" {
		t.Fatalf("cached translation unexpectedly returned task %q", cachedTaskUID)
	}
	var taskCount, translationCount, failureLogs, completionLogs int
	if err = pool.QueryRow(ctx, `select
		(select count(*) from ai_tasks where created_by=$1),
		(select count(*) from community_post_translations where post_id=$2 and locale='fr-FR'),
		(select count(*) from ai_task_logs where task_id=$3 and event='task_failed'),
		(select count(*) from ai_task_logs where task_id=$4 and event='task_completed')`,
		authorID, postID, failedTaskID, completedTaskID).Scan(&taskCount, &translationCount, &failureLogs, &completionLogs); err != nil {
		t.Fatal(err)
	}
	if taskCount != 2 || translationCount != 1 || failureLogs != 1 || completionLogs != 1 {
		t.Fatalf("final AI facts tasks/translations/failureLogs/completionLogs=%d/%d/%d/%d, want 2/1/1/1",
			taskCount, translationCount, failureLogs, completionLogs)
	}
}

func newTEST043IsolatedDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	baseConfig, err := pgxpool.ParseConfig(appconfig.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	databaseName := fmt.Sprintf("test043_content_ai_%d", time.Now().UnixNano())
	namePattern := regexp.MustCompile(`^test043_content_ai_[0-9]+$`)
	if !namePattern.MatchString(databaseName) {
		adminPool.Close()
		t.Fatalf("refuse unsafe TEST043 database name %q", databaseName)
	}
	quotedName := pgx.Identifier{databaseName}.Sanitize()
	if _, err = adminPool.Exec(ctx, "create database "+quotedName); err != nil {
		adminPool.Close()
		t.Fatal(err)
	}
	var pool *pgxpool.Pool
	t.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if !namePattern.MatchString(databaseName) {
			t.Errorf("refuse unsafe TEST043 cleanup database name %q", databaseName)
		} else if _, dropErr := adminPool.Exec(cleanupCtx, "drop database "+quotedName+" with (force)"); dropErr != nil {
			t.Errorf("drop isolated TEST043 database: %v", dropErr)
		}
		adminPool.Close()
	})
	scopedConfig, err := pgxpool.ParseConfig(appconfig.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	scopedConfig.ConnConfig.Database = databaseName
	scopedConfig.MaxConns, scopedConfig.MinConns = 6, 1
	pool, err = pgxpool.NewWithConfig(ctx, scopedConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func createTEST043User(t *testing.T, ctx context.Context, pool *pgxpool.Pool, cfg appconfig.Config) (int64, string) {
	t.Helper()
	username := fmt.Sprintf("test043-author-%d", time.Now().UnixNano())
	var userID, authVersion int64
	var publicID string
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values($1,$2,'not-used','active') returning id,public_id,auth_version`, username, username+"@example.test").
		Scan(&userID, &publicID, &authVersion); err != nil {
		t.Fatal(err)
	}
	claims, err := security.NewClaims(publicID, username, username+"@example.test", authVersion, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into auth_sessions(session_hash,user_id,auth_version,expires_at)
		values($1,$2,$3,$4)`, security.SessionFingerprint(claims.SessionID), userID, authVersion, time.Unix(claims.ExpiresAt, 0)); err != nil {
		t.Fatal(err)
	}
	token, err := security.SignToken(cfg.JWTSecret, claims)
	if err != nil {
		t.Fatal(err)
	}
	return userID, token
}

func grantTEST043Permissions(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID int64, codes ...string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `insert into permissions(code,module,name)
		select code,'test043',code from unnest($1::text[]) code on conflict(code) do nothing`, codes); err != nil {
		t.Fatal(err)
	}
	command, err := pool.Exec(ctx, `insert into user_permissions(user_id,permission_id,allow,source,source_key)
		select $1,id,true,'manual','' from permissions where code=any($2::text[])
		on conflict(user_id,permission_id,source,source_key) do update set allow=true`, userID, codes)
	if err != nil {
		t.Fatal(err)
	}
	if command.RowsAffected() != int64(len(codes)) {
		t.Fatalf("granted %d TEST-043 permissions, want %d for %v", command.RowsAffected(), len(codes), codes)
	}
}

func createTEST043ApprovedCommunityPost(t *testing.T, ctx context.Context, pool *pgxpool.Pool, authorID int64) (int64, string, int64) {
	t.Helper()
	baseline := communityPostSnapshot{Kind: "tutorial", Category: "general", Title: "TEST-043 source", SourceLocale: "en-US",
		BodyMarkdown: "TEST-043 source body", MinecraftVersions: []string{}, Projects: []communityPostReference{}, Resources: []communityPostReference{}}
	var postID int64
	var publicID string
	if err := pool.QueryRow(ctx, `insert into community_posts(kind,category,author_id,title,source_locale,body_markdown,review_status)
		values('tutorial','general',$1,$2,$3,$4,'approved') returning id,public_id`,
		authorID, baseline.Title, baseline.SourceLocale, baseline.BodyMarkdown).Scan(&postID, &publicID); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	rawSnapshot, err := json.Marshal(baseline)
	if err != nil {
		t.Fatal(err)
	}
	created, err := createContentRevisionTx(ctx, tx, createContentRevisionParams{
		EntityType: "community_post", EntityID: postID, AggregateType: communityPostAggregate, AggregateKey: publicID,
		Snapshot: rawSnapshot, Reason: "TEST-043 baseline", ActorID: authorID, Source: "user", Status: "approved",
	})
	if err == nil {
		err = applyCommunityPostSnapshotTx(ctx, tx, postID, created.RevisionID, baseline, security.Claims{Subject: authorID})
	}
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return postID, publicID, created.RevisionID
}

func performTEST043Request(
	ctx context.Context,
	handler http.HandlerFunc,
	token, method, path, body string,
	pathValues map[string]string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range pathValues {
		request.SetPathValue(key, value)
	}
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}

func requestTEST043Translation(
	t *testing.T,
	ctx context.Context,
	handler http.HandlerFunc,
	token, publicID string,
	wantStatus int,
) string {
	t.Helper()
	response := performTEST043Request(ctx, handler, token, http.MethodPost,
		"/api/v1/community/posts/"+publicID+"/translations", `{"targetLocale":"fr-FR"}`, map[string]string{"id": publicID})
	if response.Code != wantStatus {
		t.Fatalf("translation request status=%d body=%s, want %d", response.Code, response.Body.String(), wantStatus)
	}
	var envelope struct {
		Data struct {
			TaskID string `json:"taskId"`
			Status string `json:"status"`
			Cached bool   `json:"cached"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode translation response: %v body=%s", err, response.Body.String())
	}
	if wantStatus == http.StatusAccepted && (envelope.Data.TaskID == "" || envelope.Data.Status != "queued") {
		t.Fatalf("queued translation response=%#v", envelope.Data)
	}
	if wantStatus == http.StatusOK && !envelope.Data.Cached {
		t.Fatalf("cached translation response=%#v", envelope.Data)
	}
	return envelope.Data.TaskID
}

func loadTEST043TaskID(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskUID string) int64 {
	t.Helper()
	var taskID int64
	if err := pool.QueryRow(ctx, `select id from ai_tasks where task_uid=$1`, taskUID).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	return taskID
}

func marshalTEST043TaskMessage(t *testing.T, taskID int64, taskUID string) []byte {
	t.Helper()
	raw, err := json.Marshal(aiTaskMessage{TaskID: taskID, TaskUID: taskUID, TaskType: aiTaskContentTranslation})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertTEST043Task(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	taskUID, wantStatus, errorFragment string,
	wantInputTokens, wantOutputTokens int64,
) {
	t.Helper()
	var status, errorText string
	var inputTokens, outputTokens int64
	if err := pool.QueryRow(ctx, `select status,error,input_tokens,output_tokens from ai_tasks where task_uid=$1`, taskUID).
		Scan(&status, &errorText, &inputTokens, &outputTokens); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus || (errorFragment == "" && errorText != "") ||
		(errorFragment != "" && !strings.Contains(errorText, errorFragment)) ||
		inputTokens != wantInputTokens || outputTokens != wantOutputTokens {
		t.Fatalf("task %s state=%q error=%q usage=%d/%d, want %q fragment=%q usage=%d/%d",
			taskUID, status, errorText, inputTokens, outputTokens, wantStatus, errorFragment, wantInputTokens, wantOutputTokens)
	}
}

func assertTEST043Translation(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	postID, sourceRevisionID int64,
	want bool,
) {
	t.Helper()
	var count int
	var title, body string
	err := pool.QueryRow(ctx, `select count(*),coalesce(max(title),''),coalesce(max(body_markdown),'')
		from community_post_translations where post_id=$1 and locale='fr-FR' and source_revision_id=$2`,
		postID, sourceRevisionID).Scan(&count, &title, &body)
	if err != nil {
		t.Fatal(err)
	}
	if !want && count != 0 {
		t.Fatalf("failed task persisted %d community translations", count)
	}
	if want && (count != 1 || title != "Titre traduit" || body != "Corps traduit") {
		t.Fatalf("persisted translation count/title/body=%d/%q/%q", count, title, body)
	}
}
