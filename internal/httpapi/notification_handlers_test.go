package httpapi

import (
	"net/http"
	"testing"
)

func TestFollowerNotificationBody(t *testing.T) {
	tests := []struct {
		names []string
		count int
		want  string
	}{
		{names: []string{"Alex"}, count: 1, want: "Alex 关注了你"},
		{names: []string{"Alex", "Steve"}, count: 2, want: "Alex、Steve 关注了你"},
		{names: []string{"Alex", "Steve", "Creeper"}, count: 5, want: "Alex、Steve 等 5 人关注了你"},
	}
	for _, test := range tests {
		if got := followerNotificationBody(test.names, test.count); got != test.want {
			t.Fatalf("followerNotificationBody(%v, %d) = %q, want %q", test.names, test.count, got, test.want)
		}
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
