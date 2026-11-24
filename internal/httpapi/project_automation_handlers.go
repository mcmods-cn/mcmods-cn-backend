package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"mcmods-cn-backend/internal/security"
)

var autoUpdateProjectTypes = stringSet("mod", "plugin", "map", "shader_pack", "resource_pack", "datapack")
var autoUpdateKinds = stringSet("minecraft_versions", "changelog", "site_downloads")
var autoUpdateIntervals = stringSet("week", "month", "quarter", "half_year", "year", "never")

type projectAutomationTarget struct {
	RouteID      int64
	InternalID   int64
	ProjectType  string
	PublicID     string
	CanonicalURL string
}

func (s *Server) projectAutomationTarget(w http.ResponseWriter, r *http.Request) (projectAutomationTarget, bool) {
	projectType := normalizeChangelogTargetType(r.PathValue("projectType"))
	projectID := strings.ToLower(strings.TrimSpace(r.PathValue("projectId")))
	if !autoUpdateProjectTypes[projectType] || !validCatalogPublicID(projectID) {
		writeError(w, http.StatusNotFound, "项目不存在")
		return projectAutomationTarget{}, false
	}
	var target projectAutomationTarget
	err := s.db.QueryRow(r.Context(), `select id,internal_id,entity_type,public_id,canonical_path from public_routes
		where entity_type=$1 and public_id=$2`, projectType, projectID).Scan(&target.RouteID, &target.InternalID, &target.ProjectType, &target.PublicID, &target.CanonicalURL)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "项目不存在")
		return target, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取项目失败")
		return target, false
	}
	return target, true
}

func projectAutomationAllowed(claims security.Claims, permission, projectID string) bool {
	return claimsAllow(claims, permission) || claimsAllow(claims, "project.edit."+projectID) || claimsAllow(claims, "admin.*")
}

type projectAutomationSetting struct {
	Kind                  string `json:"kind"`
	SourceType            string `json:"sourceType"`
	Interval              string `json:"interval"`
	Enabled               bool   `json:"enabled"`
	LicenseOverride       bool   `json:"licenseOverride"`
	LicenseOverrideReason string `json:"licenseOverrideReason"`
	LicenseOverrideSource string `json:"licenseOverrideSource"`
}

func (next projectAutomationSetting) changesLicenseDecision(old projectAutomationSetting) bool {
	return old.LicenseOverride != next.LicenseOverride || old.LicenseOverrideReason != next.LicenseOverrideReason ||
		old.LicenseOverrideSource != next.LicenseOverrideSource || next.LicenseOverride && old.SourceType != next.SourceType
}

// Both control-plane writers serialize on the route, including before any
// settings have been created. Workers retain their run/setting/source fences.
func lockProjectAutomationConfiguration(ctx context.Context, tx pgx.Tx, routeID int64) error {
	var id int64
	return tx.QueryRow(ctx, `select id from public_routes where id=$1 for no key update`, routeID).Scan(&id)
}

