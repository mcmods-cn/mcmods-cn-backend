package httpapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"mcmods-cn-backend/internal/antiabuse"
)

// Native clients must use the real editor. The files below are synchronization
// and evidence only: publication still goes through independent HTTP review.
type auditModFormCase struct {
	CaseID              string                    `json:"caseId"`
	ActorPublicID       string                    `json:"actorPublicId"`
	ReviewerPublicID    string                    `json:"reviewerPublicId"`
	SiteID              string                    `json:"siteId"`
	Mode                string                    `json:"mode"`
	DefaultLocale       string                    `json:"defaultLocale"`
	OtherLocale         string                    `json:"otherLocale,omitempty"`
	NewURL              string                    `json:"newURL,omitempty"`
	EditURL             string                    `json:"editURL,omitempty"`
	PublicURL           string                    `json:"publicURL"`
	BaseRevisionID      string                    `json:"baseRevisionId,omitempty"`
	ProjectPublicID     string                    `json:"projectPublicId,omitempty"`
	EditPermission      string                    `json:"editPermission,omitempty"`
	FormValues          createModRequest          `json:"formValues"`
	Expected            []catalogLocalizationEdit `json:"expectedLocalizations"`
	InitialSnapshot     *createModRequest         `json:"initialSnapshot,omitempty"`
	ChangeReason        string                    `json:"changeReason,omitempty"`
	AdditionalRevisions int                       `json:"expectedFinalAdditionalRevisions"`
	ActorID             int64                     `json:"-"`
	ReviewerID          int64                     `json:"-"`
	ReviewerToken       string                    `json:"-"`
	ModID               int64                     `json:"-"`
	Token               string                    `json:"-"`
}

type auditModFormAttempt struct {
	CaseID           string `json:"caseId"`
	Method           string `json:"method"`
	Path             string `json:"path"`
	SiteID           string `json:"siteId"`
	Status           int    `json:"status"`
	Code             string `json:"code,omitempty"`
	RequestID        string `json:"requestId"`
	BodySHA256       string `json:"bodySHA256"`
	ContentSHA256    string `json:"contentSHA256"`
	IdempotencySHA   string `json:"idempotencyKeySHA256,omitempty"`
	ClientIDSHA      string `json:"clientIdSHA256,omitempty"`
	HasProof         bool   `json:"hasProof"`
	RetryAfter       int    `json:"retryAfterSeconds,omitempty"`
	ResponseEncoding string `json:"responseContentEncoding,omitempty"`
	ResponseDecoded  bool   `json:"responseJSONDecoded"`
	At               string `json:"at"`
}

type auditModFormRecorder struct {
	mu       sync.Mutex
	cases    map[string]string
	attempts []auditModFormAttempt
	file     *os.File
	writeErr error
}

type auditModFormResponse struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (w *auditModFormResponse) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *auditModFormResponse) Write(raw []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if w.body.Len()+len(raw) <= 1<<20 {
		_, _ = w.body.Write(raw)
	}
	return w.ResponseWriter.Write(raw)
}

func (recorder *auditModFormRecorder) register(site, caseID string) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.cases[site] = caseID
}

func (recorder *auditModFormRecorder) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || (r.URL.Path != "/api/v1/mods" && !(strings.HasPrefix(r.URL.Path, "/api/v1/mods/") && strings.HasSuffix(r.URL.Path, "/revisions"))) {
			next.ServeHTTP(w, r)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, maxJSONRequestBodyBytes+1))
		if err != nil {
			http.Error(w, "owned observer body read failed", 500)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var value struct {
			SiteID   string `json:"siteId"`
			Snapshot struct {
				SiteID string `json:"siteId"`
			} `json:"snapshot"`
		}
		_ = json.Unmarshal(raw, &value)
		site := value.SiteID
		if strings.HasSuffix(r.URL.Path, "/revisions") {
			site = value.Snapshot.SiteID
		}
		recorder.mu.Lock()
		caseID := recorder.cases[site]
		recorder.mu.Unlock()
		if caseID == "" {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.Header.Get("Idempotency-Key"), "owned-independent-revision-probe-") {
			caseID = "preflight-" + caseID
		}
		inspect, _ := http.NewRequest(http.MethodPost, r.URL.Path, bytes.NewReader(raw))
		inspect.Header.Set("Content-Type", "application/json")
		_, content := inspectAntiAbuseBody(inspect)
		attempt := auditModFormAttempt{CaseID: caseID, Method: r.Method, Path: r.URL.Path, SiteID: site, RequestID: r.Header.Get("X-Request-ID"), BodySHA256: sha256Hex(raw), ContentSHA256: antiabuse.ContentHash(antiabuse.NormalizeContent(content)), HasProof: r.Header.Get("X-Anti-Abuse-Challenge") != "", At: time.Now().UTC().Format(time.RFC3339Nano)}
		if key := r.Header.Get("Idempotency-Key"); key != "" {
			attempt.IdempotencySHA = sha256Hex([]byte(key))
		}
		if id := r.Header.Get("X-Client-ID"); id != "" {
			attempt.ClientIDSHA = sha256Hex([]byte(id))
		}
		observed := &auditModFormResponse{ResponseWriter: w}
		next.ServeHTTP(observed, r)
		attempt.Status = observed.status
		attempt.RetryAfter, _ = strconv.Atoi(observed.Header().Get("Retry-After"))
		attempt.ResponseEncoding = observed.Header().Get("Content-Encoding")
		decodedResponse := observed.body.Bytes()
		// This outer observation sees normal compressed wire bytes. Decode a
		// bounded copy for the error code without modifying any client response.
		if strings.EqualFold(attempt.ResponseEncoding, "gzip") {
			reader, err := gzip.NewReader(bytes.NewReader(decodedResponse))
			if err == nil {
				if value, readErr := io.ReadAll(io.LimitReader(reader, (1<<20)+1)); readErr == nil && len(value) <= 1<<20 {
					decodedResponse = value
				}
				_ = reader.Close()
			}
		}
		var errorBody struct {
			Code string `json:"code"`
		}
		attempt.ResponseDecoded = json.Unmarshal(decodedResponse, &errorBody) == nil
		attempt.Code = errorBody.Code
		recorder.mu.Lock()
		defer recorder.mu.Unlock()
		recorder.attempts = append(recorder.attempts, attempt)
		line, _ := json.Marshal(attempt)
		if _, err := recorder.file.Write(append(line, '\n')); err != nil && recorder.writeErr == nil {
			recorder.writeErr = err
		}
	})
}

func auditModFormJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

func auditModFormDo(t *testing.T, f auditFixGRFixture, token, method, path string, body any, headers map[string]string) (int, []byte, http.Header) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(raw)
	}
	r, err := http.NewRequestWithContext(f.ctx, method, f.origin.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/130.0.0.0 Safari/537.36")
	if method != http.MethodGet {
		r.Header.Set("X-Request-ID", "owned-form-"+randomCatalogPublicID())
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		r.Header.Set(key, value)
	}
	response, err := f.origin.Client().Do(r)
	if err != nil {
		t.Fatal("owned form actual HTTP failed")
	}
	defer response.Body.Close()
	result, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, result, response.Header.Clone()
}

func auditModFormInitialSubmit(t *testing.T, f auditFixGRFixture, token string, payload createModRequest) {
	t.Helper()
	auditModFormSubmitExpect(t, f, token, "/api/v1/mods", payload, "owned-initial-"+payload.SiteID, http.StatusCreated)
}

func auditModFormSubmitExpect(t *testing.T, f auditFixGRFixture, token, path string, body any, idempotency string, want int) (int, []byte) {
	t.Helper()
	for {
		form := decodeTEST022Data[antiabuse.FormToken](t, f.require(t, token, http.MethodGet, "/api/v1/anti-abuse/form-token?action=review.submit&object="+url.QueryEscape(path), nil, 200))
		timer := time.NewTimer(f.server.cfg.AntiAbuse.FormMinimumAge + 30*time.Millisecond)
		select {
		case <-timer.C:
		case <-f.ctx.Done():
			timer.Stop()
			t.Fatal(f.ctx.Err())
		}
		status, raw, headers := auditModFormDo(t, f, token, http.MethodPost, path, body, map[string]string{"X-Anti-Abuse-Form": form.Token, "Idempotency-Key": idempotency})
		if status == want {
			return status, raw
		}
		var failure struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(raw, &failure)
		if status != 429 || failure.Code != "rate_limited" {
			t.Fatalf("ordinary form status=%d expected=%d code=%s; no policy bypass", status, want, failure.Code)
		}
		seconds, err := strconv.Atoi(headers.Get("Retry-After"))
		if err != nil || seconds <= 0 {
			t.Fatal("normal form quota lacks precise positive Retry-After")
		}
		t.Logf("MOD_FORM_SETUP_QUOTA path=%s wait_actual_RetryAfter_seconds=%d snapshot_and_idempotency_unchanged=true", path, seconds)
		timer = time.NewTimer(time.Duration(seconds) * time.Second)
		select {
		case <-timer.C:
		case <-f.ctx.Done():
			timer.Stop()
			t.Fatal(f.ctx.Err())
		}
		// A previous response consumed its one-use form metadata, including a
		// quota refusal. Refresh it normally after the real wait, then keep the
		// same business body and idempotency key for the retry.
	}
}

func auditModFormPending(t *testing.T, f auditFixGRFixture, c *auditModFormCase) string {
	t.Helper()
	var revision string
	if err := f.db.QueryRow(f.ctx, `select mod.id,revision.public_id from mods mod join content_revisions revision on revision.entity_type='mod' and revision.entity_id=mod.id join change_requests request on request.proposed_revision_id=revision.id where mod.slug=$1 and mod.submitted_by=$2 and request.submitted_by=$2 and request.status='pending'`, c.SiteID, c.ActorID).Scan(&c.ModID, &revision); err != nil {
		t.Fatal("expected exact ordinary owned pending revision: ", err)
	}
	return revision
}

func auditModFormApprove(t *testing.T, f auditFixGRFixture, reviewer, site, revision string) []map[string]any {
	t.Helper()
	outcomes := []map[string]any{}
	for {
		status, raw, headers := auditModFormDo(t, f, reviewer, http.MethodPatch, "/api/v1/mods/"+site+"/revisions/"+revision, reviewModRevisionRequest{Status: "approved", Note: "ordinary independent native Mod form acceptance"}, nil)
		var failure struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(raw, &failure)
		outcomes = append(outcomes, map[string]any{"status": status, "code": failure.Code, "revisionId": revision, "requestId": headers.Get("X-Request-ID"), "at": time.Now().UTC().Format(time.RFC3339Nano)})
		if status == http.StatusOK {
			return outcomes
		}
		if status != http.StatusTooManyRequests || failure.Code != "rate_limited" {
			t.Fatalf("ordinary independent review status=%d; no review or actor bypass", status)
		}
		seconds, err := strconv.Atoi(headers.Get("Retry-After"))
		if err != nil || seconds <= 0 {
			t.Fatal("actual quota refused without precise positive Retry-After")
		}
		outcomes[len(outcomes)-1]["retryAfterSeconds"] = seconds
		t.Logf("MOD_FORM_REVIEW_QUOTA wait_actual_RetryAfter_seconds=%d", seconds)
		timer := time.NewTimer(time.Duration(seconds) * time.Second)
		select {
		case <-timer.C:
		case <-f.ctx.Done():
			timer.Stop()
			t.Fatal(f.ctx.Err())
		}
	}
}

