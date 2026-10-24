package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/activity"
)

func auditImportActivityFacts(t *testing.T, f test013Fixture) map[string]string {
	t.Helper()
	facts := auditAccessActivityFacts(t, f)
	for _, table := range []string{"system_settings", "catalog_import_jobs", "catalog_import_packages", "catalog_import_revisions", "catalog_import_job_artifacts", "mod_metadata_import_jobs", "oss_files", "oss_object_deletion_outbox", "user_drafts"} {
		var value string
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` fact`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		facts[table] = value
	}
	return facts
}

func auditImportActivityFailRecover(t *testing.T, f test013Fixture, method, path string, body any, want int, action, object int16) []byte {
	t.Helper()
	prior := auditFixActivityIntentCount(t, f, f.editor, action, object)
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], true)
	before := auditImportActivityFacts(t, f)
	raw := f.require(t, f.editor, method, path, body, 500)
	auditFixRequireSpecificActivityError(t, raw, "failed to persist import activity")
	auditFixRequireProjectFactsUnchanged(t, before, auditImportActivityFacts(t, f))
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], false)
	raw = f.require(t, f.editor, method, path, body, want)
	if got := auditFixActivityIntentCount(t, f, f.editor, action, object); got != prior+1 {
		t.Fatalf("actual activity intents=%d, prior%d", got, prior)
	}
	return raw
}

func TestAuditCMP003MetadataImportAndConfigurationActivityAtomic(t *testing.T) {
	for _, kind := range []string{"mod", "modpack", "config"} {
		t.Run(kind, func(t *testing.T) {
			f := newTEST013Fixture(t)
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.create", "admin.config.write")
			if kind == "modpack" {
				grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "modpack.create")
			}
			path, method, want, action, object := "/api/v1/mod-imports", http.MethodPost, 202, activity.ActionCreate, activity.ObjectMod
			var body any = map[string]string{"provider": "modrinth", "url": "https://modrinth.com/mod/independent-atomic-import"}
			if kind == "modpack" {
				path = "/api/v1/modpack-imports"
				object = activity.ObjectModpack
				body = map[string]string{"provider": "modrinth", "url": "https://modrinth.com/modpack/independent-atomic-import"}
			}
			if kind == "config" {
				path = "/api/v1/admin/config/mod-imports"
				method = http.MethodPut
				want = 200
				action = activity.ActionEdit
				cfg := defaultModImportConfig()
				cfg.RequestTimeoutSeconds = 27
				body = cfg
			}
			auditImportActivityFailRecover(t, f, method, path, body, want, action, object)
		})
	}
}

