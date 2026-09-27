package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestBUG042SeedCrawlerSubmissionFactsCommitTogetherAndRetryIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify atomic seed crawler submission")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/project/bug042-seed":
			_, _ = response.Write([]byte(`{"id":"bug042-external","slug":"bug042-seed","title":"Atomic seed","description":"Atomic summary","body":"Atomic body","project_type":"mod","status":"approved","client_side":"required","server_side":"required","categories":["utility"],"game_versions":["1.20.1"],"loaders":["fabric"],"license":{"id":"MIT"}}`))
		case "/project/bug042-external/version":
			_, _ = response.Write([]byte(`[]`))
		case "/project/bug042-plugin":
			_, _ = response.Write([]byte(`{"id":"bug042-plugin-external","slug":"bug042-plugin","title":"Atomic plugin","description":"Atomic plugin summary","body":"Atomic plugin body","project_type":"plugin","status":"approved","categories":["utility"],"game_versions":["1.20.1"],"loaders":["paper"],"license":{"id":"MIT"}}`))
		case "/project/bug042-plugin-external/version":
			_, _ = response.Write([]byte(`[]`))
		case "/project/bug042-manual":
			_, _ = response.Write([]byte(`{"id":"bug042-manual-external","slug":"bug042-manual","title":"Atomic manual draft","description":"Manual summary","body":"Manual body","project_type":"mod","status":"approved","client_side":"required","server_side":"required","categories":["utility"],"game_versions":["1.20.1"],"loaders":["fabric"],"license":{"id":"MIT"}}`))
		case "/project/bug042-manual-external/version":
			_, _ = response.Write([]byte(`[]`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer provider.Close()

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

	var actorID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('bug042-actor','bug042@example.test','integration-test',true) returning id`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	worker := NewSeedCrawlerWorker(loaded, pool)
	importConfig := defaultModImportConfig()
	importConfig.Modrinth.BaseURL = provider.URL
	importConfig.Modrinth.Token = ""
	sealedImportConfig, err := worker.server.sealSystemSetting(importConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value,updated_by) values($1,$2::jsonb,$3)`,
		modImportConfigSettingKey, sealedImportConfig, actorID); err != nil {
		t.Fatal(err)
	}
	var runID, candidateID int64
	if err = pool.QueryRow(ctx, `insert into seed_crawler_runs(actor_id,dry_run) values($1,false) returning id`, actorID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into seed_crawler_candidates(first_seen_run_id,last_seen_run_id,external_project_id,project_type,downloads,payload)
		values($1,$1,'bug042-external','mod',1000,'{}') returning id`, runID).Scan(&candidateID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `create function pg_temp.bug042_reject_source_binding() returns trigger language plpgsql as $$
		begin raise exception 'BUG042 forced source binding failure'; end $$;
		create trigger bug042_reject_source_binding before insert on project_external_sources
		for each row execute function pg_temp.bug042_reject_source_binding()`); err != nil {
		t.Fatal(err)
	}

	actor := security.Claims{Subject: actorID, Username: "bug042-actor", PermissionRules: []security.PermissionRule{{Code: "admin.*", Allow: true, Priority: 100}}}
	hit := seedModrinthHit{ProjectID: "bug042-external", Slug: "bug042-seed", Title: "Atomic seed", Downloads: 1000}
	status, submissionErr := worker.createSeedDraft(ctx, candidateID, actor, "mod", hit,
		seedCrawlerRuntimeConfig{AIDailyTokenBudget: 0, AutoSubmitReview: true})
	if submissionErr == nil {
		t.Fatalf("forced source failure returned status %q without an error", status)
	}
	var projectCount, sourceCount, activeDrafts, submittedDrafts int
	var candidateStatus string
	if err = pool.QueryRow(ctx, `select
		(select count(*) from mods where modrinth_project_id='bug042-external'),
		(select count(*) from project_external_sources where external_project_id='bug042-external'),
		(select count(*) from user_drafts where draft_key='seed-crawler:bug042-external' and submitted_at is null),
		(select count(*) from user_drafts where draft_key='seed-crawler:bug042-external' and submitted_at is not null),
		(select status from seed_crawler_candidates where id=$1)`, candidateID).
		Scan(&projectCount, &sourceCount, &activeDrafts, &submittedDrafts, &candidateStatus); err != nil {
		t.Fatal(err)
	}
	if projectCount != 0 || sourceCount != 0 || activeDrafts != 1 || submittedDrafts != 0 || candidateStatus != "submitting" {
		t.Fatalf("failed finalization project/source/active-draft/submitted-draft/candidate=%d/%d/%d/%d/%s, want 0/0/1/0/submitting",
			projectCount, sourceCount, activeDrafts, submittedDrafts, candidateStatus)
	}

	if _, err = pool.Exec(ctx, `drop trigger bug042_reject_source_binding on project_external_sources`); err != nil {
		t.Fatal(err)
	}
	status, err = worker.createSeedDraft(ctx, candidateID, actor, "mod", hit,
		seedCrawlerRuntimeConfig{AIDailyTokenBudget: 0, AutoSubmitReview: true})
	if err != nil || status != "submitted" {
		t.Fatalf("retry status=%q err=%v", status, err)
	}
	var draftStatus, projectKey, targetURL string
	var changeRequestID *int64
	if err = pool.QueryRow(ctx, `select
		(select count(*) from mods where modrinth_project_id='bug042-external'),
		(select count(*) from project_external_sources where external_project_id='bug042-external'),
		(select count(*) from user_drafts where draft_key='seed-crawler:bug042-external' and submitted_at is null),
		(select count(*) from user_drafts where draft_key='seed-crawler:bug042-external' and submitted_at is not null),
		(select status from seed_crawler_candidates where id=$1),
		draft.submitted_status,draft.change_request_id,draft.project_key,draft.target_url
		from user_drafts draft where draft.draft_key='seed-crawler:bug042-external' and draft.submitted_at is not null`, candidateID).
		Scan(&projectCount, &sourceCount, &activeDrafts, &submittedDrafts, &candidateStatus,
			&draftStatus, &changeRequestID, &projectKey, &targetURL); err != nil {
		t.Fatal(err)
	}
	if projectCount != 1 || sourceCount != 1 || activeDrafts != 0 || submittedDrafts != 1 || candidateStatus != "submitted" ||
		changeRequestID == nil || draftStatus != "approved" || projectKey != "mod:bug042_seed" || targetURL != "/mods/bug042_seed" {
		t.Fatalf("retry project/source/active/submitted/candidate/draft/change/project/target=%d/%d/%d/%d/%s/%s/%v/%q/%q",
			projectCount, sourceCount, activeDrafts, submittedDrafts, candidateStatus, draftStatus, changeRequestID, projectKey, targetURL)
	}

	var pluginCandidateID int64
	if err = pool.QueryRow(ctx, `insert into seed_crawler_candidates(first_seen_run_id,last_seen_run_id,external_project_id,project_type,downloads,payload)
		values($1,$1,'bug042-plugin-external','plugin',2000,'{}') returning id`, runID).Scan(&pluginCandidateID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `create function pg_temp.bug042_reject_candidate_completion() returns trigger language plpgsql as $$
		begin
			if new.external_project_id='bug042-plugin-external' and new.status='submitted' then
				raise exception 'BUG042 forced candidate completion failure';
			end if;
			return new;
		end $$;
		create trigger bug042_reject_candidate_completion before update on seed_crawler_candidates
		for each row execute function pg_temp.bug042_reject_candidate_completion()`); err != nil {
		t.Fatal(err)
	}
	pluginHit := seedModrinthHit{ProjectID: "bug042-plugin-external", Slug: "bug042-plugin", Title: "Atomic plugin", Downloads: 2000}
	pluginStatus, pluginErr := worker.createSeedDraft(ctx, pluginCandidateID, actor, "plugin", pluginHit,
		seedCrawlerRuntimeConfig{AIDailyTokenBudget: 0, AutoSubmitReview: true})
	if pluginErr == nil {
		t.Fatalf("forced plugin candidate failure returned status %q without an error", pluginStatus)
	}
	if err = pool.QueryRow(ctx, `select
		(select count(*) from simple_projects where project_type='plugin' and modrinth_project_id='bug042-plugin-external'),
		(select count(*) from project_external_sources where external_project_id='bug042-plugin-external'),
		(select count(*) from user_drafts where draft_key='seed-crawler:bug042-plugin-external' and submitted_at is null),
		(select count(*) from user_drafts where draft_key='seed-crawler:bug042-plugin-external' and submitted_at is not null),
		(select status from seed_crawler_candidates where id=$1)`, pluginCandidateID).
		Scan(&projectCount, &sourceCount, &activeDrafts, &submittedDrafts, &candidateStatus); err != nil {
		t.Fatal(err)
	}
	if projectCount != 0 || sourceCount != 0 || activeDrafts != 1 || submittedDrafts != 0 || candidateStatus != "submitting" {
		t.Fatalf("failed plugin finalization project/source/active-draft/submitted-draft/candidate=%d/%d/%d/%d/%s",
			projectCount, sourceCount, activeDrafts, submittedDrafts, candidateStatus)
	}
	if _, err = pool.Exec(ctx, `drop trigger bug042_reject_candidate_completion on seed_crawler_candidates`); err != nil {
		t.Fatal(err)
	}
	pluginStatus, pluginErr = worker.createSeedDraft(ctx, pluginCandidateID, actor, "plugin", pluginHit,
		seedCrawlerRuntimeConfig{AIDailyTokenBudget: 0, AutoSubmitReview: true})
	if pluginErr != nil || pluginStatus != "submitted" {
		t.Fatalf("plugin retry status=%q err=%v", pluginStatus, pluginErr)
	}
	if err = pool.QueryRow(ctx, `select
		(select count(*) from simple_projects where project_type='plugin' and modrinth_project_id='bug042-plugin-external'),
		(select count(*) from project_external_sources where external_project_id='bug042-plugin-external'),
		(select count(*) from user_drafts where draft_key='seed-crawler:bug042-plugin-external' and submitted_at is null),
		(select count(*) from user_drafts where draft_key='seed-crawler:bug042-plugin-external' and submitted_at is not null),
		(select status from seed_crawler_candidates where id=$1),
		draft.submitted_status,draft.change_request_id,draft.project_key,draft.target_url
		from user_drafts draft where draft.draft_key='seed-crawler:bug042-plugin-external' and draft.submitted_at is not null`, pluginCandidateID).
		Scan(&projectCount, &sourceCount, &activeDrafts, &submittedDrafts, &candidateStatus,
			&draftStatus, &changeRequestID, &projectKey, &targetURL); err != nil {
		t.Fatal(err)
	}
	if projectCount != 1 || sourceCount != 1 || activeDrafts != 0 || submittedDrafts != 1 || candidateStatus != "submitted" ||
		changeRequestID == nil || draftStatus != "approved" || projectKey != "plugin:bug042_plugin" || targetURL != "/plugins/bug042_plugin" {
		t.Fatalf("plugin retry project/source/active/submitted/candidate/draft/change/project/target=%d/%d/%d/%d/%s/%s/%v/%q/%q",
			projectCount, sourceCount, activeDrafts, submittedDrafts, candidateStatus, draftStatus, changeRequestID, projectKey, targetURL)
	}

	var manualCandidateID int64
	if err = pool.QueryRow(ctx, `insert into seed_crawler_candidates(first_seen_run_id,last_seen_run_id,external_project_id,project_type,downloads,payload)
		values($1,$1,'bug042-manual-external','mod',3000,'{}') returning id`, runID).Scan(&manualCandidateID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `create function pg_temp.bug042_reject_draft_stage() returns trigger language plpgsql as $$
		begin
			if new.external_project_id='bug042-manual-external' and new.status='draft' then
				raise exception 'BUG042 forced draft stage failure';
			end if;
			return new;
		end $$;
		create trigger bug042_reject_draft_stage before update on seed_crawler_candidates
		for each row execute function pg_temp.bug042_reject_draft_stage()`); err != nil {
		t.Fatal(err)
	}
	manualHit := seedModrinthHit{ProjectID: "bug042-manual-external", Slug: "bug042-manual", Title: "Atomic manual draft", Downloads: 3000}
	manualStatus, manualErr := worker.createSeedDraft(ctx, manualCandidateID, actor, "mod", manualHit,
		seedCrawlerRuntimeConfig{AIDailyTokenBudget: 0, AutoSubmitReview: false})
	if manualErr == nil {
		t.Fatalf("forced manual draft failure returned status %q without an error", manualStatus)
	}
	if err = pool.QueryRow(ctx, `select
		(select count(*) from user_drafts where draft_key='seed-crawler:bug042-manual-external'),
		(select status from seed_crawler_candidates where id=$1)`, manualCandidateID).Scan(&activeDrafts, &candidateStatus); err != nil {
		t.Fatal(err)
	}
	if activeDrafts != 0 || candidateStatus != "candidate" {
		t.Fatalf("failed manual stage draft/candidate=%d/%s, want 0/candidate", activeDrafts, candidateStatus)
	}
	if _, err = pool.Exec(ctx, `drop trigger bug042_reject_draft_stage on seed_crawler_candidates`); err != nil {
		t.Fatal(err)
	}
	manualStatus, manualErr = worker.createSeedDraft(ctx, manualCandidateID, actor, "mod", manualHit,
		seedCrawlerRuntimeConfig{AIDailyTokenBudget: 0, AutoSubmitReview: false})
	if manualErr != nil || manualStatus != "draft" {
		t.Fatalf("manual draft retry status=%q err=%v", manualStatus, manualErr)
	}
	if err = pool.QueryRow(ctx, `select
		(select count(*) from user_drafts where draft_key='seed-crawler:bug042-manual-external' and submitted_at is null),
		(select status from seed_crawler_candidates where id=$1)`, manualCandidateID).Scan(&activeDrafts, &candidateStatus); err != nil {
		t.Fatal(err)
	}
	if activeDrafts != 1 || candidateStatus != "draft" {
		t.Fatalf("manual draft retry draft/candidate=%d/%s, want 1/draft", activeDrafts, candidateStatus)
	}
}
