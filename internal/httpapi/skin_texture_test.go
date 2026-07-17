package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestSanitizeMinecraftTextureSkin(t *testing.T) {
	raw := encodeTestPNG(t, 64, 64, color.NRGBA{R: 20, G: 80, B: 160, A: 255})
	texture, err := sanitizeMinecraftTexture(bytes.NewReader(raw), "skin", int64(len(raw)+1024))
	if err != nil {
		t.Fatalf("sanitize skin: %v", err)
	}
	if texture.Kind != "skin" || texture.Width != 64 || texture.Height != 64 {
		t.Fatalf("unexpected sanitized skin: %#v", texture)
	}
	if texture.SizeBytes != int64(len(texture.Data)) {
		t.Fatalf("size mismatch: %d != %d", texture.SizeBytes, len(texture.Data))
	}
	digest := sha256.Sum256(texture.Data)
	if texture.Hash != hex.EncodeToString(digest[:]) {
		t.Fatalf("hash does not describe sanitized bytes")
	}
	decoded, err := png.Decode(bytes.NewReader(texture.Data))
	if err != nil {
		t.Fatalf("decode sanitized skin: %v", err)
	}
	if decoded.Bounds().Dx() != 64 || decoded.Bounds().Dy() != 64 {
		t.Fatalf("unexpected sanitized bounds: %v", decoded.Bounds())
	}
}

func TestSanitizeMinecraftTextureCapePadsLegacyLayout(t *testing.T) {
	raw := encodeTestPNG(t, 22, 17, color.NRGBA{R: 230, G: 40, B: 10, A: 255})
	texture, err := sanitizeMinecraftTexture(bytes.NewReader(raw), "cape", int64(len(raw)+2048))
	if err != nil {
		t.Fatalf("sanitize cape: %v", err)
	}
	if texture.Width != 64 || texture.Height != 32 {
		t.Fatalf("legacy cape was not padded: %dx%d", texture.Width, texture.Height)
	}
	decoded, err := png.Decode(bytes.NewReader(texture.Data))
	if err != nil {
		t.Fatalf("decode padded cape: %v", err)
	}
	inside := color.NRGBAModel.Convert(decoded.At(0, 0)).(color.NRGBA)
	if inside.A != 255 || inside.R != 230 {
		t.Fatalf("cape pixels were not preserved: %#v", inside)
	}
	padding := color.NRGBAModel.Convert(decoded.At(63, 31)).(color.NRGBA)
	if padding.A != 0 {
		t.Fatalf("cape padding is not transparent: %#v", padding)
	}
}

func TestSanitizeMinecraftTextureRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		kind   string
		width  int
		height int
	}{
		{name: "unknown kind", kind: "elytra", width: 64, height: 64},
		{name: "invalid skin ratio", kind: "skin", width: 128, height: 96},
		{name: "invalid cape ratio", kind: "cape", width: 64, height: 64},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := encodeTestPNG(t, test.width, test.height, color.NRGBA{A: 255})
			if _, err := sanitizeMinecraftTexture(bytes.NewReader(raw), test.kind, int64(len(raw)+1024)); err == nil {
				t.Fatal("expected invalid texture to be rejected")
			}
		})
	}
	raw := encodeTestPNG(t, 64, 64, color.NRGBA{A: 255})
	if _, err := sanitizeMinecraftTexture(bytes.NewReader(raw), "skin", int64(len(raw)-1)); err == nil {
		t.Fatal("expected byte limit to be enforced")
	}
}

func TestNewMinecraftUUID(t *testing.T) {
	first, err := newMinecraftUUID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newMinecraftUUID()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || len(first) != 36 || strings.Count(first, "-") != 4 {
		t.Fatalf("unexpected UUIDs: %q %q", first, second)
	}
	if first[14] != '4' || !strings.Contains("89ab", strings.ToLower(first[19:20])) {
		t.Fatalf("UUID does not have RFC 4122 v4 bits: %q", first)
	}
}

func encodeTestPNG(t *testing.T, width, height int, fill color.NRGBA) []byte {
	t.Helper()
	value := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			value.SetNRGBA(x, y, fill)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, value); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}
