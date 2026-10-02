package httpapi

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

const (
	projectAutomationTick                    = 30 * time.Second
	projectAutomationLease                   = 10 * time.Minute
	projectAutomationMaxFileBytes            = int64(256 << 20)
	projectAutomationMaxFilesPerRun          = 25
	projectAutomationMirrorBufferBytes       = 128 << 10
	projectAutomationMirrorGlobalConcurrency = 4
)

var errProjectMirrorScanRejected = errors.New("automated project mirror failed security scan")

type ProjectAutomationWorker struct {
	server *Server
}

func NewProjectAutomationWorker(cfg config.Config, db *pgxpool.Pool) *ProjectAutomationWorker {
	return &ProjectAutomationWorker{server: &Server{cfg: cfg, db: db}}
}

func (worker *ProjectAutomationWorker) Start(ctx context.Context) {
	if worker == nil || worker.server == nil || worker.server.db == nil {
		return
	}
	go worker.run(ctx)
}

func (worker *ProjectAutomationWorker) run(ctx context.Context) {
	if err := worker.tick(ctx); err != nil {
		log.Printf("project automation tick: %v", err)
	}
	ticker := time.NewTicker(projectAutomationTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := worker.tick(ctx); err != nil {
				log.Printf("project automation tick: %v", err)
			}
		}
	}
}

func (worker *ProjectAutomationWorker) tick(ctx context.Context) error {
	if err := worker.recoverExpiredLeases(ctx); err != nil {
		return fmt.Errorf("recover expired project automation leases: %w", err)
	}
	if err := worker.scheduleDue(ctx); err != nil {
		return err
	}
	if err := worker.promoteCleanMirrors(ctx); err != nil {
		return fmt.Errorf("reconcile project mirror scans: %w", err)
	}
	for index := 0; index < 4; index++ {
		processed, err := worker.processOne(ctx)
		if err != nil {
			return fmt.Errorf("process project automation run: %w", err)
		}
		if !processed {
			return nil
		}
	}
	return nil
}

