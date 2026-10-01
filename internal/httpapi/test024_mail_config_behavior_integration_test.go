package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/mailer"
	"mcmods-cn-backend/internal/queue"
	"mcmods-cn-backend/internal/security"
)

type test024MailFixture struct {
	ctx           context.Context
	db            *pgxpool.Pool
	server, peer  *Server
	origin        *httptest.Server
	token, denied string
	actorID       int64
	cfg           config.Config
}

func newTEST024MailFixture(t *testing.T) test024MailFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	db := newTEST018IsolatedDatabase(t, ctx)
	cfg := config.Load()
	cfg.JWTSecret = "test024-mail-only-signing-secret-at-least-32"
	cfg.SettingsEncryptionKey = "test024-mail-only-encryption-secret-at-least-32"
	cfg.SMTP = config.SMTPConfig{Enabled: true, Host: "fallback.invalid", Port: 587, Username: "fallback", Password: "fallback-secret", From: "sender@example.test", UseTLS: true}
	cfg.Redis.Enabled, cfg.AntiAbuse.Enabled = false, false
	cfg.NATS = config.NATSConfig{}
	q := queue.New(ctx, cfg.NATS)
	t.Cleanup(q.Close)
	server := NewServer(ctx, cfg, db, q, nil, nil, nil)
	peer := NewServer(ctx, cfg, db, q, nil, nil, nil)
	t.Cleanup(func() {
		for _, instance := range []*Server{server, peer} {
			shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
			if err := instance.Shutdown(shutdown); err != nil {
				t.Error(err)
			}
			stop()
			instance.cache.Close()
		}
	})
	origin := httptest.NewServer(server)
	t.Cleanup(origin.Close)
	actorID, _, token := createTEST044User(t, ctx, db, cfg, "test024-mail", "UTC")
	grantTEST044Permissions(t, ctx, db, actorID, "admin.config.read", "mail.write")
	_, _, denied := createTEST044User(t, ctx, db, cfg, "test024-mail-denied", "UTC")
	return test024MailFixture{ctx: ctx, db: db, server: server, peer: peer, origin: origin, token: token, denied: denied, actorID: actorID, cfg: cfg}
}

