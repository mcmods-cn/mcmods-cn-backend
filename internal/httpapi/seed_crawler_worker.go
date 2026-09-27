package httpapi

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
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
	server            *Server
	leaseDuration     time.Duration
	heartbeatInterval time.Duration
}

func NewSeedCrawlerWorker(cfg config.Config, db *pgxpool.Pool) *SeedCrawlerWorker {
	return &SeedCrawlerWorker{
		server:            &Server{cfg: cfg, db: db},
		leaseDuration:     5 * time.Minute,
		heartbeatInterval: time.Minute,
	}
}

var errSeedCrawlerLeaseOwnershipLost = errors.New("seed crawler lease ownership lost")

func (worker *SeedCrawlerWorker) Start(ctx context.Context) {
	if worker == nil || worker.server == nil || worker.server.db == nil {
		return
	}
	go worker.run(ctx)
}

func (worker *SeedCrawlerWorker) run(ctx context.Context) {
	if err := worker.schedule(ctx); err != nil {
		log.Printf("seed crawler schedule: %v", err)
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := worker.schedule(ctx); err != nil {
				log.Printf("seed crawler schedule: %v", err)
			}
		}
	}
}

func (worker *SeedCrawlerWorker) schedule(ctx context.Context) error {
	if _, err := worker.server.db.Exec(ctx, `update seed_crawler_runs set status='pending',lease_owner='',lease_expires_at=null,
		next_attempt_at=now(),last_error=case when last_error='' then 'worker lease expired' else last_error end
		where status='running' and lease_expires_at<now()`); err != nil {
		return fmt.Errorf("recover expired seed crawler leases: %w", err)
	}
	if err := worker.scheduleDueRun(ctx); err != nil {
		return err
	}
	maxConcurrency, err := worker.maxConcurrency(ctx)
	if err != nil {
		return err
	}
	batchSize := max(4, maxConcurrency)
	jobs := make(chan struct{}, batchSize)
	for range batchSize {
		jobs <- struct{}{}
	}
	close(jobs)
	results := make(chan error, maxConcurrency)
	for range maxConcurrency {
		go func() {
			for range jobs {
				processed, processErr := worker.processOne(ctx)
				if processErr != nil || !processed {
					results <- processErr
					return
				}
			}
			results <- nil
		}()
	}
	processErrors := make([]error, 0, maxConcurrency)
	for range maxConcurrency {
		if processErr := <-results; processErr != nil {
			processErrors = append(processErrors, fmt.Errorf("process seed crawler run: %w", processErr))
		}
	}
	return errors.Join(processErrors...)
}

func (worker *SeedCrawlerWorker) maxConcurrency(ctx context.Context) (int, error) {
	var value int
	if err := worker.server.db.QueryRow(ctx, `select max_concurrency from seed_crawler_configs where id`).Scan(&value); err != nil {
		return 0, fmt.Errorf("read seed crawler concurrency: %w", err)
	}
	if value < 1 || value > 16 {
		return 0, fmt.Errorf("seed crawler concurrency %d is outside the supported range", value)
	}
	return value, nil
}

