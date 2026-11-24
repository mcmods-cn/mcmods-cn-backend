package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/security"
)

// Every registered public link has a publication or audience rule. For
// comments and projects the resolver always uses the public view. An inbox
// shortcut follows the same recipient rule as the signed-in notification list.
func (s *Server) publicLinkTargetIsVisible(ctx context.Context, entityType string, internalID int64, publicID string, viewerID int64) (bool, error) {
	var query string
	switch entityType {
	case "user":
		query = `select exists(select 1 from users where id=$1 and status<>'deleted')`
	case "notification":
		if viewerID <= 0 {
			return false, nil
		}
		var visible bool
		err := s.db.QueryRow(ctx, `select exists(select 1 from notifications where id=$1 and (recipient_id is null or recipient_id=$2))`, internalID, viewerID).Scan(&visible)
		return visible, err
	case "resource", "structure", "document", "recipe", "recipe_type", "tag", "recipe_template":
		return catalogEntityIsPublic(ctx, s.db, internalID, entityType)
	case "mod":
		query = `select exists(select 1 from mods where id=$1 and review_status='approved')`
	case "modpack":
		query = `select exists(select 1 from modpacks where id=$1 and review_status='approved')`
	case "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon":
		query = `select exists(select 1 from simple_projects where id=$1 and review_status='approved')`
	case "minecraft_server":
		query = `select exists(select 1 from minecraft_servers where id=$1 and review_status='approved')`
	case "author", "team":
		var visible bool
		err := s.db.QueryRow(ctx, `select exists(select 1 from creators where id=$1 and kind=$2 and review_status='approved')`, internalID, entityType).Scan(&visible)
		return visible, err
	case "comment":
		comment, err := s.resolveVisibleComment(ctx, publicID, security.Claims{})
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return err == nil && comment.ID == internalID, err
	case "community_post":
		query = `select exists(select 1 from community_posts where id=$1 and status='active' and review_status='approved')`
	case "blueprint":
		query = `select exists(select 1 from blueprints where id=$1 and status<>'deleted' and review_status in ('approved','not_required'))`
	case "skin":
		query = `select exists(select 1 from skin_assets where id=$1 and status='active' and review_status='approved' and visibility<>'private')`
	case "player_profile":
		query = `select exists(select 1 from player_profiles where id=$1 and status='active' and visibility<>'private')`
	case "currency":
		query = `select exists(select 1 from currencies where id=$1 and status='active')`
	case "shop_item":
		query = `select exists(select 1 from shop_items where id=$1 and status='active')`
	case "task":
		query = `select exists(select 1 from task_definitions where id=$1 and status='active')`
	default:
		return false, nil
	}
	var visible bool
	err := s.db.QueryRow(ctx, query, internalID).Scan(&visible)
	return visible, err
}

// Changelog entries have a detail page. Categories are filters on the current
// parent page, so the destination follows the parent even after a slug change.
func (s *Server) publicChangelogLink(ctx context.Context, entityType string, internalID int64, publicID, target string) (string, bool, error) {
	query := `select target.entity_type,target.internal_id,target.canonical_path from project_changelogs entry
		join public_routes target on target.id=entry.object_route_id
		where entry.id=$1 and entry.status='active' and entry.review_status='approved'`
	if entityType == "project_changelog_category" {
		query = `select target.entity_type,target.internal_id,target.canonical_path from project_changelog_categories category
			join public_routes target on target.id=category.object_route_id where category.id=$1`
	}
	var parentType, parentPath string
	var parentID int64
	err := s.db.QueryRow(ctx, query, internalID).Scan(&parentType, &parentID, &parentPath)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil || !changelogTargetTypes[parentType] {
		return "", false, err
	}
	if entityType == "project_changelog_category" {
		if parentPath == "" {
			return "", false, nil
		}
		target = parentPath + "?tab=changelog&categoryId=" + url.QueryEscape(publicID) + "#changelog"
	}
	visible, err := s.publicLinkTargetIsVisible(ctx, parentType, parentID, "", 0)
	return target, visible, err
}

