package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
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
	convertedKey := persistedWebPObjectKey(key)
	var processes atomic.Int64
	// Only the external OSS protocol is simulated. The handler, quota transaction,
	// failed insert and durable compensation all use the actual PostgreSQL schema.
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			processes.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"OK"}`))
		case http.MethodHead:
			w.Header().Set("ETag", `"synthetic"`)
			w.Header().Set("x-oss-meta-sha256", hash)
			if strings.HasSuffix(r.URL.Path, ".webp") {
				w.Header().Set("Content-Type", "image/webp")
				w.Header().Set("Content-Length", "32")
			} else {
				w.Header().Set("Content-Type", "image/png")
				w.Header().Set("Content-Length", fmt.Sprint(source.Len()))
			}
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
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
	var files, intents int
	if err = pool.QueryRow(ctx, `select (select count(*) from oss_files where uploader_id=$1),(select count(*) from oss_object_deletion_outbox where object_key=any($2::text[]) and status='pending')`, actor, []string{key, convertedKey}).Scan(&files, &intents); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusInternalServerError || elapsed > 2*time.Second || files != 0 || intents != 2 || processes.Load() != 1 {
		t.Fatalf("registration recovery status=%d elapsed=%s files=%d compensation=%d conversionCalls=%d", response.Code, elapsed, files, intents, processes.Load())
	}
	if _, err = pool.Exec(ctx, `alter table oss_files drop constraint oct02_reject_converted_registration`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `select 1`); err != nil {
		t.Fatal("connection was not reusable after failed settlement")
	}
}