func (worker *SeedCrawlerWorker) scheduleDueRun(ctx context.Context) error {
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin seed crawler schedule: %w", err)
	}
	defer tx.Rollback(ctx)
	var locked bool
	if err = tx.QueryRow(ctx, `select pg_try_advisory_xact_lock(hashtext('mcmods.seed_crawler.schedule'))`).Scan(&locked); err != nil {
		return fmt.Errorf("lock seed crawler schedule: %w", err)
	}
	if !locked {
		return nil
	}
	var enabled bool
	var intervalSeconds int
	var nextRunAt *time.Time
	if err = tx.QueryRow(ctx, `select enabled,interval_seconds,next_run_at from seed_crawler_configs where id for update`).Scan(&enabled, &intervalSeconds, &nextRunAt); err != nil {
		return fmt.Errorf("read seed crawler schedule config: %w", err)
	}
	if !enabled {
		return nil
	}
	now := time.Now().UTC()
	if nextRunAt != nil && nextRunAt.After(now) {
		return nil
	}
	var active bool
	if err = tx.QueryRow(ctx, `select exists(select 1 from seed_crawler_runs where status in ('pending','running'))`).Scan(&active); err != nil {
		return fmt.Errorf("check active seed crawler run: %w", err)
	}
	if !active {
		if _, err = tx.Exec(ctx, `insert into seed_crawler_runs(dry_run) values(false)`); err != nil {
			return fmt.Errorf("schedule seed crawler run: %w", err)
		}
	}
	if _, err = tx.Exec(ctx, `update seed_crawler_configs set next_run_at=$1 where id`, now.Add(time.Duration(intervalSeconds)*time.Second)); err != nil {
		return fmt.Errorf("advance seed crawler schedule: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit seed crawler schedule: %w", err)
	}
	return nil
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
		return false, fmt.Errorf("lock seed crawler claim: %w", err)
	}
	var maxConcurrency, activeRuns int
	if err = tx.QueryRow(ctx, `select max_concurrency from seed_crawler_configs where id`).Scan(&maxConcurrency); err != nil {
		return false, fmt.Errorf("read seed crawler claim concurrency: %w", err)
	}
	if maxConcurrency < 1 || maxConcurrency > 16 {
		return false, fmt.Errorf("seed crawler concurrency %d is outside the supported range", maxConcurrency)
	}
	if err = tx.QueryRow(ctx, `select count(*)::int from seed_crawler_runs
		where status='running' and lease_expires_at>now()`).Scan(&activeRuns); err != nil {
		return false, fmt.Errorf("count active seed crawler runs: %w", err)
	}
	if activeRuns >= maxConcurrency {
		return false, nil
	}
	var runID int64
	var dryRun bool
	err = tx.QueryRow(ctx, `select id,dry_run from seed_crawler_runs
		where status='pending' and next_attempt_at<=now() order by created_at,id for update skip locked limit 1`).Scan(&runID, &dryRun)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	leaseOwner := "seed-worker:" + randomHex(16)
	if _, err = tx.Exec(ctx, `update seed_crawler_runs set status='running',lease_owner=$2,
		lease_expires_at=now()+$3::interval,attempts=attempts+1,started_at=coalesce(started_at,now()) where id=$1`,
		runID, leaseOwner, worker.effectiveLeaseDuration().String()); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	runContext, stopHeartbeat := worker.startLeaseHeartbeat(ctx, runID, leaseOwner)
	actor, actorErr := worker.server.automationActor(runContext)
	if actorErr != nil {
		heartbeatErr := stopHeartbeat()
		if heartbeatErr != nil {
			actorErr = errors.Join(actorErr, heartbeatErr)
		}
		if errors.Is(actorErr, errSeedCrawlerLeaseOwnershipLost) {
			return true, actorErr
		}
		return true, worker.failRun(ctx, runID, leaseOwner, actorErr)
	}
	command, actorErr := worker.server.db.Exec(runContext, `update seed_crawler_runs set actor_id=$2
		where id=$1 and status='running' and lease_owner=$3`, runID, actor.Subject, leaseOwner)
	if actorErr == nil && command.RowsAffected() != 1 {
		actorErr = errSeedCrawlerLeaseOwnershipLost
	}
	if actorErr != nil {
		heartbeatErr := stopHeartbeat()
		if heartbeatErr != nil {
			actorErr = errors.Join(actorErr, heartbeatErr)
		}
		if errors.Is(actorErr, errSeedCrawlerLeaseOwnershipLost) {
			return true, actorErr
		}
		return true, worker.failRun(ctx, runID, leaseOwner, actorErr)
	}
	stats, runErr := worker.executeRun(runContext, runID, actor, dryRun)
	statsRaw, _ := json.Marshal(stats)
	if heartbeatErr := stopHeartbeat(); heartbeatErr != nil {
		if errors.Is(heartbeatErr, errSeedCrawlerLeaseOwnershipLost) {
			return true, heartbeatErr
		}
		runErr = errors.Join(runErr, heartbeatErr)
	}
	if runErr != nil {
		return true, worker.failRunWithStats(ctx, runID, leaseOwner, runErr, statsRaw)
	}
	tx, err = worker.server.db.Begin(ctx)
	if err != nil {
		return true, err
	}
	defer tx.Rollback(ctx)
	command, err = tx.Exec(ctx, `update seed_crawler_runs set status='completed',stats=$2::jsonb,last_error='',
		lease_owner='',lease_expires_at=null,finished_at=now()
		where id=$1 and status='running' and lease_owner=$3`, runID, statsRaw, leaseOwner)
	if err != nil {
		return true, err
	}
	if command.RowsAffected() != 1 {
		return true, errSeedCrawlerLeaseOwnershipLost
	}
	if _, err = tx.Exec(ctx, `update seed_crawler_configs set last_run_at=now(),
		next_run_at=case when enabled then now()+make_interval(secs=>interval_seconds) else null end where id`); err != nil {
		return true, err
	}
	err = tx.Commit(ctx)
	return true, err
}

func (worker *SeedCrawlerWorker) effectiveLeaseDuration() time.Duration {
	if worker.leaseDuration <= 0 {
		return 5 * time.Minute
	}
	return worker.leaseDuration
}

func (worker *SeedCrawlerWorker) effectiveHeartbeatInterval() time.Duration {
	if worker.heartbeatInterval <= 0 {
		return time.Minute
	}
	return worker.heartbeatInterval
}

