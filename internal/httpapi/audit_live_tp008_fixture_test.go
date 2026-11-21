package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

// An opt-in real browser fixture, not a passing business test by itself. Five
// Modrinth source combinations can create actual jobs over HTTP and use the
// subscribed Worker. Six CurseForge samples use the real controlled-source
// adapter, then the existing authenticated draft API/browser create flow. No CF
// completed job is fabricated and the credential/official-origin gate remains.
func TestAuditLiveTP008Fixture(t *testing.T) {
	if os.Getenv("MCMODS_RUN_LIVE_TP008") != "1" {
		t.Skip("opt-in long-lived TP008 controlled-provider/browser fixture")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	top, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal(err)
	}
	revisions := map[string]any{}
	for name, location := range map[string]string{"mcmods-cn-backend": strings.TrimSpace(string(top)), "mcmods-cn-frontend": "/workspace/mcmods-cn-frontend", "mcmods_exporter": "/workspace/mcmods_exporter"} {
		revision := auditJourneyRevision(location)
		if revision["error"] != nil || revision["dirty"] != false {
			t.Fatalf("clean identified checkout required: %s", name)
		}
		revisions[name] = revision
	}
	if pin := os.Getenv("MCMODS_AUDIT_EXPECT_BACKEND_COMMIT"); pin != "" && revisions["mcmods-cn-backend"].(map[string]any)["commit"] != pin {
		t.Fatal("backend pin mismatch")
	}
	directory := os.Getenv("MCMODS_AUDIT_TP008_DIR")
	if !filepath.IsAbs(directory) {
		t.Fatal("absolute fresh task-owned directory required")
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	owner, err := os.OpenFile(filepath.Join(directory, "OWNER"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(owner, "mcmods-live-tp008 pid=%d\n", os.Getpid())
	owner.Close()
	if err != nil {
		t.Fatal(err)
	}
	frontendOrigin := "http://127.0.0.1:3311"
	t.Setenv("FRONTEND_ORIGIN", frontendOrigin)
	f := newAuditFixGRFixture(t, 45*time.Minute)
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	// The isolated migration helper does not install registration-role seeds.
	// Configure only the existing ordinary user's documented draft capability
	// before exposing the API, rather than silently seeding unrelated roles or
	// changing permissions during a failed business flow. This permission is in
	// seedRoles[registered]; it grants storage duration, never review bypass.
	grantTEST044Permissions(t, ctx, f.db, f.userIDs[f.editor], "user.draft.retention_seconds.2592000")
	types := []string{"plugin", "map", "resource_pack", "shader_pack", "datapack", "addon"}
	for _, kind := range types {
		grantTEST044Permissions(t, ctx, f.db, f.userIDs[f.editor], "project.create."+kind)
	}
	grantTEST044Permissions(t, ctx, f.db, f.userIDs[f.reviewer], "project.review")
	var relationCreator string
	if err = f.db.QueryRow(ctx, `insert into creators(kind,name,normalized_name,review_status) values('author','TP008 Public Author','tp008 public author','approved') returning public_id`).Scan(&relationCreator); err != nil {
		t.Fatal(err)
	}
	dbConfig := f.db.Config()
	dbConfig.MaxConns = 12
	dbConfig.MinConns = 1
	db, err := pgxpool.NewWithConfig(ctx, dbConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	references := map[string]string{}
	cfClasses := map[string]int{}
	for i, kind := range types {
		for _, section := range projectImportSourceSpecs[kind].CurseForgeSections {
			if _, exists := cfClasses[section]; !exists {
				cfClasses[section] = 100 + i
			}
		}
		for _, provider := range []string{"modrinth", "curseforge"} {
			references["tp008-"+strings.ReplaceAll(kind, "_", "-")+"-"+provider] = kind
		}
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("x-api-key") != "" {
			http.Error(w, "unexpected credential", 500)
			t.Error("public controlled source received credentials")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/project/") {
			reference := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/project/"), "/version")
			if strings.HasSuffix(r.URL.Path, "/version") {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			kind := references[reference]
			spec := projectImportSourceSpecs[kind]
			if len(spec.ModrinthSections) == 0 {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": reference, "slug": reference, "title": "TP008 " + kind + " " + reference, "description": "Synthetic controlled-provider sample", "body": "TP008 actual adapter body " + reference, "project_type": spec.ModrinthSections[0], "status": "approved", "game_versions": []string{"1.20.1"}, "loaders": []string{"paper", "iris"}, "license": map[string]string{"id": "MIT"}})
			return
		}
		switch r.URL.Path {
		case "/categories":
			classes := []map[string]any{}
			for section, id := range cfClasses {
				classes = append(classes, map[string]any{"id": id, "slug": section, "isClass": true})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": classes})
		case "/mods/search":
			reference := r.URL.Query().Get("slug")
			classID, _ := strconv.Atoi(r.URL.Query().Get("classId"))
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": classID, "classId": classID, "slug": reference, "name": "TP008 " + references[reference] + " " + reference, "summary": "Synthetic controlled-provider sample", "isAvailable": true, "latestFiles": []map[string]any{{"gameVersions": []string{"1.20.1", "Paper", "Iris"}}}, "links": map[string]string{"websiteUrl": "https://www.curseforge.com/minecraft/source/" + reference}}}})
		default:
			if strings.HasSuffix(r.URL.Path, "/description") {
				_ = json.NewEncoder(w).Encode(map[string]string{"data": "<p>TP008 actual adapter controlled-source body</p>"})
				return
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	providerConfig := defaultModImportConfig()
	providerConfig.Modrinth.BaseURL = upstream.URL
	providerConfig.Modrinth.Token = ""
	providerConfig.CurseForge.BaseURL = upstream.URL
	providerConfig.CurseForge.Enabled = false
	providerConfig.CurseForge.APIKey = ""
	providerConfig.GitHub.Token = ""
	sealed, err := f.server.sealSystemSetting(providerConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `insert into system_settings(key,value) values($1,$2::jsonb) on conflict(key) do update set value=excluded.value`, modImportConfigSettingKey, sealed); err != nil {
		t.Fatal(err)
	}
	// Persist actual adapter output, without substituting any product API response.
	combinations := []map[string]any{}
	for _, kind := range types {
		spec := projectImportSourceSpecs[kind]
		for _, provider := range []string{"modrinth", "curseforge"} {
			if provider == "modrinth" && len(spec.ModrinthSections) == 0 {
				continue
			}
			reference := "tp008-" + strings.ReplaceAll(kind, "_", "-") + "-" + provider
			sourceURL := "https://www.curseforge.com/minecraft/" + spec.CurseForgeSections[0] + "/" + reference
			row := map[string]any{"sampleId": reference, "projectType": kind, "provider": provider}
			if provider == "modrinth" {
				sourceURL = "https://modrinth.com/" + spec.ModrinthSections[0] + "/" + reference
				row["scope"] = "actual controlled-provider HTTP + job/JetStream/subscribed Worker + real browser/business API, no actual remote vendor"
			} else {
				sample, importErr := importCurseForgeSimpleProject(ctx, upstream.Client(), providerConfig, kind, sourceURL, reference)
				if importErr != nil {
					t.Fatal(importErr)
				}
				if err = normalizeAndValidateSimpleProjectDraft(&sample, true); err != nil {
					t.Fatal(err)
				}
				raw, marshalErr := json.Marshal(sample)
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				digest := sha256.Sum256(raw)
				samplePath := filepath.Join(directory, reference+".json")
				if err = os.WriteFile(samplePath, raw, 0600); err != nil {
					t.Fatal(err)
				}
				row["adapterSamplePath"] = samplePath
				row["adapterSampleSha256"] = hex.EncodeToString(digest[:])
				row["scope"] = "actual controlled-provider HTTP adapter output -> authenticated existing draft API -> real browser/business API; CF job/Worker blocked by absent official-origin credential; no completed job inserted"
			}
			row["sourceURL"] = sourceURL
			combinations = append(combinations, row)
		}
	}
	if len(combinations) != 11 {
		t.Fatal("eleven supported combinations required")
	}
	broker, err := natsserver.NewServer(&natsserver.Options{ServerName: "mcmods-owned-tp008", Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: filepath.Join(directory, "jetstream-store"), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go broker.Start()
	if !broker.ReadyForConnections(10 * time.Second) {
		t.Fatal("JetStream not ready")
	}
	t.Cleanup(func() { broker.Shutdown(); broker.WaitForShutdown() })
	cfg := f.server.cfg
	cfg.FrontendOrigin = frontendOrigin
	cfg.NATS = config.NATSConfig{Enabled: true, URL: broker.ClientURL(), SubjectPrefix: "mcmods-audit-tp008", OutboxEnabled: true, Tasks: cfg.NATS.Tasks, JetStream: config.JetStreamConfig{Enabled: true, Stream: "MCMODS_AUDIT_TP008", MaxDeliver: 5, AckWait: 30 * time.Second, PublishTimeout: 5 * time.Second}}
	q := queue.New(ctx, cfg.NATS)
	t.Cleanup(q.Close)
	q.SetDeadLetterSink(queue.NewPostgresDeadLetterSink(db))
	if state := q.Status(); !state.Connected || !state.JetStream {
		t.Fatal("actual queue unavailable")
	}
	f.origin.Close()
	api := NewServer(ctx, cfg, db, q, nil, nil, nil)
	origin := httptest.NewServer(api)
	t.Cleanup(func() {
		origin.Close()
		c, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if e := api.Shutdown(c); e != nil {
			t.Error(e)
		}
		api.cache.Close()
	})
	if err = NewModMetadataImportWorker(cfg, db, q).Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err = NewNotificationWorker(db, q, api.cache, cfg.SMTP, cfg.SettingsEncryptionKey).Start(); err != nil {
		t.Fatal(err)
	}
	if err = NewProjectUpdateNotificationWorker(db, q, api.cache).Start(ctx); err != nil {
		t.Fatal(err)
	}
	dispatcher := queue.NewOutboxDispatcher(db, q)
	dispatcher.Start(ctx)
	observer, err := nats.Connect(broker.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(observer.Close)
	js, err := observer.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	var creator, reviewer string
	if err = db.QueryRow(ctx, `select public_id from users where id=$1`, f.userIDs[f.editor]).Scan(&creator); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, `select public_id from users where id=$1`, f.userIDs[f.reviewer]).Scan(&reviewer); err != nil {
		t.Fatal(err)
	}
	authPath := filepath.Join(directory, "auth.private.json")
	auditJourneyWriteJSON(t, authPath, map[string]any{"creatorToken": f.editor, "reviewerToken": f.reviewer})
	runtimePath, stopPath := filepath.Join(directory, "runtime.json"), filepath.Join(directory, "STOP")
	runtime := map[string]any{"schemaVersion": 1, "pid": os.Getpid(), "apiOrigin": origin.URL, "frontendOrigin": frontendOrigin, "authPath": authPath, "stopPath": stopPath, "repoRevisions": revisions, "creatorPublicId": creator, "reviewerPublicId": reviewer, "parentModPublicId": f.modCode, "parentModSiteId": "audit-gr-" + f.modCode, "parentModName": "Audit game import 0", "relationCreatorPublicId": relationCreator, "relationCreatorName": "TP008 Public Author", "supportedCombinations": combinations, "natsURL": broker.ClientURL(), "natsStream": cfg.NATS.JetStream.Stream, "antiAbuseEnabled": cfg.AntiAbuse.Enabled, "draftStorageQualification": map[string]any{"permission": "user.draft.retention_seconds.2592000", "assignedBeforeAPIExposure": true, "viaActualRegistrationRolePath": false, "configuration": "isolated migration helper has no registration-role seeds; explicit ordinary capability from existing registered seed", "sources": []string{"internal/database/seeds.go:seedRoles[registered]", "internal/httpapi/draft_handlers.go:draftRetentionSeconds"}}, "scope": "Actual controlled provider HTTP, API/PostgreSQL, real JetStream+metadata Worker for MR5. CF6 adapter+draft boundary only. External browser evidence and final DB/queue review required; fixture exit is not acceptance."}
	update := func(state string) {
		runtime["state"] = state
		runtime["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
		runtime["queueStatus"] = q.Status()
		runtime["outboxMetrics"] = dispatcher.Metrics()
		runtime["cacheMetrics"] = api.cache.Metrics()
		states := []map[string]any{}
		for c := range js.ConsumersInfo(cfg.NATS.JetStream.Stream, nats.MaxWait(2*time.Second)) {
			states = append(states, map[string]any{"name": c.Name, "delivered": c.Delivered.Consumer, "acknowledged": c.AckFloor.Consumer, "pending": c.NumPending, "ackPending": c.NumAckPending, "redelivered": c.NumRedelivered})
		}
		runtime["consumerStates"] = states
		var jobs, projects, outbox []byte
		if e := db.QueryRow(ctx, `select coalesce((select jsonb_agg(jsonb_build_object('publicId',public_id,'projectType',project_type,'provider',provider,'status',status,'progress',progress,'errorPresent',error<>'')) from mod_metadata_import_jobs where user_id=$1),'[]'::jsonb),coalesce((select jsonb_agg(jsonb_build_object('publicId',public_id,'projectType',project_type,'siteId',slug,'reviewStatus',review_status,'publishedRevisionId',published_revision_id)) from simple_projects where submitted_by=$1),'[]'::jsonb),coalesce((select jsonb_object_agg(status,n) from (select status,count(*) n from nats_outbox group by status) grouped),'{}'::jsonb)`, f.userIDs[f.editor]).Scan(&jobs, &projects, &outbox); e != nil {
			runtime["snapshotError"] = true
		} else {
			runtime["jobs"] = json.RawMessage(jobs)
			runtime["projects"] = json.RawMessage(projects)
			runtime["outbox"] = json.RawMessage(outbox)
			runtime["snapshotError"] = false
		}
		auditJourneyWriteJSON(t, runtimePath, runtime)
	}
	update("READY")
	t.Logf("owned TP008 fixture READY runtime=%s; synthetic authentication stored separately with mode0600", runtimePath)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			update("EXPIRED")
			t.Fatal("owned TP008 fixture expired before STOP")
		case <-ticker.C:
			if _, e := os.Stat(stopPath); e == nil {
				update("STOPPED")
				return
			} else if !os.IsNotExist(e) {
				t.Fatal(e)
			}
			update("READY")
		}
	}
}
