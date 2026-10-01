package httpapi

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

type SeedCrawlerWorker struct {
	server *Server
}

func NewSeedCrawlerWorker(cfg config.Config, db *pgxpool.Pool) *SeedCrawlerWorker {
	return &SeedCrawlerWorker{server: &Server{cfg: cfg, db: db}}
}

func (worker *SeedCrawlerWorker) Start(ctx context.Context) {
	if worker == nil || worker.server == nil || worker.server.db == nil {
		return
	}
	go worker.run(ctx)
}

func (worker *SeedCrawlerWorker) run(ctx context.Context) {
	worker.schedule(ctx)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			worker.schedule(ctx)
		}
	}
}

func (worker *SeedCrawlerWorker) schedule(ctx context.Context) {
	if recoverExpiredSeedCrawlerRuns(ctx, worker.server.db) != nil {
		return
	}
	worker.scheduleDueRun(ctx)
	for count := 0; count < 4; count++ {
		processed, err := worker.processOne(ctx)
		if err != nil || !processed {
			return
		}
	}
}

func (worker *SeedCrawlerWorker) scheduleDueRun(ctx context.Context) {
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)
	var locked bool
	if err = tx.QueryRow(ctx, `select pg_try_advisory_xact_lock(hashtext('mcmods.seed_crawler.schedule'))`).Scan(&locked); err != nil || !locked {
		return
	}
	var enabled bool
	var intervalSeconds int
	var nextRunAt *time.Time
	if err = tx.QueryRow(ctx, `select enabled,interval_seconds,next_run_at from seed_crawler_configs where id for update`).Scan(&enabled, &intervalSeconds, &nextRunAt); err != nil || !enabled {
		return
	}
	now := time.Now().UTC()
	if nextRunAt != nil && nextRunAt.After(now) {
		return
	}
	var active bool
	if err = tx.QueryRow(ctx, `select exists(select 1 from seed_crawler_runs where status in ('pending','running'))`).Scan(&active); err != nil {
		return
	}
	if !active {
		if _, err = tx.Exec(ctx, `insert into seed_crawler_runs(dry_run) values(false)`); err != nil {
			return
		}
	}
	_, err = tx.Exec(ctx, `update seed_crawler_configs set next_run_at=$1 where id`, now.Add(time.Duration(intervalSeconds)*time.Second))
	if err == nil {
		_ = tx.Commit(ctx)
	}
}

type seedCrawlerRuntimeConfig struct {
	ProjectTypes       []string
	BatchSize          int
	DailyLimit         int
	MinimumDownloads   int64
	AIDailyTokenBudget int64
	AutoSubmitReview   bool
}