func (s *Server) projectAutomation(w http.ResponseWriter, r *http.Request) {
	target, ok := s.projectAutomationTarget(w, r)
	if !ok {
		return
	}
	var err error
	if !projectAutomationAllowed(currentClaims(r), "project.auto_update.view", target.PublicID) {
		writeError(w, 403, "无权查看项目自动更新")
		return
	}
	if r.Method == http.MethodPut {
		if !projectAutomationAllowed(currentClaims(r), "project.auto_update.configure", target.PublicID) {
			writeError(w, 403, "无权配置项目自动更新")
			return
		}
		var request struct {
			Items []projectAutomationSetting `json:"items"`
		}
		if decodeJSON(r, &request) != nil || len(request.Items) != 3 {
			writeError(w, 400, "自动更新配置不完整")
			return
		}
		tx, beginErr := s.db.Begin(r.Context())
		if beginErr != nil {
			writeError(w, 500, "创建配置事务失败")
			return
		}
		defer tx.Rollback(r.Context())
		if err = lockProjectAutomationConfiguration(r.Context(), tx, target.RouteID); err != nil {
			writeError(w, http.StatusInternalServerError, "读取自动更新配置失败")
			return
		}
		rows, err := tx.Query(r.Context(), `select update_kind,coalesce(source_type,''),interval_code,enabled,license_override,license_override_reason,license_override_source
			from project_auto_update_settings where project_route_id=$1 order by update_kind for update`, target.RouteID)
		if err != nil {
			writeError(w, 500, "读取自动更新配置失败")
			return
		}
		stored, err := pgx.CollectRows(rows, pgx.RowToStructByPos[projectAutomationSetting])
		if err != nil {
			writeError(w, 500, "读取自动更新配置失败")
			return
		}
		previous := make(map[string]projectAutomationSetting, len(stored))
		for _, item := range stored {
			previous[item.Kind] = item
		}
		sourceRows, err := tx.Query(r.Context(), `select source_type,external_project_id from project_external_sources where project_route_id=$1`, target.RouteID)
		if err != nil {
			writeError(w, 500, "读取项目来源失败")
			return
		}
		boundSources, err := pgx.CollectRows(sourceRows, pgx.RowToStructByPos[struct{ Provider, ExternalID string }])
		if err != nil {
			writeError(w, 500, "读取项目来源失败")
			return
		}
		externalIDs := make(map[string]string, len(boundSources))
		for _, source := range boundSources {
			externalIDs[source.Provider] = source.ExternalID
		}
		actor, ok := requireCurrentWriteActor(w, r, tx)
		if !ok {
			return
		}
		if !projectAutomationAllowed(actor, "project.auto_update.view", target.PublicID) || !projectAutomationAllowed(actor, "project.auto_update.configure", target.PublicID) {
			writeError(w, http.StatusForbidden, "无权配置项目自动更新")
			return
		}
		seen := map[string]bool{}
		for _, item := range request.Items {
			item.Kind = strings.ToLower(strings.TrimSpace(item.Kind))
			item.SourceType = strings.ToLower(strings.TrimSpace(item.SourceType))
			item.Interval = strings.ToLower(strings.TrimSpace(item.Interval))
			item.LicenseOverrideReason = strings.TrimSpace(item.LicenseOverrideReason)
			item.LicenseOverrideSource = strings.TrimSpace(item.LicenseOverrideSource)
			if !autoUpdateKinds[item.Kind] || seen[item.Kind] || !autoUpdateIntervals[item.Interval] ||
				!stringSet("", "modrinth", "curseforge", "github")[item.SourceType] || item.Kind == "minecraft_versions" && item.SourceType == "github" ||
				item.LicenseOverride && (item.SourceType == "" || item.LicenseOverrideReason == "" || item.LicenseOverrideSource == "") {
				writeError(w, 400, "自动更新配置包含无效来源、周期或许可覆盖")
				return
			}
			if item.Interval == "never" {
				item.Enabled = false
			}
			old, exists := previous[item.Kind]
			changedLicense := item.changesLicenseDecision(old)
			if changedLicense && !claimsAllow(actor, "project.auto_update.redistribution_override") {
				writeError(w, http.StatusForbidden, "无权更改再分发许可例外")
				return
			}
			seen[item.Kind] = true
			if exists && old == item {
				continue
			}
			if item.Enabled || item.LicenseOverride {
				if externalIDs[item.SourceType] == "" {
					writeError(w, 400, "启用自动更新前必须绑定并验证对应来源")
					return
				}
				if item.Enabled && item.Kind == "site_downloads" && !item.LicenseOverride {
					_, allowed, err := projectRedistributionPolicy(r.Context(), tx, target.ProjectType, target.InternalID)
					if err != nil {
						writeError(w, http.StatusInternalServerError, "读取项目许可证失败")
						return
					}
					if !allowed {
						writeError(w, 400, "当前项目许可证不允许自动建立站内下载源")
						return
					}
				}
			}
			nextRun := autoUpdateNextRun(item.Interval, time.Now().UTC())
			_, err = tx.Exec(r.Context(), `insert into project_auto_update_settings(project_route_id,update_kind,source_type,interval_code,enabled,next_run_at,configured_by,license_override,license_override_reason,license_override_source)
				values($1,$2,nullif($3,''),$4,$5,$6,$7,$8,$9,$10) on conflict(project_route_id,update_kind) do update set source_type=excluded.source_type,
				interval_code=excluded.interval_code,enabled=excluded.enabled,next_run_at=excluded.next_run_at,configured_by=excluded.configured_by,
				license_override=excluded.license_override,license_override_reason=excluded.license_override_reason,license_override_source=excluded.license_override_source,updated_at=now()`,
				target.RouteID, item.Kind, item.SourceType, item.Interval, item.Enabled, nextRun, actor.Subject, item.LicenseOverride, item.LicenseOverrideReason, item.LicenseOverrideSource)
			if err != nil {
				writeError(w, 500, "保存自动更新配置失败")
				return
			}
			if changedLicense {
				if err := s.recordProjectAutomationLicenseDecision(r.Context(), tx, actor, target, externalIDs, old, item); err != nil {
					writeError(w, 500, "记录再分发许可变更失败")
					return
				}
			}
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, 500, "提交自动更新配置失败")
			return
		}
		s.writeAppLog(r.Context(), "admin_operation", "info", "project.auto_update.configure", target.PublicID, currentClaims(r).Subject, r, 200, 0, nil)
	}
	s.writeProjectAutomation(w, r, target)
}

