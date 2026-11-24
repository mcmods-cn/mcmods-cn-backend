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

var errRedistributionNotAllowed = errors.New("license is not approved for redistribution")

var errProjectAutomationParentUnavailable = errors.New("automation target is not approved")

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
	ConfiguredAt    time.Time
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
		setting.update_kind,setting.source_type,source.external_project_id,setting.interval_code,setting.updated_at,
		setting.license_override,setting.license_override_reason,setting.license_override_source
		from project_auto_update_runs run
		join project_auto_update_settings setting on setting.id=run.setting_id
		join public_routes route on route.id=setting.project_route_id
		join project_external_sources source on source.project_route_id=setting.project_route_id and source.source_type=setting.source_type
		where run.status='pending' and run.next_attempt_at<=now()
		order by run.created_at,run.id for update of run skip locked limit 1`).Scan(
		&job.RunID, &job.SettingID, &job.RouteID, &job.InternalID, &job.ProjectType, &job.ProjectPublicID,
		&job.Kind, &job.SourceType, &job.ExternalID, &job.Interval, &job.ConfiguredAt,
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
	if err = updateProjectAutomationRunSummaryTx(ctx, tx, job, "completed", "", "", true); err != nil {
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
	if err = updateProjectAutomationRunSummaryTx(ctx, tx, job, status, code, message, terminal); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (worker *ProjectAutomationWorker) execute(ctx context.Context, job projectAutomationJob) (map[string]any, string, error) {
	if _, err := authorizeAutomationProject(ctx, worker.server.db, job.ActorID, job.ProjectPublicID, false, true); err != nil {
		return nil, "automation_permission_denied", err
	}
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
		if errors.Is(updateErr, errProjectAutomationParentUnavailable) {
			return result, "update_conflict", updateErr
		}
		if errors.Is(updateErr, errAutomationPermissionDenied) {
			return result, "automation_permission_denied", updateErr
		}
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
		if errors.Is(updateErr, errProjectAutomationParentUnavailable) {
			return result, "update_conflict", updateErr
		}
		if errors.Is(updateErr, errAutomationPermissionDenied) {
			return result, "automation_permission_denied", updateErr
		}
		result, maintenanceErr := worker.withMaintenanceResult(ctx, job, result, latestProviderReleaseActivity(releases))
		if updateErr == nil && maintenanceErr != nil {
			return result, "maintenance_status_update_failed", maintenanceErr
		}
		return result, "update_conflict", updateErr
	case "site_downloads":
		if _, err = requireProjectRedistribution(ctx, worker.server.db, job); err != nil {
			if errors.Is(err, errRedistributionNotAllowed) {
				return nil, "license_denied", err
			}
			return nil, "configuration_error", err
		}
		files, loadErr := worker.loadProviderFiles(ctx, job, cfg)
		if loadErr != nil {
			return nil, providerFailureCode(loadErr), loadErr
		}
		result, mirrorErr := worker.mirrorFiles(ctx, job, cfg, files)
		switch {
		case errors.Is(mirrorErr, errRedistributionNotAllowed):
			return result, "license_denied", mirrorErr
		case errors.Is(mirrorErr, errAutomationPermissionDenied):
			return result, "automation_permission_denied", mirrorErr
		case errors.Is(mirrorErr, errProjectAutomationParentUnavailable):
			return result, "update_conflict", mirrorErr
		}
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
	aggregate := simpleProjectAggregate
	if job.ProjectType == "mod" {
		aggregate = "mod"
	}
	target, err := lockProjectRevisionTargetTx(ctx, tx, aggregate, job.InternalID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && (target.reviewStatus != "approved" || target.publicID != job.ProjectPublicID || target.projectType != job.ProjectType) {
		return nil, errProjectAutomationParentUnavailable
	}
	if err != nil {
		return nil, err
	}
	if _, err = authorizeAutomationProject(ctx, tx, job.ActorID, job.ProjectPublicID, false, true); err != nil {
		return nil, err
	}
	var added int64
	if job.ProjectType == "mod" {
		if len(pairs) > 0 {
			pairLoaders := make([]string, 0, len(pairs))
			pairVersions := make([]string, 0, len(pairs))
			for _, pair := range pairs {
				pairLoaders = append(pairLoaders, pair[0])
				pairVersions = append(pairVersions, pair[1])
			}
			tag, err := tx.Exec(ctx, `insert into mod_loader_compatibilities(mod_id,loader,minecraft_version)
                select $1,item.loader,item.version from unnest($2::text[],$3::text[]) item(loader,version)
                order by item.loader,item.version on conflict do nothing`, job.InternalID, pairLoaders, pairVersions)
			if err != nil {
				return nil, err
			}
			added = tag.RowsAffected()
		}
	} else {
		err = tx.QueryRow(ctx, `with additions as (
            select array(select v from unnest($3::text[]) v where v<>all(p.minecraft_versions) order by v) versions,
                array(select l from unnest($4::text[]) l where l<>all(p.loaders) order by l) loaders
            from simple_projects p where p.id=$1 and p.project_type=$2
        ), updated as (
            update simple_projects p set minecraft_versions=p.minecraft_versions||a.versions,loaders=p.loaders||a.loaders,updated_at=now()
            from additions a where p.id=$1 and p.project_type=$2 and (cardinality(a.versions)>0 or cardinality(a.loaders)>0)
            returning cardinality(a.versions)+cardinality(a.loaders) as added
        ) select coalesce(max(added),0) from updated`, job.InternalID, job.ProjectType, mapKeys(recognizedVersions), mapKeys(recognizedLoaders)).Scan(&added)
		if err != nil {
			return nil, err
		}
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
			modrinthProjectVersion
			Changelog string `json:"changelog"`
		}
		if err = getProviderJSON(ctx, client, cfg.Modrinth.BaseURL+"/project/"+url.PathEscape(job.ExternalID)+"/version", publicProviderHeaders(cfg.UserAgent), &raw); err != nil {
			return nil, err
		}
		result := make([]projectAutomationRelease, 0, len(raw))
		for _, item := range raw {
			if !publicModrinthVersionStatus(item.Status) {
				continue
			}
			published, _ := time.Parse(time.RFC3339, item.DatePublished)
			release := projectAutomationRelease{ID: item.ID, Version: firstNonEmpty(item.VersionNumber, item.Name, item.ID), Body: item.Changelog,
				PublishedAt: published, GameVersions: item.GameVersions, Loaders: normalizeLoaders(item.Loaders), URL: "https://modrinth.com/project/" + job.ExternalID + "/version/" + item.ID}
			for _, file := range item.Files {
				release.Files = append(release.Files, providerProjectFile{ID: item.ID + ":" + file.FileName, Source: "modrinth", DisplayName: item.Name,
					FileName: file.FileName, VersionName: release.Version, ReleaseChannel: normalizeReleaseChannel(item.VersionType), GameVersions: item.GameVersions,
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
				Data *string `json:"data"`
			}
			if err := getProviderJSON(ctx, client, cfg.CurseForge.BaseURL+"/mods/"+url.PathEscape(job.ExternalID)+"/files/"+url.PathEscape(file.ID)+"/changelog", headers, &response); err != nil {
				return nil, fmt.Errorf("read CurseForge file changelog: %w", err)
			}
			if response.Data == nil {
				return nil, errors.New("CurseForge file changelog response omitted its text")
			}
			result = append(result, projectAutomationRelease{ID: file.ID, Version: firstNonEmpty(file.VersionName, file.DisplayName, file.ID), Body: *response.Data,
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
		Draft       *bool     `json:"draft"`
		Assets      []struct {
			ID                 int64  `json:"id"`
			Name               string `json:"name"`
			Size               int64  `json:"size"`
			DownloadCount      int64  `json:"download_count"`
			BrowserDownloadURL string `json:"browser_download_url"`
			ContentType        string `json:"content_type"`
		} `json:"assets"`
	}
	if err = getProviderJSON(ctx, client, cfg.GitHub.BaseURL+"/repos/"+repository+"/releases?per_page=100", publicProviderHeaders(cfg.UserAgent), &raw); err != nil {
		return nil, err
	}
	result := make([]projectAutomationRelease, 0, len(raw))
	for _, item := range raw {
		if item.Draft == nil || *item.Draft {
			continue
		}
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
	var currentVersions []string
	for _, release := range releases {
		if len(release.GameVersions) == 0 {
			currentVersions, err = worker.currentProjectVersions(ctx, job)
			if err != nil {
				return nil, fmt.Errorf("load project versions for changelog synchronization: %w", err)
			}
			break
		}
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
		_, status, err := readProjectNameAndStatus(ctx, tx, job.ProjectType, job.InternalID, true)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && status != "approved") {
			_ = tx.Rollback(ctx)
			return nil, errProjectAutomationParentUnavailable
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		}
		binding, lookupErr := lockManagedChangelogReleaseTx(ctx, tx, job.RouteID, job.SourceType, release.ID, bodyHash)
		if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
			_ = tx.Rollback(ctx)
			return nil, lookupErr
		}
		if _, err = authorizeAutomationProject(ctx, tx, job.ActorID, job.ProjectPublicID, false, true); err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		}
		if errors.Is(lookupErr, pgx.ErrNoRows) {
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
		} else if binding.bodyHash == bodyHash {
			unchanged++
		} else if binding.manual || binding.publicID == nil {
			manualConflicts++
			metadata, _ := json.Marshal(map[string]any{"upstreamChanged": true, "latestHash": bodyHash, "externalURL": release.URL,
				"minecraftVersions": release.GameVersions, "loaders": release.Loaders, "rawMinecraftVersions": rawMinecraftVersions,
				"unmappedMinecraftVersions": unmappedVersions})
			_, err = tx.Exec(ctx, `update external_release_bindings set metadata=metadata||$4::jsonb,external_url=$5,updated_at=now()
				where project_route_id=$1 and source_type=$2 and external_release_id=$3`,
				job.RouteID, job.SourceType, release.ID, metadata, release.URL)
		} else {
			if binding.entryID == 0 {
				_ = tx.Rollback(ctx)
				return nil, pgx.ErrNoRows
			}
			snapshot := projectChangelogSnapshot{EventAt: release.PublishedAt, MinecraftVersions: uniqueTrimmed(release.GameVersions, 100), ProjectVersion: release.Version,
				DefaultLocale: "en-US", Localizations: []projectChangelogLocalization{{Locale: "en-US", BodyMarkdown: body}}, Reason: "Automated external release synchronization"}
			raw, _ := json.Marshal(snapshot)
			revision, createErr := createContentRevisionTx(ctx, tx, createContentRevisionParams{EntityType: projectChangelogAggregate, EntityID: binding.entryID,
				AggregateType: projectChangelogAggregate, AggregateKey: *binding.publicID, BaseRevision: binding.publishedRevisionID, Snapshot: raw, Reason: snapshot.Reason,
				ActorID: job.ActorID, Source: "auto_update", Status: "approved", Metadata: map[string]any{"source": job.SourceType, "externalReleaseId": release.ID, "externalURL": release.URL}})
			if createErr != nil {
				_ = tx.Rollback(ctx)
				return nil, createErr
			}
			if err = applyProjectChangelogSnapshotTx(ctx, tx, binding.entryID, job.RouteID, revision.RevisionID, job.ActorID, snapshot); err == nil {
				metadata, _ := json.Marshal(releaseMetadata)
				_, err = tx.Exec(ctx, `update external_release_bindings set external_body_hash=$4,external_url=$5,metadata=$6::jsonb,updated_at=now()
					where project_route_id=$1 and source_type=$2 and external_release_id=$3`,
					job.RouteID, job.SourceType, release.ID, bodyHash, release.URL, metadata)
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

type managedChangelogRelease struct {
	publicID            *string
	entryID             int64
	publishedRevisionID *int64
	bodyHash            string
	manual              bool
}

func lockManagedChangelogReleaseTx(ctx context.Context, tx pgx.Tx, routeID int64, sourceType, releaseID, bodyHash string) (managedChangelogRelease, error) {
	var binding managedChangelogRelease
	err := tx.QueryRow(ctx, `select changelog_public_id,external_body_hash,manual_override from external_release_bindings
		where project_route_id=$1 and source_type=$2 and external_release_id=$3`,
		routeID, sourceType, releaseID).Scan(&binding.publicID, &binding.bodyHash, &binding.manual)
	if err != nil || binding.bodyHash == bodyHash {
		return binding, err
	}

	// Editors lock the revision aggregate and its entry before marking a binding
	// as manual. Keep the same order and reread the binding after waiting.
	if binding.publicID != nil {
		if err = lockContentAggregateTx(ctx, tx, projectChangelogAggregate, *binding.publicID); err != nil {
			return binding, fmt.Errorf("lock content aggregate: %w", err)
		}
		err = tx.QueryRow(ctx, `select id,published_revision_id from project_changelogs
			where public_id=$1 and object_route_id=$2 and status='active' for update`,
			*binding.publicID, routeID).Scan(&binding.entryID, &binding.publishedRevisionID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return binding, fmt.Errorf("lock managed changelog: %w", err)
		}
	}
	err = tx.QueryRow(ctx, `select external_body_hash,manual_override from external_release_bindings
		where project_route_id=$1 and source_type=$2 and external_release_id=$3
		and changelog_public_id is not distinct from $4::text for update`,
		routeID, sourceType, releaseID, binding.publicID).Scan(&binding.bodyHash, &binding.manual)
	if errors.Is(err, pgx.ErrNoRows) {
		return binding, errors.New("managed changelog binding changed while synchronizing release")
	}
	return binding, err
}

func (worker *ProjectAutomationWorker) currentProjectVersions(ctx context.Context, job projectAutomationJob) ([]string, error) {
	var versions []string
	var err error
	if job.ProjectType == "mod" {
		err = worker.server.db.QueryRow(ctx, `select coalesce(array_agg(distinct minecraft_version order by minecraft_version),'{}'::text[])
			from mod_loader_compatibilities where mod_id=$1`, job.InternalID).Scan(&versions)
	} else {
		err = worker.server.db.QueryRow(ctx, `select minecraft_versions from simple_projects where id=$1 and project_type=$2`, job.InternalID, job.ProjectType).Scan(&versions)
	}
	return versions, err
}

func requireProjectRedistribution(ctx context.Context, query databaseQuery, job projectAutomationJob) (string, error) {
	license, allowed, err := projectRedistributionPolicy(ctx, query, job.ProjectType, job.InternalID)
	if err != nil {
		return "", fmt.Errorf("read project redistribution policy: %w", err)
	}
	if !allowed && !(job.LicenseOverride && strings.TrimSpace(job.OverrideReason) != "" && strings.TrimSpace(job.OverrideSource) != "") {
		return "", fmt.Errorf("%w: %q", errRedistributionNotAllowed, license)
	}
	return license, nil
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
		var mirrorID, currentSize int64
		var currentStatus, currentFileStatus, currentScanStatus string
		var heldForPublication bool
		var currentMetadata []byte
		err = worker.server.db.QueryRow(ctx, `select mirror.id,mirror.byte_size,mirror.status,mirror.metadata,mirror.metadata ? 'publicationHold',
			coalesce(file.status,''),coalesce(file.scan_status,'')
			from mirrored_project_files mirror left join oss_files file on file.id=mirror.oss_file_id
			where mirror.project_route_id=$1 and mirror.source_type=$2 and mirror.external_file_id=$3`, job.RouteID, job.SourceType, file.ID).
			Scan(&mirrorID, &currentSize, &currentStatus, &currentMetadata, &heldForPublication, &currentFileStatus, &currentScanStatus)
		if err == nil {
			existing++
			var previous providerProjectFile
			if decodeErr := json.Unmarshal(currentMetadata, &previous); decodeErr != nil {
				if _, updateErr := worker.mutateProjectAutomationRun(ctx, job, `update mirrored_project_files set status='review'
					where id=$1 and status<>'ready'`, mirrorID); updateErr != nil {
					return result(), fmt.Errorf("mark corrupt project mirror metadata for review: %w", updateErr)
				}
				review++
				continue
			}
			previousSignature, incomingSignature := providerFileHashSignature(previous), providerFileHashSignature(file)
			if currentSize != file.SizeBytes || previousSignature != "" && incomingSignature != "" && previousSignature != incomingSignature {
				if _, updateErr := worker.mutateProjectAutomationRun(ctx, job, `update mirrored_project_files set status='source_changed'
					where id=$1`, mirrorID); updateErr != nil {
					return result(), fmt.Errorf("mark changed project mirror source: %w", updateErr)
				}
				review++
				continue
			}
			switch currentStatus {
			case "failed":
				scanFailures++
				review++
			case "review":
				if !heldForPublication || previous.ProviderProjectID != job.ExternalID {
					review++
					continue
				}
				// Only a clean mirror held by a publication policy can be reconsidered.
				// Other review reasons (metadata and Minecraft versions) remain manual.
				tag, updateErr := worker.mutateProjectAutomationRun(ctx, job, `update mirrored_project_files
					set status='scanning', metadata=metadata-'publicationHold'
					where id=$1 and status='review' and metadata ? 'publicationHold'
					and metadata->>'providerProjectId'=$2`, mirrorID, job.ExternalID)
				if updateErr != nil {
					return result(), fmt.Errorf("resume project mirror publication: %w", updateErr)
				}
				if tag.RowsAffected() == 1 {
					awaitingScan++
				} else {
					review++
				}
			case "source_changed":
				review++
			case "scanning", "ready":
				if currentScanStatus == "rejected" || currentFileStatus == "deleted" || currentFileStatus == "" {
					if _, updateErr := worker.mutateProjectAutomationRun(ctx, job, `update mirrored_project_files set status='failed'
							where id=$1 and status in ('scanning','ready')`, mirrorID); updateErr != nil {
						return result(), fmt.Errorf("mark rejected project mirror scan: %w", updateErr)
					}
					scanFailures++
					review++
				} else if currentScanStatus == "pending" {
					if _, updateErr := worker.mutateProjectAutomationRun(ctx, job, `update mirrored_project_files set status='scanning'
							where id=$1 and status='ready'`, mirrorID); updateErr != nil {
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
		if err = worker.registerDownloadedProjectMirror(ctx, job, ossCfg, file, objectKey, size, digest); err != nil {
			// The registration may still commit. Only its precommitted expiry owns deletion.
			return result(), err
		}
		queued++
	}
	if scanFailures > 0 {
		return result(), fmt.Errorf("%w: %d file(s) require a clean rescan", errProjectMirrorScanRejected, scanFailures)
	}
	return result(), nil
}

// Verify current project authority after the external download. The run fence
// holds the setting and bound source, including any written redistribution grant.
func (worker *ProjectAutomationWorker) registerDownloadedProjectMirror(ctx context.Context, job projectAutomationJob, cfg ossConfigPayload, file providerProjectFile, objectKey string, size int64, digest string) error {
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockProjectAutomationRunTx(ctx, tx, job); err != nil {
		return err
	}
	approved, err := projectFileTargetIsApprovedTx(ctx, tx, job.ProjectType, job.InternalID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !approved {
		return errProjectAutomationParentUnavailable
	}
	if err != nil {
		return err
	}
	if _, err = authorizeAutomationProject(ctx, tx, job.ActorID, job.ProjectPublicID, false, true); err != nil {
		return err
	}
	license, err := requireProjectRedistribution(ctx, tx, job)
	if err != nil {
		return err
	}
	// The external project is taken from the validated source binding. Keep it
	// with the provider snapshot for inspection after the project is rebound.
	if job.ExternalID != "" {
		file.ProviderProjectID = job.ExternalID
	}
	metadata, err := json.Marshal(file)
	if err != nil {
		return err
	}
	var ossFileID int64
	err = tx.QueryRow(ctx, `insert into oss_files(bucket,endpoint,region,object_key,category,source,original_name,source_original_name,
        content_type,size_bytes,source_size_bytes,sha256,status,scan_status) values($1,$2,$3,$4,$5,'project_auto_update',$6,$6,
        'application/octet-stream',$7,$7,$8,'active','pending') returning id`, cfg.Bucket, cfg.displayEndpoint(), cfg.Region, objectKey, ossCategoryFromObjectKey(objectKey, cfg.Prefix), file.FileName, size, digest).Scan(&ossFileID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `insert into mirrored_project_files(project_route_id,source_type,external_file_id,file_sha256,byte_size,oss_file_id,
        license_spdx_id,status,metadata) values($1,$2,$3,$4,$5,$6,$7,'scanning',$8::jsonb)`, job.RouteID, job.SourceType, file.ID, digest, size, ossFileID, license, metadata)
	if err != nil {
		return err
	}
	// A claimed or previously attempted deletion can still reach the provider.
	// Register only while this exact transfer's unclaimed receipt can be canceled.
	tag, err := tx.Exec(ctx, `delete from oss_object_deletion_outbox
		where bucket=$1 and endpoint=$2 and object_key=$3 and oss_file_id is null and reason=$4
		  and status='pending' and attempts=0 and replay_count=0`, cfg.Bucket, cfg.Endpoint, objectKey, projectMirrorUploadExpiryReason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("project mirror upload is no longer available for registration")
	}
	return tx.Commit(ctx)
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
	ioCtx, stop := context.WithTimeout(ctx, time.Duration(cfg.RequestTimeoutSeconds)*time.Second)
	defer stop()
	if err = putSpooledProjectMirror(ioCtx, slot.connection, client, ossCfg, objectKey, spooled); err != nil {
		return 0, "", err
	}
	return spooled.Size, spooled.SHA256, nil
}

const projectMirrorUploadExpiryReason = "abandoned_project_mirror_upload"

func putSpooledProjectMirror(ctx context.Context, conn *pgxpool.Conn, client *aliyunoss.Client, cfg ossConfigPayload, key string, file *spooledProviderFile) error {
	// This insert commits on the single connection already reserved by the global
	// transfer slot. A lost acknowledgement leaves a durable, delayed cleanup.
	id, err := scheduleOSSUploadExpiry(ctx, conn, cfg, key, projectMirrorUploadExpiryReason, nil)
	if err != nil {
		return err
	}
	guard, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer guard.Rollback(ctx)
	if err = guard.QueryRow(ctx, `select id from oss_object_deletion_outbox
		where id=$1 and status='pending' and attempts=0 and replay_count=0 for update`, id).Scan(&id); err != nil {
		return err
	}
	_, err = client.PutObject(ctx, &aliyunoss.PutObjectRequest{
		Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(key), ContentType: aliyunoss.Ptr("application/octet-stream"),
		ContentLength: aliyunoss.Ptr(file.Size), Body: file.File, Metadata: map[string]string{"sha256": file.SHA256},
	})
	return err
}

func downloadProviderFile(ctx context.Context, file providerProjectFile, cfg modImportConfig) (*spooledProviderFile, error) {
	return downloadProviderFileWithClientFactory(ctx, file, cfg, newProviderDownloadHTTPClient)
}

// Production always enters through the fixed safe factory above. The explicit
// private dependency permits bounded TLS download regressions without changing
// DNS, redirect policy, trusted origins, or a runtime/global setting.
func downloadProviderFileWithClientFactory(ctx context.Context, file providerProjectFile, cfg modImportConfig, newClient func(time.Duration, string, ...string) (*http.Client, error)) (*spooledProviderFile, error) {
	client, err := newClient(time.Duration(cfg.RequestTimeoutSeconds)*time.Second, file.DirectURL, providerDownloadTrustedBase(file.Source, cfg))
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
	rows, err := worker.server.db.Query(ctx, `select mirror.id,mirror.project_route_id,route.entity_type,route.internal_id,route.public_id,mirror.source_type,
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
	items := make([]projectMirrorCandidate, 0)
	for rows.Next() {
		var item projectMirrorCandidate
		if err = rows.Scan(&item.id, &item.routeID, &item.projectType, &item.internalID, &item.projectID, &item.source, &item.ossID, &item.sha, &item.size,
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
		if err = worker.promoteScannedMirror(ctx, item, file, resolvedVersions); err != nil {
			return fmt.Errorf("promote project mirror %d: %w", item.id, err)
		}
	}
	return nil
}

