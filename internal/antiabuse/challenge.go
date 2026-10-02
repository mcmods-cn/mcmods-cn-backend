package antiabuse

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (s *Service) IssueFormToken(ctx context.Context, userID int64, sessionID, ip, action, objectKey string) (FormToken, error) {
	action = strings.TrimSpace(action)
	objectKey = truncate(strings.TrimSpace(objectKey), 160)
	if userID <= 0 || sessionID == "" || action == "" || len(action) > 80 {
		return FormToken{}, fmt.Errorf("invalid form token scope")
	}
	token, err := randomToken(32)
	if err != nil {
		return FormToken{}, err
	}
	expiresAt := s.now().Add(s.cfg.FormTokenTTL)
	var publicID string
	err = s.db.QueryRow(ctx, `insert into anti_abuse_challenges(user_id,session_hash,action,object_key,kind,provider,token_hash,status,ip_hash,expires_at)
		values($1,$2,$3,$4,'form','signed',$5,'pending',$6,$7) returning public_id`, userID,
		s.privateHash("session", sessionID), action, objectKey, tokenHash(token), s.privateHash("ip", ip), expiresAt).Scan(&publicID)
	if err != nil {
		return FormToken{}, err
	}
	return FormToken{Token: publicID + "." + token, FieldName: "contact_" + publicID, ExpiresAt: expiresAt}, nil
}

func (s *Service) consumeFormToken(ctx context.Context, input Evaluation, sessionHash, ipHash string) (time.Duration, bool) {
	publicID, token, ok := strings.Cut(strings.TrimSpace(input.FormToken), ".")
	if !ok || publicID == "" || token == "" {
		return 0, false
	}
	var issuedAt time.Time
	err := s.db.QueryRow(ctx, `update anti_abuse_challenges set status='consumed',consumed_at=now()
		where public_id=$1 and token_hash=$2 and kind='form' and status='pending' and user_id=$3 and session_hash=$4
		  and action=$5 and object_key=$6 and expires_at>now() and (ip_hash='' or ip_hash=$7)
		returning issued_at`, publicID, tokenHash(token), input.UserID, sessionHash, input.Action, truncate(input.ObjectKey, 160), ipHash).Scan(&issuedAt)
	if err != nil {
		return 0, false
	}
	return input.Now.Sub(issuedAt), true
}

func (s *Service) createChallenge(ctx context.Context, input Evaluation, sessionHash, ipHash string) (ChallengeInfo, error) {
	provider := s.cfg.ChallengeProvider
	if provider == "" || provider == "disabled" {
		return ChallengeInfo{}, fmt.Errorf("challenge provider is disabled")
	}
	token, err := randomToken(24)
	if err != nil {
		return ChallengeInfo{}, err
	}
	expiresAt := s.now().Add(s.cfg.ChallengeTTL)
	answerHash, prompt := "", ""
	if provider == "proof" {
		left, _ := rand.Int(rand.Reader, big.NewInt(8))
		right, _ := rand.Int(rand.Reader, big.NewInt(8))
		leftValue, rightValue := int(left.Int64())+2, int(right.Int64())+2
		answer := fmt.Sprintf("%d", leftValue+rightValue)
		answerHash = s.challengeAnswerHash(token, answer)
		prompt = fmt.Sprintf("请输入 %d + %d 的结果", leftValue, rightValue)
	}
	var publicID string
	err = s.db.QueryRow(ctx, `insert into anti_abuse_challenges
		(user_id,session_hash,action,object_key,kind,provider,token_hash,answer_hash,status,ip_hash,expires_at,metadata)
		values($1,$2,$3,$4,'human',$5,$6,$7,'pending',$8,$9,jsonb_build_object('nonce',$10::text)) returning public_id`,
		input.UserID, sessionHash, input.Action, truncate(input.ObjectKey, 160), provider, tokenHash(token), answerHash,
		ipHash, expiresAt, token).Scan(&publicID)
	if err != nil {
		return ChallengeInfo{}, err
	}
	return ChallengeInfo{ID: publicID, Provider: provider, Prompt: prompt, SiteKey: s.cfg.TurnstileSiteKey, ExpiresAt: expiresAt}, nil
}

func (s *Service) verifyChallenge(ctx context.Context, input Evaluation, sessionHash, ipHash string) (bool, bool) {
	publicID, answer, ok := strings.Cut(strings.TrimSpace(input.ChallengeProof), ":")
	if !ok || publicID == "" || answer == "" {
		return false, true
	}
	var provider, answerHash string
	var metadata []byte
	err := s.db.QueryRow(ctx, `select provider,answer_hash,metadata from anti_abuse_challenges
		where public_id=$1 and kind='human' and status='pending' and user_id=$2 and session_hash=$3 and action=$4 and object_key=$5
		  and expires_at>now() and (ip_hash='' or ip_hash=$6) for update`, publicID, input.UserID, sessionHash, input.Action,
		truncate(input.ObjectKey, 160), ipHash).Scan(&provider, &answerHash, &metadata)
	if err != nil {
		return false, true
	}
	passed := false
	available := true
	switch provider {
	case "proof":
		var values map[string]string
		_ = json.Unmarshal(metadata, &values)
		expected := s.challengeAnswerHash(values["nonce"], strings.TrimSpace(answer))
		passed = hmac.Equal([]byte(expected), []byte(answerHash))
	case "turnstile":
		var verifyErr error
		passed, verifyErr = s.verifyTurnstile(ctx, answer, input.IP)
		if verifyErr != nil {
			return false, false
		}
	}
	status := "failed"
	if passed {
		status = "consumed"
	}
	command, updateErr := s.db.Exec(ctx, `update anti_abuse_challenges set status=$2,failure_count=failure_count+case when $3 then 0 else 1 end,
		passed_at=case when $3 then now() else passed_at end,consumed_at=case when $3 then now() else consumed_at end
		where public_id=$1 and status='pending'`, publicID, status, passed)
	return updateErr == nil && command.RowsAffected() == 1 && passed, available
}

func (s *Service) challengeAnswerHash(nonce, answer string) string {
	mac := hmac.New(sha256.New, []byte(s.cfg.HMACSecret))
	_, _ = mac.Write([]byte(nonce + "\x00" + strings.TrimSpace(answer)))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Service) verifyTurnstile(ctx context.Context, token, remoteIP string) (bool, error) {
	if s.cfg.TurnstileSecretKey == "" {
		return false, fmt.Errorf("turnstile secret is unavailable")
	}
	values := url.Values{"secret": {s.cfg.TurnstileSecretKey}, "response": {token}}
	if remoteIP != "" {
		values.Set("remoteip", remoteIP)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.TurnstileVerifyURL, strings.NewReader(values.Encode()))
	if err != nil {
		return false, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 32<<10))
	if err != nil || response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("turnstile verification failed with status %d", response.StatusCode)
	}
	var result struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return false, err
	}
	return result.Success, nil
}
