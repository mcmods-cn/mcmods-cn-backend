package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type revisionPublicIDQueryStub struct {
	publicID string
	err      error
	called   bool
}

func (query *revisionPublicIDQueryStub) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	query.called = true
	return revisionPublicIDRowStub{publicID: query.publicID, err: query.err}
}

type revisionPublicIDRowStub struct {
	publicID string
	err      error
}

func (row revisionPublicIDRowStub) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	*destinations[0].(*string) = row.publicID
	return nil
}

func TestRevisionPublicIDValuePreservesLookupErrors(t *testing.T) {
	internalID := int64(42)
	wantErr := errors.New("revision route unavailable")
	failingQuery := &revisionPublicIDQueryStub{err: wantErr}
	publicID, err := revisionPublicIDValue(context.Background(), failingQuery, &internalID)
	if publicID != nil || !errors.Is(err, wantErr) {
		t.Fatalf("lookup failure = (%v, %v), want (nil, %v)", publicID, err, wantErr)
	}

	successfulQuery := &revisionPublicIDQueryStub{publicID: "revision-public-id"}
	publicID, err = revisionPublicIDValue(context.Background(), successfulQuery, &internalID)
	if err != nil || publicID == nil || *publicID != "revision-public-id" {
		t.Fatalf("successful lookup = (%v, %v), want revision-public-id", publicID, err)
	}

	nilQuery := &revisionPublicIDQueryStub{err: errors.New("must not query")}
	publicID, err = revisionPublicIDValue(context.Background(), nilQuery, nil)
	if err != nil || publicID != nil || nilQuery.called {
		t.Fatalf("nil revision = (%v, %v, called=%t), want (nil, nil, false)", publicID, err, nilQuery.called)
	}
}

func TestPublicUserLookupReportsUnavailableStorage(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://localhost/closed")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	server := &Server{db: pool}
	for name, handler := range map[string]http.HandlerFunc{"details": server.adminUserDetails, "balances": server.adminUserBalances, "adjust balance": server.adjustAdminUserBalance,
		"permissions": server.userPermissionDetails, "update permissions": server.updateUserPermissions, "blocks": server.userBlock} {
		t.Run(name, func(t *testing.T) {
			for _, item := range []struct {
				id   string
				want int
			}{{"", http.StatusBadRequest}, {"not-an-id", http.StatusBadRequest}, {"abc123def", http.StatusServiceUnavailable}} {
				request := httptest.NewRequest("GET", "/synthetic/user", nil)
				request.SetPathValue("id", item.id)
				response := httptest.NewRecorder()
				handler(response, request)
				if response.Code != item.want || strings.Contains(response.Body.String(), "closed pool") {
					t.Fatalf("response=%d expected %d", response.Code, item.want)
				}
			}
		})
	}
}

func TestUserPathResolvesOnlyItsRegisteredEntityIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	s := &Server{db: f.db}
	var id int64
	var account, other string
	if err := f.db.QueryRow(f.ctx, `insert into users(username,email,password_hash) values('identity-test','identity-test@example.invalid','test') returning id,public_id`).Scan(&id, &account); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `insert into mods(project_code,slug,primary_name,submitted_by,review_status) values(new_public_id(),'identity-test','identity-test',$1,'approved') returning project_code`, id).Scan(&other); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path string
		want bool
	}{{" " + strings.ToUpper(account) + " ", true}, {other, false}, {"000000000", false}} {
		r := httptest.NewRequest(http.MethodGet, "/synthetic", nil)
		r.SetPathValue("id", test.path)
		w := httptest.NewRecorder()
		got, ok := s.pathUserIdentity(w, r)
		if ok != test.want || ok && (got.InternalID != id || got.PublicID != account) || !ok && w.Code != http.StatusBadRequest {
			t.Fatalf("lookup=%+v ok=%t HTTP=%d", got, ok, w.Code)
		}
	}
}
