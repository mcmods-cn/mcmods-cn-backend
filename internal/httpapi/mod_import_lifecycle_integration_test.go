package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestModMetadataImportLeaseIntegration(t *testing.T) {
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set MCMODS_TEST_DATABASE_URL to an isolated migrated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	// Only this newly allocated synthetic schema is dropped. LIKE copies the
	// project's actual columns/defaults/check constraints; PostgreSQL handles
	// the concurrent updates, while provider HTTP is deterministic and local.
	schema := pgx.Identifier{"apia_import_" + randomHex(8)}.Sanitize()
	if _, err = admin.Exec(ctx, "create schema "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, "drop schema "+schema+" cascade"); err != nil {
			t.Errorf("allocated fixture schema cleanup: %v", err)
		}
	}()
	for _, table := range []string{"mod_metadata_import_jobs", "system_settings"} {
		identifier := pgx.Identifier{table}.Sanitize()
		if _, err = admin.Exec(ctx, "create table "+schema+"."+identifier+" (like public."+identifier+" including all)"); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	server := &Server{db: pool, cfg: config.Config{SettingsEncryptionKey: randomHex(32)}}
	worker := &ModMetadataImportWorker{server: server}
	for _, test := range []struct {
		name             string
		failure, replace bool
	}{
		{"duplicate-delivery", false, false},
		{"replaced-success", false, true},
		{"replaced-failure", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			entered := make(chan struct{}, 1)
			release := make(chan struct{})
			var requests atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" || r.Header.Get("x-api-key") != "" {
					t.Error("fixture inherited a provider credential")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				requests.Add(1)
				select {
				case entered <- struct{}{}:
				default:
				}
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				if test.failure {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"fixture","slug":"fixture","title":"Fixture","project_type":"mod","status":"active","loaders":["fabric"],"game_versions":["1.21"]}`))
			}))
			defer provider.Close()
			// Explicit empty credential fields override any application defaults.
			providerConfig := func(enabled bool, baseURL string) map[string]any {
				return map[string]any{"enabled": enabled, "baseUrl": baseURL, "token": "", "apiKey": ""}
			}
			raw, err := server.sealSystemSetting(map[string]any{"requestTimeoutSeconds": 5, "modrinth": providerConfig(true, provider.URL), "github": providerConfig(false, "https://api.github.com"), "curseforge": providerConfig(false, "https://api.curseforge.com/v1")})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values($1,$2) on conflict(key) do update set value=excluded.value`, modImportConfigSettingKey, raw); err != nil {
				t.Fatal(err)
			}
			var jobID string
			if err = pool.QueryRow(ctx, `insert into mod_metadata_import_jobs(user_id,provider,source_url) values(1,'modrinth','https://modrinth.com/mod/fixture') returning public_id`).Scan(&jobID); err != nil {
				t.Fatal(err)
			}
			finished := make(chan error, 1)
			go func() { finished <- server.runModMetadataImport(ctx, jobID) }()
			select {
			case <-entered:
			case early := <-finished:
				close(release)
				t.Fatalf("worker finished before the local provider fixture: %v", early)
			case <-ctx.Done():
				close(release)
				t.Fatal(ctx.Err())
			}
			if err = server.runModMetadataImport(ctx, jobID); err != nil {
				close(release)
				t.Fatal(err)
			}
			if requests.Load() != 1 {
				close(release)
				t.Fatalf("duplicate delivery sent %d provider requests", requests.Load())
			}
			if test.replace {
				if _, err = pool.Exec(ctx, `update mod_metadata_import_jobs set started_at=started_at+interval '1 second',result='{"newer":true}' where public_id=$1`, jobID); err != nil {
					close(release)
					t.Fatal(err)
				}
			}
			close(release)
			select {
			case err = <-finished:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if !test.replace && err != nil {
				t.Fatal(err)
			}
			var status string
			var newer bool
			if err = pool.QueryRow(ctx, `select status,coalesce(result->>'newer','false')='true' from mod_metadata_import_jobs where public_id=$1`, jobID).Scan(&status, &newer); err != nil {
				t.Fatal(err)
			}
			if test.replace && (status != "running" || !newer) {
				t.Fatalf("old worker overwrote a replacement claim: status=%s newer=%v", status, newer)
			}
			if !test.replace && status != "completed" {
				t.Fatalf("normal import status=%s", status)
			}
		})
	}
	var staleID, recentID string
	if err = pool.QueryRow(ctx, `insert into mod_metadata_import_jobs(user_id,provider,source_url,status,updated_at,started_at) values(1,'modrinth','https://modrinth.com/mod/fixture','running',now()-interval '6 minutes',now()-interval '6 minutes') returning public_id`).Scan(&staleID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mod_metadata_import_jobs(user_id,provider,source_url,status,updated_at,started_at) values(1,'modrinth','https://modrinth.com/mod/fixture','running',now(),now()) returning public_id`).Scan(&recentID); err != nil {
		t.Fatal(err)
	}
	if err = worker.recoverStaleJobs(ctx); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		id, status string
		hasClaim   bool
	}{{staleID, "queued", false}, {recentID, "running", true}} {
		var status string
		var hasClaim bool
		if err = pool.QueryRow(ctx, `select status,started_at is not null from mod_metadata_import_jobs where public_id=$1`, test.id).Scan(&status, &hasClaim); err != nil {
			t.Fatal(err)
		}
		if status != test.status || hasClaim != test.hasClaim {
			t.Fatalf("recovery status=%s claim=%v", status, hasClaim)
		}
	}
}
