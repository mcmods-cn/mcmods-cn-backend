package serverprobe

import (
	"context"
	"net"
	"testing"
)

type fakeResolver struct {
	ips []net.IPAddr
	srv []*net.SRV
}

func (resolver fakeResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return resolver.ips, nil
}

func (resolver fakeResolver) LookupSRV(context.Context, string, string, string) (string, []*net.SRV, error) {
	return "", resolver.srv, nil
}

func TestParseAddressNormalizesDefaultPort(t *testing.T) {
	host, port, explicit, normalized, err := ParseAddress("Example.COM")
	if err != nil {
		t.Fatal(err)
	}
	if host != "example.com" || port != 25565 || explicit || normalized != "example.com:25565" {
		t.Fatalf("unexpected normalization: %q %d %v %q", host, port, explicit, normalized)
	}
}

func TestResolveTargetRejectsPrivateAddress(t *testing.T) {
	_, err := ResolveTarget(context.Background(), fakeResolver{
		ips: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}},
	}, "example.com")
	if err == nil {
		t.Fatal("private address was accepted")
	}
}

func TestResolveTargetKeepsOriginalHostForSRVHandshake(t *testing.T) {
	target, err := ResolveTarget(context.Background(), fakeResolver{
		ips: []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}},
		srv: []*net.SRV{{Target: "node.example.net.", Port: 25570}},
	}, "play.example.net")
	if err != nil {
		t.Fatal(err)
	}
	if target.HandshakeHost != "play.example.net" || target.ConnectHost != "node.example.net" ||
		target.ConnectPort != 25570 || target.NormalizedAddress != "play.example.net:25565" {
		t.Fatalf("unexpected SRV target: %#v", target)
	}
}

func TestSafeFaviconRejectsNonPNG(t *testing.T) {
	if safeFavicon("data:image/png;base64,SGVsbG8=") != "" {
		t.Fatal("non-PNG favicon was accepted")
	}
}

func TestEmptyForgeDataIsNotACompleteModList(t *testing.T) {
	var result Result
	err := parseStatusJSON([]byte(`{
		"version":{"name":"Velocity 1.7.2-1.21.8","protocol":772},
		"players":{"max":20,"online":1},
		"description":{"text":"test"},
		"isModded":true,
		"forgeData":{}
	}`), &result)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Modded || result.ModListComplete {
		t.Fatalf("unexpected status result: %#v", result)
	}
}
