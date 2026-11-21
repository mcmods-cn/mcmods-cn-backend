package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The archive stays outside the repository. This gate stops at byte binding:
// the browser enqueues its normal import. This standalone fixture runs no worker.
// Enable MCMODS_RUN_DB_INTEGRATION and MCMODS_RUN_REAL_GAME_MULTIPART. Point
// MCMODS_AUDIT_GAME_PACKAGE and MCMODS_AUDIT_GAME_SHA256 at the private ZIP
// and its pinned digest. MCMODS_AUDIT_FRONTEND_REPO is an isolated checkout
// with npm ci and Chromium installed. Run go test ./internal/httpapi -run
// TestRealGameMultipartResumeIntegration -count=1 -v.
func TestRealGameMultipartResumeIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_REAL_GAME_MULTIPART") != "1" {
		t.Skip("requires a real game ZIP above the production multipart threshold, its SHA256, and an isolated frontend checkout with Playwright")
	}
	archive, expected, frontend := os.Getenv("MCMODS_AUDIT_GAME_PACKAGE"), os.Getenv("MCMODS_AUDIT_GAME_SHA256"), os.Getenv("MCMODS_AUDIT_FRONTEND_REPO")
	if !filepath.IsAbs(archive) || !filepath.IsAbs(frontend) || !auditChallengeHex(expected, 64) {
		t.Fatal("required: absolute MCMODS_AUDIT_GAME_PACKAGE, MCMODS_AUDIT_FRONTEND_REPO and pinned MCMODS_AUDIT_GAME_SHA256")
	}
	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<20 || !shouldUseOSSMultipart(info.Size()) || ossMultipartPartCount(info.Size(), ossMultipartPartSize) < 4 {
		t.Fatal("supply a regular archive with at least four production parts within the fixture user's 64 MiB allowance")
	}
	size := info.Size()
	digest := sha256.New()
	n, err := io.Copy(digest, io.LimitReader(file, size+1))
	if err != nil || n != size || hex.EncodeToString(digest.Sum(nil)) != expected {
		t.Fatal("cannot verify the original archive against its recorded hash")
	}
	parts := ossMultipartPartCount(size, ossMultipartPartSize)

	// Reserve a loopback origin for the isolated Next instance. Its production
	// API and OSS client settings are injected before Next reads its config.
	reservation := httptest.NewUnstartedServer(nil)
	frontendOrigin := "http://" + reservation.Listener.Addr().String()
	reservation.Listener.Close()
	t.Setenv("FRONTEND_ORIGIN", frontendOrigin)
	base := newAuditFixGRFixture(t, 6*time.Minute)
	f := newTEST037FixtureWithBase(t, base.test013Fixture)
	var resumed atomic.Bool
	var mu sync.Mutex
	var accepted, acceptedAfter []int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/export-imports/uploads/resume") {
			resumed.Store(true)
		}
		f.server.ServeHTTP(w, r)
	}))
	defer api.Close()

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") == frontendOrigin {
			w.Header().Set("Access-Control-Allow-Origin", frontendOrigin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "PUT,OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Expose-Headers", "ETag")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		q := r.URL.Query()
		if r.Method != http.MethodPut || q.Get("uploadId") == "" {
			f.provider.Config.Handler.ServeHTTP(w, r)
			return
		}
		number, parseErr := strconv.Atoi(q.Get("partNumber"))
		key := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/"), f.cfg.Bucket+"/")
		if parseErr != nil || number < 1 || number > parts || !validTEST037PresignedRequest(r, key, f.cfg, time.Now()) {
			http.Error(w, "invalid signed part", http.StatusForbidden)
			return
		}
		second := resumed.Load()
		// Multipart PUT responses carry only an ETag. Preserve the real
		// fixture's validation and count parts only after its successful write.
		rec := httptest.NewRecorder()
		f.provider.Config.Handler.ServeHTTP(rec, r)
		for name, values := range rec.Header() {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		if rec.Code == http.StatusOK {
			mu.Lock()
			if second {
				acceptedAfter = append(acceptedAfter, number)
			} else {
				accepted = append(accepted, number)
			}
			mu.Unlock()
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	}))
	defer provider.Close()
	f.cfg.Endpoint, f.cfg.PublicEndpoint = provider.URL, provider.URL
	sealed, err := f.server.sealSystemSetting(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, "update system_settings set value=$1::jsonb where key='oss.aliyun'", sealed); err != nil {
		t.Fatal(err)
	}

	input, err := json.Marshal(map[string]string{
		"archive": archive, "sha256": expected, "frontend": frontend, "frontendOrigin": frontendOrigin, "api": api.URL, "oss": provider.URL,
		"site": "audit-gr-" + base.modCode, "version": base.versionPublic, "actor": f.editor,
	})
	if err != nil {
		t.Fatal(err)
	}
	driver := filepath.Join("..", "..", "tools", "testing", "export_multipart", "browser.mjs")
	cmd := exec.CommandContext(f.ctx, "node", driver)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 5 * time.Second
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("browser driver: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	var result realMultipartCheckpoint
	if err = json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("invalid private browser result: %v", err)
	}
	mu.Lock()
	slices.Sort(accepted)
	slices.Sort(acceptedAfter)
	slices.Sort(result.Interrupted)
	before, after := slices.Clone(accepted), slices.Clone(acceptedAfter)
	mu.Unlock()
	if !slices.Equal(before, result.Confirmed) || !slices.Equal(after, result.SentAfter) {
		t.Fatalf("provider accepted initial=%v resumed=%v, browser observed initial=%v resumed=%v", before, after, result.Confirmed, result.SentAfter)
	}
	proof := verifyRealMultipartBinding(t, f, result, expected, size)
	raw, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("MULTIPART_BINDING %s", raw)
}

