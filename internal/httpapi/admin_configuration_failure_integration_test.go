package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/queue"
	"mcmods-cn-backend/internal/security"
)

func TestAdminNATSAndTemplateFailureStateIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	var actor int64
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash)
		values('configfailureaudit','configfailureaudit@example.invalid','synthetic') returning id`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	ctx = context.WithValue(ctx, claimsContextKey, security.Claims{Subject: actor})

	t.Run("saved NATS configuration reports failed local handshake", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		stopped := make(chan struct{})
		go func() {
			defer close(stopped)
			for {
				connection, err := listener.Accept()
				if err != nil {
					return
				}
				// A controlled local connection loss exercises the real NATS client.
				// No shared NATS server or external service is reconfigured.
				_ = connection.Close()
			}
		}()
		t.Cleanup(func() {
			_ = listener.Close()
			<-stopped
		})
		localCfg := cfg
		localCfg.NATS = config.NATSConfig{Enabled: false, URL: "nats://127.0.0.1:4222"}
		queueCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		client := queue.New(queueCtx, localCfg.NATS)
		defer client.Close()
		server := &Server{db: pool, cfg: localCfg, queue: client}
		payload := config.NATSConfig{
			Enabled: true, URL: "nats://" + listener.Addr().String(),
			Username: "synthetic-nats-user", Password: "synthetic-nats-password",
			SubjectPrefix: "configfailureaudit",
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		server.updateNATSConfig(response, httptest.NewRequest(http.MethodPut, "/api/v1/admin/config/nats", bytes.NewReader(raw)).WithContext(ctx))
		var result struct {
			Code    string `json:"code"`
			Details struct {
				Saved bool `json:"saved"`
			} `json:"details"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusServiceUnavailable || result.Code != "nats_connection_failed" || !result.Details.Saved {
			t.Fatalf("connection failure: status=%d code=%q saved=%v", response.Code, result.Code, result.Details.Saved)
		}
		if strings.Contains(response.Body.String(), payload.Password) {
			t.Fatal("NATS failure exposed the synthetic password")
		}
		stored, err := database.LoadNATSConfig(ctx, pool, localCfg.NATS, cfg.SettingsEncryptionKey)
		if err != nil {
			t.Fatal(err)
		}
		if !stored.Enabled || stored.URL != payload.URL || stored.SubjectPrefix != payload.SubjectPrefix || stored.Password != payload.Password {
			t.Fatal("saved configuration was lost after connection failure")
		}
		response = httptest.NewRecorder()
		server.getNATSConfig(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/config/nats", nil).WithContext(ctx))
		var fetched struct {
			Data natsConfigResponse `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &fetched); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusOK || !fetched.Data.Enabled || fetched.Data.Status.Connected || !fetched.Data.HasPassword || fetched.Data.URL != payload.URL {
			t.Fatalf("saved disconnected configuration not recoverable: status=%d", response.Code)
		}
		if strings.Contains(response.Body.String(), payload.Password) {
			t.Fatal("NATS GET exposed the synthetic password")
		}
	})

	server := &Server{db: pool, cfg: cfg}
	templatePayload, err := json.Marshal(defaultNotificationTemplateConfig())
	if err != nil {
		t.Fatal(err)
	}
	type settingSnapshot struct {
		value     string
		updatedAt time.Time
		updatedBy int64
	}
	readSetting := func(t *testing.T, key string) settingSnapshot {
		t.Helper()
		var value settingSnapshot
		if err := pool.QueryRow(ctx, `select value::text,updated_at,coalesce(updated_by,0)
			from system_settings where key=$1`, key).Scan(&value.value, &value.updatedAt, &value.updatedBy); err != nil {
			t.Fatal(err)
		}
		return value
	}
	setSetting := func(t *testing.T, key, value string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `insert into system_settings(key,value,updated_by) values($1,$2::jsonb,$3)
			on conflict(key) do update set value=excluded.value,updated_by=excluded.updated_by,updated_at=now()`, key, value, actor); err != nil {
			t.Fatal(err)
		}
	}
	assertUnavailable := func(t *testing.T, target *Server) {
		t.Helper()
		for _, method := range []string{http.MethodGet, http.MethodPut} {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(method, "/api/v1/admin/config/notifications", bytes.NewReader(templatePayload)).WithContext(ctx)
			if method == http.MethodGet {
				target.getNotificationTemplates(response, request)
			} else {
				target.updateNotificationTemplates(response, request)
			}
			if response.Code != http.StatusServiceUnavailable {
				t.Errorf("notification %s failure status=%d wanted503", method, response.Code)
			}
		}
		response := httptest.NewRecorder()
		target.getReviewConfig(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/config/reviews", nil).WithContext(ctx))
		if response.Code != http.StatusServiceUnavailable {
			t.Errorf("review failure status=%d wanted503", response.Code)
		}
	}
	for _, scenario := range []struct {
		name, templates, review string
	}{
		{"null configuration", `null`, `null`},
		{"array configuration", `[]`, `[]`},
		{"wrong field type", `{"templates":"synthetic-invalid"}`, `{"aiTranslation":"synthetic-invalid"}`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			setSetting(t, notificationTemplatesSettingKey, scenario.templates)
			setSetting(t, reviewConfigSettingKey, scenario.review)
			beforeTemplates, beforeReview := readSetting(t, notificationTemplatesSettingKey), readSetting(t, reviewConfigSettingKey)
			assertUnavailable(t, server)
			if readSetting(t, notificationTemplatesSettingKey) != beforeTemplates || readSetting(t, reviewConfigSettingKey) != beforeReview {
				t.Fatal("unreadable configuration value or audit timestamp was overwritten")
			}
			fallback := loadReviewConfig(ctx, pool)
			if !fallback.BlueprintCreate || !fallback.BlueprintEdit || !fallback.ModContentSectionCreate || !fallback.AITranslation {
				t.Fatal("malformed review configuration disabled a conservative moderation boundary")
			}
		})
	}
	t.Run("healthy templates recover after configuration correction", func(t *testing.T) {
		setSetting(t, notificationTemplatesSettingKey, string(templatePayload))
		next := defaultNotificationTemplateConfig()
		code := next.Templates[0].Code
		translation := next.Templates[0].Translations["en-US"]
		translation.Title = "Synthetic recovered template"
		next.Templates[0].Translations["en-US"] = translation
		raw, err := json.Marshal(next)
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		server.updateNotificationTemplates(response, httptest.NewRequest(http.MethodPut, "/api/v1/admin/config/notifications", bytes.NewReader(raw)).WithContext(ctx))
		if response.Code != http.StatusOK {
			t.Fatalf("healthy template save status=%d wanted200", response.Code)
		}
		stored, err := loadNotificationTemplateConfigChecked(ctx, pool)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range stored.Templates {
			if item.Code == code {
				found = item.Translations["en-US"].Title == translation.Title && item.Version == 2
			}
		}
		if !found {
			t.Fatal("corrected template was not persisted with its next version")
		}
	})
	t.Run("closed pool does not overwrite readable settings", func(t *testing.T) {
		setSetting(t, notificationTemplatesSettingKey, string(templatePayload))
		setSetting(t, reviewConfigSettingKey, `{"aiTranslation":true}`)
		beforeTemplates, beforeReview := readSetting(t, notificationTemplatesSettingKey), readSetting(t, reviewConfigSettingKey)
		closed, err := pgxpool.NewWithConfig(ctx, pool.Config().Copy())
		if err != nil {
			t.Fatal(err)
		}
		closed.Close()
		assertUnavailable(t, &Server{db: closed, cfg: cfg})
		if readSetting(t, notificationTemplatesSettingKey) != beforeTemplates || readSetting(t, reviewConfigSettingKey) != beforeReview {
			t.Fatal("database failure changed settings observed through the healthy pool")
		}
	})
}
