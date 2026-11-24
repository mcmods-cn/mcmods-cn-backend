package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
)

func TestMetadataImportReturnsCommittedQueueReceiptIntegration(t *testing.T) {
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
	actor := newCurrentWriteTestActor(t, ctx, pool, "import-receipt", "project.create")
	s := &Server{db: pool, cfg: config.Load(), cache: querycache.New(config.RedisConfig{})}
	defer s.cache.Close()
	source := "https://modrinth.com/mod/receipt-fixture"
	observation := mutationReceiptObservation{}
	requestCtx := context.WithValue(ctx, mutationReceiptObservationKey{}, &observation)
	r := newProjectHandlerRequest(t, requestCtx, actor, http.MethodPost, "/api/v1/mod-imports",
		modMetadataImportRequest{Provider: "modrinth", URL: source})
	w := httptest.NewRecorder()
	s.createModMetadataImport(w, r)

	var stored modMetadataImportJobResponse
	if err := base.QueryRow(ctx, `select public_id,project_type,provider,source_url,status,progress,created_at,updated_at
		from mod_metadata_import_jobs where user_id=$1 and source_url=$2`, actor.Subject, source).
		Scan(&stored.ID, &stored.ProjectType, &stored.Provider, &stored.SourceURL, &stored.Status, &stored.Progress, &stored.CreatedAt, &stored.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	var events int
	if err := base.QueryRow(ctx, `select count(*) from nats_outbox where aggregate_id=$1 and payload->>'jobId'=$1
		and aggregate_type='mod_metadata_import_job' and subject='mod_metadata_import' and status='pending'`, stored.ID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if !observation.committed || events != 1 || w.Code != http.StatusAccepted {
		t.Fatalf("committed=%v outbox=%d POST status=%d", observation.committed, events, w.Code)
	}
	got := decodeTEST022Data[modMetadataImportJobResponse](t, w.Body.Bytes())
	if got.ID != stored.ID || got.ProjectType != "mod" || got.Provider != "modrinth" || got.SourceURL != source ||
		got.Status != "queued" || got.Progress != 0 || got.Error != "" || len(got.Result) != 0 ||
		!got.CreatedAt.Equal(stored.CreatedAt) || !got.UpdatedAt.Equal(stored.UpdatedAt) {
		t.Errorf("queued receipt=%+v, stored=%+v", got, stored)
	}
}

func TestMetadataImportPollingUsesRequestedProjectTypeIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.create", "modpack.create", "project.create.plugin")
	for _, tc := range []struct{ kind, source, prefix, otherPrefix string }{
		{"mod", "mod", "/api/v1/mod-imports", "/api/v1/modpack-imports"},
		{"modpack", "modpack", "/api/v1/modpack-imports", "/api/v1/mod-imports"},
		{"plugin", "plugin", "/api/v1/content-project-imports/plugin", "/api/v1/mod-imports"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			input := modMetadataImportRequest{Provider: "modrinth", URL: "https://modrinth.com/" + tc.source + "/poll-fixture"}
			data := f.require(t, f.editor, http.MethodPost, tc.prefix, input, http.StatusAccepted)
			job := decodeTEST022Data[modMetadataImportJobResponse](t, data)
			if job.ID == "" || job.ProjectType != tc.kind || job.Status != "queued" {
				t.Fatalf("wrong accepted job: %+v", job)
			}
			polled := decodeTEST022Data[modMetadataImportJobResponse](t, f.require(t, f.editor, http.MethodGet, tc.prefix+"/"+job.ID, nil, http.StatusOK))
			if polled.ID != job.ID || polled.ProjectType != tc.kind || polled.Status != "queued" {
				t.Fatalf("wrong polling result: %+v", polled)
			}
			f.require(t, f.editor, http.MethodGet, tc.otherPrefix+"/"+job.ID, nil, http.StatusNotFound)
		})
	}
}
