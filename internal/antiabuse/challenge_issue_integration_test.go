package antiabuse

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/querycache"
)

func TestProofChallengePersistsCompleteStateWithoutMetadataUpdateIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify atomic proof challenge issuance")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	loaded := config.Load()
	poolConfig, err := pgxpool.ParseConfig(loaded.DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	var userID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified,status)
		values('proof_issue','proof_issue@example.invalid','test-only',true,'active') returning id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `create function pg_temp.reject_challenge_metadata_update() returns trigger as $$
		begin
			raise exception 'challenge metadata must be complete at insert';
		end;
		$$ language plpgsql`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `create trigger trg_reject_challenge_metadata_update
		before update of metadata on anti_abuse_challenges
		for each row execute function pg_temp.reject_challenge_metadata_update()`); err != nil {
		t.Fatal(err)
	}

	cache := querycache.New(config.RedisConfig{})
	defer cache.Close()
	antiCfg := loaded.AntiAbuse
	antiCfg.Enabled = false
	antiCfg.ChallengeProvider = "proof"
	antiCfg.ChallengeTTL = time.Minute
	antiCfg.HMACSecret = "proof-issue-integration-hmac-secret"
	service := New(ctx, antiCfg, pool, cache)

	input := Evaluation{
		UserID: userID, SessionID: "proof-session", IP: "192.0.2.44",
		Action: "comment.create", ObjectKey: "community_post/proof", Now: time.Now(),
	}
	sessionHash := service.privateHash("session", input.SessionID)
	ipHash := service.privateHash("ip", input.IP)
	challenge, err := service.createChallenge(ctx, input, sessionHash, ipHash)
	if err != nil {
		t.Fatal(err)
	}
	answer := solveProofPrompt(t, challenge.Prompt)
	var nonce, answerHash, status string
	if err = pool.QueryRow(ctx, `select metadata->>'nonce',answer_hash,status
		from anti_abuse_challenges where public_id=$1`, challenge.ID).Scan(&nonce, &answerHash, &status); err != nil {
		t.Fatal(err)
	}
	if nonce == "" || answerHash != service.challengeAnswerHash(nonce, answer) || status != "pending" {
		t.Fatalf("incomplete proof challenge nonce=%q answerHashMatches=%t status=%q",
			nonce, answerHash == service.challengeAnswerHash(nonce, answer), status)
	}
	input.ChallengeProof = challenge.ID + ":" + answer
	passed, available := service.verifyChallenge(ctx, input, sessionHash, ipHash)
	if !passed || !available {
		t.Fatalf("complete proof challenge passed=%t available=%t", passed, available)
	}
	if err = pool.QueryRow(ctx, `select status from anti_abuse_challenges where public_id=$1`, challenge.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "consumed" {
		t.Fatalf("proof challenge status=%q want=consumed", status)
	}
}