func auditModFormPayload(site, primary, locale string) createModRequest {
	return createModRequest{SiteID: site, PrimaryName: primary, DefaultLocale: locale, Localizations: []catalogLocalizationEdit{}, Environment: "bothRequired", PrimaryCategory: "utility", OfficialStatus: "development", SourceStatus: "unknown", License: "Custom", SubmissionMethod: "manual", ModIDs: []modIdentifierPayload{{Identifier: strings.ReplaceAll(site, "-", "_"), Primary: true, MinecraftVersions: []string{}}}, Compatibilities: []modLoaderCompatibilityPayload{}, Tags: []string{}, SearchKeywords: []string{}, Authors: []modAuthorPayload{}, Links: []modLinkPayload{}, RelationshipGroups: []modRelationshipGroupPayload{}, GalleryImages: []modGalleryImagePayload{}}
}

func TestAuditLiveModFormFixture(t *testing.T) {
	if os.Getenv("MCMODS_RUN_LIVE_MOD_FORM") != "1" {
		t.Skip("opt-in owned actual Mod form/revision browser fixture")
	}
	mode := os.Getenv("MCMODS_AUDIT_MOD_FORM_MODE")
	localizationsOnly := mode == "localizations-only"
	if mode != "" && mode != "all" && !localizationsOnly {
		t.Fatal("unknown declared Mod form mode")
	}
	reviewerASize := 14
	if localizationsOnly {
		reviewerASize = 16
	}
	directory, frontend := os.Getenv("MCMODS_AUDIT_MOD_FORM_DIR"), os.Getenv("MCMODS_AUDIT_FRONTEND_REPO")
	if !filepath.IsAbs(directory) || !filepath.IsAbs(frontend) {
		t.Fatal("new absolute output and clean frontend checkout required")
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal("output directory must be new: ", err)
	}
	owner := fmt.Sprintf("mcmods-live-mod-form pid=%d\n", os.Getpid())
	if err := os.WriteFile(filepath.Join(directory, "OWNER"), []byte(owner), 0600); err != nil {
		t.Fatal(err)
	}
	top, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal(err)
	}
	checkouts := auditLiveSourceCheckouts(strings.TrimSpace(string(top)), frontend)
	revisions := checkouts.revisions(t)
	t.Setenv("FRONTEND_ORIGIN", "http://127.0.0.1:3531")
	unlockDDL := auditGlobalCatalogPGLock(t)
	t.Cleanup(func() { unlockDDL() })
	f := newAuditFixGRFixture(t, 65*time.Minute)
	unlockDDL()
	t.Cleanup(func() { unlockDDL = auditGlobalCatalogPGLock(t) })
	if !f.server.cfg.AntiAbuse.Enabled || f.server.cfg.AntiAbuse.ChallengeProvider != "proof" || f.server.cfg.AntiAbuse.ChallengeTTL != 10*time.Minute || f.server.cfg.AntiAbuse.FormMinimumAge != 800*time.Millisecond {
		t.Fatal("requires unchanged enabled proof/TTL600/formAge800 policy")
	}
	settings, err := f.server.antiAbuse.Settings(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.ChallengeThreshold != antiabuse.DefaultSettings().ChallengeThreshold {
		t.Fatal("requires default challenge threshold")
	}
	settingsBefore, _ := json.Marshal(settings)
	if settings.Policies["review.submit"].HourLimit != 20 {
		t.Fatal("predeclared two-reviewer matrix requires the actual normal HourLimit20; no quota is changed")
	}
	// 3 baseline approvals + 1 successful revision + 32 locale approvals =36.
	// One normal reviewer has20/hour. Declare the minimum two reviewers now,
	// before any matrix operation, and keep each at18 fixed operations.
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "content.review")
	reviewerPublicIDs := map[string]string{}
	for _, token := range []string{f.reviewer, f.denied} {
		var public string
		if err = f.db.QueryRow(f.ctx, `select public_id from users where id=$1 and email_verified and created_at<now()-interval '7 days'`, f.userIDs[token]).Scan(&public); err != nil {
			t.Fatal("ordinary established reviewer prerequisite missing")
		}
		reviewerPublicIDs[token] = public
	}
	attemptFile, err := os.OpenFile(filepath.Join(directory, "http-attempts.ndjson"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer attemptFile.Close()
	recorder := &auditModFormRecorder{file: attemptFile, cases: map[string]string{}}
	f.origin.Close()
	f.origin = httptest.NewServer(recorder.handler(f.server))
	t.Cleanup(f.origin.Close)
	private := map[string]any{"revision": map[string]string{}, "localizations": map[string]string{}, "reviewerToken": f.reviewer, "reviewerTokens": map[string]string{reviewerPublicIDs[f.reviewer]: f.reviewer, reviewerPublicIDs[f.denied]: f.denied}}
	locales := []string{"zh-CN", "zh-TW", "en-US", "ja-JP", "fr-FR", "de-DE", "es-ES", "ru-RU"}
	localeCases, revisionCases := []*auditModFormCase{}, []*auditModFormCase{}
	for index, locale := range locales {
		for _, mode := range []string{"no-localizations", "name-only-empty-body", "summary-only", "other-language-body-only"} {
			caseID := fmt.Sprintf("locale-%02d-%s", index, mode)
			site := "audit-loc-" + randomCatalogPublicID()
			primary := "Provided primary " + randomCatalogPublicID()
			id, public, token := createTEST044User(t, f.ctx, f.db, f.server.cfg, "mod-form-"+caseID+"-"+randomCatalogPublicID(), "UTC")
			f.userIDs[token] = id
			if localizationsOnly {
				// REM003 is ordinary form semantics, not new-account risk. This
				// fixed established cohort is declared before its first request.
				if _, err = f.db.Exec(f.ctx, `update users set created_at=now()-interval '30 days',email_verified=true where id=$1`, id); err != nil {
					t.Fatal(err)
				}
			}
			grantTEST044Permissions(t, f.ctx, f.db, id, "project.create", "project.create.mod")
			payload := auditModFormPayload(site, primary, locale)
			other := locales[(index+1)%len(locales)]
			if mode != "no-localizations" {
				payload.SecondaryName = "Entered name " + randomCatalogPublicID()
				entry := catalogLocalizationEdit{Locale: locale, Name: payload.SecondaryName}
				if mode == "summary-only" {
					entry.Summary = "Provided summary " + randomCatalogPublicID()
					payload.Summary = entry.Summary
				}
				payload.Localizations = append(payload.Localizations, entry)
				if mode == "other-language-body-only" {
					payload.Localizations = append(payload.Localizations, catalogLocalizationEdit{Locale: other, ContentMarkdown: "Only supplied other-language body " + randomCatalogPublicID()})
				}
			}
			// Expected public values come from the declared human input, not
			// the normalizer under test. Only the documented literal fallback
			// for a wholly absent localization is permitted.
			expected := append([]catalogLocalizationEdit{}, payload.Localizations...)
			if len(expected) == 0 {
				expected = []catalogLocalizationEdit{{Locale: locale, Name: primary}}
			}
			validation := payload
			if err = normalizeAndValidateModRequest(&validation); err != nil {
				t.Fatal("fixture semantic case invalid: ", err)
			}
			c := &auditModFormCase{CaseID: caseID, ActorPublicID: public, SiteID: site, Mode: mode, DefaultLocale: locale, OtherLocale: other, NewURL: "/mods/new", PublicURL: "/mods/" + site, FormValues: payload, Expected: expected, ActorID: id, Token: token}
			c.ReviewerToken = f.reviewer
			if len(localeCases) >= reviewerASize {
				c.ReviewerToken = f.denied
			}
			c.ReviewerID, c.ReviewerPublicID = f.userIDs[c.ReviewerToken], reviewerPublicIDs[c.ReviewerToken]
			localeCases = append(localeCases, c)
			private["localizations"].(map[string]string)[caseID] = token
			recorder.register(site, caseID)
		}
	}
	seedHash := ""
	preflights := []map[string]any{}
	if !localizationsOnly {
		for _, mode := range []string{"success", "wrong", "cancel"} {
			caseID, site := "revision-proof-"+mode, "audit-rev-"+randomCatalogPublicID()
			id, public, token := createTEST044User(t, f.ctx, f.db, f.server.cfg, "mod-form-"+mode+"-"+randomCatalogPublicID(), "UTC")
			f.userIDs[token] = id
			grantTEST044Permissions(t, f.ctx, f.db, id, "project.create", "project.create.mod")
			payload := auditModFormPayload(site, "Initial primary "+randomCatalogPublicID(), "en-US")
			payload.SecondaryName = "Initial supplied name " + randomCatalogPublicID()
			payload.Summary = "Independent initial overview " + randomCatalogPublicID()
			payload.BodyMarkdown = "Original baseline description " + randomCatalogPublicID()
			payload.Localizations = []catalogLocalizationEdit{{Locale: "en-US", Name: payload.SecondaryName, Summary: payload.Summary, ContentMarkdown: payload.BodyMarkdown}}
			c := &auditModFormCase{CaseID: caseID, ActorPublicID: public, SiteID: site, Mode: mode, DefaultLocale: "en-US", EditURL: "/mods/" + site + "/edit", PublicURL: "/mods/" + site, InitialSnapshot: &payload, ActorID: id, Token: token, ChangeReason: "User supplied revision reason retained during proof"}
			c.ReviewerToken, c.ReviewerID, c.ReviewerPublicID = f.reviewer, f.userIDs[f.reviewer], reviewerPublicIDs[f.reviewer]
			if mode == "success" {
				c.AdditionalRevisions = 1
			}
			recorder.register(site, caseID)
			auditModFormInitialSubmit(t, f, token, payload)
			revision := auditModFormPending(t, f, c)
			auditModFormApprove(t, f, c.ReviewerToken, c.SiteID, revision)
			if err = f.db.QueryRow(f.ctx, `select mod.project_code,revision.public_id from mods mod join content_revisions revision on revision.id=mod.published_revision_id where mod.id=$1`, c.ModID).Scan(&c.ProjectPublicID, &c.BaseRevisionID); err != nil {
				t.Fatal(err)
			}
			// Submission authorship alone is not the repository's edit policy.
			// Declare the ordinary author's one legitimate project edit scope.
			c.EditPermission = "project.edit." + c.ProjectPublicID
			grantTEST044Permissions(t, f.ctx, f.db, c.ActorID, c.EditPermission)
			f.require(t, token, http.MethodGet, "/api/v1/mods/"+site+"/editor", nil, 200)
			revisionCases = append(revisionCases, c)
			private["revision"].(map[string]string)[caseID] = token
		}
		// This is a real successful ordinary submission, not an inserted fingerprint,
		// synthetic restriction, forced risk state or threshold override.
		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "project.create", "project.create.mod")
		seed := auditModFormPayload("audit-reference-"+randomCatalogPublicID(), "Canonical primary provided by user", "en-US")
		seed.SecondaryName = "Human authored canonical revision source"
		seed.Summary = "Independent supplied overview of normal revision content"
		seed.BodyMarkdown = "Detailed ordinary revision source written separately from its supplied summary"
		seed.Localizations = []catalogLocalizationEdit{{Locale: "en-US", Name: seed.SecondaryName, Summary: seed.Summary, ContentMarkdown: seed.BodyMarkdown}}
		recorder.register(seed.SiteID, "setup-reference")
		auditModFormInitialSubmit(t, f, f.otherEditor, seed)
		raw, _ := json.Marshal(seed)
		inspect, _ := http.NewRequest(http.MethodPost, "/api/v1/mods", bytes.NewReader(raw))
		inspect.Header.Set("Content-Type", "application/json")
		_, content := inspectAntiAbuseBody(inspect)
		seedHash = antiabuse.ContentHash(antiabuse.NormalizeContent(content))
		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()
		pollFingerprint := time.NewTicker(20 * time.Millisecond)
		defer pollFingerprint.Stop()
		for {
			var exists bool
			if err = f.db.QueryRow(f.ctx, `select exists(select 1 from anti_abuse_content_fingerprints where user_id=$1 and action='review.submit' and exact_hash=$2)`, f.userIDs[f.otherEditor], seedHash).Scan(&exists); err != nil {
				t.Fatal(err)
			}
			if exists {
				break
			}
			select {
			case <-deadline.C:
				t.Fatal("actual successful source fingerprint absent")
			case <-pollFingerprint.C:
			}
		}
		for _, c := range revisionCases {
			expected := seed
			expected.SiteID = c.SiteID
			expected.ModIDs = c.InitialSnapshot.ModIDs
			c.FormValues = expected
			c.Expected = expected.Localizations
			probe := submitModRevisionRequest{Snapshot: expected, ChangeReason: c.ChangeReason, BaseRevisionID: &c.BaseRevisionID}
			encoded, _ := json.Marshal(probe)
			request, _ := http.NewRequest(http.MethodPost, "/", bytes.NewReader(encoded))
			request.Header.Set("Content-Type", "application/json")
			_, value := inspectAntiAbuseBody(request)
			if antiabuse.ContentHash(antiabuse.NormalizeContent(value)) != seedHash {
				t.Fatal("revision snapshot content differs from ordinary successful source")
			}
		}
		for _, c := range revisionCases {
			probeID, public, probeToken := createTEST044User(t, f.ctx, f.db, f.server.cfg, "mod-form-probe-"+c.Mode+"-"+randomCatalogPublicID(), "UTC")
			grantTEST044Permissions(t, f.ctx, f.db, probeID, c.EditPermission)
			probePath := "/api/v1/mods/" + c.SiteID + "/revisions"
			status, response := auditModFormSubmitExpect(t, f, probeToken, probePath, submitModRevisionRequest{Snapshot: c.FormValues, ChangeReason: c.ChangeReason, BaseRevisionID: &c.BaseRevisionID}, "owned-independent-revision-probe-"+c.Mode, 428)
			var preflight struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(response, &preflight)
			if status != 428 || preflight.Code != "challenge_required" {
				t.Fatalf("natural ordinary revision probe status=%d code=%s", status, preflight.Code)
			}
			var created int
			if err = f.db.QueryRow(f.ctx, `select count(*) from content_revisions where created_by=$1`, probeID).Scan(&created); err != nil || created != 0 {
				t.Fatal("preflight probe wrote a revision")
			}
			preflights = append(preflights, map[string]any{"caseId": c.CaseID, "probeActorPublicId": public, "status": status, "code": preflight.Code, "additionalRevisions": created, "scope": c.EditPermission})
		}
	}
	privateDir := filepath.Join(directory, ".private")
	if err = os.Mkdir(privateDir, 0700); err != nil {
		t.Fatal(err)
	}
	authPath := filepath.Join(privateDir, "auth.json")
	auditModFormJSON(t, authPath, private)
	var schema string
	if err = f.db.QueryRow(f.ctx, `select current_schema()`).Scan(&schema); err != nil || !regexp.MustCompile(`^mcmods_audit_[0-9a-f]{16}$`).MatchString(schema) {
		t.Fatal("owned regular schema required")
	}
	runtime := map[string]any{"owner": "mcmods-live-mod-form", "state": "READY", "pid": os.Getpid(), "ownedSchema": schema, "apiOrigin": f.origin.URL, "frontendOrigin": "http://127.0.0.1:3531", "authCookieName": authSessionCookieName, "authPath": authPath, "repoRevisions": revisions, "revisionCases": revisionCases, "localeCases": localeCases, "referenceContentSHA256": seedHash, "preflight": map[string]any{"status": 428, "code": "challenge_required", "actorIsSeparateScopedProbe": true, "actualSuccessfulOtherUserFingerprint": true, "cases": preflights}, "policy": map[string]any{"defaultThreshold": settings.ChallengeThreshold, "challengeTTLSeconds": 600, "formMinimumAgeMilliseconds": 800, "unchanged": true, "reviewer": "two predeclared ordinary independent content.review reviewers A18/B18; actualRetryAfter obeyed", "normalReviewHourLimit": settings.Policies["review.submit"].HourLimit, "reviewerPlan": []map[string]any{{"label": "A", "publicId": reviewerPublicIDs[f.reviewer], "baselineReviews": 3, "successRevisionReviews": 1, "localeReviews": 14, "totalPlanned": 18}, {"label": "B", "publicId": reviewerPublicIDs[f.denied], "baselineReviews": 0, "successRevisionReviews": 0, "localeReviews": 18, "totalPlanned": 18}}, "matrixActorRationale": "32 predeclared independent locale/variant cases, each submits one distinct Mod"}, "reviewProtocol": map[string]string{"request": "review-request.<caseId>.json {caseId,siteId,bodySHA256}", "response": "review-result.<caseId>.json", "done": "done.flag only after native completion; PG independently validated", "stop": "STOP to end without acceptance"}, "stopPath": filepath.Join(directory, "STOP"), "createdAt": time.Now().UTC(), "serveDeadline": time.Now().Add(55 * time.Minute).UTC(), "scope": "ACTUAL_HTTP_POSTGRESQL_NATIVE_DRIVER_SEPARATE; no game/queue/production claim"}

	if localizationsOnly {
		runtime["mode"] = "localizations-only"
		runtime["preflight"] = map[string]any{"status": "NOT_RUN_OUTSIDE_REM003_SCOPE", "reason": "REM002 fresh revision/challenge evidence is independently sealed in phase03", "cases": preflights}
		policy := runtime["policy"].(map[string]any)
		policy["localeActorCohort"] = "ordinary established30day emailverified, onlydeclaredcreatepermissions, noadmin/noReview/noRateMultiplier"
		policy["reviewer"] = "two predeclared normal independent content.review reviewers16/16; no onrefusal swap"
		policy["reviewerPlan"] = []map[string]any{{"label": "A", "publicId": reviewerPublicIDs[f.reviewer], "baselineReviews": 0, "successRevisionReviews": 0, "localeReviews": 16, "totalPlanned": 16}, {"label": "B", "publicId": reviewerPublicIDs[f.denied], "baselineReviews": 0, "successRevisionReviews": 0, "localeReviews": 16, "totalPlanned": 16}}
		policy["sharedIPNormalHourLimit"] = settings.Policies["review.submit"].HourLimit * 4
		policy["plannedCreateAndReviewRequests"] = 64
	} else {
		runtime["mode"] = "all"
	}
	auditModFormJSON(t, filepath.Join(directory, "runtime.json"), runtime)
	t.Logf("MOD_FORM_READY runtime=%s locales=%d revision=%d defaultpolicy=true natural_separate_probe428=%v", filepath.Join(directory, "runtime.json"), len(localeCases), len(revisionCases), !localizationsOnly)
	reviewed := map[string]bool{}
	all := map[string]*auditModFormCase{}
	for _, c := range localeCases {
		all[c.CaseID] = c
	}
	for _, c := range revisionCases {
		if c.Mode == "success" {
			all[c.CaseID] = c
		}
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	wait, cancel := context.WithTimeout(f.ctx, 55*time.Minute)
	defer cancel()
	for {
		select {
		case <-wait.Done():
			t.Fatal("native completion missing; no UI/locale/challenge acceptance inferred")
		case <-ticker.C:
			if _, err = os.Stat(filepath.Join(directory, "STOP")); err == nil {
				t.Fatal("stopped without complete native acceptance")
			}
			keys := make([]string, 0, len(all))
			for key := range all {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				if reviewed[key] {
					continue
				}
				path := filepath.Join(directory, "review-request."+key+".json")
				request, readErr := os.ReadFile(path)
				if os.IsNotExist(readErr) {
					continue
				}
				if readErr != nil {
					t.Fatal(readErr)
				}
				var receipt struct {
					CaseID           string `json:"caseId"`
					SiteID           string `json:"siteId"`
					BodySHA256       string `json:"bodySHA256"`
					ReviewerPublicID string `json:"reviewerPublicId"`
				}
				if json.Unmarshal(request, &receipt) != nil || receipt.CaseID != key || receipt.SiteID != all[key].SiteID || !auditChallengeHex(receipt.BodySHA256, 64) {
					t.Fatal("invalid owned review receipt")
				}
				if receipt.ReviewerPublicID != "" && receipt.ReviewerPublicID != all[key].ReviewerPublicID {
					t.Fatal("receipt changes predeclared independent reviewer")
				}
				recorder.mu.Lock()
				matched := false
				expectedPath := "/api/v1/mods"
				if all[key].Mode == "success" {
					expectedPath = "/api/v1/mods/" + all[key].SiteID + "/revisions"
				}
				for _, attempt := range recorder.attempts {
					if attempt.CaseID == key && attempt.Path == expectedPath && attempt.Status == 201 && attempt.BodySHA256 == receipt.BodySHA256 {
						matched = true
					}
				}
				recorder.mu.Unlock()
				if !matched {
					t.Fatal("review receipt not bound to actual accepted HTTP body")
				}
				c := all[key]
				revision := auditModFormPending(t, f, c)
				outcomes := auditModFormApprove(t, f, c.ReviewerToken, c.SiteID, revision)
				auditModFormCheckPublished(t, f, c)
				reviewed[key] = true
				auditModFormJSON(t, filepath.Join(directory, "review-result."+key+".json"), map[string]any{"status": "APPROVED_ACTUAL_INDEPENDENT_HTTP", "caseId": key, "siteId": c.SiteID, "revisionId": revision, "bodySHA256": receipt.BodySHA256, "reviewerPublicId": c.ReviewerPublicID, "outcomes": outcomes, "publicPath": c.PublicURL})
			}
			if _, err = os.Stat(filepath.Join(directory, "done.flag")); os.IsNotExist(err) {
				continue
			} else if err != nil {
				t.Fatal(err)
			}
			if len(reviewed) != len(all) {
				t.Fatalf("native completion before actual%d independent reviews: got%d", len(all), len(reviewed))
			}
			auditModFormTerminal(t, f, directory, localeCases, revisionCases, recorder, seedHash, settingsBefore, revisions)
			return
		}
	}
}

func auditModFormCheckPublished(t *testing.T, f auditFixGRFixture, c *auditModFormCase) {
	t.Helper()
	status, raw, _ := auditModFormDo(t, f, "", http.MethodGet, "/api/v1/mods/"+c.SiteID+"?locale="+url.QueryEscape(c.DefaultLocale), nil, nil)
	if status != 200 {
		t.Fatalf("independently approved public Mod status=%d", status)
	}
	result := decodeTEST022Data[modResponse](t, raw)
	if result.ReviewStatus != "approved" || result.PrimaryName != c.FormValues.PrimaryName || result.DefaultLocale != c.DefaultLocale || len(result.Localizations) != len(c.Expected) {
		t.Fatalf("public Mod fields/localization count differs for%s", c.CaseID)
	}
	for _, expected := range c.Expected {
		found := false
		for _, actual := range result.Localizations {
			if actual.Locale == expected.Locale {
				found = true
				if actual.Name != expected.Name || actual.Summary != expected.Summary || actual.ContentMarkdown != expected.ContentMarkdown {
					t.Fatalf("entered locale altered/invented for%s/%s", c.CaseID, expected.Locale)
				}
			}
		}
		if !found {
			t.Fatal("entered locale missing")
		}
	}
}

func auditModFormTerminal(t *testing.T, f auditFixGRFixture, directory string, locales, revisions []*auditModFormCase, recorder *auditModFormRecorder, reference string, settingsBefore []byte, pins map[string]any) {
	t.Helper()
	settings, err := f.server.antiAbuse.Settings(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(settings)
	if !bytes.Equal(settingsBefore, after) {
		t.Fatal("production anti-abuse policy changed during fixture")
	}
	recorder.mu.Lock()
	attempts := append([]auditModFormAttempt(nil), recorder.attempts...)
	writeErr := recorder.writeErr
	recorder.mu.Unlock()
	if writeErr != nil {
		t.Fatal("HTTP evidence write failed: ", writeErr)
	}
	checks := []map[string]any{}
	for _, c := range locales {
		var total, published, decisions int
		if err = f.db.QueryRow(f.ctx, `select count(*),count(*) filter(where mod.review_status='approved'),(select count(*) from change_requests request join content_revisions revision on revision.id=request.proposed_revision_id where request.submitted_by=$1 and request.status='approved' and revision.entity_type='mod' and exists(select 1 from review_events event where event.change_request_id=request.id and event.actor_id=$2 and event.event_type='approved')) from mods mod where mod.submitted_by=$1`, c.ActorID, c.ReviewerID).Scan(&total, &published, &decisions); err != nil || total != 1 || published != 1 || decisions != 1 {
			t.Fatalf("ordinary independent published counts%s=%d/%d/%d err%v", c.CaseID, total, published, decisions, err)
		}
		count, accepted := 0, 0
		var previous *auditModFormAttempt
		for _, attempt := range attempts {
			if attempt.CaseID == c.CaseID {
				if previous != nil && previous.Status == 429 {
					at, parseErr := time.Parse(time.RFC3339Nano, attempt.At)
					before, previousErr := time.Parse(time.RFC3339Nano, previous.At)
					if parseErr != nil || previousErr != nil || at.Before(before.Add(time.Duration(previous.RetryAfter)*time.Second)) || previous.BodySHA256 != attempt.BodySHA256 {
						t.Fatal("valid retry did not preserve body or honor actual Retry-After")
					}
				}
				count++
				if attempt.Status == 201 {
					accepted++
				} else if attempt.Status != 429 || attempt.Code != "rate_limited" || attempt.RetryAfter <= 0 {
					t.Fatalf("unexpected pre-valid/business POST for%s status%d", c.CaseID, attempt.Status)
				}
				copy := attempt
				previous = &copy
			}
		}
		if accepted != 1 {
			t.Fatalf("one accepted UI create required%s=%d", c.CaseID, accepted)
		}
		auditModFormCheckPublished(t, f, c)
		checks = append(checks, map[string]any{"caseId": c.CaseID, "actorPublicId": c.ActorPublicID, "reviewerPublicId": c.ReviewerPublicID, "siteId": c.SiteID, "published": 1, "independentDecisions": 1, "businessPOSTs": count, "defaultLocale": c.DefaultLocale, "expectedLocalizations": c.Expected})
	}
	for _, c := range revisions {
		var projects, revisionCount int
		if err = f.db.QueryRow(f.ctx, `select count(*),(select count(*) from content_revisions where entity_type='mod' and entity_id=$2 and created_by=$1) from mods where submitted_by=$1`, c.ActorID, c.ModID).Scan(&projects, &revisionCount); err != nil || projects != 1 || revisionCount != 1+c.AdditionalRevisions {
			t.Fatalf("revision%s countprojects%d revisions%d err%v", c.CaseID, projects, revisionCount, err)
		}
		var independentReviews int
		if err = f.db.QueryRow(f.ctx, `select count(*) from review_events event join change_requests request on request.id=event.change_request_id join content_revisions revision on revision.id=request.proposed_revision_id where revision.entity_type='mod' and revision.entity_id=$1 and request.submitted_by=$2 and event.actor_id=$3 and event.event_type='approved'`, c.ModID, c.ActorID, c.ReviewerID).Scan(&independentReviews); err != nil || independentReviews != 1+c.AdditionalRevisions {
			t.Fatal("revision was not independently approved by its predeclared reviewer")
		}
		if c.Mode != "success" {
			initial := *c
			initial.FormValues, initial.Expected = *c.InitialSnapshot, c.InitialSnapshot.Localizations
			auditModFormCheckPublished(t, f, &initial)
			var published string
			if err = f.db.QueryRow(f.ctx, `select revision.public_id from mods mod join content_revisions revision on revision.id=mod.published_revision_id where mod.id=$1`, c.ModID).Scan(&published); err != nil || published != c.BaseRevisionID {
				t.Fatal("wrong/cancel changed published revision pointer")
			}
		}
		selected := []auditModFormAttempt{}
		for _, attempt := range attempts {
			if attempt.CaseID == c.CaseID && strings.HasSuffix(attempt.Path, "/revisions") {
				selected = append(selected, attempt)
			}
		}
		if len(selected) == 0 || selected[0].Status != 428 || selected[0].Code != "challenge_required" || selected[0].HasProof || selected[0].ContentSHA256 != reference {
			t.Fatal("native first revision did not reach exact real challenge")
		}
		if c.Mode == "success" {
			if len(selected) != 2 || selected[1].Status != 201 || !selected[1].HasProof || selected[0].BodySHA256 != selected[1].BodySHA256 || selected[0].IdempotencySHA == "" || selected[0].IdempotencySHA != selected[1].IdempotencySHA || selected[0].ClientIDSHA == "" || selected[0].ClientIDSHA != selected[1].ClientIDSHA {
				t.Fatal("native proof changed body/idempotency/client or repeated business write")
			}
		}
		if c.Mode == "cancel" && len(selected) != 1 {
			t.Fatal("cancel sent another business POST")
		}
		if c.Mode == "wrong" {
			if len(selected) != 2 || !selected[1].HasProof || selected[1].Status == 201 || selected[0].BodySHA256 != selected[1].BodySHA256 || selected[0].IdempotencySHA == "" || selected[0].IdempotencySHA != selected[1].IdempotencySHA || selected[0].ClientIDSHA == "" || selected[0].ClientIDSHA != selected[1].ClientIDSHA {
				t.Fatal("wrong proof did not preserve body/zero-write semantics")
			}
			if !((selected[1].Status == 428 && selected[1].Code == "challenge_required") || (selected[1].Status == 429 && selected[1].Code == "rate_limited" && selected[1].RetryAfter > 0)) {
				t.Fatal("wrong proof refusal is not the precise challenge/rate contract")
			}
		}
		var events []byte
		if err = f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(jsonb_build_object('requestId',event.request_id,'outcome',event.outcome,'rules',event.rule_codes,'contentMatchesReference',event.content_hash=$3,'contextMatchesChallenge',exists(select 1 from anti_abuse_challenges challenge where challenge.user_id=event.user_id and challenge.action=event.action and challenge.object_key=event.object_key and challenge.session_hash=event.session_hash and (challenge.ip_hash='' or challenge.ip_hash=event.ip_hash))) order by event.created_at),'[]'::jsonb) from anti_abuse_events event where event.user_id=$1 and event.object_key=$2`, c.ActorID, "/api/v1/mods/"+c.SiteID+"/revisions", reference).Scan(&events); err != nil {
			t.Fatal(err)
		}
		var observedEvents []struct {
			RequestID      string   `json:"requestId"`
			Rules          []string `json:"rules"`
			ContentMatches bool     `json:"contentMatchesReference"`
			ContextMatches bool     `json:"contextMatchesChallenge"`
		}
		if json.Unmarshal(events, &observedEvents) != nil {
			t.Fatal("safe event projection invalid")
		}
		requiredRule := "exact_duplicate_network_or_object"
		if c.Mode == "success" {
			requiredRule = "challenge_passed"
		} else if c.Mode == "wrong" {
			requiredRule = "challenge_failed"
		}
		bound := false
		for _, event := range observedEvents {
			for _, rule := range event.Rules {
				if rule == requiredRule && event.ContentMatches && event.ContextMatches {
					for _, attempt := range selected {
						if event.RequestID != "" && event.RequestID == attempt.RequestID {
							bound = true
						}
					}
				}
			}
		}
		if !bound {
			t.Fatalf("specific actual %s event is not bound to this native request; arbitrary refusal is insufficient", requiredRule)
		}
		checks = append(checks, map[string]any{"caseId": c.CaseID, "actorPublicId": c.ActorPublicID, "reviewerPublicId": c.ReviewerPublicID, "independentReviews": independentReviews, "projectCount": projects, "baselinePlusAdditionalRevisions": revisionCount, "nativeAttempts": selected, "safeAntiAbuseEvents": json.RawMessage(events)})
	}
	auditModFormJSON(t, filepath.Join(directory, "sql-terminal-seal.json"), map[string]any{"status": "PASS_ACTUAL_HTTP_PG_NORMAL_REVIEW_AND_NATIVE_REQUEST_POSTCONDITIONS", "recordedAt": time.Now().UTC(), "repoRevisions": pins, "checks": checks, "localizationCases": len(locales), "revisionCases": len(revisions), "policyUnchanged": true, "productionExecuted": false, "scope": "Native driver is separate evidence. No game/queue/release claim. No real balances or production data changed."})
}
