package httpapi

import (
	"testing"
	"time"
)

func TestMapPublicOnlineStatusPreservesPrivacy(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	recent := now.Add(-time.Minute)
	old := now.Add(-publicPresenceWindow - time.Second)
	for _, test := range []struct {
		name string
		show bool
		last *time.Time
		want publicOnlineStatus
	}{
		{name: "hidden recent", show: false, last: &recent, want: publicOnlineStatusHidden},
		{name: "hidden old", show: false, last: &old, want: publicOnlineStatusHidden},
		{name: "online", show: true, last: &recent, want: publicOnlineStatusOnline},
		{name: "offline old", show: true, last: &old, want: publicOnlineStatusOffline},
		{name: "offline unknown", show: true, want: publicOnlineStatusOffline},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := mapPublicOnlineStatus(test.show, test.last, now); got != test.want {
				t.Fatalf("mapPublicOnlineStatus() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPresenceConfigurationIsCentralizedAndThrottled(t *testing.T) {
	t.Parallel()
	if publicPresenceWindow <= presenceWriteThrottle {
		t.Fatal("presence visibility window must exceed the database write throttle")
	}
	if presenceWriteThrottle < time.Minute {
		t.Fatal("heartbeat persistence must be throttled to avoid request-level writes")
	}
}
