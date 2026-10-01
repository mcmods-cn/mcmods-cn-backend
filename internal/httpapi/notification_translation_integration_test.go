package httpapi

import (
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
	"mcmods-cn-backend/internal/security"
)

func TestNotificationTranslationResultIsReadOnlyAndRejectsStaleSourceIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	server := &Server{db: pool}
	unique := fmt.Sprintf("translation_read_%d", time.Now().UnixNano())
	var userID, notificationID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash) values($1,$2,'test') returning id`, unique, unique+"@example.test").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `delete from ai_tasks where created_by=$1`, userID); err != nil {
			t.Error(err)
		}
		if _, err := pool.Exec(context.Background(), `delete from users where id=$1`, userID); err != nil {
			t.Error(err)
		}
	})
	if err = pool.QueryRow(ctx, `insert into notifications(recipient_id,kind,title,body,source_locale)
		values($1,'reply_mention','原始标题','原始正文','zh-CN') returning id`, userID).Scan(&notificationID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into notification_translations(notification_id,user_id,locale,title,body)
		values($1,$2,'en-US','Current approved title','Current approved body')`, notificationID, userID); err != nil {
		t.Fatal(err)
	}
	taskUID := "ai_" + randomHex(16)
	payload, err := json.Marshal(map[string]any{"notificationId": notificationID, "targetLocale": "en-US"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into ai_tasks(task_uid,task_type,status,created_by,payload,result)
		values($1,$2,'completed',$3,$4::jsonb,'{"items":[{"key":"title","text":"Stale task title"},{"key":"body","text":"Stale task body"}]}')`,
		taskUID, aiTaskNotificationTranslation, userID, payload); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/translations/"+taskUID, nil)
	request.SetPathValue("id", taskUID)
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: userID}))
	response := httptest.NewRecorder()
	server.notificationTranslationResult(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("result status=%d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Data struct {
			Status      string            `json:"status"`
			Translation map[string]string `json:"translation"`
		} `json:"data"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.Status != "completed" || result.Data.Translation["title"] != "Current approved title" {
		t.Fatalf("result did not read the current stored translation: %s", response.Body.String())
	}
	var storedTitle string
	if err = pool.QueryRow(ctx, `select title from notification_translations where notification_id=$1 and user_id=$2 and locale='en-US'`, notificationID, userID).Scan(&storedTitle); err != nil {
		t.Fatal(err)
	}
	if storedTitle != "Current approved title" {
		t.Fatalf("GET overwrote the approved translation: %q", storedTitle)
	}
	if _, err = pool.Exec(ctx, `update notifications set body='更新正文',updated_at=now() where id=$1`, notificationID); err != nil {
		t.Fatal(err)
	}
	result.Data.Translation = nil
	response = httptest.NewRecorder()
	server.notificationTranslationResult(response, request)
	if err = json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.Status != "failed" || result.Data.Translation != nil {
		t.Fatalf("stale notification source still reported success: %s", response.Body.String())
	}
	// Deleted resources cannot be recreated by polling a completed task.
	if _, err = pool.Exec(ctx, `delete from notifications where id=$1`, notificationID); err != nil {
		t.Fatal(err)
	}
	result.Data.Translation = nil
	response = httptest.NewRecorder()
	server.notificationTranslationResult(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("deleted source result status=%d body=%s", response.Code, response.Body.String())
	}
	if err = json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Data.Status != "failed" || result.Data.Translation != nil {
		t.Fatalf("deleted notification still reported a successful translation: %s, err=%v", response.Body.String(), err)
	}
}
