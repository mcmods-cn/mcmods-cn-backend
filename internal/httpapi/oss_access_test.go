package httpapi

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestResolveOSSObjectAccessESAPrivateOrigin(t *testing.T) {
	server := &Server{}
	cfg := ossConfigPayload{
		PublicEndpoint:        "https://oss.example.test/assets",
		DownloadURLMode:       ossDownloadModeESAPrivateOrigin,
		DownloadURLTTLMinutes: 12,
	}
	access, err := server.resolveOSSObjectAccessWithConfig(context.Background(), cfg, "project/icon.png", ossObjectAccessOptions{
		ContentDisposition: `attachment; filename="icon.png"`,
	})
	if err != nil {
		t.Fatalf("resolve ESA object access: %v", err)
	}
	if access.Mode != ossDownloadModeESAPrivateOrigin {
		t.Fatalf("unexpected mode %q", access.Mode)
	}
	if !strings.HasPrefix(access.URL, "https://oss.example.test/assets/project/icon.png?") ||
		!strings.Contains(access.URL, "response-content-disposition=") {
		t.Fatalf("unexpected ESA URL %q", access.URL)
	}
	if remaining := time.Until(access.ExpiresAt); remaining < 11*time.Minute || remaining > 13*time.Minute {
		t.Fatalf("unexpected expiry %s", remaining)
	}
}

func TestResolveStoredOSSObjectAccessLeavesExternalURLUnchanged(t *testing.T) {
	server := &Server{}
	cfg := ossConfigPayload{
		PublicEndpoint:  "https://oss.example.test",
		DownloadURLMode: ossDownloadModeESAPrivateOrigin,
	}
	const external = "https://cdn.example.test/avatar.png"
	resolved, err := server.resolveStoredOSSObjectAccessURLWithConfig(context.Background(), cfg, external)
	if err != nil {
		t.Fatalf("resolve external URL: %v", err)
	}
	if resolved != external {
		t.Fatalf("external URL changed from %q to %q", external, resolved)
	}
}

func TestResolveOSSObjectAccessPresigned(t *testing.T) {
	server := &Server{}
	cfg := ossConfigPayload{
		Enabled:               true,
		Region:                "cn-hangzhou",
		Endpoint:              "https://oss-cn-hangzhou.aliyuncs.com",
		Bucket:                "mcmods-test",
		AccessKeyID:           "test-access-key",
		AccessKeySecret:       "test-access-secret",
		DownloadURLMode:       ossDownloadModePresigned,
		DownloadURLTTLMinutes: 5,
	}
	access, err := server.resolveOSSObjectAccessWithConfig(context.Background(), cfg, "project/icon.png", ossObjectAccessOptions{})
	if err != nil {
		t.Fatalf("resolve presigned object access: %v", err)
	}
	if access.Mode != ossDownloadModePresigned {
		t.Fatalf("unexpected mode %q", access.Mode)
	}
	if !strings.Contains(strings.ToLower(access.URL), "x-oss-signature") {
		t.Fatalf("URL is not OSS-signed: %q", access.URL)
	}
}