func (worker *SeedCrawlerWorker) startLeaseHeartbeat(ctx context.Context, runID int64, leaseOwner string) (context.Context, func() error) {
	runContext, cancelRun := context.WithCancel(ctx)
	heartbeatContext, cancelHeartbeat := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(worker.effectiveHeartbeatInterval())
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatContext.Done():
				done <- nil
				return
			case <-ticker.C:
				command, err := worker.server.db.Exec(heartbeatContext, `update seed_crawler_runs
					set lease_expires_at=now()+$3::interval
					where id=$1 and status='running' and lease_owner=$2`,
					runID, leaseOwner, worker.effectiveLeaseDuration().String())
				if err == nil && command.RowsAffected() != 1 {
					err = errSeedCrawlerLeaseOwnershipLost
				}
				if err != nil {
					cancelRun()
					done <- fmt.Errorf("renew seed crawler lease: %w", err)
					return
				}
			}
		}
	}()
	return runContext, func() error {
		cancelHeartbeat()
		err := <-done
		cancelRun()
		return err
	}
}

func (worker *SeedCrawlerWorker) failRun(ctx context.Context, runID int64, leaseOwner string, err error) error {
	return worker.failRunWithStats(ctx, runID, leaseOwner, err, []byte(`{}`))
}

func (worker *SeedCrawlerWorker) failRunWithStats(ctx context.Context, runID int64, leaseOwner string, runErr error, stats []byte) error {
	command, err := worker.server.db.Exec(ctx, `update seed_crawler_runs set status=case when attempts>=5 then 'failed' else 'pending' end,
		stats=$2::jsonb,last_error=$3,lease_owner='',lease_expires_at=null,next_attempt_at=now()+make_interval(secs=>least(3600,(30*power(2,least(attempts,6)))::integer)),
		finished_at=case when attempts>=5 then now() else null end
		where id=$1 and status='running' and lease_owner=$4`, runID, stats, truncateRunes(runErr.Error(), 2000), leaseOwner)
	if err == nil && command.RowsAffected() != 1 {
		return errSeedCrawlerLeaseOwnershipLost
	}
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
	if err = worker.server.db.QueryRow(ctx, `select count(*) from seed_crawler_candidates where status in ('draft','submitted') and updated_at>=current_date`).Scan(&importedToday); err != nil {
		return nil, fmt.Errorf("count today's seed crawler imports: %w", err)
	}
	remaining := max(0, cfg.DailyLimit-importedToday)
	stats := map[string]int{
		"discovered": 0, "eligible": 0, "duplicates": 0, "drafts": 0, "submitted": 0, "failed": 0,
		"providerSucceeded": 0, "providerFailed": 0,
	}
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
	fetchErrors := make([]error, 0, len(cfg.ProjectTypes))
	for _, projectType := range cfg.ProjectTypes {
		if remaining <= 0 {
			break
		}
		hits, fetchErr := fetchSeedModrinthProjects(ctx, client, importCfg, projectType, min(cfg.BatchSize, remaining))
		if fetchErr != nil {
			stats["failed"]++
			stats["providerFailed"]++
			fetchErrors = append(fetchErrors, fmt.Errorf("%s: %w", projectType, fetchErr))
			continue
		}
		stats["providerSucceeded"]++
		stats["discovered"] += len(hits)
		for _, hit := range hits {
			if !seedCrawlerDownloadEligible(hit.Downloads, cfg.MinimumDownloads) || remaining <= 0 {
				continue
			}
			stats["eligible"]++
			candidateID, currentStatus, upsertErr := worker.upsertSeedCrawlerCandidate(ctx, runID, projectType, hit)
			err = upsertErr
			if err != nil {
				stats["failed"]++
				continue
			}
			if currentStatus == "draft" || currentStatus == "submitted" || currentStatus == "existing" {
				stats["duplicates"]++
				continue
			}
			if dryRun {
				continue
			}
			status, processErr := worker.createSeedDraft(ctx, candidateID, actor, projectType, hit, cfg)
			if processErr != nil {
				stats["failed"]++
				if _, updateErr := worker.server.db.Exec(ctx, `update seed_crawler_candidates set status='failed',last_error=$2,updated_at=now()
					where id=$1 and status not in ('submitted','existing')`, candidateID, truncateRunes(processErr.Error(), 2000)); updateErr != nil {
					return stats, errors.Join(processErr, fmt.Errorf("record failed seed crawler candidate %d: %w", candidateID, updateErr))
				}
				continue
			}
			stats[status]++
			remaining--
		}
	}
	if stats["providerSucceeded"] == 0 && stats["providerFailed"] > 0 {
		return stats, fmt.Errorf("all seed crawler provider categories failed: %w", errors.Join(fetchErrors...))
	}
	return stats, nil
}

