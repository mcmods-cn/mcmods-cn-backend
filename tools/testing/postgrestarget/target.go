// Package postgrestarget checks effective pgx endpoints for local integration tests.
package postgrestarget

import (
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ParseSingleLoopback allows TLS fallbacks only within one PostgreSQL service.
// Callers still confirm that the service and database belong to the test run.
func ParseSingleLoopback(target string) (*pgxpool.Config, error) {
	parsed, err := pgxpool.ParseConfig(target)
	if err != nil {
		return nil, errors.New("cannot parse PostgreSQL test target")
	}
	connection := parsed.ConnConfig
	switch connection.Host {
	case "127.0.0.1", "localhost", "::1":
	default:
		return nil, errors.New("PostgreSQL tests require an owned loopback service")
	}
	for _, fallback := range connection.Fallbacks {
		if fallback.Host != connection.Host || fallback.Port != connection.Port {
			return nil, errors.New("PostgreSQL tests require a single database endpoint")
		}
	}
	return parsed, nil
}
