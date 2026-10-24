package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This fixture exposes the actual API and fully migrated owned PostgreSQL for
// an external browser. Only prerequisites are published here through the real
// HTTP/no-review path. All five CONT001 subjects must be created and decided by
// browser users. No queue delivery or browser PASS follows from fixture exit.
func TestAuditLiveGlobalCatalogFixture(t *testing.T) {
	if os.Getenv("MCMODS_RUN_LIVE_GLOBAL_CATALOG") != "1" {
		t.Skip("opt-in actual global catalogue/browser fixture")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	top, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal(err)
	}
	frontend := os.Getenv("MCMODS_AUDIT_FRONTEND_REPO")
	if frontend == "" {
		frontend = "/workspace/mcmods-cn-frontend"
	}
	exporter := os.Getenv("MCMODS_AUDIT_EXPORTER_REPO")
	if exporter == "" {
		exporter = "/workspace/mcmods_exporter"
	}
	revisions := map[string]any{}
	for name, path := range map[string]string{"mcmods-cn-backend": strings.TrimSpace(string(top)), "mcmods-cn-frontend": frontend, "mcmods_exporter": exporter} {
		revision := auditJourneyRevision(path)
		if revision["error"] != nil || revision["dirty"] != false {
			t.Fatalf("clean identified actual checkout required: %s", name)
		}
		revisions[name] = revision
	}
	if pin := os.Getenv("MCMODS_AUDIT_EXPECT_BACKEND_COMMIT"); pin != "" && revisions["mcmods-cn-backend"].(map[string]any)["commit"] != pin {
		t.Fatal("actual compiled backend differs from full commit pin")
	}
	directory := os.Getenv("MCMODS_AUDIT_GLOBAL_CATALOG_DIR")
	if !filepath.IsAbs(directory) {
		t.Fatal("absolute fresh task-owned output directory required")
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	owner, err := os.OpenFile(filepath.Join(directory, "OWNER"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(owner, "mcmods-live-global-catalog pid=%d\n", os.Getpid())
	owner.Close()
	if err != nil {
		t.Fatal(err)
	}
	frontendOrigin := "http://127.0.0.1:3311"
	t.Setenv("FRONTEND_ORIGIN", frontendOrigin)
	f := newAuditFixGRFixture(t, 45*time.Minute)
	barrierContext, cancelBarrier := context.WithCancel(f.ctx)
	defer cancelBarrier()
	dbConfig := f.db.Config()
	dbConfig.MaxConns = 12
	dbConfig.MinConns = 1
	applicationName := "mcmods-live-global-" + f.modCode
	dbConfig.ConnConfig.RuntimeParams["application_name"] = applicationName
	db, err := pgxpool.NewWithConfig(f.ctx, dbConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	// These are existing ordinary capabilities, fixed before any browser request.
	// The isolated migration helper does not install registration-role seeds.
	readCaps := []string{"global_resource.list", "global_resource.view"}
	creatorCaps := append(append([]string{}, readCaps...), "content.write", "user.draft.retention_seconds.2592000")
	grantTEST044Permissions(t, f.ctx, db, f.userIDs[f.editor], creatorCaps...)
	grantTEST044Permissions(t, f.ctx, db, f.userIDs[f.reviewer], append(append([]string{}, readCaps...), "project.review."+f.modCode)...)
	grantTEST044Permissions(t, f.ctx, db, f.userIDs[f.denied], readCaps...)
	// The approved synthetic project declares the same supported1.20.1/Fabric
	// tuple that its normal version-creation API must validate. No target
	// catalogue subject or revision is inserted/approved here.
	if _, err = db.Exec(f.ctx, `insert into mod_loader_compatibilities(mod_id,loader,minecraft_version) values($1,'fabric','1.20.1') on conflict do nothing`, f.modID); err != nil {
		t.Fatal(err)
	}
	noReviewCaps := append(append([]string{}, creatorCaps...), "content.no-review", "project.edit."+f.modCode, "project.no-review."+f.modCode)
	grantTEST044Permissions(t, f.ctx, db, f.userIDs[f.otherEditor], noReviewCaps...)
	secondID, _, secondToken := createTEST044User(t, f.ctx, db, f.server.cfg, "global-second-"+randomCatalogPublicID(), "UTC")
	f.userIDs[secondToken] = secondID
	grantTEST044Permissions(t, f.ctx, db, secondID, append(append([]string{}, readCaps...), "project.edit."+f.modCode, "project.no-review."+f.modCode, "user.draft.retention_seconds.2592000")...)
	if _, err = db.Exec(f.ctx, `update users set created_at=now()-interval '30 days',email_verified=true where id=$1`, secondID); err != nil {
		t.Fatal(err)
	}
	review := defaultReviewConfig()
	review.CatalogCreate = true
	review.CatalogEdit = true
	review.CatalogDelete = true
	review.ModContentSectionCreate = true
	reviewRaw, _ := json.Marshal(review)
	if _, err = db.Exec(f.ctx, `insert into system_settings(key,value) values($1,$2::jsonb) on conflict(key) do update set value=excluded.value`, reviewConfigSettingKey, reviewRaw); err != nil {
		t.Fatal(err)
	}
	cfg := f.server.cfg
	cfg.FrontendOrigin = frontendOrigin
	// No NATS substitute is registered. The actual PostgreSQL outbox remains
	// durable/pending; its delivery and downstream effects are separate layers.
	cfg.NATS.Enabled = false
	f.origin.Close()
	api := NewServer(f.ctx, cfg, db, nil, nil, nil, nil)
	origin := httptest.NewServer(api)
	t.Cleanup(func() {
		origin.Close()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e := api.Shutdown(c); e != nil {
			t.Error(e)
		}
		api.cache.Close()
	})
	f.origin = origin
	f.db = db
	f.server = api
	localization := func(name string) map[string]any {
		return map[string]any{"reason": "Owned prerequisite; not a CONT001 subject", "defaultLocale": "en-US", "localizations": []map[string]any{{"locale": "en-US", "name": name, "summary": "Synthetic UI prerequisite", "contentMarkdown": "Synthetic public fixture"}}}
	}
	create := func(path string, payload map[string]any) catalogEditResult {
		raw := f.require(t, f.otherEditor, http.MethodPost, path, payload, 202)
		result := decodeTEST022Data[catalogEditResult](t, raw)
		if result.ReviewStatus != "approved" {
			t.Fatal("explicit no-review prerequisite did not publish through actual HTTP")
		}
		return result
	}
	resourcePayload := localization("Public catalogue fixture input")
	resourcePayload["kindCode"] = "minecraft.item"
	resourcePayload["canonicalId"] = "audit_ui:fixture_input"
	resourcePayload["definition"] = map[string]any{}
	dependencyResource := create("/api/v1/catalog/resources", resourcePayload)
	typePayload := localization("Public catalogue fixture type")
	typePayload["canonicalId"] = "audit_ui:fixture_type"
	typePayload["definition"] = map[string]any{}
	dependencyType := create("/api/v1/recipe-types", typePayload)
	templatePayload := localization("Public catalogue fixture layout")
	templatePayload["templateKey"] = "fixture-output"
	templatePayload["canvas"] = map[string]any{"width": 80, "height": 40, "imageScale": 1}
	templatePayload["definition"] = map[string]any{}
	templatePayload["slots"] = []map[string]any{{"slotKey": "input", "role": "input", "ordinal": 0, "rect": map[string]any{"x": 4, "y": 10, "width": 16, "height": 16}}, {"slotKey": "output", "role": "output", "ordinal": 1, "rect": map[string]any{"x": 40, "y": 10, "width": 16, "height": 16}}}
	dependencyTemplate := create("/api/v1/recipe-types/"+dependencyType.ObjectPublicID+"/templates", templatePayload)
	projectPath := "/api/v1/mods/audit-gr-" + f.modCode
	var builtIn string
	if err = db.QueryRow(f.ctx, `select public_id from mod_content_templates where builtin and code='item_block' and status='active'`).Scan(&builtIn); err != nil {
		t.Fatal(err)
	}
	type seedSet struct{ version, section, resource, template modContentMutationResult }
	var lastReviewBurst time.Time
	seedRPH := func(suffix string) seedSet {
		versionEdit := modContentVersionEdit{Label: "UI concurrency baseline" + suffix, MinecraftVersions: []string{"1.20.1"}, Loaders: []string{"fabric"}, ModVersion: "1.0" + suffix, Reason: "Owned prerequisite; not an RPH update"}
		version := decodeTEST022Data[modContentMutationResult](t, f.require(t, f.otherEditor, "POST", projectPath+"/content-versions", versionEdit, 201))
		if version.ReviewStatus != "approved" {
			t.Fatal("RPH version seed must be published through actual no-review HTTP")
		}
		// Version creation has its existing independent rate scope. Sections,
		// resources and templates share the unchanged3/60second review budget.
		if !lastReviewBurst.IsZero() {
			remaining := time.Until(lastReviewBurst.Add(61 * time.Second))
			if remaining > 0 {
				t.Log("waiting for the original review.submit burst window before new prerequisite requests")
				timer := time.NewTimer(remaining)
				select {
				case <-f.ctx.Done():
					timer.Stop()
					t.Fatal("fixture context ended during original quota window")
				case <-timer.C:
				}
			}
		}
		lastReviewBurst = time.Now()
		sectionEdit := modContentSectionEdit{VersionPublicID: version.PublicID, TemplatePublicID: builtIn, DefaultLocale: "en-US", DisplayMode: "compact", Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "UI concurrency section" + suffix}}, Reason: "Owned prerequisite"}
		section := decodeTEST022Data[modContentMutationResult](t, f.require(t, f.otherEditor, "POST", projectPath+"/content-sections", sectionEdit, 201))
		if section.ReviewStatus != "approved" {
			t.Fatal("RPH layout seed must be actually published")
		}
		resourceEdit := modContentResourceEdit{VersionPublicID: version.PublicID, SectionPublicID: &section.PublicID, CanonicalID: "example:ui_race_resource" + suffix, KindCode: "minecraft.item", EntryTypeCode: "default", DefaultLocale: "en-US", Definition: map[string]any{}, Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "UI race resource" + suffix}}, Reason: "Owned prerequisite"}
		resource := decodeTEST022Data[modContentMutationResult](t, f.require(t, f.otherEditor, "POST", projectPath+"/content-resources", resourceEdit, 201))
		if resource.ReviewStatus != "approved" {
			t.Fatal("RPH resource seed must be actually published")
		}
		templateEdit := modContentTemplateEdit{Code: "auditui" + suffix, DefaultLocale: "en-US", DefaultDisplayMode: "compact", Definition: templateSchemaGuardMap(false, "number", false), Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "UI race template" + suffix}}, Reason: "Owned prerequisite"}
		template := decodeTEST022Data[modContentMutationResult](t, f.require(t, f.otherEditor, "POST", projectPath+"/content-templates", templateEdit, 201))
		if template.ReviewStatus != "approved" {
			t.Fatal("RPH template seed must be actually published")
		}
		return seedSet{version, section, resource, template}
	}
	primary, secondary := seedRPH(""), seedRPH("2")
	targets := map[string]map[string]string{}
	for suffix, seed := range map[string]seedSet{"": primary, "_second": secondary} {
		targets["version"+suffix] = map[string]string{"publicId": seed.version.PublicID, "baseRevisionId": seed.version.RevisionID, "aggregateKey": modContentAggregateVersion + ":" + seed.version.PublicID}
		targets["resource"+suffix] = map[string]string{"publicId": seed.resource.PublicID, "baseRevisionId": seed.resource.RevisionID, "aggregateKey": modContentAggregateResource + ":" + seed.resource.PublicID + ":" + seed.version.PublicID}
		targets["template"+suffix] = map[string]string{"publicId": seed.template.PublicID, "baseRevisionId": seed.template.RevisionID, "aggregateKey": modContentAggregateTemplate + ":" + seed.template.PublicID}
		targets["layout"+suffix] = map[string]string{"publicId": seed.section.PublicID, "baseRevisionId": seed.section.RevisionID, "aggregateKey": modContentAggregateSection + ":" + seed.section.PublicID}
		for _, kind := range []string{"version", "resource", "template", "layout"} {
			targets[kind+suffix]["versionPublicId"] = seed.version.PublicID
			targets[kind+suffix]["sectionPublicId"] = seed.section.PublicID
		}
	}
	actors := map[string]string{}
	for name, token := range map[string]string{"creator": f.editor, "reviewer": f.reviewer, "reader": f.denied, "noReviewEditor": f.otherEditor, "secondNoReviewEditor": secondToken} {
		var publicID string
		if err = db.QueryRow(f.ctx, `select public_id from users where id=$1`, f.userIDs[token]).Scan(&publicID); err != nil {
			t.Fatal(err)
		}
		actors[name] = publicID
	}
	authPath := filepath.Join(directory, "auth.private.json")
	auditJourneyWriteJSON(t, authPath, map[string]string{"creatorToken": f.editor, "reviewerToken": f.reviewer, "readerToken": f.denied, "noReviewEditorToken": f.otherEditor, "secondNoReviewEditorToken": secondToken})
	runtimePath, stopPath := filepath.Join(directory, "runtime.json"), filepath.Join(directory, "STOP")
	runtime := map[string]any{"schemaVersion": 1, "pid": os.Getpid(), "apiOrigin": origin.URL, "frontendOrigin": frontendOrigin, "authPath": authPath, "stopPath": stopPath, "repoRevisions": revisions, "actors": actors, "antiAbuseEnabled": cfg.AntiAbuse.Enabled, "reviewConfig": review, "permissionSetup": "Explicit existing ordinary capabilities fixed before first API exposure; no admin; creator has no bypass; reviewer has content.review; reader only list/view; bootstrap/noReview editor separately declared", "draftQualification": "user.draft.retention_seconds.2592000 from existing registered seed; migration helper has no registration-role seeds; no actual registration UI claim", "dependencies": map[string]string{"resourcePublicId": dependencyResource.ObjectPublicID, "recipeTypePublicId": dependencyType.ObjectPublicID, "recipeTemplatePublicId": dependencyTemplate.ObjectPublicID}, "rph": map[string]any{"projectPublicId": f.modCode, "projectSiteId": "audit-gr-" + f.modCode, "versionPublicId": primary.version.PublicID, "sectionPublicId": primary.section.PublicID, "targets": targets, "groups": map[string]string{"twoTabs": "unsuffixed targets", "twoEditors": "_second targets"}, "normalReviewBurstReadyAt": lastReviewBurst.Add(61 * time.Second).UTC().Format(time.RFC3339Nano), "barrierArmPath": filepath.Join(directory, "rph-barrier-arm.json"), "targetRevisionsAreInitialSeeds": true, "barrierFormat": "unique attemptId[a-z0-9-], kind version/resource/template/layout or _second variants. Locked/released files use rph-barrier-<attemptId>-started.json and -released.json"}, "queueLayer": "NOT_RUN; real PostgreSQL outbox only, no actual NATS/dispatch/notification delivery claim", "scope": "Actual API/PostgreSQL normal review; only parent dependencies seeded through HTTP. External browser must create five subjects, review and verify publication. Fixture exit is not PASS."}
	barrierErrors := make(chan error, 1)
	go func() {
		barrierErrors <- auditGlobalCatalogBarrier(barrierContext, f.db, db, applicationName, directory, targets)
	}()
	update := func(state string) {
		runtime["state"] = state
		runtime["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
		runtime["cacheMetrics"] = api.cache.Metrics()
		var entities, requests, outbox []byte
		if e := db.QueryRow(f.ctx, `select coalesce((select jsonb_agg(jsonb_build_object('publicId',e.public_id,'kind',e.entity_type,'status',e.status,'publishedRevisionId',revision.public_id)) from catalog_entities e left join content_revisions revision on revision.id=e.published_revision_id),'[]'::jsonb),coalesce((select jsonb_agg(jsonb_build_object('publicId',q.public_id,'kind',q.aggregate_type,'key',q.aggregate_key,'status',q.status,'revisionPublicId',revision.public_id,'submitterPublicId',actor.public_id)) from change_requests q join content_revisions revision on revision.id=q.proposed_revision_id left join users actor on actor.id=q.submitted_by),'[]'::jsonb),coalesce((select jsonb_object_agg(status,n) from(select status,count(*) n from nats_outbox group by status) totals),'{}'::jsonb)`).Scan(&entities, &requests, &outbox); e != nil {
			runtime["snapshotError"] = true
		} else {
			runtime["entities"] = json.RawMessage(entities)
			runtime["requests"] = json.RawMessage(requests)
			runtime["outbox"] = json.RawMessage(outbox)
			runtime["snapshotError"] = false
		}
		auditJourneyWriteJSON(t, runtimePath, runtime)
	}
	update("READY")
	t.Logf("owned global catalogue fixture READY runtime=%s; authentication stored separately0600", runtimePath)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case err := <-barrierErrors:
			if err != nil {
				t.Fatal("owned RPH barrier failed:", err)
			}
			barrierErrors = nil
		case <-f.ctx.Done():
			update("EXPIRED")
			t.Fatal("owned global catalogue fixture expired before STOP")
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

// Optional external scheduling fence: actual browser requests must both reach
// the production aggregate lock before release. No API response is substituted.
func auditGlobalCatalogBarrier(ctx context.Context, control, observer *pgxpool.Pool, application, directory string, targets map[string]map[string]string) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	armPath := filepath.Join(directory, "rph-barrier-arm.json")
	validID := regexp.MustCompile(`^[a-z0-9-]{1,64}$`)
	persist := func(path string, value any) error {
		raw, e := json.MarshalIndent(value, "", "  ")
		if e != nil {
			return e
		}
		return os.WriteFile(path, raw, 0600)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			raw, e := os.ReadFile(armPath)
			if os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return e
			}
			var arm struct {
				AttemptID string `json:"attemptId"`
				Kind      string `json:"kind"`
			}
			if e = json.Unmarshal(raw, &arm); e != nil {
				return e
			}
			target, ok := targets[arm.Kind]
			if !ok || !validID.MatchString(arm.AttemptID) {
				return fmt.Errorf("invalid owned barrier target/attempt")
			}
			started := filepath.Join(directory, "rph-barrier-"+arm.AttemptID+"-started.json")
			if _, e = os.Stat(started); !os.IsNotExist(e) {
				return fmt.Errorf("refuse reused barrier attempt")
			}
			if e = os.Rename(armPath, filepath.Join(directory, "rph-barrier-"+arm.AttemptID+"-arm.json")); e != nil {
				return e
			}
			tx, e := control.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
			if e != nil {
				return e
			}
			finish := func() error {
				defer tx.Rollback(context.WithoutCancel(ctx))
				var pid int
				if e = tx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&pid); e != nil {
					return e
				}
				if _, e = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1))`, target["aggregateKey"]); e != nil {
					return e
				}
				if e = persist(started, map[string]any{"attemptId": arm.AttemptID, "kind": arm.Kind, "aggregateKey": target["aggregateKey"], "initialSeedRevisionId": target["baseRevisionId"], "lockedAt": time.Now().UTC().Format(time.RFC3339Nano), "actualDatabaseLock": true}); e != nil {
					return e
				}
				deadline := time.NewTimer(25 * time.Second)
				defer deadline.Stop()
				for {
					select {
					case <-ctx.Done():
						return nil
					case <-deadline.C:
						_ = persist(filepath.Join(directory, "rph-barrier-"+arm.AttemptID+"-timeout.json"), map[string]any{"status": "FAIL_TWO_ACTUAL_REQUESTS_DID_NOT_REACH_LOCK"})
						return fmt.Errorf("two actual RPH handlers did not reach owned aggregate lock")
					case <-ticker.C:
						var count int
						if e = observer.QueryRow(ctx, `select count(*) from pg_locks waiting join pg_stat_activity actor on actor.pid=waiting.pid join pg_locks held on held.locktype=waiting.locktype and held.database=waiting.database and held.classid=waiting.classid and held.objid=waiting.objid and held.objsubid=waiting.objsubid where not waiting.granted and waiting.locktype='advisory' and actor.application_name=$1 and held.pid=$2 and held.granted`, application, pid).Scan(&count); e != nil {
							return e
						}
						if count < 2 {
							continue
						}
						if e = tx.Commit(ctx); e != nil {
							return e
						}
						return persist(filepath.Join(directory, "rph-barrier-"+arm.AttemptID+"-released.json"), map[string]any{"attemptId": arm.AttemptID, "kind": arm.Kind, "actualWaitingHandlers": count, "releasedAt": time.Now().UTC().Format(time.RFC3339Nano), "status": "RELEASED_AFTER_TWO_ACTUAL_HANDLERS_WAITING"})
					}
				}
			}
			if e = finish(); e != nil {
				return e
			}
		}
	}
}