func (worker *SeedCrawlerWorker) upsertSeedCrawlerCandidate(ctx context.Context, runID int64, projectType string, hit seedModrinthHit) (int64, string, error) {
	payload, err := json.Marshal(hit)
	if err != nil {
		return 0, "", fmt.Errorf("encode seed crawler candidate: %w", err)
	}
	var candidateID int64
	var currentStatus string
	err = worker.server.db.QueryRow(ctx, `insert into seed_crawler_candidates(first_seen_run_id,last_seen_run_id,external_project_id,project_type,downloads,payload)
		values($1,$1,$2,$3,$4,$5::jsonb) on conflict(external_project_id) do update set last_seen_run_id=excluded.last_seen_run_id,
		downloads=excluded.downloads,payload=excluded.payload,updated_at=now()
		returning id,status`, runID, hit.ProjectID, projectType, hit.Downloads, payload).Scan(&candidateID, &currentStatus)
	return candidateID, currentStatus, err
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
	headers := providerCredentialHeaders(cfg.UserAgent, "modrinth", cfg.Modrinth.BaseURL, bearerAuthorization(cfg.Modrinth.Token), "")
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
	var err error
	if projectType == "mod" {
		err = worker.server.db.QueryRow(ctx, `select exists(select 1 from mods where modrinth_project_id=$1)`, hit.ProjectID).Scan(&exists)
	} else {
		err = worker.server.db.QueryRow(ctx, `select exists(select 1 from simple_projects where project_type=$1 and modrinth_project_id=$2)`, projectType, hit.ProjectID).Scan(&exists)
	}
	if err != nil {
		return "", fmt.Errorf("check existing seed crawler project: %w", err)
	}
	if exists {
		if _, err = worker.server.db.Exec(ctx, `update seed_crawler_candidates set status='existing',last_error='',updated_at=now() where id=$1`, candidateID); err != nil {
			return "", fmt.Errorf("record existing seed crawler candidate: %w", err)
		}
		return "duplicates", nil
	}
	var jobID string
	sourceURL := seedModrinthURL(projectType, hit.Slug)
	err = worker.server.db.QueryRow(ctx, `insert into mod_metadata_import_jobs(user_id,project_type,provider,source_url,status,progress)
		values($1,$2,'modrinth',$3,'queued',0) returning public_id`, actor.Subject, projectType, sourceURL).Scan(&jobID)
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
	translations, translateErr := worker.translateSeedDraft(ctx, candidateID, result, cfg.AIDailyTokenBudget)
	if translateErr != nil {
		return "", translateErr
	}
	localizedResult, err := applySeedDraftTranslations(projectType, result, translations)
	if err != nil {
		return "", fmt.Errorf("apply seed crawler translations: %w", err)
	}
	if err = json.Unmarshal(localizedResult, &payload); err != nil {
		return "", fmt.Errorf("decode localized seed crawler draft: %w", err)
	}
	payload["importOrigin"] = "seed_crawler_import"
	payload["externalProjectId"] = hit.ProjectID
	finalPayload, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode seed crawler draft: %w", err)
	}
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
	nextStatus := "draft"
	if cfg.AutoSubmitReview {
		nextStatus = "submitting"
	}
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin seed crawler draft stage: %w", err)
	}
	defer tx.Rollback(ctx)
	var draftID string
	err = tx.QueryRow(ctx, `insert into user_drafts(user_id,draft_key,project_key,project_title,kind,title,edit_url,payload,expires_at)
		values($1,$2,$3,$4,'seed_crawler_import',$4,$5,$6::jsonb,now()+interval '3 years')
		on conflict(user_id,draft_key) where submitted_at is null do update set payload=excluded.payload,project_title=excluded.project_title,title=excluded.title,updated_at=now(),expires_at=excluded.expires_at returning public_id`,
		actor.Subject, "seed-crawler:"+hit.ProjectID, projectType+":"+hit.ProjectID, hit.Title, editURL, finalPayload).Scan(&draftID)
	if err != nil {
		return "", fmt.Errorf("save seed crawler draft stage: %w", err)
	}
	command, err := tx.Exec(ctx, `update seed_crawler_candidates set status=$2,last_error='',updated_at=now() where id=$1`, candidateID, nextStatus)
	if err != nil {
		return "", fmt.Errorf("record seed crawler draft stage: %w", err)
	}
	if command.RowsAffected() != 1 {
		return "", errors.New("seed crawler candidate disappeared while saving draft")
	}
	if err = tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit seed crawler draft stage: %w", err)
	}
	if !cfg.AutoSubmitReview {
		return nextStatus, nil
	}
	if _, err = worker.submitSeedDraft(ctx, candidateID, draftID, actor, projectType, hit.ProjectID, sourceURL, localizedResult); err != nil {
		return "", err
	}
	return "submitted", nil
}