func auditActivityCatalogUpload(t *testing.T, f auditFixGRFixture) string {
	t.Helper()
	entry := embeddedIconCatalogEntry{Name: "Activity block", EnglishName: "Activity block", RegisterName: "example:activity_block", Type: "Block", SmallIcon: embeddedIconTestPNG(t, 32, 32), LargeIcon: embeddedIconTestPNG(t, 32, 32)}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	digest := sha256.Sum256(raw)
	base := "/api/v1/mods/audit-gr-" + f.modCode + "/catalog-imports/uploads/"
	ticket := decodeTEST022Data[test037Ticket](t, f.require(t, f.editor, http.MethodPost, base+"presign", map[string]any{"originalName": "activity.json", "contentType": "application/json", "source": iconRendererImportSource, "sizeBytes": len(raw), "sha256": hex.EncodeToString(digest[:])}, 200))
	req, err := http.NewRequestWithContext(f.ctx, ticket.Method, ticket.URL, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range ticket.Headers {
		req.Header.Set(k, v)
	}
	res, err := f.provider.Client().Do(req)
	if err != nil {
		t.Fatal("controlled catalog source PUT failed")
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal("controlled catalog source PUT rejected")
	}
	file := decodeTEST022Data[map[string]any](t, f.require(t, f.editor, http.MethodPost, base+"complete", ticket.completion(), 201))
	return file["id"].(string)
}

func TestAuditCMP003CatalogAndExportJobsActivityAtomic(t *testing.T) {
	for _, mode := range []string{"export-create", "catalog-create", "export-cancel", "catalog-cancel", "export-retry", "catalog-retry", "confirm-modid"} {
		t.Run(mode, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			base := "/api/v1/mods/audit-gr-" + f.modCode + "/export-imports"
			file := ""
			var body any
			if strings.HasPrefix(mode, "catalog-") {
				base = "/api/v1/mods/audit-gr-" + f.modCode + "/catalog-imports"
				file = auditActivityCatalogUpload(t, f)
				body = map[string]any{"ossFileId": file, "targetVersionPublicId": f.versionPublic, "source": iconRendererImportSource}
			} else {
				file = f.upload(t, auditFixGRZip(t, nil))
				body = map[string]any{"ossFileId": file, "targetVersionPublicId": f.versionPublic}
			}
			path := base
			if !strings.HasSuffix(mode, "-create") {
				job := decodeTEST022Data[modExportJobResponse](t, f.require(t, f.editor, http.MethodPost, base, body, 202))
				path = base + "/" + job.ID + "/cancel"
				body = nil
				if strings.HasSuffix(mode, "-retry") {
					auditImportFailSourceAndRestore(t, f, file, job.ID)
					path = base + "/" + job.ID + "/retry"
				}
				if mode == "confirm-modid" {
					token, err := f.server.claimModExportJob(f.ctx, job.ID)
					if err != nil {
						t.Fatal(err)
					}
					paused, err := f.server.pauseCatalogImportForMODIDConfirmation(f.ctx, job.ID, token, f.modID, strings.Repeat("a", 64), map[string]int{"unmatchedmod": 1})
					if err != nil || !paused {
						t.Fatalf("real confirmation analysis pause=%v/%v", paused, err)
					}
					var hash string
					if err = f.db.QueryRow(f.ctx, `select modid_analysis_hash from catalog_import_jobs where id=$1`, job.ID).Scan(&hash); err != nil {
						t.Fatal(err)
					}
					path = base + "/" + job.ID + "/confirm-modid"
					body = confirmMODIDMismatchRequest{Confirm: true, AnalysisHash: hash}
				}
			}
			want := 202
			if strings.HasSuffix(mode, "-cancel") {
				want = 200
			}
			raw := auditImportActivityFailRecover(t, f.test013Fixture, http.MethodPost, path, body, want, activity.ActionCreate, activity.ObjectMod)
			if strings.HasSuffix(mode, "-create") {
				before := auditImportActivityFacts(t, f.test013Fixture)
				again := decodeTEST022Data[modExportJobResponse](t, f.require(t, f.editor, http.MethodPost, path, body, 202))
				first := decodeTEST022Data[modExportJobResponse](t, raw)
				if !again.Deduplicated || again.ID != first.ID {
					t.Fatal("same uploaded source/job replay did not deduplicate")
				}
				auditFixRequireProjectFactsUnchanged(t, before, auditImportActivityFacts(t, f.test013Fixture))
			}
		})
	}
}

func TestAuditCMP003ResumeExistingCapabilityHasNoNewActivityAndForbidsOverwrite(t *testing.T) {
	f := newAuditFixGRFixture(t)
	raw := auditFixGRZip(t, nil)
	digest := sha256.Sum256(raw)
	base := "/api/v1/mods/audit-gr-" + f.modCode + "/export-imports/uploads/"
	ticket := decodeTEST022Data[test037Ticket](t, f.require(t, f.editor, http.MethodPost, base+"presign", map[string]any{"originalName": "resume.zip", "contentType": "application/zip", "source": "mcmods_exporter", "sizeBytes": len(raw), "sha256": hex.EncodeToString(digest[:])}, 200))
	before := auditImportActivityFacts(t, f.test013Fixture)
	for range 3 {
		resumed := decodeTEST022Data[test037Ticket](t, f.require(t, f.editor, http.MethodPost, base+"resume", resumeModExportUploadRequest{ObjectKey: ticket.ObjectKey, OriginalName: ticket.OriginalName, ContentType: ticket.ContentType, SizeBytes: ticket.SizeBytes, SHA256: ticket.SHA256}, 200))
		immutable := false
		for k, v := range resumed.Headers {
			if strings.EqualFold(k, "x-oss-forbid-overwrite") && v == "true" {
				immutable = true
			}
		}
		if !immutable {
			t.Error("resumed write capability lacks the initial ticket's OSS forbid-overwrite protection")
		}
	}
	auditFixRequireProjectFactsUnchanged(t, before, auditImportActivityFacts(t, f.test013Fixture))
	if !t.Failed() {
		t.Log("Three real resume HTTP requests preserve existing capability only: no task/file/session/counter/activity/XP/currency writes; actual signed header immutable=true. Controlled OSS signing, no vendor storage immutability claim")
	}
}

