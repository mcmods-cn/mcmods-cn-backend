package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/antiabuse"
)

func TestAuditAntiAbuseCanonicalDistinctFieldsActualHTTPProofSingleWriteIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	if !f.server.cfg.AntiAbuse.Enabled || f.server.cfg.AntiAbuse.ChallengeProvider != "proof" || f.server.cfg.AntiAbuse.ChallengeTTL != 10*time.Minute {
		t.Fatal("actual default enabled proof and ten-minute TTL required")
	}
	settings, err := f.server.antiAbuse.Settings(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	settingsBefore, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if settings.ChallengeThreshold != antiabuse.DefaultSettings().ChallengeThreshold {
		t.Fatal("unchanged default challenge policy required")
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "project.create", "project.create.mod")
	actor, _, token := createTEST044User(t, f.ctx, f.db, f.server.cfg, "canonical-proof-"+randomHex(4), "UTC")
	grantTEST044Permissions(t, f.ctx, f.db, actor, "project.create", "project.create.mod")
	// A new verified account and a normally aged one-use form place the
	// documented network duplicate rule in the challenge band. Keep the
	// actual Go client UA and all production risk thresholds unchanged.
	if _, err = f.db.Exec(f.ctx, `update users set email_verified=true where id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	payload := func(site string) createModRequest {
		return createModRequest{SiteID: site, PrimaryName: "Distinct visible primary name", Summary: "Legacy summary used only as independent compatibility input", BodyMarkdown: "Long independent technical description of the normal proof sample", DefaultLocale: "en-US", Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "Human authored source display name", Summary: "A short translated overview of the published source", ContentMarkdown: "Full localized content with separate wording from each overview"}}, Environment: "bothRequired", PrimaryCategory: "utility", ModIDs: []modIdentifierPayload{{Identifier: strings.ReplaceAll(site, "-", "_"), Primary: true}}, Compatibilities: []modLoaderCompatibilityPayload{{Loader: "forge", Versions: []string{"1.20.1"}}}, OfficialStatus: "active", SourceStatus: "open", License: "MIT", SubmissionMethod: "manual"}
	}
	seed := payload("canonical-seed-" + f.modCode)
	raw, err := json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	inspect := httptestCanonicalRequest(raw)
	_, content := inspectAntiAbuseBody(inspect)
	parts := strings.Split(content, "\n")
	unique := map[string]bool{}
	for _, p := range parts {
		unique[p] = true
	}
	if len(parts) != 4 || len(unique) != 4 {
		t.Fatal("four independent nonidentical collected business fields required")
	}
	expected := antiabuse.ContentHash(antiabuse.NormalizeContent(content))
	f.require(t, f.otherEditor, http.MethodPost, "/api/v1/mods", seed, 201)
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var exists bool
		if err = f.db.QueryRow(f.ctx, `select exists(select 1 from anti_abuse_content_fingerprints where user_id=$1 and action='review.submit' and exact_hash=$2)`, f.userIDs[f.otherEditor], expected).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			break
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("actual successful seed fingerprint was not persisted")
		}
	}
	target := payload("canonical-created-" + f.modCode)
	raw, err = json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	do := func(body []byte, form, proof string, want int) []byte {
		t.Helper()
		r, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.origin.URL+"/api/v1/mods", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		if form != "" {
			r.Header.Set("X-Anti-Abuse-Form", form)
		}
		if proof != "" {
			r.Header.Set("X-Anti-Abuse-Challenge", proof)
		}
		response, err := f.origin.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			var failure struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(data, &failure)
			t.Fatalf("normal actor proof status=%d expected=%d code=%s (body and credentials omitted)", response.StatusCode, want, failure.Code)
		}
		return data
	}
	form := decodeTEST022Data[antiabuse.FormToken](t, f.require(t, token, http.MethodGet, "/api/v1/anti-abuse/form-token?action=review.submit&object=/api/v1/mods", nil, 200))
	minimumAge := time.NewTimer(f.server.cfg.AntiAbuse.FormMinimumAge + 20*time.Millisecond)
	defer minimumAge.Stop()
	select {
	case <-minimumAge.C:
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	}
	before := auditResumeModpackFacts(t, f)
	challengeBody := do(raw, form.Token, "", 428)
	var failure struct {
		Code    string `json:"code"`
		Details struct {
			Challenge *antiabuse.ChallengeInfo `json:"challenge"`
		} `json:"details"`
	}
	if err = json.Unmarshal(challengeBody, &failure); err != nil || failure.Code != "challenge_required" || failure.Details.Challenge == nil {
		t.Fatal("precise actual challenge response required")
	}
	auditResumeRequireFacts(t, before, auditResumeModpackFacts(t, f))
	challenge := failure.Details.Challenge
	numbers := regexp.MustCompile(`\d+`).FindAllString(challenge.Prompt, -1)
	if challenge.Provider != "proof" || len(numbers) != 2 {
		t.Fatal("actual proof arithmetic required")
	}
	left, err := strconv.Atoi(numbers[0])
	if err != nil {
		t.Fatal(err)
	}
	right, err := strconv.Atoi(numbers[1])
	if err != nil {
		t.Fatal(err)
	}
	proof := challenge.ID + ":" + strconv.Itoa(left+right)
	// Preserve every actual business value while reserializing with a different
	// JSON key order from the original typed struct.
	var reordered map[string]any
	if err = json.Unmarshal(raw, &reordered); err != nil {
		t.Fatal(err)
	}
	retry, err := json.Marshal(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(raw, retry) {
		t.Fatal("different JSON serialization prerequisite absent")
	}
	_, retryContent := inspectAntiAbuseBody(httptestCanonicalRequest(retry))
	if antiabuse.ContentHash(antiabuse.NormalizeContent(retryContent)) != expected {
		t.Fatal("same genuine content changed fingerprint during challenge recovery")
	}
	created := decodeTEST022Data[modResponse](t, do(retry, "", proof, 201))
	if created.ReviewStatus != "pending" || created.SubmissionRevisionID == "" {
		t.Fatal("normal challenge success bypassed independent review")
	}
	var status string
	var rows, requests int
	if err = f.db.QueryRow(f.ctx, `select (select status from anti_abuse_challenges where public_id=$1),(select count(*) from mods where submitted_by=$2),(select count(*) from change_requests where submitted_by=$2)`, challenge.ID, actor).Scan(&status, &rows, &requests); err != nil {
		t.Fatal(err)
	}
	if status != "consumed" || rows != 1 || requests != 1 {
		t.Fatalf("proof/result mismatch consumed=%t rows=%d requests=%d", status == "consumed", rows, requests)
	}
	beforeReplay := auditResumeModpackFacts(t, f)
	replayed := do(retry, "", proof, 403)
	var replayError struct {
		Code string `json:"code"`
	}
	if err = json.Unmarshal(replayed, &replayError); err != nil || replayError.Code != "duplicate_content" {
		t.Fatal("replay did not hit precise duplicate refusal")
	}
	auditResumeRequireFacts(t, beforeReplay, auditResumeModpackFacts(t, f))
	settings, err = f.server.antiAbuse.Settings(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	settingsAfter, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(settingsBefore, settingsAfter) {
		t.Fatal("challenge/quota policy changed")
	}
	t.Log("CANONICAL_PROOF actual normal HTTP+PG four distinct fields, genuine default challenge428 zero business writes; correct proof with reordered keys201 one pending Mod+one request; consumed proof replay403 duplicate_content zero additional facts; no thresholds, quotas, TTLs, admin or UA override")
}

func httptestCanonicalRequest(raw []byte) *http.Request {
	r, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1/api/v1/mods", bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	return r
}
