package httpapi

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1" // #nosec G505 -- Mojang's Yggdrasil protocol mandates SHA-1 signatures for texture properties.
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

const (
	yggdrasilRequestMaxBytes = 1 << 20
	yggdrasilTokenBytes      = 32
	yggdrasilTokenInputMax   = 512
	yggdrasilIdentifierMax   = 254
	yggdrasilPasswordMax     = 256
	yggdrasilUserAgentMax    = 512
	yggdrasilIssuesPerMinute = 60
	yggdrasilLoginPermission = "skin.launcher.login"
	yggdrasilPasswordWorkers = 8
)

var (
	yggdrasilUnsignedUUIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	yggdrasilTextureHashPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	yggdrasilPasswordSlots       = make(chan struct{}, yggdrasilPasswordWorkers)
)

type yggdrasilService struct {
	privateKey     *rsa.PrivateKey
	publicKeyPEM   string
	disabledReason error
	trustedProxies []*net.IPNet
}

type yggdrasilErrorResponse struct {
	Error        string `json:"error"`
	ErrorMessage string `json:"errorMessage"`
	Cause        string `json:"cause,omitempty"`
}

type yggdrasilProperty struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Signature string `json:"signature,omitempty"`
}

type yggdrasilProfile struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Properties []yggdrasilProperty `json:"properties,omitempty"`
}

type yggdrasilUser struct {
	ID         string              `json:"id"`
	Properties []yggdrasilProperty `json:"properties"`
}

func newYggdrasilService(cfg config.Config) *yggdrasilService {
	if !cfg.Yggdrasil.Enabled {
		return &yggdrasilService{disabledReason: errors.New("yggdrasil is disabled by configuration")}
	}
	production := strings.EqualFold(strings.TrimSpace(cfg.Env), "production")
	if err := validateYggdrasilEndpoint(cfg.Yggdrasil.PublicBaseURL, "public API", production); err != nil {
		return &yggdrasilService{disabledReason: err}
	}
	if err := validateYggdrasilEndpoint(cfg.Yggdrasil.TextureBaseURL, "texture", production); err != nil {
		return &yggdrasilService{disabledReason: err}
	}
	if err := validateYggdrasilLimits(cfg.Yggdrasil.TokenTTL, cfg.Yggdrasil.JoinTTL, cfg.Yggdrasil.MaxTokens, cfg.Yggdrasil.TextureMaxBytes); err != nil {
		return &yggdrasilService{disabledReason: err}
	}
	trustedProxies, err := parseYggdrasilTrustedProxies(cfg.Yggdrasil.TrustedProxyCIDRs)
	if err != nil {
		return &yggdrasilService{disabledReason: err}
	}
	encoded := strings.TrimSpace(cfg.Yggdrasil.PrivateKeyBase64)
	if encoded == "" {
		if production {
			return &yggdrasilService{disabledReason: errors.New("YGGDRASIL_PRIVATE_KEY_BASE64 is required in production")}
		}
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return &yggdrasilService{disabledReason: fmt.Errorf("generate development signing key: %w", err)}
		}
		log.Print("WARNING: Yggdrasil is using a temporary development RSA key; launcher profile signatures will change after restart")
		service := yggdrasilServiceFromPrivateKey(key)
		service.trustedProxies = trustedProxies
		return service
	}

	key, err := parseYggdrasilPrivateKey(encoded)
	if err != nil {
		return &yggdrasilService{disabledReason: err}
	}
	service := yggdrasilServiceFromPrivateKey(key)
	service.trustedProxies = trustedProxies
	return service
}

func validateYggdrasilEndpoint(raw, label string, production bool) error {
	value := strings.TrimSpace(raw)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("yggdrasil %s URL must be an absolute HTTP(S) URL without credentials, query, or fragment", label)
	}
	if !production {
		return nil
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("yggdrasil %s URL must use HTTPS in production", label)
	}
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	ip := net.ParseIP(hostname)
	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") ||
		(ip != nil && (ip.IsLoopback() || ip.IsUnspecified() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast())) {
		return fmt.Errorf("yggdrasil %s URL cannot use a local or private address in production", label)
	}
	return nil
}

func validateYggdrasilLimits(tokenTTL, joinTTL time.Duration, maxTokens int, textureMaxBytes int64) error {
	if tokenTTL < time.Minute || tokenTTL > 30*24*time.Hour {
		return errors.New("yggdrasil token TTL must be between 1 minute and 30 days")
	}
	if joinTTL < 5*time.Second || joinTTL > 5*time.Minute {
		return errors.New("yggdrasil join TTL must be between 5 seconds and 5 minutes")
	}
	if maxTokens < 1 || maxTokens > 100 {
		return errors.New("yggdrasil maximum token count must be between 1 and 100")
	}
	if textureMaxBytes < 1024 || textureMaxBytes > maxMinecraftTextureUploadBytes {
		return fmt.Errorf("yggdrasil texture upload limit must be between 1024 and %d bytes", maxMinecraftTextureUploadBytes)
	}
	return nil
}

