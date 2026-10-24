package httpapi

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

// The anonymous resolver is a discovery surface. A route registration is never
// proof of current publication. Editor/reviewer entry points keep their own ACLs.
func (s *Server) publicLinkTargetIsVisible(ctx context.Context, entityType string, internalID int64) (bool, error) {
	var query string
	switch entityType {
	case "mod":
		query = `select exists(select 1 from mods where id=$1 and review_status='approved')`
	case "modpack":
		query = `select exists(select 1 from modpacks where id=$1 and review_status='approved')`
	case "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon":
		query = `select exists(select 1 from simple_projects where id=$1 and review_status='approved')`
	case "minecraft_server":
		query = `select exists(select 1 from minecraft_servers where id=$1 and review_status='approved')`
	case "creator":
		query = `select exists(select 1 from creators where id=$1 and review_status='approved')`
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
	case "project_changelog":
		var parentType string
		var parentID int64
		err := s.db.QueryRow(ctx, `select target.entity_type,target.internal_id from project_changelogs entry join public_routes target on target.id=entry.object_route_id where entry.id=$1 and entry.status='active' and entry.review_status='approved'`, internalID).Scan(&parentType, &parentID)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return s.publicLinkTargetIsVisible(ctx, parentType, parentID)
	default:
		return true, nil
	}
	var visible bool
	err := s.db.QueryRow(ctx, query, internalID).Scan(&visible)
	return visible, err
}
