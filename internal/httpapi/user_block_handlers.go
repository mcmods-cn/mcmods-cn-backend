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
	ossCfg := s.ossConfigFromSettings(r.Context())
	for rows.Next() {
		var item userConnectionItem
		if err = rows.Scan(&item.ID, &item.Username, &item.AvatarURL, &item.Signature); err != nil {
			writeError(w, http.StatusInternalServerError, "读取黑名单失败")
			return
		}
		item.AvatarURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(r.Context(), ossCfg, item.AvatarURL)
		if err != nil {
			writeError(w, http.StatusBadGateway, "生成用户头像访问链接失败")
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "读取黑名单失败")
		return
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
	if r.Method == http.MethodDelete {
		tag, err := s.db.Exec(r.Context(), `delete from user_blocks where blocker_id=$1 and blocked_id=$2`, claims.Subject, identity.InternalID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "解除拉黑失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"blocked": false, "removed": tag.RowsAffected() > 0})
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "拉黑用户失败")
		return
	}
	defer tx.Rollback(r.Context())
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
	if userID <= 0 {
		return false, nil
	}
	var ownerID *int64
	projectID := ""
	switch target.Type {
	case "mod":
		if err := s.db.QueryRow(ctx, `select created_by,project_code from mods where id=$1`, target.InternalID).Scan(&ownerID, &projectID); err != nil {
			return false, err
		}
	case "modpack":
		if err := s.db.QueryRow(ctx, `select created_by,public_id from modpacks where id=$1`, target.InternalID).Scan(&ownerID, &projectID); err != nil {
			return false, err
		}
	case "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon":
		if err := s.db.QueryRow(ctx, `select created_by,public_id from simple_projects where id=$1 and project_type=$2`, target.InternalID, target.Type).Scan(&ownerID, &projectID); err != nil {
			return false, err
		}
	case "mod_resource":
		if target.VersionID == nil {
			return false, pgx.ErrNoRows
		}
		if err := s.db.QueryRow(ctx, `select mod.created_by,mod.project_code
			from mod_content_versions version join mods mod on mod.id=version.mod_id
			where version.id=$1`, *target.VersionID).Scan(&ownerID, &projectID); err != nil {
			return false, err
		}
	case "community_post":
		var kind string
		if err := s.db.QueryRow(ctx, `select author_id,kind from community_posts where id=$1`, target.InternalID).Scan(&ownerID, &kind); err != nil {
			return false, err
		}
		if !communityPostAuthorModeratesComments(kind) {
			return false, nil
		}
	case "blueprint":
		if err := s.db.QueryRow(ctx, `select owner_id from blueprints where id=$1`, target.InternalID).Scan(&ownerID); err != nil {
			return false, err
		}
	case "skin":
		if err := s.db.QueryRow(ctx, `select owner_id from skin_assets where id=$1`, target.InternalID).Scan(&ownerID); err != nil {
			return false, err
		}
	case "player_profile":
		if err := s.db.QueryRow(ctx, `select user_id from player_profiles where id=$1`, target.InternalID).Scan(&ownerID); err != nil {
			return false, err
		}
	default:
		return false, nil
	}

	owner := int64(0)
	if ownerID != nil {
		owner = *ownerID
	}
	if projectID == "" {
		return s.userBlocksActor(ctx, owner, userID)
	}
	var blocked bool
	err := s.db.QueryRow(ctx, `select exists(
		select 1 from user_blocks block
		where block.blocked_id=$1 and (
			block.blocker_id=$2 or exists(
				select 1 from user_role_bindings binding
				join roles role on role.id=binding.role_id and role.status='active'
				where binding.user_id=block.blocker_id and role.code='project_owner.'||$3
			)
		))`, userID, owner, projectID).Scan(&blocked)
	return blocked, err
}
