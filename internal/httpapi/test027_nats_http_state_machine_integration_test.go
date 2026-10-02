package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/queue"
)

func TestTEST027NATSHTTPConfigurationPersistenceAndFailureIsolationIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool := newTEST018IsolatedDatabase(t, ctx)
	broker := newTEST027Broker(t, true, false)
	cfg := config.Load()
	cfg.JWTSecret = "test027-only-signing-secret"
	cfg.SettingsEncryptionKey = "test027-only-encryption-key-32-bytes-long"
	cfg.Redis.Enabled = false
	cfg.AntiAbuse.Enabled = false
	cfg.NATS = queue.NormalizeConfig(config.NATSConfig{
		Enabled: true, URL: broker.ClientURL(), Username: "test027", Password: "test027-password",
		SubjectPrefix: "test027", Realtime: true, OutboxEnabled: true,
		JetStream: config.JetStreamConfig{Enabled: true, Stream: "TEST027_BASE", MaxDeliver: 9,
			AckWait: 17 * time.Second, PublishTimeout: 3 * time.Second},
	})
	queueClient := queue.New(ctx, cfg.NATS)
	defer queueClient.Close()
	server := NewServer(ctx, cfg, pool, queueClient, nil, nil, nil)
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			t.Errorf("shutdown TEST027 server: %v", err)
		}
	}()
	httpServer := httptest.NewServer(server)
	defer httpServer.Close()
	client := httpServer.Client()
	client.Timeout = 15 * time.Second
	adminID, _, token := createTEST044User(t, ctx, pool, cfg, "test027", "UTC")
	request := test027NATSUpdate(cfg.NATS)
	put := func(t *testing.T, payload natsConfigUpdateRequest, expected int) []byte {
		t.Helper()
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		status, body, err := test027HTTPRequest(ctx, client, httpServer.URL, token, http.MethodPut, raw)
		if err != nil {
			t.Fatal(err)
		}
		if status != expected {
			t.Fatalf("PUT NATS status=%d want=%d body=%s", status, expected, body)
		}
		return body
	}
	get := func(t *testing.T, requestToken string, expected int) []byte {
		t.Helper()
		status, body, err := test027HTTPRequest(ctx, client, httpServer.URL, requestToken, http.MethodGet, nil)
		if err != nil || status != expected {
			t.Fatalf("GET NATS status=%d want=%d err=%v body=%s", status, expected, err, body)
		}
		return body
	}

	get(t, "", http.StatusUnauthorized)
	get(t, token, http.StatusForbidden)
	put(t, request, http.StatusForbidden)
	grantTEST044Permissions(t, ctx, pool, adminID, "admin.config.read")
	get(t, token, http.StatusOK)
	put(t, request, http.StatusForbidden)
	grantTEST044Permissions(t, ctx, pool, adminID, "admin.config.write")
	put(t, request, http.StatusOK)
	var originalStored []byte
	if err := pool.QueryRow(ctx, `select value from system_settings where key='nats.config'`).Scan(&originalStored); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(originalStored, []byte(cfg.NATS.Password)) || !bytes.Contains(originalStored, []byte("aes-gcm-v1")) {
		t.Fatal("NATS credentials were not encrypted at rest")
	}
	assertUnchanged := func(t *testing.T) {
		t.Helper()
		var stored []byte
		if err := pool.QueryRow(ctx, `select value from system_settings where key='nats.config'`).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		status := queueClient.Status()
		if !bytes.Equal(stored, originalStored) || !status.RealtimeReady || status.URL != cfg.NATS.URL || status.SubjectPrefix != cfg.NATS.SubjectPrefix {
			t.Fatalf("rejected PUT changed persisted/runtime authority: status=%#v", status)
		}
	}
	t.Run("every reliability field is mandatory", func(t *testing.T) {
		for _, field := range []string{"enabled", "url", "username", "subjectPrefix", "tasks", "outboxEnabled", "realtime", "jetStream"} {
			raw, _ := json.Marshal(request)
			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatal(err)
			}
			delete(payload, field)
			raw, _ = json.Marshal(payload)
			status, body, err := test027HTTPRequest(ctx, client, httpServer.URL, token, http.MethodPut, raw)
			if err != nil || status != http.StatusBadRequest {
				t.Fatalf("missing %s status=%d err=%v body=%s", field, status, err, body)
			}
			assertUnchanged(t)
		}
		for _, field := range []string{"enabled", "stream", "maxDeliver", "ackWaitSeconds", "publishTimeoutSeconds"} {
			raw, _ := json.Marshal(request)
			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatal(err)
			}
			delete(payload["jetStream"].(map[string]any), field)
			raw, _ = json.Marshal(payload)
			status, body, err := test027HTTPRequest(ctx, client, httpServer.URL, token, http.MethodPut, raw)
			if err != nil || status != http.StatusBadRequest {
				t.Fatalf("missing jetStream.%s status=%d err=%v body=%s", field, status, err, body)
			}
			assertUnchanged(t)
		}
	})
	t.Run("connection stream and subscription failures retain good runtime", func(t *testing.T) {
		for name, candidateBroker := range map[string]*natsserver.Server{
			"JetStream unavailable":  newTEST027Broker(t, false, false),
			"SUB denied":             newTEST027Broker(t, true, true),
			"connection unavailable": nil,
		} {
			t.Run(name, func(t *testing.T) {
				candidate := test027NATSUpdate(cfg.NATS)
				if candidateBroker == nil {
					candidate.URL = natsPtr("nats://127.0.0.1:1")
				} else {
					candidate.URL = natsPtr(candidateBroker.ClientURL())
				}
				put(t, candidate, http.StatusBadGateway)
				assertUnchanged(t)
			})
		}
	})
	peerEvents, unsubscribe := server.realtime.subscribe(42)
	defer unsubscribe()
	assertDelivery := func(t *testing.T, id string) {
		t.Helper()
		if err := queueClient.PublishBroadcast(ctx, "user.42", realtimeEvent{ID: id, Type: "notification.test027"}); err != nil {
			t.Fatal(err)
		}
		event := receiveBUG046RealtimeEvent(t, peerEvents, id)
		if event.ID != id {
			t.Fatalf("event identity=%q want=%q", event.ID, id)
		}
		assertNoBUG046RealtimeEvent(t, peerEvents, id+" duplicate")
	}
	assertDelivery(t, "before-persistence-failure")
	t.Run("database rejection discards prepared candidate", func(t *testing.T) {
		candidateBroker := newTEST027Broker(t, true, false)
		candidate := test027NATSUpdate(cfg.NATS)
		candidate.URL = natsPtr(candidateBroker.ClientURL())
		candidate.JetStream.Stream = natsPtr("TEST027_DISCARDED")
		if _, err := pool.Exec(ctx, `alter table system_settings add constraint test027_reject_nats_save check(key<>'nats.config') not valid`); err != nil {
			t.Fatal(err)
		}
		put(t, candidate, http.StatusInternalServerError)
		assertUnchanged(t)
		if _, err := pool.Exec(ctx, `alter table system_settings drop constraint test027_reject_nats_save`); err != nil {
			t.Fatal(err)
		}
		conn, err := nats.Connect(candidateBroker.ClientURL(), nats.UserInfo("test027", "test027-password"))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		js, err := conn.JetStream()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = js.StreamInfo("TEST027_DISCARDED"); !errors.Is(err, nats.ErrStreamNotFound) {
			t.Fatalf("discarded candidate stream lookup error=%v, want StreamNotFound", err)
		}
		assertDelivery(t, "after-persistence-failure")
		put(t, candidate, http.StatusOK)
		loaded, err := database.LoadNATSConfig(ctx, pool, cfg.NATS, cfg.SettingsEncryptionKey)
		if err != nil || loaded.URL != candidateBroker.ClientURL() || loaded.JetStream.Stream != "TEST027_DISCARDED" || queueClient.Status().URL != loaded.URL {
			t.Fatalf("successful candidate did not swap persisted/runtime authority: err=%v", err)
		}
		assertDelivery(t, "after-successful-swap")
	})
	t.Run("concurrent PUT and cleared secrets retain complete authority", func(t *testing.T) {
		var wg sync.WaitGroup
		failures := make(chan string, 2)
		for _, suffix := range []string{"A", "B"} {
			candidate := test027NATSUpdate(cfg.NATS)
			candidate.SubjectPrefix = natsPtr("test027" + suffix)
			candidate.JetStream.Stream = natsPtr("TEST027_" + suffix)
			wg.Add(1)
			go func() {
				defer wg.Done()
				raw, err := json.Marshal(candidate)
				if err != nil {
					failures <- err.Error()
					return
				}
				status, body, err := test027HTTPRequest(ctx, client, httpServer.URL, token, http.MethodPut, raw)
				if err != nil || status != http.StatusOK {
					failures <- string(body)
				}
			}()
		}
		wg.Wait()
		close(failures)
		for failure := range failures {
			t.Errorf("concurrent PUT failed: %s", failure)
		}
		loaded, err := database.LoadNATSConfig(ctx, pool, cfg.NATS, cfg.SettingsEncryptionKey)
		if err != nil || loaded.SubjectPrefix != queueClient.Status().SubjectPrefix {
			t.Fatalf("concurrent persisted/runtime authority diverged: err=%v", err)
		}
		assertDelivery(t, "after-concurrent-swaps")
		disabled := test027NATSUpdate(loaded)
		disabled.Enabled, disabled.OutboxEnabled, disabled.Realtime, disabled.JetStream.Enabled = natsPtr(false), natsPtr(false), natsPtr(false), natsPtr(false)
		disabled.Password, disabled.Token = natsPtr(""), natsPtr("test027-token")
		put(t, disabled, http.StatusOK)
		loaded, err = database.LoadNATSConfig(ctx, pool, cfg.NATS, cfg.SettingsEncryptionKey)
		if err != nil || loaded.Password != cfg.NATS.Password || loaded.Token != "test027-token" {
			t.Fatalf("blank password or replacement token was not preserved: err=%v", err)
		}
		disabled.ClearPassword, disabled.ClearToken, disabled.Token = true, true, nil
		put(t, disabled, http.StatusOK)
		loaded, err = database.LoadNATSConfig(ctx, pool, cfg.NATS, cfg.SettingsEncryptionKey)
		if err != nil || loaded.Enabled || loaded.OutboxEnabled || loaded.Realtime || loaded.JetStream.Enabled || loaded.Password != "" || loaded.Token != "" || queueClient.Status().Connected {
			t.Fatalf("stored false/cleared secrets resurrected process fallback: err=%v", err)
		}
		body := get(t, token, http.StatusOK)
		var response struct {
			Data natsConfigResponse `json:"data"`
		}
		if err = json.Unmarshal(body, &response); err != nil {
			t.Fatal(err)
		}
		want := redactNATSConfig(loaded, queueClient.Status())
		if !reflect.DeepEqual(response.Data, want) || strings.Contains(string(body), cfg.NATS.Password) || strings.Contains(string(body), "test027-token") {
			t.Fatal("GET lost complete reliability DTO or leaked credentials")
		}
	})
}

