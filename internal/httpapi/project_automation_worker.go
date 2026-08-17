package httpapi

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	projectAutomationTick           = 30 * time.Second
	projectAutomationLease          = 10 * time.Minute
	projectAutomationMaxFileBytes   = int64(256 << 20)
	projectAutomationMaxFilesPerRun = 25
)

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
	worker.tick(ctx)
	ticker := time.NewTicker(projectAutomationTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			worker.tick(ctx)
		}
	}
}

func (worker *ProjectAutomationWorker) tick(ctx context.Context) {
	_, _ = worker.server.db.Exec(ctx, `update project_auto_update_runs set status='pending',lease_owner='',lease_expires_at=null,
		next_attempt_at=now(),last_error_code='lease_expired',last_error='worker lease expired'
		where status='running' and lease_expires_at<now()`)
	worker.scheduleDue(ctx)
	worker.promoteCleanMirrors(ctx)
	for index := 0; index < 4; index++ {
		processed, err := worker.processOne(ctx)
		if err != nil || !processed {
			return
		}
	}
}

func (worker *ProjectAutomationWorker) scheduleDue(ctx context.Context) {
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)
	var locked bool
	if err = tx.QueryRow(ctx, `select pg_try_advisory_xact_lock(hashtext('mcmods.project_auto_update.schedule'))`).Scan(&locked); err != nil || !locked {
		return
	}
	rows, err := tx.Query(ctx, `select id from project_auto_update_settings setting
		where setting.enabled and setting.next_run_at<=now() and setting.source_type is not null
		and not exists(select 1 from project_auto_update_runs run where run.setting_id=setting.id and run.status in ('pending','running'))
		order by setting.next_run_at,setting.id for update skip locked limit 50`)
	if err != nil {
		return
	}
	ids := make([]int64, 0, 50)
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		_, _ = tx.Exec(ctx, `insert into project_auto_update_runs(setting_id) values($1) on conflict do nothing`, id)
	}
	_ = tx.Commit(ctx)
}