func (worker *SeedCrawlerWorker) translateSeedDraft(ctx context.Context, candidateID int64, raw []byte, budget int64) (map[string]any, error) {
	sourceLocale, items, err := seedDraftTranslationSource(raw)
	if err != nil {
		return nil, err
	}
	translations := map[string]any{sourceLocale: map[string]any{"source": true}}
	locales := make([]string, 0, len(supportedEditableContentLocales)-1)
	for _, locale := range supportedContentLocaleList() {
		if locale != sourceLocale {
			locales = append(locales, locale)
		}
	}
	if budget <= 0 {
		return translations, nil
	}
	if len(items) == 0 {
		return translations, nil
	}
	aiCfg := aiConfigFromDatabase(ctx, worker.server.db, worker.server.cfg.SettingsEncryptionKey)
	binding, ok := findAITaskModel(aiCfg.TaskModels, aiTaskContentTranslation)
	if !ok || strings.TrimSpace(binding.ModelKey) == "" {
		return translations, nil
	}
	providerCode, modelID, ok := strings.Cut(binding.ModelKey, "/")
	if !ok {
		return translations, nil
	}
	aiWorker := NewAIWorker(worker.server.db, nil, worker.server.cfg.SettingsEncryptionKey)
	for _, locale := range locales {
		payload, err := json.Marshal(map[string]any{"sourceLocale": sourceLocale, "targetLocale": locale, "items": items})
		if err != nil {
			return nil, fmt.Errorf("encode seed crawler translation %s: %w", locale, err)
		}
		prepared, err := aiWorker.prepareTask(ctx, aiTaskContentTranslation, providerCode, modelID, payload)
		if err != nil {
			return nil, fmt.Errorf("prepare seed crawler translation %s: %w", locale, err)
		}
		reservation, allowed, err := worker.reserveSeedTranslationBudget(
			ctx, candidateID, locale, budget, prepared.reservationTokens,
		)
		if err != nil {
			return nil, fmt.Errorf("reserve seed crawler translation budget for %s: %w", locale, err)
		}
		if !allowed {
			continue
		}
		result, usage, err := aiWorker.executePreparedTask(ctx, prepared)
		if err != nil {
			if settleErr := worker.settleSeedTranslationBudget(ctx, reservation, "failed", usage, err.Error()); settleErr != nil {
				return nil, errors.Join(err, fmt.Errorf("settle failed seed crawler translation %s: %w", locale, settleErr))
			}
			continue
		}
		translated, validationErr := validateSeedDraftTranslationResult(result, items)
		if validationErr != nil {
			if settleErr := worker.settleSeedTranslationBudget(ctx, reservation, "failed", usage, validationErr.Error()); settleErr != nil {
				return nil, errors.Join(validationErr, fmt.Errorf("settle invalid seed crawler translation %s: %w", locale, settleErr))
			}
			continue
		}
		if err = worker.settleSeedTranslationBudget(ctx, reservation, "completed", usage, ""); err != nil {
			return nil, fmt.Errorf("complete seed crawler translation %s: %w", locale, err)
		}
		translations[locale] = translated
	}
	return translations, nil
}

type seedTranslationBudgetReservation struct {
	candidateID    int64
	locale         string
	usageDate      time.Time
	reservedTokens int64
}

