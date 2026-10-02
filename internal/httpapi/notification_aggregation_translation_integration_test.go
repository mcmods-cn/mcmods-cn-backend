package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestOCT02AggregatedNotificationInvalidatesTranslationsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := newOCT02IsolatedDatabase(t, ctx)
	var recipient, firstActor, secondActor int64
	for name, id := range map[string]*int64{"recipient": &recipient, "first": &firstActor, "second": &secondActor} {
		if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values($1,$2,'not-used') returning id`, "oct02notify-"+name, name+"@example.invalid").Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	worker := NewNotificationWorker(pool, nil, nil, config.SMTPConfig{}, "")
	send := func(event notificationEvent) error {
		raw, err := json.Marshal(event)
		if err != nil {
			return err
		}
		return worker.handleEvent(ctx, raw)
	}
	for _, action := range []string{"follow", "comment_watch"} {
		t.Run(action, func(t *testing.T) {
			event := notificationEvent{Action: action, RecipientID: recipient, ActorID: firstActor, Title: "first source", Body: "first body", SourceLocale: "zh-CN", Data: map[string]any{"watchId": "watch-synthetic", "replyCount": 1}}
			if err := send(event); err != nil {
				t.Fatal(err)
			}
			kind := "new_follower"
			if action == "comment_watch" {
				kind = "comment_watch_reply"
			}
			var notificationID int64
			if err := pool.QueryRow(ctx, `select id from notifications where recipient_id=$1 and kind=$2`, recipient, kind).Scan(&notificationID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `insert into notification_translations(notification_id,user_id,locale,title,body) values($1,$2,'en-US','obsolete title','obsolete body')`, notificationID, recipient); err != nil {
				t.Fatal(err)
			}
			event.ActorID = secondActor
			event.Body = "second source body"
			if err := send(event); err != nil {
				t.Fatal(err)
			}
			var cached int
			if err := pool.QueryRow(ctx, `select count(*) from notification_translations where notification_id=$1`, notificationID).Scan(&cached); err != nil {
				t.Fatal(err)
			}
			if cached != 0 {
				t.Fatal("source changed but obsolete successful translation survived")
			}
			var count int
			if err := pool.QueryRow(ctx, `select count(*) from notifications where recipient_id=$1 and kind=$2`, recipient, kind).Scan(&count); err != nil || count != 1 {
				t.Fatalf("aggregation broke: %d %v", count, err)
			}
		})
	}
	t.Run("literal_template_values_are_persisted", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `update users set preferred_ui_language='en-US' where id=$1`, recipient); err != nil {
			t.Fatal(err)
		}
		event := notificationEvent{Action: "direct", Kind: "system", RecipientID: recipient, TemplateKey: "project_updated", TemplateValues: map[string]string{"project_name": "Mod {unknown} {changed_sections}", "changed_sections": "description {project_name}"}}
		if err := send(event); err != nil {
			t.Fatal(err)
		}
		var body string
		if err := pool.QueryRow(ctx, `select body from notifications where recipient_id=$1 and kind='system'`, recipient).Scan(&body); err != nil {
			t.Fatal(err)
		}
		if body != "Mod {unknown} {changed_sections} updated description {project_name}." {
			t.Fatalf("literal values changed during persistence: %q", body)
		}
	})
}

func TestOCT02NotificationTranslationDeduplicationIncludesSourceSnapshotIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := newOCT02IsolatedDatabase(t, ctx)
	server := &Server{db: pool, cfg: config.Load()}
	cfg := defaultAIConfig()
	cfg.Providers[0].Enabled = true
	cfg.Providers[0].APIKey = "synthetic-unused-key"
	cfg.Models[0].Enabled = true
	cfg.Models[0].MaxOutputTokens = 100
	for i := range cfg.TaskModels {
		cfg.TaskModels[i].ModelKey = "openai/gpt-4.1-mini"
	}
	sealed, err := server.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('ai.config',$1::jsonb) on conflict(key) do update set value=excluded.value`, sealed); err != nil {
		t.Fatal(err)
	}
	var user, notification int64
	var publicID string
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash) values('notification-source','notification-source@example.invalid','unused') returning id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into notifications(recipient_id,kind,title,body,source_locale) values($1,'comment_watch_reply','first title','first body','zh-CN') returning id,public_id`, user).Scan(&notification, &publicID); err != nil {
		t.Fatal(err)
	}
	claims := security.Claims{Subject: user, PermissionRules: []security.PermissionRule{{Code: "user.ai.daily_token_limit.100000", Allow: true}}}
	invoke := func() string {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/"+publicID+"/translate", strings.NewReader(`{"targetLocale":"en-US"}`))
		request.SetPathValue("id", publicID)
		request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
		response := httptest.NewRecorder()
		server.translateNotification(response, request)
		if response.Code != http.StatusAccepted {
			t.Fatalf("enqueue status=%d body=%s", response.Code, response.Body.String())
		}
		var envelope struct {
			Data struct {
				TaskID string `json:"taskId"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Data.TaskID == "" {
			t.Fatalf("task response: %s/%v", response.Body.String(), err)
		}
		return envelope.Data.TaskID
	}
	first := invoke()
	if same := invoke(); same != first {
		t.Fatal("identical source snapshot created another task")
	}
	if _, err = pool.Exec(ctx, `update notifications set body='changed body',source_locale='en-US' where id=$1`, notification); err != nil {
		t.Fatal(err)
	}
	if changed := invoke(); changed == first {
		t.Fatal("changed source reused obsolete queued task")
	}
	var tasks, outbox int
	if err = pool.QueryRow(ctx, `select (select count(*) from ai_tasks where created_by=$1),(select count(*) from nats_outbox where aggregate_type='ai_task')`, user).Scan(&tasks, &outbox); err != nil || tasks != 2 || outbox != 2 {
		t.Fatalf("tasks/outbox=%d/%d err=%v", tasks, outbox, err)
	}
}
