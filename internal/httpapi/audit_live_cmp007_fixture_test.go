package httpapi

import (
	"archive/zip"
	"bytes"
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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

// Opt-in owned browser fixture. No target pack, review or import job is inserted
// here. Native browser requests must create the normal jobs and packs. The
// explicit synthetic existing Mod is a prerequisite, not an audited creation.
func TestAuditLiveCMP007MixedModpackFixture(t *testing.T) {
	if os.Getenv("MCMODS_RUN_LIVE_CMP007") != "1" {
		t.Skip("opt-in actual mixed modpack/browser fixture")
	}
	directory := os.Getenv("MCMODS_AUDIT_CMP007_DIR")
	if !filepath.IsAbs(directory) {
		t.Fatal("new absolute task-owned evidence directory required")
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	owner, err := os.OpenFile(filepath.Join(directory, "OWNER"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(owner, "mcmods-live-cmp007 pid=%d\n", os.Getpid())
	owner.Close()
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	top, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal(err)
	}
	frontend := os.Getenv("MCMODS_AUDIT_CMP007_FRONTEND_REPO")
	if frontend == "" {
		frontend = "/workspace/mcmods-cn-frontend"
	}
	revisions := map[string]any{}
	for name, path := range map[string]string{"mcmods-cn-backend": strings.TrimSpace(string(top)), "mcmods-cn-frontend": frontend, "mcmods_exporter": "/workspace/mcmods_exporter"} {
		rev := auditJourneyRevision(path)
		if rev["error"] != nil || rev["dirty"] != false {
			t.Fatalf("clean identified checkout required: %s", name)
		}
		revisions[name] = rev
	}
	frontendOrigin := "http://127.0.0.1:3351"
	t.Setenv("FRONTEND_ORIGIN", frontendOrigin)
	unlock := auditGlobalCatalogPGLock(t)
	t.Cleanup(func() { unlock() })
	f := newAuditFixGRFixture(t, 30*time.Minute)
	unlock()
	t.Cleanup(func() { unlock = auditGlobalCatalogPGLock(t) })
	dbConfig := f.db.Config()
	dbConfig.MaxConns = 12
	dbConfig.MinConns = 1
	db, err := pgxpool.NewWithConfig(f.ctx, dbConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	f.db = db
	auditResumeModpackReviewPolicy(t, f)
	grantTEST044Permissions(t, f.ctx, db, f.userIDs[f.editor], "user.draft.retention_seconds.2592000")
	knownSource := "known-" + f.modCode
	providerReference := "cmp007-provider-" + f.modCode
	future := "future-" + f.modCode
	if _, err = db.Exec(f.ctx, `update mods set modrinth_project_id=$2 where id=$1`, f.modID, knownSource); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	zipWriter := zip.NewWriter(&archive)
	entry, err := zipWriter.Create("modrinth.index.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.NewEncoder(entry).Encode(map[string]any{"formatVersion": 1, "game": "minecraft", "versionId": "cmp007-release", "name": "CMP007 owned provider manifest", "dependencies": map[string]string{"minecraft": "1.20.1", "forge": "47.4.21"}, "files": []map[string]any{{"path": "mods/recorded.jar", "downloads": []string{"https://cdn.modrinth.com/data/" + knownSource + "/versions/known-release/recorded.jar"}}, {"path": "mods/future.jar", "downloads": []string{"https://cdn.modrinth.com/data/" + future + "/versions/future-release/future.jar"}}}}); err != nil {
		t.Fatal(err)
	}
	if err = zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive.Bytes())
	if err = os.WriteFile(filepath.Join(directory, "controlled-provider.mrpack"), archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	var downloads atomic.Int32
	var upstream *httptest.Server
	upstream = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("x-api-key") != "" {
			t.Error("uncredentialed controlled source received credentials")
		}
		switch r.URL.Path {
		case "/project/" + providerReference:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": providerReference, "slug": providerReference, "title": "CMP007 provider mixed " + f.modCode, "description": "Provider archive with one recorded and one not-yet-indexed Mod", "body": "Controlled source manifest for the provider-imported mixed pack " + f.modCode, "project_type": "modpack", "status": "approved", "categories": []string{"quests"}, "client_side": "required", "server_side": "required", "license": map[string]string{"id": "MIT"}})
		case "/project/" + providerReference + "/version":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "provider-release", "name": "Owned release", "version_number": "1.0", "version_type": "release", "status": "listed", "date_published": "2026-10-01T00:00:00Z", "game_versions": []string{"1.20.1"}, "loaders": []string{"forge"}, "files": []map[string]any{{"url": upstream.URL + "/archive.mrpack", "filename": "controlled-provider.mrpack", "primary": true}}}})
		case "/archive.mrpack":
			downloads.Add(1)
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archive.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	original := http.DefaultTransport
	http.DefaultTransport = upstream.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = original })
	auditResumeMetadataSettings(t, f, "modrinth", upstream.URL)
	broker, err := natsserver.NewServer(&natsserver.Options{ServerName: "mcmods-owned-cmp007", Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: filepath.Join(directory, "jetstream-store"), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go broker.Start()
	if !broker.ReadyForConnections(10 * time.Second) {
		t.Fatal("actual JetStream not ready")
	}
	t.Cleanup(func() { broker.Shutdown(); broker.WaitForShutdown() })
	cfg := f.server.cfg
	workerContext, stopWorkers := context.WithCancel(f.ctx)
	t.Cleanup(stopWorkers)
	cfg.FrontendOrigin = frontendOrigin
	cfg.NATS = config.NATSConfig{Enabled: true, URL: broker.ClientURL(), SubjectPrefix: "mcmods-audit-cmp007", OutboxEnabled: true, Tasks: cfg.NATS.Tasks, JetStream: config.JetStreamConfig{Enabled: true, Stream: "MCMODS_AUDIT_CMP007", MaxDeliver: 5, AckWait: 30 * time.Second, PublishTimeout: 5 * time.Second}}
	q := queue.New(workerContext, cfg.NATS)
	t.Cleanup(q.Close)
	q.SetDeadLetterSink(queue.NewPostgresDeadLetterSink(db))
	if status := q.Status(); !status.Connected || !status.JetStream {
		t.Fatal("actual queue unavailable")
	}
	f.origin.Close()
	api := NewServer(workerContext, cfg, db, q, nil, nil, nil)
	origin := httptest.NewServer(api)
	t.Cleanup(func() {
		origin.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := api.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		api.cache.Close()
	})
	if err = NewModMetadataImportWorker(cfg, db, q).Start(workerContext); err != nil {
		t.Fatal(err)
	}
	if err = NewNotificationWorker(db, q, api.cache, cfg.SMTP, cfg.SettingsEncryptionKey).Start(); err != nil {
		t.Fatal(err)
	}
	if err = NewProjectUpdateNotificationWorker(db, q, api.cache).Start(workerContext); err != nil {
		t.Fatal(err)
	}
	dispatcher := queue.NewOutboxDispatcher(db, q)
	dispatcher.Start(workerContext)
	observer, err := nats.Connect(broker.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(observer.Close)
	js, err := observer.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	authPath := filepath.Join(directory, "auth.private.json")
	auditJourneyWriteJSON(t, authPath, map[string]string{"creatorToken": f.editor, "reviewerToken": f.reviewer, "otherEditorToken": f.otherEditor})
	actors := map[string]string{}
	for name, token := range map[string]string{"creator": f.editor, "reviewer": f.reviewer} {
		var public string
		if err = db.QueryRow(f.ctx, `select public_id from users where id=$1`, f.userIDs[token]).Scan(&public); err != nil {
			t.Fatal(err)
		}
		actors[name] = public
	}
	manual := auditResumeManualPack(f, "cmp007-manual-"+f.modCode, "manual-future-"+f.modCode)
	manual.PrimaryName = "CMP007 manual mixed " + f.modCode
	manual.Summary = "Manually curated known and external source references"
	manual.BodyMarkdown = "Manual authored mixed manifest with a known site Mod and a separately declared future source " + f.modCode
	stop := filepath.Join(directory, "STOP")
	runtime := map[string]any{"owner": "thirdparty-resume-cmp007-20261101", "pid": os.Getpid(), "apiOrigin": origin.URL, "frontendOrigin": frontendOrigin, "authPath": authPath, "stopPath": stop, "repoRevisions": revisions, "actors": actors, "antiAbuseEnabled": cfg.AntiAbuse.Enabled, "reviewPolicy": "ModpackCreate/ModCreate require independent review; creator lacks no-review/admin; explicit ordinary draft retention capability seeded before first browser API", "manualSource": map[string]any{"sampleId": "manual-mixed-" + f.modCode, "draftPayload": map[string]any{"changeReason": "", "draft": manual}, "scope": "Explicit user-authored payload for existing qualified private draft API; browser must restore and submit. No target pack or business revision has been inserted."}, "providerSource": map[string]any{"sampleId": providerReference, "provider": "modrinth", "url": "https://modrinth.com/modpack/" + providerReference, "archiveSHA256": hex.EncodeToString(digest[:]), "archiveBytes": archive.Len(), "expectedKnownProviderId": knownSource, "expectedFutureProviderId": future, "expectedFutureVersionId": "future-release", "expectedFutureFile": "future.jar"}, "knownMod": map[string]any{"publicId": f.modCode, "siteId": "audit-gr-" + f.modCode, "primaryName": "Audit game import 0"}, "natsURL": broker.ClientURL(), "natsStream": cfg.NATS.JetStream.Stream, "scope": "Controlled TLS MR provider, actual normal API+PostgreSQL, actual file-backed JetStream/subscribed metadata/notification/project-update Workers. Browser must create/review/publish targets. No real vendor, Minecraft or production deployment claim."}
	auditJourneyWriteJSON(t, filepath.Join(directory, "runtime.json"), runtime)
	t.Logf("CMP007_LIVE_READY runtime=%s", filepath.Join(directory, "runtime.json"))
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-f.ctx.Done():
			t.Fatal("owned CMP007 fixture deadline expired without completion STOP")
		case <-ticker.C:
			consumers := []map[string]any{}
			for c := range js.ConsumersInfo(cfg.NATS.JetStream.Stream, nats.MaxWait(2*time.Second)) {
				consumers = append(consumers, map[string]any{"name": c.Name, "filter": c.Config.FilterSubject, "delivered": c.Delivered.Consumer, "ackFloor": c.AckFloor.Consumer, "ackPending": c.NumAckPending, "pending": c.NumPending, "redelivered": c.NumRedelivered})
			}
			var jobs, packs, outbox json.RawMessage
			if err = db.QueryRow(f.ctx, `select coalesce((select jsonb_agg(jsonb_build_object('publicId',public_id,'status',status,'progress',progress,'errorPresent',error<>'')) from mod_metadata_import_jobs),'[]'::jsonb),coalesce((select jsonb_agg(jsonb_build_object('publicId',pack.public_id,'siteId',pack.slug,'status',pack.review_status,'revision',(select public_id from content_revisions where id=pack.published_revision_id),'mods',(select jsonb_agg(jsonb_build_object('provider',entry.provider,'providerProjectId',entry.provider_project_id,'providerVersionId',entry.provider_version_id,'fileName',entry.file_name,'modPublicId',target.project_code,'referenceStatus',unresolved.status)) from modpack_mods entry left join mods target on target.id=entry.mod_id left join unresolved_references unresolved on unresolved.source_type='modpack_mod' and unresolved.source_id=entry.id where entry.modpack_id=pack.id))) from modpacks pack),'[]'::jsonb),coalesce((select jsonb_object_agg(status,n) from(select status,count(*) n from nats_outbox group by status) grouped),'{}'::jsonb)`).Scan(&jobs, &packs, &outbox); err != nil {
				t.Fatal(err)
			}
			runtime["jobs"] = jobs
			runtime["packs"] = packs
			runtime["outbox"] = outbox
			runtime["consumers"] = consumers
			runtime["providerArchiveDownloads"] = downloads.Load()
			runtime["observedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
			auditJourneyWriteJSON(t, filepath.Join(directory, "runtime.json"), runtime)
			if _, err = os.Stat(stop); err == nil {
				auditJourneyWriteJSON(t, filepath.Join(directory, "runtime-terminal.json"), runtime)
				stopWorkers()
				t.Log("CMP007_LIVE_STOP owned graceful cleanup; runtime is evidence only, no browser acceptance inferred")
				return
			}
		}
	}
}