func (worker *SeedCrawlerWorker) reserveSeedTranslationBudget(
	ctx context.Context,
	candidateID int64,
	locale string,
	budget int64,
	requestedTokens int64,
) (seedTranslationBudgetReservation, bool, error) {
	if worker == nil || worker.server == nil || worker.server.db == nil {
		return seedTranslationBudgetReservation{}, false, errors.New("seed crawler translation budget database is unavailable")
	}
	if candidateID <= 0 || strings.TrimSpace(locale) == "" || budget <= 0 || requestedTokens <= 0 {
		return seedTranslationBudgetReservation{}, false, nil
	}
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return seedTranslationBudgetReservation{}, false, fmt.Errorf("begin seed crawler translation budget reservation: %w", err)
	}
	defer tx.Rollback(ctx)
	var usageDate time.Time
	if err = tx.QueryRow(ctx, `select current_date`).Scan(&usageDate); err != nil {
		return seedTranslationBudgetReservation{}, false, fmt.Errorf("read seed crawler translation budget date: %w", err)
	}
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('seed-crawler-ai-budget:'||$1::date::text,0))`, usageDate); err != nil {
		return seedTranslationBudgetReservation{}, false, fmt.Errorf("lock seed crawler translation budget: %w", err)
	}
	var used int64
	if err = tx.QueryRow(ctx, `select coalesce(sum(input_tokens+output_tokens+quota_reserved_tokens),0)
		from seed_crawler_translation_tasks where usage_date=$1`, usageDate).Scan(&used); err != nil {
		return seedTranslationBudgetReservation{}, false, fmt.Errorf("read seed crawler translation budget: %w", err)
	}
	var existingDate time.Time
	var existingReserved int64
	err = tx.QueryRow(ctx, `select usage_date,quota_reserved_tokens
		from seed_crawler_translation_tasks where candidate_id=$1 and locale=$2`, candidateID, locale).
		Scan(&existingDate, &existingReserved)
	if err == nil {
		if existingDate.Equal(usageDate) && existingReserved > 0 {
			return seedTranslationBudgetReservation{}, false, nil
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return seedTranslationBudgetReservation{}, false, fmt.Errorf("read seed crawler translation reservation: %w", err)
	}
	if requestedTokens > budget || used > budget-requestedTokens {
		return seedTranslationBudgetReservation{}, false, nil
	}
	if _, err = tx.Exec(ctx, `insert into seed_crawler_translation_tasks(
		candidate_id,locale,status,attempts,input_tokens,output_tokens,usage_date,quota_reserved_tokens,last_error,updated_at)
		values($1,$2,'running',1,0,0,$3,$4,'',now())
		on conflict(candidate_id,locale) do update set status='running',attempts=seed_crawler_translation_tasks.attempts+1,
		input_tokens=case when seed_crawler_translation_tasks.usage_date=excluded.usage_date then seed_crawler_translation_tasks.input_tokens else 0 end,
		output_tokens=case when seed_crawler_translation_tasks.usage_date=excluded.usage_date then seed_crawler_translation_tasks.output_tokens else 0 end,
		usage_date=excluded.usage_date,quota_reserved_tokens=excluded.quota_reserved_tokens,
		last_error='',updated_at=now()`, candidateID, locale, usageDate, requestedTokens); err != nil {
		return seedTranslationBudgetReservation{}, false, fmt.Errorf("store seed crawler translation reservation: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return seedTranslationBudgetReservation{}, false, fmt.Errorf("commit seed crawler translation budget reservation: %w", err)
	}
	return seedTranslationBudgetReservation{
		candidateID: candidateID, locale: locale, usageDate: usageDate, reservedTokens: requestedTokens,
	}, true, nil
}

func (worker *SeedCrawlerWorker) settleSeedTranslationBudget(
	ctx context.Context,
	reservation seedTranslationBudgetReservation,
	status string,
	usage aiTaskUsage,
	lastError string,
) error {
	if worker == nil || worker.server == nil || worker.server.db == nil {
		return errors.New("seed crawler translation budget database is unavailable")
	}
	if status != "completed" && status != "failed" {
		return errors.New("seed crawler translation settlement status is invalid")
	}
	if reservation.candidateID <= 0 || reservation.locale == "" || reservation.usageDate.IsZero() || reservation.reservedTokens <= 0 {
		return errors.New("seed crawler translation reservation is invalid")
	}
	if usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.InputTokens > math.MaxInt64-usage.OutputTokens {
		return errors.New("AI provider returned invalid token usage")
	}
	actualTokens := usage.InputTokens + usage.OutputTokens
	if actualTokens > reservation.reservedTokens {
		return fmt.Errorf("AI provider usage %d exceeded the reserved %d tokens", actualTokens, reservation.reservedTokens)
	}
	remainingReservation := int64(0)
	if actualTokens == 0 {
		// A response can omit usage, and a transport failure can occur after the
		// provider accepted and billed the request. Retaining the full bound is
		// the only settlement that keeps the configured daily limit hard.
		remainingReservation = reservation.reservedTokens
	}
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin seed crawler translation budget settlement: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('seed-crawler-ai-budget:'||$1::date::text,0))`, reservation.usageDate); err != nil {
		return fmt.Errorf("lock seed crawler translation budget settlement: %w", err)
	}
	command, err := tx.Exec(ctx, `update seed_crawler_translation_tasks set status=$5,input_tokens=input_tokens+$6,output_tokens=output_tokens+$7,
		quota_reserved_tokens=$9,last_error=$8,updated_at=now()
		where candidate_id=$1 and locale=$2 and usage_date=$3 and status='running' and quota_reserved_tokens=$4`,
		reservation.candidateID, reservation.locale, reservation.usageDate, reservation.reservedTokens,
		status, usage.InputTokens, usage.OutputTokens, truncateRunes(lastError, 1000), remainingReservation)
	if err != nil {
		return fmt.Errorf("store seed crawler translation budget settlement: %w", err)
	}
	if command.RowsAffected() != 1 {
		return errors.New("seed crawler translation reservation disappeared before settlement")
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit seed crawler translation budget settlement: %w", err)
	}
	return nil
}

type seedDraftLocalizationSource struct {
	Locale          string `json:"locale"`
	Name            string `json:"name"`
	Summary         string `json:"summary"`
	ContentMarkdown string `json:"contentMarkdown"`
	BodyMarkdown    string `json:"bodyMarkdown"`
}