func TestAuditGR007EmbeddedEqualDigestsKeepIndependentCreatorSources(t *testing.T) {
	f := newAuditFixGRFixture(t)
	file := auditActivityCatalogUpload(t, f)
	base := "/api/v1/mods/audit-gr-" + f.modCode + "/catalog-imports"
	first := decodeTEST022Data[modExportJobResponse](t, f.require(t, f.editor, http.MethodPost, base, map[string]any{"ossFileId": file, "targetVersionPublicId": f.versionPublic, "source": iconRendererImportSource}, 202))
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "project.edit."+f.modCode, "user.file.single_limit.64", "user.file.daily_limit.256", "user.file.total_limit.256")
	other := f
	other.editor = f.otherEditor
	secondFile := auditActivityCatalogUpload(t, other)
	if secondFile == file {
		t.Log("Controlled OSS independently deduplicated bytes; actual owner eligibility still required")
	}
	second := decodeTEST022Data[modExportJobResponse](t, f.require(t, f.otherEditor, http.MethodPost, base, map[string]any{"ossFileId": secondFile, "targetVersionPublicId": f.versionPublic, "source": iconRendererImportSource}, 202))
	if second.ID == first.ID || second.PackageID == first.PackageID {
		t.Fatal("equal bytes from different owned source files merged jobs or packages")
	}
	before := auditImportActivityFacts(t, f.test013Fixture)
	f.require(t, f.otherEditor, http.MethodPost, base+"/"+first.ID+"/cancel", nil, 409)
	f.require(t, f.otherEditor, http.MethodPost, base+"/"+first.ID+"/retry", nil, 409)
	auditFixRequireProjectFactsUnchanged(t, before, auditImportActivityFacts(t, f.test013Fixture))
	var creator int64
	if err := f.db.QueryRow(f.ctx, `select created_by from catalog_import_jobs where id=$1`, first.ID).Scan(&creator); err != nil || creator != f.userIDs[f.editor] {
		t.Fatalf("original creator changed=%d/%v", creator, err)
	}
}

func auditImportFailSourceAndRestore(t *testing.T, f auditFixGRFixture, file, job string) {
	t.Helper()
	var key string
	if err := f.db.QueryRow(f.ctx, `select object_key from oss_files where public_id=$1`, file).Scan(&key); err != nil {
		t.Fatal(err)
	}
	f.objectMu.Lock()
	source, exists := f.objects[key]
	delete(f.objects, key)
	f.objectMu.Unlock()
	if !exists {
		t.Fatal("controlled source object absent before outage")
	}
	err := f.server.importModExportJob(f.ctx, job)
	f.objectMu.Lock()
	f.objects[key] = source
	f.objectMu.Unlock()
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("actual business Worker did not reach controlled missing source404: %v", err)
	}
	var state, code string
	if err = f.db.QueryRow(f.ctx, `select status,error_code from catalog_import_jobs where id=$1`, job).Scan(&state, &code); err != nil || state != "failed" || code != "import_failed" {
		t.Fatalf("source outage status=%s code=%s err=%v", state, code, err)
	}
	t.Log("Actual queued business Worker source HTTP404 failed before staging; exact immutable source restored for real retry, no anti-abuse budget consumed")
}

func TestAuditCMP003ImportControlReplaysStayWithinDefaultBudget(t *testing.T) {
	for _, mode := range []string{"cancel", "retry", "confirm-modid"} {
		t.Run(mode, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			file := f.upload(t, auditFixGRZip(t, nil))
			job := f.job(t, file)
			base := "/api/v1/mods/audit-gr-" + f.modCode + "/export-imports/" + job.ID + "/"
			var body any
			want := 200
			if mode == "retry" {
				auditImportFailSourceAndRestore(t, f, file, job.ID)
				want = 202
			}
			if mode == "confirm-modid" {
				token, err := f.server.claimModExportJob(f.ctx, job.ID)
				if err != nil {
					t.Fatal(err)
				}
				paused, err := f.server.pauseCatalogImportForMODIDConfirmation(f.ctx, job.ID, token, f.modID, strings.Repeat("a", 64), map[string]int{"othermod": 1})
				if err != nil || !paused {
					t.Fatal("confirmation analysis did not pause")
				}
				var hash string
				if err = f.db.QueryRow(f.ctx, `select modid_analysis_hash from catalog_import_jobs where id=$1`, job.ID).Scan(&hash); err != nil {
					t.Fatal(err)
				}
				body = confirmMODIDMismatchRequest{Confirm: true, AnalysisHash: hash}
				want = 202
			}
			f.require(t, f.editor, http.MethodPost, base+mode, body, want)
			before := auditImportActivityFacts(t, f.test013Fixture)
			f.require(t, f.editor, http.MethodPost, base+mode, body, 409)
			auditFixRequireProjectFactsUnchanged(t, before, auditImportActivityFacts(t, f.test013Fixture))
			if mode == "retry" {
				if err := f.server.importModExportJob(f.ctx, job.ID); err != nil {
					t.Fatal(err)
				}
				var state string
				if err := f.db.QueryRow(f.ctx, `select status from catalog_import_jobs where id=$1`, job.ID).Scan(&state); err != nil || state != "ready" {
					t.Fatalf("recovered actual Worker status=%s/%v", state, err)
				}
			}
		})
	}
}

