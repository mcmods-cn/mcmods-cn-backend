package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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

func (s *Server) projectAutomationTarget(r *http.Request) (projectAutomationTarget, error) {
	projectType := normalizeChangelogTargetType(r.PathValue("projectType"))
	projectID := strings.ToLower(strings.TrimSpace(r.PathValue("projectId")))
	if !autoUpdateProjectTypes[projectType] || !validCatalogPublicID(projectID) {
		return projectAutomationTarget{}, pgx.ErrNoRows
	}
	var target projectAutomationTarget
	err := s.db.QueryRow(r.Context(), `select id,internal_id,entity_type,public_id,canonical_path from public_routes
		where entity_type=$1 and public_id=$2`, projectType, projectID).Scan(&target.RouteID, &target.InternalID, &target.ProjectType, &target.PublicID, &target.CanonicalURL)
	return target, err
}

func projectAutomationAllowed(r *http.Request, permission, projectID string) bool {
	claims := currentClaims(r)
	return claimsAllow(claims, permission) || claimsAllow(claims, "project.edit."+projectID) || claimsAllow(claims, "admin.*")
}

func (s *Server) projectAutomation(w http.ResponseWriter, r *http.Request) {
	target, err := s.projectAutomationTarget(r)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "项目不存在")
		return
	}
	if err != nil {
		writeError(w, 500, "读取项目失败")
		return
	}
	if !projectAutomationAllowed(r, "project.auto_update.view", target.PublicID) {
		writeError(w, 403, "无权查看项目自动更新")
		return
	}
	if r.Method == http.MethodPut {
		if !projectAutomationAllowed(r, "project.auto_update.configure", target.PublicID) {
			writeError(w, 403, "无权配置项目自动更新")
			return
		}
		var request struct {
			Items []struct {
				Kind                  string `json:"kind"`
				SourceType            string `json:"sourceType"`
				Interval              string `json:"interval"`
				Enabled               bool   `json:"enabled"`
				LicenseOverride       bool   `json:"licenseOverride"`
				LicenseOverrideReason string `json:"licenseOverrideReason"`
				LicenseOverrideSource string `json:"licenseOverrideSource"`
			} `json:"items"`
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
		seen := map[string]bool{}
		for _, item := range request.Items {
			item.Kind = strings.ToLower(strings.TrimSpace(item.Kind))
			item.SourceType = strings.ToLower(strings.TrimSpace(item.SourceType))
			item.Interval = strings.ToLower(strings.TrimSpace(item.Interval))
			if !autoUpdateKinds[item.Kind] || seen[item.Kind] || !autoUpdateIntervals[item.Interval] ||
				!stringSet("", "modrinth", "curseforge", "github")[item.SourceType] || item.Kind == "minecraft_versions" && item.SourceType == "github" ||
				item.LicenseOverride && (!claimsAllow(currentClaims(r), "project.auto_update.redistribution_override") || strings.TrimSpace(item.LicenseOverrideReason) == "" || strings.TrimSpace(item.LicenseOverrideSource) == "") {
				writeError(w, 400, "自动更新配置包含无效来源、周期或许可覆盖")
				return
			}
			if item.Interval == "never" {
				item.Enabled = false
			}
			if item.Enabled {
				var bound bool
				if scanErr := tx.QueryRow(r.Context(), `select exists(select 1 from project_external_sources where project_route_id=$1 and source_type=$2)`, target.RouteID, item.SourceType).Scan(&bound); scanErr != nil || !bound {
					writeError(w, 400, "启用自动更新前必须绑定并验证对应来源")
					return
				}
				if item.Kind == "site_downloads" && !item.LicenseOverride {
					var allowed bool
					var license string
					licenseQuery := `select license from simple_projects where id=$1 and project_type=$2`
					if target.ProjectType == "mod" {
						licenseQuery = `select license from mods where id=$1`
					}
					if scanErr := tx.QueryRow(r.Context(), licenseQuery, projectAutomationLicenseArgs(target)...).Scan(&license); scanErr != nil {
						writeError(w, 500, "读取项目许可证失败")
						return
					}
					_ = tx.QueryRow(r.Context(), `select redistribution_allowed from license_policies where lower(spdx_id)=lower($1)`, strings.TrimSpace(license)).Scan(&allowed)
					if !allowed {
						writeError(w, 400, "当前项目许可证不允许自动建立站内下载源")
						return
					}
				}
			}
			seen[item.Kind] = true
			nextRun := autoUpdateNextRun(item.Interval, time.Now().UTC())
			_, err = tx.Exec(r.Context(), `insert into project_auto_update_settings(project_route_id,update_kind,source_type,interval_code,enabled,next_run_at,configured_by,license_override,license_override_reason,license_override_source)
				values($1,$2,nullif($3,''),$4,$5,$6,$7,$8,$9,$10) on conflict(project_route_id,update_kind) do update set source_type=excluded.source_type,
				interval_code=excluded.interval_code,enabled=excluded.enabled,next_run_at=excluded.next_run_at,configured_by=excluded.configured_by,
				license_override=excluded.license_override,license_override_reason=excluded.license_override_reason,license_override_source=excluded.license_override_source,updated_at=now()`,
				target.RouteID, item.Kind, item.SourceType, item.Interval, item.Enabled, nextRun, currentClaims(r).Subject, item.LicenseOverride,
				strings.TrimSpace(item.LicenseOverrideReason), strings.TrimSpace(item.LicenseOverrideSource))
			if err != nil {
				writeError(w, 500, "保存自动更新配置失败")
				return
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
	// Defaults are inserted in one short transaction so GET remains idempotent.
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "读取自动更新配置失败")
		return
	}
	for _, item := range []struct{ kind, interval string }{{"minecraft_versions", "quarter"}, {"changelog", "never"}, {"site_downloads", "never"}} {
		var sourceType *string
		sourceErr := tx.QueryRow(r.Context(), `select source_type from project_external_sources where project_route_id=$1
			and ($2<>'minecraft_versions' or source_type in ('modrinth','curseforge'))
			order by case source_type when 'modrinth' then 0 when 'curseforge' then 1 else 2 end limit 1`, target.RouteID, item.kind).Scan(&sourceType)
		if sourceErr != nil && !errors.Is(sourceErr, pgx.ErrNoRows) {
			err = sourceErr
			break
		}
		enabled := item.kind == "minecraft_versions" && sourceType != nil
		_, err = tx.Exec(r.Context(), `insert into project_auto_update_settings(project_route_id,update_kind,source_type,interval_code,enabled,configured_by)
			values($1,$2,$3,$4,$5,$6) on conflict(project_route_id,update_kind) do nothing`, target.RouteID, item.kind, sourceType,
			item.interval, enabled, currentClaims(r).Subject)
		if err != nil {
			break
		}
	}
	if err == nil {
		err = tx.Commit(r.Context())
	} else {
		_ = tx.Rollback(r.Context())
	}
	if err != nil {
		writeError(w, 500, "初始化自动更新配置失败")
		return
	}
	sources, err := s.querySimpleRows(r, `select source_type,external_project_id,external_project_url,verified_at from project_external_sources where project_route_id=$1 order by source_type`, target.RouteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取自动更新数据失败")
		return
	}
	settings, err := s.querySimpleRows(r, `select update_kind,coalesce(source_type,''),interval_code,enabled,next_run_at,last_run_at,last_status,last_error_code,last_error,
		license_override,license_override_reason,license_override_source,updated_at from project_auto_update_settings where project_route_id=$1 order by update_kind`, target.RouteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取自动更新数据失败")
		return
	}
	writeJSON(w, 200, map[string]any{"project": map[string]any{"id": target.PublicID, "type": target.ProjectType, "url": target.CanonicalURL}, "sources": sources, "settings": settings})
}

func projectAutomationLicenseArgs(target projectAutomationTarget) []any {
	if target.ProjectType == "mod" {
		return []any{target.InternalID}
	}
	return []any{target.InternalID, target.ProjectType}
}

type bindProjectSourceRequest struct {
	SourceType string `json:"sourceType"`
	URL        string `json:"url"`
}

func (s *Server) bindProjectAutomationSource(w http.ResponseWriter, r *http.Request) {
	target, err := s.projectAutomationTarget(r)
	if err != nil {
		writeError(w, 404, "项目不存在")
		return
	}
	if !projectAutomationAllowed(r, "project.external_source.bind", target.PublicID) {
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
		writeError(w, 400, verifyErr.Error())
		return
	}
	_, err = s.db.Exec(r.Context(), `insert into project_external_sources(project_route_id,source_type,external_project_id,external_project_url,verified_at,verified_by)
		values($1,$2,$3,$4,now(),$5) on conflict(project_route_id,source_type) do update set external_project_id=excluded.external_project_id,
		external_project_url=excluded.external_project_url,verified_at=now(),verified_by=excluded.verified_by`, target.RouteID, request.SourceType, reference, canonicalURL, currentClaims(r).Subject)
	if err != nil {
		writeError(w, 409, "该外部项目已绑定到其他站内项目")
		return
	}
	s.writeAppLog(r.Context(), "admin_operation", "info", "project.external_source.bind", target.PublicID, currentClaims(r).Subject, r, 200, 0, map[string]string{"source": request.SourceType})
	writeJSON(w, 200, map[string]any{"sourceType": request.SourceType, "externalProjectId": reference, "url": canonicalURL, "verified": true})
}

func (s *Server) verifyProjectExternalSource(ctx context.Context, projectType, sourceType, rawURL string) (string, string, error) {
	provider, canonicalURL, reference, err := parseProjectImportSource(projectType, sourceType, rawURL)
	if sourceType == "github" && err != nil {
		provider, canonicalURL, reference, err = parseProjectImportSource("mod", sourceType, rawURL)
	}
	if err != nil {
		return "", "", err
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
		headers := providerHeaders(cfg.UserAgent, cfg.Modrinth.Token, "")
		if err = getProviderJSON(ctx, client, cfg.Modrinth.BaseURL+"/project/"+url.PathEscape(reference), headers, &project); err != nil {
			return "", "", errors.New("无法验证 Modrinth 项目")
		}
		return project.ID, canonicalURL, nil
	}
	if provider == "github" {
		var repository map[string]any
		headers := providerHeaders(cfg.UserAgent, cfg.GitHub.Token, "")
		if err = getProviderJSON(ctx, client, cfg.GitHub.BaseURL+"/repos/"+reference, headers, &repository); err != nil {
			return "", "", errors.New("无法验证 GitHub 仓库")
		}
		return reference, canonicalURL, nil
	}
	headers := providerHeaders(cfg.UserAgent, "", cfg.CurseForge.APIKey)
	numericID, err := resolveCurseForgeProjectID(ctx, client, reference, cfg, headers)
	if err != nil {
		return "", "", errors.New("无法验证 CurseForge 项目")
	}
	return numericID, canonicalURL, nil
}

func (s *Server) runProjectAutomation(w http.ResponseWriter, r *http.Request) {
	target, err := s.projectAutomationTarget(r)
	if err != nil {
		writeError(w, 404, "项目不存在")
		return
	}
	if !projectAutomationAllowed(r, "project.auto_update.run", target.PublicID) {
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
	err = s.db.QueryRow(r.Context(), `insert into project_auto_update_runs(setting_id)
		select id from project_auto_update_settings where project_route_id=$1 and update_kind=$2 and source_type is not null returning public_id`, target.RouteID, request.Kind).Scan(&runID)
	if err != nil {
		writeError(w, 409, "该类型未绑定来源、已有任务运行或配置不存在")
		return
	}
	writeJSON(w, 202, map[string]any{"id": runID, "status": "pending"})
}

func (s *Server) projectAutomationRuns(w http.ResponseWriter, r *http.Request) {
	target, err := s.projectAutomationTarget(r)
	if err != nil {
		writeError(w, 404, "项目不存在")
		return
	}
	if !projectAutomationAllowed(r, "project.auto_update.view_logs", target.PublicID) {
		writeError(w, 403, "无权查看自动更新记录")
		return
	}
	items, err := s.querySimpleRows(r, `select run.public_id,setting.update_kind,run.status,run.attempts,run.result,run.last_error_code,run.last_error,
		run.created_at,run.started_at,run.finished_at from project_auto_update_runs run join project_auto_update_settings setting on setting.id=run.setting_id
		where setting.project_route_id=$1 order by run.created_at desc,run.id desc limit 100`, target.RouteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取自动更新数据失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) adminProjectAutomationOverview(w http.ResponseWriter, r *http.Request) {
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if status != "" && !stringSet("pending", "running", "completed", "failed", "dead_letter")[status] {
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
		writeError(w, http.StatusInternalServerError, "读取自动更新数据失败")
		return
	}
	settings, err := s.querySimpleRows(r, `select route.entity_type as project_type,route.public_id as project_id,route.canonical_path,
		setting.update_kind,setting.source_type,setting.interval_code,setting.enabled,setting.next_run_at,setting.last_run_at,
		setting.last_status,setting.last_error_code,setting.last_error,setting.updated_at
		from project_auto_update_settings setting join public_routes route on route.id=setting.project_route_id
		order by setting.updated_at desc,setting.id desc limit 200`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取自动更新数据失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": items, "settings": settings})
}
