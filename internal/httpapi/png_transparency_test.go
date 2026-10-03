package httpapi

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func oct02PNGWithChunk(t *testing.T, raw []byte, kind string, payload []byte) []byte {
	t.Helper()
	chunk := make([]byte, 12+len(payload))
	binary.BigEndian.PutUint32(chunk[:4], uint32(len(payload)))
	copy(chunk[4:8], kind)
	copy(chunk[8:], payload)
	binary.BigEndian.PutUint32(chunk[8+len(payload):], crc32.ChecksumIEEE(chunk[4:8+len(payload)]))
	// The encoder emits IHDR first. Place the test chunk before image data.
	offset := 8 + 12 + int(binary.BigEndian.Uint32(raw[8:12]))
	return append(append(append([]byte{}, raw[:offset]...), chunk...), raw[offset:]...)
}

func TestOCT02PNGTransparencyRequiresRealValidatedChunk(t *testing.T) {
	opaque := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			opaque.SetRGBA(x, y, color.RGBA{R: 100, G: 20, B: 10, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, opaque); err != nil {
		t.Fatal(err)
	}
	raw := buffer.Bytes()
	if raw[25] != 2 {
		t.Fatalf("expected opaque RGB PNG fixture, color type=%d", raw[25])
	}
	if _, err := validateModResourcePNGConfig(oct02PNGWithChunk(t, raw, "tEXt", []byte("note\x00tRNS")), "image/png"); err == nil {
		t.Fatal("text containing tRNS was mistaken for a transparency chunk")
	}
	transparent := oct02PNGWithChunk(t, raw, "tRNS", []byte{0, 100, 0, 20, 0, 10})
	if _, err := validateModResourcePNGConfig(transparent, "image/png"); err != nil {
		t.Fatalf("valid RGB transparency chunk rejected: %v", err)
	}
	broken := append([]byte{}, transparent...)
	broken[8+25+12+6-1] ^= 1
	if _, err := validateModResourcePNGConfig(broken, "image/png"); err == nil {
		t.Fatal("bad transparency chunk CRC accepted")
	}
	alpha := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	alpha.SetNRGBA(0, 0, color.NRGBA{R: 100, A: 100})
	buffer.Reset()
	if err := png.Encode(&buffer, alpha); err != nil {
		t.Fatal(err)
	}
	if _, err := validateModResourcePNGConfig(buffer.Bytes(), "image/png"); err != nil {
		t.Fatalf("alpha channel rejected: %v", err)
	}
}
