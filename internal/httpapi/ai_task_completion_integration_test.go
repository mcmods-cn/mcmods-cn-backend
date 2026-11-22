package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestAITranslationProjectionAndTaskCompletionCommitTogetherIntegration(t *testing.T) {
	if testing.Short() || os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("requires an isolated PostgreSQL database and a loopback TLS provider")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool := newTEST043IsolatedDatabase(t, ctx)
	cfg := config.Load()
	actor, _ := createTEST043User(t, ctx, pool, cfg)
	grantTEST043Permissions(t, ctx, pool, actor, "content.translate", "notification.translate", "user.ai.daily_token_limit.100000")

	var calls atomic.Int64
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer local-completion-fixture" {
			http.Error(w, "unexpected loopback provider request", http.StatusBadRequest)
			return
		}
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Messages) != 2 {
			http.Error(w, "invalid completion envelope", http.StatusBadRequest)
			return
		}
		_, raw, ok := strings.Cut(request.Messages[1].Content, " Input JSON: ")
		var input struct {
			Items []map[string]string `json:"items"`
		}
		if !ok || json.Unmarshal([]byte(raw), &input) != nil || len(input.Items) == 0 {
			http.Error(w, "invalid translation input", http.StatusBadRequest)
			return
		}
		unknownUsage := input.Items[0]["text"] == "Notification title"
		for _, item := range input.Items {
			item["text"] = "Translated " + item["text"]
		}
		result, _ := json.Marshal(input)
		response := map[string]any{"choices": []any{map[string]any{
			"finish_reason": "stop", "message": map[string]string{"content": string(result)},
		}}}
		if !unknownUsage {
			response["usage"] = map[string]int64{"prompt_tokens": 10, "completion_tokens": 7}
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer provider.Close()
	originalTransport := http.DefaultTransport
	http.DefaultTransport = provider.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	aiConfig := defaultAIConfig()
	aiConfig.Providers[0].Enabled = true
	aiConfig.Providers[0].BaseURL = provider.URL
	aiConfig.Providers[0].APIKey = "local-completion-fixture"
	aiConfig.Models[0].Enabled = true
	aiConfig.Models[0].Model = "completion-fixture"
	aiConfig.Models[0].MaxOutputTokens = 100
	for index := range aiConfig.TaskModels {
		aiConfig.TaskModels[index].ModelKey = "openai/completion-fixture"
	}
	server := &Server{db: pool, cfg: cfg}
	sealed, err := server.sealSystemSetting(aiConfig)
	if err != nil {
		t.Fatal(err)
	}
	reviewPolicy := defaultReviewConfig()
	reviewPolicy.AITranslation = false
	reviewRaw, err := json.Marshal(reviewPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('ai.config',$1),($2,$3::jsonb)
		on conflict(key) do update set value=excluded.value`, sealed, reviewConfigSettingKey, string(reviewRaw)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `create function reject_fixture_ai_completion() returns trigger language plpgsql as $$
		begin
			if new.status='completed' and new.task_uid like '%-reject' then
				raise exception 'fixture AI completion update rejected' using errcode='23514',
					constraint='reject_fixture_ai_completion';
			end if;
			return new;
		end $$;
		create trigger reject_fixture_ai_completion before update of status on ai_tasks
		for each row execute function reject_fixture_ai_completion()`); err != nil {
		t.Fatal(err)
	}

	postID, _, postRevision := createTEST043ApprovedCommunityPost(t, ctx, pool, actor)
	var notificationID, modID, modRevision int64
	if err = pool.QueryRow(ctx, `insert into notifications(recipient_id,kind,title,body,source_locale)
		values($1,'reply_mention','Notification title','Notification body','en-US') returning id`, actor).Scan(&notificationID); err != nil {
		t.Fatal(err)
	}
	const modPublicID = "aibatch03"
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		values($1,'ai-completion-mod','Catalog title','approved') returning id`, modPublicID).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into content_subjects(subject_type,subject_id,default_locale)
		values('mod',$1,'en-US') on conflict do nothing`, modID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into content_localizations(subject_type,subject_id,locale,name,summary,content_markdown,
		provenance,review_status) values('mod',$1,'en-US','Catalog title','Catalog summary','Catalog body','human','approved')
		returning revision_no`, modID).Scan(&modRevision); err != nil {
		t.Fatal(err)
	}

	const locale = "fr-FR"
	tests := []struct {
		name, taskType string
		payload        map[string]any
		translatedSQL  string
		translatedArgs []any
		receiptState   string
	}{
		{
			name: "catalog", taskType: aiTaskContentTranslation, receiptState: "settled",
			payload: map[string]any{"internalEntityId": modID, "entityType": "mod", "publicId": modPublicID,
				"sourceLocale": "en-US", "sourceRevisionNo": modRevision, "targetLocale": locale,
				"items": []map[string]string{{"key": "name", "text": "Catalog title"}, {"key": "summary", "text": "Catalog summary"},
					{"key": "contentMarkdown", "text": "Catalog body"}}},
			translatedSQL: `select count(*) from content_localizations where subject_type='mod' and subject_id=$1 and locale=$2
				and provenance='ai' and name='Translated Catalog title'`,
			translatedArgs: []any{modID, locale},
		},
		{
			name: "community", taskType: aiTaskContentTranslation, receiptState: "settled",
			payload: map[string]any{"scope": "community_post", "postInternalId": postID, "sourceRevisionId": postRevision,
				"sourceLocale": "en-US", "targetLocale": locale, "items": []map[string]string{
					{"key": "title", "text": "TEST-043 source"}, {"key": "bodyMarkdown", "text": "TEST-043 source body"}}},
			translatedSQL: `select count(*) from community_post_translations where post_id=$1 and locale=$2 and source_revision_id=$3
				and title='Translated TEST-043 source'`,
			translatedArgs: []any{postID, locale, postRevision},
		},
		{
			name: "notification", taskType: aiTaskNotificationTranslation, receiptState: "usage_unknown",
			payload: map[string]any{"notificationId": notificationID, "sourceLocale": "en-US", "targetLocale": locale,
				"items": []map[string]string{{"key": "title", "text": "Notification title"}, {"key": "body", "text": "Notification body"}}},
			translatedSQL: `select count(*) from notification_translations where notification_id=$1 and user_id=$2 and locale=$3
				and title='Translated Notification title'`,
			translatedArgs: []any{notificationID, actor, locale},
		},
	}
	worker := NewAIWorker(pool, nil, cfg.SettingsEncryptionKey)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			startCalls := calls.Load()
			for _, suffix := range []string{"-reject", "-complete"} {
				taskID, message := enqueueAICompletionFixture(t, ctx, pool, actor, test.name+suffix, test.taskType, test.payload)
				err := worker.handleTask(ctx, message)
				wantStatus, wantProjection := "completed", 1
				if suffix == "-reject" {
					var databaseError *pgconn.PgError
					if !errors.As(err, &databaseError) || databaseError.Code != "23514" || databaseError.ConstraintName != "reject_fixture_ai_completion" {
						t.Fatalf("expected the terminal update fault, got %v", err)
					}
					wantStatus, wantProjection = "failed", 0
				} else if err != nil {
					t.Fatal(err)
				}
				assertAICompletionFixture(t, ctx, pool, taskID, wantStatus, test.receiptState)
				var projected int
				if err = pool.QueryRow(ctx, test.translatedSQL, test.translatedArgs...).Scan(&projected); err != nil {
					t.Fatal(err)
				}
				if projected != wantProjection {
					t.Fatalf("task %s/%s persisted %d translation rows, want %d", test.name, wantStatus, projected, wantProjection)
				}
				if test.name == "catalog" {
					var changes int
					if err = pool.QueryRow(ctx, `select count(*) from change_requests where metadata->>'aiTaskId'=$1`,
						test.name+suffix).Scan(&changes); err != nil || changes != wantProjection {
						t.Fatalf("catalog review intents=%d, want %d: %v", changes, wantProjection, err)
					}
				}
				afterCall := calls.Load()
				if err = worker.handleTask(ctx, message); err != nil || calls.Load() != afterCall {
					t.Fatalf("terminal message replay called the provider or failed: %v", err)
				}
			}
			if calls.Load()-startCalls != 2 {
				t.Fatalf("loopback provider calls=%d, want 2", calls.Load()-startCalls)
			}
		})
	}
}

func enqueueAICompletionFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, actor int64, uid, taskType string, payload map[string]any) (int64, []byte) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var id int64
	if err = pool.QueryRow(ctx, `insert into ai_tasks(task_uid,task_type,provider,model,payload,created_by,quota_reserved_tokens)
		values($1,$2,'openai','completion-fixture',$3::jsonb,$4,5000) returning id`, uid, taskType, string(raw), actor).Scan(&id); err != nil {
		t.Fatal(err)
	}
	message, err := json.Marshal(aiTaskMessage{TaskID: id, TaskUID: uid, TaskType: taskType})
	if err != nil {
		t.Fatal(err)
	}
	return id, message
}

func assertAICompletionFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID int64, wantStatus, receiptState string) {
	t.Helper()
	var status string
	var empty bool
	var used, reserved, receipts int64
	if err := pool.QueryRow(ctx, `select status,result='{}'::jsonb,input_tokens+output_tokens,quota_reserved_tokens,
		(select count(*) from ai_task_logs where event=$2 and payload->>'taskId'=task.id::text and payload->>'state'=$3)
		from ai_tasks task where id=$1`, taskID, aiRequestBudgetEvent, receiptState).Scan(&status, &empty, &used, &reserved, &receipts); err != nil {
		t.Fatal(err)
	}
	wantUsed, wantReserved := int64(17), int64(0)
	if receiptState == "usage_unknown" {
		wantUsed, wantReserved = 0, 5000
	}
	if status != wantStatus || empty != (wantStatus == "failed") || used != wantUsed || reserved != wantReserved || receipts != 1 {
		t.Fatalf("completion facts status=%s empty=%t usage=%d reserved=%d %s receipts=%d", status, empty, used, reserved, receiptState, receipts)
	}
}

// Direct projection fixtures intentionally control their own transaction.
func (worker *AIWorker) persistNotificationTranslation(ctx context.Context, userID int64, rawPayload []byte, result map[string]any) error {
	return pgx.BeginFunc(ctx, worker.db, func(tx pgx.Tx) error {
		return persistNotificationTranslationTx(ctx, tx, userID, rawPayload, result)
	})
}

func (worker *AIWorker) persistCommunityPostTranslation(ctx context.Context, taskID int64, rawPayload []byte, result map[string]any) error {
	return pgx.BeginFunc(ctx, worker.db, func(tx pgx.Tx) error {
		return persistCommunityPostTranslationTx(ctx, tx, taskID, rawPayload, result)
	})
}

func (worker *AIWorker) persistCatalogContentTranslation(ctx context.Context, taskID, createdBy int64, rawPayload []byte, result map[string]any) error {
	return pgx.BeginFunc(ctx, worker.db, func(tx pgx.Tx) error {
		return persistCatalogContentTranslationTx(ctx, tx, taskID, createdBy, rawPayload, result)
	})
}