func seedDraftTranslationSource(raw []byte) (string, []map[string]string, error) {
	var source struct {
		DefaultLocale string                        `json:"defaultLocale"`
		PrimaryName   string                        `json:"primaryName"`
		Summary       string                        `json:"summary"`
		BodyMarkdown  string                        `json:"bodyMarkdown"`
		Localizations []seedDraftLocalizationSource `json:"localizations"`
	}
	if err := json.Unmarshal(raw, &source); err != nil {
		return "", nil, fmt.Errorf("decode seed crawler translation source: %w", err)
	}
	sourceLocale := normalizeContentLocale(source.DefaultLocale)
	if sourceLocale == "" {
		return "", nil, errors.New("seed crawler translation source locale is invalid")
	}
	selected := seedDraftLocalizationSource{Locale: sourceLocale, Name: source.PrimaryName,
		Summary: source.Summary, BodyMarkdown: source.BodyMarkdown}
	for _, localization := range source.Localizations {
		if normalizeContentLocale(localization.Locale) == sourceLocale {
			selected = localization
			break
		}
	}
	body := selected.ContentMarkdown
	if strings.TrimSpace(body) == "" {
		body = selected.BodyMarkdown
	}
	items := make([]map[string]string, 0, 3)
	for _, item := range []struct{ key, text string }{
		{"name", selected.Name}, {"summary", selected.Summary}, {"bodyMarkdown", body},
	} {
		if text := strings.TrimSpace(item.text); text != "" {
			items = append(items, map[string]string{"key": item.key, "text": text})
		}
	}
	if len(items) == 0 || items[0]["key"] != "name" {
		return "", nil, errors.New("seed crawler translation source has no localized name")
	}
	return sourceLocale, items, nil
}

func validateSeedDraftTranslationResult(result map[string]any, items []map[string]string) (map[string]string, error) {
	allowed := make(map[string]bool, len(items))
	for _, item := range items {
		allowed[item["key"]] = true
	}
	translated, err := strictTranslationItemsToMap(result, allowed)
	if err != nil {
		return nil, fmt.Errorf("decode seed crawler translation result: %w", err)
	}
	if len(translated) != len(allowed) {
		return nil, errors.New("seed crawler translation result is incomplete")
	}
	for key := range allowed {
		if strings.TrimSpace(translated[key]) == "" {
			return nil, fmt.Errorf("seed crawler translation result has empty %s", key)
		}
	}
	return translated, nil
}

func applySeedDraftTranslations(projectType string, raw []byte, translations map[string]any) ([]byte, error) {
	if projectType == "mod" {
		var snapshot createModRequest
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			return nil, fmt.Errorf("decode seed crawler mod draft: %w", err)
		}
		existing := make(map[string]bool, len(snapshot.Localizations))
		for _, localization := range snapshot.Localizations {
			existing[normalizeContentLocale(localization.Locale)] = true
		}
		for _, locale := range supportedContentLocaleList() {
			translated, ok, err := seedDraftTranslationValues(translations[locale])
			if err != nil {
				return nil, fmt.Errorf("decode seed crawler %s translation: %w", locale, err)
			}
			if !ok || existing[locale] {
				continue
			}
			snapshot.Localizations = append(snapshot.Localizations, catalogLocalizationEdit{Locale: locale,
				Name: translated["name"], Summary: translated["summary"], ContentMarkdown: translated["bodyMarkdown"]})
		}
		if err := normalizeAndValidateModRequest(&snapshot); err != nil {
			return nil, fmt.Errorf("validate localized seed crawler mod draft: %w", err)
		}
		return json.Marshal(snapshot)
	}
	if normalizeSimpleProjectType(projectType) == "" {
		return nil, fmt.Errorf("unsupported seed crawler project type %q", projectType)
	}
	var snapshot simpleProjectSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, fmt.Errorf("decode seed crawler project draft: %w", err)
	}
	existing := make(map[string]bool, len(snapshot.Localizations))
	for _, localization := range snapshot.Localizations {
		existing[normalizeContentLocale(localization.Locale)] = true
	}
	for _, locale := range supportedContentLocaleList() {
		translated, ok, err := seedDraftTranslationValues(translations[locale])
		if err != nil {
			return nil, fmt.Errorf("decode seed crawler %s translation: %w", locale, err)
		}
		if !ok || existing[locale] {
			continue
		}
		snapshot.Localizations = append(snapshot.Localizations, simpleProjectLocalization{Locale: locale,
			Name: translated["name"], Summary: translated["summary"], BodyMarkdown: translated["bodyMarkdown"]})
	}
	if err := normalizeAndValidateSimpleProjectDraft(&snapshot, true); err != nil {
		return nil, fmt.Errorf("validate localized seed crawler project draft: %w", err)
	}
	return json.Marshal(snapshot)
}

