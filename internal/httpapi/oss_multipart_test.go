package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
)

func TestOSSMultipartPersistenceAndAdminRoutesStayWired(t *testing.T) {
	t.Parallel()
	assertions := map[string][]string{
		"oss_handlers.go": {
			"registerOSSMultipartSession",
			"beginOSSMultipartSettlement",
			"finishOSSMultipartSettlement",
		},
		"server.go": {
			`GET /api/v1/admin/infrastructure/oss-multipart-sessions", s.requirePermission("admin.config.read"`,
			`POST /api/v1/admin/infrastructure/oss-multipart-sessions/{id}/replay", s.requirePermission("admin.config.write"`,
		},
		"../app/runtime.go": {
			"NewOSSMultipartCleanupWorker(cfg, db).Start(ctx)",
			`Code: "oss_multipart_lifecycle_unavailable"`,
		},
	}
	for file, required := range assertions {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range required {
			if !strings.Contains(string(raw), value) {
				t.Fatalf("%s is missing %q", file, value)
			}
		}
	}
	runtimeSource, err := os.ReadFile("../app/runtime.go")
	if err != nil {
		t.Fatal(err)
	}
	validateAt := strings.Index(string(runtimeSource), "NewOSSMultipartCleanupWorker(cfg, db).Start(ctx)")
	firstBackgroundWorkerAt := strings.Index(string(runtimeSource), "StartUnreadReconciliation(ctx")
	if validateAt < 0 || firstBackgroundWorkerAt < 0 || validateAt > firstBackgroundWorkerAt {
		t.Fatal("OSS lifecycle validation must run before background workers are started")
	}
}

func TestOSSMultipartSizing(t *testing.T) {
	if shouldUseOSSMultipart(ossMultipartThreshold - 1) {
		t.Fatal("small upload unexpectedly selected multipart mode")
	}
	if !shouldUseOSSMultipart(ossMultipartThreshold) {
		t.Fatal("threshold upload did not select multipart mode")
	}
	if got := ossMultipartPartCount(53<<20, ossMultipartPartSize); got != 7 {
		t.Fatalf("53 MiB upload uses %d parts, want 7", got)
	}
}

func TestValidOSSMultipartUploadID(t *testing.T) {
	for _, value := range []string{"upload-abc_123456", "7E91C4C8A2F24C19A8E102"} {
		if !validOSSMultipartUploadID(value) {
			t.Fatalf("expected upload ID %q to be valid", value)
		}
	}
	for _, value := range []string{"", "short", "../escape", "contains space"} {
		if validOSSMultipartUploadID(value) {
			t.Fatalf("expected upload ID %q to be rejected", value)
		}
	}
}

func TestCompletedOSSMultipartObjectMustMatchSession(t *testing.T) {
	matching := &aliyunoss.HeadObjectResult{ContentLength: 20 << 20, Metadata: map[string]string{"sha256": strings.Repeat("a", 64)}}
	if !completedOSSMultipartObjectMatches(matching, 20<<20, strings.Repeat("a", 64)) {
		t.Fatal("matching completed object was rejected")
	}
	wrongSize := *matching
	wrongSize.ContentLength++
	if completedOSSMultipartObjectMatches(&wrongSize, 20<<20, strings.Repeat("a", 64)) {
		t.Fatal("size-mismatched completed object was accepted")
	}
	wrongHash := *matching
	wrongHash.Metadata = map[string]string{"sha256": strings.Repeat("b", 64)}
	if completedOSSMultipartObjectMatches(&wrongHash, 20<<20, strings.Repeat("a", 64)) {
		t.Fatal("hash-mismatched completed object was accepted")
	}
}

func TestVerifyCompletedOSSMultipartObjectUsesProviderMetadata(t *testing.T) {
	sha256 := strings.Repeat("c", 64)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead || r.URL.Path != "/bucket/object.zip" {
			t.Errorf("unexpected provider request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", strconv.FormatInt(20<<20, 10))
		w.Header().Set("x-oss-meta-sha256", sha256)
		w.WriteHeader(http.StatusOK)
	}))
	defer provider.Close()
	cfg := ossConfigPayload{AccessKeyID: "id", AccessKeySecret: "secret", Bucket: "bucket", Endpoint: provider.URL, Region: "region", UseCName: true}
	head, err := verifyCompletedOSSMultipartObject(context.Background(), newOSSClient(cfg, cfg.Endpoint, true), cfg, "object.zip", 20<<20, sha256)
	if err != nil || head.ContentLength != 20<<20 {
		t.Fatalf("provider verification = %#v/%v", head, err)
	}
}

func TestOSSMultipartLifecycleRequiresEnabledUnfilteredSevenDayAbort(t *testing.T) {
	enabled, disabled := "Enabled", "Disabled"
	empty, matching, other := "", "mcmods/", "other/"
	seven, eight := int32(7), int32(8)
	valid := aliyunoss.LifecycleRule{Status: &enabled, Prefix: &empty,
		AbortMultipartUpload: &aliyunoss.LifecycleRuleAbortMultipartUpload{Days: &seven}}
	if !hasBoundedOSSMultipartLifecycle([]aliyunoss.LifecycleRule{valid}, "mcmods/oss") {
		t.Fatal("valid global seven-day abort rule was rejected")
	}
	valid.Prefix = &matching
	if !hasBoundedOSSMultipartLifecycle([]aliyunoss.LifecycleRule{valid}, "mcmods/oss") {
		t.Fatal("matching prefix rule was rejected")
	}
	for name, rule := range map[string]aliyunoss.LifecycleRule{
		"disabled": {Status: &disabled, Prefix: &empty, AbortMultipartUpload: &aliyunoss.LifecycleRuleAbortMultipartUpload{Days: &seven}},
		"too long": {Status: &enabled, Prefix: &empty, AbortMultipartUpload: &aliyunoss.LifecycleRuleAbortMultipartUpload{Days: &eight}},
		"mismatch": {Status: &enabled, Prefix: &other, AbortMultipartUpload: &aliyunoss.LifecycleRuleAbortMultipartUpload{Days: &seven}},
		"filtered": {Status: &enabled, Prefix: &empty, Filter: &aliyunoss.LifecycleRuleFilter{}, AbortMultipartUpload: &aliyunoss.LifecycleRuleAbortMultipartUpload{Days: &seven}},
	} {
		if hasBoundedOSSMultipartLifecycle([]aliyunoss.LifecycleRule{rule}, "mcmods/oss") {
			t.Fatalf("%s lifecycle rule was accepted", name)
		}
	}
}