type realMultipartCheckpoint struct {
	FileID      string `json:"fileId"`
	UploadID    string `json:"uploadId"`
	ObjectKey   string `json:"objectKey"`
	SHA256      string `json:"sha256"`
	SizeBytes   int64  `json:"sizeBytes"`
	Browser     string `json:"browser"`
	Confirmed   []int  `json:"confirmed"`
	Interrupted []int  `json:"interrupted"`
	SentAfter   []int  `json:"sentAfter"`
	Presigns    int    `json:"presigns"`
	Resumes     int    `json:"resumes"`
	Statuses    []int  `json:"statuses"`
}

// A long-lived fixture calls this from its observation loop. The browser holds
// the original job POST until the checked file exists, so the worker cannot
// consume the object while the active binding and quota are inspected.
func auditJourneyMultipartBinding(t *testing.T, f test037Fixture, directory, expectedSHA string, size int64) {
	t.Helper()
	checked := filepath.Join(directory, "multipart-binding-checked")
	if _, err := os.Stat(checked); err == nil {
		return
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	ready, err := os.Open(filepath.Join(directory, "multipart-binding-ready.json"))
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	defer ready.Close()
	var checkpoint realMultipartCheckpoint
	if err = json.NewDecoder(io.LimitReader(ready, 16<<10)).Decode(&checkpoint); err != nil {
		t.Fatal(err)
	}
	proof := verifyRealMultipartBinding(t, f, checkpoint, expectedSHA, size)
	auditJourneyWriteJSON(t, filepath.Join(directory, "multipart-binding-proof.json"), proof)
	handle, err := os.OpenFile(checked, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if os.IsExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if err = handle.Close(); err != nil {
		t.Fatal(err)
	}
}

func verifyRealMultipartBinding(t *testing.T, f test037Fixture, result realMultipartCheckpoint, expected string, size int64) map[string]any {
	t.Helper()
	parts := ossMultipartPartCount(size, ossMultipartPartSize)
	var wanted []int
	for n := 1; n <= parts; n++ {
		if n != 1 && n != 3 {
			wanted = append(wanted, n)
		}
	}
	slices.Sort(result.Interrupted)
	if !validCatalogPublicID(result.FileID) || !validOSSMultipartUploadID(result.UploadID) || result.ObjectKey == "" || result.SHA256 != expected || result.SizeBytes != size ||
		!slices.Equal(result.Confirmed, []int{1, 3}) || !slices.Equal(result.SentAfter, wanted) || len(result.Interrupted) == 0 || result.Presigns != 1 || result.Resumes != 1 || !slices.Equal(result.Statuses, []int{201, 200}) {
		t.Fatalf("browser checkpoint did not preserve the source, session and parts: initial=%v interrupted=%v resumed=%v confirmation=%v", result.Confirmed, result.Interrupted, result.SentAfter, result.Statuses)
	}

	var status, storedKey, fileHash, storedHash, verifiedSource, verifiedStored, publicID string
	var sourceBytes, storedBytes, owner, activeBytes, reservedBytes, dailyBytes, dailyReserved int64
	var quotaStored, reservedStored, dailyStored, dailyStoredReserved int64
	var activeFiles, bindings, sessionCount int
	err := f.db.QueryRow(f.ctx, `select session.status, binding.stored_object_key,
		file.public_id,file.sha256,file.stored_sha256,binding.source_verified_sha256,binding.stored_verified_sha256,
		file.source_size_bytes,file.size_bytes,file.uploader_id,q.active_source_bytes,q.reserved_source_bytes,q.active_stored_bytes,q.reserved_stored_bytes,
		(select coalesce(sum(active_stored_bytes),0) from oss_user_daily_quota_usage where user_id=q.user_id),
		(select coalesce(sum(reserved_stored_bytes),0) from oss_user_daily_quota_usage where user_id=q.user_id),
		(select coalesce(sum(active_source_bytes),0) from oss_user_daily_quota_usage where user_id=q.user_id),
		(select coalesce(sum(reserved_source_bytes),0) from oss_user_daily_quota_usage where user_id=q.user_id),
		(select count(*) from oss_files where uploader_id=q.user_id and status='active'),
		(select count(*) from oss_upload_byte_bindings where upload_object_key=$1 and status='committed'),
		(select count(*) from oss_multipart_sessions where object_key=$1)
		from oss_multipart_sessions session join oss_upload_byte_bindings binding on binding.upload_object_key=session.object_key and binding.status='committed'
		join oss_files file on file.id=binding.file_id and file.status='active'
		join oss_user_quota_usage q on q.user_id=file.uploader_id
		where session.object_key=$1 and session.upload_id=$2`, result.ObjectKey, result.UploadID).
		Scan(&status, &storedKey, &publicID, &fileHash, &storedHash, &verifiedSource, &verifiedStored, &sourceBytes, &storedBytes, &owner, &activeBytes, &reservedBytes, &quotaStored, &reservedStored, &dailyStored, &dailyStoredReserved, &dailyBytes, &dailyReserved, &activeFiles, &bindings, &sessionCount)
	if err != nil {
		t.Fatal(err)
	}
	if status != "completed" || publicID != result.FileID || owner != f.userIDs[f.editor] || activeFiles != 1 || bindings != 1 || sessionCount != 1 || sourceBytes != size || storedBytes != size ||
		fileHash != expected || storedHash != expected || verifiedSource != expected || verifiedStored != expected || quotaStored != size || dailyStored != size || reservedStored != 0 || dailyStoredReserved != 0 || activeBytes != size || dailyBytes != size || reservedBytes != 0 || dailyReserved != 0 {
		t.Fatalf("binding facts status=%s identity=%t owner=%t hash=%t quota=%d/%d daily=%d/%d records=%d/%d/%d", status, publicID == result.FileID, owner == f.userIDs[f.editor], fileHash == expected && storedHash == expected && verifiedSource == expected && verifiedStored == expected, activeBytes, reservedBytes, dailyBytes, dailyReserved, activeFiles, bindings, sessionCount)
	}
	f.store.mu.Lock()
	object, exists := f.store.objects[storedKey]
	_, unfinished := f.store.uploads[result.UploadID]
	physicalKeys := len(f.store.objects)
	providerUploads := f.store.sequence
	staging, stagingPresent := f.store.objects[result.ObjectKey]
	stagingHash := sha256.Sum256(staging.data)
	contentDigest := sha256.Sum256(object.data)
	f.store.mu.Unlock()
	if !exists || !stagingPresent || result.ObjectKey == storedKey || int64(len(staging.data)) != size || hex.EncodeToString(stagingHash[:]) != expected || unfinished || providerUploads != 1 || int64(len(object.data)) != size || hex.EncodeToString(contentDigest[:]) != expected {
		t.Fatal("provider bytes or multipart cleanup differ from the confirmed binding")
	}
	return map[string]any{
		"status": "PASS", "browser": result.Browser, "sha256": expected, "sourceBytes": size, "productionPartBytes": ossMultipartPartSize,
		"partCount": parts, "acceptedBefore": result.Confirmed, "interruptedBeforeStorage": result.Interrupted, "acceptedAfter": result.SentAfter,
		"presigns": result.Presigns, "resumes": result.Resumes, "sameUploadID": true, "completeStatuses": result.Statuses,
		"fileSHA256": fileHash, "storedFileSHA256": storedHash, "bindingVerifiedSourceSHA256": verifiedSource, "bindingVerifiedStoredSHA256": verifiedStored,
		"stagingObjectSHA256": hex.EncodeToString(stagingHash[:]), "boundObjectSHA256": hex.EncodeToString(contentDigest[:]),
		"activeOwnerFiles": activeFiles, "committedBindings": bindings, "multipartSessions": sessionCount, "ownerMatchesScopedEditor": true,
		"physicalProviderKeys": physicalKeys, "stagingObjectPresent": stagingPresent, "providerUploads": providerUploads,
		"userActiveSourceBytes": activeBytes, "userActiveStoredBytes": quotaStored, "userReservedSourceBytes": reservedBytes, "userReservedStoredBytes": reservedStored,
		"dailyActiveSourceBytes": dailyBytes, "dailyActiveStoredBytes": dailyStored, "dailyReservedSourceBytes": dailyReserved, "dailyReservedStoredBytes": dailyStoredReserved,
	}
}