func parseYggdrasilTrustedProxies(values []string) ([]*net.IPNet, error) {
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
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return nil, fmt.Errorf("invalid Yggdrasil trusted proxy CIDR %q", value)
		}
		result = append(result, network)
	}
	return result, nil
}

func yggdrasilServiceFromPrivateKey(key *rsa.PrivateKey) *yggdrasilService {
	if key == nil {
		return &yggdrasilService{disabledReason: errors.New("nil Yggdrasil private key")}
	}
	if key.N.BitLen() < 2048 {
		return &yggdrasilService{disabledReason: errors.New("yggdrasil RSA private key must be at least 2048 bits")}
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return &yggdrasilService{disabledReason: fmt.Errorf("marshal Yggdrasil public key: %w", err)}
	}
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})
	return &yggdrasilService{privateKey: key, publicKeyPEM: string(publicPEM)}
}

func parseYggdrasilPrivateKey(encoded string) (*rsa.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(encoded)
	}
	if err != nil {
		return nil, errors.New("decode Yggdrasil private key base64")
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("decode Yggdrasil private key PEM")
	}

	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		var parsed any
		parsed, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		if err == nil {
			var ok bool
			key, ok = parsed.(*rsa.PrivateKey)
			if !ok {
				err = errors.New("yggdrasil private key is not RSA")
			}
		}
	default:
		err = fmt.Errorf("unsupported Yggdrasil PEM block %q", block.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("parse Yggdrasil private key: %w", err)
	}
	if err := key.Validate(); err != nil {
		return nil, fmt.Errorf("validate Yggdrasil private key: %w", err)
	}
	return key, nil
}

func (s *Server) yggdrasilRoutes() {
	handle := func(pattern string, handler http.HandlerFunc) {
		s.mux.HandleFunc(pattern, s.requireYggdrasilService(handler))
	}
	handle("GET /api/yggdrasil/{$}", s.yggdrasilMetadata)
	handle("POST /api/yggdrasil/authserver/authenticate", s.yggdrasilAuthenticate)
	handle("POST /api/yggdrasil/authserver/refresh", s.yggdrasilRefresh)
	handle("POST /api/yggdrasil/authserver/validate", s.yggdrasilValidate)
	handle("POST /api/yggdrasil/authserver/invalidate", s.yggdrasilInvalidate)
	handle("POST /api/yggdrasil/authserver/signout", s.yggdrasilSignout)
	handle("POST /api/yggdrasil/sessionserver/session/minecraft/join", s.yggdrasilJoin)
	handle("GET /api/yggdrasil/sessionserver/session/minecraft/hasJoined", s.yggdrasilHasJoined)
	handle("GET /api/yggdrasil/sessionserver/session/minecraft/profile/{uuid}", s.yggdrasilProfileByUUID)
	handle("POST /api/yggdrasil/api/profiles/minecraft", s.yggdrasilProfilesByName)
	handle("GET /api/yggdrasil/users/profiles/minecraft/{username}", s.yggdrasilProfileByName)
	handle("PUT /api/yggdrasil/api/user/profile/{uuid}/{textureType}", s.yggdrasilSetTexture)
	handle("DELETE /api/yggdrasil/api/user/profile/{uuid}/{textureType}", s.yggdrasilDeleteTexture)
	s.mux.HandleFunc("GET /api/yggdrasil/textures/{hash}", s.yggdrasilTextureContent)
}

func (s *Server) requireYggdrasilService(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, service := s.yggdrasilRuntimeSnapshot()
		if service == nil || service.privateKey == nil || service.disabledReason != nil {
			writeYggdrasilError(w, http.StatusServiceUnavailable, "ServiceUnavailableException", "Authentication service is unavailable.", "")
			return
		}
		next(w, r)
	}
}

func (s *Server) yggdrasilALI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg, service := s.yggdrasilRuntimeSnapshot()
		if (r.URL.Path == "/" || r.URL.Path == "/api/yggdrasil" || r.URL.Path == "/api/yggdrasil/") &&
			service != nil && service.privateKey != nil && service.disabledReason == nil {
			w.Header().Set("X-Authlib-Injector-API-Location", normalizedYggdrasilBaseURL(cfg.PublicBaseURL))
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) yggdrasilMetadata(w http.ResponseWriter, _ *http.Request) {
	cfg, service := s.yggdrasilRuntimeSnapshot()
	textureBase := normalizedYggdrasilBaseURL(cfg.TextureBaseURL)
	skinDomains := make([]string, 0, 1)
	if parsed, err := url.Parse(textureBase); err == nil && parsed.Hostname() != "" {
		skinDomains = append(skinDomains, parsed.Hostname())
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeYggdrasilJSON(w, http.StatusOK, map[string]any{
		"meta": map[string]any{
			"serverName":            defaultString(strings.TrimSpace(cfg.ServerName), "Mcmods-cn"),
			"implementationName":    "mcmods-cn-yggdrasil",
			"implementationVersion": "1",
			"links": map[string]string{
				"homepage": s.cfg.FrontendOrigin,
				"register": strings.TrimRight(s.cfg.FrontendOrigin, "/") + "/login",
			},
			"feature.non_email_login":             true,
			"feature.legacy_skin_api":             false,
			"feature.enable_profile_key":          false,
			"feature.username_check":              true,
			"feature.enable_mojang_anti_features": false,
		},
		"skinDomains":        skinDomains,
		"signaturePublickey": service.publicKeyPEM,
	})
}

func normalizedYggdrasilBaseURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	return strings.TrimRight(value, "/") + "/"
}

