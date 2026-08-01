package httpapi

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestAllowedExternalModIconHosts(t *testing.T) {
	for _, host := range []string{"cdn.modrinth.com", "media.forgecdn.net", "mediafilez.forgecdn.net", "avatars.githubusercontent.com"} {
		if !isAllowedExternalModIconHost(host) {
			t.Fatalf("expected %q to be allowed", host)
		}
	}
	for _, host := range []string{"cdn.modrinth.com.example.org", "forgecdn.net.example.org", "127.0.0.1", "localhost"} {
		if isAllowedExternalModIconHost(host) {
			t.Fatalf("expected %q to be rejected", host)
		}
	}
}

func TestExternalModIconFormatRequiresDecodableImage(t *testing.T) {
	imageData := image.NewRGBA(image.Rect(0, 0, 2, 2))
	imageData.Set(0, 0, color.RGBA{R: 0xff, A: 0xff})
	paletted := image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Black, color.White})
	tests := []struct {
		name      string
		extension string
		encode    func(*bytes.Buffer) error
	}{
		{name: "png", extension: ".png", encode: func(output *bytes.Buffer) error { return png.Encode(output, imageData) }},
		{name: "jpeg", extension: ".jpg", encode: func(output *bytes.Buffer) error { return jpeg.Encode(output, imageData, nil) }},
		{name: "gif", extension: ".gif", encode: func(output *bytes.Buffer) error { return gif.Encode(output, paletted, nil) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := test.encode(&output); err != nil {
				t.Fatalf("encode fixture: %v", err)
			}
			_, extension, err := externalModIconFormat(output.Bytes())
			if err != nil || extension != test.extension {
				t.Fatalf("detected as %q with error %v", extension, err)
			}
			truncated := append([]byte(nil), output.Bytes()[:len(output.Bytes())/2]...)
			if _, _, err = externalModIconFormat(truncated); err == nil {
				t.Fatal("truncated image must not be accepted")
			}
		})
	}
	for _, data := range [][]byte{
		append([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, make([]byte, 24)...),
		append([]byte{0xff, 0xd8, 0xff, 0xe0}, make([]byte, 28)...),
		append([]byte("GIF89a"), make([]byte, 26)...),
		[]byte("<svg></svg>"),
	} {
		if _, _, err := externalModIconFormat(data); err == nil {
			t.Fatal("header-only or active image content must not be accepted")
		}
	}
}

func TestOSSObjectKeyUnderEndpoint(t *testing.T) {
	objectKey, ok := ossObjectKeyUnderEndpoint(
		"https://oss.mcmods.cn/base/mcmods/project/mods/m123abc/icons/project/original/icon.webp",
		"https://oss.mcmods.cn/base",
	)
	if !ok || objectKey != "mcmods/project/mods/m123abc/icons/project/original/icon.webp" {
		t.Fatalf("unexpected object key %q, ok=%v", objectKey, ok)
	}
	for _, rawURL := range []string{
		"https://oss.mcmods.cn.example.org/base/mcmods/icon.webp",
		"https://oss.mcmods.cn/other/mcmods/icon.webp",
		"https://oss.mcmods.cn/base/../secret",
	} {
		if _, ok = ossObjectKeyUnderEndpoint(rawURL, "https://oss.mcmods.cn/base"); ok {
			t.Fatalf("unsafe endpoint URL accepted: %s", rawURL)
		}
	}
}
