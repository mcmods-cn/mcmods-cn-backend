package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestOCT02AISettingsReadFailureCannotReplaceEncryptedConfigurationIntegration(t *testing.T) {
	pool := oct02AITestPool(t)
	if _, err := pool.Exec(context.Background(), `alter table system_settings add column updated_by bigint,
	 add column updated_at timestamptz default now()`); err != nil {
		t.Fatal(err)
	}
	owner := &Server{db: pool, cfg: config.Config{SettingsEncryptionKey: strings.Repeat("owner-fixture-key", 3)}}
	cfg := defaultAIConfig()
	cfg.Providers[0].APIKey = "synthetic-secret-never-sent"
	raw, err := owner.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(context.Background(), `insert into system_settings(key,value) values('ai.config',$1)`, raw); err != nil {
		t.Fatal(err)
	}
	var before, after []byte
	if err = pool.QueryRow(context.Background(), `select value from system_settings where key='ai.config'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	// A wrong task-specific key cannot decrypt the persisted configuration. The
	// previous implementation silently returned defaults, then saved an empty key.
	server := &Server{db: pool, cfg: config.Config{SettingsEncryptionKey: strings.Repeat("other-fixture-key", 3)}}
	getResponse := httptest.NewRecorder()
	server.getAIConfig(getResponse, httptest.NewRequest(http.MethodGet, "/api/v1/admin/ai/config", nil))
	cfg.Providers[0].APIKey = ""
	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	putResponse := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/ai/config", bytes.NewReader(body))
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{Subject: 42}))
	server.updateAIConfig(putResponse, request)
	if err = pool.QueryRow(context.Background(), `select value from system_settings where key='ai.config'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if getResponse.Code != http.StatusServiceUnavailable || putResponse.Code != http.StatusServiceUnavailable || !bytes.Equal(before, after) {
		t.Fatalf("unreadable settings were displayed/replaced: get=%d put=%d unchanged=%t", getResponse.Code, putResponse.Code, bytes.Equal(before, after))
	}
}

func TestOCT02AISettingsDefaultsAndSavingStayWithinOneConnectionIntegration(t *testing.T) {
	owner := oct02AITestPool(t)
	poolConfig := owner.Config()
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err = pool.Exec(ctx, `alter table system_settings add column updated_by bigint,
	 add column updated_at timestamptz default now()`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool, cfg: config.Config{SettingsEncryptionKey: strings.Repeat("fixture-default", 3)}}
	response := httptest.NewRecorder()
	server.getAIConfig(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/ai/config", nil).WithContext(ctx))
	if response.Code != http.StatusOK {
		t.Fatalf("missing config did not use disabled defaults: %d", response.Code)
	}
	cfg := defaultAIConfig()
	cfg.Providers[0].APIKey = "synthetic-key"
	for i := 0; i < 2; i++ {
		body, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/ai/config", bytes.NewReader(body)).WithContext(
			context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 42}))
		response = httptest.NewRecorder()
		server.updateAIConfig(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("MaxConns1 save failed: status=%d body=%s", response.Code, response.Body.String())
		}
		cfg.Providers[0].APIKey = "" // Redacted roundtrip must retain the current key.
	}
	stored, err := readAIConfigFromSettings(ctx, pool, server.cfg.SettingsEncryptionKey)
	if err != nil || stored.Providers[0].APIKey != "synthetic-key" {
		t.Fatalf("hidden key was lost: error=%v", err)
	}
}

func TestOCT02AISettingsRejectDuplicateNormalizedProviderCodesIntegration(t *testing.T) {
	pool := oct02AITestPool(t)
	cfg := defaultAIConfig()
	cfg.Providers[1].Code = " OpenAI "
	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	(&Server{db: pool}).updateAIConfig(response, httptest.NewRequest(http.MethodPut, "/api/v1/admin/ai/config", bytes.NewReader(body)))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("ambiguous provider configuration accepted: %d", response.Code)
	}
	var count int
	if err = pool.QueryRow(context.Background(), `select count(*) from system_settings`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid config reached storage: count=%d error=%v", count, err)
	}
}

