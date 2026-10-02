package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/netip"
	"strings"
	"time"
)

var errAuthRateLimited = errors.New("authentication rate limit exceeded")

type authLimitDimension struct {
	name, value          string
	limit, degradedLimit int
	window               time.Duration
}

func authRateDigest(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func authRateNetwork(ip string) string {
	address, err := netip.ParseAddr(normalizeIPAddress(ip))
	if err != nil {
		return ""
	}
	bits := 64
	if address.Is4() {
		bits = 24
	}
	return netip.PrefixFrom(address, bits).Masked().String()
}

func (s *Server) authenticationRateLimited(ctx context.Context, action string, dimensions ...authLimitDimension) bool {
	for _, dimension := range dimensions {
		if dimension.value == "" || dimension.limit <= 0 || dimension.window <= 0 {
			continue
		}
		key := "auth:" + action + ":" + dimension.name + ":" + dimension.value
		result := s.cache.ConsumeLocalRateLimit(key, dimension.degradedLimit, dimension.window)
		if cacheConfig := s.cache.Config(); cacheConfig.AuthRateLimitEnabled || cacheConfig.RateLimitFailClosed {
			result = s.cache.ConsumeRateLimitPolicy(ctx, key, dimension.limit, dimension.degradedLimit, dimension.window)
		}
		if !result.Allowed {
			return true
		}
	}
	return false
}

func (s *Server) authAttemptRateLimited(ctx context.Context, account, ip, clientID string) (bool, error) {
	ip = normalizeIPAddress(ip)
	return s.authenticationRateLimited(ctx, "login",
		authLimitDimension{"account", authRateDigest(account), 12, 6, 15 * time.Minute},
		authLimitDimension{"ip", ip, 30, 12, 15 * time.Minute},
		authLimitDimension{"network", authRateNetwork(ip), 60, 20, 15 * time.Minute},
		authLimitDimension{"client", authRateDigest(clientID), 24, 8, 15 * time.Minute},
	), nil
}

func (s *Server) registrationRateLimited(ctx context.Context, ip, clientID string) (bool, error) {
	ip = normalizeIPAddress(ip)
	return s.authenticationRateLimited(ctx, "register",
		authLimitDimension{"ip", ip, 10, 4, time.Hour},
		authLimitDimension{"network", authRateNetwork(ip), 20, 8, time.Hour},
		authLimitDimension{"client", authRateDigest(clientID), 6, 3, time.Hour},
	), nil
}

func (s *Server) storeEmailVerificationCode(ctx context.Context, email, purpose, codeHash, ip, clientID string, expiresAt time.Time) (int64, error) {
	ip = normalizeIPAddress(ip)
	if s.authenticationRateLimited(ctx, "email-code:"+purpose,
		authLimitDimension{"email", authRateDigest(email), 3, 2, 10 * time.Minute},
		authLimitDimension{"ip", ip, 20, 8, 10 * time.Minute},
		authLimitDimension{"network", authRateNetwork(ip), 30, 12, 10 * time.Minute},
		authLimitDimension{"client", authRateDigest(clientID), 8, 4, 10 * time.Minute},
	) {
		return 0, errAuthRateLimited
	}
	var codeID int64
	err := s.db.QueryRow(ctx, `insert into email_verification_codes(email,purpose,code_hash,request_ip,expires_at)
		values($1,$2,$3,$4,$5) returning id`, email, purpose, codeHash, ip, expiresAt).Scan(&codeID)
	return codeID, err
}