func (service *yggdrasilService) signPropertyValue(value string) (string, error) {
	if service == nil || service.privateKey == nil {
		return "", errors.New("yggdrasil signing key unavailable")
	}
	digest := sha1.Sum([]byte(value)) // #nosec G401 -- protocol compatibility; this is a signature digest, not password hashing.
	signature, err := rsa.SignPKCS1v15(rand.Reader, service.privateKey, crypto.SHA1, digest[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

func newYggdrasilToken() (plain string, hash string, err error) {
	raw := make([]byte, yggdrasilTokenBytes)
	if _, err = rand.Read(raw); err != nil {
		return "", "", err
	}
	plain = base64.RawURLEncoding.EncodeToString(raw)
	return plain, hashYggdrasilToken(plain), nil
}

func newYggdrasilClientToken() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func hashYggdrasilToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func unsignedYggdrasilUUID(value string) (string, bool) {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", ""))
	return normalized, yggdrasilUnsignedUUIDPattern.MatchString(normalized)
}

func databaseYggdrasilUUID(value string) (string, bool) {
	unsigned, ok := unsignedYggdrasilUUID(value)
	if !ok {
		return "", false
	}
	return unsigned[0:8] + "-" + unsigned[8:12] + "-" + unsigned[12:16] + "-" + unsigned[16:20] + "-" + unsigned[20:32], true
}

func writeYggdrasilJSON(w http.ResponseWriter, status int, value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "authentication service is unavailable", http.StatusServiceUnavailable)
		return
	}
	payload = append(payload, '\n')
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if _, err = w.Write(payload); err != nil {
		log.Printf("write Yggdrasil JSON response: %v", err)
	}
}

func writeYggdrasilError(w http.ResponseWriter, status int, code, message, cause string) {
	writeYggdrasilJSON(w, status, yggdrasilErrorResponse{Error: code, ErrorMessage: message, Cause: cause})
}

func writeYggdrasilNoContent(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusNoContent)
}

func decodeYggdrasilJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, yggdrasilRequestMaxBytes)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func yggdrasilBearerToken(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(value) < 8 || !strings.EqualFold(value[:7], "Bearer ") {
		return ""
	}
	token := strings.TrimSpace(value[7:])
	if len(token) > yggdrasilTokenInputMax {
		return ""
	}
	return token
}

func (s *Server) yggdrasilUserAllowed(ctx context.Context, userID int64) bool {
	return userID > 0 && s.userHasPermission(ctx, userID, yggdrasilLoginPermission)
}

func (s *Server) yggdrasilClientLocation(r *http.Request) clientLocation {
	peer := normalizeIPAddress(remoteIP(r.RemoteAddr))
	parsedPeer := net.ParseIP(peer)
	_, service := s.yggdrasilRuntimeSnapshot()
	if service == nil || parsedPeer == nil || !service.isTrustedProxy(parsedPeer) {
		return clientLocation{IP: peer}
	}
	return requestClientLocationFromProxy(r, peer, service.trustedProxies)
}

func (service *yggdrasilService) isTrustedProxy(ip net.IP) bool {
	if service == nil || ip == nil {
		return false
	}
	for _, network := range service.trustedProxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func verifyYggdrasilPassword(ctx context.Context, password, encoded string) (bool, bool) {
	select {
	case yggdrasilPasswordSlots <- struct{}{}:
		defer func() { <-yggdrasilPasswordSlots }()
	case <-ctx.Done():
		return false, false
	default:
		return false, false
	}
	return security.VerifyPassword(password, encoded), true
}

func hashYggdrasilPassword(ctx context.Context, password string) (string, bool, error) {
	select {
	case yggdrasilPasswordSlots <- struct{}{}:
		defer func() { <-yggdrasilPasswordSlots }()
	case <-ctx.Done():
		return "", false, ctx.Err()
	default:
		return "", false, nil
	}
	hash, err := security.HashPassword(password)
	return hash, true, err
}

func boundedYggdrasilUserAgent(r *http.Request) string {
	value := strings.ToValidUTF8(r.UserAgent(), "")
	if len(value) <= yggdrasilUserAgentMax {
		return value
	}
	end := yggdrasilUserAgentMax
	for end > 0 && value[end]&0xc0 == 0x80 {
		end--
	}
	return value[:end]
}