func (s *Server) recordProjectAutomationLicenseDecision(ctx context.Context, tx pgx.Tx, actor security.Claims, target projectAutomationTarget, externalIDs map[string]string, old, next projectAutomationSetting) error {
	// Approval sources can include private correspondence. Preserve the two
	// decisions under the existing settings key without logging their text.
	type evidence struct {
		Enabled bool   `json:"enabled"`
		Reason  string `json:"reason"`
		Source  string `json:"source"`
	}
	sealed, err := s.sealSystemSetting(map[string]evidence{
		"before": {old.LicenseOverride, old.LicenseOverrideReason, old.LicenseOverrideSource},
		"after":  {next.LicenseOverride, next.LicenseOverrideReason, next.LicenseOverrideSource},
	})
	if err != nil {
		return err
	}
	return appendAuditEventTx(ctx, tx, auditEventParams{
		EntityType: target.ProjectType, EntityID: target.InternalID, AggregateType: "project_automation_license", AggregateKey: target.PublicID,
		ActorID: actor.Subject, ActorSnapshot: actor.Username, Action: "project.auto_update.redistribution_override",
		Metadata: map[string]any{"kind": next.Kind, "sourceType": next.SourceType, "externalProjectId": externalIDs[next.SourceType],
			"previousSourceType": old.SourceType, "previousExternalProjectId": externalIDs[old.SourceType], "enabled": next.LicenseOverride,
			"evidence": json.RawMessage(sealed)},
	})
}

func autoUpdateNextRun(interval string, now time.Time) *time.Time {
	var next time.Time
	switch interval {
	case "week":
		next = now.AddDate(0, 0, 7)
	case "month":
		next = now.AddDate(0, 1, 0)
	case "quarter":
		next = now.AddDate(0, 3, 0)
	case "half_year":
		next = now.AddDate(0, 6, 0)
	case "year":
		next = now.AddDate(1, 0, 0)
	default:
		return nil
	}
	return &next
}

