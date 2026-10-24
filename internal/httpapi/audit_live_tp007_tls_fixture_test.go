package httpapi

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// No product behavior is mocked. Only this task-owned OSS protocol service and
// explicitly published synthetic ancestors are prerequisites for the external
// real Next development/production browser. The TLS gateway must preserve exact
// response bytes and security headers. Neither fixture exit nor TLS readiness
// alone is an image/browser/production PASS.
func TestAuditLiveTP007TLSFixture(t *testing.T) {
	if os.Getenv("MCMODS_RUN_LIVE_TP007_TLS") != "1" {
		t.Skip("opt-in real HTTPS image/browser fixture")
	}
	directory := os.Getenv("MCMODS_AUDIT_TP007_TLS_DIR")
	if !filepath.IsAbs(directory) || os.Getenv("SSL_CERT_FILE") == "" {
		t.Fatal("fresh absolute owned directory and explicit trusted test CA are required")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rawTop, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal(err)
	}
	revisions := map[string]any{}
	for name, path := range map[string]string{"mcmods-cn-backend": strings.TrimSpace(string(rawTop)), "mcmods-cn-frontend": "/workspace/mcmods-cn-frontend", "mcmods_exporter": "/workspace/mcmods_exporter"} {
		rev := auditJourneyRevision(path)
		if rev["error"] != nil || rev["dirty"] != false {
			t.Fatal("clean source required:", name)
		}
		revisions[name] = rev
	}
	if pin := os.Getenv("MCMODS_AUDIT_EXPECT_BACKEND_COMMIT"); pin == "" || revisions["mcmods-cn-backend"].(map[string]any)["commit"] != pin {
		t.Fatal("full actual backend pin required")
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	owner, err := os.OpenFile(filepath.Join(directory, "OWNER"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(owner, "mcmods-live-tp007-tls pid=%d\n", os.Getpid())
	owner.Close()
	const publicOrigin = "https://127.0.0.1:3443"
	t.Setenv("FRONTEND_ORIGIN", publicOrigin)
	t.Setenv("MCMODS_AUDIT_PUBLIC_OSS_ORIGIN", publicOrigin)
	unlockDDL := auditGlobalCatalogPGLock(t)
	t.Cleanup(func() { unlockDDL() })
	f := newAuditFixGRFixture(t, 45*time.Minute)
	unlockDDL()
	t.Cleanup(func() { unlockDDL = auditGlobalCatalogPGLock(t) })
	cfg := f.server.ossConfigFromSettings(f.ctx)
	cfg.Endpoint = publicOrigin
	cfg.PublicEndpoint = publicOrigin
	sealed, err := f.server.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `update system_settings set value=$1::jsonb where key='oss.aliyun'`, sealed); err != nil {
		t.Fatal(err)
	}
	runtimePath := filepath.Join(directory, "runtime.json")
	readyPath := filepath.Join(directory, "TLS_READY")
	stopPath := filepath.Join(directory, "STOP")
	runtime := map[string]any{"schemaVersion": 1, "pid": os.Getpid(), "state": "BOOTSTRAP_WAIT_TLS_PROXY", "apiOrigin": f.origin.URL, "frontendOrigin": publicOrigin, "internalOSSOrigin": f.provider.URL, "ossObjectPrefix": "/mcmods/project/mods/" + f.modCode + "/icons/item/original/", "tlsReadyPath": readyPath, "stopPath": stopPath, "repoRevisions": revisions, "antiAbuseEnabled": f.server.cfg.AntiAbuse.Enabled, "privateBucketPolicy": "presigned downloads; no public bucket or permission bypass", "CAEnvironment": "SSL_CERT_FILE explicitly trusts task-owned CA; no insecure TLS client", "scope": "Actual authorized upload and actual API/PG/controlled OSS; explicit synthetic approved ancestors. External browser must test real development/production images and empty fallback. Fixture exit is not PASS."}
	auditJourneyWriteJSON(t, runtimePath, runtime)
	timer := time.NewTicker(200 * time.Millisecond)
	defer timer.Stop()
	waiting := true
	for waiting {
		select {
		case <-f.ctx.Done():
			t.Fatal("TLS gateway bootstrap expired")
		case <-timer.C:
			if _, err = os.Stat(readyPath); err == nil {
				waiting = false
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
	}
	// A flag cannot substitute for a real verified TLS connection. Probe the
	// actual private API through the exact configured gateway before SDK upload.
	probe, err := http.NewRequestWithContext(f.ctx, http.MethodGet, publicOrigin+"/api/v1/modpacks/audit-tls-probe-missing", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(probe)
	if err != nil {
		t.Fatal("verified TLS probe:", err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.TLS == nil || !response.TLS.HandshakeComplete || len(response.TLS.VerifiedChains) == 0 || response.StatusCode != 404 {
		t.Fatal("TLS gateway must serve real verified API404")
	}
	runtime["verifiedTLSProbe"] = map[string]any{"status": response.StatusCode, "version": response.TLS.Version, "verifiedCertificateChains": len(response.TLS.VerifiedChains), "handshakeComplete": true}
	runtime["tp007"] = auditGlobalCatalogTP007Ancestors(t, f, directory)
	runtime["state"] = "READY"
	runtime["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	auditJourneyWriteJSON(t, runtimePath, runtime)
	t.Log("actual TP007 HTTPS fixture READY; private synthetic signing keys are not written")
	for {
		select {
		case <-f.ctx.Done():
			runtime["state"] = "EXPIRED"
			auditJourneyWriteJSON(t, runtimePath, runtime)
			t.Fatal("owned TLS fixture expired before STOP")
		case <-timer.C:
			if _, err = os.Stat(stopPath); err == nil {
				runtime["state"] = "STOPPED"
				runtime["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
				auditJourneyWriteJSON(t, runtimePath, runtime)
				return
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
	}
}
