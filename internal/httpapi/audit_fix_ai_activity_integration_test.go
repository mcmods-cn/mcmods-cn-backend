package httpapi

import (
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/activity"
)

func auditFixAIActivityFacts(t *testing.T, f test013Fixture) map[string]string {
	t.Helper()
	facts := auditFixGovernanceActivityFacts(t, f)
	for _, table := range []string{"ai_tasks", "ai_task_logs", "dead_letter_events", "app_logs"} {
		var raw string
		filter := ""
		if table == "app_logs" {
			filter = " where action='ai_delivery_retried'"
		}
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` fact`+filter).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		facts[table] = raw
	}
	return facts
}
func auditFixConfigureEnqueueAI(t *testing.T, f test013Fixture) {
	t.Helper()
	s := f.origin.Config.Handler.(*Server)
	cfg := defaultAIConfig()
	cfg.Providers[0].Enabled = true
	cfg.Providers[0].APIKey = "synthetic-not-used"
	cfg.Models[0].Enabled = true
	cfg.Models[0].MaxOutputTokens = 100
	for i := range cfg.TaskModels {
		cfg.TaskModels[i].ModelKey = "openai/gpt-4.1-mini"
	}
	raw, err := s.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values('ai.config',$1) on conflict(key) do update set value=excluded.value`, raw); err != nil {
		t.Fatal(err)
	}
}
func TestAuditFixCMP003AITaskActivityAndDeliveryCreationIdempotencyIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "ai.task.enqueue", "ai.read")
	auditFixConfigureEnqueueAI(t, f)
	if _, err := f.db.Exec(f.ctx, `update task_definitions set status='disabled'`); err != nil {
		t.Fatal(err)
	}
	var rewardTask int64
	if err := f.db.QueryRow(f.ctx, `insert into task_definitions(code,name,condition,rewards,status) values('audit_ai_create_once','AI actual creation once','{"action":"create","objectType":"task","metric":"count","target":1}','{"experience":7,"currencies":{"diamond":2}}','active') returning id`).Scan(&rewardTask); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"taskType": aiTaskI18nTranslation, "concurrencyKey": "actual-business-create", "payload": map[string]any{"targetLocale": "zh-CN", "items": []map[string]string{{"key": "title", "text": "Synthetic source"}}}}
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], true)
	before := auditFixAIActivityFacts(t, f)
	raw := f.require(t, f.editor, http.MethodPost, "/api/v1/admin/ai/tasks", body, 500)
	auditFixRequireSpecificActivityError(t, raw, "failed to persist AI activity")
	auditFixRequireProjectFactsUnchanged(t, before, auditFixAIActivityFacts(t, f))
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], false)
	first := decodeTEST022Data[struct {
		ID string `json:"id"`
	}](t, f.require(t, f.editor, http.MethodPost, "/api/v1/admin/ai/tasks", body, 201))
	if got := auditFixActivityIntentCount(t, f, f.editor, activity.ActionCreate, activity.ObjectTask); got != 1 {
		t.Fatalf("new task intents=%d", got)
	}
	// Idempotent HTTP replay returns the existing UID and cannot create another
	// task, publisher envelope or creation reward, even if activity storage is down.
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], true)
	for range 3 {
		again := decodeTEST022Data[struct {
			ID string `json:"id"`
		}](t, f.require(t, f.editor, http.MethodPost, "/api/v1/admin/ai/tasks", body, 202))
		if again.ID != first.ID {
			t.Fatal("reuse changed task identity")
		}
	}
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], false)
	var tasks, envelopes int
	if err := f.db.QueryRow(f.ctx, `select (select count(*) from ai_tasks),(select count(*) from nats_outbox where aggregate_type='ai_task')`).Scan(&tasks, &envelopes); err != nil || tasks != 1 || envelopes != 1 {
		t.Fatalf("AI task/envelope=%d/%d err=%v", tasks, envelopes, err)
	}
	var eventID string
	if err := f.db.QueryRow(f.ctx, `update nats_outbox set status='dead',attempts=10 where aggregate_type='ai_task' and aggregate_id=$1 returning event_id`, first.ID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into dead_letter_events(event_id,subject,event_type,aggregate_type,aggregate_id,payload,failure_stage,last_error) select event_id,subject,event_type,aggregate_type,aggregate_id,payload,'publish','synthetic supplier unavailable' from nats_outbox where event_id=$1`, eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `alter table app_logs add constraint audit_reject_ai_retry_log check(action<>'ai_delivery_retried') not valid`); err != nil {
		t.Fatal(err)
	}
	before = auditFixAIActivityFacts(t, f)
	response := f.require(t, f.editor, http.MethodPost, "/api/v1/admin/ai/tasks/"+first.ID+"/retry-delivery", nil, 500)
	auditFixRequireSpecificActivityError(t, response, "AI_DELIVERY_RETRY_FAILED")
	auditFixRequireProjectFactsUnchanged(t, before, auditFixAIActivityFacts(t, f))
	if _, err := f.db.Exec(f.ctx, `alter table app_logs drop constraint audit_reject_ai_retry_log`); err != nil {
		t.Fatal(err)
	}
	// Publisher redelivery has its own transactional security audit; it preserves
	// the same task/event rather than issuing a second business Create reward.
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], true)
	f.require(t, f.editor, http.MethodPost, "/api/v1/admin/ai/tasks/"+first.ID+"/retry-delivery", nil, 202)
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], false)
	if got := auditFixActivityIntentCount(t, f, f.editor, activity.ActionCreate, activity.ObjectTask); got != 1 {
		t.Fatalf("publisher retry created extra activity: %d", got)
	}
	before = auditFixAIActivityFacts(t, f)
	f.require(t, f.editor, http.MethodPost, "/api/v1/admin/ai/tasks/"+first.ID+"/retry-delivery", nil, 409)
	auditFixRequireProjectFactsUnchanged(t, before, auditFixAIActivityFacts(t, f))
	auditFixActivityMonitor(t, f)
	auditFixWaitActivity(t, func() bool {
		var n int
		err := f.db.QueryRow(f.ctx, `select count(*) from user_task_progress where user_id=$1 and task_id=$2 and rewarded_at is not null`, f.userIDs[f.editor], rewardTask).Scan(&n)
		return err == nil && n == 1
	})
	var events, progress, rewarded, xp, balance, ledgers, audits int64
	if err := f.db.QueryRow(f.ctx, `select
 (select count(*) from user_activity_events where user_id=$1 and action_id=$3 and object_type_id=$4),
 (select progress from user_task_progress where user_id=$1 and task_id=$2),
 (select count(*) from user_task_progress where user_id=$1 and task_id=$2 and rewarded_at is not null),
 (select experience from user_experience where user_id=$1),
 (select balance from user_currency_balances b join currencies c on c.id=b.currency_id where b.user_id=$1 and c.code='diamond'),
 (select count(*) from currency_transactions where user_id=$1),
 (select count(*) from app_logs where action='ai_delivery_retried' and target=$5)`, f.userIDs[f.editor], rewardTask, activity.ActionCreate, activity.ObjectTask, first.ID).Scan(&events, &progress, &rewarded, &xp, &balance, &ledgers, &audits); err != nil {
		t.Fatal(err)
	}
	if events != 1 || progress != 1 || rewarded != 1 || xp != 7 || balance != 2 || ledgers != 1 || audits != 1 {
		t.Fatalf("AI creation reward event/progress/reward/xp/balance/ledger/retryaudit=%d/%d/%d/%d/%d/%d/%d", events, progress, rewarded, xp, balance, ledgers, audits)
	}
}

