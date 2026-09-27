package httpapi

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

func TestOSSFileRecordExposesConversionDetails(t *testing.T) {
	record := ossFileRecord(
		"file23456", "bucket", "endpoint", "region", "mcmods/user/1/files/playground/id.webp", "user/1/files/playground", "playground",
		"image.webp", "image.png", "image/webp", 70, 158, "hash", "active", "pending", time.Time{}, time.Time{},
	)
	if record["converted"] != true {
		t.Fatalf("converted = %#v, want true", record["converted"])
	}
	if record["sourceSizeBytes"] != int64(158) || record["sizeBytes"] != int64(70) {
		t.Fatalf("unexpected conversion sizes: %#v", record)
	}
}

func TestReadAPNGAnimationControl(t *testing.T) {
	png := bytes.NewBuffer([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	for _, chunkType := range []string{"IHDR", "acTL"} {
		_ = binary.Write(png, binary.BigEndian, uint32(0))
		png.WriteString(chunkType)
		png.Write(make([]byte, 4))
	}
	animated, err := readAPNGAnimationControl(png)
	if err != nil {
		t.Fatalf("readAPNGAnimationControl() error = %v", err)
	}
	if !animated {
		t.Fatal("expected acTL chunk to identify an animated PNG")
	}
}

func TestShouldPersistMarkdownImageAsWebP(t *testing.T) {
	tests := []struct {
		name        string
		original    string
		contentType string
		source      string
		category    string
		want        bool
	}{
		{name: "playground png", original: "image.png", contentType: "image/png", source: "playground", category: "user/1/files/playground", want: true},
		{name: "comment jpeg", original: "photo.jpg", contentType: "image/jpeg", source: "comment", category: "user/1/files/comments", want: true},
		{name: "project text attachment", original: "cover.jpeg", contentType: "image/jpeg", category: "project/mods/abc234567/files/text/def345678", want: true},
		{name: "legacy project description", original: "legacy.png", contentType: "image/png", category: "projects/42/description", want: true},
		{name: "avatar remains original", original: "avatar.png", contentType: "image/png", source: "avatar", category: "user/1/files/avatars", want: false},
		{name: "webp remains original", original: "image.webp", contentType: "image/webp", source: "playground", category: "user/1/files/playground", want: false},
		{name: "non image remains original", original: "notes.png", contentType: "text/plain", source: "playground", category: "user/1/files/playground", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldPersistMarkdownImageAsWebP(test.original, test.contentType, test.source, test.category); got != test.want {
				t.Fatalf("shouldPersistMarkdownImageAsWebP() = %v, want %v", got, test.want)
			}
		})
	}
}
