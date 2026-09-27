package httpapi

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestResolveOSSObjectAccessLegacyESAModeUsesPresignedURL(t *testing.T) {
	server := &Server{}
	cfg := ossConfigPayload{
		Enabled:               true,
		Region:                "cn-hangzhou",
		Endpoint:              "https://oss-cn-hangzhou.aliyuncs.com",
		PublicEndpoint:        "https://oss.example.test/assets",
		Bucket:                "mcmods-test",
		AccessKeyID:           "test-access-key",
		AccessKeySecret:       "test-access-secret",
		DownloadURLMode:       "esa_private_origin",
		DownloadURLTTLMinutes: 12,
	}
	access, err := server.resolveOSSObjectAccessWithConfig(context.Background(), cfg, "project/icon.png", ossObjectAccessOptions{
		ContentDisposition: `attachment; filename="icon.png"`,
	})
	if err != nil {
		t.Fatalf("resolve legacy ESA object access: %v", err)
	}
	if access.Mode != ossDownloadModePresigned {
		t.Fatalf("unexpected mode %q", access.Mode)
	}
	if !strings.Contains(strings.ToLower(access.URL), "x-oss-signature") ||
		!strings.Contains(access.URL, "response-content-disposition=") ||
		strings.HasPrefix(access.URL, "https://oss.example.test/") {
		t.Fatalf("unexpected signed URL %q", access.URL)
	}
	if remaining := time.Until(access.ExpiresAt); remaining < 11*time.Minute || remaining > 13*time.Minute {
		t.Fatalf("unexpected expiry %s", remaining)
	}
}

func TestResolveStoredOSSObjectAccessLeavesExternalURLUnchanged(t *testing.T) {
	server := &Server{}
	cfg := ossConfigPayload{
		PublicEndpoint:  "https://oss.example.test",
		DownloadURLMode: "esa_private_origin",
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
