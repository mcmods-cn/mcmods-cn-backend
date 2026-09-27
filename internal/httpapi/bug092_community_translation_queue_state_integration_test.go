package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestCommunityTranslationQueuedMeansDurableOutboxAcceptanceIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify community translation queue state")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cfg := config.Load()
	poolConfig, err := pgxpool.ParseConfig(cfg.DB.ConnString())
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
			t.Errorf("drop BUG-092 ephemeral schema: %v", dropErr)
		}
	}()

	server := &Server{cfg: cfg, db: pool}
	aiConfig := defaultAIConfig()
	aiConfig.Providers[0].Enabled = true
	aiConfig.Providers[0].APIKey = "bug092-test-key"
	aiConfig.Models[0].Enabled = true
	for index := range aiConfig.TaskModels {
		if aiConfig.TaskModels[index].TaskType == aiTaskContentTranslation {
			aiConfig.TaskModels[index].ModelKey = "openai/gpt-4.1-mini"
		}
	}
	sealedConfig, err := server.sealSystemSetting(aiConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('ai.config',$1::jsonb)`, sealedConfig); err != nil {
		t.Fatal(err)
	}

	suffix := time.Now().UnixNano()
	var authorID, postID int64
	var publicID string
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values($1,$2,'not-used','active') returning id`, fmt.Sprintf("bug092_author_%d", suffix), fmt.Sprintf("bug092_author_%d@example.test", suffix)).Scan(&authorID); err != nil {
		t.Fatal(err)
	}
	baseline := communityPostSnapshot{Kind: "tutorial", Category: "general", Title: "BUG-092 source", SourceLocale: "en-US",
		BodyMarkdown: "Translation source body", MinecraftVersions: []string{}, Projects: []communityPostReference{}, Resources: []communityPostReference{}}
	if err = pool.QueryRow(ctx, `insert into community_posts(kind,category,author_id,title,source_locale,body_markdown,review_status)
		values('tutorial','general',$1,$2,$3,$4,'approved') returning id,public_id`, authorID, baseline.Title, baseline.SourceLocale, baseline.BodyMarkdown).
		Scan(&postID, &publicID); err != nil {
		t.Fatal(err)
	}
	setupTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rawSnapshot, err := json.Marshal(baseline)
	if err != nil {
		setupTx.Rollback(ctx)
		t.Fatal(err)
	}
	created, err := createContentRevisionTx(ctx, setupTx, createContentRevisionParams{
		EntityType: "community_post", EntityID: postID, AggregateType: communityPostAggregate, AggregateKey: publicID,
		Snapshot: rawSnapshot, Reason: "BUG-092 baseline", ActorID: authorID, Source: "user", Status: "approved",
	})
	if err == nil {
		err = applyCommunityPostSnapshotTx(ctx, setupTx, postID, created.RevisionID, baseline, security.Claims{Subject: authorID})
	}
	if err != nil {
		setupTx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = setupTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err = pool.Exec(ctx, `create function pg_temp.reject_bug092_outbox() returns trigger language plpgsql as $$
		begin
			if new.event_type='ai.community_post_translation.requested' then
				raise exception 'BUG-092 forced outbox failure';
			end if;
			return new;
		end $$;
		create trigger reject_bug092_outbox before insert on nats_outbox
		for each row execute function pg_temp.reject_bug092_outbox()`); err != nil {
		t.Fatal(err)
	}
	claims := security.Claims{Subject: authorID, PermissionRules: []security.PermissionRule{{Code: "admin.*", Allow: true, Priority: 100}}}
	rejected := invokeBUG092CommunityTranslation(t, ctx, server, claims, publicID, "fr-FR")
	if rejected.Code != http.StatusServiceUnavailable {
		t.Fatalf("outbox failure status = %d body=%s, want 503", rejected.Code, rejected.Body.String())
	}
	var taskCount, eventCount int
	if err = pool.QueryRow(ctx, `select
		(select count(*) from ai_tasks where created_by=$1),
		(select count(*) from nats_outbox where aggregate_type='ai_task')`, authorID).Scan(&taskCount, &eventCount); err != nil {
		t.Fatal(err)
	}
	if taskCount != 0 || eventCount != 0 {
		t.Fatalf("outbox failure left task/event = %d/%d, want 0/0", taskCount, eventCount)
	}
	if _, err = pool.Exec(ctx, `drop trigger reject_bug092_outbox on nats_outbox`); err != nil {
		t.Fatal(err)
	}

	accepted := invokeBUG092CommunityTranslation(t, ctx, server, claims, publicID, "fr-FR")
	if accepted.Code != http.StatusAccepted {
		t.Fatalf("durable queue status = %d body=%s, want 202", accepted.Code, accepted.Body.String())
	}
	var response struct {
		Data struct {
			TaskID string `json:"taskId"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err = json.NewDecoder(accepted.Body).Decode(&response); err != nil || response.Data.TaskID == "" || response.Data.Status != "queued" {
		t.Fatalf("accepted queue response = %#v err=%v", response, err)
	}
	var taskStatus, outboxStatus string
	if err = pool.QueryRow(ctx, `select task.status,event.status from ai_tasks task
		join nats_outbox event on event.aggregate_type='ai_task' and event.aggregate_id=task.task_uid
		where task.task_uid=$1`, response.Data.TaskID).Scan(&taskStatus, &outboxStatus); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "queued" || outboxStatus != "pending" {
		t.Fatalf("durable task/outbox states = %q/%q, want queued/pending", taskStatus, outboxStatus)
	}
	if _, err = pool.Exec(ctx, `update nats_outbox set status='failed',last_error='NATS unavailable',available_at=now()+interval '1 second'
		where aggregate_type='ai_task' and aggregate_id=$1`, response.Data.TaskID); err != nil {
		t.Fatal(err)
	}
	poll := httptest.NewRequest(http.MethodGet, "/api/v1/community/translations/"+response.Data.TaskID, nil)
	poll.SetPathValue("taskId", response.Data.TaskID)
	poll = poll.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	pollResponse := httptest.NewRecorder()
	server.communityPostTranslationResult(pollResponse, poll)
	if pollResponse.Code != http.StatusOK {
		t.Fatalf("retryable publish failure poll status = %d body=%s", pollResponse.Code, pollResponse.Body.String())
	}
	var pollEnvelope struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err = json.NewDecoder(pollResponse.Body).Decode(&pollEnvelope); err != nil || pollEnvelope.Data.Status != "queued" {
		t.Fatalf("retryable publish failure task state = %#v err=%v, want queued", pollEnvelope, err)
	}
}

func invokeBUG092CommunityTranslation(t *testing.T, ctx context.Context, server *Server, claims security.Claims, publicID, targetLocale string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"targetLocale": targetLocale})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/community/posts/"+publicID+"/translations", bytes.NewReader(body))
	request.SetPathValue("id", publicID)
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	response := httptest.NewRecorder()
	server.requestCommunityPostTranslation(response, request)
	return response
}
