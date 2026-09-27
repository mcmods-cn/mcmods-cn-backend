package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestOSSUploadQuotaHotPathsUseAtomicBuckets(t *testing.T) {
	ossBytes, err := os.ReadFile("oss_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	ossAdminBytes, err := os.ReadFile("oss_admin_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	ossSource := string(ossBytes) + string(ossAdminBytes)
	for _, name := range []string{"enforceUserFileUploadLimits", "enforceUserStoredFileLimit"} {
		body := goFunctionBody(t, ossSource, name)
		if strings.Contains(strings.ToLower(body), "sum(") || strings.Contains(body, "from oss_files") {
			t.Fatalf("%s still aggregates oss_files on the upload hot path", name)
		}
	}
	quotaBytes, err := os.ReadFile("oss_quota.go")
	if err != nil {
		t.Fatal(err)
	}
	quotaSource := string(quotaBytes)
	for _, fragment := range []string{
		"reserveUserOSSUploadQuota",
		"settleUserOSSUploadQuotaTx",
		"releaseUserOSSUploadQuotaReservation",
		"oss_user_upload_quota_reservations",
	} {
		if !strings.Contains(ossSource+quotaSource, fragment) {
			t.Fatalf("OSS upload flow is missing atomic quota contract %q", fragment)
		}
	}
	presignBody := goFunctionBody(t, ossSource, "createOSSDirectUploadWithScope")
	for _, fragment := range []string{"reserveUserOSSUploadQuota", "releaseQuotaReservation", "quotaReservationObjectKey"} {
		if !strings.Contains(presignBody, fragment) {
			t.Fatalf("direct upload presign does not use quota reservation step %q", fragment)
		}
	}
	completeBody := goFunctionBody(t, ossSource, "completeOSSDirectUploadWithScope")
	for _, fragment := range []string{"settleUserOSSUploadQuotaTx", "quotaTx.QueryRow", "releaseUserOSSUploadQuotaReservation"} {
		if !strings.Contains(completeBody, fragment) {
			t.Fatalf("direct upload completion does not atomically settle quota step %q", fragment)
		}
	}

	userBytes, err := os.ReadFile("user_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	quotaBody := goFunctionBody(t, string(userBytes), "loadUserOSSFileQuotaUsage")
	if strings.Contains(strings.ToLower(quotaBody), "sum(") || strings.Contains(quotaBody, "from oss_files") {
		t.Fatal("user quota read still aggregates the full OSS file history")
	}
}

func TestOSSQuotaArithmeticRejectsOverflowAndExactOversubscription(t *testing.T) {
	for _, test := range []struct {
		active, reserved, requested, limit int64
		want                               bool
	}{
		{active: 600, reserved: 300, requested: 100, limit: 1_000, want: true},
		{active: 600, reserved: 300, requested: 101, limit: 1_000, want: false},
		{active: 1_000, reserved: 0, requested: maxPermissionBytes, limit: 1_000, want: false},
		{active: maxPermissionBytes, reserved: maxPermissionBytes, requested: maxPermissionBytes, limit: maxPermissionBytes, want: true},
		{active: -1, reserved: 0, requested: 1, limit: 1_000, want: false},
	} {
		if got := quotaAllows(test.active, test.reserved, test.requested, test.limit); got != test.want {
			t.Errorf("quotaAllows(%d,%d,%d,%d)=%t want %t", test.active, test.reserved, test.requested, test.limit, got, test.want)
		}
	}
}

func goFunctionBody(t *testing.T, source, name string) string {
	t.Helper()
	start := strings.Index(source, "func ")
	for start >= 0 {
		candidate := source[start:]
		lineEnd := strings.IndexByte(candidate, '\n')
		if lineEnd < 0 {
			lineEnd = len(candidate)
		}
		if strings.Contains(candidate[:lineEnd], name+"(") {
			depth := 0
			opened := false
			for index, char := range candidate {
				switch char {
				case '{':
					depth++
					opened = true
				case '}':
					depth--
					if opened && depth == 0 {
						return candidate[:index+1]
					}
				}
			}
			break
		}
		next := strings.Index(candidate[len("func "):], "func ")
		if next < 0 {
			break
		}
		start += len("func ") + next
	}
	t.Fatalf("function %s not found", name)
	return ""
}
