package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
)

func TestMetadataImportLateWorkerCannotOverwriteReplacementAttemptIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 with an owned test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `create temp table system_settings(key text primary key,value jsonb not null);
		create temp table mod_metadata_import_jobs(public_id text primary key,user_id bigint,project_type text,provider text,source_url text,
		status text,progress integer,error text default '',started_at timestamptz,updated_at timestamptz,finished_at timestamptz,result jsonb)`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool, cfg: config.Config{SettingsEncryptionKey: "synthetic-test-only-settings-key-32"}}
	for _, scenario := range []string{"success", "failure"} {
		t.Run(scenario, func(t *testing.T) {
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/project/fence-test" {
					// Simulate recovery and a newer claim while the first worker is
					// awaiting its upstream response. All writes use our own table.
					if _, err := pool.Exec(ctx, `update mod_metadata_import_jobs set
						started_at=started_at+interval '1 minute',progress=42,result='{"replacement":true}' where public_id='lease0001'`); err != nil {
						t.Errorf("replace attempt: %v", err)
					}
					if scenario == "failure" {
						http.Error(w, "synthetic provider failure", http.StatusServiceUnavailable)
						return
					}
					_, _ = w.Write([]byte(`{"id":"fence-id","slug":"fence-test","title":"Fenced import","project_type":"mod","status":"approved","client_side":"required","server_side":"required","license":{"id":"MIT"}}`))
					return
				}
				_, _ = w.Write([]byte(`[]`))
			}))
			defer provider.Close()
			cfg := defaultModImportConfig()
			cfg.Modrinth = modImportProviderConfig{Enabled: true, BaseURL: provider.URL}
			cfg.CurseForge.Enabled, cfg.GitHub.Enabled = false, false
			cfg.RequestTimeoutSeconds = 5
			sealed, err := server.sealSystemSetting(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values($1,$2::jsonb)
				on conflict(key) do update set value=excluded.value`, modImportConfigSettingKey, sealed); err != nil {
				t.Fatal(err)
			}
			if _, err = pool.Exec(ctx, `insert into mod_metadata_import_jobs(public_id,user_id,project_type,provider,source_url,status,progress)
				values('lease0001',1,'mod','modrinth','https://modrinth.com/mod/fence-test','queued',0)
				on conflict(public_id) do update set status='queued',progress=0,started_at=null,result=null,error=''`); err != nil {
				t.Fatal(err)
			}
			if err = server.runModMetadataImport(ctx, "lease0001"); err != nil {
				t.Fatal(err)
			}
			var status, result string
			var progress int
			if err = pool.QueryRow(ctx, `select status,progress,result::text from mod_metadata_import_jobs where public_id='lease0001'`).Scan(&status, &progress, &result); err != nil {
				t.Fatal(err)
			}
			if status != "running" || progress != 42 || !strings.Contains(result, `"replacement": true`) {
				t.Fatalf("old worker overwrote replacement attempt: status=%s progress=%d result=%s", status, progress, result)
			}
		})
	}
	pool.Close()
	if err := server.runModMetadataImport(ctx, "lease0001"); err == nil {
		t.Fatal("claim storage failure was acknowledged as successful processing")
	}
}
