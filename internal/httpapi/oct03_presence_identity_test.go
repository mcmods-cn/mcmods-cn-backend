package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestOCT03PresenceIdentityBindsSecretNetworkAndNormalizedBoundedUserAgent(t *testing.T) {
	const secret = "oct03-owned-anonymous-identity-test-secret-at-least-32-bytes"
	sign := func(message string) string {
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(message))
		return hex.EncodeToString(mac.Sum(nil))
	}
	token, fingerprint, source := anonymousPresenceIdentity(secret, "203.0.113.9", "  OCT03   Browser  ", "arbitrary-client-id")
	expected := "p1." + sign("presence:v1\x00203.0.113.9\x00oct03 browser")
	digest := sha256.Sum256([]byte(expected))
	if token != expected || fingerprint != hex.EncodeToString(digest[:]) || source != sign("presence-source:v1\x00203.0.113.9") {
		t.Fatal("anonymous identity did not follow the server-signed, domain-separated contract")
	}
	otherUA, _, sameSource := anonymousPresenceIdentity(secret, "203.0.113.9", "other browser", token)
	if otherUA == token || sameSource != source {
		t.Fatal("user-agent changes must change visitor identity while keeping one network admission source")
	}
	otherNetwork, _, otherSource := anonymousPresenceIdentity(secret, "203.0.113.10", "oct03 browser", token)
	if otherNetwork == token || otherSource == source {
		t.Fatal("server identity and admission source were not bound to the network")
	}
	longUA := strings.Repeat("a", 256)
	bounded, _, boundedSource := anonymousPresenceIdentity(secret, "203.0.113.9", longUA, "")
	suffix, _, suffixSource := anonymousPresenceIdentity(secret, "203.0.113.9", longUA+strings.Repeat("b", 4096), "")
	if suffix != bounded || suffixSource != boundedSource {
		t.Fatal("long user agents exceeded the bounded fingerprint input")
	}
}
