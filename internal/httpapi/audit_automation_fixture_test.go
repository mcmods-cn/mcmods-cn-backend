package httpapi

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/security"
	"mcmods-cn-backend/internal/systemactor"
)

// Old positive worker fixtures used interactive users and forged admin claims.
// These fixtures exercise the current noninteractive subject and real RBAC;
// each caller declares only the permissions its original positive path needs.
func seedAuthorizedAutomationFixture(t *testing.T, ctx context.Context, db *pgxpool.Pool, permissions ...string) security.Claims {
	t.Helper()
	var id int64
	if err := db.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified,status)
 values($1,$2,'login-disabled',true,$3) returning id`, systemactor.AutobotUsername, systemactor.AutobotEmail, systemactor.AutobotStatus).Scan(&id); err != nil {
		t.Fatal(err)
	}
	grantTEST044Permissions(t, ctx, db, id, permissions...)
	actor, err := automationActorQuery(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if actor.Subject != id {
		t.Fatal("automation fixture resolved a different subject")
	}
	return actor
}

// A fully initialized fixture already owns the real system actor. Select it
// and set only the grants the caller needs for its enable/revoke scenarios.
func configureSeededAutomationFixture(t *testing.T, ctx context.Context, db *pgxpool.Pool, permissions ...string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(ctx, `select id from users where username=$1 and email=$2 and status=$3
		and password_hash='password-login-disabled'`,
		systemactor.AutobotUsername, systemactor.AutobotEmail, systemactor.AutobotStatus).Scan(&id); err != nil {
		t.Fatalf("load installed system actor: %v", err)
	}
	if _, err := db.Exec(ctx, `delete from user_permissions where user_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	grantTEST044Permissions(t, ctx, db, id, permissions...)
	return id
}

// The production HTTP creation handler intentionally hides DB errors. Capture
// only the exact synthetic P0001 marker in this fixture's own connection tracer
// so a different SQL/authentication failure cannot satisfy an injected-fault case.
type automationFixtureFaultTrace struct {
	mu       sync.Mutex
	messages []string
}

func (*automationFixtureFaultTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}
func (f *automationFixtureFaultTrace) TraceQueryEnd(_ context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	var e *pgconn.PgError
	if errors.As(data.Err, &e) && e.Code == "P0001" && strings.HasPrefix(e.Message, "BUG042 forced ") {
		f.mu.Lock()
		f.messages = append(f.messages, e.Message)
		f.mu.Unlock()
	}
}
func (f *automationFixtureFaultTrace) reached(message string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, got := range f.messages {
		if got == message {
			return true
		}
	}
	return false
}
