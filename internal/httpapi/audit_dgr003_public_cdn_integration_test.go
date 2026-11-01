package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// This opt-in test exercises the unchanged production downloader policy and
// transport against public GitHub release assets. No credentials or provider
// fixture trust are configured, and signed redirect queries are never logged.
func TestAuditDGR003OfficialPublicGitHubCDNChecksumIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_PUBLIC_CDN") != "1" {
		t.Skip("opt-in public GitHub CDN network acceptance")
	}
	dir := os.Getenv("MCMODS_AUDIT_PUBLIC_CDN_DIR")
	if !filepath.IsAbs(dir) {
		t.Fatal("new absolute owned evidence directory required")
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	head, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	status, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil || strings.TrimSpace(string(status)) != "" {
		t.Fatal("clean source required")
	}
	const base = "https://github.com/nats-io/nats-server/releases/download/v2.10.22/"
	const filename = "nats-server-v2.10.22-linux-amd64.tar.gz"
	facts := map[string]any{"recordedAt": time.Now().UTC().Format(time.RFC3339Nano), "sourceCommit": strings.TrimSpace(string(head)), "status": "PENDING", "scope": "Real public GitHub release/CDN using original production client DNS/IP, TLS and redirect policy; no API credentials or TLS/proxy exception. Not private-source or provider Worker acceptance."}
	var attempts []map[string]any
	t.Cleanup(func() {
		facts["attempts"] = attempts
		raw, e := json.MarshalIndent(facts, "", "  ")
		if e != nil {
			t.Error(e)
			return
		}
		f, e := os.OpenFile(filepath.Join(dir, "public-cdn-result.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			t.Error(e)
			return
		}
		_, e = f.Write(append(raw, '\n'))
		if e != nil {
			t.Error(e)
		}
		if e = f.Close(); e != nil {
			t.Error(e)
		}
	})
	fetch := func(name string, budget int64) ([]byte, bool) {
		t.Helper()
		rawURL := base + name
		attempt := map[string]any{"originalURL": rawURL, "byteLimit": budget}
		attempts = append(attempts, attempt)
		client, e := newProviderDownloadHTTPClient(30*time.Second, rawURL)
		if e != nil {
			attempt["error"] = auditPublicCDNError(e)
			return nil, false
		}
		transport, ok := client.Transport.(*http.Transport)
		if !ok || transport.Proxy != nil || (transport.TLSClientConfig != nil && transport.TLSClientConfig.InsecureSkipVerify) {
			t.Fatal("production safe transport changed")
		}
		originalRedirect := client.CheckRedirect
		var hops []map[string]any
		client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
			e := originalRedirect(next, via)
			hop := map[string]any{"host": next.URL.Hostname(), "path": next.URL.EscapedPath(), "queryPresentNotRetained": next.URL.RawQuery != "", "policyAllowed": e == nil}
			if next.Response != nil {
				hop["status"] = next.Response.StatusCode
			}
			hops = append(hops, hop)
			if next.Header.Get("Authorization") != "" || next.Header.Get("Proxy-Authorization") != "" || next.Header.Get("x-api-key") != "" || next.Header.Get("Cookie") != "" {
				return fmt.Errorf("unexpected credentials on public redirect")
			}
			return e
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		r, e := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if e != nil {
			attempt["error"] = auditPublicCDNError(e)
			return nil, false
		}
		response, e := client.Do(r)
		attempt["redirects"] = hops
		if e != nil {
			attempt["error"] = auditPublicCDNError(e)
			return nil, false
		}
		defer response.Body.Close()
		attempt["status"] = response.StatusCode
		attempt["finalHost"] = response.Request.URL.Hostname()
		attempt["finalPath"] = response.Request.URL.EscapedPath()
		attempt["finalQueryPresentNotRetained"] = response.Request.URL.RawQuery != ""
		if response.StatusCode != 200 {
			attempt["error"] = "public release returned non-200"
			return nil, false
		}
		if response.ContentLength > budget {
			attempt["error"] = "public asset exceeds explicit byte budget"
			return nil, false
		}
		body, e := io.ReadAll(io.LimitReader(response.Body, budget+1))
		if e != nil {
			attempt["error"] = auditPublicCDNError(e)
			return nil, false
		}
		if int64(len(body)) > budget {
			attempt["error"] = "public asset exceeds explicit byte budget"
			return nil, false
		}
		hash := sha256.Sum256(body)
		attempt["bytes"] = len(body)
		attempt["sha256"] = hex.EncodeToString(hash[:])
		return body, true
	}
	archive, ok := fetch(filename, 20<<20)
	if !ok {
		facts["status"] = "BLOCKED_ENV_OR_UPSTREAM"
		t.Fatal("official public artifact unavailable through unchanged production downloader; sanitized evidence retained")
	}
	checksums, ok := fetch("SHA256SUMS", 256<<10)
	if !ok {
		facts["status"] = "BLOCKED_ENV_OR_UPSTREAM"
		t.Fatal("official public checksum unavailable through unchanged production downloader; sanitized evidence retained")
	}
	want := ""
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == filename {
			want = fields[0]
		}
	}
	actual := sha256.Sum256(archive)
	if len(want) != 64 || want != hex.EncodeToString(actual[:]) {
		facts["status"] = "FAIL_CHECKSUM"
		t.Fatal("official checksum does not match downloaded bytes")
	}
	if err = os.WriteFile(filepath.Join(dir, filename), archive, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "SHA256SUMS"), checksums, 0600); err != nil {
		t.Fatal(err)
	}
	facts["status"] = "PASS_REAL_PUBLIC_CDN_OFFICIAL_CHECKSUM"
	t.Log("DGR003_PUBLIC official fixed release artifact + official checksum downloaded with production safe client; real redirect/CDN bytes match SHA; no credentials, query output, transport replacement or relaxed policy")
}

func auditPublicCDNError(err error) string {
	for {
		var u *url.Error
		if errors.As(err, &u) {
			err = u.Err
			continue
		}
		break
	}
	var n *net.OpError
	if errors.As(err, &n) {
		return n.Op + " " + n.Net + ": " + auditPublicCDNError(n.Err)
	}
	// Provider URLs may carry signed queries. Retain the underlying reason but
	// never record request/redirect URLs outside the explicit queryless fields.
	return regexp.MustCompile(`https?://[^\s"']+`).ReplaceAllString(err.Error(), "<URL omitted>")
}