func seedDraftTranslationValues(raw any) (map[string]string, bool, error) {
	if raw == nil {
		return nil, false, nil
	}
	if values, ok := raw.(map[string]string); ok {
		if strings.TrimSpace(values["name"]) == "" {
			return nil, false, errors.New("translated name is empty")
		}
		return values, true, nil
	}
	values, ok := raw.(map[string]any)
	if !ok {
		return nil, false, errors.New("translation is not an object")
	}
	if source, _ := values["source"].(bool); source {
		return nil, false, nil
	}
	result := make(map[string]string, 3)
	for _, key := range []string{"name", "summary", "bodyMarkdown"} {
		if value, exists := values[key]; exists {
			text, valid := value.(string)
			if !valid {
				return nil, false, fmt.Errorf("translation field %s is not text", key)
			}
			result[key] = text
		}
	}
	if strings.TrimSpace(result["name"]) == "" {
		return nil, false, errors.New("translated name is empty")
	}
	return result, true, nil
}

func (worker *SeedCrawlerWorker) submitSeedDraft(ctx context.Context, candidateID int64, draftID string, actor security.Claims, projectType, externalProjectID, externalURL string, raw []byte) (string, error) {
	finalizedStatus := ""
	hook := func(hookContext context.Context, tx pgx.Tx, result projectCreationTransactionResult) error {
		if result.ProjectType != projectType || result.ProjectID <= 0 || result.ProjectPublicID == "" || result.SiteID == "" ||
			result.ChangeRequestID <= 0 || result.ChangeRequestUID == "" ||
			(result.ReviewStatus != "approved" && result.ReviewStatus != "pending") {
			return errors.New("seed crawler project creation returned invalid transactional facts")
		}
		var routeID int64
		if err := tx.QueryRow(hookContext, `select id from public_routes where entity_type=$1 and internal_id=$2`,
			result.ProjectType, result.ProjectID).Scan(&routeID); err != nil {
			return fmt.Errorf("resolve submitted seed crawler project: %w", err)
		}
		if _, err := tx.Exec(hookContext, `insert into project_external_sources(project_route_id,source_type,external_project_id,external_project_url,verified_at,verified_by)
			values($1,'modrinth',$2,$3,now(),$4) on conflict(project_route_id,source_type) do update set
			external_project_id=excluded.external_project_id,external_project_url=excluded.external_project_url,verified_at=now(),verified_by=excluded.verified_by`,
			routeID, externalProjectID, externalURL, actor.Subject); err != nil {
			return fmt.Errorf("bind submitted seed crawler external source: %w", err)
		}
		draftUpdate, err := tx.Exec(hookContext, `update user_drafts set project_key=$2,project_title=$3,target_url=$4,
			change_request_id=$5,review_target_type='',review_target_id=null,submitted_status=$6,
			submitted_at=now(),updated_at=now() where public_id=$1 and user_id=$7 and submitted_at is null`,
			draftID, result.ProjectType+":"+result.SiteID, result.ProjectTitle, result.TargetURL,
			result.ChangeRequestID, result.ReviewStatus, actor.Subject)
		if err != nil {
			return fmt.Errorf("complete submitted seed crawler draft: %w", err)
		}
		if draftUpdate.RowsAffected() != 1 {
			return errors.New("submitted seed crawler draft is missing or already completed")
		}
		candidateUpdate, err := tx.Exec(hookContext, `update seed_crawler_candidates set status='submitted',last_error='',updated_at=now()
			where id=$1 and status='submitting'`, candidateID)
		if err != nil {
			return fmt.Errorf("complete submitted seed crawler candidate: %w", err)
		}
		if candidateUpdate.RowsAffected() != 1 {
			return errors.New("seed crawler candidate is not in the submitting state")
		}
		finalizedStatus = result.ReviewStatus
		return nil
	}
	request := httptest.NewRequest(http.MethodPost, "/internal/seed-crawler", bytes.NewReader(raw)).WithContext(
		withProjectCreationTransactionHook(context.WithValue(ctx, claimsContextKey, actor), hook),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	if projectType == "mod" {
		worker.server.createMod(response, request)
	} else {
		worker.server.createSimpleProject(response, request, projectType)
	}
	if response.Code >= 200 && response.Code < 300 && (finalizedStatus == "approved" || finalizedStatus == "pending") {
		return finalizedStatus, nil
	}
	var committedStatus string
	stateErr := worker.server.db.QueryRow(ctx, `select draft.submitted_status from seed_crawler_candidates candidate
		join user_drafts draft on draft.public_id=$2 and draft.user_id=$3
		where candidate.id=$1 and candidate.status='submitted' and draft.submitted_at is not null and draft.change_request_id is not null`,
		candidateID, draftID, actor.Subject).Scan(&committedStatus)
	if stateErr == nil && (committedStatus == "approved" || committedStatus == "pending") {
		return committedStatus, nil
	}
	return "", fmt.Errorf("seed draft submission failed (%d): %s: committed state: %v",
		response.Code, truncateRunes(response.Body.String(), 500), stateErr)
}
