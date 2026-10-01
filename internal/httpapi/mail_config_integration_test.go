package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/mailer"
)

func TestMailConfigurationRespectsDisabledAndKeepsFallbackImmutableIntegration(t *testing.T) {
	url := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("requires dedicated migrated PostgreSQL via MCMODS_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := pgx.Identifier{"apia_mail_" + randomHex(8)}.Sanitize()
	if _, err = admin.Exec(ctx, "create schema "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if _, err := admin.Exec(cleanup, "drop schema "+schema+" cascade"); err != nil {
			t.Errorf("allocated mail fixture cleanup: %v", err)
		}
	}()
	if _, err = admin.Exec(ctx, "create table "+schema+".system_settings (like public.system_settings including all)"); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	fallback := config.SMTPConfig{Host: "fallback.invalid", Port: 25, From: "fixture@example.test"}
	server := &Server{db: pool, mailer: mailer.New(fallback), cfg: config.Config{SMTP: fallback, SettingsEncryptionKey: randomHex(32)}}
	put := func(enabled bool) {
		t.Helper()
		body := fmt.Sprintf(`{"enabled":%t,"host":"saved.invalid","port":25,"from":"fixture@example.test"}`, enabled)
		req := httptest.NewRequest(http.MethodPut, "/mail-config", strings.NewReader(body)).WithContext(ctx)
		res := httptest.NewRecorder()
		server.updateMailConfig(res, req)
		if res.Code != http.StatusOK {
			t.Errorf("saving synthetic SMTP configuration failed: %d %s", res.Code, res.Body.String())
		}
	}
	put(false)
	if server.mailConfigFromSettings(ctx).Enabled || server.activeMailer(ctx).Enabled() {
		t.Fatal("disabled SMTP configuration became enabled merely because host/from exist")
	}
	put(true)
	if !server.activeMailer(ctx).Enabled() {
		t.Fatal("enabled SMTP configuration did not become active")
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 24 {
			put(i%2 == 0)
		}
	}()
	go func() {
		defer wg.Done()
		for range 48 {
			_ = server.mailConfigFromSettings(ctx)
			_ = server.activeMailer(ctx)
		}
	}()
	wg.Wait()
	if server.mailer.Config != fallback {
		t.Fatal("saving SMTP configuration mutated the concurrently read server fallback")
	}
	// No Mailer.Send call, SMTP connection, or real email occurs in this test.
}
