package antiabuse

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/querycache"
)

func TestFormAndHumanChallengesAreBoundAndSingleUse(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 with an isolated development database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cfg := config.Load()
	pool, err := pgxpool.New(ctx, cfg.DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	suffix := time.Now().UnixNano()
	var userID int64
	err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified) values($1,$2,'integration-test',true) returning id`,
		fmt.Sprintf("anti-abuse-it-%d", suffix), fmt.Sprintf("anti-abuse-it-%d@example.invalid", suffix)).Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(context.Background(), `delete from users where id=$1`, userID) }()

	antiCfg := cfg.AntiAbuse
	antiCfg.Enabled, antiCfg.HMACSecret, antiCfg.IPHashSecret = true, "integration-hmac-secret-that-is-at-least-32-bytes", "integration-ip-secret-that-is-at-least-32-bytes"
	antiCfg.FormTokenTTL, antiCfg.FormMinimumAge, antiCfg.ChallengeTTL, antiCfg.ChallengeProvider = time.Minute, 0, time.Minute, "proof"
	service := New(context.Background(), antiCfg, pool, querycache.New(config.RedisConfig{}))
	objectKey, sessionID, ip := "/api/v1/comment-targets/community_post/test/comments", "integration-session", "192.0.2.20"
	form, err := service.IssueFormToken(ctx, userID, sessionID, ip, "comment.create", objectKey)
	if err != nil {
		t.Fatal(err)
	}
	base := Evaluation{UserID: userID, SessionID: sessionID, IP: ip, DeviceID: "integration-device", UserAgent: "Mozilla/5.0", Action: "comment.create", ObjectKey: objectKey, Now: time.Now()}
	first, err := service.Evaluate(ctx, withEvaluation(base, form.Token, "first unique integration comment", ""))
	if err != nil || antiAbuseTestBlocks(first.Outcome) {
		t.Fatalf("valid form token was blocked: decision=%+v err=%v", first, err)
	}
	second, err := service.Evaluate(ctx, withEvaluation(base, form.Token, "second unique integration comment", ""))
	if err != nil || second.Outcome != Challenge || second.Challenge == nil {
		t.Fatalf("replayed form token did not trigger a challenge: decision=%+v err=%v", second, err)
	}
	answer := solveProofPrompt(t, second.Challenge.Prompt)
	passed, err := service.Evaluate(ctx, withEvaluation(base, "", "second unique integration comment", second.Challenge.ID+":"+answer))
	if err != nil || antiAbuseTestBlocks(passed.Outcome) {
		t.Fatalf("valid one-time challenge was blocked: decision=%+v err=%v", passed, err)
	}
	reused, err := service.Evaluate(ctx, withEvaluation(base, "", "third unique integration comment", second.Challenge.ID+":"+answer))
	if err != nil || reused.Outcome == Allow {
		t.Fatalf("reused challenge proof unexpectedly allowed: decision=%+v err=%v", reused, err)
	}

	concurrent, err := service.IssueFormToken(ctx, userID, sessionID, ip, "comment.reply", objectKey)
	if err != nil {
		t.Fatal(err)
	}
	input := base
	input.Action, input.FormToken = "comment.reply", concurrent.Token
	var successes atomic.Int32
	var group sync.WaitGroup
	for range 12 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, ok := service.consumeFormToken(ctx, input, service.privateHash("session", sessionID), service.privateHash("ip", ip)); ok {
				successes.Add(1)
			}
		}()
	}
	group.Wait()
	if successes.Load() != 1 {
		t.Fatalf("concurrent form token consumed %d times, want 1", successes.Load())
	}

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer provider.Close()
	turnstileCfg := antiCfg
	turnstileCfg.ChallengeProvider, turnstileCfg.TurnstileSiteKey, turnstileCfg.TurnstileSecretKey, turnstileCfg.TurnstileVerifyURL = "turnstile", "test-site", "test-secret", provider.URL
	turnstile := New(context.Background(), turnstileCfg, pool, querycache.New(config.RedisConfig{}))
	challenge, err := turnstile.createChallenge(ctx, base, turnstile.privateHash("session", sessionID), turnstile.privateHash("ip", ip))
	if err != nil {
		t.Fatal(err)
	}
	degraded, err := turnstile.Evaluate(ctx, withEvaluation(base, "", "provider outage content", challenge.ID+":provider-token"))
	if err != nil || degraded.Outcome != Moderation || !degraded.Moderation {
		t.Fatalf("challenge outage did not degrade to moderation: decision=%+v err=%v", degraded, err)
	}
}

func withEvaluation(base Evaluation, form, content, proof string) Evaluation {
	base.FormToken, base.Content, base.ChallengeProof = form, content, proof
	return base
}
func antiAbuseTestBlocks(value Outcome) bool {
	return value == Challenge || value == Delay || value == TempBlock || value == Deny || value == AccountReview
}
func solveProofPrompt(t *testing.T, prompt string) string {
	t.Helper()
	values := regexp.MustCompile(`\d+`).FindAllString(prompt, -1)
	if len(values) != 2 {
		t.Fatalf("unexpected proof prompt %q", prompt)
	}
	left, _ := strconv.Atoi(values[0])
	right, _ := strconv.Atoi(values[1])
	return strconv.Itoa(left + right)
}
