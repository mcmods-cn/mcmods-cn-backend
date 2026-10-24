package httpapi

import (
	"fmt"
	"mcmods-cn-backend/internal/systemactor"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuditFixCMP006AutomationRevokedAuthorityIntegration(t *testing.T) {
	for _, mode := range []string{"enabled-control", "disabled-subject", "explicit-permission-deny", "expired-permissions", "wrong-project-scope", "revoked-during-provider"} {
		t.Run(mode, func(t *testing.T) {
			f := newTEST013Fixture(t)
			s := f.origin.Config.Handler.(*Server)
			var actorID int64
			if err := f.db.QueryRow(f.ctx, "insert into users(username,email,password_hash,email_verified,status) values($1,$2,'login-disabled',true,$3) returning id", systemactor.AutobotUsername, systemactor.AutobotEmail, systemactor.AutobotStatus).Scan(&actorID); err != nil {
				t.Fatal(err)
			}
			permissions := []string{"project.create", "project.edit", "project.no-review", "content.no-review"}
			grantTEST044Permissions(t, f.ctx, f.db, actorID, permissions...)
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "*")
			var calls atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/project/audit-auto/version" {
					http.NotFound(w, r)
					return
				}
				calls.Add(1)
				if mode == "revoked-during-provider" {
					if _, revokeErr := f.db.Exec(f.ctx, `update user_permissions set allow=false where user_id=$1`, actorID); revokeErr != nil {
						t.Error(revokeErr)
						http.Error(w, "fixture revoke failed", 500)
						return
					}
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, "[{\"id\":\"audit-release\",\"name\":\"Audit release\",\"version_number\":\"1.0.0\",\"changelog\":\"Audit automated source\",\"date_published\":%q,\"version_type\":\"release\",\"game_versions\":[\"1.21.1\"],\"loaders\":[\"neoforge\"],\"files\":[]}]", time.Now().UTC().Format(time.RFC3339))
			}))
			t.Cleanup(provider.Close)
			imp := defaultModImportConfig()
			imp.RequestTimeoutSeconds = 2
			imp.Modrinth.BaseURL = provider.URL
			imp.Modrinth.Token = ""
			sealed, err := s.sealSystemSetting(imp)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.db.Exec(f.ctx, "insert into system_settings(key,value) values($1,$2::jsonb) on conflict(key) do update set value=excluded.value", modImportConfigSettingKey, sealed); err != nil {
				t.Fatal(err)
			}
			if err = saveMinecraftVersionConfigAndInvalidateStaleArtifacts(f.ctx, f.db, minecraftVersionConfig{Versions: []minecraftVersionOption{{Code: "1.21.1", Type: "release"}}, Loaders: []minecraftLoaderOption{{Code: "neoforge", Name: "NeoForge", Versions: []string{"1.21.1"}}}}); err != nil {
				t.Fatal(err)
			}
			var routeID, settingID, runID int64
			if err = f.db.QueryRow(f.ctx, "select id from public_routes where entity_type='mod' and internal_id=$1", f.modID).Scan(&routeID); err != nil {
				t.Fatal(err)
			}
			if _, err = f.db.Exec(f.ctx, "insert into project_external_sources(project_route_id,source_type,external_project_id,external_project_url,verified_at,verified_by) values($1,'modrinth','audit-auto',$2,now(),$3)", routeID, provider.URL+"/project/audit-auto", f.userIDs[f.reviewer]); err != nil {
				t.Fatal(err)
			}
			if err = f.db.QueryRow(f.ctx, "insert into project_auto_update_settings(project_route_id,update_kind,source_type,interval_code,enabled,configured_by) values($1,'changelog','modrinth','week',false,$2) returning id", routeID, f.userIDs[f.reviewer]).Scan(&settingID); err != nil {
				t.Fatal(err)
			}
			if mode == "disabled-subject" {
				if _, err = f.db.Exec(f.ctx, "update users set status='disabled' where id=$1", actorID); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "expired-permissions" {
				if _, err = f.db.Exec(f.ctx, `update user_permissions set expires_at=now()-interval '1 second' where user_id=$1`, actorID); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "wrong-project-scope" {
				if _, err = f.db.Exec(f.ctx, `delete from user_permissions where user_id=$1 and permission_id=(select id from permissions where code='project.edit')`, actorID); err != nil {
					t.Fatal(err)
				}
				grantTEST044Permissions(t, f.ctx, f.db, actorID, "project.edit."+f.otherModCode)
			}
			if mode == "explicit-permission-deny" {
				if _, err = f.db.Exec(f.ctx, "update user_permissions set allow=false where user_id=$1", actorID); err != nil {
					t.Fatal(err)
				}
				claims, err := s.automationActor(f.ctx)
				if err != nil {
					t.Fatal(err)
				}
				for _, p := range permissions {
					if claimsAllow(claims, p) {
						t.Fatalf("deny fixture still permits %s", p)
					}
				}
			}
			if err = f.db.QueryRow(f.ctx, "insert into project_auto_update_runs(setting_id) values($1) returning id", settingID).Scan(&runID); err != nil {
				t.Fatal(err)
			}
			worker := &ProjectAutomationWorker{server: s}
			processed, err := worker.processOne(f.ctx)
			if err != nil || !processed {
				t.Fatalf("worker processed=%v err=%v", processed, err)
			}
			var status, code string
			var recordedActor *int64
			var published int
			if err = f.db.QueryRow(f.ctx, "select status,last_error_code,actor_id from project_auto_update_runs where id=$1", runID).Scan(&status, &code, &recordedActor); err != nil {
				t.Fatal(err)
			}
			if err = f.db.QueryRow(f.ctx, "select count(*) from project_changelogs where object_route_id=$1 and review_status='approved'", routeID).Scan(&published); err != nil {
				t.Fatal(err)
			}
			actorMatches := recordedActor != nil && *recordedActor == actorID
			t.Logf("mode=%s run=%s error=%s providerRequests=%d approvedLogs=%d actorIsAutobot=%v", mode, status, code, calls.Load(), published, actorMatches)
			if mode == "enabled-control" {
				if status != "completed" || published != 1 || !actorMatches || calls.Load() != 1 {
					t.Fatal("positive worker control did not publish once with actual system actor")
				}
			} else if mode == "disabled-subject" {
				if status != "pending" || code != "automation_actor_unavailable" || published != 0 || calls.Load() != 0 || recordedActor != nil {
					t.Fatal("disabled system actor was replaced by administrator or published")
				}
			} else if published != 0 || status == "completed" {
				t.Error("explicitly revoked automation authority still published approved business data")
			} else if strings.TrimSpace(code) == "" {
				t.Error("denied worker lacks observable failure code")
			}

		})
	}
}

