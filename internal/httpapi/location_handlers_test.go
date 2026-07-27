package httpapi

import (
	"net"
	"net/http/httptest"
	"testing"
)

func TestRequestClientLocationIgnoresHeadersFromUntrustedPeer(t *testing.T) {
	request := httptest.NewRequest("GET", "http://mcmods.cn/api/v1/location", nil)
	request.Header.Set("Ali-Real-Client-Ip", "203.0.113.9")
	request.Header.Set("Ali-Ip-Country", "cn")
	request.Header.Set("Ali-Ip-City", "Hangzhou")

	location := (&Server{}).requestClientLocation(request)
	if location.IP == "203.0.113.9" || location.CountryCode != "" || location.City != "" {
		t.Fatalf("untrusted forwarding headers were accepted: %#v", location)
	}
}

func TestRequestClientLocationAcceptsHeadersFromTrustedPeer(t *testing.T) {
	request := httptest.NewRequest("GET", "http://mcmods.cn/api/v1/location", nil)
	request.Header.Set("Ali-Real-Client-Ip", "203.0.113.9")
	request.Header.Set("Ali-Ip-Country", "cn")
	request.Header.Set("Ali-Ip-City", "Hangzhou")
	_, trustedNetwork, err := net.ParseCIDR("192.0.2.0/24")
	if err != nil {
		t.Fatal(err)
	}
	location := (&Server{trustedProxies: []*net.IPNet{trustedNetwork}}).requestClientLocation(request)
	if location.IP != "203.0.113.9" || location.CountryCode != "CN" || location.City != "Hangzhou" {
		t.Fatalf("unexpected location: %#v", location)
	}
}