func (s *Server) writeProjectAutomation(w http.ResponseWriter, r *http.Request, target projectAutomationTarget) {
	sources, err := s.querySimpleRows(r, `select source_type,external_project_id,external_project_url,verified_at from project_external_sources where project_route_id=$1 order by source_type`, target.RouteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取自动更新来源失败")
		return
	}
	sources = renameProjectAutomationFields(sources, map[string]string{
		"source_type": "sourceType", "external_project_id": "externalProjectId",
		"external_project_url": "externalProjectUrl", "verified_at": "verifiedAt",
	})
	settings, err := s.querySimpleRows(r, `select update_kind,coalesce(source_type,'') as source_type,interval_code,enabled,next_run_at,last_run_at,last_status,last_error_code,last_error,
		license_override,license_override_reason,license_override_source,updated_at from project_auto_update_settings where project_route_id=$1 order by update_kind`, target.RouteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取自动更新配置失败")
		return
	}
	settings = renameProjectAutomationFields(settings, map[string]string{
		"update_kind": "updateKind", "source_type": "sourceType", "interval_code": "interval",
		"next_run_at": "nextRunAt", "last_run_at": "lastRunAt", "last_status": "lastStatus",
		"last_error_code": "lastErrorCode", "last_error": "lastError", "license_override": "licenseOverride",
		"license_override_reason": "licenseOverrideReason", "license_override_source": "licenseOverrideSource",
		"updated_at": "updatedAt",
	})
	settings = completeProjectAutomationSettings(settings, sources)
	writeJSON(w, 200, map[string]any{"project": map[string]any{"id": target.PublicID, "type": target.ProjectType, "url": target.CanonicalURL}, "sources": sources, "settings": settings})
}

func renameProjectAutomationFields(rows []map[string]any, names map[string]string) []map[string]any {
	for _, row := range rows {
		for current, canonical := range names {
			if value, exists := row[current]; exists {
				row[canonical] = value
				delete(row, current)
			}
		}
	}
	return rows
}

func completeProjectAutomationSettings(settings, sources []map[string]any) []map[string]any {
	byKind := make(map[string]map[string]any, len(settings))
	for _, setting := range settings {
		if kind, ok := setting["updateKind"].(string); ok && autoUpdateKinds[kind] {
			byKind[kind] = setting
		}
	}
	availableSources := make(map[string]bool, len(sources))
	for _, source := range sources {
		if sourceType, ok := source["sourceType"].(string); ok {
			availableSources[sourceType] = true
		}
	}
	result := make([]map[string]any, 0, 3)
	for _, specification := range []struct{ kind, interval string }{
		{"minecraft_versions", "quarter"}, {"changelog", "never"}, {"site_downloads", "never"},
	} {
		if setting := byKind[specification.kind]; setting != nil {
			result = append(result, setting)
			continue
		}
		sourceType := ""
		for _, candidate := range []string{"modrinth", "curseforge", "github"} {
			if availableSources[candidate] && (specification.kind != "minecraft_versions" || candidate != "github") {
				sourceType = candidate
				break
			}
		}
		result = append(result, map[string]any{
			"updateKind": specification.kind, "sourceType": sourceType, "interval": specification.interval,
			"enabled": false, "nextRunAt": nil, "lastRunAt": nil, "lastStatus": "never", "lastErrorCode": "", "lastError": "",
			"licenseOverride": false, "licenseOverrideReason": "", "licenseOverrideSource": "", "updatedAt": nil,
		})
	}
	return result
}

func projectRedistributionPolicy(ctx context.Context, db databaseQuery, projectType string, internalID int64) (string, bool, error) {
	project := `select license from simple_projects where id=$1 and project_type=$2`
	args := []any{internalID, projectType}
	if projectType == "mod" {
		project = `select license from mods where id=$1`
		args = []any{internalID}
	}
	var license string
	var allowed bool
	err := db.QueryRow(ctx, `select project.license,exists(select 1 from license_policies policy
		where lower(policy.spdx_id)=lower(btrim(project.license)) and policy.redistribution_allowed)
		from (`+project+`) project`, args...).Scan(&license, &allowed)
	return license, allowed, err
}

type bindProjectSourceRequest struct {
	SourceType string `json:"sourceType"`
	URL        string `json:"url"`
}

