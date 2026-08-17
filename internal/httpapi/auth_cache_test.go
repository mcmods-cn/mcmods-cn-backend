package httpapi

import (
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/security"
)

func TestSessionCacheKeyUsesFingerprintNotToken(t *testing.T) {
	claims, err := security.NewClaims("abc234567", "user", "user@example.test", 3, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	key := sessionCacheKey(claims.SessionID)
	if strings.Contains(key, claims.SessionID) || !strings.HasPrefix(key, "session:") || len(key) != len("session:")+64 {
		t.Fatalf("unsafe session cache key %q", key)
	}
}

func TestRBACCacheKeyIsVersionScoped(t *testing.T) {
	first := rbacCacheKey(3, 8, 11)
	if first == rbacCacheKey(4, 8, 11) || first == rbacCacheKey(3, 8, 12) {
		t.Fatal("RBAC key did not change with version")
	}
}

func TestAuthenticationRateDimensionsNormalizeAndHash(t *testing.T) {
	if got := authRateNetwork("192.168.1.44"); got != "192.168.1.0/24" {
		t.Fatalf("IPv4 network = %q", got)
	}
	if got := authRateNetwork("2001:db8:1:2::5"); got != "2001:db8:1:2::/64" {
		t.Fatalf("IPv6 network = %q", got)
	}
	digest := authRateDigest(" User@Example.Test ")
	if digest == "" || strings.Contains(digest, "example") || digest != authRateDigest("user@example.test") {
		t.Fatal("email dimension is not stable and private")
	}
	if authRateDigest("") != "" {
		t.Fatal("empty dimensions must be skipped")
	}
}

func TestPublicUserCardKeyIsPermissionNeutral(t *testing.T) {
	if got := publicUserCardCacheKey(42); got != "user-card:public:42" {
		t.Fatalf("unexpected user card key %q", got)
	}
}