func test027NATSUpdate(cfg config.NATSConfig) natsConfigUpdateRequest {
	return natsConfigUpdateRequest{
		Enabled: natsPtr(cfg.Enabled), URL: natsPtr(cfg.URL), Username: natsPtr(cfg.Username), SubjectPrefix: natsPtr(cfg.SubjectPrefix),
		Tasks: &cfg.Tasks, OutboxEnabled: natsPtr(cfg.OutboxEnabled), Realtime: natsPtr(cfg.Realtime),
		JetStream: &natsJetStreamUpdateRequest{Enabled: natsPtr(cfg.JetStream.Enabled), Stream: natsPtr(cfg.JetStream.Stream), MaxDeliver: natsPtr(cfg.JetStream.MaxDeliver),
			AckWaitSeconds: natsPtr(int(cfg.JetStream.AckWait / time.Second)), PublishTimeoutSeconds: natsPtr(int(cfg.JetStream.PublishTimeout / time.Second))},
	}
}

func newTEST027Broker(t *testing.T, jetStream, denyBroadcast bool) *natsserver.Server {
	t.Helper()
	user := &natsserver.User{Username: "test027", Password: "test027-password"}
	if denyBroadcast {
		user.Permissions = &natsserver.Permissions{Subscribe: &natsserver.SubjectPermission{Deny: []string{"test027.realtime.>"}}}
	}
	broker, err := natsserver.NewServer(&natsserver.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true,
		JetStream: jetStream, StoreDir: t.TempDir(), Users: []*natsserver.User{user}})
	if err != nil {
		t.Fatal(err)
	}
	go broker.Start()
	t.Cleanup(func() { broker.Shutdown(); broker.WaitForShutdown() })
	if !broker.ReadyForConnections(5 * time.Second) {
		t.Fatal("TEST027 broker did not start")
	}
	return broker
}

func test027HTTPRequest(ctx context.Context, client *http.Client, origin, token, method string, body []byte) (int, []byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, origin+"/api/v1/admin/config/nats", bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return response.StatusCode, raw, err
}
