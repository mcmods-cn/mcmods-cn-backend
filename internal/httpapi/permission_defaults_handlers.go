package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

const permissionDefaultsSettingKey = "permission.default_roles"

type permissionDefaultsPayload struct {
	RegisteredRole string `json:"registeredRole"`
	BannedRole     string `json:"bannedRole"`
}

type updateUserStatusRequest struct {
	Status string `json:"status"`
}

func (s *Server) getPermissionDefaults(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.permissionDefaultsFromSettings(r.Context()))
}

func (s *Server) updatePermissionDefaults(w http.ResponseWriter, r *http.Request) {
	var payload permissionDefaultsPayload
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	payload = normalizePermissionDefaults(payload)
	if payload.RegisteredRole != "" && payload.RegisteredRole == payload.BannedRole {
		writeError(w, http.StatusBadRequest, "新注册用户权限组和封禁用户权限组不能相同")
		return
	}
	for _, role := range []string{payload.RegisteredRole, payload.BannedRole} {
		if role == "" {
			continue
		}
		var exists bool
		if err := s.db.QueryRow(r.Context(), `select exists(select 1 from roles where code = $1 and status = 'active')`, role).Scan(&exists); err != nil || !exists {
			writeError(w, http.StatusBadRequest, "权限组不存在: "+role)
			return
		}
	}
	raw, _ := json.Marshal(payload)
	_, err := s.db.Exec(
		r.Context(),
		`insert into system_settings (key, value, updated_by, updated_at)
		 values ($1, $2::jsonb, $3, now())
		 on conflict (key) do update
		 set value = excluded.value, updated_by = excluded.updated_by, updated_at = now()`,
		permissionDefaultsSettingKey,
		string(raw),
		currentClaims(r).Subject,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存默认权限组失败")
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) updateUserStatus(w http.ResponseWriter, r *http.Request) {
	userID, ok := pathUserID(w, r)
	if !ok {
		return
	}
	var request updateUserStatusRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	request.Status = strings.ToLower(strings.TrimSpace(request.Status))
	if request.Status != "active" && request.Status != "banned" && request.Status != "disabled" && request.Status != "deleted" {
		writeError(w, http.StatusBadRequest, "用户状态不正确")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "修改用户状态失败")
		return
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(r.Context(), `update users set status = $2, updated_at = now() where id = $1`, userID, request.Status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "修改用户状态失败")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	if request.Status == "banned" {
		if err := s.assignConfiguredRoleTx(r.Context(), tx, userID, "banned"); err != nil {
			writeError(w, http.StatusInternalServerError, "分配封禁权限组失败")
			return
		}
	} else if err := s.removeConfiguredRoleTx(r.Context(), tx, userID, "banned"); err != nil {
		writeError(w, http.StatusInternalServerError, "移除封禁权限组失败")
		return
	}
	s.auditPermissionChangeTx(r.Context(), tx, currentClaims(r).Subject, &userID, "update_user_status", map[string]any{"status": request.Status})
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "修改用户状态失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": userID, "status": request.Status})
}

func (s *Server) permissionDefaultsFromSettings(ctx context.Context) permissionDefaultsPayload {
	var payload permissionDefaultsPayload
	var raw []byte
	if err := s.db.QueryRow(ctx, `select value from system_settings where key = $1`, permissionDefaultsSettingKey).Scan(&raw); err == nil {
		_ = json.Unmarshal(raw, &payload)
	}
	return normalizePermissionDefaults(payload)
}

func normalizePermissionDefaults(payload permissionDefaultsPayload) permissionDefaultsPayload {
	payload.RegisteredRole = normalizeCode(payload.RegisteredRole)
	payload.BannedRole = normalizeCode(payload.BannedRole)
	return payload
}

func (s *Server) assignConfiguredRoleTx(ctx context.Context, tx pgx.Tx, userID int64, kind string) error {
	defaults := s.permissionDefaultsFromSettings(ctx)
	role := defaults.RegisteredRole
	if kind == "banned" {
		role = defaults.BannedRole
	}
	if role == "" {
		return nil
	}
	_, err := tx.Exec(
		ctx,
		`insert into user_role_bindings (user_id, role_id)
		 select $1, id from roles where code = $2 and status = 'active'
		 on conflict do nothing`,
		userID,
		role,
	)
	return err
}

func (s *Server) removeConfiguredRoleTx(ctx context.Context, tx pgx.Tx, userID int64, kind string) error {
	defaults := s.permissionDefaultsFromSettings(ctx)
	role := defaults.RegisteredRole
	if kind == "banned" {
		role = defaults.BannedRole
	}
	if role == "" {
		return nil
	}
	_, err := tx.Exec(
		ctx,
		`delete from user_role_bindings b using roles r
		 where b.role_id = r.id and b.user_id = $1 and r.code = $2`,
		userID,
		role,
	)
	return err
}