func (s *Server) bindProjectAutomationSource(w http.ResponseWriter, r *http.Request) {
	target, ok := s.projectAutomationTarget(w, r)
	if !ok {
		return
	}
	if !projectAutomationAllowed(currentClaims(r), "project.external_source.bind", target.PublicID) {
		writeError(w, 403, "无权绑定项目来源")
		return
	}
	var request bindProjectSourceRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, 400, "来源格式不正确")
		return
	}
	request.SourceType = strings.ToLower(strings.TrimSpace(request.SourceType))
	reference, canonicalURL, verifyErr := s.verifyProjectExternalSource(r.Context(), target.ProjectType, request.SourceType, request.URL)
	if verifyErr != nil {
		switch {
		case errors.Is(verifyErr, errInvalidAutomationSource):
			writeError(w, http.StatusBadRequest, "来源链接无效或与项目类型不匹配")
		case errors.Is(verifyErr, errProviderSourceNotPublic), isProviderNotFound(verifyErr):
			writeError(w, http.StatusBadRequest, "没有找到可公开使用的来源项目")
		default:
			writeError(w, http.StatusServiceUnavailable, "暂时无法验证来源，请稍后重试")
		}
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "保存来源失败")
		return
	}
	defer tx.Rollback(r.Context())
	if err = lockProjectAutomationConfiguration(r.Context(), tx, target.RouteID); err != nil {
		writeError(w, 500, "读取来源配置失败")
		return
	}
	actor, ok := requireCurrentWriteActor(w, r, tx)
	if !ok {
		return
	}
	if !projectAutomationAllowed(actor, "project.external_source.bind", target.PublicID) {
		writeError(w, 403, "无权绑定项目来源")
		return
	}
	var wouldReplaceApprovedSource bool
	if err = tx.QueryRow(r.Context(), `select exists(select 1 from project_external_sources source
		join project_auto_update_settings setting on setting.project_route_id=source.project_route_id and setting.source_type=source.source_type
		where source.project_route_id=$1 and source.source_type=$2 and source.external_project_id<>$3 and setting.license_override)`,
		target.RouteID, request.SourceType, reference).Scan(&wouldReplaceApprovedSource); err != nil {
		writeError(w, 500, "读取来源许可失败")
		return
	}
	if wouldReplaceApprovedSource {
		writeError(w, 409, "请先撤销现有来源的再分发许可例外，再绑定新项目")
		return
	}
	_, err = tx.Exec(r.Context(), `insert into project_external_sources(project_route_id,source_type,external_project_id,external_project_url,verified_at,verified_by)
		values($1,$2,$3,$4,now(),$5) on conflict(project_route_id,source_type) do update set external_project_id=excluded.external_project_id,
		external_project_url=excluded.external_project_url,verified_at=now(),verified_by=excluded.verified_by`, target.RouteID, request.SourceType, reference, canonicalURL, actor.Subject)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, 409, "该外部项目已绑定到其他站内项目")
		} else {
			writeError(w, 500, "保存来源失败")
		}
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "保存来源失败")
		return
	}
	s.writeAppLog(r.Context(), "admin_operation", "info", "project.external_source.bind", target.PublicID, currentClaims(r).Subject, r, 200, 0, map[string]string{"source": request.SourceType})
	writeJSON(w, 200, map[string]any{"sourceType": request.SourceType, "externalProjectId": reference, "url": canonicalURL, "verified": true})
}

var errInvalidAutomationSource = errors.New("invalid automation source reference")

