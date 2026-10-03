package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestOCT02OSSConfigReadFailureCannotEraseSavedCredentialsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := newOCT02IsolatedDatabase(t, ctx)
	var actor int64
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values('oss-config-reader','oss-config-reader@example.invalid','unused') returning id`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool, cfg: config.Config{SettingsEncryptionKey: "synthetic-good-settings-key-32-characters"}}
	stored := ossConfigPayload{Enabled: true, Region: "cn-test", Endpoint: "https://oss.example.invalid", Bucket: "synthetic", AccessKeyID: "synthetic-access-id", AccessKeySecret: "synthetic-saved-secret", SecurityToken: "synthetic-saved-token"}
	raw, err := server.sealSystemSetting(stored)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb) on conflict(key) do update set value=excluded.value`, raw); err != nil {
		t.Fatal(err)
	}
	var before []byte
	if err = pool.QueryRow(ctx, `select value from system_settings where key='oss.aliyun'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	server.cfg.SettingsEncryptionKey = "synthetic-wrong-settings-key-32-characters"
	response := httptest.NewRecorder()
	server.getOSSConfig(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/oss/config", nil).WithContext(ctx))
	if response.Code != http.StatusServiceUnavailable {
		t.Errorf("unreadable stored configuration GET status=%d want503", response.Code)
	}
	payload, _ := json.Marshal(ossConfigPayload{Enabled: false, Region: stored.Region, Endpoint: stored.Endpoint, Bucket: stored.Bucket, AccessKeyID: stored.AccessKeyID})
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/oss/config", bytes.NewReader(payload)).WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: actor}))
	response = httptest.NewRecorder()
	server.updateOSSConfig(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Errorf("unreadable config PUT status=%d want503", response.Code)
	}
	if strings.Contains(response.Body.String(), "synthetic-") || strings.Contains(response.Body.String(), "cipher") {
		t.Error("secret or encryption internals disclosed")
	}
	var after []byte
	if err = pool.QueryRow(ctx, `select value from system_settings where key='oss.aliyun'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("failed configuration read overwrote saved credentials")
	}
	server.cfg.SettingsEncryptionKey = "synthetic-good-settings-key-32-characters"
	request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/oss/config", bytes.NewReader(payload)).WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: actor}))
	response = httptest.NewRecorder()
	server.updateOSSConfig(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("recovered config PUT status=%d", response.Code)
	}
	if err = pool.QueryRow(ctx, `select value from system_settings where key='oss.aliyun'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	var retained ossConfigPayload
	if err = server.openSystemSetting(after, &retained); err != nil || retained.Enabled || retained.AccessKeySecret != stored.AccessKeySecret || retained.SecurityToken != stored.SecurityToken {
		t.Fatalf("recovered save lost retained credentials or enabled state; error=%v", err)
	}
}
