package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
)

// After commit only the explicitly reported security version propagation may
// still need a database connection. Reading project details then is made to fail.
func TestProjectCreationReturnsCompleteCommittedReceiptsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	base := newOCT02IsolatedDatabase(t, ctx)
	pc := base.Config()
	pc.ConnConfig.Tracer = mutationReceiptTracer{}
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	s := &Server{db: pool, cfg: config.Load(), cache: querycache.New(config.RedisConfig{})}
	actor := newCurrentWriteTestActor(t, ctx, pool, "creation-receipt", "project.create", "modpack.create", "project.create.plugin")
	mod := createModRequest{SiteID: "receipt-mod", PrimaryName: "Receipt mod", Environment: "bothRequired", PrimaryCategory: "technology", OfficialStatus: "active",
		SourceStatus: "open", License: "MIT", Tags: []string{"automation"}, SubmissionMethod: "manual"}
	for _, tc := range []struct {
		kind, site, field string
		input             any
		handler           http.HandlerFunc
	}{
		{"mod", mod.SiteID, "tags", mod, s.createMod},
		{"modpack", "receipt-pack", "links", perf020ModpackSnapshot("receipt-pack", 0), s.createModpack},
		{"plugin", "receipt-plugin", "localizations", projectCreationTestSnapshot("plugin", "receipt-plugin", "manual"),
			func(w http.ResponseWriter, r *http.Request) { s.createSimpleProject(w, r, "plugin") }},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			observation := mutationReceiptObservation{allowProjectACLRefresh: true}
			requestCtx := context.WithValue(ctx, mutationReceiptObservationKey{}, &observation)
			r := newProjectHandlerRequest(t, requestCtx, actor, http.MethodPost, "/projects", tc.input)
			w := httptest.NewRecorder()
			tc.handler(w, r)
			var revision, change, status string
			err := base.QueryRow(ctx, `select rev.public_id,change.public_id,change.status from content_revisions rev
				join change_requests change on change.proposed_revision_id=rev.id
				where rev.entity_type=$1 and rev.snapshot->>'siteId'=$2 and rev.created_by=$3`, tc.kind, tc.site, actor.Subject).Scan(&revision, &change, &status)
			if err != nil {
				t.Fatalf("cannot find committed project revision: %v", err)
			}
			if w.Code != http.StatusCreated || !observation.committed || observation.lateSQLAttempts != 0 {
				t.Fatalf("project exists but response=%d late reads=%d committed=%v", w.Code, observation.lateSQLAttempts, observation.committed)
			}
			data := decodeTEST022Data[map[string]json.RawMessage](t, w.Body.Bytes())
			for field, want := range map[string]string{"siteId": tc.site, "reviewStatus": status, "changeRequestId": change, "submissionRevisionId": revision} {
				var got string
				if err := json.Unmarshal(data[field], &got); err != nil || got != want {
					t.Errorf("receipt %s = %q, want %q (%v)", field, got, want, err)
				}
			}
			var associated []json.RawMessage
			if err := json.Unmarshal(data[tc.field], &associated); err != nil || len(associated) == 0 {
				t.Fatalf("incomplete %s receipt: %v", tc.field, err)
			}
		})
	}
}
