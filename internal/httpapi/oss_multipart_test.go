package httpapi

import "testing"

func TestOSSMultipartSizing(t *testing.T) {
	if shouldUseOSSMultipart(true, ossMultipartThreshold-1) {
		t.Fatal("small upload unexpectedly selected multipart mode")
	}
	if !shouldUseOSSMultipart(true, ossMultipartThreshold) {
		t.Fatal("threshold upload did not select multipart mode")
	}
	if shouldUseOSSMultipart(false, ossMultipartThreshold*2) {
		t.Fatal("multipart mode must remain opt-in for older clients")
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
