package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/security"
)

var followableProjectTypes = map[string]struct{}{
	"mod": {}, "modpack": {}, "plugin": {}, "map": {}, "resource_pack": {}, "shader_pack": {}, "datapack": {}, "addon": {},
	"minecraft_server": {}, "community_post": {}, "blueprint": {}, "skin": {},
}

type followProjectTarget struct {
	RouteID     int64  `json:"-"`
	InternalID  int64  `json:"-"`
	PublicID    string `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	UpdatedAt   string `json:"updatedAt"`
	Unavailable bool   `json:"unavailable,omitempty"`
}

func (s *Server) resolveFollowProjectTarget(ctx context.Context, publicID string, claims security.Claims) (followProjectTarget, error) {
	return resolveFollowProjectTargetWithQueryer(ctx, s.db, publicID, claims)
}

type followProjectQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type followProjectBatchQueryer interface {
	followProjectQueryer
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type followProjectTargetScanner interface {
	Scan(...any) error
}

func followProjectTargetNameSQL(route string) string {
	return `case ` + route + `.entity_type
		when 'mod' then (select primary_name from mods where id=` + route + `.internal_id)
		when 'modpack' then (select primary_name from modpacks where id=` + route + `.internal_id)
		when 'minecraft_server' then (select name from minecraft_servers where id=` + route + `.internal_id)
		when 'community_post' then (select title from community_posts where id=` + route + `.internal_id)
		when 'blueprint' then (select title from blueprints where id=` + route + `.internal_id)
		when 'skin' then (select display_name from skin_assets where id=` + route + `.internal_id)
		else (select primary_name from simple_projects where id=` + route + `.internal_id and project_type=` + route + `.entity_type) end`
}

func followProjectTargetUpdatedAtSQL(route string) string {
	return `case ` + route + `.entity_type
		when 'mod' then (select updated_at from mods where id=` + route + `.internal_id)
		when 'modpack' then (select updated_at from modpacks where id=` + route + `.internal_id)
		when 'minecraft_server' then (select updated_at from minecraft_servers where id=` + route + `.internal_id)
		when 'community_post' then (select updated_at from community_posts where id=` + route + `.internal_id)
		when 'blueprint' then (select updated_at from blueprints where id=` + route + `.internal_id)
		when 'skin' then (select updated_at from skin_assets where id=` + route + `.internal_id)
		else (select updated_at from simple_projects where id=` + route + `.internal_id and project_type=` + route + `.entity_type) end`
}

func followProjectTargetVisibilitySQL(route, viewerParameter, moderatorParameter string) string {
	return `((` + followProjectTargetPublicVisibilitySQL(route) + `) or case
		when ` + route + `.entity_type='mod' then exists(select 1 from mods where id=` + route + `.internal_id and (submitted_by=` + viewerParameter + ` or ` + moderatorParameter + `))
		when ` + route + `.entity_type='modpack' then exists(select 1 from modpacks where id=` + route + `.internal_id and (submitted_by=` + viewerParameter + ` or ` + moderatorParameter + `))
		when ` + route + `.entity_type in ('plugin','map','resource_pack','shader_pack','datapack','addon') then exists(select 1 from simple_projects where id=` + route + `.internal_id and project_type=` + route + `.entity_type and (submitted_by=` + viewerParameter + ` or ` + moderatorParameter + `))
		when ` + route + `.entity_type='minecraft_server' then exists(select 1 from minecraft_servers where id=` + route + `.internal_id and (submitted_by=` + viewerParameter + ` or ` + moderatorParameter + `))
		when ` + route + `.entity_type='community_post' then exists(select 1 from community_posts where id=` + route + `.internal_id and status='active' and (author_id=` + viewerParameter + ` or ` + moderatorParameter + `))
		when ` + route + `.entity_type='blueprint' then exists(select 1 from blueprints where id=` + route + `.internal_id and status<>'deleted' and (owner_id=` + viewerParameter + ` or ` + moderatorParameter + `))
		when ` + route + `.entity_type='skin' then exists(select 1 from skin_assets where id=` + route + `.internal_id and status='active' and (owner_id=` + viewerParameter + ` or ` + moderatorParameter + `))
		else false end)`
}

func followProjectTargetPublicVisibilitySQL(route string) string {
	return `case
		when ` + route + `.entity_type='mod' then exists(select 1 from mods where id=` + route + `.internal_id and review_status='approved')
		when ` + route + `.entity_type='modpack' then exists(select 1 from modpacks where id=` + route + `.internal_id and review_status='approved')
		when ` + route + `.entity_type in ('plugin','map','resource_pack','shader_pack','datapack','addon') then exists(select 1 from simple_projects where id=` + route + `.internal_id and project_type=` + route + `.entity_type and review_status='approved')
		when ` + route + `.entity_type='minecraft_server' then exists(select 1 from minecraft_servers where id=` + route + `.internal_id and review_status='approved')
		when ` + route + `.entity_type='community_post' then exists(select 1 from community_posts where id=` + route + `.internal_id and status='active' and review_status='approved')
		when ` + route + `.entity_type='blueprint' then exists(select 1 from blueprints where id=` + route + `.internal_id and status in ('ready','partial') and review_status in ('approved','not_required'))
		when ` + route + `.entity_type='skin' then exists(select 1 from skin_assets where id=` + route + `.internal_id and status='active' and visibility in ('public','unlisted') and review_status='approved')
		else false end`
}

func followProjectTargetSelectSQL(route string) string {
	return `select ` + route + `.id,` + route + `.internal_id,` + route + `.public_id,` + route + `.entity_type,` + route + `.canonical_path,
		coalesce(` + followProjectTargetNameSQL(route) + `,` + route + `.public_id),
		coalesce((` + followProjectTargetUpdatedAtSQL(route) + `)::text,'') from public_routes ` + route
}

func resolveFollowProjectTargetWithQueryer(ctx context.Context, queryer followProjectQueryer, publicID string, claims security.Claims) (followProjectTarget, error) {
	var target followProjectTarget
	publicID = strings.ToLower(strings.TrimSpace(publicID))
	if !validCatalogPublicID(publicID) {
		return target, pgx.ErrNoRows
	}
	moderator := claimsAllow(claims, "admin.*") || claimsAllow(claims, "project.review")
	err := scanFollowProjectTarget(queryer.QueryRow(ctx, followProjectTargetSelectSQL("route")+`
		where route.public_id=$1 and `+followProjectTargetVisibilitySQL("route", "$2", "$3"), publicID, claims.Subject, moderator), &target)
	return target, err
}

func loadFollowProjectTargetsWithQueryer(ctx context.Context, queryer followProjectBatchQueryer, publicIDs []string, claims security.Claims) (map[string]followProjectTarget, error) {
	unique := make([]string, 0, len(publicIDs))
	seen := make(map[string]struct{}, len(publicIDs))
	for _, publicID := range publicIDs {
		publicID = strings.ToLower(strings.TrimSpace(publicID))
		if !validCatalogPublicID(publicID) {
			continue
		}
		if _, exists := seen[publicID]; exists {
			continue
		}
		seen[publicID] = struct{}{}
		unique = append(unique, publicID)
	}
	result := make(map[string]followProjectTarget, len(unique))
	if len(unique) == 0 {
		return result, nil
	}
	moderator := claimsAllow(claims, "admin.*") || claimsAllow(claims, "project.review")
	rows, err := queryer.Query(ctx, followProjectTargetSelectSQL("route")+`
		where route.public_id=any($1::text[]) and `+followProjectTargetVisibilitySQL("route", "$2", "$3"), unique, claims.Subject, moderator)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var target followProjectTarget
		if err = scanFollowProjectTarget(rows, &target); err != nil {
			return nil, err
		}
		result[target.PublicID] = target
	}
	return result, rows.Err()
}

func scanFollowProjectTarget(scanner followProjectTargetScanner, target *followProjectTarget) error {
	return scanner.Scan(&target.RouteID, &target.InternalID, &target.PublicID, &target.Type, &target.URL, &target.Name, &target.UpdatedAt)
}

func (s *Server) projectFollowStatus(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusNotFound, "project does not exist")
		return
	}
	var target followProjectTarget
	var followed, notificationsEnabled, public bool
	err := s.db.QueryRow(r.Context(), `select route.id,route.internal_id,route.public_id,route.entity_type,
		case when visibility.public then route.canonical_path else '' end,
		case when visibility.public then coalesce(`+followProjectTargetNameSQL("route")+`,route.public_id) else '' end,
		case when visibility.public then coalesce((`+followProjectTargetUpdatedAtSQL("route")+`)::text,'') else '' end,
		follow.user_id is not null,coalesce(follow.notifications_enabled,false),visibility.public
		from public_routes route
		cross join lateral(select (`+followProjectTargetPublicVisibilitySQL("route")+`) public) visibility
		left join project_follows follow on follow.project_route_id=route.id and follow.user_id=$2
		where route.public_id=$1 and (visibility.public or follow.user_id is not null)`, publicID, currentClaims(r).Subject).
		Scan(&target.RouteID, &target.InternalID, &target.PublicID, &target.Type, &target.URL, &target.Name, &target.UpdatedAt,
			&followed, &notificationsEnabled, &public)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "project does not exist")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project")
		return
	}
	target.Unavailable = !public
	writeJSON(w, http.StatusOK, map[string]any{"followed": followed, "notificationsEnabled": notificationsEnabled, "target": target})
}

func (s *Server) followProject(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusNotFound, "project does not exist or is not public")
		return
	}
	var notificationsEnabled bool
	err := s.db.QueryRow(r.Context(), `insert into project_follows(user_id,project_route_id,notifications_enabled)
		select $1,route.id,true from public_routes route where route.public_id=$2 and (`+followProjectTargetPublicVisibilitySQL("route")+`)
		on conflict(user_id,project_route_id) do update set updated_at=now() returning notifications_enabled`,
		currentClaims(r).Subject, publicID).Scan(&notificationsEnabled)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "project does not exist or is not public")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"followed": true, "notificationsEnabled": notificationsEnabled})
}

type updateProjectFollowNotificationsRequest struct {
	NotificationsEnabled *bool `json:"notificationsEnabled"`
}

func (s *Server) updateProjectFollowNotifications(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusNotFound, "project follow does not exist")
		return
	}
	var request updateProjectFollowNotificationsRequest
	if decodeJSON(r, &request) != nil || request.NotificationsEnabled == nil {
		writeError(w, http.StatusBadRequest, "notificationsEnabled must be a boolean")
		return
	}
	var notificationsEnabled bool
	err := s.db.QueryRow(r.Context(), `update project_follows follow
		set notifications_enabled=$3,updated_at=now()
		from public_routes route
		where follow.user_id=$1 and follow.project_route_id=route.id and route.public_id=$2
		returning follow.notifications_enabled`, currentClaims(r).Subject, publicID, *request.NotificationsEnabled).
		Scan(&notificationsEnabled)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "project follow does not exist")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update project follow notifications")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"followed": true, "notificationsEnabled": notificationsEnabled})
}

func (s *Server) unfollowProject(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	if !validCatalogPublicID(publicID) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if _, err := s.db.Exec(r.Context(), `delete from project_follows follow using public_routes route
		where follow.user_id=$1 and follow.project_route_id=route.id and route.public_id=$2`, currentClaims(r).Subject, publicID); err != nil {
		writeError(w, 500, "failed to unfollow project")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) myProjectFollows(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	request, err := parseProjectFollowPageRequest(r.URL.Query(), claims.Subject)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var cursorCreatedAt time.Time
	var cursorRouteID int64
	if request.Cursor != nil {
		cursorCreatedAt = request.Cursor.CreatedAt
		cursorRouteID = request.Cursor.RouteID
	}
	rows, err := s.db.Query(r.Context(), `select route.id,route.public_id,route.entity_type,
		case when visibility.public then route.canonical_path else '' end,follow.notifications_enabled,follow.created_at,
		case when visibility.public then coalesce(`+followProjectTargetNameSQL("route")+`,route.public_id) else '' end,
		case when visibility.public then (`+followProjectTargetUpdatedAtSQL("route")+`) else null end updated_at,
		not visibility.public unavailable
		from project_follows follow join public_routes route on route.id=follow.project_route_id
		cross join lateral(select (`+followProjectTargetPublicVisibilitySQL("route")+`) public) visibility
		where follow.user_id=$1 and ($2='' or route.entity_type=$2)
		and ($3='' or route.public_id=$3 or visibility.public and lower(coalesce(`+followProjectTargetNameSQL("route")+`,'')) like '%'||$3||'%')
		and (not $4::boolean or follow.created_at<$5::timestamptz or (follow.created_at=$5::timestamptz and follow.project_route_id>$6))
		order by follow.created_at desc,follow.project_route_id asc limit $7`, claims.Subject, request.TargetType, request.Query,
		request.Cursor != nil, cursorCreatedAt, cursorRouteID, request.Limit+1)
	if err != nil {
		writeError(w, 500, "failed to load followed projects")
		return
	}
	defer rows.Close()
	type followedProjectRow struct {
		value     map[string]any
		routeID   int64
		createdAt time.Time
	}
	pageRows := make([]followedProjectRow, 0, request.Limit+1)
	for rows.Next() {
		var routeID int64
		var publicID, kind, url, name string
		var notifications, unavailable bool
		var createdAt time.Time
		var updatedAt *time.Time
		if err = rows.Scan(&routeID, &publicID, &kind, &url, &notifications, &createdAt, &name, &updatedAt, &unavailable); err != nil {
			writeError(w, 500, "failed to read followed projects")
			return
		}
		var updatedAtValue any = ""
		if !unavailable && updatedAt != nil {
			updatedAtValue = *updatedAt
		}
		pageRows = append(pageRows, followedProjectRow{
			routeID: routeID, createdAt: createdAt,
			value: map[string]any{"id": publicID, "type": kind, "url": url, "name": name, "notificationsEnabled": notifications,
				"createdAt": createdAt, "updatedAt": updatedAtValue, "unavailable": unavailable},
		})
	}
	if err = finishRows(rows); err != nil {
		writeError(w, 500, "failed to load followed projects")
		return
	}
	hasMore := len(pageRows) > request.Limit
	if hasMore {
		pageRows = pageRows[:request.Limit]
	}
	items := make([]map[string]any, len(pageRows))
	for index := range pageRows {
		items[index] = pageRows[index].value
	}
	nextCursor := ""
	if hasMore && len(pageRows) > 0 {
		last := pageRows[len(pageRows)-1]
		nextCursor = encodeProjectFollowPageCursor(projectFollowPageCursor{
			Version: projectFollowPageCursorVersion, Scope: request.Scope, CreatedAt: last.createdAt, RouteID: last.routeID,
		})
	}
	writeJSON(w, 200, map[string]any{"items": items, "limit": request.Limit, "hasMore": hasMore, "nextCursor": nextCursor})
}
