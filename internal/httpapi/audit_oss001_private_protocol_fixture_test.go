package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/signer"
)

// This owned protocol fixture validates actual SDK V4 request signatures and
// wall-clock query expiration. It is deliberately not a real OSS/CDN claim.
// It never logs authorization, credentials, query values or signature strings.
type auditOSS001PrivateProtocol struct {
	cfg          ossConfigPayload
	mu           sync.Mutex
	observations []map[string]any
}

func auditOSS001HMAC(key []byte, value string) []byte {
	h := hmac.New(sha256.New, key)
	io.WriteString(h, value)
	return h.Sum(nil)
}
func auditOSS001EscapePath(value string) string {
	var out strings.Builder
	for _, b := range []byte(value) {
		if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || strings.ContainsRune("-_.~/", rune(b)) {
			out.WriteByte(b)
		} else {
			fmt.Fprintf(&out, "%%%02X", b)
		}
	}
	return out.String()
}
func auditOSS001DefaultSignedHeader(value string) bool {
	return strings.HasPrefix(value, "x-oss-") || value == "content-type" || value == "content-md5"
}
func (p *auditOSS001PrivateProtocol) authorize(r *http.Request) (bool, string) {
	key := strings.TrimPrefix(r.URL.Path, "/")
	key = strings.TrimPrefix(key, p.cfg.Bucket+"/")
	if key == "" {
		return false, "empty_key"
	}
	if authorization := r.Header.Get("Authorization"); authorization != "" {
		if !strings.HasPrefix(authorization, "OSS4-HMAC-SHA256 ") {
			return false, "unsupported_header_auth"
		}
		date, err := time.Parse("20060102T150405Z", r.Header.Get("x-oss-date"))
		if err != nil || time.Since(date) > 5*time.Minute || time.Until(date) > time.Minute {
			return false, "header_time"
		}
		var extra []string
		for _, part := range strings.Split(strings.TrimPrefix(authorization, "OSS4-HMAC-SHA256 "), ",") {
			if strings.HasPrefix(part, "AdditionalHeaders=") {
				extra = strings.Split(strings.TrimPrefix(part, "AdditionalHeaders="), ";")
			}
		}
		clone := r.Clone(r.Context())
		copied := *r.URL
		clone.URL = &copied
		clone.Header = r.Header.Clone()
		clone.Header.Del("Authorization")
		product := "oss"
		cred := credentials.Credentials{AccessKeyID: p.cfg.AccessKeyID, AccessKeySecret: p.cfg.AccessKeySecret, SecurityToken: p.cfg.SecurityToken}
		ctx := signer.SigningContext{Product: &product, Region: &p.cfg.Region, Bucket: &p.cfg.Bucket, Key: &key, Request: clone, Credentials: &cred, Time: date, AdditionalHeaders: extra}
		if err = (&signer.SignerV4{}).Sign(r.Context(), &ctx); err != nil {
			return false, "header_signer"
		}
		if subtle.ConstantTimeCompare([]byte(authorization), []byte(clone.Header.Get("Authorization"))) != 1 {
			return false, "header_signature"
		}
		return true, "verified_sdk_header"
	}
	query := r.URL.Query()
	if query.Get("x-oss-signature-version") != "OSS4-HMAC-SHA256" {
		return false, "unsigned_or_unsupported"
	}
	for _, name := range []string{"x-oss-date", "x-oss-expires", "x-oss-credential", "x-oss-signature", "x-oss-signature-version"} {
		if len(query[name]) != 1 {
			return false, "duplicate_or_missing_query"
		}
	}
	date, err := time.Parse("20060102T150405Z", query.Get("x-oss-date"))
	if err != nil || time.Until(date) > time.Minute {
		return false, "query_time"
	}
	expires, err := strconv.ParseInt(query.Get("x-oss-expires"), 10, 64)
	if err != nil || expires <= 0 || expires > 3600 {
		return false, "query_expiry_range"
	}
	if !time.Now().Before(date.Add(time.Duration(expires) * time.Second)) {
		return false, "expired_query"
	}
	scope := date.Format("20060102") + "/" + p.cfg.Region + "/oss/aliyun_v4_request"
	if query.Get("x-oss-credential") != p.cfg.AccessKeyID+"/"+scope {
		return false, "query_credential_scope"
	}
	if query.Get("x-oss-security-token") != p.cfg.SecurityToken {
		return false, "query_security_token"
	}
	var encoded []string
	for _, part := range strings.Split(strings.ReplaceAll(r.URL.RawQuery, "+", "%20"), "&") {
		name, _, _ := strings.Cut(part, "=")
		if name != "x-oss-signature" {
			encoded = append(encoded, part)
		}
	}
	sort.Strings(encoded)
	extra := []string{}
	if value := query.Get("x-oss-additional-headers"); value != "" {
		extra = strings.Split(value, ";")
		sort.Strings(extra)
	}
	selected := map[string]bool{}
	for _, name := range extra {
		selected[strings.ToLower(name)] = true
	}
	var names []string
	for name := range r.Header {
		name = strings.ToLower(name)
		if auditOSS001DefaultSignedHeader(name) || selected[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var canonicalHeaders strings.Builder
	for _, name := range names {
		values := append([]string(nil), r.Header.Values(name)...)
		for i := range values {
			values[i] = strings.TrimSpace(values[i])
		}
		canonicalHeaders.WriteString(name + ":" + strings.Join(values, ",") + "\n")
	}
	payload := r.Header.Get("x-oss-content-sha256")
	if payload == "" {
		payload = "UNSIGNED-PAYLOAD"
	}
	canonical := r.Method + "\n" + auditOSS001EscapePath("/"+p.cfg.Bucket+"/"+key) + "\n" + strings.Join(encoded, "&") + "\n" + canonicalHeaders.String() + "\n" + strings.Join(extra, ";") + "\n" + payload
	digest := sha256.Sum256([]byte(canonical))
	toSign := "OSS4-HMAC-SHA256\n" + date.Format("20060102T150405Z") + "\n" + scope + "\n" + hex.EncodeToString(digest[:])
	signingKey := auditOSS001HMAC([]byte("aliyun_v4"+p.cfg.AccessKeySecret), date.Format("20060102"))
	signingKey = auditOSS001HMAC(signingKey, p.cfg.Region)
	signingKey = auditOSS001HMAC(signingKey, "oss")
	signingKey = auditOSS001HMAC(signingKey, "aliyun_v4_request")
	expected := hex.EncodeToString(auditOSS001HMAC(signingKey, toSign))
	if subtle.ConstantTimeCompare([]byte(expected), []byte(query.Get("x-oss-signature"))) != 1 {
		return false, "query_signature"
	}
	return true, "verified_sdk_query"
}
func (p *auditOSS001PrivateProtocol) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		allowed, reason := p.authorize(r)
		p.mu.Lock()
		p.observations = append(p.observations, map[string]any{"method": r.Method, "path": r.URL.Path, "queryPresent": r.URL.RawQuery != "", "authorized": allowed, "reason": reason, "queryValuesRecorded": false, "at": time.Now().UTC().Format(time.RFC3339Nano)})
		p.mu.Unlock()
		w.Header().Set("Cache-Control", "private, no-store")
		if !allowed {
			http.Error(w, "Private owned OSS request rejected", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (p *auditOSS001PrivateProtocol) snapshot() []map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]map[string]any(nil), p.observations...)
}

func TestAuditOSS001PrivateProtocolVerifiesActualSDKSignaturesAndExpiry(t *testing.T) {
	p := &auditOSS001PrivateProtocol{cfg: ossConfigPayload{Bucket: "audit-owned", Region: "cn-test", AccessKeyID: "owned-protocol-key", AccessKeySecret: "synthetic-protocol-secret"}}
	server := httptest.NewServer(p.handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200); w.Write([]byte("owned")) })))
	defer server.Close()
	p.cfg.Endpoint = server.URL
	client := newOSSClient(p.cfg, server.URL, true)
	key := "mcmods/user/42/files/authors/author/avatar.png"
	signed, err := client.Presign(context.Background(), &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(p.cfg.Bucket), Key: &key}, aliyunoss.PresignExpires(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	require := func(raw string, want int) {
		t.Helper()
		response, err := server.Client().Get(raw)
		if err != nil {
			t.Fatal("owned protocol request failed")
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("protocol status=%d want=%d observations=%v", response.StatusCode, want, p.snapshot())
		}
	}
	require(signed.URL, 200)
	parsed, err := url.Parse(signed.URL)
	if err != nil {
		t.Fatal(err)
	}
	unsigned := *parsed
	unsigned.RawQuery = ""
	require(unsigned.String(), 403)
	tampered := *parsed
	values := tampered.Query()
	values.Set("x-oss-signature", strings.Repeat("0", 64))
	tampered.RawQuery = values.Encode()
	require(tampered.String(), 403)
	wrong := *parsed
	wrong.Path += ".other"
	require(wrong.String(), 403)
	headerRequest, err := http.NewRequest(http.MethodGet, unsigned.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	product := "oss"
	cred := credentials.Credentials{AccessKeyID: p.cfg.AccessKeyID, AccessKeySecret: p.cfg.AccessKeySecret}
	ctx := signer.SigningContext{Product: &product, Region: &p.cfg.Region, Bucket: &p.cfg.Bucket, Key: &key, Request: headerRequest, Credentials: &cred}
	if err = (&signer.SignerV4{}).Sign(context.Background(), &ctx); err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(headerRequest)
	if err != nil {
		t.Fatal("owned header request failed")
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("official SDK header not verified", p.snapshot())
	}
	when, err := time.Parse("20060102T150405Z", parsed.Query().Get("x-oss-date"))
	if err != nil {
		t.Fatal(err)
	}
	duration, err := strconv.Atoi(parsed.Query().Get("x-oss-expires"))
	if err != nil {
		t.Fatal(err)
	}
	if delay := time.Until(when.Add(time.Duration(duration)*time.Second)) + 30*time.Millisecond; delay > 0 {
		time.Sleep(delay)
	}
	require(signed.URL, 403)
}