func (worker *SeedCrawlerWorker) processOne(ctx context.Context) (bool, error) {
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('mcmods.seed_crawler.claim'))`); err != nil {
		return false, err
	}
	var concurrency, running int
	if err = tx.QueryRow(ctx, `select max_concurrency from seed_crawler_configs where id`).Scan(&concurrency); err != nil {
		return false, err
	}
	if err = tx.QueryRow(ctx, `select count(*) from seed_crawler_runs where status='running' and lease_expires_at>clock_timestamp()`).Scan(&running); err != nil {
		return false, err
	}
	if running >= max(1, min(concurrency, 16)) {
		return false, nil
	}
	var runID int64
	var dryRun bool
	err = tx.QueryRow(ctx, `select id,dry_run from seed_crawler_runs
		where status='pending' and stats->>'kind' is distinct from 'translation_recovery' and next_attempt_at<=now() order by created_at,id for update skip locked limit 1`).Scan(&runID, &dryRun)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var tokenBytes [24]byte
	if _, err = cryptorand.Read(tokenBytes[:]); err != nil {
		return false, err
	}
	token := hex.EncodeToString(tokenBytes[:])
	if _, err = tx.Exec(ctx, `update seed_crawler_runs set status='running',lease_owner=$2,lease_expires_at=now()+interval '5 minutes',attempts=attempts+1,started_at=coalesce(started_at,now()) where id=$1`, runID, token); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	ctx, stopLease := maintainSeedCrawlerLease(ctx, worker.server.db, runID, token)
	defer stopLease()
	actor, actorErr := worker.server.automationActor(ctx)
	if actorErr != nil {
		return true, worker.failRun(ctx, runID, actorErr)
	}
	if _, actorErr = worker.server.db.Exec(ctx, `update seed_crawler_runs set actor_id=$2 where id=$1 and status='running' and lease_owner=$3 and lease_expires_at>clock_timestamp()`, runID, actor.Subject, token); actorErr != nil {
		return true, worker.failRun(ctx, runID, actorErr)
	}
	stats, runErr := worker.executeRun(ctx, runID, actor, dryRun)
	statsRaw, _ := json.Marshal(stats)
	if runErr != nil {
		return true, worker.failRunWithStats(ctx, runID, runErr, statsRaw)
	}
	tx, err = worker.server.db.Begin(ctx)
	if err != nil {
		return true, err
	}
	defer tx.Rollback(ctx)
	if err = lockSeedCrawlerLeaseTx(ctx, tx); err != nil {
		return true, err
	}
	if _, err = tx.Exec(ctx, `update seed_crawler_runs set status='completed',stats=$2::jsonb,last_error='',lease_owner='',lease_expires_at=null,finished_at=now() where id=$1`, runID, statsRaw); err != nil {
		return true, err
	}
	if _, err = tx.Exec(ctx, `update seed_crawler_configs set last_run_at=now(),
		next_run_at=case when enabled then now()+make_interval(secs=>interval_seconds) else null end where id`); err != nil {
		return true, err
	}
	err = tx.Commit(ctx)
	return true, err
}

func (worker *SeedCrawlerWorker) failRun(ctx context.Context, runID int64, err error) error {
	return worker.failRunWithStats(ctx, runID, err, []byte(`{}`))
}

func (worker *SeedCrawlerWorker) failRunWithStats(ctx context.Context, runID int64, runErr error, stats []byte) error {
	_, err := worker.server.db.Exec(ctx, `update seed_crawler_runs set status=case when attempts>=5 then 'failed' else 'pending' end,
		stats=$2::jsonb,last_error=$3,lease_owner='',lease_expires_at=null,next_attempt_at=now()+make_interval(secs=>least(3600,(30*power(2,least(attempts,6)))::integer)),
		finished_at=case when attempts>=5 then now() else null end where id=$1 and status='running' and lease_owner=$4 and lease_expires_at>clock_timestamp()`, runID, stats, truncateRunes(runErr.Error(), 2000), seedCrawlerLeaseToken(ctx))
	return err
}

func (worker *SeedCrawlerWorker) executeRun(ctx context.Context, runID int64, actor security.Claims, dryRun bool) (map[string]int, error) {
	var cfg seedCrawlerRuntimeConfig
	err := worker.server.db.QueryRow(ctx, `select project_types,batch_size,daily_limit,minimum_downloads,ai_daily_token_budget,auto_submit_review from seed_crawler_configs where id`).Scan(
		&cfg.ProjectTypes, &cfg.BatchSize, &cfg.DailyLimit, &cfg.MinimumDownloads, &cfg.AIDailyTokenBudget, &cfg.AutoSubmitReview)
	if err != nil {
		return nil, err
	}
	var importedToday int
	if err = worker.server.db.QueryRow(ctx, `select count(*) from seed_crawler_candidates where status in ('draft','submitted') and updated_at>=date_trunc('day',now() at time zone 'UTC') at time zone 'UTC'`).Scan(&importedToday); err != nil {
		return nil, err
	}
	remaining := max(0, cfg.DailyLimit-importedToday)
	stats := map[string]int{"discovered": 0, "eligible": 0, "duplicates": 0, "drafts": 0, "submitted": 0, "failed": 0}
	if remaining == 0 {
		return stats, nil
	}
	importCfg, err := worker.server.modImportConfigFromSettings(ctx)
	if err != nil {
		return stats, err
	}
	if err = ensureModImportProviderAvailable(importCfg, "modrinth"); err != nil {
		return stats, err
	}
	client, err := newProviderHTTPClient(time.Duration(importCfg.RequestTimeoutSeconds)*time.Second, importCfg.Modrinth.BaseURL)
	if err != nil {
		return stats, err
	}
	for _, projectType := range cfg.ProjectTypes {
		if remaining <= 0 {
			break
		}
		hits, fetchErr := fetchSeedModrinthProjects(ctx, client, importCfg, projectType, min(cfg.BatchSize, remaining))
		if fetchErr != nil {
			stats["failed"]++
			continue
		}
		stats["discovered"] += len(hits)
		for _, hit := range hits {
			if !seedCrawlerDownloadEligible(hit.Downloads, cfg.MinimumDownloads) || remaining <= 0 {
				continue
			}
			stats["eligible"]++
			candidateID, claimStatus, claimErr := worker.claimSeedCandidate(ctx, runID, projectType, hit, dryRun)
			if claimErr != nil {
				return stats, claimErr
			}
			if claimStatus == "limit" {
				return stats, nil
			}
			if claimStatus == "duplicate" {
				stats["duplicates"]++
				continue
			}
			if dryRun {
				continue
			}
			status, processErr := worker.createSeedDraft(ctx, candidateID, actor, projectType, hit, cfg)
			if processErr != nil {
				stats["failed"]++
				_, _ = worker.execSeedWrite(ctx, `update seed_crawler_candidates set status=case when status='processing' then 'failed' else status end,last_error=$2,updated_at=now() where id=$1`, candidateID, truncateRunes(processErr.Error(), 2000))
				continue
			}
			stats[status]++
			remaining--
		}
	}
	return stats, nil
}

type seedModrinthHit struct {
	ProjectID   string `json:"projectId"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Downloads   int64  `json:"downloads"`
}

