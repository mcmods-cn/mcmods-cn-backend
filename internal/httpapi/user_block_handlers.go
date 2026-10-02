package httpapi

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
)

func (s *Server) userBlockList(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	page := boundedInt(r.URL.Query().Get("page"), 1, 1, 10000)
	pageSize := boundedInt(r.URL.Query().Get("pageSize"), 24, 1, 60)
	offset := (page - 1) * pageSize

	var total int64
	if err := s.db.QueryRow(r.Context(), `select count(*)
		from user_blocks block
		join users account on account.id=block.blocked_id
		where block.blocker_id=$1 and account.status='active'`, claims.Subject).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "读取黑名单失败")
		return
	}
	rows, err := s.db.Query(r.Context(), `select account.public_id,account.username,account.avatar_url,account.signature
		from user_blocks block
		join users account on account.id=block.blocked_id
		where block.blocker_id=$1 and account.status='active'
		order by block.created_at desc,block.blocked_id desc limit $2 offset $3`, claims.Subject, pageSize, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取黑名单失败")
		return
	}
	defer rows.Close()
	items := make([]userConnectionItem, 0)
	for rows.Next() {
		var item userConnectionItem
		if err = rows.Scan(&item.ID, &item.Username, &item.AvatarURL, &item.Signature); err != nil {
			writeError(w, http.StatusInternalServerError, "读取黑名单失败")
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "读取黑名单失败")
		return
	}
	rows.Close()
	ossCfg := s.ossConfigFromSettings(r.Context())
	avatarURLs := make([]string, len(items))
	for index := range items {
		avatarURLs[index] = items[index].AvatarURL
	}
	avatarURLs, err = s.resolveStoredOSSImageURLsWithConfig(r.Context(), ossCfg, avatarURLs)
	if err != nil {
		writeError(w, http.StatusBadGateway, "生成用户头像访问链接失败")
		return
	}
	for index := range items {
		items[index].AvatarURL = avatarURLs[index]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "page": page, "pageSize": pageSize, "total": total,
	})
}

func (s *Server) userBlock(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.pathUserIdentity(w, r)
	if !ok {
		return
	}
	claims := currentClaims(r)
	if identity.InternalID == claims.Subject {
		writeError(w, http.StatusBadRequest, "不能拉黑自己")
		return
	}
	failureMessage := "拉黑用户失败"
	if r.Method == http.MethodDelete {
		failureMessage = "解除拉黑失败"
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, failureMessage)
		return
	}
	defer tx.Rollback(r.Context())
	if err = lockUserRelationshipPairTx(r.Context(), tx, claims.Subject, identity.InternalID); err != nil {
		writeError(w, http.StatusInternalServerError, failureMessage)
		return
	}
	if r.Method == http.MethodDelete {
		tag, err := tx.Exec(r.Context(), `delete from user_blocks where blocker_id=$1 and blocked_id=$2`, claims.Subject, identity.InternalID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "解除拉黑失败")
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "解除拉黑失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"blocked": false, "removed": tag.RowsAffected() > 0})
		return
	}
	tag, err := tx.Exec(r.Context(), `insert into user_blocks(blocker_id,blocked_id)
		select $1,id from users where id=$2 and status='active'
		on conflict do nothing`, claims.Subject, identity.InternalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "拉黑用户失败")
		return
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err = tx.QueryRow(r.Context(), `select exists(select 1
			from user_blocks block join users account on account.id=block.blocked_id
			where block.blocker_id=$1 and block.blocked_id=$2 and account.status='active')`, claims.Subject, identity.InternalID).Scan(&exists); err != nil {
			writeError(w, http.StatusInternalServerError, "拉黑用户失败")
			return
		}
		if !exists {
			writeError(w, http.StatusNotFound, "用户不存在")
			return
		}
	}
	// A block immediately severs existing follows in both directions. A later
	// follow is also rejected while either directional block remains active.
	if _, err = tx.Exec(r.Context(), `delete from user_follows
		where (follower_id=$1 and followed_id=$2) or (follower_id=$2 and followed_id=$1)`, claims.Subject, identity.InternalID); err != nil {
		writeError(w, http.StatusInternalServerError, "拉黑用户失败")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "拉黑用户失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"blocked": true, "created": tag.RowsAffected() > 0})
}

func (s *Server) usersBlockEachOther(ctx context.Context, firstUserID, secondUserID int64) (bool, error) {
	if firstUserID <= 0 || secondUserID <= 0 || firstUserID == secondUserID {
		return false, nil
	}
	var blocked bool
	err := s.db.QueryRow(ctx, `select exists(select 1 from user_blocks
		where (blocker_id=$1 and blocked_id=$2) or (blocker_id=$2 and blocked_id=$1))`, firstUserID, secondUserID).Scan(&blocked)
	return blocked, err
}

