package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/security"
)

var followableProjectTypes = map[string]struct{}{
	"mod": {}, "modpack": {}, "plugin": {}, "map": {}, "resource_pack": {}, "shader_pack": {}, "datapack": {}, "addon": {},
	"minecraft_server": {}, "community_post": {}, "blueprint": {}, "skin": {},
}

type followProjectTarget struct {
	RouteID    int64  `json:"-"`
	InternalID int64  `json:"-"`
	PublicID   string `json:"id"`
	Type       string `json:"type"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	UpdatedAt  string `json:"updatedAt"`
}

func (s *Server) resolveFollowProjectTarget(ctx context.Context, publicID string, claims security.Claims) (followProjectTarget, error) {
	var target followProjectTarget
	publicID = strings.ToLower(strings.TrimSpace(publicID))
	if !validCatalogPublicID(publicID) {
		return target, pgx.ErrNoRows
	}
	if err := s.db.QueryRow(ctx, `select id,internal_id,public_id,entity_type,canonical_path from public_routes where public_id=$1`, publicID).
		Scan(&target.RouteID, &target.InternalID, &target.PublicID, &target.Type, &target.URL); err != nil {
		return target, err
	}
	if _, ok := followableProjectTypes[target.Type]; !ok {
		return target, pgx.ErrNoRows
	}
	viewerID := claims.Subject
	moderator := claimsAllow(claims, "admin.*") || claimsAllow(claims, "project.review")
	var visible bool
	var err error
	switch target.Type {
	case "mod":
		err = s.db.QueryRow(ctx, `select primary_name,review_status='approved' or created_by=$2 or $3,updated_at::text from mods where id=$1`, target.InternalID, viewerID, moderator).Scan(&target.Name, &visible, &target.UpdatedAt)
	case "modpack":
		err = s.db.QueryRow(ctx, `select primary_name,review_status='approved' or created_by=$2 or $3,updated_at::text from modpacks where id=$1`, target.InternalID, viewerID, moderator).Scan(&target.Name, &visible, &target.UpdatedAt)
	case "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon":
		err = s.db.QueryRow(ctx, `select primary_name,review_status='approved' or created_by=$3 or $4,updated_at::text from simple_projects where id=$1 and project_type=$2`, target.InternalID, target.Type, viewerID, moderator).Scan(&target.Name, &visible, &target.UpdatedAt)
	case "minecraft_server":
		err = s.db.QueryRow(ctx, `select name,review_status='approved' or created_by=$2 or $3,updated_at::text from minecraft_servers where id=$1`, target.InternalID, viewerID, moderator).Scan(&target.Name, &visible, &target.UpdatedAt)
	case "community_post":
		err = s.db.QueryRow(ctx, `select title,status='active' and (review_status='approved' or author_id=$2 or $3),updated_at::text from community_posts where id=$1`, target.InternalID, viewerID, moderator).Scan(&target.Name, &visible, &target.UpdatedAt)
	case "blueprint":
		err = s.db.QueryRow(ctx, `select title,status<>'deleted' and (review_status in ('approved','not_required') or owner_id=$2 or $3),updated_at::text from blueprints where id=$1`, target.InternalID, viewerID, moderator).Scan(&target.Name, &visible, &target.UpdatedAt)
	case "skin":
		err = s.db.QueryRow(ctx, `select display_name,status='active' and ((visibility in ('public','unlisted') and review_status='approved') or owner_id=$2 or $3),updated_at::text from skin_assets where id=$1`, target.InternalID, viewerID, moderator).Scan(&target.Name, &visible, &target.UpdatedAt)
	}
	if err != nil || !visible {
		return target, pgx.ErrNoRows
	}
	return target, nil
}

func (s *Server) projectFollowStatus(w http.ResponseWriter, r *http.Request) {
	target, err := s.resolveFollowProjectTarget(r.Context(), r.PathValue("publicId"), currentClaims(r))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "project does not exist")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project")
		return
	}
	var followed, notificationsEnabled bool
	_ = s.db.QueryRow(r.Context(), `select true,notifications_enabled from project_follows where user_id=$1 and project_route_id=$2`, currentClaims(r).Subject, target.RouteID).Scan(&followed, &notificationsEnabled)
	writeJSON(w, http.StatusOK, map[string]any{"followed": followed, "notificationsEnabled": notificationsEnabled, "target": target})
}

func (s *Server) followProject(w http.ResponseWriter, r *http.Request) {
	target, err := s.resolveFollowProjectTarget(r.Context(), r.PathValue("publicId"), currentClaims(r))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "project does not exist or is not public")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project")
		return
	}
	if _, err = s.db.Exec(r.Context(), `insert into project_follows(user_id,project_route_id,notifications_enabled) values($1,$2,true) on conflict(user_id,project_route_id) do update set updated_at=now()`, currentClaims(r).Subject, target.RouteID); err != nil {
		writeError(w, 500, "failed to follow project")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"followed": true, "notificationsEnabled": true})
}

func (s *Server) unfollowProject(w http.ResponseWriter, r *http.Request) {
	target, err := s.resolveFollowProjectTarget(r.Context(), r.PathValue("publicId"), currentClaims(r))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "project does not exist")
		return
	}
	if err != nil {
		writeError(w, 500, "failed to load project")
		return
	}
	if _, err = s.db.Exec(r.Context(), `delete from project_follows where user_id=$1 and project_route_id=$2`, currentClaims(r).Subject, target.RouteID); err != nil {
		writeError(w, 500, "failed to unfollow project")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) myProjectFollows(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	targetType := strings.TrimSpace(r.URL.Query().Get("type"))
	limit := 24
	if parsed, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && parsed > 0 && parsed <= 100 {
		limit = parsed
	}
	offset := 0
	if parsed, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && parsed > 0 {
		offset = parsed
	}
	moderator := claimsAllow(currentClaims(r), "admin.*") || claimsAllow(currentClaims(r), "project.review")
	rows, err := s.db.Query(r.Context(), `select route.public_id,route.entity_type,route.canonical_path,follow.notifications_enabled,follow.created_at,
		case route.entity_type when 'mod' then (select primary_name from mods where id=route.internal_id)
		when 'modpack' then (select primary_name from modpacks where id=route.internal_id)
		when 'minecraft_server' then (select name from minecraft_servers where id=route.internal_id)
		when 'community_post' then (select title from community_posts where id=route.internal_id)
		when 'blueprint' then (select title from blueprints where id=route.internal_id)
		when 'skin' then (select display_name from skin_assets where id=route.internal_id)
		else (select primary_name from simple_projects where id=route.internal_id and project_type=route.entity_type) end name,
		case route.entity_type when 'mod' then (select updated_at from mods where id=route.internal_id)
		when 'modpack' then (select updated_at from modpacks where id=route.internal_id)
		when 'minecraft_server' then (select updated_at from minecraft_servers where id=route.internal_id)
		when 'community_post' then (select updated_at from community_posts where id=route.internal_id)
		when 'blueprint' then (select updated_at from blueprints where id=route.internal_id)
		when 'skin' then (select updated_at from skin_assets where id=route.internal_id)
		else (select updated_at from simple_projects where id=route.internal_id and project_type=route.entity_type) end updated_at
		from project_follows follow join public_routes route on route.id=follow.project_route_id
		where follow.user_id=$1 and ($2='' or route.entity_type=$2)
		and ($3='' or route.public_id=$3 or lower(coalesce(case route.entity_type when 'mod' then (select primary_name from mods where id=route.internal_id) when 'modpack' then (select primary_name from modpacks where id=route.internal_id) when 'community_post' then (select title from community_posts where id=route.internal_id) when 'minecraft_server' then (select name from minecraft_servers where id=route.internal_id) when 'blueprint' then (select title from blueprints where id=route.internal_id) when 'skin' then (select display_name from skin_assets where id=route.internal_id) else (select primary_name from simple_projects where id=route.internal_id and project_type=route.entity_type) end,'')) like '%'||lower($3)||'%')
		and case route.entity_type
			when 'mod' then exists(select 1 from mods where id=route.internal_id and (review_status='approved' or created_by=$1 or $4))
			when 'modpack' then exists(select 1 from modpacks where id=route.internal_id and (review_status='approved' or created_by=$1 or $4))
			when 'plugin' then exists(select 1 from simple_projects where id=route.internal_id and project_type=route.entity_type and (review_status='approved' or created_by=$1 or $4))
			when 'map' then exists(select 1 from simple_projects where id=route.internal_id and project_type=route.entity_type and (review_status='approved' or created_by=$1 or $4))
			when 'resource_pack' then exists(select 1 from simple_projects where id=route.internal_id and project_type=route.entity_type and (review_status='approved' or created_by=$1 or $4))
			when 'shader_pack' then exists(select 1 from simple_projects where id=route.internal_id and project_type=route.entity_type and (review_status='approved' or created_by=$1 or $4))
			when 'datapack' then exists(select 1 from simple_projects where id=route.internal_id and project_type=route.entity_type and (review_status='approved' or created_by=$1 or $4))
			when 'addon' then exists(select 1 from simple_projects where id=route.internal_id and project_type=route.entity_type and (review_status='approved' or created_by=$1 or $4))
			when 'minecraft_server' then exists(select 1 from minecraft_servers where id=route.internal_id and (review_status='approved' or created_by=$1 or $4))
			when 'community_post' then exists(select 1 from community_posts where id=route.internal_id and status='active' and (review_status='approved' or author_id=$1 or $4))
			when 'blueprint' then exists(select 1 from blueprints where id=route.internal_id and status<>'deleted' and (review_status in ('approved','not_required') or owner_id=$1 or $4))
			when 'skin' then exists(select 1 from skin_assets where id=route.internal_id and status='active' and ((visibility in ('public','unlisted') and review_status='approved') or owner_id=$1 or $4))
			else false end
		order by follow.created_at desc,route.id desc limit $5 offset $6`, currentClaims(r).Subject, targetType, query, moderator, limit, offset)
	if err != nil {
		writeError(w, 500, "failed to load followed projects")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var publicID, kind, url, name string
		var notifications bool
		var createdAt, updatedAt any
		if rows.Scan(&publicID, &kind, &url, &notifications, &createdAt, &name, &updatedAt) != nil {
			writeError(w, 500, "failed to read followed projects")
			return
		}
		items = append(items, map[string]any{"id": publicID, "type": kind, "url": url, "name": name, "notificationsEnabled": notifications, "createdAt": createdAt, "updatedAt": updatedAt})
	}
	writeJSON(w, 200, map[string]any{"items": items, "limit": limit, "offset": offset})
}
