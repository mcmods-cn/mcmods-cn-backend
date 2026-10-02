package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

func TestOPS008SiteLogoSharedHTTPStorageReplacementAndCleanupIntegration(t *testing.T) {
	testOPS008SharedLogoLifecycle(t, 0)
}

func testOPS008SharedLogoLifecycle(t *testing.T, deletionFailureDelay time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool := newTEST018IsolatedDatabase(t, ctx)
	cfg := config.Load()
	cfg.JWTSecret = "ops008-only-jwt-signing-secret"
	cfg.SettingsEncryptionKey = "ops008-only-encryption-key-32-bytes"
	cfg.Redis.Enabled, cfg.AntiAbuse.Enabled = false, false
	cfg.Redis.SettingsCacheEnabled = true // exercise a warm peer, not only cold reads
	cfg.NATS = config.NATSConfig{}
	var storageMu sync.Mutex
	objects := map[string][]byte{}
	puts, deletes := 0, 0
	failDeletes := false
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		storageMu.Lock()
		defer storageMu.Unlock()
		switch r.Method {
		case http.MethodPut:
			puts++
			data, err := io.ReadAll(io.LimitReader(r.Body, 2*1024*1024+1))
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			objects[r.URL.Path] = data
			w.Header().Set("ETag", `"ops008-test"`)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			data, exists := objects[r.URL.Path]
			if !exists {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(data)
		case http.MethodDelete:
			if failDeletes {
				// Owned provider latency is a fault fixture, not a longer worker budget.
				if deletionFailureDelay > 0 {
					timer := time.NewTimer(deletionFailureDelay)
					select {
					case <-r.Context().Done():
						timer.Stop()
						return
					case <-timer.C:
					}
				}
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(w, `<Error><Code>ServiceUnavailable</Code><Message>owned injected failure</Message></Error>`)
				return
			}
			deletes++
			delete(objects, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer provider.Close()
	servers := make([]*Server, 2)
	origins := make([]*httptest.Server, 2)
	for index := range servers {
		queueClient := queue.New(ctx, cfg.NATS)
		t.Cleanup(queueClient.Close)
		server := NewServer(ctx, cfg, pool, queueClient, nil, nil, nil)
		servers[index] = server
		t.Cleanup(func() {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			if err := server.Shutdown(shutdownCtx); err != nil {
				t.Errorf("shutdown shared logo server: %v", err)
			}
			_ = server.cache.Close()
		})
		origins[index] = httptest.NewServer(server)
		t.Cleanup(origins[index].Close)
	}
	ossConfig := ossConfigPayload{Enabled: true, Region: "cn-test", Endpoint: provider.URL, PublicEndpoint: provider.URL, Bucket: "ops008-only", AccessKeyID: "ops008-key", AccessKeySecret: "ops008-secret", UseCName: true, Prefix: "mcmods", DownloadURLMode: ossDownloadModePresigned}
	sealed, err := servers[0].sealSystemSetting(ossConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb)`, sealed); err != nil {
		t.Fatal(err)
	}
	if actual := servers[0].ossConfigFromSettings(ctx); actual.Endpoint != provider.URL || actual.PublicEndpoint != provider.URL || !actual.UseCName {
		t.Fatalf("refuse a test OSS configuration outside the owned provider: %#v", redactOSSConfig(actual))
	}
	adminID, _, token := createTEST044User(t, ctx, pool, cfg, "ops008", "UTC")
	grantTEST044Permissions(t, ctx, pool, adminID, "admin.config.write")
	client := origins[0].Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	request := func(origin int, token, method, path, contentType string, body []byte, want int) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, origins[origin].URL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if token != "" {
			if strings.HasPrefix(token, siteLogoCredentialScheme) {
				req.Header.Set("Authorization", token)
			} else {
				req.Header.Set("Authorization", "Bearer "+token)
			}
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024+1))
		if err != nil || response.StatusCode != want {
			t.Fatalf("%s %s status=%d want=%d err=%v body=%s", method, path, response.StatusCode, want, err, raw)
		}
		return raw
	}
	var pngBody bytes.Buffer
	raster := image.NewRGBA(image.Rect(0, 0, 32, 16))
	raster.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err = png.Encode(&pngBody, raster); err != nil {
		t.Fatal(err)
	}
	// Warm the peer's settings cache before the mutation.
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('site.general','{"siteName":"Legacy Name","logoUrl":"/site-assets/site-logo-0123456789abcdefabcd.webp"}'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	if raw := request(1, "", http.MethodGet, "/api/v1/site/config", "", nil, http.StatusOK); !bytes.Contains(raw, []byte("Legacy Name")) || !bytes.Contains(raw, []byte(`"logoUrl":""`)) {
		t.Fatalf("legacy cleanup lost name or retained local authority: %s", raw)
	}
	uploadAuthorization := token
	upload := func(origin int) string {
		t.Helper()
		raw := request(origin, uploadAuthorization, http.MethodPost, "/api/v1/admin/config/general/logo", "image/png", pngBody.Bytes(), http.StatusCreated)
		var envelope struct {
			Data struct {
				URL string `json:"url"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil || !regexp.MustCompile(`^/site-assets/site-logo-[a-z0-9]{9}\.png$`).MatchString(envelope.Data.URL) {
			t.Fatalf("invalid shared asset response: err=%v body=%s", err, raw)
		}
		return envelope.Data.URL
	}
	first := upload(0)
	publicID := func(logo string) string {
		return strings.TrimSuffix(strings.TrimPrefix(logo, "/site-assets/site-logo-"), ".png")
	}
	publicAsset := func(origin int, logo string, want int) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, origins[origin].URL+"/api/v1/site/logo/"+publicID(logo), nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("public logo status=%d want=%d", response.StatusCode, want)
		}
		if want != http.StatusTemporaryRedirect {
			return
		}
		location, err := url.Parse(response.Header.Get("Location"))
		if err != nil || location.Scheme+"://"+location.Host != provider.URL || !strings.Contains(response.Header.Get("Cache-Control"), "no-store") || !strings.Contains(location.RawQuery, "x-oss-signature=") {
			t.Fatalf("unsafe/non-shared logo redirect: %v headers=%v", err, response.Header)
		}
		asset, err := client.Get(location.String())
		if err != nil {
			t.Fatal(err)
		}
		defer asset.Body.Close()
		decoded, format, err := image.Decode(io.LimitReader(asset.Body, siteLogoMaximumOutputBytes+1))
		if err != nil || asset.StatusCode != http.StatusOK || format != "png" || decoded.Bounds() != raster.Bounds() {
			t.Fatalf("shared PNG derivative: status=%d format=%s error=%v", asset.StatusCode, format, err)
		}
	}
	publicAsset(1, first, http.StatusTemporaryRedirect)
	request(0, "", http.MethodPost, "/api/v1/admin/config/general/logo", "image/png", pngBody.Bytes(), http.StatusUnauthorized)
	_, _, denied := createTEST044User(t, ctx, pool, cfg, "ops008-denied", "UTC")
	request(0, denied, http.MethodPost, "/api/v1/admin/config/general/logo", "image/png", pngBody.Bytes(), http.StatusForbidden)
	for _, invalid := range []struct {
		mime   string
		body   []byte
		status int
	}{
		{"image/svg+xml", []byte(`<svg/>`), http.StatusUnsupportedMediaType},
		{"image/png", pngBody.Bytes()[:len(pngBody.Bytes())/2], http.StatusUnprocessableEntity},
		{"image/webp", pngBody.Bytes(), http.StatusUnprocessableEntity},
		{"image/png", bytes.Repeat([]byte{0}, siteLogoMaximumInputBytes+1), http.StatusRequestEntityTooLarge},
	} {
		request(0, token, http.MethodPost, "/api/v1/admin/config/general/logo", invalid.mime, invalid.body, invalid.status)
	}
	storageMu.Lock()
	unchangedPuts := puts
	storageMu.Unlock()
	if unchangedPuts != 1 {
		t.Fatalf("denied/invalid input reached shared storage: PUT=%d", unchangedPuts)
	}
	save := func(logo string, want int) {
		t.Helper()
		body := fmt.Sprintf(`{"siteName":"Shared Brand","logoUrl":%q}`, logo)
		request(0, token, http.MethodPut, "/api/v1/admin/config/general", "application/json", []byte(body), want)
	}
	save(first, http.StatusOK)
	if raw := request(1, "", http.MethodGet, "/api/v1/site/config", "", nil, http.StatusOK); !bytes.Contains(raw, []byte(first)) || !bytes.Contains(raw, []byte("Shared Brand")) {
		t.Fatalf("warm peer did not observe authoritative shared brand: %s", raw)
	}
	var delegation struct {
		Data struct {
			Authorization string `json:"authorization"`
		} `json:"data"`
	}
	if err = json.Unmarshal(request(1, token, http.MethodPost, "/api/v1/admin/config/general/logo-upload-authorization", "application/json", []byte(`{}`), http.StatusOK), &delegation); err != nil {
		t.Fatal(err)
	}
	uploadAuthorization = delegation.Data.Authorization
	second := upload(1) // the real shared PUT path also accepts the bounded delegation
	if second == first {
		t.Fatal("two uploads reused an immutable version identity")
	}
	// A guessed OSS identity cannot turn a private/quarantined file into branding.
	if _, err = pool.Exec(ctx, `update oss_files set source='private-upload' where public_id=$1`, publicID(second)); err != nil {
		t.Fatal(err)
	}
	publicAsset(0, second, http.StatusNotFound)
	save(second, http.StatusUnprocessableEntity)
	if _, err = pool.Exec(ctx, `update oss_files set source='site_logo_pending',scan_status='rejected' where public_id=$1`, publicID(second)); err != nil {
		t.Fatal(err)
	}
	publicAsset(0, second, http.StatusNotFound)
	save(second, http.StatusUnprocessableEntity)
	if _, err = pool.Exec(ctx, `update oss_files set scan_status='trusted_generated' where public_id=$1`, publicID(second)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `alter table oss_object_deletion_outbox add constraint ops008_reject_cleanup check(reason<>'site_logo_replaced') not valid`); err != nil {
		t.Fatal(err)
	}
	save(second, http.StatusInternalServerError)
	if raw := request(1, "", http.MethodGet, "/api/v1/site/config", "", nil, http.StatusOK); !bytes.Contains(raw, []byte(first)) {
		t.Fatalf("failed cleanup partially replaced brand: %s", raw)
	}
	if _, err = pool.Exec(ctx, `alter table oss_object_deletion_outbox drop constraint ops008_reject_cleanup`); err != nil {
		t.Fatal(err)
	}
	save(second, http.StatusOK)
	publicAsset(0, first, http.StatusNotFound)
	publicAsset(0, second, http.StatusTemporaryRedirect)
	var tombstones, queued int
	if err = pool.QueryRow(ctx, `select count(*) from oss_files where source='site_logo' and status='deleted'`).Scan(&tombstones); err != nil || tombstones != 1 {
		t.Fatalf("old logo tombstones=%d err=%v", tombstones, err)
	}
	if err = pool.QueryRow(ctx, `select count(*) from oss_object_deletion_outbox where reason='site_logo_replaced'`).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("old logo cleanup tasks=%d err=%v", queued, err)
	}
	third := upload(0)
	thirdID := strings.TrimSuffix(strings.TrimPrefix(third, "/site-assets/site-logo-"), ".png")
	if _, err = pool.Exec(ctx, `update oss_files set created_at=now()-interval '2 hours' where public_id=$1`, thirdID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update oss_files set created_at=now()-interval '2 hours' where public_id=$1`, publicID(second)); err != nil {
		t.Fatal(err)
	}
	publicAsset(1, third, http.StatusNotFound)
	save(third, http.StatusUnprocessableEntity)
	if _, err = pool.Exec(ctx, `alter table oss_object_deletion_outbox add constraint ops008_reject_expiry check(reason<>'site_logo_upload_expired') not valid`); err != nil {
		t.Fatal(err)
	}
	NewMaintenanceWorker(pool, servers[0].cache).prune(ctx)
	if err = pool.QueryRow(ctx, `select count(*) from oss_files where public_id=$1 and status='active'`, thirdID).Scan(&tombstones); err != nil || tombstones != 1 {
		t.Fatalf("failed expiry must rollback tombstone: %d %v", tombstones, err)
	}
	if _, err = pool.Exec(ctx, `alter table oss_object_deletion_outbox drop constraint ops008_reject_expiry`); err != nil {
		t.Fatal(err)
	}
	NewMaintenanceWorker(pool, servers[0].cache).prune(ctx)
	if err = pool.QueryRow(ctx, `select count(*) from oss_files where public_id=$1 and status='deleted'`, thirdID).Scan(&tombstones); err != nil || tombstones != 1 {
		t.Fatalf("abandoned shared logo remained active: count=%d err=%v", tombstones, err)
	}
	publicAsset(1, second, http.StatusTemporaryRedirect) // bound logos are not staging TTL victims
	// Provider outage leaves durable retryable jobs; a fresh worker resumes them.
	storageMu.Lock()
	failDeletes = true
	storageMu.Unlock()
	// Claim both first-attempt jobs before invoking the actual provider. A drain
	// may correctly reclaim an earlier job once its two-second retry is due
	// while a later object's SDK retry is still running. This fixture verifies
	// exactly one failure per claimed job; recovery below uses a fresh full drain.
	outageWorker := NewOSSDeletionWorker(cfg, pool)
	failedJobs := make([]ossDeletionJob, 0, 2)
	for range 2 {
		job, claimErr := outageWorker.claim(ctx)
		if claimErr != nil || job.Attempts != 1 {
			t.Fatalf("first outage claim attempts=%d err=%v", job.Attempts, claimErr)
		}
		if len(failedJobs) > 0 && failedJobs[0].ID == job.ID {
			t.Fatal("claimed the same live lease twice")
		}
		failedJobs = append(failedJobs, job)
	}
	for _, job := range failedJobs {
		deletionErr := outageWorker.deleteObject(ctx, job)
		if deletionErr == nil || classifyOSSDeletionFailure(deletionErr).class != "remote_transient" {
			t.Fatalf("owned provider did not return actual transient failure: %v", deletionErr)
		}
		if dead, failureErr := outageWorker.recordFailure(ctx, job, deletionErr); failureErr != nil || dead {
			t.Fatalf("persist owned provider failure dead=%t err=%v", dead, failureErr)
		}
	}
	if err = pool.QueryRow(ctx, `select count(*) from oss_object_deletion_outbox where status='pending' and attempts=1 and failure_class='remote_transient'`).Scan(&queued); err != nil || queued != 2 {
		var jobs string
		diagnosticErr := pool.QueryRow(ctx, `select coalesce(jsonb_agg(jsonb_build_object(
			'reason',reason,'status',status,'attempts',attempts,'failureClass',failure_class,
			'lastError',last_error,'due',next_attempt_at<=now()) order by id),'[]'::jsonb)::text
			from oss_object_deletion_outbox`).Scan(&jobs)
		t.Fatalf("durable failed deletes=%d err=%v jobs=%s diagnosticErr=%v", queued, err, jobs, diagnosticErr)
	}
	storageMu.Lock()
	failDeletes = false
	storageMu.Unlock()
	if _, err = pool.Exec(ctx, `update oss_object_deletion_outbox set next_attempt_at=now() where status='pending'`); err != nil {
		t.Fatal(err)
	}
	NewOSSDeletionWorker(cfg, pool).drain(ctx)
	storageMu.Lock()
	remaining, actualPuts, actualDeletes := len(objects), puts, deletes
	storageMu.Unlock()
	if remaining != 1 || actualPuts != 3 || actualDeletes != 2 {
		t.Fatalf("shared physical lifecycle: objects=%d PUT=%d DELETE=%d want=1/3/2", remaining, actualPuts, actualDeletes)
	}
	// A successful PUT followed by a failed registration is also eventually removed.
	if _, err = pool.Exec(ctx, `alter table oss_files add constraint ops008_reject_registration check(source<>'site_logo_pending') not valid`); err != nil {
		t.Fatal(err)
	}
	request(0, token, http.MethodPost, "/api/v1/admin/config/general/logo", "image/png", pngBody.Bytes(), http.StatusInternalServerError)
	if _, err = pool.Exec(ctx, `alter table oss_files drop constraint ops008_reject_registration`); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select count(*) from oss_object_deletion_outbox where reason='generated-object-registration-failed' and oss_file_id is null and status='pending'`).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("unregistered object compensation=%d err=%v", queued, err)
	}
	NewOSSDeletionWorker(cfg, pool).drain(ctx)
	save("", http.StatusOK)
	publicAsset(1, second, http.StatusNotFound)
	if raw := request(1, "", http.MethodGet, "/api/v1/site/config", "", nil, http.StatusOK); !bytes.Contains(raw, []byte(`"logoUrl":""`)) {
		t.Fatalf("peer kept removed logo: %s", raw)
	}
	NewOSSDeletionWorker(cfg, pool).drain(ctx)
	NewOSSDeletionWorker(cfg, pool).drain(ctx) // replay does not delete an object twice
	storageMu.Lock()
	remaining, actualPuts, actualDeletes = len(objects), puts, deletes
	storageMu.Unlock()
	if remaining != 0 || actualPuts != 4 || actualDeletes != 4 {
		t.Fatalf("final shared lifecycle: objects=%d PUT=%d DELETE=%d want=0/4/4", remaining, actualPuts, actualDeletes)
	}
	// Competing administrators are serialized by the authoritative setting,
	// without leaving two bound objects or resurrecting the replaced version.
	fourth, fifth := upload(0), upload(1)
	var concurrent sync.WaitGroup
	for origin, logo := range []string{fourth, fifth} {
		concurrent.Add(1)
		go func() {
			defer concurrent.Done()
			body := fmt.Sprintf(`{"siteName":"Concurrent Brand","logoUrl":%q}`, logo)
			request(origin, token, http.MethodPut, "/api/v1/admin/config/general", "application/json", []byte(body), http.StatusOK)
		}()
	}
	concurrent.Wait()
	var final struct {
		Data siteGeneralConfig `json:"data"`
	}
	if err = json.Unmarshal(request(1, "", http.MethodGet, "/api/v1/site/config", "", nil, http.StatusOK), &final); err != nil {
		t.Fatal(err)
	}
	if final.Data.LogoURL != fourth && final.Data.LogoURL != fifth {
		t.Fatalf("concurrent saved unknown logo: %#v", final.Data)
	}
	if err = pool.QueryRow(ctx, `select count(*) from oss_files where source='site_logo' and status='active'`).Scan(&remaining); err != nil || remaining != 1 {
		t.Fatalf("concurrent binding leaked active versions: %d %v", remaining, err)
	}
	publicAsset(0, final.Data.LogoURL, http.StatusTemporaryRedirect)
	loser := fourth
	if loser == final.Data.LogoURL {
		loser = fifth
	}
	publicAsset(0, loser, http.StatusNotFound)
	save(loser, http.StatusUnprocessableEntity)
	// Construct a new application after mutation. It has no warmed peer state
	// or Next instance directory and still sees the persisted shared reference.
	freshQueue := queue.New(ctx, cfg.NATS)
	defer freshQueue.Close()
	fresh := NewServer(ctx, cfg, pool, freshQueue, nil, nil, nil)
	defer fresh.cache.Close()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := fresh.Shutdown(shutdownCtx); err != nil {
			t.Error(err)
		}
	}()
	restarted := httptest.NewServer(fresh)
	defer restarted.Close()
	restartResponse, err := client.Get(restarted.URL + "/api/v1/site/config")
	if err != nil {
		t.Fatal(err)
	}
	restartBody, err := io.ReadAll(restartResponse.Body)
	restartResponse.Body.Close()
	if err != nil || restartResponse.StatusCode != http.StatusOK || !bytes.Contains(restartBody, []byte(final.Data.LogoURL)) {
		t.Fatalf("restart lost shared brand: status=%d error=%v body=%s", restartResponse.StatusCode, err, restartBody)
	}
	save("", http.StatusOK)
	NewOSSDeletionWorker(cfg, pool).drain(ctx)
	storageMu.Lock()
	remaining, actualPuts, actualDeletes = len(objects), puts, deletes
	storageMu.Unlock()
	if remaining != 0 || actualPuts != 6 || actualDeletes != 6 {
		t.Fatalf("concurrent/restart lifecycle leaked: objects=%d PUT=%d DELETE=%d", remaining, actualPuts, actualDeletes)
	}
}
