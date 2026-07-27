package httpapi

import (
	"context"
	"errors"
	"strings"
	"time"
)

var errAuthRateLimited = errors.New("authentication rate limit exceeded")

func (s *Server) authAttemptRateLimited(ctx context.Context, account, ip string) (bool, error) {
	account = strings.ToLower(strings.TrimSpace(account))
	ip = normalizeIPAddress(ip)
	var accountFailures, ipFailures int
	err := s.db.QueryRow(ctx, `select
		count(*) filter(where lower(account)=$1),
		count(*) filter(where ip=$2)
		from user_login_logs
		where success=false and created_at>now()-interval '15 minutes'
		  and (lower(account)=$1 or ($2<>'' and ip=$2))`, account, ip).Scan(&accountFailures, &ipFailures)
	return accountFailures >= 12 || ipFailures >= 30, err
}

func (s *Server) registrationRateLimited(ctx context.Context, ip string) (bool, error) {
	ip = normalizeIPAddress(ip)
	if ip == "" {
		return false, nil
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('register-ip:'||$1,0))`, ip); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `delete from user_registration_attempts
		where ip=$1 and created_at<now()-interval '24 hours'`, ip); err != nil {
		return false, err
	}
	var attempts int
	if err = tx.QueryRow(ctx, `select count(*) from user_registration_attempts
		where ip=$1 and created_at>now()-interval '1 hour'`, ip).Scan(&attempts); err != nil {
		return false, err
	}
	if attempts >= 10 {
		return true, tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `insert into user_registration_attempts(ip) values($1)`, ip); err != nil {
		return false, err
	}
	return false, tx.Commit(ctx)
}

func (s *Server) storeEmailVerificationCode(ctx context.Context, email, purpose, codeHash, ip string, expiresAt time.Time) (int64, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	ip = normalizeIPAddress(ip)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('email-code:'||$1,0))`, email); err != nil {
		return 0, err
	}
	if ip != "" {
		if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('email-code-ip:'||$1,0))`, ip); err != nil {
			return 0, err
		}
	}
	if _, err = tx.Exec(ctx, `delete from email_verification_codes
		where created_at<now()-interval '24 hours'
		  and (email=$1 or ($2<>'' and request_ip=$2))`, email, ip); err != nil {
		return 0, err
	}
	var emailCount, ipCount int
	if err = tx.QueryRow(ctx, `select
		count(*) filter(where email=$1 and purpose=$2),
		count(*) filter(where $3<>'' and request_ip=$3)
		from email_verification_codes
		where created_at>now()-interval '10 minutes'
		  and ((email=$1 and purpose=$2) or ($3<>'' and request_ip=$3))`,
		email, purpose, ip).Scan(&emailCount, &ipCount); err != nil {
		return 0, err
	}
	if emailCount >= 3 || ipCount >= 20 {
		return 0, errAuthRateLimited
	}
	var codeID int64
	if err = tx.QueryRow(ctx, `insert into email_verification_codes(email,purpose,code_hash,request_ip,expires_at)
		values($1,$2,$3,$4,$5) returning id`, email, purpose, codeHash, ip, expiresAt).Scan(&codeID); err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return codeID, nil
}