func (worker *ProjectAutomationWorker) scheduleDue(ctx context.Context) error {
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin project automation schedule: %w", err)
	}
	defer tx.Rollback(ctx)
	var locked bool
	if err = tx.QueryRow(ctx, `select pg_try_advisory_xact_lock(hashtext('mcmods.project_auto_update.schedule'))`).Scan(&locked); err != nil {
		return fmt.Errorf("lock project automation schedule: %w", err)
	}
	if !locked {
		return nil
	}
	rows, err := tx.Query(ctx, `select id from project_auto_update_settings setting
		where setting.enabled and setting.next_run_at<=now() and setting.source_type is not null
		and not exists(select 1 from project_auto_update_runs run where run.setting_id=setting.id and run.status in ('pending','running'))
		order by setting.next_run_at,setting.id for update skip locked limit 50`)
	if err != nil {
		return fmt.Errorf("read due project automation settings: %w", err)
	}
	ids := make([]int64, 0, 50)
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan due project automation setting: %w", err)
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate due project automation settings: %w", err)
	}
	rows.Close()
	for _, id := range ids {
		if _, err = tx.Exec(ctx, `insert into project_auto_update_runs(setting_id) values($1) on conflict do nothing`, id); err != nil {
			return fmt.Errorf("schedule project automation setting %d: %w", id, err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit project automation schedule: %w", err)
	}
	return nil
}

type projectAutomationJob struct {
	RunID           int64
	LeaseOwner      string
	SettingID       int64
	RouteID         int64
	InternalID      int64
	ProjectType     string
	ProjectPublicID string
	Kind            string
	SourceType      string
	ExternalID      string
	Interval        string
	ActorID         int64
	LicenseOverride bool
	OverrideReason  string
	OverrideSource  string
}

func (worker *ProjectAutomationWorker) processOne(ctx context.Context) (bool, error) {
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var job projectAutomationJob
	err = tx.QueryRow(ctx, `select run.id,setting.id,route.id,route.internal_id,route.entity_type,route.public_id,
		setting.update_kind,setting.source_type,source.external_project_id,setting.interval_code,
		setting.license_override,setting.license_override_reason,setting.license_override_source
		from project_auto_update_runs run
		join project_auto_update_settings setting on setting.id=run.setting_id
		join public_routes route on route.id=setting.project_route_id
		join project_external_sources source on source.project_route_id=setting.project_route_id and source.source_type=setting.source_type
		where run.status='pending' and run.next_attempt_at<=now()
		order by run.created_at,run.id for update of run skip locked limit 1`).Scan(
		&job.RunID, &job.SettingID, &job.RouteID, &job.InternalID, &job.ProjectType, &job.ProjectPublicID,
		&job.Kind, &job.SourceType, &job.ExternalID, &job.Interval,
		&job.LicenseOverride, &job.OverrideReason, &job.OverrideSource)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	job.LeaseOwner = "project-auto-update:" + newExportID()
	if _, err = tx.Exec(ctx, `update project_auto_update_runs set status='running',lease_owner=$3,
		lease_expires_at=now()+$2::interval,attempts=attempts+1,started_at=coalesce(started_at,now()) where id=$1`,
		job.RunID, projectAutomationLease.String(), job.LeaseOwner); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	actor, actorErr := worker.server.automationActor(ctx)
	if actorErr != nil {
		return true, worker.fail(ctx, job, "automation_actor_unavailable", actorErr, []byte(`{}`))
	}
	job.ActorID = actor.Subject
	if _, actorErr = worker.mutateProjectAutomationRun(ctx, job, `update project_auto_update_runs set actor_id=$2 where id=$1`, job.RunID, job.ActorID); actorErr != nil {
		return true, worker.fail(ctx, job, "automation_actor_unavailable", actorErr, []byte(`{}`))
	}

	result, code, runErr := worker.execute(ctx, job)
	raw, _ := json.Marshal(result)
	if runErr != nil {
		return true, worker.fail(ctx, job, code, runErr, raw)
	}
	next := autoUpdateNextRun(job.Interval, time.Now().UTC())
	tx, err = worker.server.db.Begin(ctx)
	if err != nil {
		return true, err
	}
	defer tx.Rollback(ctx)
	if err = lockProjectAutomationRunTx(ctx, tx, job); err != nil {
		return true, err
	}
	if _, err = tx.Exec(ctx, `update project_auto_update_runs set status='completed',result=$2::jsonb,last_error_code='',last_error='',
		lease_owner='',lease_expires_at=null,finished_at=now() where id=$1`, job.RunID, raw); err != nil {
		return true, err
	}
	if _, err = tx.Exec(ctx, `update project_auto_update_settings set last_run_at=now(),last_status='completed',last_error_code='',last_error='',next_run_at=$2 where id=$1`, job.SettingID, next); err != nil {
		return true, err
	}
	return true, tx.Commit(ctx)
}

func (worker *ProjectAutomationWorker) fail(ctx context.Context, job projectAutomationJob, code string, runErr error, result []byte) error {
	if code == "" {
		code = "external_service_unavailable"
	}
	message := truncateRunes(runErr.Error(), 2000)
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockProjectAutomationRunTx(ctx, tx, job); err != nil {
		return err
	}
	var attempts int
	if err = tx.QueryRow(ctx, `select attempts from project_auto_update_runs where id=$1 for update`, job.RunID).Scan(&attempts); err != nil {
		return err
	}
	terminal := attempts >= 5 || code == "license_denied" || code == "unsupported_source" || code == "source_binding_invalid"
	status := "pending"
	if terminal {
		status = "dead_letter"
	}
	if _, err = tx.Exec(ctx, `update project_auto_update_runs set status=$2,result=$3::jsonb,last_error_code=$4,last_error=$5,
		lease_owner='',lease_expires_at=null,next_attempt_at=now()+make_interval(secs=>least(7200,(30*power(2,least(attempts,7)))::integer)),
		finished_at=case when $2='dead_letter' then now() else null end where id=$1`, job.RunID, status, result, code, message); err != nil {
		return err
	}
	next := autoUpdateNextRun(job.Interval, time.Now().UTC())
	if _, err = tx.Exec(ctx, `update project_auto_update_settings set last_run_at=now(),last_status=$2,last_error_code=$3,last_error=$4,
		next_run_at=case when $2='dead_letter' then $5 else next_run_at end where id=$1`, job.SettingID, status, code, message, next); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (worker *ProjectAutomationWorker) execute(ctx context.Context, job projectAutomationJob) (map[string]any, string, error) {
	cfg, err := worker.server.modImportConfigFromSettings(ctx)
	if err != nil {
		return nil, "configuration_error", err
	}
	if err = ensureModImportProviderAvailable(cfg, job.SourceType); err != nil {
		return nil, "credentials_invalid", err
	}
	switch job.Kind {
	case "minecraft_versions":
		if job.SourceType == "github" {
			return nil, "unsupported_source", errors.New("GitHub does not provide authoritative Minecraft compatibility")
		}
		files, loadErr := worker.loadProviderFiles(ctx, job, cfg)
		if loadErr != nil {
			return nil, providerFailureCode(loadErr), loadErr
		}
		result, updateErr := worker.mergeCompatibility(ctx, job, files)
		result, maintenanceErr := worker.withMaintenanceResult(ctx, job, result, latestProviderFileActivity(files))
		if updateErr == nil && maintenanceErr != nil {
			return result, "maintenance_status_update_failed", maintenanceErr
		}
		return result, "minecraft_version_unmapped", updateErr
	case "changelog":
		releases, loadErr := worker.loadReleases(ctx, job, cfg)
		if loadErr != nil {
			return nil, providerFailureCode(loadErr), loadErr
		}
		result, updateErr := worker.syncChangelogs(ctx, job, releases)
		result, maintenanceErr := worker.withMaintenanceResult(ctx, job, result, latestProviderReleaseActivity(releases))
		if updateErr == nil && maintenanceErr != nil {
			return result, "maintenance_status_update_failed", maintenanceErr
		}
		return result, "update_conflict", updateErr
	case "site_downloads":
		if err = worker.checkRedistribution(ctx, job); err != nil {
			return nil, "license_denied", err
		}
		files, loadErr := worker.loadProviderFiles(ctx, job, cfg)
		if loadErr != nil {
			return nil, providerFailureCode(loadErr), loadErr
		}
		result, mirrorErr := worker.mirrorFiles(ctx, job, cfg, files)
		result, maintenanceErr := worker.withMaintenanceResult(ctx, job, result, latestProviderFileActivity(files))
		if mirrorErr == nil && maintenanceErr != nil {
			return result, "maintenance_status_update_failed", maintenanceErr
		}
		failureCode := "download_failed"
		if errors.Is(mirrorErr, errProjectMirrorScanRejected) {
			failureCode = "project_mirror_scan_rejected"
		}
		return result, failureCode, mirrorErr
	default:
		return nil, "unsupported_source", errors.New("unsupported update kind")
	}
}

func (worker *ProjectAutomationWorker) withMaintenanceResult(ctx context.Context, job projectAutomationJob, result map[string]any, observedAt time.Time) (map[string]any, error) {
	maintenance, err := worker.applyProjectMaintenancePolicy(ctx, job, observedAt, time.Now().UTC())
	if result == nil {
		result = map[string]any{}
	}
	if err == nil {
		result["maintenance"] = maintenance
	}
	return result, err
}

func providerFailureCode(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "http 401") || strings.Contains(message, "http 403"):
		return "credentials_invalid"
	case strings.Contains(message, "http 404"):
		return "source_not_found"
	case strings.Contains(message, "http 429"):
		return "provider_rate_limited"
	default:
		return "external_service_unavailable"
	}
}

func (worker *ProjectAutomationWorker) loadProviderFiles(ctx context.Context, job projectAutomationJob, cfg modImportConfig) ([]providerProjectFile, error) {
	switch job.SourceType {
	case "modrinth":
		return loadModrinthProjectFiles(ctx, job.ExternalID, cfg)
	case "curseforge":
		return loadCurseForgeProjectFiles(ctx, job.ExternalID, cfg)
	case "github":
		releases, err := worker.loadGitHubReleases(ctx, job.ExternalID, cfg)
		if err != nil {
			return nil, err
		}
		files := make([]providerProjectFile, 0)
		for _, release := range releases {
			files = append(files, release.Files...)
		}
		return files, nil
	default:
		return nil, errors.New("unsupported provider")
	}
}

