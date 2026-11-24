package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type auditExpiryBrowserReceipt struct {
	CaseID            string    `json:"caseId"`
	ActorPublicID     string    `json:"actorPublicId"`
	ChallengeID       string    `json:"challengeId"`
	RequestID         string    `json:"requestId"`
	ExpiredRequestAt  time.Time `json:"expiredRequestAt"`
	FirstBodySHA256   string    `json:"firstBodySHA256"`
	ExpiredBodySHA256 string    `json:"expiredBodySHA256"`
	AnswerSHA256      string    `json:"answerSHA256"`
	BackendCommit     string    `json:"backendCommit"`
	FrontendCommit    string    `json:"frontendCommit"`
}

func TestAuditChallengeEvidenceCodecUsesRealSchemaAndDoesNotExposeSecrets(t *testing.T) {
	base := newTEST013Fixture(t)
	f := auditFixGRFixture{test013Fixture: base, server: base.origin.Config.Handler.(*Server)}
	actor := base.userIDs[base.editor]
	dir := t.TempDir()
	const nonce = "fixture-private-challenge-nonce"
	mac := hmac.New(sha256.New, []byte(f.server.cfg.AntiAbuse.HMACSecret))
	_, _ = mac.Write([]byte(nonce + "\x00" + "10"))
	answerHash := hex.EncodeToString(mac.Sum(nil))
	metadata, _ := json.Marshal(map[string]string{"nonce": nonce})
	var challenge, publicID string
	if err := base.db.QueryRow(base.ctx, `select public_id from users where id=$1`, actor).Scan(&publicID); err != nil {
		t.Fatal(err)
	}
	if err := base.db.QueryRow(base.ctx, `insert into anti_abuse_challenges(user_id,session_hash,action,object_key,kind,provider,token_hash,answer_hash,ip_hash,issued_at,expires_at,metadata) values($1,'helper-session-hash','review.submit','/api/v1/mods','human','proof','helper-token-hash',$2,'helper-ip-hash',now()-interval '11 minutes',now()-interval '1 minute',$3::jsonb) returning public_id`, actor, answerHash, metadata).Scan(&challenge); err != nil {
		t.Fatal(err)
	}
	const requestID = "synthetic-evidence-codec-not-a-browser-run"
	seedDigest := sha256.Sum256([]byte("codec body"))
	seedHash := hex.EncodeToString(seedDigest[:])
	if _, err := base.db.Exec(base.ctx, `insert into anti_abuse_events(user_id,session_hash,ip_hash,action,object_key,outcome,risk_score,rule_codes,content_hash,request_id) values($1,'helper-session-hash','helper-ip-hash','review.submit','/api/v1/mods','delay',65,array['challenge_failed','rate_object_user'],$2,$3)`, actor, seedHash, requestID); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("10"))
	body := sha256.Sum256([]byte("stable synthetic raw body"))
	r := auditExpiryBrowserReceipt{CaseID: "codec-only", ActorPublicID: publicID, ChallengeID: challenge, RequestID: requestID, ExpiredRequestAt: time.Now(), FirstBodySHA256: hex.EncodeToString(body[:]), ExpiredBodySHA256: hex.EncodeToString(body[:]), AnswerSHA256: hex.EncodeToString(digest[:]), BackendCommit: strings.Repeat("b", 40), FrontendCommit: strings.Repeat("a", 40)}
	raw, _ := json.Marshal(r)
	if err := os.WriteFile(filepath.Join(dir, "done.flag"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	seal, err := sealAuditExpiredBrowserRequest(base.ctx, f, map[string]int64{"expiry": actor}, map[string]string{"expiry": publicID}, dir, "codec-only", r.BackendCommit, r.FrontendCommit, seedHash)
	if err != nil || seal["contextBound"] != true || seal["correctSolutionBound"] != true {
		t.Fatalf("evidence binding codec failed: %v", err)
	}
	// This actor already owns the seeded fixture Mod. A matched challenge event
	// cannot, on its own, certify the browser's separate zero-submission claim.
	var modCount int
	if err := base.db.QueryRow(base.ctx, `select count(*) from mods where submitted_by=$1`, actor).Scan(&modCount); err != nil || modCount == 0 {
		t.Fatalf("fixture needs an existing submitted Mod, got %d: %v", modCount, err)
	}
	if _, claimed := seal["result"]; claimed {
		t.Fatal("an event binding emitted a verdict before checking the submission postconditions")
	}

	snapshot := filepath.Join(dir, "safe.json")
	if err := writeAuditChallengeSQLSnapshot(base.ctx, f, map[string]int64{"expiry": actor}, seedHash, snapshot); err != nil {
		t.Fatal(err)
	}
	safe, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{nonce, "helper-session-hash", "helper-token-hash", "helper-ip-hash", "answer_hash", "token_hash", "session_hash", "ip_hash"} {
		if strings.Contains(string(safe), secret) {
			t.Fatal("safe evidence snapshot exposed a forbidden private field")
		}
	}
	wrong := sha256.Sum256([]byte("11"))
	r.AnswerSHA256 = hex.EncodeToString(wrong[:])
	raw, _ = json.Marshal(r)
	_ = os.WriteFile(filepath.Join(dir, "done.flag"), raw, 0600)
	if _, err = sealAuditExpiredBrowserRequest(base.ctx, f, map[string]int64{"expiry": actor}, map[string]string{"expiry": publicID}, dir, "codec-only", r.BackendCommit, r.FrontendCommit, seedHash); err == nil || !strings.Contains(err.Error(), "correct solution") {
		t.Fatal("codec accepted a different answer commitment")
	}
	t.Log("Evidence helper serializer/SQL binding only: synthetic expired DB rows validate exact schema/context/content/answer comparison and secret exclusion. This does not prove natural product expiry or a browser run.")
}

func auditChallengeHex(value string, n int) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(value) == n && len(decoded) == n/2
}