func TestOCT02AISettingsDatabaseFailureIsUnavailable(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://fixture@127.0.0.1:1/unreachable_fixture?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close() // Failure is local; never make a network connection.
	server := &Server{db: pool}
	response := httptest.NewRecorder()
	server.getAIConfig(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/ai/config", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("closed pool settings read returned success: %d", response.Code)
	}
	if cfg := server.aiConfigFromSettings(context.Background()); cfg.Translation.Enabled || cfg.Providers[0].Enabled {
		t.Fatal("execution fallback enabled external requests")
	}
}

type oct02AISettingsReadBarrier struct {
	once   sync.Once
	loaded chan struct{}
	resume chan struct{}
}

type oct02AISettingsMetadataKey struct{}
type oct02AISettingsReadKey struct{}

func (barrier *oct02AISettingsReadBarrier) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, oct02AISettingsReadKey{}, ctx.Value(oct02AISettingsMetadataKey{}) == true &&
		strings.Contains(data.SQL, "select value from system_settings"))
}

func (barrier *oct02AISettingsReadBarrier) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if ctx.Value(oct02AISettingsReadKey{}) == true {
		barrier.once.Do(func() {
			close(barrier.loaded)
			select {
			case <-barrier.resume:
			case <-ctx.Done():
			}
		})
	}
}

func TestOCT02AISettingsHiddenKeyMergeSerializesWithRotationIntegration(t *testing.T) {
	owner := oct02AITestPool(t)
	barrier := &oct02AISettingsReadBarrier{loaded: make(chan struct{}), resume: make(chan struct{})}
	poolConfig := owner.Config()
	poolConfig.ConnConfig.Tracer = barrier
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer func() {
		select {
		case <-barrier.resume:
		default:
			close(barrier.resume)
		}
	}()
	if _, err = pool.Exec(ctx, `alter table system_settings add column updated_by bigint,
	 add column updated_at timestamptz default now()`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool, cfg: config.Config{SettingsEncryptionKey: strings.Repeat("fixture-rotation", 3)}}
	initial := defaultAIConfig()
	initial.Providers[0].APIKey = "synthetic-old-key"
	raw, err := server.sealSystemSetting(initial)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('ai.config',$1)`, raw); err != nil {
		t.Fatal(err)
	}
	invoke := func(ctx context.Context, key string, output chan<- int) {
		cfg := defaultAIConfig()
		cfg.Providers[0].APIKey = key
		body, err := json.Marshal(cfg)
		if err != nil {
			output <- 0
			return
		}
		request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/ai/config", bytes.NewReader(body)).WithContext(
			context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 42}))
		response := httptest.NewRecorder()
		server.updateAIConfig(response, request)
		output <- response.Code
	}
	metadata, rotation := make(chan int, 1), make(chan int, 1)
	go invoke(context.WithValue(ctx, oct02AISettingsMetadataKey{}, true), "", metadata)
	select {
	case <-barrier.loaded:
	case <-ctx.Done():
		t.Fatal("metadata configuration did not reach controlled read")
	}
	go invoke(ctx, "synthetic-rotated-key", rotation)
	// Rotation must wait on the settings transaction while metadata holds the
	// old snapshot. On the old implementation it commits and is then overwritten.
	select {
	case status := <-rotation:
		t.Fatalf("rotation bypassed in-flight hidden-key merge: status=%d", status)
	case <-time.After(150 * time.Millisecond):
	}
	close(barrier.resume)
	for name, output := range map[string]<-chan int{"metadata": metadata, "rotation": rotation} {
		select {
		case status := <-output:
			if status != http.StatusOK {
				t.Fatalf("%s update failed: %d", name, status)
			}
		case <-ctx.Done():
			t.Fatalf("%s update did not finish", name)
		}
	}
	stored, err := readAIConfigFromSettings(ctx, pool, server.cfg.SettingsEncryptionKey)
	if err != nil || stored.Providers[0].APIKey != "synthetic-rotated-key" {
		t.Fatalf("concurrent key rotation was overwritten: error=%v", err)
	}
}
