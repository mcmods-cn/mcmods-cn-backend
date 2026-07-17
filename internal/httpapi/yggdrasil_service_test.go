package httpapi

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/config"
)

func TestUnsignedYggdrasilUUID(t *testing.T) {
	unsigned, ok := unsignedYggdrasilUUID("550E8400-E29B-41D4-A716-446655440000")
	if !ok || unsigned != "550e8400e29b41d4a716446655440000" {
		t.Fatalf("unexpected unsigned UUID: %q, %v", unsigned, ok)
	}
	database, ok := databaseYggdrasilUUID(unsigned)
	if !ok || database != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("unexpected database UUID: %q, %v", database, ok)
	}
	for _, invalid := range []string{"", "550e8400", "z50e8400e29b41d4a716446655440000"} {
		if _, ok := unsignedYggdrasilUUID(invalid); ok {
			t.Fatalf("expected UUID %q to be rejected", invalid)
		}
	}
}

func TestYggdrasilTokenHashAndEntropy(t *testing.T) {
	plain, hash, err := newYggdrasilToken()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(plain)
	if err != nil || len(raw) != yggdrasilTokenBytes {
		t.Fatalf("token is not %d random bytes: len=%d err=%v", yggdrasilTokenBytes, len(raw), err)
	}
	if len(hash) != 64 || hash != hashYggdrasilToken(plain) || strings.Contains(hash, plain) {
		t.Fatalf("unexpected token hash %q", hash)
	}
}

func TestYggdrasilRejectsWeakSigningKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	service := yggdrasilServiceFromPrivateKey(key)
	if service.disabledReason == nil || service.privateKey != nil {
		t.Fatal("1024-bit Yggdrasil signing key was accepted")
	}
}

func TestValidateYggdrasilEndpoint(t *testing.T) {
	valid := []struct {
		url        string
		production bool
	}{
		{url: "http://127.0.0.1:8080/api/yggdrasil/"},
		{url: "https://auth.mcmods.cn/api/yggdrasil/", production: true},
	}
	for _, test := range valid {
		if err := validateYggdrasilEndpoint(test.url, "test", test.production); err != nil {
			t.Fatalf("expected %q to be valid: %v", test.url, err)
		}
	}
	invalid := []struct {
		url        string
		production bool
	}{
		{url: "/api/yggdrasil/"},
		{url: "ftp://auth.mcmods.cn/yggdrasil/"},
		{url: "https://user:secret@auth.mcmods.cn/yggdrasil/"},
		{url: "https://auth.mcmods.cn/yggdrasil/?tenant=1"},
		{url: "http://auth.mcmods.cn/yggdrasil/", production: true},
		{url: "https://localhost/yggdrasil/", production: true},
		{url: "https://127.0.0.1/yggdrasil/", production: true},
	}
	for _, test := range invalid {
		if err := validateYggdrasilEndpoint(test.url, "test", test.production); err == nil {
			t.Fatalf("expected %q (production=%v) to be rejected", test.url, test.production)
		}
	}
}

func TestBoundedYggdrasilUserAgentPreservesUTF8(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("User-Agent", strings.Repeat("a", yggdrasilUserAgentMax-1)+"界")
	got := boundedYggdrasilUserAgent(request)
	if len(got) > yggdrasilUserAgentMax || !strings.HasSuffix(got, "a") {
		t.Fatalf("unexpected bounded user agent length=%d value=%q", len(got), got)
	}
}

func TestYggdrasilPropertySignature(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	service := yggdrasilServiceFromPrivateKey(key)
	value := base64.StdEncoding.EncodeToString([]byte(`{"profileId":"550e8400e29b41d4a716446655440000"}`))
	encodedSignature, err := service.signPropertyValue(value)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := base64.StdEncoding.DecodeString(encodedSignature)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha1.Sum([]byte(value))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA1, digest[:], signature); err != nil {
		t.Fatalf("signature did not verify over exact property value: %v", err)
	}
	decoded, _ := base64.StdEncoding.DecodeString(value)
	wrongDigest := sha1.Sum(decoded)
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA1, wrongDigest[:], signature); err == nil {
		t.Fatal("signature unexpectedly verified over decoded JSON")
	}
}

func TestYggdrasilRawJSONResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeYggdrasilJSON(recorder, http.StatusOK, map[string]string{"accessToken": "secret"})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("content type=%q", got)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["accessToken"] != "secret" || body["data"] != nil {
		t.Fatalf("response was wrapped or malformed: %#v", body)
	}
}

func TestYggdrasilResponseProfileFields(t *testing.T) {
	authenticateJSON, err := json.Marshal(yggdrasilAuthenticationResponse{
		AccessToken:       "access",
		ClientToken:       "client",
		AvailableProfiles: []yggdrasilProfile{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(authenticateJSON), `"availableProfiles":[]`) {
		t.Fatalf("authenticate response must include an empty profile list: %s", authenticateJSON)
	}

	refreshJSON, err := json.Marshal(yggdrasilRefreshResponse{AccessToken: "access", ClientToken: "client"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(refreshJSON), "availableProfiles") {
		t.Fatalf("refresh response must not include availableProfiles: %s", refreshJSON)
	}
}

func TestYggdrasilErrorResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeYggdrasilInvalidToken(recorder)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d", recorder.Code)
	}
	var body yggdrasilErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error != "ForbiddenOperationException" || body.ErrorMessage != "Invalid token." || body.Cause != "" {
		t.Fatalf("unexpected error response: %#v", body)
	}
}

func TestYggdrasilMetadataIsRawAndContainsPublicKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		cfg: config.Config{
			FrontendOrigin: "https://mcmods.cn",
			Yggdrasil: config.YggdrasilConfig{
				PublicBaseURL:  "https://api.mcmods.cn/yggdrasil/",
				TextureBaseURL: "https://textures.mcmods.cn/texture/",
				ServerName:     "Mcmods-cn",
			},
		},
		ygg: yggdrasilServiceFromPrivateKey(key),
	}
	recorder := httptest.NewRecorder()
	server.yggdrasilMetadata(recorder, httptest.NewRequest(http.MethodGet, "/api/yggdrasil/", nil))
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["data"] != nil || !strings.Contains(body["signaturePublickey"].(string), "BEGIN PUBLIC KEY") {
		t.Fatalf("unexpected metadata: %#v", body)
	}
	domains := body["skinDomains"].([]any)
	if len(domains) != 1 || domains[0] != "textures.mcmods.cn" {
		t.Fatalf("unexpected skin domains: %#v", domains)
	}
	links := body["meta"].(map[string]any)["links"].(map[string]any)
	if links["register"] != "https://mcmods.cn/login" {
		t.Fatalf("unexpected registration URL: %#v", links)
	}
}

func TestYggdrasilTextureURL(t *testing.T) {
	hash := strings.Repeat("a", 64)
	url, err := yggdrasilTextureURL("https://textures.mcmods.cn/texture", hash)
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://textures.mcmods.cn/texture/"+hash {
		t.Fatalf("unexpected texture URL %q", url)
	}
	if _, err := yggdrasilTextureURL("https://textures.mcmods.cn/texture/", "../secret"); err == nil {
		t.Fatal("invalid texture hash accepted")
	}
}
