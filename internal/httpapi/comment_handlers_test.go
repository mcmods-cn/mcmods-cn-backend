package httpapi

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPureCYWatchCommand(t *testing.T) {
	for _, value := range []string{"CY", " cy ", "Cy。", "cY!", "cy！", "cy."} {
		if !isPureCY(value) {
			t.Fatalf("expected %q to be treated as a CY watch command", value)
		}
	}
	for _, value := range []string{"", "C Y", "cy please", "reply cy", "cy??", "cya"} {
		if isPureCY(value) {
			t.Fatalf("did not expect %q to be treated as a CY watch command", value)
		}
	}
}

func TestPlainCommentSummary(t *testing.T) {
	got := plainCommentSummary("## [Hello](https://example.com) **Minecraft**\nworld", 20)
	if strings.ContainsAny(got, "#[]()*\n") {
		t.Fatalf("plainCommentSummary() kept markup in %q", got)
	}
	if utf8.RuneCountInString(got) > 21 || !strings.HasSuffix(got, "…") {
		t.Fatalf("plainCommentSummary() did not truncate correctly: %q", got)
	}
}

func TestCommentLogAttachmentName(t *testing.T) {
	for _, value := range []string{"latest.log", "LATEST.LOG", "错误报告-2026-08-19.zip", `C:\\Users\\tester\\错误报告.zip`} {
		if !isCommentLogAttachmentName(value) {
			t.Fatalf("expected %q to use the sanitized log viewer", value)
		}
	}
	for _, value := range []string{"notes.txt", "logs.zip", "错误报告.txt", "错误报告.zip.exe"} {
		if isCommentLogAttachmentName(value) {
			t.Fatalf("did not expect %q to use the sanitized log viewer", value)
		}
	}
}