func (worker *ProjectAutomationWorker) mergeCompatibility(ctx context.Context, job projectAutomationJob, files []providerProjectFile) (map[string]any, error) {
	catalog, err := loadMinecraftVersionConfig(ctx, worker.server.db)
	if err != nil {
		return nil, fmt.Errorf("load Minecraft version configuration for project automation: %w", err)
	}
	versions := make(map[string]bool, len(catalog.Versions))
	for _, version := range catalog.Versions {
		versions[version.Code] = true
	}
	loaders := map[string]bool{}
	for _, loader := range catalog.Loaders {
		loaders[strings.ToLower(loader.Code)] = true
	}
	recognizedVersions, recognizedLoaders := map[string]bool{}, map[string]bool{}
	unmappedVersions, unmappedLoaders := map[string]bool{}, map[string]bool{}
	pairs := map[string][2]string{}
	for _, file := range files {
		for _, version := range uniqueTrimmed(file.GameVersions, 100) {
			if versions[version] {
				recognizedVersions[version] = true
			} else {
				unmappedVersions[version] = true
			}
		}
		for _, loader := range normalizeLoaders(file.Loaders) {
			loader = strings.ToLower(loader)
			if loaders[loader] {
				recognizedLoaders[loader] = true
			} else {
				unmappedLoaders[loader] = true
			}
		}
		for _, version := range file.GameVersions {
			if !versions[version] {
				continue
			}
			for _, loader := range normalizeLoaders(file.Loaders) {
				loader = strings.ToLower(loader)
				if loaders[loader] {
					pairs[loader+"\x00"+version] = [2]string{loader, version}
				}
			}
		}
	}
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err = lockProjectAutomationRunTx(ctx, tx, job); err != nil {
		return nil, err
	}
	added := int64(0)
	if job.ProjectType == "mod" {
		for _, pair := range pairs {
			command, execErr := tx.Exec(ctx, `insert into mod_loader_compatibilities(mod_id,loader,minecraft_version)
				values($1,$2,$3) on conflict do nothing`, job.InternalID, pair[0], pair[1])
			if execErr != nil {
				return nil, execErr
			}
			added += command.RowsAffected()
		}
	} else {
		versionList, loaderList := mapKeys(recognizedVersions), mapKeys(recognizedLoaders)
		command, execErr := tx.Exec(ctx, `update simple_projects set
			minecraft_versions=(select coalesce(array_agg(distinct value order by value),'{}'::text[]) from unnest(minecraft_versions||$3::text[]) value),
			loaders=(select coalesce(array_agg(distinct value order by value),'{}'::text[]) from unnest(loaders||$4::text[]) value),updated_at=now()
			where id=$1 and project_type=$2`, job.InternalID, job.ProjectType, versionList, loaderList)
		if execErr != nil {
			return nil, execErr
		}
		added = command.RowsAffected()
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	result := map[string]any{"relationsAdded": added, "versionsSeen": mapKeys(recognizedVersions), "loadersSeen": mapKeys(recognizedLoaders),
		"unmappedVersions": mapKeys(unmappedVersions), "unmappedLoaders": mapKeys(unmappedLoaders)}
	if len(unmappedVersions) > 0 || len(unmappedLoaders) > 0 {
		return result, errors.New("provider returned compatibility values missing from the site registry")
	}
	return result, nil
}

func mapKeys(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

type projectAutomationRelease struct {
	ID           string
	Version      string
	Body         string
	PublishedAt  time.Time
	GameVersions []string
	Loaders      []string
	URL          string
	Files        []providerProjectFile
}

func (worker *ProjectAutomationWorker) loadReleases(ctx context.Context, job projectAutomationJob, cfg modImportConfig) ([]projectAutomationRelease, error) {
	switch job.SourceType {
	case "modrinth":
		client, err := newProviderHTTPClient(time.Duration(cfg.RequestTimeoutSeconds)*time.Second, cfg.Modrinth.BaseURL)
		if err != nil {
			return nil, err
		}
		var raw []struct {
			ID, Name, VersionNumber, Changelog, DatePublished, VersionType string
			GameVersions                                                   []string `json:"game_versions"`
			Loaders                                                        []string `json:"loaders"`
			Files                                                          []struct {
				URL, Filename string
				Size          int64
				Hashes        map[string]string
			} `json:"files"`
		}
		if err = getProviderJSON(ctx, client, cfg.Modrinth.BaseURL+"/project/"+url.PathEscape(job.ExternalID)+"/version", providerCredentialHeaders(cfg.UserAgent, "modrinth", cfg.Modrinth.BaseURL, bearerAuthorization(cfg.Modrinth.Token), ""), &raw); err != nil {
			return nil, err
		}
		result := make([]projectAutomationRelease, 0, len(raw))
		for _, item := range raw {
			published, _ := time.Parse(time.RFC3339, item.DatePublished)
			release := projectAutomationRelease{ID: item.ID, Version: firstNonEmpty(item.VersionNumber, item.Name, item.ID), Body: item.Changelog,
				PublishedAt: published, GameVersions: item.GameVersions, Loaders: normalizeLoaders(item.Loaders), URL: "https://modrinth.com/project/" + job.ExternalID + "/version/" + item.ID}
			for _, file := range item.Files {
				release.Files = append(release.Files, providerProjectFile{ID: item.ID + ":" + file.Filename, Source: "modrinth", DisplayName: item.Name,
					FileName: file.Filename, VersionName: release.Version, ReleaseChannel: normalizeReleaseChannel(item.VersionType), GameVersions: item.GameVersions,
					Loaders: release.Loaders, PublishedAt: published, SizeBytes: file.Size, SHA1: file.Hashes["sha1"], SHA512: file.Hashes["sha512"], DirectURL: file.URL})
			}
			result = append(result, release)
		}
		return result, nil
	case "curseforge":
		files, err := loadCurseForgeProjectFiles(ctx, job.ExternalID, cfg)
		if err != nil {
			return nil, err
		}
		client, err := newProviderHTTPClient(time.Duration(cfg.RequestTimeoutSeconds)*time.Second, cfg.CurseForge.BaseURL)
		if err != nil {
			return nil, err
		}
		headers := providerCredentialHeaders(cfg.UserAgent, "curseforge", cfg.CurseForge.BaseURL, "", cfg.CurseForge.APIKey)
		result := make([]projectAutomationRelease, 0, min(len(files), 100))
		for _, file := range files {
			if len(result) >= 100 {
				break
			}
			var response struct {
				Data string `json:"data"`
			}
			_ = getProviderJSON(ctx, client, cfg.CurseForge.BaseURL+"/mods/"+url.PathEscape(job.ExternalID)+"/files/"+url.PathEscape(file.ID)+"/changelog", headers, &response)
			result = append(result, projectAutomationRelease{ID: file.ID, Version: firstNonEmpty(file.VersionName, file.DisplayName, file.ID), Body: response.Data,
				PublishedAt: file.PublishedAt, GameVersions: file.GameVersions, Loaders: file.Loaders,
				URL: "https://www.curseforge.com/minecraft/mc-mods/" + job.ExternalID + "/files/" + file.ID, Files: []providerProjectFile{file}})
		}
		return result, nil
	case "github":
		return worker.loadGitHubReleases(ctx, job.ExternalID, cfg)
	default:
		return nil, errors.New("unsupported provider")
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return "external-release"
}

func (worker *ProjectAutomationWorker) loadGitHubReleases(ctx context.Context, repository string, cfg modImportConfig) ([]projectAutomationRelease, error) {
	client, err := newProviderHTTPClient(time.Duration(cfg.RequestTimeoutSeconds)*time.Second, cfg.GitHub.BaseURL)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		ID          int64     `json:"id"`
		TagName     string    `json:"tag_name"`
		Name        string    `json:"name"`
		Body        string    `json:"body"`
		HTMLURL     string    `json:"html_url"`
		PublishedAt time.Time `json:"published_at"`
		Prerelease  bool      `json:"prerelease"`
		Assets      []struct {
			ID                 int64  `json:"id"`
			Name               string `json:"name"`
			Size               int64  `json:"size"`
			DownloadCount      int64  `json:"download_count"`
			BrowserDownloadURL string `json:"browser_download_url"`
			ContentType        string `json:"content_type"`
		} `json:"assets"`
	}
	if err = getProviderJSON(ctx, client, cfg.GitHub.BaseURL+"/repos/"+repository+"/releases?per_page=100", providerCredentialHeaders(cfg.UserAgent, "github", cfg.GitHub.BaseURL, bearerAuthorization(cfg.GitHub.Token), ""), &raw); err != nil {
		return nil, err
	}
	result := make([]projectAutomationRelease, 0, len(raw))
	for _, item := range raw {
		release := projectAutomationRelease{ID: fmt.Sprint(item.ID), Version: firstNonEmpty(item.TagName, item.Name, fmt.Sprint(item.ID)), Body: item.Body,
			PublishedAt: item.PublishedAt, URL: item.HTMLURL}
		channel := "release"
		if item.Prerelease {
			channel = "beta"
		}
		for _, asset := range item.Assets {
			release.Files = append(release.Files, providerProjectFile{ID: fmt.Sprint(asset.ID), Source: "github", DisplayName: firstNonEmpty(item.Name, item.TagName),
				FileName: asset.Name, VersionName: release.Version, ReleaseChannel: channel, PublishedAt: item.PublishedAt, SizeBytes: asset.Size,
				DownloadCount: asset.DownloadCount, DirectURL: asset.BrowserDownloadURL})
		}
		result = append(result, release)
	}
	return result, nil
}

func (worker *ProjectAutomationWorker) syncChangelogs(ctx context.Context, job projectAutomationJob, releases []projectAutomationRelease) (map[string]any, error) {
	created, updated, unchanged, manualConflicts, skippedUnknownVersions := 0, 0, 0, 0, 0
	catalog, err := loadMinecraftVersionConfig(ctx, worker.server.db)
	if err != nil {
		return nil, fmt.Errorf("load Minecraft version configuration for changelog synchronization: %w", err)
	}
	currentVersions, _, currentVersionErr := classifyMinecraftVersionCodes(catalog, worker.currentProjectVersions(ctx, job), 100)
	if currentVersionErr != nil {
		currentVersions = nil
	}
	for _, release := range releases {
		if release.PublishedAt.IsZero() {
			release.PublishedAt = time.Now().UTC()
		}
		rawMinecraftVersions := uniqueTrimmed(release.GameVersions, 100)
		candidateVersions := release.GameVersions
		if len(candidateVersions) == 0 {
			candidateVersions = currentVersions
		}
		resolvedVersions, unmappedVersions, resolveErr := classifyMinecraftVersionCodes(catalog, candidateVersions, 100)
		if resolveErr != nil || len(resolvedVersions) == 0 {
			skippedUnknownVersions++
			continue
		}
		release.GameVersions = resolvedVersions
		body := strings.TrimSpace(release.Body)
		if body == "" {
			body = "[View the external release](" + release.URL + ")"
		}
		releaseMetadata := map[string]any{"minecraftVersions": release.GameVersions, "loaders": release.Loaders,
			"rawMinecraftVersions": rawMinecraftVersions, "unmappedMinecraftVersions": unmappedVersions}
		signature, _ := json.Marshal(map[string]any{"body": body, "metadata": releaseMetadata})
		hashBytes := sha256.Sum256(signature)
		bodyHash := hex.EncodeToString(hashBytes[:])
		tx, err := worker.server.db.Begin(ctx)
		if err != nil {
			return nil, err
		}
		if err = lockProjectAutomationRunTx(ctx, tx, job); err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		}
		var bindingID int64
		var changelogID *string
		var previousHash string
		var manual bool
		err = tx.QueryRow(ctx, `select id,changelog_public_id,external_body_hash,manual_override from external_release_bindings
			where source_type=$1 and external_release_id=$2 for update`, job.SourceType, release.ID).Scan(&bindingID, &changelogID, &previousHash, &manual)
		if errors.Is(err, pgx.ErrNoRows) {
			var internalID int64
			var publicID string
			if err = tx.QueryRow(ctx, `insert into project_changelogs(object_route_id,event_at,minecraft_versions,project_version,default_locale,created_by)
				values($1,$2,$3,$4,'en-US',$5) returning id,public_id`, job.RouteID, release.PublishedAt, uniqueTrimmed(release.GameVersions, 100), release.Version, job.ActorID).Scan(&internalID, &publicID); err != nil {
				_ = tx.Rollback(ctx)
				return nil, err
			}
			snapshot := projectChangelogSnapshot{EventAt: release.PublishedAt, MinecraftVersions: uniqueTrimmed(release.GameVersions, 100), ProjectVersion: release.Version,
				DefaultLocale: "en-US", Localizations: []projectChangelogLocalization{{Locale: "en-US", BodyMarkdown: body}}, Reason: "Automated external release synchronization"}
			raw, _ := json.Marshal(snapshot)
			revision, createErr := createContentRevisionTx(ctx, tx, createContentRevisionParams{EntityType: projectChangelogAggregate, EntityID: internalID,
				AggregateType: projectChangelogAggregate, AggregateKey: publicID, Snapshot: raw, Reason: snapshot.Reason, ActorID: job.ActorID,
				Source: "auto_update", Status: "approved", Metadata: map[string]any{"source": job.SourceType, "externalReleaseId": release.ID, "externalURL": release.URL}})
			if createErr != nil {
				_ = tx.Rollback(ctx)
				return nil, createErr
			}
			if err = applyProjectChangelogSnapshotTx(ctx, tx, internalID, job.RouteID, revision.RevisionID, job.ActorID, snapshot); err != nil {
				_ = tx.Rollback(ctx)
				return nil, err
			}
			metadata, _ := json.Marshal(releaseMetadata)
			_, err = tx.Exec(ctx, `insert into external_release_bindings(project_route_id,source_type,external_release_id,changelog_public_id,external_body_hash,external_url,metadata)
				values($1,$2,$3,$4,$5,$6,$7::jsonb)`, job.RouteID, job.SourceType, release.ID, publicID, bodyHash, release.URL, metadata)
			created++
		} else if err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		} else if previousHash == bodyHash {
			unchanged++
		} else if manual || changelogID == nil {
			manualConflicts++
			metadata, _ := json.Marshal(map[string]any{"upstreamChanged": true, "latestHash": bodyHash, "externalURL": release.URL,
				"minecraftVersions": release.GameVersions, "loaders": release.Loaders, "rawMinecraftVersions": rawMinecraftVersions,
				"unmappedMinecraftVersions": unmappedVersions})
			_, err = tx.Exec(ctx, `update external_release_bindings set metadata=metadata||$2::jsonb,external_url=$3,updated_at=now() where id=$1`, bindingID, metadata, release.URL)
		} else {
			var internalID int64
			var publishedRevisionID *int64
			if err = tx.QueryRow(ctx, `select id,published_revision_id from project_changelogs where public_id=$1 and object_route_id=$2 and status='active' for update`, *changelogID, job.RouteID).Scan(&internalID, &publishedRevisionID); err != nil {
				_ = tx.Rollback(ctx)
				return nil, err
			}
			snapshot := projectChangelogSnapshot{EventAt: release.PublishedAt, MinecraftVersions: uniqueTrimmed(release.GameVersions, 100), ProjectVersion: release.Version,
				DefaultLocale: "en-US", Localizations: []projectChangelogLocalization{{Locale: "en-US", BodyMarkdown: body}}, Reason: "Automated external release synchronization"}
			raw, _ := json.Marshal(snapshot)
			revision, createErr := createContentRevisionTx(ctx, tx, createContentRevisionParams{EntityType: projectChangelogAggregate, EntityID: internalID,
				AggregateType: projectChangelogAggregate, AggregateKey: *changelogID, BaseRevision: publishedRevisionID, Snapshot: raw, Reason: snapshot.Reason,
				ActorID: job.ActorID, Source: "auto_update", Status: "approved", Metadata: map[string]any{"source": job.SourceType, "externalReleaseId": release.ID, "externalURL": release.URL}})
			if createErr != nil {
				_ = tx.Rollback(ctx)
				return nil, createErr
			}
			if err = applyProjectChangelogSnapshotTx(ctx, tx, internalID, job.RouteID, revision.RevisionID, job.ActorID, snapshot); err == nil {
				metadata, _ := json.Marshal(releaseMetadata)
				_, err = tx.Exec(ctx, `update external_release_bindings set external_body_hash=$2,external_url=$3,metadata=$4::jsonb,updated_at=now() where id=$1`,
					bindingID, bodyHash, release.URL, metadata)
			}
			updated++
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
	}
	return map[string]any{"created": created, "updated": updated, "unchanged": unchanged, "manualOverrideConflicts": manualConflicts,
		"skippedUnknownMinecraftVersions": skippedUnknownVersions}, nil
}

func (worker *ProjectAutomationWorker) currentProjectVersions(ctx context.Context, job projectAutomationJob) []string {
	var versions []string
	if job.ProjectType == "mod" {
		_ = worker.server.db.QueryRow(ctx, `select coalesce(array_agg(distinct minecraft_version order by minecraft_version),'{}'::text[])
			from mod_loader_compatibilities where mod_id=$1`, job.InternalID).Scan(&versions)
	} else {
		_ = worker.server.db.QueryRow(ctx, `select minecraft_versions from simple_projects where id=$1 and project_type=$2`, job.InternalID, job.ProjectType).Scan(&versions)
	}
	return versions
}

func (worker *ProjectAutomationWorker) checkRedistribution(ctx context.Context, job projectAutomationJob) error {
	var license string
	if job.ProjectType == "mod" {
		_ = worker.server.db.QueryRow(ctx, `select license from mods where id=$1`, job.InternalID).Scan(&license)
	} else {
		_ = worker.server.db.QueryRow(ctx, `select license from simple_projects where id=$1 and project_type=$2`, job.InternalID, job.ProjectType).Scan(&license)
	}
	var allowed bool
	err := worker.server.db.QueryRow(ctx, `select redistribution_allowed from license_policies where lower(spdx_id)=lower($1)`, strings.TrimSpace(license)).Scan(&allowed)
	if err == nil && allowed {
		return nil
	}
	if job.LicenseOverride && strings.TrimSpace(job.OverrideReason) != "" && strings.TrimSpace(job.OverrideSource) != "" {
		return nil
	}
	return fmt.Errorf("license %q is not approved for redistribution", license)
}

func (worker *ProjectAutomationWorker) mirrorFiles(ctx context.Context, job projectAutomationJob, cfg modImportConfig, files []providerProjectFile) (map[string]any, error) {
	client, ossCfg, err := worker.server.ossClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("OSS unavailable: %w", err)
	}
	queued, existing, awaitingScan, scanFailures, review := 0, 0, 0, 0, 0
	result := func() map[string]any {
		return map[string]any{
			"queuedForScan": queued,
			"existing":      existing,
			"awaitingScan":  awaitingScan,
			"scanFailures":  scanFailures,
			"needsReview":   review,
		}
	}
	for _, file := range files {
		if queued >= projectAutomationMaxFilesPerRun {
			break
		}
		if file.SizeBytes <= 0 || file.SizeBytes > projectAutomationMaxFileBytes || !validProviderDownloadURL(file.DirectURL) || !projectFileExtensionAllowed(job.ProjectType, strings.ToLower(filepath.Ext(file.FileName))) {
			review++
			continue
		}
		var currentSize int64
		var currentStatus, currentFileStatus, currentScanStatus string
		var currentMetadata []byte
		err = worker.server.db.QueryRow(ctx, `select mirror.byte_size,mirror.status,mirror.metadata,
			coalesce(file.status,''),coalesce(file.scan_status,'')
			from mirrored_project_files mirror left join oss_files file on file.id=mirror.oss_file_id
			where mirror.source_type=$1 and mirror.external_file_id=$2`, job.SourceType, file.ID).
			Scan(&currentSize, &currentStatus, &currentMetadata, &currentFileStatus, &currentScanStatus)
		if err == nil {
			existing++
			var previous providerProjectFile
			if decodeErr := json.Unmarshal(currentMetadata, &previous); decodeErr != nil {
				if _, updateErr := worker.mutateProjectAutomationRun(ctx, job, `update mirrored_project_files set status='review'
					where source_type=$1 and external_file_id=$2 and status<>'ready'`, job.SourceType, file.ID); updateErr != nil {
					return result(), fmt.Errorf("mark corrupt project mirror metadata for review: %w", updateErr)
				}
				review++
				continue
			}
			previousSignature, incomingSignature := providerFileHashSignature(previous), providerFileHashSignature(file)
			if currentSize != file.SizeBytes || previousSignature != "" && incomingSignature != "" && previousSignature != incomingSignature {
				if _, updateErr := worker.mutateProjectAutomationRun(ctx, job, `update mirrored_project_files set status='source_changed'
					where source_type=$1 and external_file_id=$2`, job.SourceType, file.ID); updateErr != nil {
					return result(), fmt.Errorf("mark changed project mirror source: %w", updateErr)
				}
				review++
				continue
			}
			switch currentStatus {
			case "failed":
				scanFailures++
				review++
			case "review", "source_changed":
				review++
			case "scanning", "ready":
				if currentScanStatus == "rejected" || currentFileStatus == "deleted" || currentFileStatus == "" {
					if _, updateErr := worker.mutateProjectAutomationRun(ctx, job, `update mirrored_project_files set status='failed'
						where source_type=$1 and external_file_id=$2 and status in ('scanning','ready')`, job.SourceType, file.ID); updateErr != nil {
						return result(), fmt.Errorf("mark rejected project mirror scan: %w", updateErr)
					}
					scanFailures++
					review++
				} else if currentScanStatus == "pending" {
					if _, updateErr := worker.mutateProjectAutomationRun(ctx, job, `update mirrored_project_files set status='scanning'
						where source_type=$1 and external_file_id=$2 and status='ready'`, job.SourceType, file.ID); updateErr != nil {
						return result(), fmt.Errorf("mark project mirror rescan pending: %w", updateErr)
					}
					awaitingScan++
				} else if currentStatus == "scanning" {
					awaitingScan++
				}
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return result(), fmt.Errorf("check existing project mirror: %w", err)
		}
		objectKey := buildOSSObjectKeyForFile(ossCfg.Prefix, "project/download/automated/"+job.ProjectPublicID, file.FileName)
		size, digest, downloadErr := worker.uploadProviderFile(ctx, client, ossCfg, objectKey, file, cfg)
		if downloadErr != nil {
			return result(), downloadErr
		}
		metadata, _ := json.Marshal(file)
		tx, txErr := worker.server.db.Begin(ctx)
		if txErr != nil {
			return nil, txErr
		}
		var ossFileID int64
		txErr = lockProjectAutomationRunTx(ctx, tx, job)
		if txErr == nil {
			txErr = tx.QueryRow(ctx, `insert into oss_files(bucket,endpoint,region,object_key,category,source,original_name,source_original_name,
			content_type,size_bytes,source_size_bytes,sha256,status,scan_status) values($1,$2,$3,$4,$5,'project_auto_update',$6,$6,
			'application/octet-stream',$7,$7,$8,'active','pending') returning id`, ossCfg.Bucket, ossCfg.displayEndpoint(), ossCfg.Region,
				objectKey, ossCategoryFromObjectKey(objectKey, ossCfg.Prefix), file.FileName, size, digest).Scan(&ossFileID)
		}
		if txErr == nil {
			_, txErr = tx.Exec(ctx, `insert into mirrored_project_files(project_route_id,source_type,external_file_id,file_sha256,byte_size,oss_file_id,
			license_spdx_id,status,metadata) values($1,$2,$3,$4,$5,$6,coalesce(
				(select license from mods where $7='mod' and id=$8),
				(select license from simple_projects where $7<>'mod' and id=$8 and project_type=$7)),
				'scanning',$9::jsonb)`, job.RouteID, job.SourceType, file.ID, digest, size, ossFileID, job.ProjectType, job.InternalID, metadata)
		}
		if txErr == nil {
			txErr = tx.Commit(ctx)
		} else {
			_ = tx.Rollback(ctx)
		}
		if txErr != nil {
			_ = tx.Rollback(ctx)
			cleanupErr := worker.cleanupUnregisteredProjectMirror(ctx, client, ossCfg, objectKey)
			if cleanupErr != nil {
				return nil, errors.Join(txErr, cleanupErr)
			}
			return nil, txErr
		}
		queued++
	}
	if scanFailures > 0 {
		return result(), fmt.Errorf("%w: %d file(s) require a clean rescan", errProjectMirrorScanRejected, scanFailures)
	}
	return result(), nil
}

func (worker *ProjectAutomationWorker) cleanupUnregisteredProjectMirror(ctx context.Context, client *aliyunoss.Client, cfg ossConfigPayload, objectKey string) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	var registered bool
	if err := worker.server.db.QueryRow(cleanupCtx, `select exists(select 1 from oss_files where object_key=$1)`, objectKey).Scan(&registered); err != nil {
		return fmt.Errorf("verify failed project mirror registration before cleanup: %w", err)
	}
	if registered {
		return nil
	}
	if _, err := client.DeleteObject(cleanupCtx, &aliyunoss.DeleteObjectRequest{
		Bucket: aliyunoss.Ptr(cfg.Bucket),
		Key:    aliyunoss.Ptr(objectKey),
	}); err == nil {
		return nil
	} else {
		directDeleteErr := err
		tx, beginErr := worker.server.db.Begin(cleanupCtx)
		if beginErr != nil {
			return errors.Join(fmt.Errorf("delete unregistered project mirror: %w", directDeleteErr), fmt.Errorf("begin durable cleanup: %w", beginErr))
		}
		defer tx.Rollback(cleanupCtx)
		queueErr := enqueueOSSObjectDeletionTx(cleanupCtx, tx, ossDeletionTarget{
			Bucket: cfg.Bucket, Endpoint: cfg.Endpoint, Region: cfg.Region, UseCName: cfg.UseCName || isCustomOSSEndpoint(cfg.Endpoint),
			ObjectKey: objectKey, Reason: "project-automation-registration-failed",
		})
		if queueErr == nil {
			queueErr = tx.Commit(cleanupCtx)
		}
		if queueErr != nil {
			return errors.Join(fmt.Errorf("delete unregistered project mirror: %w", directDeleteErr), fmt.Errorf("queue durable cleanup: %w", queueErr))
		}
		return fmt.Errorf("direct project mirror cleanup failed; durable deletion queued: %w", directDeleteErr)
	}
}

