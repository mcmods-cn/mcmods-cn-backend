package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Claims struct {
	Subject           int64            `json:"-"`
	PublicSubject     string           `json:"sub"`
	SessionID         string           `json:"sid"`
	AuthVersion       int64            `json:"ver"`
	PermissionVersion int64            `json:"-"`
	RBACVersion       int64            `json:"-"`
	Username          string           `json:"username"`
	Email             string           `json:"email"`
	PermissionRules   []PermissionRule `json:"-"`
	IssuedAt          int64            `json:"iat"`
	ExpiresAt         int64            `json:"exp"`
}

// PermissionRule is the resolved authorization rule attached to a request.
// It is deliberately excluded from the signed token payload: middleware
// refreshes it from the database after validating the session so role changes,
// explicit denies, priorities, and expiration are authoritative immediately.
type PermissionRule struct {
	Code     string `json:"code"`
	Allow    bool   `json:"allow"`
	Priority int    `json:"priority"`
	Source   string `json:"source,omitempty"`
}

func SignToken(secret string, claims Claims) (string, error) {
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	headerBytes, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	headerPart := base64.RawURLEncoding.EncodeToString(headerBytes)
	payloadPart := base64.RawURLEncoding.EncodeToString(payloadBytes)
	unsigned := headerPart + "." + payloadPart
	signature := sign(secret, unsigned)
	return unsigned + "." + signature, nil
}

func ParseToken(secret string, token string) (Claims, error) {
	var claims Claims
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims, errors.New("invalid token")
	}
	headerPayload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return claims, errors.New("invalid token header")
	}
	var header struct {
		Algorithm string `json:"alg"`
		Type      string `json:"typ"`
	}
	if err = json.Unmarshal(headerPayload, &header); err != nil || header.Algorithm != "HS256" || header.Type != "JWT" {
		return claims, errors.New("unsupported token header")
	}
	unsigned := parts[0] + "." + parts[1]
	expected := sign(secret, unsigned)
	if !subtleCompare(expected, parts[2]) {
		return claims, errors.New("invalid token signature")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims, err
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return claims, err
	}
	now := time.Now().Unix()
	if claims.ExpiresAt <= now {
		return claims, errors.New("token expired")
	}
	if claims.IssuedAt <= 0 || claims.IssuedAt > now+60 || claims.ExpiresAt <= claims.IssuedAt {
		return claims, errors.New("invalid token lifetime")
	}
	if !validPublicID(claims.PublicSubject) || claims.SessionID == "" || claims.AuthVersion <= 0 {
		return claims, errors.New("invalid token claims")
	}
	return claims, nil
}

func sign(secret string, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func subtleCompare(a string, b string) bool {
	return hmac.Equal([]byte(a), []byte(b))
}

func NewClaims(publicUserID string, username string, email string, authVersion int64, ttl time.Duration) (Claims, error) {
	sessionBytes := make([]byte, 32)
	if _, err := rand.Read(sessionBytes); err != nil {
		return Claims{}, err
	}
	now := time.Now()
	return Claims{
		PublicSubject: publicUserID,
		SessionID:     base64.RawURLEncoding.EncodeToString(sessionBytes),
		AuthVersion:   authVersion,
		Username:      username,
		Email:         email,
		IssuedAt:      now.Unix(),
		ExpiresAt:     now.Add(ttl).Unix(),
	}, nil
}

func SessionFingerprint(sessionID string) []byte {
	sum := sha256.Sum256([]byte(sessionID))
	return sum[:]
}

func BearerToken(header string) (string, error) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", fmt.Errorf("missing bearer token")
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if token == "" {
		return "", fmt.Errorf("empty bearer token")
	}
	return token, nil
}

func validPublicID(value string) bool {
	if len(value) != 9 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9') {
			return false
		}
	}
	return true
}
