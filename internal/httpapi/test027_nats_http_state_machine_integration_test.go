package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
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
		JetStream: config.JetStreamConfig{Enabled: true, Stream: "TEST027_BASE", NewStreamMaxBytes: 4 << 20, MaxDeliver: 9,
			AckWait: 17 * time.Second, PublishTimeout: 3 * time.Second},
	})
	queueClient := queue.New(ctx, cfg.NATS)
	defer queueClient.Close()
	peerQueue := queue.New(ctx, cfg.NATS)
	defer peerQueue.Close()
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
	read := func(t *testing.T) natsConfigResponse {
		t.Helper()
		return decodeTEST022Data[natsConfigResponse](t, get(t, token, http.StatusOK))
	}
	initial := read(t)
	put(t, request, http.StatusForbidden)
	grantTEST044Permissions(t, ctx, pool, adminID, "admin.config.write")
	if raw := put(t, request, http.StatusPreconditionRequired); !bytes.Contains(raw, []byte("NATS_CONFIG_REVISION_REQUIRED")) {
		t.Fatal("missing revision must request a fresh read")
	}
	request.Revision = initial.Revision
	if first := decodeTEST022Data[natsConfigResponse](t, put(t, request, http.StatusOK)); first.Revision == "" || first.Revision == initial.Revision || first.Revision != read(t).Revision {
		t.Fatal("save did not return the new revision read by the next client")
	}
	if raw := put(t, request, http.StatusConflict); !bytes.Contains(raw, []byte("NATS_CONFIG_CONFLICT")) {
		t.Fatal("older client snapshot was accepted")
	}
	request.Revision = read(t).Revision
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
		for _, field := range []string{"enabled", "url", "username", "subjectPrefix", "tasks", "realtime", "jetStream"} {
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
				candidate.Revision = read(t).Revision
				if raw := put(t, candidate, http.StatusBadRequest); !bytes.Contains(raw, []byte("NATS_CREDENTIAL_DECISION_REQUIRED")) {
					t.Fatal("changing the broker requires an explicit credential decision")
				}
				candidate.Password = natsPtr(cfg.NATS.Password)
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
	// The successful swap at the end of this subtest remains active for the
	// subsequent reconfiguration checks. Its broker belongs to the outer test.
	candidateBroker := newTEST027Broker(t, true, false)
	t.Run("database rejection discards prepared candidate", func(t *testing.T) {
		candidate := test027NATSUpdate(cfg.NATS)
		candidate.URL = natsPtr(candidateBroker.ClientURL())
		candidate.Revision, candidate.Password = read(t).Revision, natsPtr(cfg.NATS.Password)
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
		if _, err = js.StreamInfo("TEST027_DISCARDED"); err != nil {
			t.Fatalf("rejected candidate lost its shareable stream: %v", err)
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
		codes := make(chan int, 2)
		initial := read(t)
		for _, suffix := range []string{"A", "B"} {
			candidate := test027NATSUpdate(cfg.NATS)
			candidate.Revision, candidate.Password = initial.Revision, natsPtr(cfg.NATS.Password)
			candidate.SubjectPrefix = natsPtr("test027" + suffix)
			candidate.JetStream.Stream = natsPtr("TEST027_" + suffix)
			wg.Add(1)
			go func() {
				defer wg.Done()
				raw, err := json.Marshal(candidate)
				if err != nil {
					t.Error(err)
					return
				}
				status, body, err := test027HTTPRequest(ctx, client, httpServer.URL, token, http.MethodPut, raw)
				if err != nil || (status == http.StatusConflict && !bytes.Contains(body, []byte("NATS_CONFIG_CONFLICT"))) {
					t.Errorf("concurrent PUT status=%d err=%v", status, err)
				}
				codes <- status
			}()
		}
		wg.Wait()
		close(codes)
		counts := make(map[int]int)
		for code := range codes {
			counts[code]++
		}
		if counts[http.StatusOK] != 1 || counts[http.StatusConflict] != 1 {
			t.Fatalf("competing saves returned %v", counts)
		}
		loaded, err := database.LoadNATSConfig(ctx, pool, cfg.NATS, cfg.SettingsEncryptionKey)
		if err != nil || loaded.SubjectPrefix != queueClient.Status().SubjectPrefix {
			t.Fatalf("concurrent persisted/runtime authority diverged: err=%v", err)
		}
		assertDelivery(t, "after-concurrent-swaps")
		disabled := test027NATSUpdate(loaded)
		disabled.Revision = read(t).Revision
		disabled.Enabled, disabled.OutboxEnabled, disabled.Realtime, disabled.JetStream.Enabled = natsPtr(false), natsPtr(false), natsPtr(false), natsPtr(false)
		disabled.Password, disabled.Token = natsPtr(""), natsPtr("test027-token")
		put(t, disabled, http.StatusOK)
		loaded, err = database.LoadNATSConfig(ctx, pool, cfg.NATS, cfg.SettingsEncryptionKey)
		if err != nil || loaded.Password != cfg.NATS.Password || loaded.Token != "test027-token" {
			t.Fatalf("blank password or replacement token was not preserved: err=%v", err)
		}
		disabled.Revision = read(t).Revision
		disabled.ClearPassword, disabled.ClearToken, disabled.Token = true, true, nil
		put(t, disabled, http.StatusOK)
		loaded, err = database.LoadNATSConfig(ctx, pool, cfg.NATS, cfg.SettingsEncryptionKey)
		if err != nil || loaded.Enabled || !loaded.OutboxEnabled || loaded.Realtime || loaded.JetStream.Enabled || loaded.Password != "" || loaded.Token != "" || queueClient.Status().Connected {
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
		latest, err := database.ReadNATSConfig(ctx, pool, cfg.NATS, cfg.SettingsEncryptionKey)
		if err != nil {
			t.Fatal(err)
		}
		want.Revision = encryptedSettingRevision(latest.Encrypted)
		if !reflect.DeepEqual(response.Data, want) || strings.Contains(string(body), cfg.NATS.Password) || strings.Contains(string(body), "test027-token") {
			t.Fatal("GET lost complete reliability DTO or leaked credentials")
		}
	})
	t.Run("a peer can distinguish the saved configuration from its own runtime", func(t *testing.T) {
		peer := &Server{db: pool, cfg: cfg, queue: peerQueue}
		getConfig := func() natsConfigResponse {
			t.Helper()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/config/nats", nil).WithContext(ctx)
			response := httptest.NewRecorder()
			peer.getNATSConfig(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("peer read status=%d", response.Code)
			}
			return decodeTEST022Data[natsConfigResponse](t, response.Body.Bytes())
		}
		before := getConfig()
		if before.Enabled || !before.Status.Enabled || before.Status.SubjectPrefix != cfg.NATS.SubjectPrefix {
			t.Fatalf("stale peer did not expose both saved and running state: enabled=%t runtime=%t", before.Enabled, before.Status.Enabled)
		}
		loaded, err := database.LoadNATSConfig(ctx, pool, cfg.NATS, cfg.SettingsEncryptionKey)
		if err != nil {
			t.Fatal(err)
		}
		restarted := queue.New(ctx, loaded)
		defer restarted.Close()
		peer.queue = restarted
		after := getConfig()
		if after.Enabled || after.Status.Enabled || after.Status.SubjectPrefix != loaded.SubjectPrefix {
			t.Fatalf("restarted peer did not load the saved configuration: enabled=%t runtime=%t", after.Enabled, after.Status.Enabled)
		}
	})
	t.Run("two API instances commit only the configuration they read", func(t *testing.T) {
		current, err := database.LoadNATSConfig(ctx, pool, cfg.NATS, cfg.SettingsEncryptionKey)
		if err != nil {
			t.Fatal(err)
		}
		peerRuntime := queue.New(ctx, current)
		defer peerRuntime.Close()
		peer := NewServer(ctx, cfg, pool, peerRuntime, nil, nil, nil)
		defer func() {
			shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			if err := peer.Shutdown(shutdown); err != nil {
				t.Error(err)
			}
		}()
		peerHTTP := httptest.NewServer(peer)
		defer peerHTTP.Close()
		submit := test027NATSUpdate(current)
		rawStatus, rawView, err := test027HTTPRequest(ctx, peerHTTP.Client(), peerHTTP.URL, token, http.MethodGet, nil)
		if err != nil || rawStatus != http.StatusOK {
			t.Fatalf("peer GET status=%d err=%v", rawStatus, err)
		}
		submit.Revision = decodeTEST022Data[natsConfigResponse](t, rawView).Revision
		if submit.Revision != read(t).Revision {
			t.Fatal("replicas read different stored revisions")
		}

		// Both replicas must have completed preparation and be waiting to persist
		// before either can commit, independent of the goroutine scheduler.
		barrier, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer barrier.Rollback(ctx)
		var blocker int
		if err = barrier.QueryRow(ctx, `select pg_backend_pid() from pg_advisory_xact_lock(hashtextextended('settings:nats.config',0))`).Scan(&blocker); err != nil {
			t.Fatal(err)
		}
		results := make(chan int, 2)
		for i, origin := range []*httptest.Server{httpServer, peerHTTP} {
			next := submit
			next.SubjectPrefix = natsPtr([]string{"peer.one", "peer.two"}[i])
			next.Password = natsPtr("synthetic-rotation-" + *next.SubjectPrefix)
			body, err := json.Marshal(next)
			if err != nil {
				t.Fatal(err)
			}
			go func(origin *httptest.Server, body []byte) {
				status, result, err := test027HTTPRequest(ctx, origin.Client(), origin.URL, token, http.MethodPut, body)
				if err != nil || (status == http.StatusConflict && !bytes.Contains(result, []byte("NATS_CONFIG_CONFLICT"))) {
					t.Errorf("peer PUT status=%d err=%v", status, err)
				}
				results <- status
			}(origin, body)
		}
		waitCtx, stop := context.WithTimeout(ctx, 10*time.Second)
		defer stop()
		tick := time.NewTicker(15 * time.Millisecond)
		defer tick.Stop()
		for {
			var waiting int
			if err := barrier.QueryRow(waitCtx, `select count(*) from pg_locks holder join pg_locks waiter using(locktype,database,classid,objid,objsubid)
				where holder.pid=$1 and holder.locktype='advisory' and holder.granted and not waiter.granted`, blocker).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			if waiting == 2 {
				break
			}
			select {
			case <-waitCtx.Done():
				t.Fatal("not all peer candidates reached persistence")
			case <-tick.C:
			}
		}
		if err = barrier.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		counts := map[int]int{}
		for i := 0; i < 2; i++ {
			counts[<-results]++
		}
		if counts[http.StatusOK] != 1 || counts[http.StatusConflict] != 1 {
			t.Fatalf("peer outcomes: %v", counts)
		}
		winner, err := database.LoadNATSConfig(ctx, pool, cfg.NATS, cfg.SettingsEncryptionKey)
		if err != nil || winner.Password != "synthetic-rotation-"+winner.SubjectPrefix {
			t.Fatal("stored credentials and public config came from different candidates")
		}
		if submit.Revision == read(t).Revision {
			t.Fatal("winning write retained the old revision")
		}
		retry := test027NATSUpdate(winner)
		retry.Revision = read(t).Revision
		retry.SubjectPrefix = natsPtr("peer.retry")
		put(t, retry, http.StatusOK)
		after, err := database.LoadNATSConfig(ctx, pool, cfg.NATS, cfg.SettingsEncryptionKey)
		if err != nil || after.Password != winner.Password || after.SubjectPrefix != "peer.retry" {
			t.Fatal("fresh retry failed to retain the winning credential")
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
		JetStream: jetStream, JetStreamMaxStore: 32 << 20, StoreDir: t.TempDir(), Users: []*natsserver.User{user}})
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
