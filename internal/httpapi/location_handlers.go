package httpapi

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

type clientLocation struct {
	IP          string `json:"ip"`
	CountryCode string `json:"countryCode"`
	City        string `json:"city"`
}

func (s *Server) visitorLocation(w http.ResponseWriter, r *http.Request) {
	location := s.requestClientLocation(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"ip":              location.IP,
		"countryCode":     location.CountryCode,
		"city":            location.City,
		"isMainlandChina": location.CountryCode == "CN",
	})
}

func (s *Server) requestClientLocation(r *http.Request) clientLocation {
	peer := normalizeIPAddress(remoteIP(r.RemoteAddr))
	if !networkContainsIP(s.trustedProxies, peer) {
		return clientLocation{IP: peer}
	}
	return requestClientLocationFromProxy(r, peer)
}

func requestClientLocationFromProxy(r *http.Request, fallbackIP string) clientLocation {
	ip := firstHeaderValue(r, "Ali-Real-Client-Ip", "Ali-Cdn-Real-Ip", "True-Client-IP")
	if ip == "" {
		ip = firstForwardedIP(r.Header.Get("X-Forwarded-For"))
	}
	if ip == "" {
		ip = fallbackIP
	}

	countryCode := strings.ToUpper(firstHeaderValue(r, "Ali-Ip-Country", "IP-Country-Code", "CF-IPCountry"))
	if len(countryCode) > 8 {
		countryCode = ""
	}

	return clientLocation{
		IP:          normalizeIPAddress(ip),
		CountryCode: countryCode,
		City:        decodeLocationHeader(firstHeaderValue(r, "Ali-Ip-City", "IP-City")),
	}
}

func parseTrustedProxyNetworks(values []string) []*net.IPNet {
	result := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if ip := net.ParseIP(value); ip != nil {
			bits := 128
			if ip.To4() != nil {
				bits = 32
			}
			result = append(result, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		if _, network, err := net.ParseCIDR(value); err == nil {
			result = append(result, network)
		}
	}
	return result
}

func networkContainsIP(networks []*net.IPNet, value string) bool {
	ip := net.ParseIP(value)
	if ip == nil {
		return false
	}
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func firstHeaderValue(r *http.Request, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(r.Header.Get(name)); value != "" {
			return value
		}
	}
	return ""
}

func firstForwardedIP(value string) string {
	if first, _, ok := strings.Cut(value, ","); ok {
		return strings.TrimSpace(first)
	}
	return strings.TrimSpace(value)
}

func remoteIP(address string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(address))
	if err == nil {
		return host
	}
	return strings.TrimSpace(address)
}

func normalizeIPAddress(value string) string {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return ""
	}
	return ip.String()
}

func decodeLocationHeader(value string) string {
	value = strings.TrimSpace(value)
	if decoded, err := url.QueryUnescape(value); err == nil {
		value = decoded
	}
	if len(value) > 128 {
		return value[:128]
	}
	return value
}