func providerFileHashSignature(file providerProjectFile) string {
	if value := strings.ToLower(strings.TrimSpace(file.SHA512)); value != "" {
		return "sha512:" + value
	}
	if value := strings.ToLower(strings.TrimSpace(file.SHA1)); value != "" {
		return "sha1:" + value
	}
	return ""
}

type spooledProviderFile struct {
	File   *os.File
	Path   string
	Size   int64
	SHA256 string
}

func (file *spooledProviderFile) Close() {
	if file == nil {
		return
	}
	if file.File != nil {
		_ = file.File.Close()
	}
	if file.Path != "" {
		_ = os.Remove(file.Path)
	}
}

func (worker *ProjectAutomationWorker) uploadProviderFile(ctx context.Context, client *aliyunoss.Client, ossCfg ossConfigPayload,
	objectKey string, file providerProjectFile, cfg modImportConfig) (int64, string, error) {
	acquireCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.RequestTimeoutSeconds)*time.Second)
	defer cancel()
	slot, err := acquireProjectAutomationMirrorSlot(acquireCtx, worker.server.db)
	if err != nil {
		return 0, "", fmt.Errorf("acquire global project mirror capacity: %w", err)
	}
	defer slot.Release()

	spooled, err := downloadProviderFile(ctx, file, cfg)
	if err != nil {
		return 0, "", err
	}
	defer spooled.Close()
	_, err = client.PutObject(ctx, &aliyunoss.PutObjectRequest{
		Bucket: aliyunoss.Ptr(ossCfg.Bucket), Key: aliyunoss.Ptr(objectKey), ContentType: aliyunoss.Ptr("application/octet-stream"),
		ContentLength: aliyunoss.Ptr(spooled.Size), Body: spooled.File, Metadata: map[string]string{"sha256": spooled.SHA256},
	})
	if err != nil {
		return 0, "", err
	}
	return spooled.Size, spooled.SHA256, nil
}