type projectMirrorCandidate struct {
	id, routeID, internalID, ossID, size int64
	projectType, projectID, source, sha  string
	metadata                             []byte
	mirrorStatus, fileStatus, scanStatus string
}

// The source/configuration locks precede the file and mirror locks, as on
// normal runs. A completed publication stays bound to its own scanned bytes.
func (worker *ProjectAutomationWorker) promoteScannedMirror(ctx context.Context, item projectMirrorCandidate, file providerProjectFile, versions []string) error {
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	job := projectAutomationJob{RouteID: item.routeID, ProjectType: item.projectType, InternalID: item.internalID,
		ProjectPublicID: item.projectID, SourceType: item.source, ExternalID: file.ProviderProjectID}
	err = tx.QueryRow(ctx, `select setting.license_override,setting.license_override_reason,setting.license_override_source
		from project_auto_update_settings setting join project_external_sources source
		on source.project_route_id=setting.project_route_id and source.source_type=setting.source_type
		where setting.project_route_id=$1 and setting.update_kind='site_downloads' and setting.source_type=$2
		and source.external_project_id=$3 for share of setting,source`, job.RouteID, job.SourceType, job.ExternalID).
		Scan(&job.LicenseOverride, &job.OverrideReason, &job.OverrideSource)
	sourceMatches := err == nil && job.ExternalID != ""
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	var liveStatus, liveScan string
	err = tx.QueryRow(ctx, `select status,scan_status from oss_files where id=$1 for share`, item.ossID).Scan(&liveStatus, &liveScan)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var lockedStatus string
	var lockedFileID *int64
	if err = tx.QueryRow(ctx, `select status,oss_file_id from mirrored_project_files where id=$1 for update`, item.id).
		Scan(&lockedStatus, &lockedFileID); err != nil {
		return err
	}
	if (lockedStatus != "scanning" && lockedStatus != "failed") || lockedFileID == nil || *lockedFileID != item.ossID {
		return nil
	}
	if liveStatus != "active" || liveScan != "clean" && liveScan != "trusted_generated" {
		status := "failed"
		if liveStatus == "active" && liveScan == "pending" {
			status = "scanning"
		}
		if _, err = tx.Exec(ctx, `update mirrored_project_files set status=$2 where id=$1`, item.id, status); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	var published bool
	if err = tx.QueryRow(ctx, `select exists(select 1 from project_files where project_type=$1 and project_internal_id=$2 and oss_file_id=$3)`,
		item.projectType, item.internalID, item.ossID).Scan(&published); err != nil {
		return err
	}
	if !published {
		reason, policyErr := firstMirrorPublicationHold(ctx, tx, job, sourceMatches)
		if policyErr != nil {
			return policyErr
		}
		if reason != "" {
			if _, err = tx.Exec(ctx, `update mirrored_project_files set status='review',
				metadata=jsonb_set(metadata,'{publicationHold}',to_jsonb($2::text)) where id=$1`, item.id, reason); err != nil {
				return err
			}
			return tx.Commit(ctx)
		}
		var filePublicID string
		const generation = 1
		err = tx.QueryRow(ctx, `insert into project_files(project_type,project_internal_id,oss_file_id,display_name,version_name,release_channel,
			game_versions,loaders,file_name,content_type,size_bytes,sha256,status,publication_generation)
			values($1,$2,$3,$4,$5,$6,$7,$8,$9,'application/octet-stream',$10,$11,'active',$12)
			on conflict(project_type,project_internal_id,oss_file_id) do nothing returning public_id`, item.projectType, item.internalID, item.ossID,
			firstNonEmpty(file.DisplayName, file.FileName), firstNonEmpty(file.VersionName, file.DisplayName), normalizeReleaseChannel(file.ReleaseChannel),
			versions, normalizeLoaders(file.Loaders), file.FileName, item.size, item.sha, generation).Scan(&filePublicID)
		if err == nil {
			if err = enqueueProjectFileUpdateEventByRouteTx(ctx, tx, item.routeID, 0, "download_added", filePublicID, generation); err != nil {
				return err
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `update mirrored_project_files set status='ready' where id=$1`, item.id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func firstMirrorPublicationHold(ctx context.Context, tx pgx.Tx, job projectAutomationJob, sourceMatches bool) (string, error) {
	if !sourceMatches {
		return "source_changed", nil
	}
	approved, err := projectFileTargetIsApprovedTx(ctx, tx, job.ProjectType, job.InternalID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !approved {
		return "project_unavailable", nil
	}
	if err != nil {
		return "", err
	}
	if _, err = authorizeAutomationProject(ctx, tx, 0, job.ProjectPublicID, false, true); err != nil {
		if errors.Is(err, errAutomationPermissionDenied) || errors.Is(err, pgx.ErrNoRows) {
			return "actor_unavailable", nil
		}
		return "", err
	}
	if _, err = requireProjectRedistribution(ctx, tx, job); errors.Is(err, errRedistributionNotAllowed) {
		return "redistribution_unavailable", nil
	}
	return "", err
}
