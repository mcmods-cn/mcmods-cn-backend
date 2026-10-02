package httpapi

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var roleTrackCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type roleTrackPayload struct {
	Code        string   `json:"code"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Roles       []string `json:"roles"`
}

func (s *Server) roleTracks(w http.ResponseWriter, r *http.Request) {
	tracks, err := s.loadRoleTracks(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取权限组线路失败")
		return
	}
	writeJSON(w, http.StatusOK, tracks)
}

func (s *Server) createRoleTrack(w http.ResponseWriter, r *http.Request) {
	s.saveRoleTrack(w, r, "")
}

func (s *Server) updateRoleTrack(w http.ResponseWriter, r *http.Request) {
	s.saveRoleTrack(w, r, strings.TrimSpace(r.PathValue("code")))
}

func (s *Server) saveRoleTrack(w http.ResponseWriter, r *http.Request, currentCode string) {
	var payload roleTrackPayload
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	payload.Code = strings.ToLower(strings.TrimSpace(payload.Code))
	payload.Name = strings.TrimSpace(payload.Name)
	payload.Description = strings.TrimSpace(payload.Description)
	payload.Roles = normalizeCodes(payload.Roles)
	if !roleTrackCodePattern.MatchString(payload.Code) || payload.Name == "" {
		writeError(w, http.StatusBadRequest, "线路代码或名称不正确")
		return
	}
	if len(payload.Roles) < 2 {
		writeError(w, http.StatusBadRequest, "权限组线路至少需要两个权限组")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存权限组线路失败")
		return
	}
	defer tx.Rollback(r.Context())
	if currentCode == "" {
		_, err = tx.Exec(
			r.Context(),
			`insert into permission_role_tracks (code, name, description, updated_at) values ($1, $2, $3, now())`,
			payload.Code,
			payload.Name,
			payload.Description,
		)
	} else {
		if _, err := tx.Exec(r.Context(), `delete from permission_role_track_roles where track_code = $1`, currentCode); err != nil {
			writeError(w, http.StatusInternalServerError, "保存线路权限组失败")
			return
		}
		_, err = tx.Exec(
			r.Context(),
			`update permission_role_tracks set code = $2, name = $3, description = $4, updated_at = now() where code = $1`,
			currentCode,
			payload.Code,
			payload.Name,
			payload.Description,
		)
	}
	if err != nil {
		writeError(w, http.StatusConflict, "线路代码已存在或线路不存在")
		return
	}
	if _, err := tx.Exec(r.Context(), `delete from permission_role_track_roles where track_code = $1`, payload.Code); err != nil {
		writeError(w, http.StatusInternalServerError, "保存线路权限组失败")
		return
	}
	for position, role := range payload.Roles {
		tag, err := tx.Exec(
			r.Context(),
			`insert into permission_role_track_roles (track_code, role_id, position)
			 select $1, id, $3 from roles where code = $2 and status = 'active'`,
			payload.Code,
			role,
			position,
		)
		if err != nil || tag.RowsAffected() == 0 {
			writeError(w, http.StatusBadRequest, "权限组不存在: "+role)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "保存权限组线路失败")
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) deleteRoleTrack(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.PathValue("code"))
	tag, err := s.db.Exec(r.Context(), `delete from permission_role_tracks where code = $1`, code)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "删除权限组线路失败")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "权限组线路不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) upgradeUserRoleTrack(w http.ResponseWriter, r *http.Request) {
	s.applyUserRoleTrack(w, r, 1)
}

func (s *Server) downgradeUserRoleTrack(w http.ResponseWriter, r *http.Request) {
	s.applyUserRoleTrack(w, r, -1)
}

func (s *Server) applyUserRoleTrack(w http.ResponseWriter, r *http.Request, direction int) {
	identity, ok := s.pathUserIdentity(w, r)
	if !ok {
		return
	}
	userID := identity.InternalID
	trackCode := strings.TrimSpace(r.PathValue("code"))
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "调整用户权限组失败")
		return
	}
	defer tx.Rollback(r.Context())
	var lockedUserID int64
	if err = tx.QueryRow(r.Context(), `select id from users where id=$1 for update`, userID).Scan(&lockedUserID); err != nil {
		writeError(w, http.StatusInternalServerError, "锁定用户授权失败")
		return
	}
	var lockedTrackCode string
	if err = tx.QueryRow(r.Context(), `select code from permission_role_tracks where code=$1 for share`, trackCode).
		Scan(&lockedTrackCode); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "权限组线路不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "读取权限组线路失败")
		return
	}
	roleRows, err := tx.Query(r.Context(), `select role.code from permission_role_track_roles track_role
		join roles role on role.id=track_role.role_id where track_role.track_code=$1
		order by track_role.position for share of track_role`, trackCode)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取权限组线路失败")
		return
	}
	roles := make([]string, 0)
	for roleRows.Next() {
		var role string
		if err = roleRows.Scan(&role); err != nil {
			roleRows.Close()
			writeError(w, http.StatusInternalServerError, "读取权限组线路失败")
			return
		}
		roles = append(roles, role)
	}
	if err = roleRows.Err(); err != nil {
		roleRows.Close()
		writeError(w, http.StatusInternalServerError, "读取权限组线路失败")
		return
	}
	roleRows.Close()
	if len(roles) < 2 {
		writeError(w, http.StatusNotFound, "权限组线路不存在")
		return
	}
	rows, err := tx.Query(r.Context(), `select role.code,binding.expires_at from user_role_bindings binding
		join roles role on role.id=binding.role_id
		where binding.user_id=$1 and binding.source=$2 and binding.source_key=''
		  and role.code=any($3::text[]) order by role.code`,
		userID, authorizationSourceManual, roles)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取用户权限组失败")
		return
	}
	current := make([]string, 0)
	currentExpiries := make(map[string]*time.Time)
	for rows.Next() {
		var role string
		var expiresAt *time.Time
		if err = rows.Scan(&role, &expiresAt); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "读取用户权限组失败")
			return
		}
		current = append(current, role)
		currentExpiries[role] = expiresAt
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		writeError(w, http.StatusInternalServerError, "读取用户权限组失败")
		return
	}
	rows.Close()
	if len(current) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"changed": false, "roles": []string{}})
		return
	}
	targets := shiftRoleTrackRoles(roles, current, direction)
	targetExpiries := make(map[string]*time.Time, len(targets))
	for _, role := range current {
		shifted := shiftRoleTrackRoles(roles, []string{role}, direction)
		if len(shifted) == 0 {
			continue
		}
		target := shifted[0]
		expiresAt := currentExpiries[role]
		currentExpiry, exists := targetExpiries[target]
		if !exists || currentExpiry != nil && (expiresAt == nil || expiresAt.After(*currentExpiry)) {
			targetExpiries[target] = expiresAt
		}
	}
	if _, err := tx.Exec(
		r.Context(),
		`delete from user_role_bindings b using roles r
		 where b.role_id=r.id and b.user_id=$1 and b.source=$3 and b.source_key=''
		   and r.code=any($2::text[])`,
		userID,
		roles,
		authorizationSourceManual,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "调整用户权限组失败")
		return
	}
	for _, role := range targets {
		if _, err := tx.Exec(
			r.Context(),
			`insert into user_role_bindings (user_id,role_id,source,source_key,expires_at)
			 select $1,id,$3,'',$4 from roles where code=$2 on conflict do nothing`,
			userID,
			role,
			authorizationSourceManual,
			targetExpiries[role],
		); err != nil {
			writeError(w, http.StatusInternalServerError, "调整用户权限组失败")
			return
		}
	}
	action := "upgrade_role_track"
	if direction < 0 {
		action = "downgrade_role_track"
	}
	if err := s.auditPermissionChangeTx(r.Context(), tx, currentClaims(r).Subject, &userID, action, map[string]any{"track": trackCode, "from": current, "to": targets}); err != nil {
		writeError(w, http.StatusInternalServerError, "记录权限组调整失败")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "调整用户权限组失败")
		return
	}
	if !s.requireSecurityVersionRefresh(w, r, action, userID, s.refreshPermissionVersion(r.Context(), userID)) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"changed": true, "roles": targets})
}

func shiftRoleTrackRoles(track []string, current []string, direction int) []string {
	positions := make(map[string]int, len(track))
	for index, role := range track {
		positions[role] = index
	}
	targetSet := map[string]struct{}{}
	for _, role := range current {
		index, exists := positions[role]
		if !exists {
			continue
		}
		position := index + direction
		if position < 0 {
			position = 0
		}
		if position >= len(track) {
			position = len(track) - 1
		}
		targetSet[track[position]] = struct{}{}
	}
	targets := make([]string, 0, len(targetSet))
	for role := range targetSet {
		targets = append(targets, role)
	}
	sort.Slice(targets, func(i, j int) bool { return positions[targets[i]] < positions[targets[j]] })
	return targets
}

func (s *Server) loadRoleTracks(ctx context.Context) ([]roleTrackPayload, error) {
	rows, err := s.db.Query(ctx, `select track.code,track.name,track.description,
		coalesce(array_agg(role.code order by member.position) filter(where role.id is not null),'{}'::text[])
		from permission_role_tracks track
		left join permission_role_track_roles member on member.track_code=track.code
		left join roles role on role.id=member.role_id
		group by track.code,track.name,track.description order by track.code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tracks := make([]roleTrackPayload, 0)
	for rows.Next() {
		var track roleTrackPayload
		if err := rows.Scan(&track.Code, &track.Name, &track.Description, &track.Roles); err != nil {
			return nil, err
		}
		tracks = append(tracks, track)
	}
	return tracks, rows.Err()
}
