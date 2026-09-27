package httpapi

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDirectMessageEmailUsesLocalizedTransactionalOutbox(t *testing.T) {
	messageSource, err := os.ReadFile("message_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	messageText := string(messageSource)
	start := strings.Index(messageText, "func (s *Server) sendConversationMessage(")
	end := strings.Index(messageText, "func (s *Server) updateConversationPresence(")
	if start < 0 || end <= start {
		t.Fatal("direct message handler boundary was not found")
	}
	handler := messageText[start:end]
	for _, forbidden := range []string{"给你发来一条私聊", "enqueueNotificationTask("} {
		if strings.Contains(handler, forbidden) {
			t.Fatalf("direct message handler retains split/fixed-language path %q", forbidden)
		}
	}
	if !strings.Contains(handler, "createDirectMessageTx(r.Context(), tx") || !strings.Contains(handler, `TemplateKey: "direct_message_email"`) {
		t.Fatal("direct message handler does not use the localized transactional message primitive")
	}

	reliableSource, err := os.ReadFile("reliable_notification.go")
	if err != nil {
		t.Fatal(err)
	}
	reliable := string(reliableSource)
	for _, forbidden := range []string{"OutboxEnabled", "PublishTask"} {
		if strings.Contains(reliable, forbidden) {
			t.Fatalf("reliable notification producer retains alternate queue path %q", forbidden)
		}
	}
	workerSource, err := os.ReadFile("notification_worker.go")
	if err != nil {
		t.Fatal(err)
	}
	worker := string(workerSource)
	if strings.Contains(worker, "PublishTask") {
		t.Fatal("notification worker still forwards email through Core NATS")
	}
	if !strings.Contains(worker, "renderNotificationTemplate(ctx, worker.db, event.RecipientID, event.TemplateKey") {
		t.Fatal("email worker does not render the recipient-localized template")
	}
}

func TestDirectMessageEmailTemplateIsLocalized(t *testing.T) {
	config := defaultNotificationTemplateConfig()
	english, err := renderNotificationTemplateForLocale(config, "en-US", "direct_message_email", map[string]string{"sender": "Alex", "preview": "Hello"})
	if err != nil {
		t.Fatal(err)
	}
	chinese, err := renderNotificationTemplateForLocale(config, "zh-CN", "direct_message_email", map[string]string{"sender": "小明", "preview": "你好"})
	if err != nil {
		t.Fatal(err)
	}
	if english.Locale != "en-US" || !strings.Contains(english.Title, "Alex") || !strings.Contains(english.Body, "Hello") {
		t.Fatalf("unexpected English direct-message email: %#v", english)
	}
	if chinese.Locale != "zh-CN" || !strings.Contains(chinese.Title, "小明") || !strings.Contains(chinese.Body, "你好") || english.Title == chinese.Title {
		t.Fatalf("unexpected Chinese direct-message email: %#v", chinese)
	}
}

func TestDirectMessageAndEmailOutboxCommitOrRollbackTogether(t *testing.T) {
	db := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	connection, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	if _, err = connection.Exec(ctx, `create temp table direct_conversations(id bigint primary key,last_message_id bigint,updated_at timestamptz not null);
		create temp table direct_messages(
			id bigserial primary key,public_id text not null unique default md5(random()::text),conversation_id bigint not null,
			sender_id bigint not null,recipient_id bigint not null,body text not null,read_at timestamptz,created_at timestamptz not null default now());
		create temp table nats_outbox(
			id bigserial primary key,event_id text not null unique,event_type text not null,schema_version integer not null default 1,
			subject text not null,aggregate_type text not null,aggregate_id text not null,trace_id text not null default '',
			payload jsonb not null,occurred_at timestamptz not null default now(),status text not null default 'pending',
			available_at timestamptz not null default now(),created_at timestamptz not null default now());
		insert into direct_conversations(id,updated_at) values(5,'2000-01-01')`); err != nil {
		t.Fatal(err)
	}

	event := notificationEvent{Action: "email", RecipientID: 8, TemplateKey: "direct_message_email", TemplateValues: map[string]string{"sender": "Alex", "preview": "Hello"}}
	tx, err := connection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	item, err := createDirectMessageTx(ctx, tx, 5, 7, 8, "Hello", false, &event, "trace-message")
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var messages, events int
	var aggregateID, traceID, templateKey, sender string
	if err = connection.QueryRow(ctx, `select count(*) from direct_messages where public_id=$1`, item.ID).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if err = connection.QueryRow(ctx, `select count(*),coalesce(max(aggregate_id),''),coalesce(max(trace_id),''),
		coalesce(max(payload->>'templateKey'),''),coalesce(max(payload#>>'{templateValues,sender}'),'')
		from nats_outbox where subject='notifications' and event_type='notification.direct_message_email.requested'`).
		Scan(&events, &aggregateID, &traceID, &templateKey, &sender); err != nil {
		t.Fatal(err)
	}
	if messages != 1 || events != 1 || aggregateID != item.ID || traceID != "trace-message" || templateKey != "direct_message_email" || sender != "Alex" {
		t.Fatalf("message/outbox = %d/%d aggregate=%q trace=%q template=%q sender=%q", messages, events, aggregateID, traceID, templateKey, sender)
	}
	var committedUpdatedAt time.Time
	var committedLastMessageID int64
	if err = connection.QueryRow(ctx, `select updated_at,last_message_id from direct_conversations where id=5`).Scan(&committedUpdatedAt, &committedLastMessageID); err != nil {
		t.Fatal(err)
	}
	if committedLastMessageID <= 0 {
		t.Fatalf("conversation last_message_id=%d", committedLastMessageID)
	}

	if _, err = connection.Exec(ctx, `alter table nats_outbox add constraint reject_message_email check(payload#>>'{templateValues,sender}'<>'Rejected')`); err != nil {
		t.Fatal(err)
	}
	rejected := notificationEvent{Action: "email", RecipientID: 8, TemplateKey: "direct_message_email", TemplateValues: map[string]string{"sender": "Rejected", "preview": "No"}}
	tx, err = connection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = createDirectMessageTx(ctx, tx, 5, 7, 8, "must rollback", false, &rejected, "trace-rejected"); err == nil {
		t.Fatal("outbox constraint failure was not returned")
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = connection.QueryRow(ctx, `select count(*) from direct_messages where body='must rollback'`).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	var updatedAt time.Time
	var lastMessageID int64
	if err = connection.QueryRow(ctx, `select updated_at,last_message_id from direct_conversations where id=5`).Scan(&updatedAt, &lastMessageID); err != nil {
		t.Fatal(err)
	}
	if messages != 0 || !updatedAt.Equal(committedUpdatedAt) || lastMessageID != committedLastMessageID {
		t.Fatalf("failed event left message/conversation changes: messages=%d updated=%s/%d want=%s/%d", messages, updatedAt, lastMessageID, committedUpdatedAt, committedLastMessageID)
	}
}