type seedModrinthSearchResponse struct {
	Hits []struct {
		ProjectID   string `json:"project_id"`
		Slug        string `json:"slug"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Downloads   int64  `json:"downloads"`
	} `json:"hits"`
	TotalHits int `json:"total_hits"`
}

func fetchSeedModrinthProjects(ctx context.Context, client *http.Client, cfg modImportConfig, projectType string, limit int) ([]seedModrinthHit, error) {
	providerType := map[string]string{"mod": "mod", "plugin": "plugin", "shader_pack": "shader", "resource_pack": "resourcepack"}[projectType]
	if providerType == "" {
		return nil, errors.New("unsupported seed project type")
	}
	if limit < 1 {
		return nil, nil
	}
	headers := providerHeaders(cfg.UserAgent, cfg.Modrinth.Token, "")
	var probe seedModrinthSearchResponse
	if err := getProviderJSON(ctx, client, seedModrinthSearchURL(cfg.Modrinth.BaseURL, providerType, 1, 0), headers, &probe); err != nil {
		return nil, err
	}
	if probe.TotalHits < 1 {
		return nil, nil
	}
	limit = min(limit, probe.TotalHits)
	offset, err := randomSeedCrawlerOffset(probe.TotalHits, limit)
	if err != nil {
		return nil, fmt.Errorf("select random Modrinth candidates: %w", err)
	}
	var response seedModrinthSearchResponse
	if err = getProviderJSON(ctx, client, seedModrinthSearchURL(cfg.Modrinth.BaseURL, providerType, limit, offset), headers, &response); err != nil {
		return nil, err
	}
	result := make([]seedModrinthHit, 0, len(response.Hits))
	for _, hit := range response.Hits {
		result = append(result, seedModrinthHit{ProjectID: hit.ProjectID, Slug: hit.Slug, Title: hit.Title, Description: hit.Description, Downloads: hit.Downloads})
	}
	return result, nil
}

func seedModrinthSearchURL(baseURL, providerType string, limit, offset int) string {
	endpoint, _ := url.Parse(strings.TrimRight(baseURL, "/") + "/search")
	query := endpoint.Query()
	query.Set("limit", fmt.Sprint(limit))
	query.Set("offset", fmt.Sprint(offset))
	query.Set("index", "downloads")
	facets, _ := json.Marshal([][]string{{"project_type:" + providerType}})
	query.Set("facets", string(facets))
	endpoint.RawQuery = query.Encode()
	return endpoint.String()
}

func seedCrawlerDownloadEligible(downloads, minimum int64) bool {
	return downloads > minimum
}

func randomSeedCrawlerOffset(total, limit int) (int, error) {
	maximum := total - limit
	if maximum <= 0 {
		return 0, nil
	}
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(maximum+1)))
	if err != nil {
		return 0, err
	}
	return int(value.Int64()), nil
}

func seedModrinthURL(projectType, slug string) string {
	section := map[string]string{"mod": "mod", "plugin": "plugin", "shader_pack": "shader", "resource_pack": "resourcepack"}[projectType]
	return "https://modrinth.com/" + section + "/" + slug
}

func (worker *SeedCrawlerWorker) createSeedDraft(ctx context.Context, candidateID int64, actor security.Claims, projectType string, hit seedModrinthHit, cfg seedCrawlerRuntimeConfig) (string, error) {
	var exists bool
	if projectType == "mod" {
		if err := worker.server.db.QueryRow(ctx, `select exists(select 1 from mods where modrinth_project_id=$1)`, hit.ProjectID).Scan(&exists); err != nil {
			return "", err
		}
	} else {
		if err := worker.server.db.QueryRow(ctx, `select exists(select 1 from simple_projects where project_type=$1 and modrinth_project_id=$2)`, projectType, hit.ProjectID).Scan(&exists); err != nil {
			return "", err
		}
	}
	if exists {
		_, err := worker.execSeedWrite(ctx, `update seed_crawler_candidates set status='existing',last_error='',updated_at=now() where id=$1`, candidateID)
		return "duplicates", err
	}
	var jobID string
	sourceURL := seedModrinthURL(projectType, hit.Slug)
	jobTx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer jobTx.Rollback(ctx)
	if err = lockSeedCrawlerLeaseTx(ctx, jobTx); err != nil {
		return "", err
	}
	err = jobTx.QueryRow(ctx, `insert into mod_metadata_import_jobs(user_id,project_type,provider,source_url,status,progress)
		values($1,$2,'modrinth',$3,'queued',0) returning public_id`, actor.Subject, projectType, sourceURL).Scan(&jobID)
	if err == nil {
		err = jobTx.Commit(ctx)
	}
	if err != nil {
		return "", err
	}
	if err = worker.server.runModMetadataImport(ctx, jobID); err != nil {
		return "", err
	}
	var status string
	var result []byte
	var importError string
	err = worker.server.db.QueryRow(ctx, `select status,coalesce(result,'{}'::jsonb),error from mod_metadata_import_jobs where public_id=$1`, jobID).Scan(&status, &result, &importError)
	if err != nil || status != "completed" {
		if importError == "" {
			importError = "metadata import did not complete"
		}
		return "", errors.New(importError)
	}
	var payload map[string]any
	if json.Unmarshal(result, &payload) != nil {
		return "", errors.New("metadata importer returned invalid draft JSON")
	}
	payload["importOrigin"] = "seed_crawler_import"
	payload["externalProjectId"] = hit.ProjectID
	translations := worker.translateSeedDraft(ctx, candidateID, jobID, result, cfg.AIDailyTokenBudget)
	applySeedDraftTranslations(payload, translations, projectType)
	payload["seedTranslations"] = translations
	finalPayload, _ := json.Marshal(payload)
	editURL := "/" + strings.ReplaceAll(projectType, "_pack", "s") + "/new"
	if projectType == "mod" {
		editURL = "/mods/new"
	} else if projectType == "plugin" {
		editURL = "/plugins/new"
	} else if projectType == "shader_pack" {
		editURL = "/shaders/new"
	} else if projectType == "resource_pack" {
		editURL = "/resource-packs/new"
	}
	var draftID string
	draftTx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer draftTx.Rollback(ctx)
	if err = lockSeedCrawlerLeaseTx(ctx, draftTx); err != nil {
		return "", err
	}
	err = draftTx.QueryRow(ctx, `insert into user_drafts(user_id,draft_key,project_key,project_title,kind,title,edit_url,payload,expires_at)
		values($1,$2,$3,$4,'seed_crawler_import',$4,$5,$6::jsonb,now()+interval '3 years')
		on conflict(user_id,draft_key) where submitted_at is null do update set payload=excluded.payload,project_title=excluded.project_title,title=excluded.title,updated_at=now(),expires_at=excluded.expires_at returning public_id`,
		actor.Subject, "seed-crawler:"+hit.ProjectID, projectType+":"+hit.ProjectID, hit.Title, editURL, finalPayload).Scan(&draftID)
	if err == nil {
		_, err = draftTx.Exec(ctx, `update seed_crawler_candidates set status='draft',payload=payload||jsonb_build_object('draftPublicId',$2::text),last_error='',updated_at=now() where id=$1`, candidateID, draftID)
	}
	if err == nil {
		err = draftTx.Commit(ctx)
	}
	if err != nil {
		return "", err
	}
	nextStatus := "draft"
	if cfg.AutoSubmitReview {
		submittedStatus, submitErr := worker.submitSeedDraft(ctx, actor, projectType, hit.ProjectID, sourceURL, seedSubmissionPayload(payload))
		if submitErr != nil {
			return "", submitErr
		}
		nextStatus = "submitted"
		if _, err = worker.execSeedWrite(ctx, `update user_drafts set submitted_status=$2,submitted_at=now(),updated_at=now() where public_id=$1`, draftID, submittedStatus); err != nil {
			return "", err
		}
	}
	_, err = worker.execSeedWrite(ctx, `update seed_crawler_candidates set status=$2,last_error='',updated_at=now() where id=$1`, candidateID, nextStatus)
	return nextStatus, err
}

func (worker *SeedCrawlerWorker) submitSeedDraft(ctx context.Context, actor security.Claims, projectType, externalProjectID, externalURL string, raw []byte) (string, error) {
	request := httptest.NewRequest(http.MethodPost, "/internal/seed-crawler", bytes.NewReader(raw)).WithContext(
		context.WithValue(ctx, claimsContextKey, actor),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	if projectType == "mod" {
		worker.server.createMod(response, request)
	} else {
		worker.server.createSimpleProject(response, request, projectType)
	}
	if response.Code < 200 || response.Code >= 300 {
		return "", fmt.Errorf("seed draft submission failed (%d): %s", response.Code, truncateRunes(response.Body.String(), 500))
	}
	var created struct {
		Data struct {
			ReviewStatus string `json:"reviewStatus"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		return "", fmt.Errorf("decode seed draft submission: %w", err)
	}
	if created.Data.ReviewStatus != "approved" && created.Data.ReviewStatus != "pending" {
		return "", fmt.Errorf("seed draft submission returned invalid review status %q", created.Data.ReviewStatus)
	}
	// The normal importer has already verified this external identifier. Bind
	// it immediately so later automatic updates can never guess by project
	// name. The project creation and public-route triggers are committed by the
	// handler before this relationship is written.
	var routeID int64
	var publicID string
	if projectType == "mod" {
		err := worker.server.db.QueryRow(ctx, `select route.id,route.public_id from mods project
			join public_routes route on route.entity_type='mod' and route.internal_id=project.id
			where project.modrinth_project_id=$1`, externalProjectID).Scan(&routeID, &publicID)
		if err == nil {
			_, err = worker.execSeedWrite(ctx, `insert into project_external_sources(project_route_id,source_type,external_project_id,external_project_url,verified_at,verified_by)
				values($1,'modrinth',$2,$3,now(),$4) on conflict(project_route_id,source_type) do update set
				external_project_id=excluded.external_project_id,external_project_url=excluded.external_project_url,verified_at=now(),verified_by=excluded.verified_by`,
				routeID, externalProjectID, externalURL, actor.Subject)
		}
		if err != nil {
			return "", err
		}
	} else {
		err := worker.server.db.QueryRow(ctx, `select route.id,route.public_id from simple_projects project
			join public_routes route on route.entity_type=project.project_type and route.internal_id=project.id
			where project.project_type=$1 and project.modrinth_project_id=$2`, projectType, externalProjectID).Scan(&routeID, &publicID)
		if err == nil {
			_, err = worker.execSeedWrite(ctx, `insert into project_external_sources(project_route_id,source_type,external_project_id,external_project_url,verified_at,verified_by)
				values($1,'modrinth',$2,$3,now(),$4) on conflict(project_route_id,source_type) do update set
				external_project_id=excluded.external_project_id,external_project_url=excluded.external_project_url,verified_at=now(),verified_by=excluded.verified_by`,
				routeID, externalProjectID, externalURL, actor.Subject)
		}
		if err != nil {
			return "", err
		}
	}
	return created.Data.ReviewStatus, nil
}

