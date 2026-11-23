package postgrestarget

import "testing"

func TestParseSingleLoopbackKeepsOneEndpoint(t *testing.T) {
	for _, scenario := range []struct {
		name, target string
		allowed      bool
	}{
		{"same endpoint TLS fallback", "127.0.0.1:5432", true},
		{"different loopback service", "127.0.0.1:5432,127.0.0.1:5433", false},
		{"non-loopback service", "database.example.invalid:5432", false},
		{"non-loopback fallback", "127.0.0.1:5432,database.example.invalid:5432", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			_, err := ParseSingleLoopback("postgres://test@" + scenario.target + "/test_fixture?sslmode=prefer")
			if (err == nil) != scenario.allowed {
				t.Fatalf("allowed=%v, error=%v", scenario.allowed, err)
			}
		})
	}
}
