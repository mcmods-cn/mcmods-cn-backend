package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type oct02ChangelogLookupDisconnect struct {
	query string
	hits  atomic.Int32
}

func (fault *oct02ChangelogLookupDisconnect) TraceQueryStart(ctx context.Context, conn *pgx.Conn, query pgx.TraceQueryStartData) context.Context {
	if strings.TrimSpace(query.SQL) == fault.query {
		fault.hits.Add(1)
		// Only this test's separate pool connection is closed. The authenticated
		// request has already loaded its real project and changelog successfully.
		_ = conn.Close(ctx)
	}
	return ctx
}
func (*oct02ChangelogLookupDisconnect) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func TestOCT02ChangelogLookupDatabaseFailureIsNotNotFoundIntegration(t *testing.T) {
	f := newTEST017Fixture(t)
	proposal := f.submit(t, f.editor, http.MethodPost, "/api/v1/changelogs?targetType=mod&targetId="+f.modCode, test017Snapshot("Published fixture body"), http.StatusCreated)
	f.review(t, f.denied, proposal, "approved", http.StatusOK)
	before := f.facts(t)

	for _, stage := range []struct {
		query    string
		status   int
		expected string
	}{
		{"select id from project_changelogs where public_id=$1", http.StatusInternalServerError, `"code":"CHANGELOG_LOOKUP_FAILED"`},
		{"select id from content_revisions where public_id=$1", http.StatusServiceUnavailable, "failed to read changelog revision"},
	} {
		t.Run(stage.expected, func(t *testing.T) {
			poolConfig := f.db.Config().Copy()
			fault := &oct02ChangelogLookupDisconnect{query: stage.query}
			poolConfig.ConnConfig.Tracer = fault
			pool, err := pgxpool.NewWithConfig(f.ctx, poolConfig)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			original := f.origin.Config.Handler.(*Server)
			server := &Server{cfg: original.cfg, db: pool, cache: original.cache, antiAbuse: original.antiAbuse, activity: original.activity}
			router := http.NewServeMux()
			router.Handle("PUT /api/v1/changelogs/{id}", server.requireAuth(http.HandlerFunc(server.projectChangelogItem)))
			origin := httptest.NewServer(router)
			defer origin.Close()
			faultFixture := f
			faultFixture.origin = origin
			snapshot := test017Snapshot("Must not be published")
			snapshot.BaseRevisionID = f.detail(t, f.editor, proposal.ID, http.StatusOK).PublishedRevisionID
			response := faultFixture.require(t, f.editor, http.MethodPut, "/api/v1/changelogs/"+proposal.ID, snapshot, stage.status)
			if fault.hits.Load() != 1 {
				t.Fatalf("exact lookup fault hits=%d, want 1", fault.hits.Load())
			}
			if !bytes.Contains(response, []byte(stage.expected)) {
				t.Fatalf("missing stable lookup error code: %s", response)
			}
			for _, internal := range []string{"select id", "conn closed", "project_changelogs", "postgres", "password"} {
				if bytes.Contains(response, []byte(internal)) {
					t.Fatalf("lookup failure exposed internal error detail %q", internal)
				}
			}
		})
	}
	f.unchanged(t, before)
	f.detail(t, f.editor, "oct02abs1", http.StatusNotFound)
}
