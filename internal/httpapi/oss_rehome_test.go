package httpapi

import "testing"

func TestGalleryRehomeDoesNotOverwriteAReplacementFile(t *testing.T) {
	old := modGalleryObjectForRehome{galleryPublicID: "abc234567", filePublicID: "bcd234567", objectKey: "uploads/old.PNG"}
	replacement := old
	replacement.filePublicID = "cde234567"
	replacement.objectKey = "uploads/new.png"
	prefix := "mcmods/project/mods/m23456789/files/text/m23456789/gallery"
	oldKey := modGalleryRehomeObjectKey(prefix, old)
	newKey := modGalleryRehomeObjectKey(prefix, replacement)
	if oldKey == newKey {
		t.Fatalf("a delayed old copy targets the replacement object %q", oldKey)
	}
	if want := prefix + "/abc234567-bcd234567.png"; oldKey != want {
		t.Fatalf("old key=%q want %q", oldKey, want)
	}
	// Moving the same registered file does not change its destination or strip
	// an extension derived from the sanitized upload's original filename.
	old.objectKey = "uploads/without-extension"
	old.originalName = "original.png"
	if key := modGalleryRehomeObjectKey(prefix, old); key != oldKey {
		t.Fatalf("same file destination changed to %q", key)
	}
}
