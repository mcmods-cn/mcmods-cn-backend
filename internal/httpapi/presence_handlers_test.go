package httpapi

import (
	"strings"
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

func TestAnonymousPresenceIdentityIsServerSignedAndClientInputCannotMultiplyIt(t *testing.T) {
	secret := "test-presence-secret-that-is-long-enough"
	firstToken, firstFingerprint, firstSource := anonymousPresenceIdentity(secret, "203.0.113.9", "Example Browser", "attacker-one")
	secondToken, secondFingerprint, secondSource := anonymousPresenceIdentity(secret, "203.0.113.9", "Example Browser", "attacker-two")
	if firstToken != secondToken || firstFingerprint != secondFingerprint || firstSource != secondSource {
		t.Fatal("arbitrary visitorId changed the server-derived presence identity")
	}
	if !strings.HasPrefix(firstToken, "p1.") || strings.Contains(firstToken, "203.0.113.9") || len(firstFingerprint) != 64 {
		t.Fatalf("presence token/fingerprint is not an opaque signed identity: %q/%q", firstToken, firstFingerprint)
	}
	otherToken, _, _ := anonymousPresenceIdentity("different-secret-that-is-also-long-enough", "203.0.113.9", "Example Browser", firstToken)
	if otherToken == firstToken {
		t.Fatal("presence token was not bound to the server secret")
	}
}

func TestPresenceSnapshotIsNotRequestLevel(t *testing.T) {
	t.Parallel()
	if defaultPresenceSnapshotInterval < 5*time.Minute {
		t.Fatal("PostgreSQL presence snapshots must remain low frequency")
	}
}
