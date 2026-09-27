package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

func TestTEST021NotificationHandlersEnforceVisibilityReadStateAndCacheRepairIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify the notification Handler behavior matrix")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if dropErr := database.DropEphemeralSchema(cleanupCtx, pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	cache := querycache.New(config.RedisConfig{UnreadTTL: time.Minute})
	defer cache.Close()
	server := &Server{db: pool, cache: cache}

	suffix := randomCatalogPublicID()
	var userA, userB int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status) values($1,$2,'test-only','active') returning id`,
		"test021_a_"+suffix, "test021_a_"+suffix+"@example.invalid").Scan(&userA); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status) values($1,$2,'test-only','active') returning id`,
		"test021_b_"+suffix, "test021_b_"+suffix+"@example.invalid").Scan(&userB); err != nil {
		t.Fatal(err)
	}

	base := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Second)
	insertNotification := func(recipient any, kind, title string, updatedAt time.Time) (int64, string) {
		t.Helper()
		var id int64
		var publicID string
		if queryErr := pool.QueryRow(ctx, `insert into notifications(recipient_id,kind,title,body,source_locale,created_at,updated_at)
			values($1,$2,$3,$3,'zh-CN',$4,$4) returning id,public_id`, recipient, kind, title, updatedAt).Scan(&id, &publicID); queryErr != nil {
			t.Fatal(queryErr)
		}
		return id, publicID
	}
	_, oldDirectID := insertNotification(userA, "review", "a-old", base.Add(time.Second))
	broadcastInternalID, broadcastID := insertNotification(nil, "system", "broadcast", base.Add(2*time.Second))
	_, newDirectID := insertNotification(userA, "review", "a-new", base.Add(3*time.Second))
	foreignInternalID, foreignID := insertNotification(userB, "review", "b-private", base.Add(4*time.Second))

	first := invokeTEST021NotificationList(t, ctx, server, userA, "/api/v1/notifications?limit=2")
	if len(first.Items) != 2 || !first.HasMore || first.NextCursor == "" ||
		first.Items[0].ID != newDirectID || first.Items[1].ID != broadcastID {
		t.Fatalf("user A first page=%+v", first)
	}
	if first.Items[1].TranslationAllowed {
		t.Fatal("system broadcast unexpectedly allowed AI translation")
	}
	second := invokeTEST021NotificationList(t, ctx, server, userA,
		"/api/v1/notifications?limit=2&cursor="+first.NextCursor)
	if len(second.Items) != 1 || second.HasMore || second.NextCursor != "" || second.Items[0].ID != oldDirectID {
		t.Fatalf("user A second page=%+v", second)
	}
	foreignPage := invokeTEST021NotificationList(t, ctx, server, userB, "/api/v1/notifications?limit=10")
	if len(foreignPage.Items) != 2 || foreignPage.Items[0].ID != foreignID || foreignPage.Items[1].ID != broadcastID {
		t.Fatalf("user B page leaked or omitted notifications: %+v", foreignPage)
	}

	if summary := invokeTEST021UnreadSummary(t, ctx, server, userA); summary.Notifications != 3 || summary.Total != 3 {
		t.Fatalf("initial user A unread=%+v", summary)
	}
	markOne := invokeTEST021NotificationMutation(ctx, server, userA, newDirectID, false)
	if markOne.Code != http.StatusOK || !strings.Contains(markOne.Body.String(), `"read":true`) {
		t.Fatalf("mark one status=%d body=%s", markOne.Code, markOne.Body.String())
	}
	if summary := invokeTEST021UnreadSummary(t, ctx, server, userA); summary.Notifications != 2 || summary.Total != 2 {
		t.Fatalf("post mark-one user A unread=%+v", summary)
	}
	foreignMark := invokeTEST021NotificationMutation(ctx, server, userA, foreignID, false)
	if foreignMark.Code != http.StatusOK {
		t.Fatalf("foreign mark should be non-enumerating and idempotent: %d %s", foreignMark.Code, foreignMark.Body.String())
	}
	var foreignReceipts int
	if err = pool.QueryRow(ctx, `select count(*) from notification_receipts where notification_id=$1 and user_id=$2`, foreignInternalID, userA).Scan(&foreignReceipts); err != nil || foreignReceipts != 0 {
		t.Fatalf("foreign notification receipt count=%d err=%v", foreignReceipts, err)
	}

	markAll := invokeTEST021NotificationMutation(ctx, server, userA, "", true)
	if markAll.Code != http.StatusOK || !strings.Contains(markAll.Body.String(), `"readBefore"`) {
		t.Fatalf("mark all status=%d body=%s", markAll.Code, markAll.Body.String())
	}
	if summary := invokeTEST021UnreadSummary(t, ctx, server, userA); summary.Notifications != 0 || summary.Total != 0 {
		t.Fatalf("post mark-all user A unread=%+v", summary)
	}
	if summary := invokeTEST021UnreadSummary(t, ctx, server, userB); summary.Notifications != 2 || summary.Total != 2 {
		t.Fatalf("user B read state was changed by user A: %+v", summary)
	}

	if _, err = pool.Exec(ctx, `update notifications set updated_at=clock_timestamp()+interval '1 second' where id=$1`, broadcastInternalID); err != nil {
		t.Fatal(err)
	}
	if summary := invokeTEST021UnreadSummary(t, ctx, server, userA); summary.Notifications != 0 {
		t.Fatalf("fixture did not establish cached drift: %+v", summary)
	}
	processed, drifts, err := reconcileUnreadUsers(ctx, pool, cache, []int64{userA})
	if err != nil || processed != 1 || drifts != 1 {
		t.Fatalf("reconcile processed=%d drifts=%d err=%v", processed, drifts, err)
	}
	if summary := invokeTEST021UnreadSummary(t, ctx, server, userA); summary.Notifications != 1 || summary.Total != 1 {
		t.Fatalf("reconciled user A unread=%+v", summary)
	}

	systemTranslate := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/"+broadcastID+"/translate",
		strings.NewReader(`{"targetLocale":"en-US"}`))
	systemTranslate.SetPathValue("id", broadcastID)
	systemTranslate = systemTranslate.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: userA}))
	systemResponse := httptest.NewRecorder()
	server.translateNotification(systemResponse, systemTranslate)
	if systemResponse.Code != http.StatusForbidden || !strings.Contains(systemResponse.Body.String(), "SYSTEM_NOTIFICATION_TRANSLATION_DISABLED") {
		t.Fatalf("system translation status=%d body=%s", systemResponse.Code, systemResponse.Body.String())
	}

	var oldDirectInternalID int64
	if err = pool.QueryRow(ctx, `select id from notifications where public_id=$1`, oldDirectID).Scan(&oldDirectInternalID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into notification_translations(notification_id,user_id,locale,title,body)
		values($1,$2,'en-US','Cached review','Cached body')`, oldDirectInternalID, userA); err != nil {
		t.Fatal(err)
	}
	canonicalLocale := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/"+oldDirectID+"/translate",
		strings.NewReader(`{"targetLocale":"EN_us"}`))
	canonicalLocale.SetPathValue("id", oldDirectID)
	canonicalLocale = canonicalLocale.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: userA}))
	canonicalResponse := httptest.NewRecorder()
	server.translateNotification(canonicalResponse, canonicalLocale)
	if canonicalResponse.Code != http.StatusOK || !strings.Contains(canonicalResponse.Body.String(), `"cached":true`) ||
		!strings.Contains(canonicalResponse.Body.String(), "Cached review") {
		t.Fatalf("canonical locale status=%d body=%s", canonicalResponse.Code, canonicalResponse.Body.String())
	}

	invalidLocale := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/"+oldDirectID+"/translate",
		strings.NewReader(`{"targetLocale":"pt-BR"}`))
	invalidLocale.SetPathValue("id", oldDirectID)
	invalidLocale = invalidLocale.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: userA}))
	invalidResponse := httptest.NewRecorder()
	server.translateNotification(invalidResponse, invalidLocale)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid locale status=%d body=%s", invalidResponse.Code, invalidResponse.Body.String())
	}
}

type test021NotificationPage struct {
	Items      []notificationItem `json:"items"`
	HasMore    bool               `json:"hasMore"`
	NextCursor string             `json:"nextCursor"`
}

func invokeTEST021NotificationList(t *testing.T, ctx context.Context, server *Server, userID int64, path string) test021NotificationPage {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: userID}))
	response := httptest.NewRecorder()
	server.notifications(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("notification list status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data test021NotificationPage `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

type test021UnreadSummary struct {
	Notifications int64 `json:"notifications"`
	Messages      int64 `json:"messages"`
	Total         int64 `json:"total"`
}

func invokeTEST021UnreadSummary(t *testing.T, ctx context.Context, server *Server, userID int64) test021UnreadSummary {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/me/unread-summary", nil)
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: userID}))
	response := httptest.NewRecorder()
	server.unreadSummary(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unread summary status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data test021UnreadSummary `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

func invokeTEST021NotificationMutation(ctx context.Context, server *Server, userID int64, publicID string, all bool) *httptest.ResponseRecorder {
	path := "/api/v1/notifications/" + publicID + "/read"
	if all {
		path = "/api/v1/notifications/read-all"
	}
	request := httptest.NewRequest(http.MethodPost, path, nil)
	if !all {
		request.SetPathValue("id", publicID)
	}
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: userID}))
	response := httptest.NewRecorder()
	if all {
		server.markAllNotificationsRead(response, request)
	} else {
		server.markNotificationRead(response, request)
	}
	return response
}