func (s *Server) userBlocksActor(ctx context.Context, blockerID, actorID int64) (bool, error) {
	if blockerID <= 0 || actorID <= 0 || blockerID == actorID {
		return false, nil
	}
	var blocked bool
	err := s.db.QueryRow(ctx, `select exists(select 1 from user_blocks where blocker_id=$1 and blocked_id=$2)`, blockerID, actorID).Scan(&blocked)
	return blocked, err
}

func (s *Server) commentTargetOwnerBlocksUser(ctx context.Context, target commentTargetInfo, userID int64) (bool, error) {
	identity := newCommentTargetIdentity(target.Type, target.InternalID, target.VersionID)
	blockedByTarget, err := s.commentTargetOwnersBlockUser(ctx, []commentTargetIdentity{identity}, userID)
	return blockedByTarget[identity], err
}

func (s *Server) commentTargetOwnersBlockUser(ctx context.Context, identities []commentTargetIdentity,
	userID int64) (map[commentTargetIdentity]bool, error) {
	result := make(map[commentTargetIdentity]bool, len(identities))
	if userID <= 0 || len(identities) == 0 {
		return result, nil
	}
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
		types = append(types, identity.Type)
		ids = append(ids, identity.ID)
		versionKeys = append(versionKeys, identity.VersionKey)
	}
	if len(types) == 0 {
		return result, nil
	}
	rows, err := s.db.Query(ctx, `with requested(target_type,target_id,target_version_key) as (
		select * from unnest($1::text[],$2::bigint[],$3::bigint[])
	), target_facts as (
		select request.target_type,request.target_id,request.target_version_key,
			case when request.target_type='mod_resource' then 'mod'
				when request.target_type in ('mod','modpack','plugin','map','resource_pack','shader_pack','datapack','addon') then request.target_type
				else '' end project_type,
			coalesce(direct_mod.id,direct_modpack.id,direct_simple.id,resource_mod.id) project_id,
			case
				when request.target_type='community_post' and post.kind in ('tutorial','discussion') then post.author_id
				when request.target_type='blueprint' then blueprint.owner_id
				when request.target_type='skin' then skin.owner_id
				when request.target_type='player_profile' then player.user_id
			end owner_id,
			case
				when request.target_type='mod' then direct_mod.id is not null
				when request.target_type='modpack' then direct_modpack.id is not null
				when request.target_type in ('plugin','map','resource_pack','shader_pack','datapack','addon') then direct_simple.id is not null
				when request.target_type='mod_resource' then resource_mod.id is not null
				when request.target_type='community_post' then post.id is not null
				when request.target_type='blueprint' then blueprint.id is not null
				when request.target_type='skin' then skin.id is not null
				when request.target_type='player_profile' then player.id is not null
				else true
			end resolved
		from requested request
		left join mods direct_mod on request.target_type='mod' and direct_mod.id=request.target_id
		left join modpacks direct_modpack on request.target_type='modpack' and direct_modpack.id=request.target_id
		left join simple_projects direct_simple on request.target_type=direct_simple.project_type and direct_simple.id=request.target_id
		left join mod_content_versions resource_version
			on request.target_type='mod_resource' and resource_version.id=request.target_version_key
		left join mods resource_mod on resource_mod.id=resource_version.mod_id
		left join community_posts post on request.target_type='community_post' and post.id=request.target_id
		left join blueprints blueprint on request.target_type='blueprint' and blueprint.id=request.target_id
		left join skin_assets skin on request.target_type='skin' and skin.id=request.target_id
		left join player_profiles player on request.target_type='player_profile' and player.id=request.target_id
	)
	select target_type,target_id,target_version_key,resolved,
		case
			when project_id is not null then exists(
				select 1 from user_blocks block
				join effective_project_access access on access.user_id=block.blocker_id
				where block.blocked_id=$4 and access.project_type=target_facts.project_type
				  and access.project_id=target_facts.project_id
				  and access.access_level='developer')
			when owner_id is not null and owner_id<>$4 then exists(
				select 1 from user_blocks block where block.blocker_id=owner_id and block.blocked_id=$4)
			else false
		end
	from target_facts`, types, ids, versionKeys, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var identity commentTargetIdentity
		var resolved, blocked bool
		if err = rows.Scan(&identity.Type, &identity.ID, &identity.VersionKey, &resolved, &blocked); err != nil {
			return nil, err
		}
		if !resolved {
			return nil, pgx.ErrNoRows
		}
		result[identity] = blocked
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
