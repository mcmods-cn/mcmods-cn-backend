package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestSystemNotificationTranslationIsRejectedIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the system-notification translation boundary")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var userID int64
	if err = pool.QueryRow(ctx, `select id from users where status='active' order by id limit 1`).Scan(&userID); err != nil {
		t.Fatalf("load integration user: %v", err)
	}
	var notificationID int64
	var publicID string
	if err = pool.QueryRow(ctx, `insert into notifications(recipient_id,kind,title,body,source_locale)
		values($1,'system','系统标题','系统正文','zh-CN') returning id,public_id`, userID).Scan(&notificationID, &publicID); err != nil {
		t.Fatalf("create system notification: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `delete from notifications where id=$1`, notificationID)
	}()

	server := &Server{db: pool}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/"+publicID+"/translate", strings.NewReader(`{"targetLocale":"en-US"}`)).WithContext(
		context.WithValue(ctx, claimsContextKey, security.Claims{Subject: userID}),
	)
	request.SetPathValue("id", publicID)
	response := httptest.NewRecorder()
	server.translateNotification(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "SYSTEM_NOTIFICATION_TRANSLATION_DISABLED") {
		t.Fatalf("translate system notification status=%d body=%s", response.Code, response.Body.String())
	}
}