func (s *Server) verifyProjectExternalSource(ctx context.Context, projectType, sourceType, rawURL string) (string, string, error) {
	provider, canonicalURL, reference, err := parseProjectImportSource(projectType, sourceType, rawURL)
	if sourceType == "github" && err != nil {
		provider, canonicalURL, reference, err = parseProjectImportSource("mod", sourceType, rawURL)
	}
	if err != nil {
		return "", "", errInvalidAutomationSource
	}
	cfg, err := s.modImportConfigFromSettings(ctx)
	if err != nil {
		return "", "", err
	}
	if err = ensureModImportProviderAvailable(cfg, provider); err != nil {
		return "", "", err
	}
	clientBase := cfg.Modrinth.BaseURL
	if provider == "curseforge" {
		clientBase = cfg.CurseForge.BaseURL
	} else if provider == "github" {
		clientBase = cfg.GitHub.BaseURL
	}
	client, err := newProviderHTTPClient(time.Duration(cfg.RequestTimeoutSeconds)*time.Second, clientBase)
	if err != nil {
		return "", "", err
	}
	if provider == "modrinth" {
		var project modrinthProject
		headers := publicProviderHeaders(cfg.UserAgent)
		if err = getProviderJSON(ctx, client, cfg.Modrinth.BaseURL+"/project/"+url.PathEscape(reference), headers, &project); err != nil {
			return "", "", err
		}
		if !publicModrinthProjectStatus(project.Status) || strings.TrimSpace(project.ID) == "" {
			return "", "", errProviderSourceNotPublic
		}
		return project.ID, canonicalURL, nil
	}
	if provider == "github" {
		var repository githubRepository
		headers := publicProviderHeaders(cfg.UserAgent)
		if err = getProviderJSON(ctx, client, cfg.GitHub.BaseURL+"/repos/"+reference, headers, &repository); err != nil {
			return "", "", err
		}
		if repository.Private == nil || *repository.Private {
			return "", "", errProviderSourceNotPublic
		}
		return reference, canonicalURL, nil
	}
	headers := providerCredentialHeaders(cfg.UserAgent, "curseforge", cfg.CurseForge.BaseURL, "", cfg.CurseForge.APIKey)
	numericID, err := resolveCurseForgeProjectID(ctx, client, reference, cfg, headers)
	if err != nil {
		return "", "", err
	}
	var project struct {
		Data curseForgeMod `json:"data"`
	}
	if err = getProviderJSONLimited(ctx, client, cfg.CurseForge.BaseURL+"/mods/"+numericID, headers, projectFileProviderJSONLimit, &project); err != nil {
		return "", "", err
	}
	if !project.Data.IsAvailable {
		return "", "", errProviderSourceNotPublic
	}
	if project.Data.ID <= 0 || strconv.FormatInt(project.Data.ID, 10) != numericID {
		return "", "", errors.New("CurseForge returned a different project identity")
	}
	_, _, slug, err := parseProjectImportSource(projectType, provider, project.Data.Links.WebsiteURL)
	_, numericReferenceErr := strconv.ParseInt(reference, 10, 64)
	if err != nil || (numericReferenceErr != nil && !strings.EqualFold(slug, reference)) {
		return "", "", errInvalidAutomationSource
	}
	return numericID, canonicalURL, nil
}

func (s *Server) runProjectAutomation(w http.ResponseWriter, r *http.Request) {
	target, ok := s.projectAutomationTarget(w, r)
	if !ok {
		return
	}
	if !projectAutomationAllowed(currentClaims(r), "project.auto_update.run", target.PublicID) {
		writeError(w, 403, "无权运行项目自动更新")
		return
	}
	var request struct {
		Kind string `json:"kind"`
	}
	if decodeJSON(r, &request) != nil || !autoUpdateKinds[strings.ToLower(strings.TrimSpace(request.Kind))] {
		writeError(w, 400, "更新类型不正确")
		return
	}
	request.Kind = strings.ToLower(strings.TrimSpace(request.Kind))
	var runID string
	err := s.db.QueryRow(r.Context(), `insert into project_auto_update_runs(setting_id)
		select id from project_auto_update_settings where project_route_id=$1 and update_kind=$2 and source_type is not null returning public_id`, target.RouteID, request.Kind).Scan(&runID)
	var conflict *pgconn.PgError
	if errors.Is(err, pgx.ErrNoRows) || errors.As(err, &conflict) && conflict.Code == "23505" && conflict.ConstraintName == "uq_project_auto_update_active_run" {
		writeError(w, http.StatusConflict, "该类型未绑定来源、已有任务运行或配置不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建自动更新任务失败")
		return
	}
	writeJSON(w, 202, map[string]any{"id": runID, "status": "pending"})
}

