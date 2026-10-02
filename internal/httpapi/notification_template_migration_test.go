package httpapi

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
)

func TestMigratedSystemNotificationsHaveLocalizedTemplates(t *testing.T) {
	configuration := defaultNotificationTemplateConfig()
	if err := validateNotificationTemplateConfig(configuration); err != nil {
		t.Fatalf("default notification templates are invalid: %v", err)
	}
	values := map[string]map[string]string{
		"project_editor_application_approved": {"name": "Example", "note": "Looks good"},
		"project_editor_application_rejected": {"name": "Example", "note": "Needs work"},
		"creator_claim_revoked":               {"name": "Creator", "reason": "Ownership changed"},
		"review_completed":                    {"name": "Example"},
		"new_follower":                        {"actors": "Alex, Steve", "count": "2"},
		"account_banned":                      nil,
		"ban_released":                        nil,
		"report_resolved_valid":               nil,
		"report_resolved_invalid":             nil,
		"moderation_action":                   nil,
	}
	for code, templateValues := range values {
		t.Run(code, func(t *testing.T) {
			english, err := renderNotificationTemplateForLocale(configuration, "en-US", code, templateValues)
			if err != nil {
				t.Fatal(err)
			}
			chinese, err := renderNotificationTemplateForLocale(configuration, "zh-CN", code, templateValues)
			if err != nil {
				t.Fatal(err)
			}
			if english.Key != code || english.Locale != "en-US" || chinese.Locale != "zh-CN" {
				t.Fatalf("unexpected template identity: English=%#v Chinese=%#v", english, chinese)
			}
			if english.Title == chinese.Title || english.Body == chinese.Body {
				t.Fatalf("template %q did not select locale-specific copy", code)
			}
		})
	}
}

