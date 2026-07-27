package httpapi

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestBlueprintCoverRequiresSynchronousRasterValidation(t *testing.T) {
	if !requiresSynchronousCatalogImageValidation("blueprint_cover:bcd345678") {
		t.Fatal("blueprint covers must be validated before becoming active")
	}
}

func TestWebPContainerRejectsHeaderOnlyAndTruncatedPayloads(t *testing.T) {
	vp8x := makeWebPFixture("VP8X", []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	if _, _, ok := webPDimensions(vp8x); ok {
		t.Fatal("VP8X canvas header without image payload must be rejected")
	}

	vp8lHeaderOnly := makeWebPFixture("VP8L", []byte{0x2f, 0, 0, 0, 0, 0})
	if _, _, ok := webPDimensions(vp8lHeaderOnly); ok {
		t.Fatal("header-only VP8L payload must be rejected")
	}
	if _, err := validateRasterImageBytes(vp8lHeaderOnly, "image/webp"); err == nil {
		t.Fatal("fake VP8L bitstream must not be accepted as a raster")
	}

	// A superficially plausible key-frame header still is not a complete VP8
	// bitstream and must fail the real decoder.
	vp8Payload := []byte{0x30, 0, 0, 0x9d, 0x01, 0x2a, 1, 0, 1, 0, 0}
	fakeVP8 := makeWebPFixture("VP8 ", vp8Payload)
	if _, err := validateRasterImageBytes(fakeVP8, "image/webp"); err == nil {
		t.Fatal("fake VP8 bitstream must not be accepted as a raster")
	}

	truncated := append([]byte(nil), makeWebPFixture("VP8L", make([]byte, 12))...)
	truncated = truncated[:len(truncated)-1]
	if _, _, ok := webPDimensions(truncated); ok {
		t.Fatal("truncated RIFF chunk must be rejected")
	}
}

func TestValidateRasterImageBytesDecodesWholePNG(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 2, 2))
	source.Set(0, 0, color.RGBA{G: 0xff, A: 0xff})
	var output bytes.Buffer
	if err := png.Encode(&output, source); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	if _, err := validateRasterImageBytes(output.Bytes(), "image/png"); err != nil {
		t.Fatalf("valid PNG rejected: %v", err)
	}
	truncated := append([]byte(nil), output.Bytes()[:len(output.Bytes())-6]...)
	if _, err := validateRasterImageBytes(truncated, "image/png"); err == nil {
		t.Fatal("truncated PNG must not be accepted")
	}
}

func TestParseModTextUploadSource(t *testing.T) {
	siteID, contentID, ok := parseModTextUploadSource("mod_text:Applied-Energistics-2:rsc234567")
	if !ok || siteID != "applied-energistics-2" || contentID != "rsc234567" {
		t.Fatalf("unexpected parsed source: %q, %q, %v", siteID, contentID, ok)
	}
	if _, _, ok = parseModTextUploadSource("mod_text:missing-content"); ok {
		t.Fatal("incomplete mod_text source must be rejected")
	}
	if got, want := ossProjectTextCategory("mod", "mod234567", contentID, "content"),
		"project/mods/mod234567/files/text/rsc234567/content"; got != want {
		t.Fatalf("unexpected mod text category: got %q want %q", got, want)
	}
}

func TestPersistedWebPObjectKey(t *testing.T) {
	if got, want := persistedWebPObjectKey("mcmods/user/1/files/text/photo.png"), "mcmods/user/1/files/text/photo.webp"; got != want {
		t.Fatalf("unexpected converted key: got %q want %q", got, want)
	}
}

func makeWebPFixture(chunkType string, payload []byte) []byte {
	paddedSize := len(payload)
	if paddedSize%2 != 0 {
		paddedSize++
	}
	data := make([]byte, 12+8+paddedSize)
	copy(data[:4], "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))
	copy(data[8:12], "WEBP")
	copy(data[12:16], chunkType)
	binary.LittleEndian.PutUint32(data[16:20], uint32(len(payload)))
	copy(data[20:], payload)
	return data
}
