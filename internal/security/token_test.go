package security

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSessionTokenRoundTripAndRandomIdentity(t *testing.T) {
	const secret = "test-session-signing-secret"
	first, err := NewClaims("user00001", "tester", "tester@example.test", 7, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewClaims("user00001", "tester", "tester@example.test", 7, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if first.SessionID == second.SessionID || len(first.SessionID) != 43 || len(second.SessionID) != 43 {
		t.Fatalf("session identities are not independent 256-bit values: %q / %q", first.SessionID, second.SessionID)
	}
	token, err := SignToken(secret, first)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseToken(secret, token)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.PublicSubject != first.PublicSubject || parsed.SessionID != first.SessionID ||
		parsed.AuthVersion != first.AuthVersion || parsed.Username != first.Username || parsed.Email != first.Email ||
		parsed.IssuedAt != first.IssuedAt || parsed.ExpiresAt != first.ExpiresAt {
		t.Fatalf("parsed claims = %#v, want %#v", parsed, first)
	}
	if string(SessionFingerprint(first.SessionID)) == string(SessionFingerprint(second.SessionID)) {
		t.Fatal("distinct session IDs produced the same fingerprint")
	}
}

func TestSessionTokenRejectsInvalidHeadersSignaturesLifetimesAndClaims(t *testing.T) {
	const secret = "test-session-signing-secret"
	now := time.Now().Unix()
	valid := Claims{PublicSubject: "user00001", SessionID: "session-id", AuthVersion: 3, IssuedAt: now - 1, ExpiresAt: now + 3600}
	tests := []struct {
		name   string
		header map[string]string
		claims Claims
		secret string
		mutate func(string) string
	}{
		{name: "algorithm", header: map[string]string{"alg": "none", "typ": "JWT"}, claims: valid},
		{name: "type", header: map[string]string{"alg": "HS256", "typ": "JWS"}, claims: valid},
		{name: "expired", claims: withTokenTimes(valid, now-120, now-1)},
		{name: "future issued at", claims: withTokenTimes(valid, now+61, now+3600)},
		{name: "non-positive issued at", claims: withTokenTimes(valid, 0, now+3600)},
		{name: "reversed lifetime", claims: withTokenTimes(valid, now+10, now+10)},
		{name: "invalid public id", claims: withTokenPublicID(valid, "USER")},
		{name: "missing session", claims: withTokenSessionID(valid, "")},
		{name: "missing auth version", claims: withTokenAuthVersion(valid, 0)},
		{name: "wrong verification secret", claims: valid, secret: "other-secret"},
		{name: "tampered signature", claims: valid, mutate: func(token string) string { return token[:len(token)-1] + "x" }},
		{name: "malformed segment count", claims: valid, mutate: func(token string) string { return strings.TrimSuffix(token, token[strings.LastIndexByte(token, '.'):]) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			header := test.header
			if header == nil {
				header = map[string]string{"alg": "HS256", "typ": "JWT"}
			}
			token := makeTokenForTest(t, secret, header, test.claims)
			if test.mutate != nil {
				token = test.mutate(token)
			}
			verificationSecret := secret
			if test.secret != "" {
				verificationSecret = test.secret
			}
			if _, err := ParseToken(verificationSecret, token); err == nil {
				t.Fatalf("invalid token was accepted: %s", token)
			}
		})
	}

	if _, err := ParseToken(secret, "not-base64.payload.signature"); err == nil {
		t.Fatal("malformed base64 token was accepted")
	}
}

func TestBearerTokenRequiresCanonicalNonEmptyScheme(t *testing.T) {
	for _, test := range []struct {
		header string
		want   string
		ok     bool
	}{
		{header: "Bearer signed-token", want: "signed-token", ok: true},
		{header: "Bearer   signed-token  ", want: "signed-token", ok: true},
		{header: "Bearer ", ok: false},
		{header: "bearer signed-token", ok: false},
		{header: "signed-token", ok: false},
		{header: "", ok: false},
	} {
		got, err := BearerToken(test.header)
		if (err == nil) != test.ok || got != test.want {
			t.Fatalf("BearerToken(%q) = %q, %v; want %q, ok=%t", test.header, got, err, test.want, test.ok)
		}
	}
}

func makeTokenForTest(t *testing.T, secret string, header map[string]string, claims Claims) string {
	t.Helper()
	headerRaw, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	payloadRaw, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	unsigned := base64.RawURLEncoding.EncodeToString(headerRaw) + "." + base64.RawURLEncoding.EncodeToString(payloadRaw)
	return unsigned + "." + sign(secret, unsigned)
}

func withTokenTimes(claims Claims, issuedAt, expiresAt int64) Claims {
	claims.IssuedAt, claims.ExpiresAt = issuedAt, expiresAt
	return claims
}

func withTokenPublicID(claims Claims, value string) Claims {
	claims.PublicSubject = value
	return claims
}

func withTokenSessionID(claims Claims, value string) Claims {
	claims.SessionID = value
	return claims
}

func withTokenAuthVersion(claims Claims, value int64) Claims {
	claims.AuthVersion = value
	return claims
}