func downloadProviderFile(ctx context.Context, file providerProjectFile, cfg modImportConfig) (*spooledProviderFile, error) {
	client, err := newProviderHTTPClient(time.Duration(cfg.RequestTimeoutSeconds)*time.Second, file.DirectURL)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, file.DirectURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", cfg.UserAgent)
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d while downloading provider file", response.StatusCode)
	}
	if response.ContentLength > projectAutomationMaxFileBytes {
		return nil, errors.New("provider file exceeds the safe mirror limit")
	}
	if response.ContentLength >= 0 && file.SizeBytes > 0 && response.ContentLength != file.SizeBytes {
		return nil, errors.New("provider file size mismatch")
	}
	return spoolProviderFile(response.Body, file, projectAutomationMaxFileBytes)
}

func spoolProviderFile(source io.Reader, file providerProjectFile, maximumBytes int64) (*spooledProviderFile, error) {
	if maximumBytes < 1 || maximumBytes > projectAutomationMaxFileBytes {
		return nil, errors.New("invalid provider file limit")
	}
	temporary, err := os.CreateTemp("", "mcmods-project-mirror-*")
	if err != nil {
		return nil, err
	}
	result := &spooledProviderFile{File: temporary, Path: temporary.Name()}
	fail := func(cause error) (*spooledProviderFile, error) {
		result.Close()
		return nil, cause
	}
	sha1Hash := sha1.New()
	sha256Hash := sha256.New()
	sha512Hash := sha512.New()
	writer := io.MultiWriter(temporary, sha1Hash, sha256Hash, sha512Hash)
	written, err := io.CopyBuffer(writer, io.LimitReader(source, maximumBytes+1), make([]byte, projectAutomationMirrorBufferBytes))
	if err != nil {
		return fail(err)
	}
	if written > maximumBytes {
		return fail(errors.New("provider file exceeds the safe mirror limit"))
	}
	if file.SizeBytes > 0 && written != file.SizeBytes {
		return fail(errors.New("provider file size mismatch"))
	}
	if file.SHA1 != "" {
		if !strings.EqualFold(hex.EncodeToString(sha1Hash.Sum(nil)), file.SHA1) {
			return fail(errors.New("provider SHA-1 mismatch"))
		}
	}
	if file.SHA512 != "" {
		if !strings.EqualFold(hex.EncodeToString(sha512Hash.Sum(nil)), file.SHA512) {
			return fail(errors.New("provider SHA-512 mismatch"))
		}
	}
	if _, err = temporary.Seek(0, io.SeekStart); err != nil {
		return fail(err)
	}
	result.Size = written
	result.SHA256 = hex.EncodeToString(sha256Hash.Sum(nil))
	return result, nil
}

