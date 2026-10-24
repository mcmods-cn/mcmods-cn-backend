package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"mcmods-cn-backend/internal/antiabuse"
)

// This service seeds legitimate identical wording from another user's actual
// successful submission. The normal duplicate/network rule plus a new account
// triggers the unchanged production challenge policy; no forced risk state,
// synthetic challenge, permission bypass or shortened expiry is installed.
func TestAuditLiveRealChallengeBrowserFixture(t *testing.T) {
	if os.Getenv("MCMODS_RUN_LIVE_REAL_CHALLENGE") != "1" {
		t.Skip("opt-in real anti-abuse browser fixture")
	}
	dir, authPath, lockPath := os.Getenv("MCMODS_AUDIT_CHALLENGE_DIR"), os.Getenv("MCMODS_AUDIT_CHALLENGE_AUTH"), os.Getenv("MCMODS_AUDIT_PG_LOCK")
	if !filepath.IsAbs(dir) || !filepath.IsAbs(authPath) || !filepath.IsAbs(lockPath) {
		t.Fatal("absolute task paths required")
	}
	head, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	status, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil || len(bytes.TrimSpace(status)) != 0 {
		t.Fatal("requires clean immutable source checkout")
	}
	if err = os.Mkdir(dir, 0700); err != nil {
		t.Fatal("runtime must be new task-owned directory")
	}
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); _ = lock.Close() })
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	waitMinutes := 30
	if raw := os.Getenv("MCMODS_AUDIT_CHALLENGE_WAIT_MINUTES"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 20 || value > 40 {
			t.Fatal("owned fixture wait must be20..40minutes, product challenge TTL is unchanged")
		}
		waitMinutes = value
	}
	f := newAuditFixGRFixture(t, time.Duration(waitMinutes+2)*time.Minute)
	t.Cleanup(func() {
		if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
			t.Error(err)
		}
	})
	frontendOrigin := f.server.cfg.FrontendOrigin
	if (frontendOrigin != "http://127.0.0.1:3311" && frontendOrigin != "http://127.0.0.1:3331") || !f.server.cfg.AntiAbuse.Enabled || f.server.cfg.AntiAbuse.ChallengeProvider != "proof" || f.server.cfg.AntiAbuse.ChallengeTTL != 10*time.Minute || f.server.cfg.AntiAbuse.FormMinimumAge != 800*time.Millisecond {
		t.Fatal("requires actual default proof/10min TTL/800ms form age with exact browser origin")
	}
	expiryOnly := os.Getenv("MCMODS_AUDIT_CHALLENGE_MODE") == "expiry-only"
	caseID, frontendCommit := os.Getenv("MCMODS_AUDIT_CHALLENGE_CASE_ID"), os.Getenv("MCMODS_AUDIT_EXPECT_FRONTEND_COMMIT")
	if expiryOnly && (caseID == "" || !auditChallengeHex(frontendCommit, 40)) {
		t.Fatal("expiry-only run requires declared caseID and exact expected frontend commit")
	}
	settings, err := f.server.antiAbuse.Settings(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.ChallengeThreshold != antiabuse.DefaultSettings().ChallengeThreshold {
		t.Fatal("challenge threshold differs from default")
	}
	settingsBefore, _ := json.Marshal(settings)
	const content = "Independent legitimate community proof fixture wording"
	payload := func(slug string) createModRequest {
		return createModRequest{SiteID: slug, PrimaryName: content, SecondaryName: content, Summary: content, BodyMarkdown: content, DefaultLocale: "en-US", Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: content, Summary: content, ContentMarkdown: content}}, Environment: "bothRequired", PrimaryCategory: "utility", ModIDs: []modIdentifierPayload{{Identifier: strings.ReplaceAll(slug, "-", "_"), Primary: true}}, Compatibilities: []modLoaderCompatibilityPayload{{Loader: "forge", Versions: []string{"1.20.1"}}}, OfficialStatus: "active", SourceStatus: "open", License: "MIT", SubmissionMethod: "manual"}
	}
	seedRaw, _ := json.Marshal(payload("proof-reference-seed"))
	seedRequest, _ := http.NewRequest(http.MethodPost, "/api/v1/mods", bytes.NewReader(seedRaw))
	seedRequest.Header.Set("Content-Type", "application/json")
	_, seedContent := inspectAntiAbuseBody(seedRequest)
	normalizedSeed := antiabuse.NormalizeContent(seedContent)
	if strings.Count(normalizedSeed, strings.ToLower(content)) != 4 {
		t.Fatal("reference must match actual ModEditor legacy aliases plus default-localization four collected strings")
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "project.create", "project.create.mod")
	f.require(t, f.otherEditor, http.MethodPost, "/api/v1/mods", payload("proof-reference-seed"), 201)
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var n int
		if err = f.db.QueryRow(f.ctx, `select count(*) from anti_abuse_content_fingerprints where user_id=$1 and action='review.submit' and object_key='/api/v1/mods'`, f.userIDs[f.otherEditor]).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("actual successful seed did not produce fingerprint")
		case <-ticker.C:
		}
	}
	tokens := map[string]string{}
	ids := map[string]int64{}
	publicIDs := map[string]string{}
	for _, name := range []string{"creator", "expiry", "failure", "cancel", "probe"} {
		id, public, token := createTEST044User(t, f.ctx, f.db, f.server.cfg, "browser-real-proof-"+name+"-"+randomHex(4), "UTC")
		grantTEST044Permissions(t, f.ctx, f.db, id, "project.create", "project.create.mod")
		tokens[name+"Token"] = token
		ids[name] = id
		publicIDs[name] = public
	}
	// Real request preflight proves this ordinary state actually reaches 428.
	form := decodeTEST022Data[antiabuse.FormToken](t, f.require(t, tokens["probeToken"], http.MethodGet, "/api/v1/anti-abuse/form-token?action=review.submit&object=/api/v1/mods", nil, 200))
	select {
	case <-time.After(time.Second):
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	}
	body, _ := json.Marshal(payload("proof-probe-unused"))
	req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.origin.URL+"/api/v1/mods", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokens["probeToken"])
	req.Header.Set("X-Anti-Abuse-Form", form.Token)
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/130.0.0.0 Safari/537.36")
	res, err := f.origin.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var challenge map[string]any
	if err = json.NewDecoder(res.Body).Decode(&challenge); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 428 || challenge["code"] != "challenge_required" {
		t.Fatalf("ordinary exact-network duplicate did not reach precise challenge: status=%d body=%v", res.StatusCode, challenge)
	}
	delete(tokens, "probeToken")
	auth, _ := json.Marshal(tokens)
	private, err := os.OpenFile(authPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("private auth must be new")
	}
	if _, err = private.Write(auth); err != nil {
		private.Close()
		t.Fatal(err)
	}
	if err = private.Close(); err != nil {
		t.Fatal(err)
	}
	expectedCreated := 1
	if expiryOnly {
		expectedCreated = 0
	}
	runtime, _ := json.MarshalIndent(map[string]any{"apiOrigin": f.origin.URL, "frontendOrigin": frontendOrigin, "frontendExpectedCommit": frontendCommit, "caseId": caseID, "expiryOnly": expiryOnly, "authCookieName": authSessionCookieName, "authPath": authPath, "actorPublicIds": publicIDs, "payload": payload("proof-browser-created"), "expiryPayload": payload("proof-browser-expiry"), "failurePayload": payload("proof-browser-failure"), "cancelPayload": payload("proof-browser-cancel"), "normalizedSeedContentSHA256": antiabuse.ContentHash(normalizedSeed), "collectedEqualContentFields": 4, "sourceCommit": strings.TrimSpace(string(head)), "sourceTreeClean": true, "challengeTTLSeconds": 600, "minimumFormAgeMilliseconds": 800, "unchangedPolicy": true, "trigger": "actual successful ordinary other-user identical wording fingerprint + new unverified account; no force or threshold override", "preflight": "actual HTTP 428 challenge_required", "expectedCreatorPendingModCount": expectedCreated, "productionExecuted": false}, "", "  ")
	if err = os.WriteFile(filepath.Join(dir, "runtime.json"), runtime, 0600); err != nil {
		t.Fatal(err)
	}
	checkpoints, err := os.OpenFile(filepath.Join(dir, "sql-checkpoints.ndjson"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer checkpoints.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	t.Logf("REAL_CHALLENGE_READY runtime=%s source=%s defaultTTL600 ordinaryactors=true", filepath.Join(dir, "runtime.json"), strings.TrimSpace(string(head)))
	wait, cancel := context.WithTimeout(f.ctx, time.Duration(waitMinutes)*time.Minute)
	defer cancel()
	poll := time.NewTicker(250 * time.Millisecond)
	defer poll.Stop()
	var lastCheckpoint time.Time
	for {
		select {
		case <-wait.Done():
			t.Fatal("browser completion absent; no challenge acceptance inferred")
		case <-poll.C:
			if time.Since(lastCheckpoint) >= 15*time.Second {
				// Persist actual read-only observations while the same browser run
				// is live. A missing final marker cannot erase a genuine SQL fact,
				// and an intermediate checkpoint is never a final acceptance seal.
				observed, stop := context.WithTimeout(f.ctx, 5*time.Second)
				var at time.Time
				var total, pending, others int
				err := f.db.QueryRow(observed, `select clock_timestamp(),(select count(*) from mods where submitted_by=$1),(select count(*) from mods where submitted_by=$1 and review_status='pending'),(select count(*) from mods where submitted_by=any($2::bigint[]))`, ids["creator"], []int64{ids["expiry"], ids["failure"], ids["cancel"], ids["probe"]}).Scan(&at, &total, &pending, &others)
				stop()
				record := map[string]any{"observation": "INTERMEDIATE_NOT_FINAL_SEAL", "sourceCommit": strings.TrimSpace(string(head)), "productionExecuted": false}
				if err != nil {
					record["status"] = "BLOCKED_ENV_DB_OBSERVATION"
				} else {
					record["status"] = "OBSERVED"
					record["databaseTime"] = at
					record["creatorModCount"] = total
					record["creatorPendingModCount"] = pending
					record["otherActorModCount"] = others
				}
				line, _ := json.Marshal(record)
				if _, err := checkpoints.Write(append(line, '\n')); err != nil {
					t.Fatal("checkpoint write failed")
				}
				lastCheckpoint = time.Now()
			}
			if _, err = os.Stat(filepath.Join(dir, "done.flag")); os.IsNotExist(err) {
				continue
			} else if err != nil {
				t.Fatal(err)
			}
			seedHash := antiabuse.ContentHash(normalizedSeed)
			if err = writeAuditChallengeSQLSnapshot(f.ctx, f, ids, seedHash, filepath.Join(dir, "actors-before-final-seal.json")); err != nil {
				t.Fatal("could not preserve final pre-seal SQL facts: ", err)
			}
			var expirySeal map[string]any
			if expiryOnly {
				expirySeal, err = sealAuditExpiredBrowserRequest(f.ctx, f, ids, publicIDs, dir, caseID, strings.TrimSpace(string(head)), frontendCommit, seedHash)
				if err != nil {
					failure, _ := json.MarshalIndent(map[string]any{"result": "FAIL_REQUIRED_EXPIRY_EVENT_BINDING_NOT_VERIFIED", "reason": err.Error(), "productionExecuted": false}, "", "  ")
					_ = os.WriteFile(filepath.Join(dir, "expiry-specific-failure.json"), failure, 0600)
					_ = writeAuditChallengeSQLSnapshot(f.ctx, f, ids, seedHash, filepath.Join(dir, "actors-after-seal-failure.json"))
					t.Fatal("specific expiry request not verified; saved available facts: ", err)
				}
			}
			// Browser completion is the boundary: drain the owned telemetry
			// writers before reading final facts, then clean up this fixture.
			drain, stopDrain := context.WithTimeout(f.ctx, 10*time.Second)
			if err = f.server.antiAbuse.Close(drain); err != nil {
				stopDrain()
				t.Fatal("could not drain final anti-abuse telemetry")
			}
			stopDrain()
			if err = writeAuditChallengeSQLSnapshot(f.ctx, f, ids, seedHash, filepath.Join(dir, "actors-final-sql.json")); err != nil {
				t.Fatal("could not preserve final SQL facts: ", err)
			}
			if expiryOnly {
				encoded, _ := json.MarshalIndent(expirySeal, "", "  ")
				if err = os.WriteFile(filepath.Join(dir, "expiry-specific-seal.json"), encoded, 0600); err != nil {
					t.Fatal(err)
				}
			}
			var total, pending, others int
			if err = f.db.QueryRow(f.ctx, `select count(*),count(*) filter(where review_status='pending') from mods where submitted_by=$1`, ids["creator"]).Scan(&total, &pending); err != nil || total != expectedCreated || pending != expectedCreated {
				t.Fatalf("creator total=%d pending=%d err=%v", total, pending, err)
			}
			if err = f.db.QueryRow(f.ctx, `select count(*) from mods where submitted_by=any($1::bigint[])`, []int64{ids["expiry"], ids["failure"], ids["cancel"], ids["probe"]}).Scan(&others); err != nil || others != 0 {
				t.Fatalf("failed/expired/probe actors wrote mods=%d err=%v", others, err)
			}
			current, err := f.server.antiAbuse.Settings(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			after, _ := json.Marshal(current)
			if !bytes.Equal(settingsBefore, after) {
				t.Fatal("anti-abuse policy was changed")
			}
			report, _ := json.MarshalIndent(map[string]any{"result": "PASS", "scope": "actual browser proof business outcome independently checked; browser evidence owns error/cancel/expiry UI detail", "sourceCommit": strings.TrimSpace(string(head)), "frontendExpectedCommit": frontendCommit, "expiryOnly": expiryOnly, "specificExpiryEventBound": expiryOnly, "creatorModCount": total, "pendingModCount": pending, "failedExpiredProbeModCount": others, "policyUnchanged": true, "challengeTTLSeconds": 600, "productionExecuted": false}, "", "  ")
			if err = os.WriteFile(filepath.Join(dir, "backend-postconditions.json"), report, 0600); err != nil {
				t.Fatal(err)
			}
			t.Log(fmt.Sprintf("REAL_CHALLENGE_PASS creatorPending=%d others0 specificExpiryBound=%v policyUnchanged=true production=false", expectedCreated, expiryOnly))
			return
		}
	}
}
