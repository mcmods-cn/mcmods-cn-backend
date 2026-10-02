package httpapi

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"testing"
)

func TestSanitizeSharedSiteLogoFullyDecodesWebPAndStripsMetadata(t *testing.T) {
	// A real Sharp 0.35.4-produced 2x1 WebP with an EXIF Artist tag. The
	// checked-in bytes make the Go gate independent of a Node installation.
	data, err := base64.StdEncoding.DecodeString("UklGRjwBAABXRUJQVlA4WAoAAAAIAAAAAQAAAAAAVlA4IDAAAADQAQCdASoCAAEAAUAmJaACdLoB+AADsAD+8JtD/xZyMR2eb/8my+WhfLQv+ScAAABFWElG5gAAAEV4aWYAAElJKgAIAAAABwASAQMAAQAAAAEAAAAaAQUAAQAAAGIAAAAbAQUAAQAAAGoAAAAoAQMAAQAAAAIAAAA7AQIAHwAAAHIAAAATAgMAAQAAAAEAAABphwQAAQAAAJIAAAAAAAAAOGMAAOgDAAA4YwAA6AMAAE9QUzAwOCBtZXRhZGF0YSBtdXN0IGRpc2FwcGVhcgAABgAAkAcABAAAADAyMTABkQcABAAAAAECAwAAoAcABAAAADAxMDABoAMAAQAAAP//AAACoAQAAQAAAAIAAAADoAQAAQAAAAEAAAAAAAAA")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("OPS008 metadata must disappear")) {
		t.Fatal("metadata fixture is missing its tag")
	}
	safe, err := sanitizeSharedSiteLogo(data, "image/webp")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(safe))
	if err != nil || decoded.Bounds() != image.Rect(0, 0, 2, 1) {
		t.Fatalf("safe PNG geometry: %v %v", decoded, err)
	}
	for _, metadata := range [][]byte{[]byte("EXIF"), []byte("Exif"), []byte("OPS008 metadata must disappear"), []byte("iCCP"), []byte("tEXt")} {
		if bytes.Contains(safe, metadata) {
			t.Fatalf("source metadata survived the PNG re-encode: %q", metadata)
		}
	}
	if _, err := sanitizeSharedSiteLogo(data[:len(data)/2], "image/webp"); err == nil {
		t.Fatal("accepted incomplete WebP pixels/metadata")
	}
	if _, err := sanitizeSharedSiteLogo(data, "image/png"); err == nil {
		t.Fatal("trusted a MIME mismatch")
	}
}

func TestSanitizeSharedSiteLogoRejectsOversizedGeometryAndTruncatedPixels(t *testing.T) {
	for _, width := range []int{1, siteLogoMaximumEdge, siteLogoMaximumEdge + 1} {
		var source bytes.Buffer
		if err := png.Encode(&source, image.NewRGBA(image.Rect(0, 0, width, 1))); err != nil {
			t.Fatal(err)
		}
		safe, err := sanitizeSharedSiteLogo(source.Bytes(), "image/png")
		if width <= siteLogoMaximumEdge && (err != nil || len(safe) == 0) {
			t.Fatalf("safe width %d rejected: %v", width, err)
		}
		if width > siteLogoMaximumEdge && err == nil {
			t.Fatalf("unbounded width %d accepted", width)
		}
		if _, err := sanitizeSharedSiteLogo(source.Bytes()[:40], "image/png"); err == nil {
			t.Fatal("accepted header without complete pixels")
		}
	}
}
