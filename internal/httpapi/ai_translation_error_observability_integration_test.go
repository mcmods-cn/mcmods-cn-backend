package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/security"
)

func TestCatalogTranslationResultRejectsCorruptCompletedTasksIntegration(t *testing.T) {
	// The source-visibility contract needs real parent tables and constraints;
	// a task-only temporary table cannot represent a currently readable source.
	f := newTEST013Fixture(t)
	pool := f.db
	ctx, cancel := context.WithTimeout(f.ctx, 20*time.Second)
	defer cancel()
	var err error
	if _, err = pool.Exec(ctx, `
        insert into users(id,username,email,password_hash,status) values
          (42,'arch026-owner','arch026-owner@example.test','not-used','active'),
          (77,'arch026-other','arch026-other@example.test','not-used','active');
        insert into mods(id,project_code,slug,primary_name,review_status) values(7,'m00000007','arch026-source','Source','approved');
        insert into content_subjects(subject_type,subject_id,default_locale) values('mod',7,'en-US') on conflict do nothing;
        insert into content_localizations(subject_type,subject_id,locale,name,review_status) values('mod',7,'en-US','Source','approved') on conflict do nothing;
        insert into notifications(id,recipient_id,kind,title,body) values(17,42,'system','Source','Body'),(18,42,'system','Source','Body');
		insert into ai_tasks(task_uid,task_type,status,result,payload,error,created_by,input_tokens,output_tokens) values
			('arch026good','content_translation_completion','completed','{"items":[{"key":"name","text":"名称"}]}'::jsonb,
			 '{"internalEntityId":7,"publicId":"m00000007","sourceLocale":"en-US","sourceRevisionNo":3,"targetLocale":"zh-CN"}'::jsonb,'',42,10,20),
			('arch026payload','content_translation_completion','completed','{"items":[{"key":"name","text":"名称"}]}'::jsonb,
			 '"corrupt-payload"'::jsonb,'',42,10,20),
			('arch026result','content_translation_completion','completed','"corrupt-result"'::jsonb,
			 '{"internalEntityId":7,"publicId":"m00000007","sourceLocale":"en-US","sourceRevisionNo":3,"targetLocale":"zh-CN"}'::jsonb,'',42,10,20),
			('arch026item','content_translation_completion','completed','{"items":[{"key":"name","text":17}]}'::jsonb,
			 '{"internalEntityId":7,"publicId":"m00000007","sourceLocale":"en-US","sourceRevisionNo":3,"targetLocale":"zh-CN"}'::jsonb,'',42,10,20),
			('arch026running','content_translation_completion','running','"not-yet-a-result"'::jsonb,'{"internalEntityId":7,"publicId":"m00000007","sourceLocale":"en-US","sourceRevisionNo":3,"targetLocale":"zh-CN"}'::jsonb,'',42,0,0),
			('arch026other','content_translation_completion','completed','{"items":[{"key":"name","text":"名称"}]}'::jsonb,
			 '{"internalEntityId":7,"publicId":"m00000007","sourceLocale":"en-US","sourceRevisionNo":3,"targetLocale":"zh-CN"}'::jsonb,'',77,10,20),
			('arch026notifgood','notification_translation_completion','completed','{"items":[{"key":"title","text":"标题"},{"key":"body","text":"正文"}]}'::jsonb,
			 '{"notificationId":18,"targetLocale":"zh-CN"}'::jsonb,'',42,5,7),
			('arch026notifpayload','notification_translation_completion','completed','{"items":[{"key":"title","text":"标题"}]}'::jsonb,
			 '"corrupt-payload"'::jsonb,'',42,5,7),
			('arch026notifresult','notification_translation_completion','completed','"corrupt-result"'::jsonb,
			 '{"notificationId":18,"targetLocale":"zh-CN"}'::jsonb,'',42,5,7),
			('arch026notifitem','notification_translation_completion','completed','{"items":[{"key":"body","text":17}]}'::jsonb,
			 '{"notificationId":18,"targetLocale":"zh-CN"}'::jsonb,'',42,5,7);
		insert into notification_translations(notification_id,user_id,locale,title,body,created_at)
			values(18,42,'zh-CN','标题','正文','2020-01-01T00:00:00Z');
	`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	for _, test := range []struct {
		name, taskID string
		want         int
	}{
		{"healthy", "arch026good", http.StatusOK},
		{"corrupt payload", "arch026payload", http.StatusInternalServerError},
		{"corrupt result", "arch026result", http.StatusInternalServerError},
		{"corrupt item", "arch026item", http.StatusInternalServerError},
		{"running does not parse unfinished result", "arch026running", http.StatusOK},
		{"wrong owner", "arch026other", http.StatusForbidden},
		{"missing", "arch026absent", http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, body := invokeCatalogTranslationResult(server, ctx, test.taskID)
			if status != test.want {
				t.Fatalf("status=%d body=%s; want %d", status, body, test.want)
			}
		})
	}
	for _, test := range []struct {
		name, taskID string
		want         int
	}{
		{"notification healthy", "arch026notifgood", http.StatusOK},
		{"notification corrupt payload", "arch026notifpayload", http.StatusInternalServerError},
		{"notification corrupt result", "arch026notifresult", http.StatusInternalServerError},
		{"notification corrupt item", "arch026notifitem", http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, body := invokeNotificationTranslationResult(server, ctx, test.taskID)
			if status != test.want {
				t.Fatalf("status=%d body=%s; want %d", status, body, test.want)
			}
		})
	}
	var persistedAt time.Time
	if err = pool.QueryRow(ctx, `select created_at from notification_translations where notification_id=18 and user_id=42 and locale='zh-CN'`).Scan(&persistedAt); err != nil || !persistedAt.Equal(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("notification result GET mutated persisted business result: time=%s error=%v", persistedAt, err)
	}

	worker := NewAIWorker(pool, nil, "")
	notificationPayload := []byte(`{"notificationId":17,"targetLocale":"zh-CN"}`)
	notificationResult := map[string]any{"items": []any{
		map[string]any{"key": "title", "text": "标题"},
		map[string]any{"key": "body", "text": "正文"},
	}}
	if err = worker.persistNotificationTranslation(ctx, 42, notificationPayload, notificationResult); err != nil {
		t.Fatalf("persist healthy notification translation: %v", err)
	}
	var title, body string
	if err = pool.QueryRow(ctx, `select title,body from notification_translations where notification_id=17 and user_id=42 and locale='zh-CN'`).Scan(&title, &body); err != nil || title != "标题" || body != "正文" {
		t.Fatalf("persisted notification=%q/%q error=%v", title, body, err)
	}
	if err = worker.persistNotificationTranslation(ctx, 42, []byte(`"corrupt"`), notificationResult); err == nil {
		t.Fatal("corrupt notification payload was silently ignored")
	}
	if err = worker.persistNotificationTranslation(ctx, 42, notificationPayload, map[string]any{"items": []any{map[string]any{"key": "body", "text": 17}}}); err == nil {
		t.Fatal("corrupt notification result item was silently ignored")
	}
	if _, err = pool.Exec(ctx, `alter table notification_translations rename column body to arch026_broken_body`); err != nil {
		t.Fatal(err)
	}
	if err = worker.persistNotificationTranslation(ctx, 42, notificationPayload, notificationResult); err == nil {
		t.Fatal("notification translation upsert failure was silently ignored")
	}

	var runningTaskID int64
	if err = pool.QueryRow(ctx, `select id from ai_tasks where task_uid='arch026running'`).Scan(&runningTaskID); err != nil {
		t.Fatal(err)
	}
	if err = worker.failTask(ctx, runningTaskID, errors.New("ARCH026 forced task failure")); err != nil {
		t.Fatalf("persist failed task state: %v", err)
	}
	var failedStatus, failedError string
	if err = pool.QueryRow(ctx, `select status,error from ai_tasks where id=$1`, runningTaskID).Scan(&failedStatus, &failedError); err != nil || failedStatus != "failed" || !strings.Contains(failedError, "ARCH026") {
		t.Fatalf("failed state=%q/%q error=%v", failedStatus, failedError, err)
	}
	if _, err = pool.Exec(ctx, `
		insert into ai_tasks(task_uid,task_type,status,result,payload) values(
			'arch026failwrite','content_translation_completion','running','{}'::jsonb,'{}'::jsonb);
		create function pg_temp.arch026_reject_failed_state() returns trigger language plpgsql as $$
		begin
			if old.task_uid='arch026failwrite' then
				raise exception 'ARCH026 forced failed-state write error' using errcode='XX000';
			end if;
			return new;
		end $$;
		create trigger arch026_reject_failed_state before update on ai_tasks
			for each row execute function pg_temp.arch026_reject_failed_state();
	`); err != nil {
		t.Fatal(err)
	}
	var brokenTaskID int64
	if err = pool.QueryRow(ctx, `select id from ai_tasks where task_uid='arch026failwrite'`).Scan(&brokenTaskID); err != nil {
		t.Fatal(err)
	}
	if err = worker.failTask(ctx, brokenTaskID, errors.New("original failure")); err == nil || !strings.Contains(err.Error(), "forced failed-state write error") {
		t.Fatalf("failed-state persistence error=%v", err)
	}
}

func invokeCatalogTranslationResult(server *Server, ctx context.Context, taskID string) (int, string) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/content/translations/"+taskID, nil)
	request.SetPathValue("taskId", taskID)
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 42}))
	response := httptest.NewRecorder()
	server.catalogContentTranslationResult(response, request)
	return response.Code, response.Body.String()
}

func invokeNotificationTranslationResult(server *Server, ctx context.Context, taskID string) (int, string) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/translations/"+taskID, nil)
	request.SetPathValue("id", taskID)
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 42}))
	response := httptest.NewRecorder()
	server.notificationTranslationResult(response, request)
	return response.Code, response.Body.String()
}
