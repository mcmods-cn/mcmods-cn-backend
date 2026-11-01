package httpapi

// Actual ordinary HTTP + full PostgreSQL + the unchanged Worker business body.
// Only private explicit factories connect declared official-origin requests to
// owned certificate-verified TLS peers. No global transport/DNS/hosts mutation,
// disabled TLS, production SSRF exception or real provider key is used.
import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const auditCFWorkerKey = "tp003-tp004-synthetic-local-only-key"
const auditCFWorkerArchiveURL = "https://edge.forgecdn.net/files/owned/cf-pack.zip"

type auditCFWorkerTransport struct {
	archive     bool
	destination *url.URL
	transport   *http.Transport
	mu          sync.Mutex
	requests    []string
	verified    int
	deadline    bool
}

func (transport *auditCFWorkerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	valid := request.Method == http.MethodGet && request.URL.Scheme == "https" && request.URL.User == nil && request.URL.Fragment == "" && request.URL.RawPath == "" && !request.URL.ForceQuery
	if transport.archive {
		valid = valid && request.URL.String() == auditCFWorkerArchiveURL && request.Header.Get("Authorization") == "" && request.Header.Get("x-api-key") == "" && request.Header.Get("Cookie") == ""
	} else {
		valid = valid && request.URL.Host == "api.curseforge.com" && request.Header.Get("x-api-key") == auditCFWorkerKey && request.Header.Get("Authorization") == "" && request.Header.Get("Cookie") == ""
		switch request.URL.Path {
		case "/v1/mods/search":
			valid = valid && request.URL.Query().Encode() == url.Values{"gameId": {"432"}, "classId": {"4471"}, "slug": {"owned-cf-pack"}, "pageSize": {"1"}}.Encode()
		case "/v1/mods/123/description":
			valid = valid && request.URL.RawQuery == ""
		case "/v1/mods/123/files":
			index, err := strconv.Atoi(request.URL.Query().Get("index"))
			valid = valid && err == nil && index >= 0 && index <= 950 && index%50 == 0 && len(request.URL.Query()) == 2 && request.URL.Query().Get("pageSize") == "50"
		default:
			valid = false
		}
	}
	if !valid {
		return nil, errors.New("owned CF transport refused an undeclared request or credential scope")
	}
	if deadline, ok := request.Context().Deadline(); !ok || time.Until(deadline) > 5*time.Second {
		return nil, errors.New("owned CF transport requires the unchanged bounded request context")
	}
	transport.mu.Lock()
	transport.requests = append(transport.requests, request.URL.RequestURI())
	transport.mu.Unlock()
	clone := request.Clone(request.Context())
	// net/http adds the deprecated Cancel channel for an unknown RoundTripper.
	// Forward the modern, already bounded/cancellable context to the cloned
	// request instead; a legacy timer must not mask DeadlineExceeded as the
	// unrelated request-canceled error observed by the first fixture draft.
	clone.Cancel = nil
	target := *request.URL
	target.Scheme, target.Host = transport.destination.Scheme, transport.destination.Host
	clone.URL, clone.Host = &target, ""
	response, err := transport.transport.RoundTrip(clone)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			transport.mu.Lock()
			transport.deadline = true
			transport.mu.Unlock()
		}
		return nil, err
	}
	if response.TLS == nil || !response.TLS.HandshakeComplete || len(response.TLS.VerifiedChains) == 0 {
		response.Body.Close()
		return nil, errors.New("owned CF transport requires a verified TLS response")
	}
	transport.mu.Lock()
	transport.verified++
	transport.mu.Unlock()
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		response.Body.Close()
		return nil, errors.New("owned CF transport refuses redirects")
	}
	return response, nil
}

