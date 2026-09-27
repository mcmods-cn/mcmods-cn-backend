package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestTemporaryStickerUploadSourceIsAnExplicitClosedPrefix(t *testing.T) {
	for _, source := range []string{"sticker-upload", "sticker-upload:01234567-89ab-cdef-0123-456789abcdef"} {
		if !isTemporaryStickerUploadSource(source) {
			t.Fatalf("temporary sticker source %q was rejected", source)
		}
	}
	for _, source := range []string{"", "sticker_derived", "sticker-uploaded", "other:sticker-upload"} {
		if isTemporaryStickerUploadSource(source) {
			t.Fatalf("non-temporary source %q was accepted", source)
		}
	}
}

func TestStickerUploadLifecycleHasExplicitDiscardAndExpiryCleanup(t *testing.T) {
	routes, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	handlers, err := os.ReadFile("sticker_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	maintenance, err := os.ReadFile("maintenance_worker.go")
	if err != nil {
		t.Fatal(err)
	}
	combined := string(routes) + string(handlers) + string(maintenance)
	for _, required := range []string{
		`DELETE /api/v1/admin/sticker-upload-files/{fileId}`,
		`discardStickerUpload`, `pruneStickerUploads`, `sticker_upload_expired`,
		`tombstoneUnreferencedStickerFileTx`, `source='sticker-upload'`, `source like 'sticker-upload:%'`,
	} {
		if !strings.Contains(combined, required) {
			t.Errorf("sticker temporary upload lifecycle is missing %q", required)
		}
	}
}
