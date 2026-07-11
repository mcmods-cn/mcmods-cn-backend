package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestRequestClientLocationFromESAHeaders(t *testing.T) {
	request := httptest.NewRequest("GET", "http://mcmods.cn/api/v1/location", nil)
	request.Header.Set("Ali-Real-Client-Ip", "203.0.113.9")
	request.Header.Set("Ali-Ip-Country", "cn")
	request.Header.Set("Ali-Ip-City", "Hangzhou")

	location := requestClientLocation(request)
	if location.IP != "203.0.113.9" || location.CountryCode != "CN" || location.City != "Hangzhou" {
		t.Fatalf("unexpected location: %#v", location)
	}
}