type projectAutomationJob struct {
	RunID           int64
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
	if _, err = tx.Exec(ctx, `update project_auto_update_runs set status='running',lease_owner='project-auto-update',
		lease_expires_at=now()+$2::interval,attempts=attempts+1,started_at=coalesce(started_at,now()) where id=$1`,
		job.RunID, projectAutomationLease.String()); err != nil {
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
	if _, actorErr = worker.server.db.Exec(ctx, `update project_auto_update_runs set actor_id=$2 where id=$1`, job.RunID, job.ActorID); actorErr != nil {
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
		return result, "download_failed", mirrorErr
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
	catalog := loadMinecraftVersionConfig(ctx, worker.server.db)
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
		if err = getProviderJSON(ctx, client, cfg.Modrinth.BaseURL+"/project/"+url.PathEscape(job.ExternalID)+"/version", providerHeaders(cfg.UserAgent, cfg.Modrinth.Token, ""), &raw); err != nil {
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
		headers := providerHeaders(cfg.UserAgent, "", cfg.CurseForge.APIKey)
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
	if err = getProviderJSON(ctx, client, cfg.GitHub.BaseURL+"/repos/"+repository+"/releases?per_page=100", providerHeaders(cfg.UserAgent, cfg.GitHub.Token, ""), &raw); err != nil {
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
	created, updated, unchanged, manualConflicts := 0, 0, 0, 0
	currentVersions := worker.currentProjectVersions(ctx, job)
	for _, release := range releases {
		if release.PublishedAt.IsZero() {
			release.PublishedAt = time.Now().UTC()
		}
		if len(release.GameVersions) == 0 {
			release.GameVersions = currentVersions
		}
		if len(release.GameVersions) == 0 {
			release.GameVersions = []string{"unspecified"}
		}
		body := strings.TrimSpace(release.Body)
		if body == "" {
			body = "[View the external release](" + release.URL + ")"
		}
		hashBytes := sha256.Sum256([]byte(body))
		bodyHash := hex.EncodeToString(hashBytes[:])
		tx, err := worker.server.db.Begin(ctx)
		if err != nil {
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
			metadata, _ := json.Marshal(map[string]any{"minecraftVersions": release.GameVersions, "loaders": release.Loaders})
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
			metadata, _ := json.Marshal(map[string]any{"upstreamChanged": true, "latestHash": bodyHash, "externalURL": release.URL})
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
				_, err = tx.Exec(ctx, `update external_release_bindings set external_body_hash=$2,external_url=$3,updated_at=now() where id=$1`, bindingID, bodyHash, release.URL)
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
	return map[string]any{"created": created, "updated": updated, "unchanged": unchanged, "manualOverrideConflicts": manualConflicts}, nil
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
	queued, existing, review := 0, 0, 0
	for _, file := range files {
		if queued >= projectAutomationMaxFilesPerRun {
			break
		}
		if file.SizeBytes <= 0 || file.SizeBytes > projectAutomationMaxFileBytes || !validProviderDownloadURL(file.DirectURL) || !projectFileExtensionAllowed(job.ProjectType, strings.ToLower(filepath.Ext(file.FileName))) {
			review++
			continue
		}
		var currentSHA string
		var currentSize int64
		var currentStatus string
		var currentMetadata []byte
		err = worker.server.db.QueryRow(ctx, `select file_sha256,byte_size,status,metadata from mirrored_project_files where source_type=$1 and external_file_id=$2`, job.SourceType, file.ID).Scan(&currentSHA, &currentSize, &currentStatus, &currentMetadata)
		if err == nil {
			existing++
			var previous providerProjectFile
			_ = json.Unmarshal(currentMetadata, &previous)
			previousSignature, incomingSignature := providerFileHashSignature(previous), providerFileHashSignature(file)
			if currentSize != file.SizeBytes || previousSignature != "" && incomingSignature != "" && previousSignature != incomingSignature {
				_, _ = worker.server.db.Exec(ctx, `update mirrored_project_files set status='source_changed' where source_type=$1 and external_file_id=$2`, job.SourceType, file.ID)
				review++
			}
			continue
		}
		data, digest, downloadErr := downloadProviderFile(ctx, file, cfg)
		if downloadErr != nil {
			return map[string]any{"queuedForScan": queued, "existing": existing, "needsReview": review}, downloadErr
		}
		objectKey := buildOSSObjectKeyForFile(ossCfg.Prefix, "project/download/automated/"+job.ProjectPublicID, file.FileName)
		_, err = client.PutObject(ctx, &aliyunoss.PutObjectRequest{Bucket: aliyunoss.Ptr(ossCfg.Bucket), Key: aliyunoss.Ptr(objectKey),
			ContentType: aliyunoss.Ptr("application/octet-stream"), ContentLength: aliyunoss.Ptr(int64(len(data))), Body: bytes.NewReader(data), Metadata: map[string]string{"sha256": digest}})
		if err != nil {
			return nil, err
		}
		metadata, _ := json.Marshal(file)
		tx, txErr := worker.server.db.Begin(ctx)
		if txErr != nil {
			return nil, txErr
		}
		var ossFileID int64
		txErr = tx.QueryRow(ctx, `insert into oss_files(bucket,endpoint,region,object_key,category,source,original_name,source_original_name,
			content_type,size_bytes,source_size_bytes,sha256,status,scan_status) values($1,$2,$3,$4,$5,'project_auto_update',$6,$6,
			'application/octet-stream',$7,$7,$8,'active','pending') returning id`, ossCfg.Bucket, ossCfg.displayEndpoint(), ossCfg.Region,
			objectKey, ossCategoryFromObjectKey(objectKey, ossCfg.Prefix), file.FileName, len(data), digest).Scan(&ossFileID)
		if txErr == nil {
			_, txErr = tx.Exec(ctx, `insert into mirrored_project_files(project_route_id,source_type,external_file_id,file_sha256,byte_size,oss_file_id,
			license_spdx_id,status,metadata) values($1,$2,$3,$4,$5,$6,coalesce(
				(select license from mods where $7='mod' and id=$8),
				(select license from simple_projects where $7<>'mod' and id=$8 and project_type=$7)),
				'scanning',$9::jsonb)`, job.RouteID, job.SourceType, file.ID, digest, len(data), ossFileID, job.ProjectType, job.InternalID, metadata)
		}
		if txErr == nil {
			txErr = tx.Commit(ctx)
		} else {
			_ = tx.Rollback(ctx)
		}
		if txErr != nil {
			return nil, txErr
		}
		queued++
	}
	return map[string]any{"queuedForScan": queued, "existing": existing, "needsReview": review}, nil
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

func downloadProviderFile(ctx context.Context, file providerProjectFile, cfg modImportConfig) ([]byte, string, error) {
	client, err := newProviderHTTPClient(time.Duration(cfg.RequestTimeoutSeconds)*time.Second, file.DirectURL)
	if err != nil {
		return nil, "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, file.DirectURL, nil)
	if err != nil {
		return nil, "", err
	}
	request.Header.Set("User-Agent", cfg.UserAgent)
	response, err := client.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", fmt.Errorf("HTTP %d while downloading provider file", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, projectAutomationMaxFileBytes+1))
	if err != nil || int64(len(data)) > projectAutomationMaxFileBytes {
		return nil, "", errors.New("provider file exceeds the safe mirror limit")
	}
	if file.SizeBytes > 0 && int64(len(data)) != file.SizeBytes {
		return nil, "", errors.New("provider file size mismatch")
	}
	if file.SHA1 != "" {
		sum := sha1.Sum(data)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), file.SHA1) {
			return nil, "", errors.New("provider SHA-1 mismatch")
		}
	}
	if file.SHA512 != "" {
		sum := sha512.Sum512(data)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), file.SHA512) {
			return nil, "", errors.New("provider SHA-512 mismatch")
		}
	}
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), nil
}

func (worker *ProjectAutomationWorker) promoteCleanMirrors(ctx context.Context) {
	rows, err := worker.server.db.Query(ctx, `select mirror.id,route.entity_type,route.internal_id,mirror.oss_file_id,mirror.file_sha256,mirror.byte_size,mirror.metadata
		from mirrored_project_files mirror join public_routes route on route.id=mirror.project_route_id
		join oss_files file on file.id=mirror.oss_file_id and file.status='active' and file.scan_status='clean'
		where mirror.status='scanning' order by mirror.id limit 50`)
	if err != nil {
		return
	}
	type pending struct {
		id, internalID, ossID, size int64
		projectType, sha            string
		metadata                    []byte
	}
	items := make([]pending, 0)
	for rows.Next() {
		var item pending
		if rows.Scan(&item.id, &item.projectType, &item.internalID, &item.ossID, &item.sha, &item.size, &item.metadata) == nil {
			items = append(items, item)
		}
	}
	rows.Close()
	for _, item := range items {
		var file providerProjectFile
		if json.Unmarshal(item.metadata, &file) != nil {
			continue
		}
		tx, beginErr := worker.server.db.Begin(ctx)
		if beginErr != nil {
			continue
		}
		var lockedStatus string
		if tx.QueryRow(ctx, `select status from mirrored_project_files where id=$1 for update`, item.id).Scan(&lockedStatus) != nil || lockedStatus != "scanning" {
			_ = tx.Rollback(ctx)
			continue
		}
		_, insertErr := tx.Exec(ctx, `insert into project_files(project_type,project_internal_id,oss_file_id,display_name,version_name,release_channel,
			game_versions,loaders,file_name,content_type,size_bytes,sha256) values($1,$2,$3,$4,$5,$6,$7,$8,$9,'application/octet-stream',$10,$11)
			on conflict(project_type,project_internal_id,oss_file_id) do nothing`, item.projectType, item.internalID, item.ossID,
			firstNonEmpty(file.DisplayName, file.FileName), firstNonEmpty(file.VersionName, file.DisplayName), normalizeReleaseChannel(file.ReleaseChannel),
			uniqueTrimmed(file.GameVersions, 100), normalizeLoaders(file.Loaders), file.FileName, item.size, item.sha)
		if insertErr == nil {
			_, insertErr = tx.Exec(ctx, `update mirrored_project_files set status='ready' where id=$1`, item.id)
		}
		if insertErr == nil {
			_ = tx.Commit(ctx)
		} else {
			_ = tx.Rollback(ctx)
		}
	}
}
