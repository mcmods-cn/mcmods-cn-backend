package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

// Only the external OSS HEAD/DELETE boundary is simulated. Registration,
// constraints, quota accounting and concurrent transactions use PostgreSQL.
func ossUploadTestServer(t *testing.T, headSize int64) (*Server, security.Claims) {
	t.Helper()
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	schema := "oss_upload_" + randomHex(8)
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err = pool.Exec(ctx, "create schema "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	// Isolate the synthetic OSS configuration without replacing global settings
	// used by other integration fixtures.
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, "drop schema "+quotedSchema+" cascade"); err != nil {
			t.Error(err)
		}
	})
	if _, err = pool.Exec(ctx, "create table "+quotedSchema+".system_settings (like public.system_settings including all)"); err != nil {
		t.Fatal(err)
	}
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	appPool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(appPool.Close)
	unique := fmt.Sprintf("oss_upload_%d", time.Now().UnixNano())
	var userID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash) values($1,$2,'test') returning id`, unique, unique+"@example.test").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Explicit cleanup is necessary because uploader_id uses SET NULL.
		for _, statement := range []string{
			`delete from oss_upload_logs where uploader_id=$1`,
			`delete from oss_files where uploader_id=$1`,
			`delete from users where id=$1`,
		} {
			if _, err := pool.Exec(ctx, statement, userID); err != nil {
				t.Error(err)
			}
		}
	})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.Header().Set("Content-Length", strconv.FormatInt(headSize, 10))
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("x-oss-meta-sha256", strings.Repeat("a", 64))
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(endpoint.Close)
	ossConfig := defaultOSSConfig()
	ossConfig.Enabled, ossConfig.UseCName = true, true
	ossConfig.Region, ossConfig.Bucket = "cn-test", "test-only"
	ossConfig.Endpoint, ossConfig.PublicEndpoint = endpoint.URL, endpoint.URL
	ossConfig.AccessKeyID, ossConfig.AccessKeySecret = "test-key", "test-secret"
	ossConfig.DownloadURLMode = ossDownloadModePresigned
	server := &Server{db: appPool, cfg: config.Config{SettingsEncryptionKey: randomHex(32)}}
	raw, err := server.sealSystemSetting(ossConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = appPool.Exec(ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb)`, raw); err != nil {
		t.Fatal(err)
	}
	claims := security.Claims{Subject: userID, PermissionRules: []security.PermissionRule{
		{Code: "user.file.single_limit.32", Allow: true, Priority: 100},
		{Code: "user.file.daily_limit.32", Allow: true, Priority: 100},
		{Code: "user.file.total_limit.32", Allow: true, Priority: 100},
	}}
	return server, claims
}

func ossUploadTestRequest(t *testing.T, body any, claims security.Claims) *http.Request {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/projects/test/files/uploads/complete", bytes.NewReader(raw))
	return r.WithContext(context.WithValue(r.Context(), claimsContextKey, claims))
}

func TestOSSProjectUploadRegistersReleaseFormatsIntegration(t *testing.T) {
	s, claims := ossUploadTestServer(t, 1024)
	for _, test := range []struct{ projectType, extension string }{
		{"modpack", ".mrpack"}, {"map", ".zip"}, {"shader_pack", ".zip"}, {"addon", ".jar"},
	} {
		t.Run(test.projectType, func(t *testing.T) {
			scope := ossProjectDownloadScope(test.projectType, "abc123xyz")
			response := httptest.NewRecorder()
			s.createOSSDirectUploadWithScope(response, ossUploadTestRequest(t, ossDirectUploadRequest{
				OriginalName: "release" + test.extension, SizeBytes: 1024, SHA256: strings.Repeat("a", 64),
			}, claims), scope)
			if response.Code != http.StatusOK {
				t.Fatalf("presign status=%d body=%s", response.Code, response.Body.String())
			}
			var signed struct {
				Data struct{ ObjectKey string } `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &signed); err != nil || signed.Data.ObjectKey == "" {
				t.Fatalf("invalid presign response: %s err=%v", response.Body.String(), err)
			}
			response = httptest.NewRecorder()
			s.completeOSSDirectUploadWithScope(response, ossUploadTestRequest(t, ossCompleteUploadRequest{
				ObjectKey: signed.Data.ObjectKey, OriginalName: "release" + test.extension, SizeBytes: 1024, SHA256: strings.Repeat("a", 64),
			}, claims), scope)
			if response.Code != http.StatusCreated {
				t.Fatalf("complete status=%d body=%s", response.Code, response.Body.String())
			}
			var count int
			if err := s.db.QueryRow(context.Background(), `select count(*) from oss_files where uploader_id=$1 and object_key=$2 and status='active'`, claims.Subject, signed.Data.ObjectKey).Scan(&count); err != nil || count != 1 {
				t.Fatalf("file was not durably registered: count=%d err=%v", count, err)
			}
		})
	}
}

func TestOSSCompletionChecksActualObjectSizeIntegration(t *testing.T) {
	s, claims := ossUploadTestServer(t, maxOSSUploadBytes+1)
	response := httptest.NewRecorder()
	s.completeOSSDirectUploadWithScope(response, ossUploadTestRequest(t, ossCompleteUploadRequest{
		ObjectKey: "mcmods/" + ossProjectReleaseCategory("mod", "abc123xyz") + "/oversized.jar", OriginalName: "oversized.jar", SHA256: strings.Repeat("a", 64),
		// An omitted size must not bypass the actual HEAD size limit.
	}, claims), ossProjectDownloadScope("mod", "abc123xyz"))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "上传内容过大") {
		t.Fatalf("oversized object status=%d body=%s", response.Code, response.Body.String())
	}
	var count int
	if err := s.db.QueryRow(context.Background(), `select count(*) from oss_files where uploader_id=$1`, claims.Subject).Scan(&count); err != nil || count != 0 {
		t.Fatalf("oversized object was registered: count=%d err=%v", count, err)
	}
}

func TestOSSConcurrentProjectCompletionsRespectTotalQuotaIntegration(t *testing.T) {
	const objectSize = 600 << 10
	s, claims := ossUploadTestServer(t, objectSize)
	claims.PermissionRules[2].Code = "user.file.total_limit.1"
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	var workers sync.WaitGroup
	for i := 0; i < 2; i++ {
		r := ossUploadTestRequest(t, ossCompleteUploadRequest{
			ObjectKey: fmt.Sprintf("mcmods/%s/quota_%d.jar", ossProjectReleaseCategory("mod", "abc123xyz"), i), OriginalName: "quota.jar", SHA256: strings.Repeat("a", 64), SizeBytes: objectSize,
		}, claims)
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			response := httptest.NewRecorder()
			s.completeOSSDirectUploadWithScope(response, r, ossProjectDownloadScope("mod", "abc123xyz"))
			results <- response
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	var succeeded, rejected int
	for response := range results {
		switch response.Code {
		case http.StatusCreated:
			succeeded++
		case http.StatusForbidden:
			rejected++
		default:
			t.Fatalf("unexpected completion status=%d body=%s", response.Code, response.Body.String())
		}
	}
	var stored int64
	if err := s.db.QueryRow(context.Background(), `select coalesce(sum(size_bytes),0) from oss_files where uploader_id=$1 and status='active'`, claims.Subject).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if succeeded != 1 || rejected != 1 || stored != objectSize {
		t.Fatalf("quota violated: successful=%d rejected=%d stored=%d", succeeded, rejected, stored)
	}
}