func (f test024MailFixture) call(auth, method, path string, body any) (int, []byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	request, err := http.NewRequestWithContext(f.ctx, method, f.origin.URL+path, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if auth != "" {
		request.Header.Set("Authorization", "Bearer "+auth)
	}
	response, err := f.origin.Client().Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	raw, err = io.ReadAll(response.Body)
	return response.StatusCode, raw, err
}

func (f test024MailFixture) require(t *testing.T, auth, method, path string, body any, want int) []byte {
	t.Helper()
	status, raw, err := f.call(auth, method, path, body)
	if err != nil || status != want {
		t.Fatalf("%s %s status=%d want=%d err=%v body=%s", method, path, status, want, err, raw)
	}
	return raw
}

func (f test024MailFixture) active(t *testing.T, runtime interface {
	activeMailer(context.Context) (mailer.Mailer, error)
}) mailer.Mailer {
	t.Helper()
	active, err := runtime.activeMailer(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	return active
}

func TestTEST024MailConfigurationHTTPEncryptionVersionAndConcurrentUpdatesIntegration(t *testing.T) {
	f := newTEST024MailFixture(t)
	path := "/api/v1/admin/config/mail"
	payload := mailConfigPayload{Enabled: false, Host: "localhost", Port: 2525, Username: "test024", Password: "owned-mail-password", From: "sender@example.test", UseTLS: true}
	version := func() int64 {
		t.Helper()
		var value int64
		if err := f.db.QueryRow(f.ctx, `select version from runtime_versions where name='settings'`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	initial := version()
	f.require(t, "", http.MethodPut, path, payload, http.StatusUnauthorized)
	f.require(t, f.denied, http.MethodPut, path, payload, http.StatusForbidden)
	if version() != initial {
		t.Fatal("unauthorized update changed runtime version")
	}
	invalid := payload
	invalid.Enabled = true
	invalid.Host = ""
	f.require(t, f.token, http.MethodPut, path, invalid, http.StatusBadRequest)
	invalid.Host = "localhost"
	invalid.Port = 65536
	f.require(t, f.token, http.MethodPut, path, invalid, http.StatusBadRequest)
	if version() != initial {
		t.Fatal("invalid update changed runtime version")
	}
	response := f.require(t, f.token, http.MethodPut, path, payload, http.StatusOK)
	if bytes.Contains(response, []byte(payload.Password)) || !bytes.Contains(response, []byte(`"hasPassword":true`)) {
		t.Fatal("HTTP response must expose only password presence")
	}
	var stored []byte
	var actor int64
	if err := f.db.QueryRow(f.ctx, `select value,updated_by from system_settings where key='mail.smtp'`).Scan(&stored, &actor); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte(payload.Password)) || actor != f.actorID || version() <= initial {
		t.Fatal("setting must be sealed, attributed and versioned")
	}
	var opened mailConfigPayload
	if err := f.server.openSystemSetting(stored, &opened); err != nil || opened != payload {
		t.Fatalf("encrypted setting round trip: %v", err)
	}
	worker := NewNotificationWorker(f.db, nil, nil, f.cfg.SMTP, f.cfg.SettingsEncryptionKey)
	if f.active(t, f.server).Enabled() || f.active(t, f.peer).Enabled() || f.active(t, worker).Enabled() {
		t.Fatal("persisted disabled must override configured fallback across every runtime")
	}
	response = f.require(t, f.token, http.MethodGet, "/api/v1/admin/config", nil, http.StatusOK)
	if bytes.Contains(response, []byte(payload.Password)) || bytes.Contains(response, []byte(f.cfg.SMTP.Password)) {
		t.Fatal("aggregate configuration exposed mail credentials")
	}
	payload.Enabled = true
	payload.Host = "127.0.0.1"
	payload.Password = ""
	f.require(t, f.token, http.MethodPut, path, payload, http.StatusOK)
	if got := f.active(t, f.peer); got.Config.Password != "owned-mail-password" || !got.Enabled() || got.Config.Host != "127.0.0.1" {
		t.Fatal("omitted secret and changed configuration must be visible immediately on another instance")
	}
	beforeFailure := version()
	if _, err := f.db.Exec(f.ctx, `alter table system_settings add constraint test024_reject_mail check (key<>'mail.smtp' or updated_by<>`+fmt.Sprint(f.actorID)+`) not valid`); err != nil {
		t.Fatal(err)
	}
	payload.Host = "rejected.invalid"
	payload.Password = "rejected-password"
	f.require(t, f.token, http.MethodPut, path, payload, http.StatusInternalServerError)
	if version() != beforeFailure || f.active(t, f.peer).Config.Host != "127.0.0.1" {
		t.Fatal("failed persistence changed version or runtime configuration")
	}
	if _, err := f.db.Exec(f.ctx, `alter table system_settings drop constraint test024_reject_mail`); err != nil {
		t.Fatal(err)
	}
	const total = 24
	start := make(chan struct{})
	results := make(chan error, total)
	var group sync.WaitGroup
	for ordinal := 0; ordinal < total; ordinal++ {
		group.Add(1)
		go func(ordinal int) {
			defer group.Done()
			<-start
			candidate := mailConfigPayload{Enabled: false, Host: fmt.Sprintf("owned-%d.invalid", ordinal), Port: 2525, Username: fmt.Sprintf("actor-%d", ordinal), Password: fmt.Sprintf("owned-password-%d", ordinal), From: "sender@example.test", UseTLS: true}
			status, raw, err := f.call(f.token, http.MethodPut, path, candidate)
			if err == nil && (status != http.StatusOK || bytes.Contains(raw, []byte(candidate.Password))) {
				err = fmt.Errorf("concurrent update status=%d or secret exposure", status)
			}
			results <- err
		}(ordinal)
	}
	close(start)
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	// INSERT ... ON CONFLICT DO UPDATE fires both statement-level INSERT and
	// UPDATE triggers in the authoritative schema, including the zero-row arm.
	if version() != beforeFailure+2*total {
		t.Fatal("each successful upsert must advance both authoritative statement triggers")
	}
	if err := f.db.QueryRow(f.ctx, `select value from system_settings where key='mail.smtp'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if err := f.server.openSystemSetting(stored, &opened); err != nil {
		t.Fatal(err)
	}
	for _, runtime := range []config.SMTPConfig{f.active(t, f.server).Config, f.active(t, f.peer).Config, f.active(t, worker).Config} {
		if runtime != smtpConfigFromPayload(opened) {
			t.Fatal("runtime must use one complete committed configuration, not torn in-memory fields")
		}
	}
	for round := 0; round < 8; round++ {
		rotated := fmt.Sprintf("owned-rotated-secret-%d", round)
		start := make(chan struct{})
		results := make(chan error, 2)
		for _, password := range []string{rotated, ""} {
			go func(password string) {
				<-start
				candidate := payload
				candidate.Enabled, candidate.Host, candidate.Password = false, "localhost", password
				status, _, err := f.call(f.token, http.MethodPut, path, candidate)
				if err == nil && status != http.StatusOK {
					err = fmt.Errorf("secret retention race status=%d", status)
				}
				results <- err
			}(password)
		}
		close(start)
		for ordinal := 0; ordinal < 2; ordinal++ {
			if err := <-results; err != nil {
				t.Fatal(err)
			}
		}
		if got := f.active(t, f.peer).Config.Password; got != rotated {
			t.Fatal("omitted password raced with rotation and restored an obsolete secret")
		}
	}
}

func TestTEST024CorruptMailAndBrandConfigurationCannotFallBackToEnabledSuccessIntegration(t *testing.T) {
	f := newTEST024MailFixture(t)
	// Absence is the only legitimate environment fallback. A present but
	// unreadable row is an operational error, not authorization to send mail.
	if !f.active(t, f.server).Enabled() {
		t.Fatal("absent setting must retain explicit configured fallback")
	}
	ciphertext, err := security.EncryptSetting("another-owned-encryption-key-at-least-32", []byte(`{"enabled":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values('mail.smtp',$1::jsonb)`, ciphertext); err != nil {
		t.Fatal(err)
	}
	t.Run("HTTP_read", func(t *testing.T) {
		f.require(t, f.token, http.MethodGet, "/api/v1/admin/config", nil, http.StatusServiceUnavailable)
	})
	t.Run("server_fail_closed", func(t *testing.T) {
		if active, err := f.server.activeMailer(f.ctx); err == nil || active.Enabled() {
			t.Fatal("corrupt stored disable must not silently re-enable fallback SMTP")
		}
	})
	t.Run("worker_fail_closed", func(t *testing.T) {
		if active, err := NewNotificationWorker(f.db, nil, nil, f.cfg.SMTP, f.cfg.SettingsEncryptionKey).activeMailer(f.ctx); err == nil || active.Enabled() {
			t.Fatal("worker must not silently re-enable fallback SMTP")
		}
	})
	t.Run("omitted_secret", func(t *testing.T) {
		f.require(t, f.token, http.MethodPut, "/api/v1/admin/config/mail", mailConfigPayload{Enabled: false, Host: "localhost", Port: 2525, From: "sender@example.test"}, http.StatusServiceUnavailable)
	})
	t.Run("admin_test_mail", func(t *testing.T) {
		raw := f.require(t, f.token, http.MethodPost, "/api/v1/admin/mail/test", testMailRequest{To: "recipient@example.test"}, http.StatusServiceUnavailable)
		if !bytes.Contains(raw, []byte("MAIL_SETTINGS_UNAVAILABLE")) || bytes.Contains(raw, []byte(f.cfg.SMTP.Password)) {
			t.Fatal("mail failure must be coded and redacted")
		}
	})
	if _, err = f.db.Exec(f.ctx, `update users set email_verified=true where id=$1`, f.actorID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `insert into user_notification_settings(user_id,email_enabled) values($1,true) on conflict(user_id) do update set email_enabled=true`, f.actorID); err != nil {
		t.Fatal(err)
	}
	worker := NewNotificationWorker(f.db, nil, nil, f.cfg.SMTP, f.cfg.SettingsEncryptionKey)
	if err = worker.sendUserEmail(f.ctx, f.actorID, "subject", "body"); !errors.Is(err, errMailSettingsUnavailable) {
		t.Fatalf("eligible notification mail must propagate setting failure for durable retry: %v", err)
	}
	var email string
	if err = f.db.QueryRow(f.ctx, `select email from users where id=$1`, f.actorID).Scan(&email); err != nil {
		t.Fatal(err)
	}
	f.require(t, "", http.MethodPost, "/api/v1/auth/email-code", map[string]string{"email": email, "purpose": "login"}, http.StatusServiceUnavailable)
	var generated, usable int
	if err = f.db.QueryRow(f.ctx, `select count(*),count(*) filter(where consumed_at is null) from email_verification_codes where email=$1`, email).Scan(&generated, &usable); err != nil || generated != 1 || usable != 0 {
		t.Fatalf("failed mail must leave no usable verification code: generated=%d usable=%d err=%v", generated, usable, err)
	}
	for _, plaintext := range []string{"null", `{"enabled":true}`, `{"enabled":true,"host":17}`, `{"enabled":false,"password":17}`} {
		t.Run("malformed_"+plaintext, func(t *testing.T) {
			sealed, err := security.EncryptSetting(f.cfg.SettingsEncryptionKey, []byte(plaintext))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.db.Exec(f.ctx, `update system_settings set value=$1::jsonb where key='mail.smtp'`, sealed); err != nil {
				t.Fatal(err)
			}
			f.require(t, f.token, http.MethodGet, "/api/v1/admin/config", nil, http.StatusServiceUnavailable)
			if active, err := worker.activeMailer(f.ctx); err == nil || active.Enabled() {
				t.Fatal("malformed authoritative setting must fail closed")
			}
		})
	}
	if _, err = f.db.Exec(f.ctx, `delete from system_settings where key='mail.smtp'; insert into system_settings(key,value) values('site.general','{"siteName":17}'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.token, http.MethodGet, "/api/v1/admin/config", nil, http.StatusServiceUnavailable)
	f.require(t, "", http.MethodGet, "/api/v1/site/config", nil, http.StatusServiceUnavailable)
	// A real database read failure must also remain failure, without touching
	// shared/public data outside this random owned database.
	if _, err = f.db.Exec(f.ctx, `alter table system_settings rename to test024_owned_unavailable_settings`); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.token, http.MethodGet, "/api/v1/admin/config", nil, http.StatusServiceUnavailable)
	if active, err := f.server.activeMailer(f.ctx); err == nil || active.Enabled() {
		t.Fatal("database failure re-enabled environment mail")
	}
}
