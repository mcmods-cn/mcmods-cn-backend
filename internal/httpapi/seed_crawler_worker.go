package httpapi

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
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
	_, _ = worker.server.db.Exec(ctx, `update seed_crawler_runs set status='pending',lease_owner='',lease_expires_at=null,
		next_attempt_at=now(),last_error=case when last_error='' then 'worker lease expired' else last_error end
		where status='running' and lease_expires_at<now()`)
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
	var runID int64
	var runPublicID string
	var dryRun bool
	var requestedBy *int64
	err = tx.QueryRow(ctx, `select id,public_id,dry_run,requested_by from seed_crawler_runs
		where status='pending' and next_attempt_at<=now() order by created_at,id for update skip locked limit 1`).Scan(&runID, &runPublicID, &dryRun, &requestedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `update seed_crawler_runs set status='running',lease_owner=$2,lease_expires_at=now()+interval '5 minutes',attempts=attempts+1,started_at=coalesce(started_at,now()) where id=$1`, runID, "seed-worker"); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	if requestedBy == nil {
		var serviceUser int64
		if scanErr := worker.server.db.QueryRow(ctx, `select id from users where status='active' order by case username when 'admin' then 0 else 1 end,id limit 1`).Scan(&serviceUser); scanErr != nil {
			return true, worker.failRun(ctx, runID, scanErr)
		}
		requestedBy = &serviceUser
	}
	stats, runErr := worker.executeRun(ctx, runID, *requestedBy, dryRun)
	statsRaw, _ := json.Marshal(stats)
	if runErr != nil {
		return true, worker.failRunWithStats(ctx, runID, runErr, statsRaw)
	}
	tx, err = worker.server.db.Begin(ctx)
	if err != nil {
		return true, err
	}
	defer tx.Rollback(ctx)
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
		finished_at=case when attempts>=5 then now() else null end where id=$1`, runID, stats, truncateRunes(runErr.Error(), 2000))
	return err
}

func (worker *SeedCrawlerWorker) executeRun(ctx context.Context, runID, actorID int64, dryRun bool) (map[string]int, error) {
	var cfg seedCrawlerRuntimeConfig
	err := worker.server.db.QueryRow(ctx, `select project_types,batch_size,daily_limit,minimum_downloads,ai_daily_token_budget,auto_submit_review from seed_crawler_configs where id`).Scan(
		&cfg.ProjectTypes, &cfg.BatchSize, &cfg.DailyLimit, &cfg.MinimumDownloads, &cfg.AIDailyTokenBudget, &cfg.AutoSubmitReview)
	if err != nil {
		return nil, err
	}
	var importedToday int
	_ = worker.server.db.QueryRow(ctx, `select count(*) from seed_crawler_candidates where status in ('draft','submitted') and updated_at>=current_date`).Scan(&importedToday)
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
			payload, _ := json.Marshal(hit)
			var candidateID int64
			var currentStatus string
			err = worker.server.db.QueryRow(ctx, `insert into seed_crawler_candidates(run_id,external_project_id,project_type,downloads,payload)
				values($1,$2,$3,$4,$5::jsonb) on conflict(external_project_id) do update set downloads=excluded.downloads,payload=excluded.payload,updated_at=now()
				returning id,status`, runID, hit.ProjectID, projectType, hit.Downloads, payload).Scan(&candidateID, &currentStatus)
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
			status, processErr := worker.createSeedDraft(ctx, candidateID, actorID, projectType, hit, cfg)
			if processErr != nil {
				stats["failed"]++
				_, _ = worker.server.db.Exec(ctx, `update seed_crawler_candidates set status='failed',last_error=$2,updated_at=now() where id=$1`, candidateID, truncateRunes(processErr.Error(), 2000))
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

func (worker *SeedCrawlerWorker) createSeedDraft(ctx context.Context, candidateID, actorID int64, projectType string, hit seedModrinthHit, cfg seedCrawlerRuntimeConfig) (string, error) {
	var exists bool
	if projectType == "mod" {
		_ = worker.server.db.QueryRow(ctx, `select exists(select 1 from mods where modrinth_project_id=$1)`, hit.ProjectID).Scan(&exists)
	} else {
		_ = worker.server.db.QueryRow(ctx, `select exists(select 1 from simple_projects where project_type=$1 and modrinth_project_id=$2)`, projectType, hit.ProjectID).Scan(&exists)
	}
	if exists {
		_, _ = worker.server.db.Exec(ctx, `update seed_crawler_candidates set status='existing',last_error='',updated_at=now() where id=$1`, candidateID)
		return "duplicates", nil
	}
	var jobID string
	sourceURL := seedModrinthURL(projectType, hit.Slug)
	err := worker.server.db.QueryRow(ctx, `insert into mod_metadata_import_jobs(user_id,project_type,provider,source_url,status,progress)
		values($1,$2,'modrinth',$3,'queued',0) returning public_id`, actorID, projectType, sourceURL).Scan(&jobID)
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
	payload["seedTranslations"] = worker.translateSeedDraft(ctx, candidateID, result, cfg.AIDailyTokenBudget)
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
	err = worker.server.db.QueryRow(ctx, `insert into user_drafts(user_id,draft_key,project_key,project_title,kind,title,edit_url,payload,expires_at)
		values($1,$2,$3,$4,'seed_crawler_import',$4,$5,$6::jsonb,now()+interval '3 years')
		on conflict(user_id,draft_key) where submitted_at is null do update set payload=excluded.payload,project_title=excluded.project_title,title=excluded.title,updated_at=now(),expires_at=excluded.expires_at returning public_id`,
		actorID, "seed-crawler:"+hit.ProjectID, projectType+":"+hit.ProjectID, hit.Title, editURL, finalPayload).Scan(&draftID)
	if err != nil {
		return "", err
	}
	nextStatus := "draft"
	if cfg.AutoSubmitReview {
		if submitErr := worker.submitSeedDraft(ctx, actorID, projectType, hit.ProjectID, sourceURL, result); submitErr != nil {
			return "", submitErr
		}
		nextStatus = "submitted"
		_, _ = worker.server.db.Exec(ctx, `update user_drafts set submitted_status='pending',submitted_at=now(),updated_at=now() where public_id=$1`, draftID)
	}
	_, err = worker.server.db.Exec(ctx, `update seed_crawler_candidates set status=$2,last_error='',updated_at=now() where id=$1`, candidateID, nextStatus)
	return nextStatus, err
}

func (worker *SeedCrawlerWorker) translateSeedDraft(ctx context.Context, candidateID int64, raw []byte, budget int64) map[string]any {
	translations := map[string]any{"en-US": map[string]any{"source": true}}
	locales := make([]string, 0, len(supportedEditableContentLocales)-1)
	for _, locale := range supportedContentLocaleList() {
		if locale != "en-US" {
			locales = append(locales, locale)
		}
	}
	var used int64
	_ = worker.server.db.QueryRow(ctx, `select coalesce(sum(input_tokens+output_tokens),0) from seed_crawler_translation_tasks where updated_at>=current_date`).Scan(&used)
	if budget <= 0 || used >= budget {
		return translations
	}
	var source map[string]any
	_ = json.Unmarshal(raw, &source)
	items := []map[string]string{}
	for _, key := range []string{"primaryName", "summary", "bodyMarkdown"} {
		if text, ok := source[key].(string); ok && strings.TrimSpace(text) != "" {
			items = append(items, map[string]string{"key": key, "text": text})
		}
	}
	if len(items) == 0 {
		if localizations, ok := source["localizations"].([]any); ok && len(localizations) > 0 {
			if first, ok := localizations[0].(map[string]any); ok {
				for _, key := range []string{"name", "summary", "bodyMarkdown"} {
					if text, ok := first[key].(string); ok && strings.TrimSpace(text) != "" {
						items = append(items, map[string]string{"key": key, "text": text})
					}
				}
			}
		}
	}
	if len(items) == 0 {
		return translations
	}
	aiCfg := aiConfigFromDatabase(ctx, worker.server.db, worker.server.cfg.SettingsEncryptionKey)
	binding, ok := findAITaskModel(aiCfg.TaskModels, aiTaskContentTranslation)
	if !ok || strings.TrimSpace(binding.ModelKey) == "" {
		return translations
	}
	providerCode, modelID, ok := strings.Cut(binding.ModelKey, "/")
	if !ok {
		return translations
	}
	aiWorker := NewAIWorker(worker.server.db, nil, worker.server.cfg.SettingsEncryptionKey)
	for _, locale := range locales {
		if used >= budget {
			break
		}
		_, _ = worker.server.db.Exec(ctx, `insert into seed_crawler_translation_tasks(candidate_id,locale,status) values($1,$2,'running')
			on conflict(candidate_id,locale) do update set status='running',attempts=seed_crawler_translation_tasks.attempts+1,updated_at=now()`, candidateID, locale)
		payload, _ := json.Marshal(map[string]any{"sourceLocale": "en-US", "targetLocale": locale, "items": items})
		result, usage, err := aiWorker.executeTask(ctx, aiTaskContentTranslation, providerCode, modelID, payload)
		if err != nil {
			_, _ = worker.server.db.Exec(ctx, `update seed_crawler_translation_tasks set status='failed',last_error=$3,updated_at=now() where candidate_id=$1 and locale=$2`, candidateID, locale, truncateRunes(err.Error(), 1000))
			continue
		}
		translated := translationItemsToMap(result)
		translations[locale] = translated
		used += usage.InputTokens + usage.OutputTokens
		_, _ = worker.server.db.Exec(ctx, `update seed_crawler_translation_tasks set status='completed',input_tokens=$3,output_tokens=$4,last_error='',updated_at=now() where candidate_id=$1 and locale=$2`, candidateID, locale, usage.InputTokens, usage.OutputTokens)
	}
	return translations
}

func (worker *SeedCrawlerWorker) submitSeedDraft(ctx context.Context, actorID int64, projectType, externalProjectID, externalURL string, raw []byte) error {
	request := httptest.NewRequest(http.MethodPost, "/internal/seed-crawler", bytes.NewReader(raw)).WithContext(
		context.WithValue(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: actorID, PermissionRules: []security.PermissionRule{
			{Code: "project.create", Allow: true, Priority: 100},
			{Code: "project.create." + projectType, Allow: true, Priority: 100},
		}}), antiAbuseModerationContextKey, true),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	if projectType == "mod" {
		worker.server.createMod(response, request)
	} else {
		worker.server.createSimpleProject(response, request, projectType)
	}
	if response.Code < 200 || response.Code >= 300 {
		return fmt.Errorf("seed draft review submission failed (%d): %s", response.Code, truncateRunes(response.Body.String(), 500))
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
			_, _ = worker.server.db.Exec(ctx, `insert into project_external_sources(project_route_id,source_type,external_project_id,external_project_url,verified_at,verified_by)
				values($1,'modrinth',$2,$3,now(),$4) on conflict(project_route_id,source_type) do update set
				external_project_id=excluded.external_project_id,external_project_url=excluded.external_project_url,verified_at=now(),verified_by=excluded.verified_by`,
				routeID, externalProjectID, externalURL, actorID)
		}
	} else {
		err := worker.server.db.QueryRow(ctx, `select route.id,route.public_id from simple_projects project
			join public_routes route on route.entity_type=project.project_type and route.internal_id=project.id
			where project.project_type=$1 and project.modrinth_project_id=$2`, projectType, externalProjectID).Scan(&routeID, &publicID)
		if err == nil {
			_, _ = worker.server.db.Exec(ctx, `insert into project_external_sources(project_route_id,source_type,external_project_id,external_project_url,verified_at,verified_by)
				values($1,'modrinth',$2,$3,now(),$4) on conflict(project_route_id,source_type) do update set
				external_project_id=excluded.external_project_id,external_project_url=excluded.external_project_url,verified_at=now(),verified_by=excluded.verified_by`,
				routeID, externalProjectID, externalURL, actorID)
		}
	}
	return nil
}