// Reserve a candidate and its daily slot together. Finished candidates retain
// their original day; another run cannot replace a live candidate's ownership.
func (worker *SeedCrawlerWorker) claimSeedCandidate(ctx context.Context, runID int64, projectType string, hit seedModrinthHit, dryRun bool) (int64, string, error) {
	lease, ok := ctx.Value(seedCrawlerLeaseContextKey{}).(seedCrawlerLeaseIdentity)
	if !ok || lease.RunID != runID {
		return 0, "", errSeedCrawlerLeaseLost
	}
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback(ctx)
	if err = lockSeedCrawlerLeaseTx(ctx, tx); err != nil {
		return 0, "", err
	}
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('mcmods.seed_crawler.candidate_day'))`); err != nil {
		return 0, "", err
	}
	payload, err := json.Marshal(hit)
	if err != nil {
		return 0, "", err
	}
	if _, err = tx.Exec(ctx, `insert into seed_crawler_candidates(run_id,external_project_id,project_type,downloads,payload) values($1,$2,$3,$4,$5::jsonb) on conflict(external_project_id) do nothing`, runID, hit.ProjectID, projectType, hit.Downloads, payload); err != nil {
		return 0, "", err
	}
	var id int64
	var status string
	var busy bool
	if err = tx.QueryRow(ctx, `select c.id,c.status,exists(select 1 from seed_crawler_runs r where r.id=c.run_id and r.status='running' and r.lease_expires_at>clock_timestamp()) from seed_crawler_candidates c where external_project_id=$1 for update`, hit.ProjectID).Scan(&id, &status, &busy); err != nil {
		return 0, "", err
	}
	if status == "draft" || status == "submitted" || status == "existing" || (status == "processing" && busy) {
		return id, "duplicate", tx.Commit(ctx)
	}
	if !dryRun {
		var dailyLimit, consumed int
		if err = tx.QueryRow(ctx, `select daily_limit from seed_crawler_configs where id for share`).Scan(&dailyLimit); err != nil {
			return 0, "", err
		}
		if err = tx.QueryRow(ctx, `select count(*) from seed_crawler_candidates c where c.updated_at>=date_trunc('day',clock_timestamp() at time zone 'UTC') at time zone 'UTC' and (c.status in ('draft','submitted') or (c.status='processing' and exists(select 1 from seed_crawler_runs r where r.id=c.run_id and r.status='running' and r.lease_expires_at>clock_timestamp())))`).Scan(&consumed); err != nil {
			return 0, "", err
		}
		if consumed >= dailyLimit {
			return id, "limit", tx.Commit(ctx)
		}
		status = "processing"
	} else {
		status = "candidate"
	}
	if _, err = tx.Exec(ctx, `update seed_crawler_candidates set run_id=$2,status=$3,project_type=$4,downloads=$5,payload=$6::jsonb,last_error='',updated_at=now() where id=$1`, id, runID, status, projectType, hit.Downloads, payload); err != nil {
		return 0, "", err
	}
	return id, "claimed", tx.Commit(ctx)
}
