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

func TestBUG043NotificationTranslationAcceptsOnlyCanonicalSiteLocalesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the notification translation locale boundary")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, userID := newPostgresUserFixture(t, ctx, "bug043-notification-translation")
	var notificationID int64
	var publicID string
	if err := pool.QueryRow(ctx, `insert into notifications(recipient_id,kind,title,body,source_locale)
		values($1,'review','待审核','请查看','zh-CN') returning id,public_id`, userID).Scan(&notificationID, &publicID); err != nil {
		t.Fatalf("create notification: %v", err)
	}
	if _, err := pool.Exec(ctx, `insert into notification_translations(notification_id,user_id,locale,title,body) values
		($1,$2,'pt-BR','Inútil','Sem consumidor'),($1,$2,'en-US','Review','Please review')`, notificationID, userID); err != nil {
		t.Fatalf("create translation cache controls: %v", err)
	}

	server := &Server{db: pool}
	invoke := func(targetLocale string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/"+publicID+"/translate",
			strings.NewReader(`{"targetLocale":"`+targetLocale+`"}`)).WithContext(
			context.WithValue(ctx, claimsContextKey, security.Claims{Subject: userID}),
		)
		request.SetPathValue("id", publicID)
		response := httptest.NewRecorder()
		server.translateNotification(response, request)
		return response
	}

	t.Run("registered alias uses canonical cache key", func(t *testing.T) {
		response := invoke("EN_us")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"title":"Review"`) {
			t.Fatalf("canonical alias status=%d body=%s", response.Code, response.Body.String())
		}
	})
	t.Run("unregistered locale is rejected before cache lookup", func(t *testing.T) {
		response := invoke("pt-BR")
		if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "Inútil") {
			t.Fatalf("unregistered locale status=%d body=%s", response.Code, response.Body.String())
		}
	})
}

func TestBUG043NotificationTranslationWorkerRejectsUnregisteredLocale(t *testing.T) {
	result := map[string]any{"items": []any{
		map[string]any{"key": "title", "text": "Título"},
		map[string]any{"key": "body", "text": "Corpo"},
	}}
	for _, locale := range supportedContentLocaleList() {
		payload, _, err := decodeNotificationTranslation(
			[]byte(`{"notificationId":7,"targetLocale":"`+locale+`"}`), result,
		)
		if err != nil || payload.TargetLocale != locale {
			t.Errorf("registered locale %q was rejected: payload=%#v err=%v", locale, payload, err)
		}
	}
	if _, _, err := decodeNotificationTranslation([]byte(`{"notificationId":7,"targetLocale":"pt-BR"}`), result); err == nil {
		t.Fatal("notification translation worker accepted an unregistered locale")
	}
	payload, _, err := decodeNotificationTranslation([]byte(`{"notificationId":7,"targetLocale":"EN_us"}`), result)
	if err != nil || payload.TargetLocale != "en-US" {
		t.Fatalf("registered alias was not canonicalized: payload=%#v err=%v", payload, err)
	}
}