func (s *Server) resolvePublicLink(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusNotFound, "短链接不存在")
		return
	}
	var entityType, target string
	var internalID int64
	err := s.db.QueryRow(r.Context(), `select entity_type,internal_id,canonical_path from public_routes where public_id=$1`, publicID).Scan(&entityType, &internalID, &target)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "短链接不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "解析短链接失败")
		return
	}
	var visible bool
	if entityType == "project_changelog" || entityType == "project_changelog_category" {
		target, visible, err = s.publicChangelogLink(r.Context(), entityType, internalID, publicID, target)
	} else {
		visible, err = s.publicLinkTargetIsVisible(r.Context(), entityType, internalID, publicID, currentClaims(r).Subject)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "解析短链接发布状态失败")
		return
	}
	if !visible {
		writeError(w, http.StatusNotFound, "短链接不存在")
		return
	}
	if target == "" {
		target, err = s.catalogPublicTarget(r.Context(), entityType, internalID, publicID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "解析短链接失败")
			return
		}
	}
	// These objects live in the account's economy workspace. Historical route
	// registry rows still contain retired standalone paths, so normalize on read
	// for both existing rows and newly generated public IDs.
	if entityType == "currency" || entityType == "shop_item" || entityType == "task" {
		target = "/user?section=economy&objectType=" + url.QueryEscape(entityType) + "&publicId=" + url.QueryEscape(publicID)
	}
	if target == "" {
		writeError(w, http.StatusNotFound, "该内容暂时没有可访问页面")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": publicID, "entityType": entityType, "target": target})
}

func (s *Server) catalogPublicTarget(ctx context.Context, entityType string, internalID int64, publicID string) (string, error) {
	switch entityType {
	case "tag":
		return "/mods-tag?publicId=" + url.QueryEscape(publicID), nil
	case "recipe_type":
		return "/recipe-types?publicId=" + url.QueryEscape(publicID), nil
	case "recipe":
		return "/recipe-types?recipePublicId=" + url.QueryEscape(publicID), nil
	case "resource", "structure", "document":
		var versionID, siteID string
		// Manual publications use a content-version identity, rather than an
		// import revision. Prefer a currently visible manual source and use a
		// deterministic version ordering when there are several publications.
		manualErr := s.db.QueryRow(ctx, `select version.public_id,mod.slug
			from mod_resource_version_details detail
			join mod_content_versions version on version.id=detail.version_id and version.status='active'
			join mods mod on mod.id=version.mod_id and mod.review_status='approved'
			join game_resources resource on resource.entity_id=detail.resource_id
			where detail.resource_id=$1 and detail.status='active' and detail.published_revision_id is not null
			order by version.updated_at desc,version.id desc limit 1`, internalID).Scan(&versionID, &siteID)
		if manualErr == nil {
			return "/mods/" + url.PathEscape(siteID) + "/resources/" + url.PathEscape(publicID) + "?version=" + url.QueryEscape(versionID), nil
		}
		if !errors.Is(manualErr, pgx.ErrNoRows) {
			return "", manualErr
		}
		// Resource detail pages select a content version. An import revision is
		// a source for that version, not a frontend route/version identity.
		err := s.db.QueryRow(ctx, `select version.public_id,mod.slug
			from game_resources resource
			join resource_import_snapshots snapshot on snapshot.resource_id=resource.entity_id
			join catalog_import_revisions revision on revision.id=snapshot.revision_id and revision.is_active
			join mods mod on mod.id=revision.mod_id and mod.review_status='approved'
			join mod_content_versions version on version.id=revision.target_version_id and version.mod_id=mod.id and version.status='active'
			where resource.entity_id=$1 and revision.status in ('ready','partial') order by revision.activated_at desc nulls last limit 1`, internalID).
			Scan(&versionID, &siteID)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		return "/mods/" + url.PathEscape(siteID) + "/resources/" + url.PathEscape(publicID) + "?version=" + url.QueryEscape(versionID), nil
	}
	return "", nil
}
