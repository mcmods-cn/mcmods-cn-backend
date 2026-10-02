package httpapi

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSEC029LegacyESAAccessBecomesObjectBoundExpiringSignature(t *testing.T) {
	server := &Server{}
	cfg := ossConfigPayload{
		Enabled:               true,
		Region:                "cn-hangzhou",
		Endpoint:              "https://oss-cn-hangzhou.aliyuncs.com",
		PublicEndpoint:        "https://esa.example.test/assets",
		Bucket:                "mcmods-test",
		AccessKeyID:           "test-access-key",
		AccessKeySecret:       "test-access-secret",
		DownloadURLMode:       "esa_private_origin",
		DownloadURLTTLMinutes: 10_080,
	}
	started := time.Now()
	first, err := server.resolveOSSObjectAccessWithConfig(context.Background(), cfg, "private/user-a/file.txt", ossObjectAccessOptions{
		Expires:            24 * time.Hour,
		ContentDisposition: `attachment; filename="file.txt"`,
	})
	if err != nil {
		t.Fatalf("resolve legacy ESA access: %v", err)
	}
	if first.Mode != ossDownloadModePresigned {
		t.Fatalf("legacy ESA mode resolved as %q, want %q", first.Mode, ossDownloadModePresigned)
	}
	if first.ExpiresAt.After(started.Add(time.Hour + 2*time.Second)) {
		t.Fatalf("access expiry %s exceeds one-hour hard limit", first.ExpiresAt.Sub(started))
	}
	firstURL, err := url.Parse(first.URL)
	if err != nil {
		t.Fatal(err)
	}
	firstQuery := lowerURLQuery(firstURL.Query())
	firstSignature := firstQuery.Get("x-oss-signature")
	if firstSignature == "" || firstQuery.Get("x-oss-expires") == "" {
		t.Fatalf("legacy mode returned a URL without executable OSS expiry/signature: %q", first.URL)
	}
	if strings.EqualFold(firstURL.Host, "esa.example.test") {
		t.Fatalf("private access still uses the replayable public ESA endpoint: %q", first.URL)
	}
	if firstQuery.Get("response-content-disposition") == "" {
		t.Fatalf("download purpose is not carried by the signed URL: %q", first.URL)
	}

	second, err := server.resolveOSSObjectAccessWithConfig(context.Background(), cfg, "private/user-b/file.txt", ossObjectAccessOptions{
		Expires:            24 * time.Hour,
		ContentDisposition: `attachment; filename="file.txt"`,
	})
	if err != nil {
		t.Fatalf("resolve second object access: %v", err)
	}
	secondURL, err := url.Parse(second.URL)
	if err != nil {
		t.Fatal(err)
	}
	if secondSignature := lowerURLQuery(secondURL.Query()).Get("x-oss-signature"); secondSignature == "" || secondSignature == firstSignature {
		t.Fatalf("signatures are not bound to their object paths: first=%q second=%q", firstSignature, secondSignature)
	}
	otherPurpose, err := server.resolveOSSObjectAccessWithConfig(context.Background(), cfg, "private/user-a/file.txt", ossObjectAccessOptions{
		Expires:            24 * time.Hour,
		ContentDisposition: `inline; filename="file.txt"`,
	})
	if err != nil {
		t.Fatalf("resolve alternate download purpose: %v", err)
	}
	otherPurposeURL, err := url.Parse(otherPurpose.URL)
	if err != nil {
		t.Fatal(err)
	}
	if purposeSignature := lowerURLQuery(otherPurposeURL.Query()).Get("x-oss-signature"); purposeSignature == "" || purposeSignature == firstSignature {
		t.Fatalf("signatures are not bound to response disposition: first=%q alternate=%q", firstSignature, purposeSignature)
	}
}

func TestSEC029DownloadConfigurationHasOneSafeModeAndShortTTL(t *testing.T) {
	for _, legacy := range []string{"", "esa_private_origin", "esa-private-origin", "oss_presigned", "presigned-url"} {
		if got := normalizeOSSDownloadMode(legacy); got != ossDownloadModePresigned {
			t.Errorf("normalizeOSSDownloadMode(%q)=%q, want %q", legacy, got, ossDownloadModePresigned)
		}
	}
	if got := normalizeOSSDownloadMode("unknown-mode"); got != ossDownloadModePresigned {
		t.Fatalf("unknown download mode normalized to %q, want safe default", got)
	}
	cfg := normalizeOSSConfig(ossConfigPayload{DownloadURLMode: "esa_private_origin", DownloadURLTTLMinutes: 10_080})
	if cfg.DownloadURLMode != ossDownloadModePresigned || cfg.DownloadURLTTLMinutes != 60 {
		t.Fatalf("legacy config normalized to mode=%q ttl=%d, want %q/60", cfg.DownloadURLMode, cfg.DownloadURLTTLMinutes, ossDownloadModePresigned)
	}
	defaults := defaultOSSConfig()
	if defaults.DownloadURLMode != ossDownloadModePresigned || defaults.DownloadURLTTLMinutes <= 0 || defaults.DownloadURLTTLMinutes > 60 {
		t.Fatalf("unsafe OSS download defaults: mode=%q ttl=%d", defaults.DownloadURLMode, defaults.DownloadURLTTLMinutes)
	}
}

func lowerURLQuery(values url.Values) url.Values {
	lowered := make(url.Values, len(values))
	for key, items := range values {
		lowered[strings.ToLower(key)] = append([]string(nil), items...)
	}
	return lowered
}
