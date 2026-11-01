package httpapi

import "testing"

func TestAuditOSS001BoundAvatarReferenceIdentityDoesNotStripUnregisteredBusinessOrTrustOrigins(t *testing.T) {
	cfg := ossConfigPayload{Endpoint: "https://oss.example.test/base", PublicEndpoint: "https://images.example.test/base", Bucket: "owned-bucket"}
	const key = "mcmods/users/avatar.png"
	cases := []struct {
		name, reference string
		matches         bool
	}{
		{"canonical", "https://images.example.test/base/" + key, true},
		{"registered_v4", "https://images.example.test/base/" + key + "?x-oss-date=20261101T000000Z&x-oss-expires=1&x-oss-signature=expired", true},
		{"actual_bucket_path", "https://oss.example.test/base/owned-bucket/" + key + "?Signature=expired&Expires=1&OSSAccessKeyId=old", true},
		{"recognized_retired_esa_identity_only", "https://images.example.test/base/" + key + "?auth_key=old", true},
		{"external_signature", "https://foreign.example.test/base/" + key + "?Signature=old", false},
		{"userinfo", "https://someone@images.example.test/base/" + key + "?Signature=old", false},
		{"wrong_scheme", "http://images.example.test/base/" + key + "?Signature=old", false},
		{"wrong_port", "https://images.example.test:8443/base/" + key + "?Signature=old", false},
		{"wrong_key", "https://images.example.test/base/mcmods/users/other.png?Signature=old", false},
		{"wrong_bucket", "https://oss.example.test/base/other-bucket/" + key + "?Signature=old", false},
		{"business_transformation", "https://images.example.test/base/" + key + "?Signature=old&size=256", false},
		{"stable_business_query", "https://images.example.test/base/" + key + "?size=256", false},
		{"ambiguous_case_signature", "https://images.example.test/base/" + key + "?Signature=one&signature=two", false},
		{"invalid_query", "https://images.example.test/base/" + key + "?Signature=%", false},
		{"dot_segment", "https://images.example.test/base/mcmods/users/../users/avatar.png?Signature=old", false},
		{"fragment", "https://images.example.test/base/" + key + "?Signature=old#different", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := creatorAvatarReferenceMatchesFile(cfg, tc.reference, key); got != tc.matches {
				t.Fatalf("current asset identity matches=%v want=%v", got, tc.matches)
			}
		})
	}
	for _, value := range []string{"https://images.example/avatar.png?x-oss-signature=%", "https://images.example/avatar.png?SIGNATURE=old", "https://images.example/avatar.png?auth_key=old"} {
		if !temporaryCreatorAvatarReference(value) {
			t.Error("a known signing key must be kept out of durable references even if its value is invalid")
		}
	}
}
