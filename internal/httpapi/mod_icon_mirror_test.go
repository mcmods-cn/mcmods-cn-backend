package httpapi

import "testing"

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

func TestExternalModIconFormat(t *testing.T) {
	tests := []struct {
		name      string
		data      []byte
		extension string
	}{
		{name: "png", data: append([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, make([]byte, 24)...), extension: ".png"},
		{name: "jpeg", data: append([]byte{0xff, 0xd8, 0xff, 0xe0}, make([]byte, 28)...), extension: ".jpg"},
		{name: "gif", data: append([]byte("GIF89a"), make([]byte, 26)...), extension: ".gif"},
		{name: "webp", data: []byte{'R', 'I', 'F', 'F', 0x10, 0, 0, 0, 'W', 'E', 'B', 'P', 'V', 'P', '8', ' '}, extension: ".webp"},
	}
	for _, test := range tests {
		_, extension, err := externalModIconFormat(test.data)
		if err != nil || extension != test.extension {
			t.Fatalf("%s detected as %q with error %v", test.name, extension, err)
		}
	}
	if _, _, err := externalModIconFormat([]byte("<svg></svg>")); err == nil {
		t.Fatal("SVG must not be accepted as a mirrored mod icon")
	}
}

func TestURLUnderEndpoint(t *testing.T) {
	if !isURLUnderEndpoint("https://oss.mcmods.cn/mcmods/project/mods/m123abc/icons/project/original/icon.webp", "https://oss.mcmods.cn") {
		t.Fatal("expected OSS object URL to match public endpoint")
	}
	if isURLUnderEndpoint("https://oss.mcmods.cn.example.org/mcmods/icon.webp", "https://oss.mcmods.cn") {
		t.Fatal("lookalike host must not match public endpoint")
	}
}
