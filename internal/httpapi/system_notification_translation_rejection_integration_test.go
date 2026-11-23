package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/security"
)

func TestSystemNotificationTranslationIsRejectedIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the system-notification translation boundary")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, userID := newPostgresUserFixture(t, ctx, "system-notification-translation")
	var publicID string
	if err := pool.QueryRow(ctx, `insert into notifications(recipient_id,kind,title,body,source_locale)
		values($1,'system','系统标题','系统正文','zh-CN') returning public_id`, userID).Scan(&publicID); err != nil {
		t.Fatalf("create system notification: %v", err)
	}

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
