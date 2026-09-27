package httpapi

import (
	"context"
	"strings"

	"mcmods-cn-backend/internal/security"
)

type commentTargetIdentity struct {
	Type       string
	ID         int64
	VersionKey int64
}

func newCommentTargetIdentity(targetType string, targetID int64, targetVersionID *int64) commentTargetIdentity {
	identity := commentTargetIdentity{Type: targetType, ID: targetID}
	if targetVersionID != nil {
		identity.VersionKey = *targetVersionID
	}
	return identity
}

func (s *Server) queryCommentTargetsByInternal(ctx context.Context, identities []commentTargetIdentity, claims security.Claims) (map[commentTargetIdentity]commentTargetInfo, error) {
	return queryCommentTargetsByInternalWithQueryer(ctx, s.db, identities, claims)
}

func queryCommentTargetsByInternalWithQueryer(ctx context.Context, queryer commentQueryer, identities []commentTargetIdentity, claims security.Claims) (map[commentTargetIdentity]commentTargetInfo, error) {
	result := make(map[commentTargetIdentity]commentTargetInfo, len(identities))
	if len(identities) == 0 {
		return result, nil
	}
	targetTypes := make(map[string]bool)
	seen := make(map[commentTargetIdentity]struct{}, len(identities))
	types := make([]string, 0, len(identities))
	ids := make([]int64, 0, len(identities))
	versionKeys := make([]int64, 0, len(identities))
	for _, identity := range identities {
		if identity.Type == "" || identity.ID <= 0 {
			continue
		}
		if _, exists := seen[identity]; exists {
			continue
		}
		seen[identity] = struct{}{}
		targetTypes[identity.Type] = true
		types = append(types, identity.Type)
		ids = append(ids, identity.ID)
		versionKeys = append(versionKeys, identity.VersionKey)
	}
	if len(types) == 0 {
		return result, nil
	}

	branches := make([]string, 0, 12)
	if targetTypes["mod"] {
		branches = append(branches, `select request.target_type,request.target_id,request.target_version_key,
			mod.project_code,mod.primary_name,route.canonical_path
			from requested request
			join mods mod on request.target_type='mod' and mod.id=request.target_id
			join public_routes route on route.public_id=mod.project_code and route.entity_type='mod'
			where mod.review_status='approved' or mod.submitted_by=$4 or $5::boolean`)
	}
	if targetTypes["modpack"] {
		branches = append(branches, `select request.target_type,request.target_id,request.target_version_key,
			pack.public_id,pack.primary_name,route.canonical_path
			from requested request
			join modpacks pack on request.target_type='modpack' and pack.id=request.target_id
			join public_routes route on route.public_id=pack.public_id and route.entity_type='modpack'
			where pack.review_status='approved' or pack.submitted_by=$4 or $5::boolean`)
	}
	if targetTypes["plugin"] || targetTypes["map"] || targetTypes["resource_pack"] ||
		targetTypes["shader_pack"] || targetTypes["datapack"] || targetTypes["addon"] {
		branches = append(branches, `select request.target_type,request.target_id,request.target_version_key,
			project.public_id,project.primary_name,route.canonical_path
			from requested request
			join simple_projects project on project.project_type=request.target_type and project.id=request.target_id
			join public_routes route on route.public_id=project.public_id and route.entity_type=project.project_type
			where request.target_type in ('plugin','map','resource_pack','shader_pack','datapack','addon')
			  and (project.review_status='approved' or project.submitted_by=$4 or $5::boolean)`)
	}
	if targetTypes["blueprint"] {
		branches = append(branches, `select request.target_type,request.target_id,request.target_version_key,
			blueprint.public_id,blueprint.title,route.canonical_path
			from requested request
			join blueprints blueprint on request.target_type='blueprint' and blueprint.id=request.target_id
			join public_routes route on route.public_id=blueprint.public_id and route.entity_type='blueprint'
			where blueprint.status<>'deleted'
			  and (blueprint.review_status in ('not_required','approved') or blueprint.owner_id=$4 or $5::boolean)`)
	}
	if targetTypes["skin"] {
		branches = append(branches, `select request.target_type,request.target_id,request.target_version_key,
			asset.public_id,asset.display_name,route.canonical_path
			from requested request
			join skin_assets asset on request.target_type='skin' and asset.id=request.target_id
			join public_routes route on route.public_id=asset.public_id and route.entity_type='skin'
			where asset.status='active' and
			  ((asset.visibility in ('public','unlisted') and asset.review_status='approved') or asset.owner_id=$4 or $5::boolean)`)
	}
	if targetTypes["creator"] {
		branches = append(branches, `select request.target_type,request.target_id,request.target_version_key,
			creator.public_id,creator.name,
			(case when creator.kind='team' then '/teams/' else '/authors/' end)||creator.public_id
			from requested request
			join creators creator on request.target_type='creator' and creator.id=request.target_id
			where creator.review_status='approved' or creator.created_by=$4 or $5::boolean`)
	}
	if targetTypes["community_post"] {
		branches = append(branches, `select request.target_type,request.target_id,request.target_version_key,
			post.public_id,post.title,route.canonical_path
			from requested request
			join community_posts post on request.target_type='community_post' and post.id=request.target_id
			join public_routes route on route.public_id=post.public_id and route.entity_type='community_post'
			where post.status='active' and (post.review_status='approved' or post.author_id=$4 or $5::boolean)`)
	}
	if targetTypes["ban_record"] {
		branches = append(branches, `select request.target_type,request.target_id,request.target_version_key,
			ban.public_id,'小黑屋 · '||ban.username_snapshot,'/site-affairs/blackroom/'||ban.public_id
			from requested request
			join ban_records ban on request.target_type='ban_record' and ban.id=request.target_id`)
	}
	if targetTypes["player_profile"] {
		branches = append(branches, `select request.target_type,request.target_id,request.target_version_key,
			profile.public_id,profile.name,'/players/'||profile.public_id
			from requested request
			join player_profiles profile on request.target_type='player_profile' and profile.id=request.target_id
			where profile.status='active'
			  and (profile.visibility in ('public','unlisted') or profile.user_id=$4 or $5::boolean)`)
	}
	if targetTypes["tag"] {
		branches = append(branches, `select request.target_type,request.target_id,request.target_version_key,
			entity.public_id,coalesce(nullif(localization.name,''),'#'||definition.canonical_id),
			'/mods-tag?publicId='||entity.public_id
			from requested request
			join catalog_entities entity on request.target_type='tag' and entity.id=request.target_id and entity.entity_type='tag'
			join catalog_tags definition on definition.entity_id=entity.id
			left join lateral (select name from content_localizations where catalog_entity_id=entity.id and name<>''
				order by case locale when 'zh-CN' then 0 when 'en-US' then 1 else 2 end limit 1) localization on true
			where entity.status='active'`)
	}
	if targetTypes["recipe_type"] {
		branches = append(branches, `select request.target_type,request.target_id,request.target_version_key,
			entity.public_id,coalesce(nullif(localization.name,''),definition.canonical_id),
			'/recipe-types?publicId='||entity.public_id
			from requested request
			join catalog_entities entity on request.target_type='recipe_type' and entity.id=request.target_id and entity.entity_type='recipe_type'
			join recipe_types definition on definition.entity_id=entity.id
			left join lateral (select name from content_localizations where catalog_entity_id=entity.id and name<>''
				order by case locale when 'zh-CN' then 0 when 'en-US' then 1 else 2 end limit 1) localization on true
			where entity.status='active'`)
	}
	if targetTypes["mod_resource"] {
		branches = append(branches, `select request.target_type,request.target_id,request.target_version_key,
			entity.public_id||'~'||version.public_id,
			coalesce(nullif(localization.name,''),resource.canonical_id)||
				case when version.label<>'' then ' · '||version.label else '' end,
			'/mods/'||mod.slug||'/resources/'||entity.public_id||'?version='||version.public_id
			from requested request
			join catalog_entities entity on request.target_type='mod_resource' and entity.id=request.target_id
			join game_resources resource on resource.entity_id=entity.id
			join mod_content_versions version on version.id=request.target_version_key
			join mods mod on mod.id=version.mod_id
			join mod_resource_bindings binding on binding.resource_id=resource.entity_id and binding.mod_id=mod.id
			join mod_resource_version_details detail on detail.resource_id=resource.entity_id and detail.version_id=version.id
			left join lateral (select name from mod_resource_version_detail_localizations localization
				where localization.resource_id=resource.entity_id and localization.version_id=version.id and localization.name<>''
				order by case localization.locale when 'zh-CN' then 0 when 'en-US' then 1 else 2 end limit 1) localization on true
			where entity.status='active' and detail.status='active' and version.status='active'
			  and (mod.review_status='approved' or mod.submitted_by=$4 or $5::boolean)`)
	}
	if len(branches) == 0 {
		return result, nil
	}
	query := `with requested as (
		select target_type,target_id,target_version_key,$4::bigint viewer_id,$5::boolean moderator
		from unnest($1::text[],$2::bigint[],$3::bigint[])
			as request(target_type,target_id,target_version_key)
	)` + strings.Join(branches, " union all ")
	moderator := claimsAllow(claims, "comment.moderate") || claimsAllow(claims, "admin.*")
	rows, err := queryer.Query(ctx, query, types, ids, versionKeys, claims.Subject, moderator)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var identity commentTargetIdentity
		var info commentTargetInfo
		if err = rows.Scan(&identity.Type, &identity.ID, &identity.VersionKey, &info.Key, &info.Title, &info.URL); err != nil {
			return nil, err
		}
		info.Type = identity.Type
		info.InternalID = identity.ID
		if identity.VersionKey != 0 {
			versionID := identity.VersionKey
			info.VersionID = &versionID
		}
		result[identity] = info
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