func (s *Server) projectAutomationRuns(w http.ResponseWriter, r *http.Request) {
	target, ok := s.projectAutomationTarget(w, r)
	if !ok {
		return
	}
	if !projectAutomationAllowed(currentClaims(r), "project.auto_update.view_logs", target.PublicID) {
		writeError(w, 403, "无权查看自动更新记录")
		return
	}
	items, err := s.querySimpleRows(r, `select run.public_id,setting.update_kind,run.status,run.attempts,run.result,run.last_error_code,run.last_error,
		run.created_at,run.started_at,run.finished_at from project_auto_update_runs run join project_auto_update_settings setting on setting.id=run.setting_id
		where setting.project_route_id=$1 order by run.created_at desc,run.id desc limit 100`, target.RouteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取自动更新记录失败")
		return
	}
	items = renameProjectAutomationFields(items, map[string]string{
		"public_id": "id", "update_kind": "updateKind", "last_error_code": "lastErrorCode",
		"last_error": "lastError", "created_at": "createdAt", "started_at": "startedAt", "finished_at": "finishedAt",
	})
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) adminProjectAutomationOverview(w http.ResponseWriter, r *http.Request) {
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if status != "" && !stringSet("pending", "running", "completed", "dead_letter")[status] {
		writeError(w, http.StatusBadRequest, "自动更新任务状态不正确")
		return
	}
	limit := boundedLimit(r.URL.Query().Get("limit"), 100, 200)
	items, err := s.querySimpleRows(r, `select run.public_id,route.entity_type as project_type,route.public_id as project_id,route.canonical_path,
		setting.update_kind,setting.source_type,run.status,run.attempts,run.result,run.last_error_code,run.last_error,
		run.created_at,run.started_at,run.finished_at
		from project_auto_update_runs run
		join project_auto_update_settings setting on setting.id=run.setting_id
		join public_routes route on route.id=setting.project_route_id
		where ($1='' or run.status=$1) order by run.created_at desc,run.id desc limit $2`, status, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取自动更新任务失败")
		return
	}
	items = renameProjectAutomationFields(items, map[string]string{
		"public_id": "id", "project_type": "projectType", "project_id": "projectId", "canonical_path": "canonicalPath",
		"update_kind": "updateKind", "source_type": "sourceType", "last_error_code": "lastErrorCode",
		"last_error": "lastError", "created_at": "createdAt", "started_at": "startedAt", "finished_at": "finishedAt",
	})
	settings, err := s.querySimpleRows(r, `select route.entity_type as project_type,route.public_id as project_id,route.canonical_path,
		setting.update_kind,setting.source_type,setting.interval_code,setting.enabled,setting.next_run_at,setting.last_run_at,
		setting.last_status,setting.last_error_code,setting.last_error,setting.updated_at
		from project_auto_update_settings setting join public_routes route on route.id=setting.project_route_id
		order by setting.updated_at desc,setting.id desc limit 200`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取自动更新配置失败")
		return
	}
	settings = renameProjectAutomationFields(settings, map[string]string{
		"project_type": "projectType", "project_id": "projectId", "canonical_path": "canonicalPath",
		"update_kind": "updateKind", "source_type": "sourceType", "interval_code": "interval",
		"next_run_at": "nextRunAt", "last_run_at": "lastRunAt", "last_status": "lastStatus",
		"last_error_code": "lastErrorCode", "last_error": "lastError", "updated_at": "updatedAt",
	})
	writeJSON(w, http.StatusOK, map[string]any{"runs": items, "settings": settings})
}