// Persist only public synthetic IDs, states, timestamps, rules and comparison
// booleans. Nonces, answer/token/session/IP hashes and credentials stay private.
func writeAuditChallengeSQLSnapshot(ctx context.Context, f auditFixGRFixture, ids map[string]int64, seedHash, path string) error {
	roles := make([]string, 0, len(ids))
	for role := range ids {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	users := make([]int64, len(roles))
	for i, role := range roles {
		users[i] = ids[role]
	}
	var raw []byte
	err := f.db.QueryRow(ctx, `with actors as(select * from unnest($1::text[],$2::bigint[]) a(role,user_id))
	select jsonb_build_object('databaseTime',clock_timestamp(),'productionExecuted',false,'actors',coalesce((
	select jsonb_agg(jsonb_build_object('role',a.role,'publicId',u.public_id,
	'modCount',(select count(*) from mods m where m.submitted_by=u.id),
	'pendingModCount',(select count(*) from mods m where m.submitted_by=u.id and m.review_status='pending'),
	'challenges',coalesce((select jsonb_agg(jsonb_build_object('id',c.public_id,'kind',c.kind,'provider',c.provider,'status',c.status,
	'failureCount',c.failure_count,'action',c.action,'objectKey',c.object_key,'issuedAt',c.issued_at,'expiresAt',c.expires_at,
	'passedAt',c.passed_at,'consumedAt',c.consumed_at) order by c.issued_at) from anti_abuse_challenges c where c.user_id=u.id),'[]'::jsonb),
	'riskEvents',coalesce((select jsonb_agg(jsonb_build_object('id',e.public_id,'createdAt',e.created_at,'requestId',e.request_id,
	'action',e.action,'objectKey',e.object_key,'outcome',e.outcome,'riskScore',e.risk_score,'rules',e.rule_codes,
	'bodyMatchesReference',e.content_hash=$3,
	'contextMatchesHuman',exists(select 1 from anti_abuse_challenges c where c.user_id=e.user_id and c.kind='human'
	and c.action=e.action and c.object_key=e.object_key and c.session_hash=e.session_hash and (c.ip_hash='' or c.ip_hash=e.ip_hash)))
	order by e.created_at) from anti_abuse_events e where e.user_id=u.id),'[]'::jsonb)) order by a.role)
	from actors a join users u on u.id=a.user_id),'[]'::jsonb))`, roles, users, seedHash).Scan(&raw)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0600)
}

