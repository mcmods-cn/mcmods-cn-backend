package serverprobe

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"testing"
)

func TestFaviconRequiresValidBoundedPNGDimensions(t *testing.T) {
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 64, 64))); err != nil {
		t.Fatal(err)
	}
	valid := buffer.Bytes()
	oversized := append([]byte(nil), valid...)
	// Only the declaration and CRC change. Never allocate the advertised image.
	binary.BigEndian.PutUint32(oversized[16:20], 1<<20)
	binary.BigEndian.PutUint32(oversized[29:33], crc32.ChecksumIEEE(oversized[12:29]))
	for _, tc := range []struct {
		name string
		data []byte
		want bool
	}{
		{"normal Minecraft icon", valid, true},
		{"valid signature without image header", valid[:8], false},
		{"oversized declared width", oversized, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(tc.data)
			if got := safeFavicon(uri); (got != "") != tc.want {
				t.Fatalf("favicon accepted=%t want=%t", got != "", tc.want)
			}
		})
	}
}