func newAuditCFWorkerTransport(t *testing.T, peer *httptest.Server, archive bool) *auditCFWorkerTransport {
	t.Helper()
	destination, err := url.Parse(peer.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := peer.Client().Transport.(*http.Transport).Clone()
	transport.Proxy = nil
	if transport.TLSClientConfig == nil || transport.TLSClientConfig.InsecureSkipVerify || transport.TLSClientConfig.RootCAs == nil {
		t.Fatal("owned TLS transport must preserve certificate verification")
	}
	t.Cleanup(transport.CloseIdleConnections)
	return &auditCFWorkerTransport{destination: destination, transport: transport, archive: archive}
}

func auditCFWorkerManifest(t *testing.T, mutate func(map[string]any)) []byte {
	t.Helper()
	manifest := map[string]any{"manifestType": "minecraftModpack", "manifestVersion": 1,
		"name": "Owned CF Pack", "version": "1.0", "minecraft": map[string]any{"version": "1.20.1", "modLoaders": []map[string]any{{"id": "forge-47.4.0", "primary": true}}},
		"files": []map[string]any{{"projectID": 22, "fileID": 33, "required": true}}}
	if mutate != nil {
		mutate(manifest)
	}
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	entry, err := archive.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(entry).Encode(manifest); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

type auditCFWorkerCase struct {
	name, mode, errorText string
	mutate                func(map[string]any)
	pages                 int
	archive               bool
	success               bool
}

func auditRunCFWorkerCase(t *testing.T, test auditCFWorkerCase) {
	t.Helper()
	f := newAuditTP001CredentialFixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "modpack.create")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "modpack.create")
	archiveBytes := auditCFWorkerManifest(t, test.mutate)
	archive := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/files/owned/cf-pack.zip" || request.Header.Get("x-api-key") != "" || request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" {
			t.Error("actual archive peer received a foreign path or API credentials")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(archiveBytes)
	}))
	t.Cleanup(archive.Close)
	api := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("x-api-key") != auditCFWorkerKey || request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" {
			t.Error("actual API peer did not receive only the declared synthetic credential")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/mods/search":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": 123, "classId": 4471, "slug": "owned-cf-pack", "name": "Owned CF Pack", "summary": "Synthetic public source", "isAvailable": true, "categories": []map[string]string{{"slug": "tech", "name": "Technology"}}, "links": map[string]string{"websiteUrl": "https://www.curseforge.com/minecraft/modpacks/owned-cf-pack"}}}})
		case "/v1/mods/123/description":
			_ = json.NewEncoder(w).Encode(map[string]string{"data": "<p>Owned public pack description</p>"})
		case "/v1/mods/123/files":
			index, _ := strconv.Atoi(request.URL.Query().Get("index"))
			if index == 50 && test.mode == "429" {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":"controlled page2 quota"}`))
				return
			}
			if index == 50 && test.mode == "timeout" {
				<-request.Context().Done()
				return
			}
			count, total := 50, 51
			if test.mode == "boundary-1000" || test.mode == "budget-1001" {
				total = 1000
				if test.mode == "budget-1001" {
					total = 1001
				}
			} else if index == 50 && test.mode != "repeat" {
				count = 1
			}
			files := make([]curseForgePackFile, count)
			for i := range files {
				files[i] = curseForgePackFile{ID: int64(index + i + 1), ReleaseType: 2, FileStatus: 4, IsAvailable: true,
					FileName: "beta.zip", FileDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
			}
			if test.mode == "repeat" && index == 50 {
				files[0].ID, total = 1, 100
			}
			if (index == 50 && test.mode == "page2") || (index == 950 && test.mode == "boundary-1000") {
				at := len(files) - 1
				files[at].ReleaseType, files[at].DisplayName, files[at].FileName, files[at].DownloadURL = 1, "Owned release", "owned-cf-pack.zip", auditCFWorkerArchiveURL
				files[at].GameVersions = []string{"1.20.1"}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": files, "pagination": map[string]int{"index": index, "resultCount": count, "totalCount": total}})
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(api.Close)
	apiTransport, archiveTransport := newAuditCFWorkerTransport(t, api, false), newAuditCFWorkerTransport(t, archive, true)
	cfg := defaultModImportConfig()
	cfg.Modrinth.Token, cfg.GitHub.Token = "", ""
	cfg.RequestTimeoutSeconds = 5 // Existing supported minimum, normalized through the unchanged production contract.
	cfg.CurseForge.Enabled, cfg.CurseForge.APIKey = true, auditCFWorkerKey
	if err := normalizeModImportConfig(&cfg); err != nil {
		t.Fatal("unchanged official-origin/settings contract rejected the synthetic configuration")
	}
	sealed, err := f.server.sealSystemSetting(cfg)
	if err != nil || strings.Contains(sealed, auditCFWorkerKey) {
		t.Fatal("owned provider configuration must remain encrypted")
	}
	if _, err := f.db.Exec(f.ctx, `insert into system_settings(key,value) values($1,$2) on conflict(key) do update set value=excluded.value`, modImportConfigSettingKey, sealed); err != nil {
		t.Fatal(err)
	}
	job := decodeTEST022Data[modMetadataImportJobResponse](t, f.http(t, f.editor, http.MethodPost, "/api/v1/modpack-imports", modMetadataImportRequest{Provider: "curseforge", URL: "https://www.curseforge.com/minecraft/modpacks/owned-cf-pack"}, http.StatusAccepted))
	if job.ID == "" || job.Status != "queued" || job.ProjectType != "modpack" {
		t.Fatal("actual ordinary API did not create the queued modpack task")
	}
	before := auditFixProjectActivityFacts(t, f.test013Fixture)
	apiFactories, archiveFactories := 0, 0
	apiFactory := func(timeout time.Duration, base string) (*http.Client, error) {
		apiFactories++
		if timeout != 5*time.Second || base != "https://api.curseforge.com/v1" {
			return nil, errors.New("owned API factory received an undeclared configuration")
		}
		client, err := newProviderHTTPClient(timeout, base)
		if err == nil {
			client.Transport = apiTransport
		}
		return client, err
	}
	archiveFactory := func(timeout time.Duration, rawURL string, trusted ...string) (*http.Client, error) {
		archiveFactories++
		if timeout != 5*time.Second || rawURL != auditCFWorkerArchiveURL || !reflect.DeepEqual(trusted, []string{"https://api.curseforge.com/v1"}) {
			return nil, errors.New("owned archive factory received an undeclared origin or trust input")
		}
		client, err := newProviderDownloadHTTPClient(timeout, rawURL, trusted...)
		if err == nil {
			client.Transport = archiveTransport
		}
		return client, err
	}
	if err := f.server.runModMetadataImportWithProviderClients(f.ctx, job.ID, apiFactory, archiveFactory); err != nil {
		t.Fatal("actual Worker failed to persist its declared terminal state")
	}
	auditFixRequireProjectFactsUnchanged(t, before, auditFixProjectActivityFacts(t, f.test013Fixture))
	var status, failure string
	var resultNull, finished bool
	var actor int64
	if err := f.db.QueryRow(f.ctx, `select status,error,result is null,finished_at is not null,user_id from mod_metadata_import_jobs where public_id=$1`, job.ID).Scan(&status, &failure, &resultNull, &finished, &actor); err != nil || !finished || actor != f.userIDs[f.editor] {
		t.Fatalf("actual task terminal identity/timestamp failed: %v", err)
	}
	stored := decodeTEST022Data[modMetadataImportJobResponse](t, f.http(t, f.editor, http.MethodGet, "/api/v1/modpack-imports/"+job.ID, nil, http.StatusOK))
	f.http(t, f.otherEditor, http.MethodGet, "/api/v1/modpack-imports/"+job.ID, nil, http.StatusNotFound)
	if stored.Status != status || strings.Contains(string(stored.Result), auditCFWorkerKey) || strings.Contains(stored.Error, auditCFWorkerKey) {
		t.Fatal("actual owner result projection disagrees with task or exposed a credential")
	}
	apiTransport.mu.Lock()
	requests, verified, deadline := append([]string(nil), apiTransport.requests...), apiTransport.verified, apiTransport.deadline
	apiTransport.mu.Unlock()
	pages := []int{}
	for _, request := range requests {
		parsed, err := url.Parse(request)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Path == "/v1/mods/123/files" {
			index, _ := strconv.Atoi(parsed.Query().Get("index"))
			pages = append(pages, index)
		}
	}
	wantPages := make([]int, test.pages)
	for index := range wantPages {
		wantPages[index] = index * 50
	}
	archiveTransport.mu.Lock()
	archiveCalls, archiveVerified := len(archiveTransport.requests), archiveTransport.verified
	archiveTransport.mu.Unlock()
	wantArchive := 0
	if test.archive {
		wantArchive = 1
	}
	wantVerified := len(requests)
	if test.mode == "timeout" {
		wantVerified-- // The second page reached the real TLS peer, but intentionally has no response.
	}
	if !reflect.DeepEqual(pages, wantPages) || apiFactories != 1 || len(requests) != test.pages+2 || verified != wantVerified || archiveFactories != wantArchive || archiveCalls != wantArchive || archiveVerified != wantArchive {
		t.Fatalf("request/verified/factory budgets differ: pages%v want%v API%d/%d/%d archive%d/%d/%d", pages, wantPages, len(requests), verified, apiFactories, archiveFactories, archiveCalls, archiveVerified)
	}
	if test.success {
		var draft createModpackRequest
		if status != "completed" || resultNull || failure != "" || json.Unmarshal(stored.Result, &draft) != nil {
			t.Fatalf("legal archive did not produce its completed typed result: status%s null%v error%q", status, resultNull, failure)
		}
		selected := "51"
		if test.mode == "boundary-1000" {
			selected = "1000"
		}
		if draft.PrimaryName != "Owned CF Pack" || draft.PrimaryCategory != "technology" || draft.ImportSelection == nil || draft.ImportSelection.FileID != selected || len(draft.Mods) != 1 || draft.Mods[0].ProviderProjectID != "22" || draft.Mods[0].ProviderVersionID != "33" || !draft.Mods[0].ClientRequired || !draft.Mods[0].ServerRequired || !reflect.DeepEqual(draft.Compatibilities, []modLoaderCompatibilityPayload{{Loader: "Forge", Versions: []string{"1.20.1"}}}) {
			t.Fatalf("completed business projection mismatch: name%q category%q selection%+v mods%+v compatibilities%+v", draft.PrimaryName, draft.PrimaryCategory, draft.ImportSelection, draft.Mods, draft.Compatibilities)
		}
	} else {
		if status != "failed" || !resultNull || len(stored.Result) != 0 || failure == "" {
			t.Fatalf("rejected source left a usable result: status%s resultNull%v error%q", status, resultNull, failure)
		}
		if test.mode == "timeout" {
			if !deadline || !strings.HasPrefix(failure, "CurseForge file pagination failed: ") || !strings.Contains(failure, "context deadline exceeded") {
				t.Fatalf("deadline was not the exact failed boundary: %q typedDeadline%v", failure, deadline)
			}
		} else if failure != test.errorText {
			t.Fatalf("wrong rejection satisfied the negative case: got%q expected%q", failure, test.errorText)
		}
	}
	public := decodeTEST022Data[modpackListResponse](t, f.http(t, "", http.MethodGet, "/api/v1/modpacks?limit=24", nil, http.StatusOK))
	if public.Total != 0 || len(public.Items) != 0 {
		t.Fatal("an import preview/failure appeared in the anonymous public catalog")
	}
	t.Logf("actual ordinary HTTP202→fullWorker→%s, resultNull=%v pages=%v APIverified=%d archiveVerified=%d ZIPsha256=%x; exact error/draft, creator-only result, full business/outbox unchanged and anonymous publication0; controlled verified TLS, no official network or queue delivery claim", status, resultNull, pages, verified, archiveVerified, sha256.Sum256(archiveBytes))
}
