package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/antiabuse"
	"mcmods-cn-backend/internal/database"
)

// This is a focused production-boundary regression, not a replacement for the
// original native SYS03-T03 journey. It uses real HTTP authentication, the
// production AA service, PostgreSQL, Redis, ACL refresh and member audit writes.
func TestSYS03MemberRoleRestoreRetainsAntiAbuseGatesHTTPPostgresRedisIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_SYS03_MEMBER_AA_HTTP_REGRESSION") != "1" || os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("explicit private owned-resource regression allocation is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	cfg, private := sys03MemberAALoadConfig(t, ctx)
	writersClosed := true
	db := sys03MemberAAOwnedDatabase(t, ctx, cfg, private, &writersClosed)
	sys03MemberAARedisScope(t, ctx, cfg.Redis, private, &writersClosed)
	if err := database.SeedRBAC(ctx, db); err != nil {
		sys03MemberAAFail(t, "production_rbac_seed", err)
	}

	type actor struct {
		id    int64
		token string
	}
	actors := make(map[string]actor)
	for _, spec := range []struct {
		name        string
		permissions []string
	}{
		{"ordinaryA", []string{"creator.claim"}},
		{"reviewerB", []string{"admin.access", "creator.claim.review"}},
		{"managerM", []string{"team.members.manage"}},
		{"riskManagerR", []string{"team.members.manage"}},
		{"contentWriterC", []string{"creator.create"}},
		{"invalidManagerV", []string{"team.members.manage"}},
	} {
		id, _, token := createTEST044User(t, ctx, db, cfg, private.Nonce[:12]+"-"+spec.name, "UTC")
		grantTEST044Permissions(t, ctx, db, id, spec.permissions...)
		if _, err := db.Exec(ctx, `update users set created_at=now()-interval '30 days',email_verified=true where id=$1`, id); err != nil {
			t.Fatal("established actor seed unavailable")
		}
		var actual []string
		var roles int
		if err := db.QueryRow(ctx, `select array(select p.code from user_permissions u join permissions p on p.id=u.permission_id where u.user_id=$1 and u.allow order by p.code),
			(select count(*) from user_role_bindings where user_id=$1)`, id).Scan(&actual, &roles); err != nil {
			t.Fatal("actual actor permission baseline unavailable")
		}
		want := append([]string(nil), spec.permissions...)
		sort.Strings(want)
		if !reflect.DeepEqual(actual, want) || roles != 0 {
			t.Fatal("actor must have exact minimal permissions and no default/admin role")
		}
		actors[spec.name] = actor{id: id, token: token}
	}
	ordinary, reviewer, manager, riskManager, writer := actors["ordinaryA"], actors["reviewerB"], actors["managerM"], actors["riskManagerR"], actors["contentWriterC"]
	// A genuine persisted risk state must still issue the real proof challenge.
	// No threshold, rate multiplier, provider, form age or title is changed.
	if _, err := db.Exec(ctx, `insert into anti_abuse_user_states(user_id,trust_level,challenge_required_until)
		values($1,'high_risk',now()+interval '10 minutes')`, riskManager.id); err != nil {
		t.Fatal("synthetic high-risk account precondition unavailable")
	}
	var authorID, teamID, roleID, modID int64
	var authorPublic, teamPublic, rolePublic, modCode, modSite string
	for _, creator := range []struct {
		kind   string
		id     *int64
		public *string
	}{{"author", &authorID, &authorPublic}, {"team", &teamID, &teamPublic}} {
		if err := db.QueryRow(ctx, `insert into creators(kind,name,normalized_name,review_status,created_by)
			values($1,$2,lower($2),'approved',$3) returning id,public_id`, creator.kind, "Member AA "+creator.kind+" "+private.Nonce[:12], reviewer.id).Scan(creator.id, creator.public); err != nil {
			sys03MemberAAFail(t, "approved_creator_seed", err)
		}
	}
	if err := db.QueryRow(ctx, `select id,public_id from creator_role_definitions where code='developer' and permission_granting`).Scan(&roleID, &rolePublic); err != nil {
		t.Fatal("production permission-granting developer role unavailable")
	}
	modCode = randomCatalogPublicID()
	modSite = "member-aa-" + private.Nonce[:12]
	if err := db.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values($1,$2,'Member AA bound project','approved',$3) returning id,project_code,slug`, modCode, modSite, reviewer.id).Scan(&modID, &modCode, &modSite); err != nil {
		sys03MemberAAFail(t, "approved_project_seed", err)
	}
	if modSite == modCode {
		t.Fatal("editor route must be the actual slug, separate from project code")
	}
	if _, err := db.Exec(ctx, `insert into mod_loader_compatibilities(mod_id,loader,minecraft_version) values($1,'neoforge','1.21.1')`, modID); err != nil {
		t.Fatal("owned editor baseline unavailable")
	}
	if _, err := db.Exec(ctx, `insert into content_creator_bindings(subject_type,subject_id,creator_id,role_id,name_snapshot,role_snapshot,status,permission_granting,approved_by,approved_at)
		values('mod',$1,$2,$3,'Member AA team','Developer','approved',true,$4,now())`, modID, teamID, roleID, reviewer.id); err != nil {
		sys03MemberAAFail(t, "approved_team_project_binding_seed", err)
	}

	writersClosed = false
	server := NewServer(ctx, cfg, db, nil, nil, nil, nil)
	origin := httptest.NewUnstartedServer(server)
	origin.Config.BaseContext = func(net.Listener) context.Context { return ctx }
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if client := origin.Client(); client != nil {
			client.CloseIdleConnections()
		}
		if origin.Config.Shutdown(cleanupCtx) != nil {
			t.Error("owned HTTP request drain failed; retain dependent scope")
			return
		}
		origin.Close()
		if server.Shutdown(cleanupCtx) != nil {
			t.Error("owned AA/access-log drain failed; retain dependent scope")
			return
		}
		if server.cache.Close() != nil {
			t.Error("owned Redis cache close failed; retain dependent scope")
			return
		}
		writersClosed = true
	})
	origin.Start()
	if !server.cache.Enabled() || server.cache.Ping(ctx) != nil {
		t.Fatal("real enabled Redis cache is required")
	}
	settings, err := server.antiAbuse.Settings(ctx)
	if err != nil || !reflect.DeepEqual(settings, antiabuse.DefaultSettings()) {
		t.Fatal("unchanged production default AA settings are required")
	}
	settingsBefore, err := json.Marshal(settings)
	if err != nil {
		t.Fatal("settings snapshot failed")
	}
	request := func(who actor, method, path string, body []byte, want int, wantCode string) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, origin.URL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal("owned HTTP request construction failed")
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+who.token)
		// Retain the actual Go client UA and normal anti-abuse headers. No
		// idempotency nonce, bypass flag, fake browser or renamed role title.
		response, err := origin.Client().Do(req)
		if err != nil {
			t.Fatal("owned actual HTTP request failed; credentials and transport values withheld")
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || len(raw) > 1<<20 {
			t.Fatal("bounded owned HTTP response collection failed")
		}
		var envelope struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(raw, &envelope) != nil {
			t.Fatal("owned API response was not a JSON envelope")
		}
		safeCode := ""
		for _, allowed := range []string{"duplicate_content", "challenge_required", "rate_limited", "CREATOR_CLAIM_INDEPENDENT_REVIEW_REQUIRED", "HTTP_400", "HTTP_403"} {
			if envelope.Code == allowed {
				safeCode = allowed
			}
		}
		if response.StatusCode != want || envelope.Code != wantCode {
			digest := sha256.Sum256(raw)
			t.Fatalf("owned HTTP status=%d want=%d stableCode=%s responseSHA256=%s (raw body/headers withheld)", response.StatusCode, want, safeCode, hex.EncodeToString(digest[:]))
		}
		if want == http.StatusTooManyRequests {
			retry, err := strconv.Atoi(response.Header.Get("Retry-After"))
			if err != nil || retry <= 0 {
				t.Fatal("real frequency denial must retain positive Retry-After")
			}
		}
		return raw
	}
	decodeData := func(raw []byte, target any) {
		t.Helper()
		var envelope struct {
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(raw, &envelope) != nil || json.Unmarshal(envelope.Data, target) != nil {
			t.Fatal("typed owned response data unavailable")
		}
	}
	membership, err := json.Marshal(map[string]any{"members": []creatorMemberPayload{{CreatorID: authorPublic, RoleID: rolePublic, Title: "Developer"}}})
	if err != nil {
		t.Fatal("fixed relationship payload encoding failed")
	}
	remove := []byte(`{"members":[]}`)
	path := "/api/v1/creators/" + teamPublic + "/members"
	facts := func() sys03MemberAAFacts {
		return sys03MemberAAReadFacts(t, ctx, db, ordinary.id, teamID, authorID, roleID, modID)
	}
	baseline := facts()
	riskBody := request(riskManager, http.MethodPut, path, membership, 428, "challenge_required")
	var challenge struct {
		Details struct {
			Challenge *antiabuse.ChallengeInfo `json:"challenge"`
		} `json:"details"`
	}
	if json.Unmarshal(riskBody, &challenge) != nil || challenge.Details.Challenge == nil || challenge.Details.Challenge.Provider != "proof" || challenge.Details.Challenge.Prompt == "" {
		t.Fatal("real production proof challenge must be issued")
	}
	var challenges int
	if err := db.QueryRow(ctx, `select count(*) from anti_abuse_challenges where public_id=$1 and user_id=$2 and action='review.submit' and object_key=$3 and provider='proof' and status='pending' and expires_at>now()`, challenge.Details.Challenge.ID, riskManager.id, path).Scan(&challenges); err != nil || challenges != 1 {
		t.Fatal("persisted real pending challenge missing")
	}
	if facts() != baseline {
		t.Fatal("challenge-required member request changed business relations")
	}

	claimRaw := request(ordinary, http.MethodPost, "/api/v1/creators/"+authorPublic+"/claims", []byte(`{"proofMarkdown":"Ordinary synthetic author ownership evidence","proofFileIds":[]}`), 201, "")
	var claim struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	decodeData(claimRaw, &claim)
	if !validCatalogPublicID(claim.ID) || claim.Status != "pending" {
		t.Fatal("ordinary author claim must begin pending")
	}
	request(reviewer, http.MethodPatch, "/api/v1/admin/creator-claims/"+claim.ID, []byte(`{"status":"approved","note":"Independent synthetic ownership review"}`), 200, "")
	beforeDenied := facts()
	if beforeDenied.ClaimStatus != "approved" || beforeDenied.Derived != 0 || beforeDenied.Members != 0 {
		t.Fatal("approved claim alone must not grant team access")
	}
	request(ordinary, http.MethodPut, path, membership, 403, "HTTP_403")
	if facts() != beforeDenied {
		t.Fatal("ordinary member-management denial changed scoped business facts")
	}
	request(ordinary, http.MethodGet, "/api/v1/mods/"+modSite+"/editor", nil, 403, "HTTP_403")
	// Cookie-origin protection runs before the same membership boundary and
	// must still reject an unrelated origin without consuming a valid write.
	csrfRequest, err := http.NewRequestWithContext(ctx, http.MethodPut, origin.URL+path, bytes.NewReader(membership))
	if err != nil {
		t.Fatal("cookie-origin regression request construction failed")
	}
	csrfRequest.Header.Set("Content-Type", "application/json")
	csrfRequest.Header.Set("Origin", "https://unrelated-origin.example.test")
	csrfRequest.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: manager.token})
	csrfResponse, err := origin.Client().Do(csrfRequest)
	if err != nil {
		t.Fatal("cookie-origin regression HTTP request failed")
	}
	_, csrfReadErr := io.Copy(io.Discard, io.LimitReader(csrfResponse.Body, 1<<20))
	csrfCloseErr := csrfResponse.Body.Close()
	if csrfReadErr != nil || csrfCloseErr != nil || csrfResponse.StatusCode != 403 || facts() != beforeDenied {
		t.Fatal("cookie-origin gate failed its forbidden/zero-business-write contract")
	}

	for index, step := range []struct {
		body    []byte
		status  string
		derived int
		audits  int
	}{
		{membership, "approved", 1, 1}, {remove, "revoked", 0, 2}, {membership, "approved", 1, 3},
	} {
		request(manager, http.MethodPut, path, step.body, 200, "")
		state := facts()
		if state.Status != step.status || state.Title != "Developer" || state.Members != 1 || state.Audits != step.audits || state.Derived != step.derived || state.OtherAccess != 0 ||
			state.ClaimStatus != "approved" || state.ClaimReviewer != reviewer.id || state.AuthVersion != beforeDenied.AuthVersion || state.SessionHash != beforeDenied.SessionHash || state.Sessions != 1 || state.Assignments != 0 {
			t.Fatalf("member add/remove/restore phase=%d failed real relation/ACL/audit/same-session assertions", index+1)
		}
		wantEditor := http.StatusForbidden
		if step.derived == 1 {
			wantEditor = http.StatusOK
		}
		wantEditorCode := "HTTP_403"
		if wantEditor == http.StatusOK {
			wantEditorCode = ""
		}
		request(ordinary, http.MethodGet, "/api/v1/mods/"+modSite+"/editor", nil, wantEditor, wantEditorCode)
	}
	// Same exact member, role, title and original JWT. The next request must
	// hit the unchanged Redis rate limiter, rather than duplicate-content.
	beforeRate := facts()
	request(manager, http.MethodPut, path, membership, 429, "rate_limited")
	if facts() != beforeRate {
		t.Fatal("frequency refusal changed restored membership/ACL/audit state")
	}
	invalidManager := actors["invalidManagerV"]
	for _, invalid := range []string{
		`{"members":[{"creatorId":"` + authorPublic + `","roleId":"` + rolePublic + `","title":"Developer"}],"skipFingerprint":true}`,
		`{"members":[{"creatorId":"` + authorPublic + `","roleId":"` + rolePublic + `","title":"` + strings.Repeat("界", 54) + `"}]}`,
		`{"members":[{"creatorId":"` + authorPublic + `","roleId":"` + rolePublic + `","title":42}]}`,
	} {
		request(invalidManager, http.MethodPut, path, []byte(invalid), 400, "HTTP_400")
		if facts() != beforeRate {
			t.Fatal("unknown/client-flag/type/length payload changed restored relationship facts")
		}
	}

	content := []byte(`{"kind":"author","name":"Normal narrative creator","descriptionMarkdown":"Independent human-authored narrative is still protected from duplicate publication.","defaultLocale":"en-US"}`)
	request(writer, http.MethodPost, "/api/v1/creators", content, 201, "")
	request(writer, http.MethodPost, "/api/v1/creators", content, 403, "duplicate_content")
	// Drain normal success/security telemetry before checking both persistence
	// projections. The title is retained in genuine challenge event auditing.
	if server.antiAbuse.Close(ctx) != nil {
		t.Fatal("normal AA success/security drain did not complete")
	}
	var metadataFingerprints, narrativeFingerprints, writerRows, riskTitleEvents int
	if err := db.QueryRow(ctx, `select
		(select count(*) from anti_abuse_content_fingerprints where user_id=any($1::bigint[])),
		(select count(*) from anti_abuse_content_fingerprints where user_id=$2 and action='review.submit'),
		(select count(*) from creators where created_by=$2),
		(select count(*) from anti_abuse_events where user_id=$3 and outcome='challenge' and content_hash=$4)`,
		[]int64{manager.id, riskManager.id}, writer.id, riskManager.id, antiabuse.ContentHash(antiabuse.NormalizeContent("Developer"))).Scan(&metadataFingerprints, &narrativeFingerprints, &writerRows, &riskTitleEvents); err != nil {
		t.Fatal("real fingerprint/audit persistence projection unavailable")
	}
	if metadataFingerprints != 0 || narrativeFingerprints != 1 || writerRows != 1 || riskTitleEvents != 1 {
		t.Fatal("only relationship duplicate fingerprints may be omitted; real narrative fingerprint and original title audit must remain")
	}
	var degraded int
	if err := db.QueryRow(ctx, `select count(*) from app_logs where action in ('anti_abuse_degraded','anti_abuse_state_persist_failed')`).Scan(&degraded); err != nil {
		sys03MemberAAFail(t, "degraded_audit_check", err)
	}
	if degraded != 0 {
		t.Fatal("a fail-soft anti-abuse path cannot count as a protected membership regression pass")
	}
	metrics := server.cache.Metrics()
	async := server.antiAbuse.AsyncMetrics()
	// Preserve the exact snapshots used by the unchanged composite assertion.
	// Only counters/booleans are recorded; do not infer an uncaptured earlier
	// attempt's values or discard a failed condition to obtain a passing test.
	sys03MemberAAPrivateReceipt(t, private, "nondegraded-metrics", map[string]any{
		"schemaVersion": 1,
		"counterValues": map[string]any{
			"cache.RedisHits":         metrics.RedisHits,
			"cache.Errors":            metrics.Errors,
			"cache.LocalFallbacks":    metrics.LocalFallbacks,
			"cache.RateLimitFalls":    metrics.RateLimitFalls,
			"async.SuccessFailed":     async.SuccessFailed,
			"async.SuccessDropped":    async.SuccessDropped,
			"async.SuccessProcessed":  async.SuccessProcessed,
			"async.SuccessQueued":     async.SuccessQueued,
			"async.SuccessQueueDepth": async.SuccessQueueDepth,
			"async.RiskDropped":       async.RiskDropped,
			"async.RiskEventFailed":   async.RiskEventFailed,
			"async.RiskDailyFailed":   async.RiskDailyFailed,
			"async.RestrictionFailed": async.RestrictionFailed,
			"async.BotRuleLoadFailed": async.BotRuleLoadFailed,
			"async.RiskQueueDepth":    async.RiskQueueDepth,
		},
		"failedConditions": map[string]bool{
			"cache.RedisHits == 0":                          metrics.RedisHits == 0,
			"cache.Errors != 0":                             metrics.Errors != 0,
			"cache.LocalFallbacks != 0":                     metrics.LocalFallbacks != 0,
			"cache.RateLimitFalls != 0":                     metrics.RateLimitFalls != 0,
			"async.SuccessFailed != 0":                      async.SuccessFailed != 0,
			"async.SuccessDropped != 0":                     async.SuccessDropped != 0,
			"async.SuccessProcessed != async.SuccessQueued": async.SuccessProcessed != async.SuccessQueued,
			"async.SuccessQueueDepth != 0":                  async.SuccessQueueDepth != 0,
			"async.RiskDropped != 0":                        async.RiskDropped != 0,
			"async.RiskEventFailed != 0":                    async.RiskEventFailed != 0,
			"async.RiskDailyFailed != 0":                    async.RiskDailyFailed != 0,
			"async.RestrictionFailed != 0":                  async.RestrictionFailed != 0,
			"async.BotRuleLoadFailed != 0":                  async.BotRuleLoadFailed != 0,
			"async.RiskQueueDepth != 0":                     async.RiskQueueDepth != 0,
		},
	})
	if metrics.RedisHits == 0 || metrics.Errors != 0 || metrics.LocalFallbacks != 0 || metrics.RateLimitFalls != 0 ||
		async.SuccessFailed != 0 || async.SuccessDropped != 0 || async.SuccessProcessed != async.SuccessQueued || async.SuccessQueueDepth != 0 ||
		async.RiskDropped != 0 || async.RiskEventFailed != 0 || async.RiskDailyFailed != 0 || async.RestrictionFailed != 0 || async.BotRuleLoadFailed != 0 || async.RiskQueueDepth != 0 {
		t.Fatal("actual shared Redis and completed nondegraded AA persistence are required")
	}
	afterSettings, err := server.antiAbuse.Settings(ctx)
	if err != nil {
		t.Fatal("final AA settings unavailable")
	}
	settingsAfter, err := json.Marshal(afterSettings)
	if err != nil || !bytes.Equal(settingsBefore, settingsAfter) {
		t.Fatal("AA settings/thresholds/quotas changed")
	}
	t.Log("SYS03_MEMBER_AA_REGRESSION exact_Developer_add_remove_restore=200/200/200 ordinary_forbidden=403 content_hard_duplicate=403 proof_challenge=428 redis_frequency=429 real_acl_audit_same_session=true")
}

type sys03MemberAAFacts struct {
	Status, Title, ClaimStatus, SessionHash                      string
	Members, Audits, Derived, OtherAccess, Sessions, Assignments int
	ClaimReviewer, AuthVersion                                   int64
}

func sys03MemberAAReadFacts(t *testing.T, ctx context.Context, db *pgxpool.Pool, user, team, author, role, mod int64) sys03MemberAAFacts {
	t.Helper()
	var out sys03MemberAAFacts
	err := db.QueryRow(ctx, `select
		coalesce((select status from creator_team_members where team_id=$2 and member_creator_id=$3 and role_id=$4),''),
		coalesce((select title from creator_team_members where team_id=$2 and member_creator_id=$3 and role_id=$4),''),
		coalesce((select status from creator_claims where creator_id=$3 and user_id=$1),''),
		coalesce((select encode(session_hash,'hex') from auth_sessions where user_id=$1),''),
		(select count(*) from creator_team_members where team_id=$2),
		(select count(*) from permission_audit_logs where action='creator.team_members.replace'),
		(select count(*) from effective_project_access where user_id=$1 and project_type='mod' and project_id=$5 and source_type='author_team_relation' and source_id=$2),
		(select count(*) from effective_project_access where user_id=$1 and not(project_type='mod' and project_id=$5 and source_type='author_team_relation' and source_id=$2)),
		(select count(*) from auth_sessions where user_id=$1),
		(select count(*) from project_editor_assignments),
		coalesce((select reviewed_by from creator_claims where creator_id=$3 and user_id=$1),0),
		(select auth_version from users where id=$1)`, user, team, author, role, mod).Scan(&out.Status, &out.Title, &out.ClaimStatus, &out.SessionHash, &out.Members, &out.Audits, &out.Derived, &out.OtherAccess, &out.Sessions, &out.Assignments, &out.ClaimReviewer, &out.AuthVersion)
	if err != nil {
		sys03MemberAAFail(t, "scoped_member_claim_acl_audit_session_facts", err)
	}
	return out
}
