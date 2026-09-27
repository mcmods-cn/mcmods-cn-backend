package httpapi

import (
	"net/http"
	"testing"
)

func TestFollowerNotificationUsesRecipientLocaleTemplate(t *testing.T) {
	config := defaultNotificationTemplateConfig()
	values := map[string]string{"actors": "Alex, Steve", "count": "2"}
	english, err := renderNotificationTemplateForLocale(config, "en-US", "new_follower", values)
	if err != nil {
		t.Fatal(err)
	}
	chinese, err := renderNotificationTemplateForLocale(config, "zh-CN", "new_follower", values)
	if err != nil {
		t.Fatal(err)
	}
	if english.Locale != "en-US" || chinese.Locale != "zh-CN" || english.Title == chinese.Title || english.Body == chinese.Body {
		t.Fatalf("localized follower templates = %#v / %#v", english, chinese)
	}
}

func TestOrderedUserIDs(t *testing.T) {
	low, high := orderedUserIDs(9, 2)
	if low != 2 || high != 9 {
		t.Fatalf("ordered ids = %d, %d", low, high)
	}
}

func TestRemainingTokens(t *testing.T) {
	if got := remainingTokens(1000, 250); got != 750 {
		t.Fatalf("remaining tokens = %d", got)
	}
	if got := remainingTokens(1000, 1200); got != 0 {
		t.Fatalf("over-limit remaining tokens = %d", got)
	}
}

func TestMarkAllNotificationsReadRouteIsRegistered(t *testing.T) {
	server := &Server{mux: http.NewServeMux()}
	server.routes()
	request, err := http.NewRequest(http.MethodPost, "/api/v1/notifications/read-all", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, pattern := server.mux.Handler(request)
	if pattern == "" {
		t.Fatal("mark-all-notifications-read route is not registered")
	}
}