type projectAutomationMirrorSlot struct {
	connection *pgxpool.Conn
	index      int
}

func acquireProjectAutomationMirrorSlot(ctx context.Context, db *pgxpool.Pool) (*projectAutomationMirrorSlot, error) {
	if db == nil {
		return nil, errors.New("project automation database is required")
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		connection, err := db.Acquire(ctx)
		if err != nil {
			return nil, err
		}
		for index := 1; index <= projectAutomationMirrorGlobalConcurrency; index++ {
			var acquired bool
			err = connection.QueryRow(ctx, `select pg_try_advisory_lock(hashtext('mcmods.project_auto_update.mirror'),$1::integer)`, index).Scan(&acquired)
			if err != nil {
				connection.Release()
				return nil, err
			}
			if acquired {
				return &projectAutomationMirrorSlot{connection: connection, index: index}, nil
			}
		}
		connection.Release()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (slot *projectAutomationMirrorSlot) Release() {
	if slot == nil || slot.connection == nil {
		return
	}
	connection := slot.connection
	slot.connection = nil
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	var released bool
	err := connection.QueryRow(ctx, `select pg_advisory_unlock(hashtext('mcmods.project_auto_update.mirror'),$1::integer)`, slot.index).Scan(&released)
	cancel()
	if err != nil || !released {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = connection.Hijack().Close(closeCtx)
		closeCancel()
		return
	}
	connection.Release()
}

func (worker *ProjectAutomationWorker) promoteCleanMirrors(ctx context.Context) error {
	rows, err := worker.server.db.Query(ctx, `select mirror.id,mirror.project_route_id,route.entity_type,route.internal_id,
		coalesce(mirror.oss_file_id,0),mirror.file_sha256,mirror.byte_size,mirror.metadata,mirror.status,
		coalesce(file.status,''),coalesce(file.scan_status,'')
		from mirrored_project_files mirror join public_routes route on route.id=mirror.project_route_id
		left join oss_files file on file.id=mirror.oss_file_id
		where (mirror.status in ('scanning','ready') and (file.id is null or file.status='deleted' or file.scan_status='rejected'))
		or (mirror.status='ready' and file.scan_status='pending')
		or (mirror.status in ('scanning','failed') and file.status='active' and file.scan_status in ('clean','trusted_generated'))
		order by mirror.id limit 50`)
	if err != nil {
		return fmt.Errorf("read project mirror scan transitions: %w", err)
	}
	type pending struct {
		id, routeID, internalID, ossID, size int64
		projectType, sha                     string
		metadata                             []byte
		mirrorStatus, fileStatus, scanStatus string
	}
	items := make([]pending, 0)
	for rows.Next() {
		var item pending
		if err = rows.Scan(&item.id, &item.routeID, &item.projectType, &item.internalID, &item.ossID, &item.sha, &item.size,
			&item.metadata, &item.mirrorStatus, &item.fileStatus, &item.scanStatus); err != nil {
			rows.Close()
			return fmt.Errorf("scan project mirror transition: %w", err)
		}
		items = append(items, item)
	}
	if err = finishRows(rows); err != nil {
		return fmt.Errorf("iterate project mirror transitions: %w", err)
	}
	var catalog minecraftVersionConfig
	catalogLoaded := false
	for _, item := range items {
		if item.scanStatus == "rejected" || item.fileStatus == "deleted" || item.ossID == 0 {
			if _, err = worker.server.db.Exec(ctx, `update mirrored_project_files set status='failed'
				where id=$1 and status in ('scanning','ready')`, item.id); err != nil {
				return fmt.Errorf("persist rejected project mirror scan: %w", err)
			}
			continue
		}
		if item.mirrorStatus == "ready" && item.scanStatus == "pending" {
			if _, err = worker.server.db.Exec(ctx, `update mirrored_project_files set status='scanning'
				where id=$1 and status='ready'`, item.id); err != nil {
				return fmt.Errorf("persist pending project mirror rescan: %w", err)
			}
			continue
		}
		if !catalogLoaded {
			catalog, err = loadMinecraftVersionConfig(ctx, worker.server.db)
			if err != nil {
				return fmt.Errorf("load Minecraft versions for project mirror promotion: %w", err)
			}
			catalogLoaded = true
		}
		var file providerProjectFile
		if decodeErr := json.Unmarshal(item.metadata, &file); decodeErr != nil {
			if _, err = worker.server.db.Exec(ctx, `update mirrored_project_files set status='review'
				where id=$1 and status in ('scanning','failed')`, item.id); err != nil {
				return fmt.Errorf("persist corrupt project mirror metadata review: %w", err)
			}
			continue
		}
		resolvedVersions, _, resolveErr := classifyMinecraftVersionCodes(catalog, file.GameVersions, 100)
		if resolveErr != nil || len(resolvedVersions) == 0 {
			if _, err = worker.server.db.Exec(ctx, `update mirrored_project_files set status='review'
				where id=$1 and status in ('scanning','failed')`, item.id); err != nil {
				return fmt.Errorf("persist project mirror version review: %w", err)
			}
			continue
		}
		tx, beginErr := worker.server.db.Begin(ctx)
		if beginErr != nil {
			return fmt.Errorf("begin project mirror promotion: %w", beginErr)
		}
		var lockedStatus string
		if lockErr := tx.QueryRow(ctx, `select status from mirrored_project_files where id=$1 for update`, item.id).Scan(&lockedStatus); lockErr != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("lock project mirror promotion: %w", lockErr)
		}
		if lockedStatus != "scanning" && lockedStatus != "failed" {
			_ = tx.Rollback(ctx)
			continue
		}
		// The first scan is only a candidate list. Hold the actual bound file
		// through publication so a concurrent rescan or tombstone cannot turn
		// an obsolete clean result into an active downloadable file.
		var liveStatus, liveScan string
		fileErr := tx.QueryRow(ctx, `select file.status,file.scan_status from oss_files file
			join mirrored_project_files mirror on mirror.oss_file_id=file.id
			where mirror.id=$1 and file.id=$2 for share of file`, item.id, item.ossID).Scan(&liveStatus, &liveScan)
		if fileErr != nil && !errors.Is(fileErr, pgx.ErrNoRows) {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("recheck bound project mirror scan: %w", fileErr)
		}
		if fileErr != nil || liveStatus != "active" || liveScan != "clean" && liveScan != "trusted_generated" {
			status := "failed"
			if liveStatus == "active" && liveScan == "pending" {
				status = "scanning"
			}
			if _, fileErr = tx.Exec(ctx, `update mirrored_project_files set status=$2 where id=$1`, item.id, status); fileErr == nil {
				fileErr = tx.Commit(ctx)
			} else {
				_ = tx.Rollback(ctx)
			}
			if fileErr != nil {
				return fmt.Errorf("persist changed project mirror scan: %w", fileErr)
			}
			continue
		}
		projectApproved, insertErr := projectFileTargetIsApprovedTx(ctx, tx, item.projectType, item.internalID)
		var filePublicID string
		const publicationGeneration = 1
		if insertErr == nil {
			insertErr = tx.QueryRow(ctx, `insert into project_files(project_type,project_internal_id,oss_file_id,display_name,version_name,release_channel,
				game_versions,loaders,file_name,content_type,size_bytes,sha256,status,publication_generation)
				values($1,$2,$3,$4,$5,$6,$7,$8,$9,'application/octet-stream',$10,$11,'active',$12)
				on conflict(project_type,project_internal_id,oss_file_id) do nothing returning public_id`, item.projectType, item.internalID, item.ossID,
				firstNonEmpty(file.DisplayName, file.FileName), firstNonEmpty(file.VersionName, file.DisplayName), normalizeReleaseChannel(file.ReleaseChannel),
				resolvedVersions, normalizeLoaders(file.Loaders), file.FileName, item.size, item.sha, publicationGeneration).Scan(&filePublicID)
		}
		inserted := insertErr == nil
		if errors.Is(insertErr, pgx.ErrNoRows) {
			insertErr = nil
		}
		if insertErr == nil && inserted && projectApproved {
			insertErr = enqueueProjectFileUpdateEventByRouteTx(ctx, tx, item.routeID, 0,
				"download_added", filePublicID, publicationGeneration)
		}
		if insertErr == nil {
			_, insertErr = tx.Exec(ctx, `update mirrored_project_files set status='ready' where id=$1`, item.id)
		}
		if insertErr == nil {
			if commitErr := tx.Commit(ctx); commitErr != nil {
				return fmt.Errorf("commit project mirror promotion: %w", commitErr)
			}
		} else {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("promote project mirror %d: %w", item.id, insertErr)
		}
	}
	return nil
}