func TestSystemNotificationMigrationRemovesFixedPersistencePaths(t *testing.T) {
	checks := map[string][]string{
		"project_editor_application_handlers.go": {"project_editor_application_approved", "project_editor_application_rejected", "enqueueTemplatedNotificationTx"},
		"creator_handlers.go":                    {"creator_claim_revoked", "enqueueTemplatedNotificationTx"},
		"review_lock_handlers.go":                {"review_completed", "enqueueTemplatedNotificationTx"},
		"governance_handlers.go":                 {"account_banned", "ban_released", "report_resolved_valid", "report_resolved_invalid", "moderation_action"},
		"notification_worker.go":                 {`renderNotificationTemplate(ctx, tx, event.RecipientID, "new_follower"`, "template_version", "enqueueUserEmailTx"},
	}
	for filename, required := range checks {
		raw, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for _, fragment := range required {
			if !strings.Contains(text, fragment) {
				t.Errorf("%s is missing migrated notification evidence %q", filename, fragment)
			}
		}
	}

	for _, filename := range []string{"project_editor_application_handlers.go", "creator_handlers.go", "review_lock_handlers.go", "governance_handlers.go", "notification_worker.go", "comment_handlers.go"} {
		raw, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for _, forbidden := range []string{"enqueueOrCreateDirectNotification", "enqueueDirectNotificationTx", "followerNotificationBody"} {
			if strings.Contains(text, forbidden) {
				t.Errorf("%s retains legacy direct notification path %q", filename, forbidden)
			}
		}
	}

	raw, err := os.ReadFile("notification_template_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	templateSource := string(raw)
	start := strings.Index(templateSource, "func (s *Server) sendTemplatedNotification(")
	end := strings.Index(templateSource, "func (s *Server) getNotificationTemplates(")
	if start < 0 || end <= start {
		t.Fatal("templated notification sender boundary was not found")
	}
	sender := templateSource[start:end]
	if strings.Contains(strings.ToLower(sender), "insert into notifications") || !strings.Contains(sender, "slog.Error") || !strings.Contains(sender, "return err") {
		t.Fatalf("templated notification sender is not an observable Outbox-only path:\n%s", sender)
	}
}

func TestNotificationTemplateFailuresAreExplicitIntegration(t *testing.T) {
	db := openNotificationTemplateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	createNotificationTemplateTestTables(t, ctx, db)
	if _, err := db.Exec(ctx, `insert into users(id,username,preferred_ui_language) values(1,'recipient','en-US')`); err != nil {
		t.Fatal(err)
	}

	rendered, err := renderNotificationTemplate(ctx, db, 1, "account_banned", nil)
	if err != nil || rendered.Locale != "en-US" {
		t.Fatalf("default template render = %#v, %v", rendered, err)
	}
	if _, err = db.Exec(ctx, `insert into system_settings(key,value) values($1,'"broken"'::jsonb)`, notificationTemplatesSettingKey); err != nil {
		t.Fatal(err)
	}
	if _, err = loadNotificationTemplateConfig(ctx, db); err == nil || !strings.Contains(err.Error(), "decode notification templates") {
		t.Fatalf("malformed template setting returned %v", err)
	}
	if _, err = renderNotificationTemplate(ctx, db, 1, "account_banned", nil); err == nil || !strings.Contains(err.Error(), "decode notification templates") {
		t.Fatalf("render swallowed malformed template setting: %v", err)
	}

	if _, err = db.Exec(ctx, `delete from system_settings; alter table system_settings drop column value`); err != nil {
		t.Fatal(err)
	}
	if _, err = loadNotificationTemplateConfig(ctx, db); err == nil || !strings.Contains(err.Error(), "load notification templates") {
		t.Fatalf("template loader swallowed database query failure: %v", err)
	}
	if _, err = db.Exec(ctx, `alter table users drop column preferred_ui_language`); err != nil {
		t.Fatal(err)
	}
	if _, err = renderNotificationTemplate(ctx, db, 1, "account_banned", nil); err == nil || !strings.Contains(err.Error(), "load notification recipient 1 locale") {
		t.Fatalf("render swallowed recipient locale query failure: %v", err)
	}
}

func TestTemplatedNotificationOutboxCommitsOrRollsBackIntegration(t *testing.T) {
	db := openNotificationTemplateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	createNotificationTemplateTestTables(t, ctx, db)

	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into business_notification_facts(id,state) values(1,'approved')`); err != nil {
		t.Fatal(err)
	}
	if err = enqueueTemplatedNotificationTx(ctx, tx, "project_editor.reviewed", 41, 7,
		"project_editor_application_approved", map[string]string{"name": "Example", "note": "OK"},
		map[string]any{"applicationId": "application-1"}, "trace-notification"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var factCount, eventCount int
	var eventType, aggregateType, aggregateID, traceID, action, kind, templateKey, name string
	var recipientID, actorID int64
	if err = db.QueryRow(ctx, `select count(*),coalesce(max(event_type),''),coalesce(max(aggregate_type),''),
		coalesce(max(aggregate_id),''),coalesce(max(trace_id),''),coalesce(max(payload->>'action'),''),
		coalesce(max(payload->>'kind'),''),coalesce(max(payload->>'templateKey'),''),
		coalesce(max(payload#>>'{templateValues,name}'),''),coalesce(max((payload->>'recipientId')::bigint),0),
		coalesce(max((payload->>'actorId')::bigint),0) from nats_outbox`).
		Scan(&eventCount, &eventType, &aggregateType, &aggregateID, &traceID, &action, &kind, &templateKey, &name, &recipientID, &actorID); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, `select count(*) from business_notification_facts where id=1`).Scan(&factCount); err != nil {
		t.Fatal(err)
	}
	if factCount != 1 || eventCount != 1 || eventType != "project_editor.reviewed" || aggregateType != "user" || aggregateID != "41" ||
		traceID != "trace-notification" || action != "direct" || kind != "system" || templateKey != "project_editor_application_approved" ||
		name != "Example" || recipientID != 41 || actorID != 7 {
		t.Fatalf("business/event=%d/%d type=%q aggregate=%q/%q trace=%q action=%q kind=%q template=%q name=%q recipient=%d actor=%d",
			factCount, eventCount, eventType, aggregateType, aggregateID, traceID, action, kind, templateKey, name, recipientID, actorID)
	}

	if _, err = db.Exec(ctx, `alter table nats_outbox add constraint reject_notification_template
		check(payload->>'templateKey'<>'reject_me')`); err != nil {
		t.Fatal(err)
	}
	tx, err = db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into business_notification_facts(id,state) values(2,'must_rollback')`); err != nil {
		t.Fatal(err)
	}
	if err = enqueueTemplatedNotificationTx(ctx, tx, "test.rejected", 42, 0, "reject_me", nil, nil, ""); err == nil {
		t.Fatal("Outbox constraint failure was not returned")
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, `select count(*) from business_notification_facts where id=2`).Scan(&factCount); err != nil {
		t.Fatal(err)
	}
	if factCount != 0 {
		t.Fatalf("failed notification event left %d business facts", factCount)
	}
	server := &Server{db: db}
	if err = server.sendTemplatedNotification(ctx, 42, "reject_me", nil, nil); err == nil {
		t.Fatal("standalone templated notification swallowed Outbox failure")
	}
}

func TestReviewCompletionNotificationFactsCommitAtomicallyIntegration(t *testing.T) {
	db := openNotificationTemplateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	createNotificationTemplateTestTables(t, ctx, db)
	if _, err := db.Exec(ctx, `create temp table change_requests(id bigint primary key,public_id text not null);
		create temp table review_completion_subscriptions(
			change_request_id bigint not null,user_id bigint not null,target_label text not null,target_url text not null,
			notified_at timestamptz,primary key(change_request_id,user_id));
		insert into change_requests(id,public_id) values(5,'change-5'),(6,'change-6');
		insert into review_completion_subscriptions(change_request_id,user_id,target_label,target_url)
		values(5,11,'Project A','/mods/a'),(5,12,'Project B','/mods/b'),(6,19,'Rejected','/mods/rejected')`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = createReviewCompletionNotificationsTx(ctx, tx, 5, "approved"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var notified, events int
	if err = db.QueryRow(ctx, `select count(*) from review_completion_subscriptions
		where change_request_id=5 and notified_at is not null`).Scan(&notified); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, `select count(*) from nats_outbox
		where event_type='review.completed' and payload->>'templateKey'='review_completed'
		and payload#>>'{data,reviewStatus}'='approved'`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if notified != 2 || events != 2 {
		t.Fatalf("review completion subscriptions/events = %d/%d, want 2/2", notified, events)
	}

	if _, err = db.Exec(ctx, `alter table nats_outbox add constraint reject_review_recipient
		check(coalesce((payload->>'recipientId')::bigint,0)<>19)`); err != nil {
		t.Fatal(err)
	}
	tx, err = db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = createReviewCompletionNotificationsTx(ctx, tx, 6, "rejected"); err == nil {
		t.Fatal("review completion Outbox failure was not returned")
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, `select count(*) from review_completion_subscriptions
		where change_request_id=6 and notified_at is not null`).Scan(&notified); err != nil {
		t.Fatal(err)
	}
	if notified != 0 {
		t.Fatalf("failed review notification marked %d subscriptions notified", notified)
	}
}

func TestFollowerWorkerPersistsLocalizedTemplateSnapshotIntegration(t *testing.T) {
	db := openNotificationTemplateTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	createNotificationTemplateTestTables(t, ctx, db)
	if _, err := db.Exec(ctx, `insert into users(id,username,preferred_ui_language)
		values(1,'Recipient','en-US'),(2,'Alex','zh-CN')`); err != nil {
		t.Fatal(err)
	}
	cache := querycache.New(config.RedisConfig{})
	worker := NewNotificationWorker(db, nil, cache, config.SMTPConfig{}, "")
	if err := worker.aggregateFollowerNotification(ctx, notificationEvent{Action: "follow", RecipientID: 1, ActorID: 2}); err != nil {
		t.Fatal(err)
	}

	var title, body, locale, templateKey, actors, count string
	if err := db.QueryRow(ctx, `select title,body,source_locale,template_key,
		template_params->>'actors',template_params->>'count' from notifications where recipient_id=1 and kind='new_follower'`).
		Scan(&title, &body, &locale, &templateKey, &actors, &count); err != nil {
		t.Fatal(err)
	}
	if locale != "en-US" || templateKey != "new_follower" || actors != "Alex" || count != "1" ||
		!strings.Contains(title, "follower") || !strings.Contains(body, "Alex") {
		t.Fatalf("localized follower snapshot title=%q body=%q locale=%q template=%q actors=%q count=%q", title, body, locale, templateKey, actors, count)
	}
	var emailAction, emailTitle, emailBody string
	if err := db.QueryRow(ctx, `select payload->>'action',payload->>'title',payload->>'body'
		from nats_outbox where event_type='notification.email.requested'`).Scan(&emailAction, &emailTitle, &emailBody); err != nil {
		t.Fatal(err)
	}
	if emailAction != "email" || emailTitle != title || emailBody != body {
		t.Fatalf("email event action=%q title/body=%q/%q want snapshot %q/%q", emailAction, emailTitle, emailBody, title, body)
	}
}

func openNotificationTemplateTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to run notification template integration tests")
		}
		databaseURL = config.Load().DB.ConnString()
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func createNotificationTemplateTestTables(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	if _, err := db.Exec(ctx, `create temp table users(
		id bigint primary key,username text not null,preferred_ui_language text not null default 'zh-CN');
		create temp table system_settings(key text primary key,value jsonb not null);
		create temp table notifications(
			id bigserial primary key,recipient_id bigint,kind text not null,title text not null,body text not null,
			source_locale text not null,data jsonb not null default '{}'::jsonb,source_event_id text,
			template_key text,template_version integer not null default 0,template_params jsonb not null default '{}'::jsonb,
			updated_at timestamptz not null default now());
		create unique index notification_source_event_unique on notifications(source_event_id) where source_event_id is not null;
		create temp table notification_actors(
			notification_id bigint not null,actor_id bigint not null,created_at timestamptz not null default now(),
			primary key(notification_id,actor_id));
		create temp table notification_receipts(
			notification_id bigint not null,user_id bigint not null,read_at timestamptz,primary key(notification_id,user_id));
		create temp table nats_outbox(
			id bigserial primary key,event_id text not null unique,event_type text not null,schema_version integer not null default 1,
			subject text not null,aggregate_type text not null,aggregate_id text not null,trace_id text not null default '',
			payload jsonb not null,occurred_at timestamptz not null default now(),status text not null default 'pending',
			available_at timestamptz not null default now(),created_at timestamptz not null default now());
		create temp table business_notification_facts(id bigint primary key,state text not null)`); err != nil {
		t.Fatal(err)
	}
}