func TestAuditCMP003MetadataActivityConsumerFailureRestartRewardsOnce(t *testing.T) {
	f := newTEST013Fixture(t)
	actor := f.userIDs[f.editor]
	grantTEST044Permissions(t, f.ctx, f.db, actor, "project.create")
	if _, err := f.db.Exec(f.ctx, `update task_definitions set status='disabled'`); err != nil {
		t.Fatal(err)
	}
	var task int64
	if err := f.db.QueryRow(f.ctx, `insert into task_definitions(code,name,condition,rewards,status) values('audit_import_create_once','Import queue actual creation','{"action":"create","objectType":"mod","metric":"count","target":1}','{"experience":7,"currencies":{"diamond":2}}','active') returning id`).Scan(&task); err != nil {
		t.Fatal(err)
	}
	auditImportActivityFailRecover(t, f, http.MethodPost, "/api/v1/mod-imports", map[string]string{"provider": "modrinth", "url": "https://modrinth.com/mod/activity-consumer-recovery"}, 202, activity.ActionCreate, activity.ObjectMod)
	if _, err := f.db.Exec(f.ctx, `alter table user_experience add constraint audit_import_reward_failure check(user_id<>`+fmt.Sprint(actor)+`) not valid`); err != nil {
		t.Fatal(err)
	}
	m := auditFixActivityMonitor(t, f)
	auditFixWaitActivity(t, func() bool {
		var n int
		err := f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where user_id=$1 and attempts>0`, actor).Scan(&n)
		return err == nil && n == 1
	})
	var events, rewards int
	if err := f.db.QueryRow(f.ctx, `select (select count(*) from user_activity_events where user_id=$1),(select count(*) from currency_transactions where user_id=$1)`, actor).Scan(&events, &rewards); err != nil || events != 0 || rewards != 0 {
		t.Fatalf("failed consumer left partial activity/reward %d/%d/%v", events, rewards, err)
	}
	stop, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	if err := m.Close(stop); err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := f.db.Exec(f.ctx, `alter table user_experience drop constraint audit_import_reward_failure`); err != nil {
		t.Fatal(err)
	}
	auditFixActivityMonitor(t, f)
	auditFixWaitActivity(t, func() bool {
		var n int
		err := f.db.QueryRow(f.ctx, `select count(*) from user_task_progress where user_id=$1 and task_id=$2 and rewarded_at is not null`, actor, task).Scan(&n)
		return err == nil && n == 1
	})
	var progress, rewarded, xp, balance, ledgers, outbox int64
	if err := f.db.QueryRow(f.ctx, `select (select count(*) from user_activity_events where user_id=$1 and action_id=$3 and object_type_id=$4),(select progress from user_task_progress where user_id=$1 and task_id=$2),(select count(*) from user_task_progress where user_id=$1 and task_id=$2 and rewarded_at is not null),(select experience from user_experience where user_id=$1),(select balance from user_currency_balances b join currencies c on c.id=b.currency_id where b.user_id=$1 and c.code='diamond'),(select count(*) from currency_transactions where user_id=$1),(select count(*) from activity_event_outbox where user_id=$1)`, actor, task, activity.ActionCreate, activity.ObjectMod).Scan(&events, &progress, &rewarded, &xp, &balance, &ledgers, &outbox); err != nil {
		t.Fatal(err)
	}
	if events != 1 || progress != 1 || rewarded != 1 || xp != 7 || balance != 2 || ledgers != 1 || outbox != 0 {
		t.Fatalf("events/progress/reward/xp/balance/ledger/outbox=%d/%d/%d/%d/%d/%d/%d", events, progress, rewarded, xp, balance, ledgers, outbox)
	}
	t.Log("Actual metadata HTTP job+NATS intent+activity atomic; actual activity business consumer fails precise XP constraint, rolls back, restarts and grants exactly1 task reward/7XP/2diamonds/1ledger; no NATS transport or provider fetch claimed")
}
