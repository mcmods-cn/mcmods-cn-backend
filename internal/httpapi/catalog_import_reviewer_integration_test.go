package httpapi

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/security"
)

func TestCatalogReviewUsesCurrentSessionAndProjectGrantInTransaction(t *testing.T) {
	f := newAuditFixGRFixture(t)
	actor := f.userIDs[f.otherEditor]
	code := "project.review." + f.modCode
	grantTEST044Permissions(t, f.ctx, f.db, actor, code)
	claims, err := security.ParseToken(f.server.cfg.JWTSecret, f.otherEditor)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.server.resolveClaimsSubject(f.ctx, &claims); err != nil {
		t.Fatal(err)
	}
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = lockImportedContentVersionTx(f.ctx, tx, f.versionID); err != nil {
		t.Fatal(err)
	}
	check := func(project string, author int64, want bool) {
		t.Helper()
		allowed, lookupErr := catalogReviewerAllowedTx(f.ctx, tx, claims, project, author)
		if lookupErr != nil || allowed != want {
			t.Fatalf("review authorization=%v want=%v err=%v", allowed, want, lookupErr)
		}
	}
	check(f.modCode, f.userIDs[f.editor], true)
	check(f.otherModCode, f.userIDs[f.editor], false)
	check(f.modCode, actor, false)

	// Set an actual elapsed end inside this open transaction. PostgreSQL's
	// transaction start time predates it, while current statements must see it.
	var expiredAfterBegin bool
	if err = tx.QueryRow(f.ctx, `update user_permissions set expires_at=statement_timestamp()-interval '1 microsecond'
		where user_id=$1 and permission_id=(select id from permissions where code=$2)
		returning expires_at>transaction_timestamp()`, actor, code).Scan(&expiredAfterBegin); err != nil || !expiredAfterBegin {
		t.Fatalf("unexpected fixture grant end relative to transaction start: %v", err)
	}
	check(f.modCode, f.userIDs[f.editor], false)
	if _, err = tx.Exec(f.ctx, `update auth_sessions set revoked_at=statement_timestamp() where session_hash=$1`, security.SessionFingerprint(claims.SessionID)); err != nil {
		t.Fatal(err)
	}
	if allowed, lookupErr := catalogReviewerAllowedTx(f.ctx, tx, claims, f.modCode, f.userIDs[f.editor]); allowed || !errors.Is(lookupErr, pgx.ErrNoRows) {
		t.Fatalf("revoked session produced authorization=%v err=%v", allowed, lookupErr)
	}
}