func sealAuditExpiredBrowserRequest(ctx context.Context, f auditFixGRFixture, ids map[string]int64, publicIDs map[string]string, dir, caseID, backendCommit, frontendCommit, seedHash string) (map[string]any, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "done.flag"))
	if err != nil {
		return nil, err
	}
	var receipt auditExpiryBrowserReceipt
	if json.Unmarshal(raw, &receipt) != nil || receipt.CaseID != caseID || receipt.ActorPublicID != publicIDs["expiry"] || !validCatalogPublicID(receipt.ChallengeID) || receipt.RequestID == "" || len(receipt.RequestID) > 128 || strings.ContainsAny(receipt.RequestID, "\r\n") || receipt.ExpiredRequestAt.IsZero() || receipt.BackendCommit != backendCommit || receipt.FrontendCommit != frontendCommit || !auditChallengeHex(receipt.FirstBodySHA256, 64) || receipt.FirstBodySHA256 != receipt.ExpiredBodySHA256 || !auditChallengeHex(receipt.AnswerSHA256, 64) {
		return nil, errors.New("invalid or mismatched expiry browser receipt; no event acceptance inferred")
	}
	var nonce, answerHash, status string
	var expires time.Time
	var failures int
	err = f.db.QueryRow(ctx, `select metadata->>'nonce',answer_hash,expires_at,status,failure_count from anti_abuse_challenges
	where public_id=$1 and user_id=$2 and kind='human' and provider='proof' and action='review.submit' and object_key='/api/v1/mods'`, receipt.ChallengeID, ids["expiry"]).Scan(&nonce, &answerHash, &expires, &status, &failures)
	if err != nil {
		return nil, errors.New("receipt does not identify the actual actor's scoped proof challenge")
	}
	if receipt.ExpiredRequestAt.Before(expires) || status != "pending" || failures != 0 {
		return nil, errors.New("receipt did not submit an expired unused challenge after its real expiry")
	}
	// The production proof generator uses sums of two integers in[2,9]. Match
	// its private answer commitment without persisting the nonce or the answer.
	correctSolution := false
	for sum := 4; sum <= 18; sum++ {
		answer := strconv.Itoa(sum)
		mac := hmac.New(sha256.New, []byte(f.server.cfg.AntiAbuse.HMACSecret))
		_, _ = mac.Write([]byte(nonce + "\x00" + answer))
		if hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(answerHash)) {
			digest := sha256.Sum256([]byte(answer))
			correctSolution = hex.EncodeToString(digest[:]) == receipt.AnswerSHA256
			break
		}
	}
	if !correctSolution {
		return nil, errors.New("browser answer SHA does not match this actual challenge's correct solution commitment")
	}
	wait, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var matched []byte
	for {
		err = f.db.QueryRow(wait, `select jsonb_build_object('eventId',e.public_id,'requestId',e.request_id,'eventAt',e.created_at,
		'challengeId',c.public_id,'expiresAt',c.expires_at,'outcome',e.outcome,'rules',e.rule_codes,
		'contextBound',true,'bodyMatchesReference',true,'correctSolutionBound',true,'expiredChallengeUnused',true)
		from anti_abuse_events e join anti_abuse_challenges c on c.user_id=e.user_id and c.public_id=$2
		and c.kind='human' and c.status='pending' and c.failure_count=0 and c.passed_at is null and c.consumed_at is null
		and c.action=e.action and c.object_key=e.object_key and c.session_hash=e.session_hash and (c.ip_hash='' or c.ip_hash=e.ip_hash)
		where e.user_id=$1 and e.request_id=$3 and e.content_hash=$4 and e.action='review.submit' and e.object_key='/api/v1/mods'
		and e.rule_codes @>array['challenge_failed']::text[] and e.created_at>=c.expires_at and c.expires_at<=$5
		order by e.created_at limit 1`, ids["expiry"], receipt.ChallengeID, receipt.RequestID, seedHash, receipt.ExpiredRequestAt).Scan(&matched)
		if err == nil {
			break
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("read exact expiry risk-event binding: %w", err)
		}
		select {
		case <-ticker.C:
		case <-wait.Done():
			return nil, errors.New("specific expired request challenge_failed event not observed within20s; generic429 is insufficient")
		}
	}
	var result map[string]any
	if err = json.Unmarshal(matched, &result); err != nil {
		return nil, err
	}
	result["caseId"] = caseID
	result["actorPublicId"] = receipt.ActorPublicID
	result["stableBodySHA256"] = receipt.FirstBodySHA256
	result["backendCommit"] = backendCommit
	result["frontendCommit"] = frontendCommit
	result["productChallengeTTLSeconds"] = 600
	result["productionExecuted"] = false
	return result, nil
}