func TestAuditFixCMP003CommunityTranslationActivitySharesQuotaAndTaskTransaction(t *testing.T) {
	fixture := newTEST042Fixture(t)
	fixture.reviewPolicy(t, false)
	post := fixture.create(t, fixture.editor, fixture.snapshot("discussion", "Synthetic translation activity"))
	f := fixture.test013Fixture
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "content.translate", "user.ai.daily_token_limit.100000")
	auditFixConfigureEnqueueAI(t, f)
	path := "/api/v1/community/posts/" + post.ID + "/translations"
	body := map[string]string{"targetLocale": "fr-FR"}
	prior := auditFixActivityIntentCount(t, f, f.editor, activity.ActionCreate, activity.ObjectCommunityPost)
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], true)
	before := auditFixAIActivityFacts(t, f)
	raw := f.require(t, f.editor, http.MethodPost, path, body, 503)
	auditFixRequireSpecificActivityError(t, raw, "failed to persist AI translation activity")
	auditFixRequireProjectFactsUnchanged(t, before, auditFixAIActivityFacts(t, f))
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], false)
	first := decodeTEST022Data[struct {
		TaskID string `json:"taskId"`
	}](t, f.require(t, f.editor, http.MethodPost, path, body, 202))
	if got := auditFixActivityIntentCount(t, f, f.editor, activity.ActionCreate, activity.ObjectCommunityPost); got != prior+1 {
		t.Fatalf("translation activity=%d prior=%d", got, prior)
	}
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], true)
	again := decodeTEST022Data[struct {
		TaskID string `json:"taskId"`
	}](t, f.require(t, f.editor, http.MethodPost, path, body, 202))
	if again.TaskID != first.TaskID {
		t.Fatal("translation duplicate changed task")
	}
	if got := auditFixActivityIntentCount(t, f, f.editor, activity.ActionCreate, activity.ObjectCommunityPost); got != prior+1 {
		t.Fatalf("translation duplicate activity=%d prior=%d", got, prior)
	}
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], false)
	var tasks, envelopes int
	var reservation int64
	if err := f.db.QueryRow(f.ctx, `select (select count(*) from ai_tasks),(select count(*) from nats_outbox where aggregate_type='ai_task'),(select quota_reserved_tokens from ai_tasks where task_uid=$1)`, first.TaskID).Scan(&tasks, &envelopes, &reservation); err != nil || tasks != 1 || envelopes != 1 || reservation <= 0 {
		t.Fatalf("translation tasks/envelopes/reservation=%d/%d/%d err=%v", tasks, envelopes, reservation, err)
	}
	// A typed cache fixture tests the cached HTTP branch only. Actual provider
	// execution and fenced result persistence remain the separate TEST043 layer.
	if _, err := f.db.Exec(f.ctx, `insert into community_post_translations(post_id,locale,title,body_markdown,source_revision_id,ai_task_id)
 select post.id,'fr-FR','Titre synthétique','Corps synthétique',post.published_revision_id,task.id from community_posts post cross join ai_tasks task where post.public_id=$1 and task.task_uid=$2`, post.ID, first.TaskID); err != nil {
		t.Fatal(err)
	}
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], true)
	cached := decodeTEST022Data[struct {
		Cached bool `json:"cached"`
	}](t, f.require(t, f.editor, http.MethodPost, path, body, 200))
	if !cached.Cached {
		t.Fatal("cached translation was re-enqueued")
	}
	if got := auditFixActivityIntentCount(t, f, f.editor, activity.ActionCreate, activity.ObjectCommunityPost); got != prior+1 {
		t.Fatalf("cached read created activity=%d prior=%d", got, prior)
	}
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], false)
}
