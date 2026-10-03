package httpapi

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

func TestOCT02NATSURLSecretsAreHiddenAndMaskedSaveAuthenticates(t *testing.T) {
	for _, auth := range []string{"password", "token"} {
		t.Run(auth, func(t *testing.T) {
			options := &natsserver.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true}
			if auth == "password" {
				options.Username, options.Password = "synthetic-user", "synthetic-password"
			} else {
				options.Authorization = "synthetic-token"
			}
			broker, err := natsserver.NewServer(options)
			if err != nil {
				t.Fatal(err)
			}
			go broker.Start()
			if !broker.ReadyForConnections(5 * time.Second) {
				broker.Shutdown()
				t.Fatal("broker readiness")
			}
			t.Cleanup(func() { broker.Shutdown(); broker.WaitForShutdown() })
			address, err := url.Parse(broker.ClientURL())
			if err != nil {
				t.Fatal(err)
			}
			if auth == "password" {
				address.User = url.UserPassword(options.Username, options.Password)
			} else {
				address.User = url.User(options.Authorization)
			}
			current := config.NATSConfig{Enabled: true, URL: address.String(), SubjectPrefix: "oct02", Realtime: true}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			runtime := queue.New(ctx, current)
			defer runtime.Close()
			if !runtime.Status().Connected {
				t.Fatal("legacy URL authentication must connect")
			}
			response := redactNATSConfig(current, runtime.Status())
			if strings.Contains(response.URL, "synthetic-") || strings.Contains(response.Status.URL, "synthetic-") {
				t.Fatal("response leaked URL credentials")
			}
			if response.HasPassword != (auth == "password") || response.HasToken != (auth == "token") {
				t.Fatalf("wrong secret flags: password=%v token=%v", response.HasPassword, response.HasToken)
			}
			request := completeNATSUpdateRequest()
			request.Enabled, request.URL, request.Realtime = natsPtr(true), &response.URL, natsPtr(true)
			next, err := request.config(current)
			if err != nil {
				t.Fatal(err)
			}
			if next.URL != current.URL {
				t.Fatal("masked save changed internal URL credentials")
			}
			if err = runtime.Reconfigure(next); err != nil || !runtime.Status().Connected {
				t.Fatalf("masked save broke real broker authentication: %v", err)
			}
			if auth == "token" {
				request.ClearToken = true
			} else {
				request.ClearPassword = true
			}
			cleared, err := request.config(current)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := url.Parse(cleared.URL)
			if err != nil {
				t.Fatal(err)
			}
			if auth == "token" {
				if parsed.User != nil {
					t.Fatal("clearToken retained URL token")
				}
			} else {
				password, marker := parsed.User.Password()
				if !marker || password != "" {
					t.Fatal("clearPassword must keep username/password auth kind")
				}
			}
			if err = runtime.Reconfigure(cleared); err == nil {
				t.Fatal("cleared credentials unexpectedly authenticated")
			}
			if !runtime.Status().Connected {
				t.Fatal("rejected clear destroyed previous valid runtime")
			}
		})
	}
}

func TestOCT02NATSURLCredentialReplacementAuthenticates(t *testing.T) {
	for _, auth := range []string{"password", "token"} {
		t.Run(auth, func(t *testing.T) {
			options := &natsserver.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true}
			if auth == "password" {
				options.Username, options.Password = "synthetic-user", "replacement-password"
			} else {
				options.Authorization = "replacement-token"
			}
			broker, err := natsserver.NewServer(options)
			if err != nil {
				t.Fatal(err)
			}
			go broker.Start()
			if !broker.ReadyForConnections(5 * time.Second) {
				broker.Shutdown()
				t.Fatal("broker readiness")
			}
			t.Cleanup(func() { broker.Shutdown(); broker.WaitForShutdown() })
			address, err := url.Parse(broker.ClientURL())
			if err != nil {
				t.Fatal(err)
			}
			if auth == "password" {
				address.User = url.UserPassword("synthetic-user", "obsolete-password")
			} else {
				address.User = url.User("obsolete-token")
			}
			current := config.NATSConfig{URL: address.String()}
			request := completeNATSUpdateRequest()
			request.Enabled = natsPtr(true)
			request.URL = natsPtr(broker.ClientURL())
			if auth == "password" {
				request.Password = natsPtr(options.Password)
			} else {
				request.Token = natsPtr(options.Authorization)
			}
			next, err := request.config(current)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			runtime := queue.New(ctx, next)
			defer runtime.Close()
			if !runtime.Status().Connected {
				t.Fatal("replacement did not authenticate with real broker")
			}
		})
	}
}