func TestAuditFixCMP006CrawlerRevokedCreateAuthorityIntegration(t *testing.T) {
	for _, mode := range []string{"enabled-control", "disabled-subject", "create-explicitly-denied"} {
		t.Run(mode, func(t *testing.T) {
			f := newTEST013Fixture(t)
			s := f.origin.Config.Handler.(*Server)
			var actorID int64
			if err := f.db.QueryRow(f.ctx, "insert into users(username,email,password_hash,email_verified,status) values($1,$2,'disabled-login',true,$3) returning id", systemactor.AutobotUsername, systemactor.AutobotEmail, systemactor.AutobotStatus).Scan(&actorID); err != nil {
				t.Fatal(err)
			}
			grantTEST044Permissions(t, f.ctx, f.db, actorID, "project.create", "project.edit", "project.no-review", "content.no-review")
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/search":
					fmt.Fprint(w, `{"hits":[{"project_id":"audit-crawl","slug":"audit-crawl","title":"Audit crawler","description":"synthetic","downloads":1000}],"total_hits":1}`)
				case "/project/audit-crawl":
					fmt.Fprint(w, `{"id":"audit-crawl","slug":"audit-crawl","title":"Audit crawler","description":"Audit source","body":"Audit source body","project_type":"mod","status":"approved","client_side":"required","server_side":"required","categories":["utility"],"game_versions":["1.21.1"],"loaders":["neoforge"],"license":{"id":"MIT"}}`)
				case "/project/audit-crawl/version":
					fmt.Fprint(w, "[]")
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(provider.Close)
			imp := defaultModImportConfig()
			imp.Modrinth.BaseURL = provider.URL
			imp.Modrinth.Token = ""
			imp.RequestTimeoutSeconds = 2
			sealed, err := s.sealSystemSetting(imp)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.db.Exec(f.ctx, "insert into system_settings(key,value) values($1,$2::jsonb)", modImportConfigSettingKey, sealed); err != nil {
				t.Fatal(err)
			}
			if _, err = f.db.Exec(f.ctx, "insert into seed_crawler_configs(id,enabled,project_types,batch_size,daily_limit,minimum_downloads,interval_seconds,max_concurrency,ai_daily_token_budget,auto_submit_review) values(true,false,array['mod'],1,10,0,3600,1,0,true)"); err != nil {
				t.Fatal(err)
			}
			if mode == "disabled-subject" {
				if _, err = f.db.Exec(f.ctx, "update users set status='disabled' where id=$1", actorID); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "create-explicitly-denied" {
				if _, err = f.db.Exec(f.ctx, "update user_permissions set allow=false where user_id=$1 and permission_id=(select id from permissions where code='project.create')", actorID); err != nil {
					t.Fatal(err)
				}
				claims, err := s.automationActor(f.ctx)
				if err != nil || claimsAllow(claims, "project.create") || !claimsAllow(claims, "project.no-review") {
					t.Fatal("denied-create fixture invalid")
				}
			}
			var runID int64
			if err = f.db.QueryRow(f.ctx, "insert into seed_crawler_runs(dry_run) values(false) returning id").Scan(&runID); err != nil {
				t.Fatal(err)
			}
			worker := NewSeedCrawlerWorker(s.cfg, f.db)
			ok, err := worker.processOne(f.ctx)
			if err != nil || !ok {
				t.Fatalf("process=%v err=%v", ok, err)
			}
			var state, detail string
			var projects, approved int
			if err = f.db.QueryRow(f.ctx, "select status,last_error from seed_crawler_runs where id=$1", runID).Scan(&state, &detail); err != nil {
				t.Fatal(err)
			}
			if err = f.db.QueryRow(f.ctx, "select count(*),count(*) filter(where review_status='approved') from mods where modrinth_project_id='audit-crawl'").Scan(&projects, &approved); err != nil {
				t.Fatal(err)
			}
			t.Logf("mode=%s task=%s projects=%d approved=%d diagnostic=%s", mode, state, projects, approved, detail)
			if mode == "enabled-control" {
				if state != "completed" || projects != 1 || approved != 1 {
					t.Fatal("crawler healthy positive control failed")
				}
			} else if projects != 0 {
				t.Error("revoked or disabled system subject still creates business project")
			}
			if mode == "disabled-subject" && detail == "" {
				t.Error("disabled subject lacks observable diagnostic")
			}
		})
	}
}
