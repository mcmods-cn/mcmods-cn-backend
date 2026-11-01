package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	xwebp "golang.org/x/image/webp"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestOCT02ConvertedUploadRegistrationFailureReleasesQuotaConnectionIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	base := newOCT02IsolatedDatabase(t, ctx)
	poolConfig := base.Config()
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var actor int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash) values('cleanup-pool','cleanup-pool@example.invalid','synthetic') returning id`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	var source bytes.Buffer
	if err = png.Encode(&source, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(source.Bytes())
	hash := hex.EncodeToString(digest[:])
	key := fmt.Sprintf("mcmods/user/%d/files/playground/synthetic.png", actor)
	converted, err := os.ReadFile("testdata/audit-20261024/oss-atomic-transparent-2x2.webp")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := xwebp.Decode(bytes.NewReader(converted))
	if err != nil || decoded.Bounds().Dx() != 2 || decoded.Bounds().Dy() != 2 {
		t.Fatalf("invalid actual WebP fixture: %v", err)
	}
	var processes atomic.Int64
	var copiedSource, processedObject atomic.Value
	copiedSource.Store("")
	processedObject.Store("")
	// Simulate only the external provider, with actual readable PNG and WebP.
	// The test must reach its specific PostgreSQL registration constraint rather
	// than treating an earlier unsupported GET/hash error as that failure.
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestKey := strings.TrimPrefix(r.URL.Path, "/")
		if r.Method == http.MethodPut && r.Header.Get("x-oss-copy-source") != "" {
			if r.Header.Get("x-oss-copy-source") != "/owned-cleanup/"+key || !isOSSSealedObjectKey(requestKey) || path.Base(requestKey) != path.Base(key) {
				http.Error(w, "wrong copy identity", 400)
				return
			}
			copiedSource.Store(requestKey)
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, `<CopyObjectResult><LastModified>2026-10-01T00:00:00.000Z</LastModified><ETag>owned-copy</ETag></CopyObjectResult>`)
			return
		}
		if r.Method == http.MethodPost {
			body, readErr := io.ReadAll(io.LimitReader(r.Body, 8193))
			values, parseErr := url.ParseQuery(string(body))
			destination := persistedWebPObjectKey(requestKey)
			if readErr != nil || parseErr != nil || len(body) > 8192 || requestKey != copiedSource.Load().(string) || values.Get("x-oss-process") != "image/format,webp|sys/saveas,o_"+base64.RawURLEncoding.EncodeToString([]byte(destination)) {
				http.Error(w, "wrong sealed process source or destination", 400)
				return
			}
			processedObject.Store(destination)
			processes.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"OK"}`))
			return
		}
		raw := source.Bytes()
		contentType := "image/png"
		switch requestKey {
		case key, copiedSource.Load().(string):
		case processedObject.Load().(string):
			raw, contentType = converted, "image/webp"
		default:
			w.WriteHeader(404)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		checksum := sha256.Sum256(raw)
		w.Header().Set("ETag", `"synthetic"`)
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", fmt.Sprint(len(raw)))
		w.Header().Set("x-oss-meta-sha256", hex.EncodeToString(checksum[:]))
		if r.Method == http.MethodGet {
			_, _ = w.Write(raw)
		}
	}))
	defer provider.Close()
	server := &Server{db: pool, cfg: config.Config{SettingsEncryptionKey: "oct02-cleanup-owned-settings-key-at-least-32"}}
	cfg := defaultOSSConfig()
	cfg.Enabled = true
	cfg.UseCName = true
	cfg.Endpoint = provider.URL
	cfg.PublicEndpoint = provider.URL
	cfg.Bucket = "owned-cleanup"
	cfg.Region = "cn-test"
	cfg.AccessKeyID = "synthetic"
	cfg.AccessKeySecret = "synthetic-only"
	sealed, err := server.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb)`, sealed); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `alter table oss_files add constraint oct02_reject_converted_registration check(source<>'playground') not valid`); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(ossCompleteUploadRequest{ObjectKey: key, OriginalName: "synthetic.png", ContentType: "image/png", SizeBytes: int64(source.Len()), SHA256: hash, Category: "playground", Source: "playground"})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/me/oss/uploads/complete", bytes.NewReader(body))
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: actor, PermissionRules: []security.PermissionRule{{Code: "user.file.single_limit.64", Allow: true}, {Code: "user.file.daily_limit.256", Allow: true}, {Code: "user.file.total_limit.256", Allow: true}}}))
	response := httptest.NewRecorder()
	start := time.Now()
	server.completeUserOSSDirectUpload(response, request)
	elapsed := time.Since(start)
	var files, intents, rawCleanup int
	var sealedSource, convertedKey string
	if err = pool.QueryRow(ctx, `select sealed_source_key,stored_object_key from oss_upload_byte_bindings where upload_object_key=$1 and owner_id=$2 and status='cleanup_pending' and file_id is null`, key, actor).Scan(&sealedSource, &convertedKey); err != nil {
		t.Fatal(err)
	}
	if sealedSource != copiedSource.Load().(string) || convertedKey != processedObject.Load().(string) || convertedKey != persistedWebPObjectKey(sealedSource) {
		t.Fatal("provider source/result did not match the exact unadopted receipt")
	}
	if err = pool.QueryRow(ctx, `select (select count(*) from oss_files where uploader_id=$1),(select count(*) from oss_object_deletion_outbox where object_key=any($2::text[]) and status='pending'),(select count(*) from oss_object_deletion_outbox where object_key=$3)`, actor, []string{sealedSource, convertedKey}, key).Scan(&files, &intents, &rawCleanup); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "保存 OSS 文件记录失败") || elapsed > 2*time.Second || files != 0 || intents != 2 || rawCleanup != 0 || processes.Load() != 1 {
		t.Fatalf("registration recovery status=%d elapsed=%s files=%d compensation=%d rawCleanup=%d conversionCalls=%d", response.Code, elapsed, files, intents, rawCleanup, processes.Load())
	}
	if _, err = pool.Exec(ctx, `alter table oss_files drop constraint oct02_reject_converted_registration`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `select 1`); err != nil {
		t.Fatal("connection was not reusable after failed settlement")
	}
}
