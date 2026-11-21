package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

// A long-lived fixture is not acceptance by itself. All eleven browser-created
// jobs use real outbox/FileJetStream and a declared fixture subscription adapter
// invoking the unchanged Worker body. Only CF uses controlled credential TLS;
// MR keeps its normal uncredentialed local provider/visibility-GET client.
func TestAuditLiveTP008WorkerTransportFixture(t *testing.T) {
	if os.Getenv("MCMODS_RUN_LIVE_TP008_WORKER") != "1" {
		t.Skip("opt-in actual TP008 worker-body/native fixture")
	}
	directory := os.Getenv("MCMODS_AUDIT_TP008_DIR")
	if !filepath.IsAbs(directory) {
		t.Fatal("fresh absolute task-owned directory required")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		t.Fatal(err)
	}
	nonce := hex.EncodeToString(secret[:6])
	owner, err := os.OpenFile(filepath.Join(directory, "OWNER"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(owner, "mcmods-tp008-worker-native pid=%d nonce=%s\n", os.Getpid(), nonce)
	owner.Close()
	if err != nil {
		t.Fatal(err)
	}
	backend, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	frontend := os.Getenv("MCMODS_AUDIT_TP008_FRONTEND_CHECKOUT")
	exporter := os.Getenv("MCMODS_AUDIT_TP008_EXPORTER_CHECKOUT")
	if !filepath.IsAbs(frontend) || !filepath.IsAbs(exporter) {
		t.Fatal("explicit fixed frontend/exporter checkouts required")
	}
	revisions := map[string]any{}
	for name, location := range map[string]string{"mcmods-cn-backend": backend, "mcmods-cn-frontend": frontend, "mcmods_exporter": exporter} {
		revision := auditJourneyRevision(location)
		if revision["error"] != nil || revision["dirty"] != false {
			t.Fatalf("clean identified checkout required for %s", name)
		}
		revisions[name] = revision
	}
	if pin := os.Getenv("MCMODS_AUDIT_EXPECT_BACKEND_COMMIT"); pin == "" || revisions["mcmods-cn-backend"].(map[string]any)["commit"] != pin {
		t.Fatal("exact backend pin required")
	}
	for name, prefix := range map[string]string{"mcmods-cn-frontend": "e532dec", "mcmods_exporter": "8062148"} {
		if !strings.HasPrefix(revisions[name].(map[string]any)["commit"].(string), prefix) {
			t.Fatalf("declared cold tuple mismatch for %s", name)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Minute)
	defer cancel()
	db, cleanupDB := newAuditTP008OwnedDatabase(t, ctx)
	defer cleanupDB()
	cfg := config.Load()
	if !cfg.AntiAbuse.Enabled {
		t.Fatal("unchanged anti-abuse policy must remain enabled")
	}
	cfg.FrontendOrigin = "http://127.0.0.1:3551"
	cfg.JWTSecret = "tp008-worker-native-owned-signing-secret-at-least-32-bytes"
	cfg.SettingsEncryptionKey = "tp008-worker-native-owned-settings-secret-at-least-32-bytes"
	var api *Server
	var origin *httptest.Server
	var q *queue.Client
	var broker *natsserver.Server
	var observer *nats.Conn
	var callbackMu sync.Mutex
	var closed bool
	var callbackWG sync.WaitGroup
	var callbackFacts []map[string]any
	scanDone := make(chan struct{})
	scanStarted := false
	defer func() {
		cancel()
		if origin != nil {
			origin.CloseClientConnections()
			origin.Close()
		}
		if api != nil {
			stopCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			if e := api.Shutdown(stopCtx); e != nil {
				t.Error(e)
			}
			stop()
			api.cache.Close()
		}
		callbackMu.Lock()
		closed = true
		callbackMu.Unlock()
		if q != nil {
			q.Close()
		}
		done := make(chan struct{})
		go func() { callbackWG.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Error("owned subscription callbacks did not stop")
		}
		if scanStarted {
			select {
			case <-scanDone:
			case <-time.After(10 * time.Second):
				t.Error("owned manual-scan operator did not stop")
			}
		}
		if observer != nil {
			observer.Close()
		}
		if broker != nil {
			broker.Shutdown()
			broker.WaitForShutdown()
		}
	}()
	seq := int64(binary.BigEndian.Uint32(secret[8:12])) + 2000000000
	if _, err = db.Exec(ctx, `select setval(pg_get_serial_sequence('users','id'),$1,false)`, seq); err != nil {
		t.Fatal(err)
	}
	tokens, ids, publicIDs := map[string]string{}, map[string]int64{}, map[string]string{}
	for _, role := range []string{"creatorA", "creatorB", "denied", "reviewer", "scan", "revoked"} {
		id, public, token := createTEST044User(t, ctx, db, cfg, "tp008-"+nonce+"-"+strings.ToLower(role), "UTC")
		tokens[role], ids[role], publicIDs[role] = token, id, public
	}
	allIDs := []int64{}
	for _, id := range ids {
		allIDs = append(allIDs, id)
	}
	if _, err = db.Exec(ctx, `update users set created_at=clock_timestamp()-interval '30 days',email_verified=true where id=any($1::bigint[])`, allIDs); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"creatorA", "creatorB"} {
		permissions := []string{"user.draft.retention_seconds.2592000", "user.file.single_limit.64", "user.file.daily_limit.256", "user.file.total_limit.256"}
		for _, kind := range []string{"plugin", "map", "resource_pack", "shader_pack", "datapack", "addon"} {
			permissions = append(permissions, "project.create."+kind)
		}
		grantTEST044Permissions(t, ctx, db, ids[role], permissions...)
	}
	grantTEST044Permissions(t, ctx, db, ids["reviewer"], "project.review")
	grantTEST044Permissions(t, ctx, db, ids["scan"], "oss.write")
	grantTEST044Permissions(t, ctx, db, ids["revoked"], "project.create.plugin")
	if _, err = db.Exec(ctx, `delete from user_permissions where user_id=$1 and permission_id=(select id from permissions where code='project.create.plugin') and source='manual' and source_key=''`, ids["revoked"]); err != nil {
		t.Fatal(err)
	}
	parent := randomCatalogPublicID()
	parentSlug := "tp008_parent_" + nonce
	if _, err = db.Exec(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by) values($1,$2,'TP008 approved selectable parent','approved',$3)`, parent, parentSlug, ids["creatorA"]); err != nil {
		t.Fatal(err)
	}
	var author string
	authorName := "TP008 Selectable Author " + nonce
	if err = db.QueryRow(ctx, `insert into creators(kind,name,normalized_name,review_status) values('author',$1,lower($1),'approved') returning public_id`, authorName).Scan(&author); err != nil {
		t.Fatal(err)
	}
	cfKey := "tp008-owned-synthetic-" + hex.EncodeToString(secret[16:])
	sources := newAuditTP008Sources(t, nonce, cfKey)
	storage := newAuditTP008OwnedOSS(t)
	settings := defaultModImportConfig()
	settings.Modrinth.BaseURL, settings.Modrinth.Token = sources.MR.URL, ""
	settings.CurseForge.BaseURL, settings.CurseForge.APIKey, settings.CurseForge.Enabled = "https://api.curseforge.com/v1", cfKey, true
	settings.GitHub.Token = ""
	signer := &Server{cfg: cfg, db: db}
	for key, value := range map[string]any{modImportConfigSettingKey: settings, "oss.aliyun": storage.cfg} {
		sealed, e := signer.sealSystemSetting(value)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(ctx, `insert into system_settings(key,value) values($1,$2::jsonb) on conflict(key) do update set value=excluded.value`, key, sealed); e != nil {
			t.Fatal(e)
		}
	}
	inputImages, allowedPNGs := map[string]any{}, map[string]string{}
	sampleDirectory := filepath.Join(directory, "samples")
	if err = os.MkdirAll(sampleDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	for _, sample := range sources.Cases {
		picture := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				picture.SetNRGBA(x, y, color.NRGBA{R: uint8(17*sample.Index + x), G: uint8(11 * y), B: 193, A: 255})
			}
		}
		var raw bytes.Buffer
		if err = png.Encode(&raw, picture); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(sampleDirectory, fmt.Sprintf("gallery-%02d.png", sample.Index))
		if err = os.WriteFile(path, raw.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		digest := sha256Hex(raw.Bytes())
		allowedPNGs[digest] = sample.SampleID
		inputImages[sample.SampleID] = map[string]any{"path": path, "sha256": digest, "bytes": raw.Len(), "dimensions": []int{8, 8}}
	}
	broker, err = natsserver.NewServer(&natsserver.Options{ServerName: "mcmods-tp008-owned-" + nonce, Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: filepath.Join(directory, "jetstream-store"), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go broker.Start()
	if !broker.ReadyForConnections(10 * time.Second) {
		t.Fatal("owned FileJetStream unavailable")
	}
	cfg.NATS = config.NATSConfig{Enabled: true, URL: broker.ClientURL(), SubjectPrefix: "mcmods-tp008-" + nonce, OutboxEnabled: true, Tasks: cfg.NATS.Tasks, JetStream: config.JetStreamConfig{Enabled: true, Stream: "MCMODS_TP008_" + strings.ToUpper(nonce), MaxDeliver: 5, AckWait: 30 * time.Second, PublishTimeout: 5 * time.Second}}
	q = queue.New(ctx, cfg.NATS)
	q.SetDeadLetterSink(queue.NewPostgresDeadLetterSink(db))
	if state := q.Status(); !state.Connected || !state.JetStream {
		t.Fatal("actual queue unavailable")
	}
	api = NewServer(ctx, cfg, db, q, nil, nil, nil)
	origin = httptest.NewServer(api)
	factory := func(timeout time.Duration, baseURL string) (*http.Client, error) {
		if baseURL == sources.MR.URL {
			return newProviderHTTPClient(timeout, baseURL)
		}
		return sources.Transport.factory(timeout, baseURL)
	}
	if err = q.SubscribeTask(modMetadataImportTaskCode, func(callCtx context.Context, raw []byte) error {
		callbackMu.Lock()
		if closed {
			callbackMu.Unlock()
			return errors.New("owned fixture subscription stopped")
		}
		callbackWG.Add(1)
		callbackMu.Unlock()
		defer callbackWG.Done()
		var message modMetadataImportMessage
		if json.Unmarshal(raw, &message) != nil || strings.TrimSpace(message.JobID) == "" {
			return errors.New("invalid actual metadata task payload")
		}
		started := time.Now().UTC().Format(time.RFC3339Nano)
		e := api.runModMetadataImportWithProviderClientFactory(callCtx, message.JobID, factory)
		callbackMu.Lock()
		callbackFacts = append(callbackFacts, map[string]any{"jobId": message.JobID, "startedAt": started, "finishedAt": time.Now().UTC().Format(time.RFC3339Nano), "returnedError": e != nil, "layer": "actual FileJetStream fixture subscription adapter invokes unchanged Worker business body with explicit private provider dependency; not production Worker.Start"})
		callbackMu.Unlock()
		return e
	}); err != nil {
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
	observer, err = nats.Connect(broker.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	js, err := observer.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	scan := &auditTP008ManualScan{attempts: map[string]bool{}}
	scanStarted = true
	go func() {
		defer close(scanDone)
		scan.run(ctx, api, origin.URL, tokens["scan"], []int64{ids["creatorA"], ids["creatorB"]}, allowedPNGs)
	}()
	authPath := filepath.Join(directory, "auth.private.json")
	auditJourneyWriteJSON(t, authPath, tokens)
	runtime := map[string]any{"schemaVersion": 2, "pid": os.Getpid(), "nonce": nonce, "apiOrigin": origin.URL, "frontendOrigin": "http://127.0.0.1:3551", "nextOrigin": "http://127.0.0.1:3552", "authPath": authPath, "repoRevisions": revisions, "databaseName": db.Config().ConnConfig.Database, "actorPublicIds": publicIDs, "supportedCombinations": sources.Cases, "inputImages": inputImages, "ownedOSSOrigin": storage.server.URL, "parentModPublicId": parent, "parentModSiteId": parentSlug, "parentModName": "TP008 approved selectable parent", "relationCreatorPublicId": author, "relationCreatorName": authorName, "antiAbuseEnabled": cfg.AntiAbuse.Enabled, "credentialValueInEvidence": false, "scope": "ACTUAL_FRONTEND_API_POSTGRESQL_FILE_JETSTREAM_WORKER_BODY_CONTROLLED_CF_CREDENTIAL_TRANSPORT_FIXTURE_SUBSCRIPTION_ADAPTER; MR uncredentialed local HTTP production client; no official provider/DNS/TLS/SSRF, production Worker.Start, automatic virus engine or Minecraft claim", "fixtureExitIsBusinessAcceptance": false, "ordinaryCapabilityQualification": "Explicit existing ordinary create/draft/quota capabilities installed before API exposure; actual registration flow not claimed", "roles": "Fixed creatorA6/creatorB5, denied, revoked-before-exposure, independent project.review, separate oss.write manual scan operator, anonymous; no admin/wildcard/review bypass", "natsStream": cfg.NATS.JetStream.Stream, "natsURL": broker.ClientURL(), "ratePlan": map[string]any{"groups": [][]int{{0, 1}, {2, 3}, {4, 5}, {6, 7}, {8, 9}, {10}}, "earliestGroupSpacingSeconds": 61, "fixedActors": true, "automaticWriteReplay": false, "limitsUnchanged": true, "reason": "At most one normal image upload plus optional preview presign and one project create per actor/window; preserves5upload/3review burst and20review/hour"}}
	capture := func(state string) {
		runtime["state"], runtime["updatedAt"], runtime["queueStatus"], runtime["outboxMetrics"] = state, time.Now().UTC().Format(time.RFC3339Nano), q.Status(), dispatcher.Metrics()
		callbackMu.Lock()
		runtime["subscriptionInvocations"] = append([]map[string]any{}, callbackFacts...)
		callbackMu.Unlock()
		runtime["sources"], runtime["storage"], runtime["manualScans"] = sources.snapshot(), storage.snapshot(), scan.snapshot()
		rows, e := db.Query(ctx, `select public_id,project_type,provider,status,user_id,coalesce(result::text,'null'),error<>'' from mod_metadata_import_jobs order by id`)
		jobs := []map[string]any{}
		if e == nil {
			for rows.Next() {
				var id, kind, provider, status, result string
				var owner int64
				var failed bool
				if e = rows.Scan(&id, &kind, &provider, &status, &owner, &result, &failed); e != nil {
					break
				}
				job := map[string]any{"id": id, "projectType": kind, "provider": provider, "status": status, "ownerId": owner, "resultSha256": sha256Hex([]byte(result)), "errorPresent": failed}
				jobs = append(jobs, job)
				if status == "completed" {
					var decoded any
					if json.Unmarshal([]byte(result), &decoded) == nil {
						canonical, _ := json.Marshal(decoded)
						job["resultCanonicalSha256"] = sha256Hex(canonical)
					}
					evidencePath := filepath.Join(directory, "result-"+id+".json")
					job["resultEvidencePath"] = evidencePath
					previous, readErr := os.ReadFile(evidencePath)
					if os.IsNotExist(readErr) {
						if writeErr := os.WriteFile(evidencePath, []byte(result), 0600); writeErr != nil {
							job["resultEvidenceError"] = true
						}
					} else if readErr != nil || !bytes.Equal(previous, []byte(result)) {
						job["resultEvidenceChanged"] = true
					}
				}
			}
			if rows.Err() != nil {
				e = rows.Err()
			}
			rows.Close()
		}
		runtime["jobs"], runtime["snapshotError"] = jobs, e != nil
		var business []byte
		if e = db.QueryRow(ctx, `select jsonb_build_object('projects',(select coalesce(jsonb_agg(jsonb_build_object('id',public_id,'projectType',project_type,'siteId',slug,'reviewStatus',review_status,'publishedRevisionId',published_revision_id,'createdBy',submitted_by) order by id),'[]'::jsonb) from simple_projects),'outbox',(select coalesce(jsonb_object_agg(status,n),'{}'::jsonb) from (select status,count(*) n from nats_outbox group by status) grouped),'galleryCount',(select count(*) from simple_project_gallery_images),'creatorBindingCount',(select count(*) from content_creator_bindings),'parentCount',(select count(*) from simple_project_parent_refs))`).Scan(&business); e != nil {
			runtime["snapshotError"] = true
		} else {
			runtime["business"] = json.RawMessage(business)
		}
		consumers := []map[string]any{}
		for c := range js.ConsumersInfo(cfg.NATS.JetStream.Stream, nats.MaxWait(2*time.Second)) {
			consumers = append(consumers, map[string]any{"name": c.Name, "delivered": c.Delivered.Consumer, "acknowledged": c.AckFloor.Consumer, "pending": c.NumPending, "ackPending": c.NumAckPending, "redelivered": c.NumRedelivered})
		}
		runtime["consumerStates"] = consumers
		auditJourneyWriteJSON(t, filepath.Join(directory, "runtime.json"), runtime)
	}
	capture("READY")
	t.Logf("TP008 actual Worker-body/native fixture READY runtime=%s; synthetic sessions private0600, CFkey only encrypted normal settings", filepath.Join(directory, "runtime.json"))
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			runtime["state"], runtime["updatedAt"] = "EXPIRED", time.Now().UTC().Format(time.RFC3339Nano)
			auditJourneyWriteJSON(t, filepath.Join(directory, "runtime.json"), runtime)
			t.Fatal("owned TP008 fixture expired before STOP")
		case <-ticker.C:
			if _, e := os.Stat(filepath.Join(directory, "STOP")); e == nil {
				capture("STOPPED")
				return
			} else if !os.IsNotExist(e) {
				t.Fatal(e)
			}
			capture("READY")
		}
	}
}
