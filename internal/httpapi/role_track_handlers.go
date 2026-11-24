package httpapi

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var roleTrackCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type roleTrackPayload struct {
	Code        string   `json:"code"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Roles       []string `json:"roles"`
}

// A moderator can also be the target. Lock the distinct accounts in one order
// before taking a track or its memberships, including when two admins act on
// each other. The later session/permission check then uses those same rows.
const roleTrackAccountLockSQL = `select coalesce(bool_or(id=$1),false),coalesce(bool_or(id=$2),false)
	from (select id from users where id in ($1,$2) order by id for no key update) locked_accounts`

func lockRoleTrackAccounts(w http.ResponseWriter, r *http.Request, tx pgx.Tx, targetID int64) bool {
	var actorFound, targetFound bool
	if err := tx.QueryRow(r.Context(), roleTrackAccountLockSQL, currentClaims(r).Subject, targetID).Scan(&actorFound, &targetFound); err != nil {
		writeError(w, http.StatusServiceUnavailable, "锁定用户授权失败")
		return false
	}
	if !actorFound {
		writeCurrentWriteActorFailure(w, pgx.ErrNoRows)
		return false
	}
	if targetID != 0 && !targetFound {
		writeError(w, http.StatusNotFound, "用户不存在")
		return false
	}
	return true
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
	if !lockRoleTrackAccounts(w, r, tx, 0) {
		return
	}
	if currentCode == "" {
		_, err = tx.Exec(
			r.Context(),
			`insert into permission_role_tracks (code, name, description, updated_at) values ($1, $2, $3, now())`,
			payload.Code,
			payload.Name,
			payload.Description,
		)
	} else {
		if !lockRoleTrackForEdit(w, r, tx, currentCode, payload.Code, payload.Roles) {
			return
		}
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
		var databaseErr *pgconn.PgError
		switch {
		case isUniqueViolation(err):
			writeError(w, http.StatusConflict, "线路代码已存在")
		case errors.As(err, &databaseErr) && databaseErr.Code == "23503":
			writeError(w, http.StatusConflict, "权限组线路正在使用，无法更改代码")
		default:
			writeError(w, http.StatusInternalServerError, "保存权限组线路失败")
		}
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
		if err != nil {
			writeError(w, http.StatusInternalServerError, "保存线路权限组失败")
			return
		}
		if tag.RowsAffected() == 0 {
			writeError(w, http.StatusBadRequest, "权限组不存在: "+role)
			return
		}
	}
	if _, ok := requireCurrentWritePermission(w, r, tx, "permission.write"); !ok {
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "保存权限组线路失败")
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

// Level jobs snapshot ordered role IDs but day-to-day experience uses the active
// track. Hold the same track row as a level configuration write until commit.
func lockRoleTrackForEdit(w http.ResponseWriter, r *http.Request, tx pgx.Tx, code, nextCode string, nextRoles []string) bool {
	var lockedCode string
	if err := tx.QueryRow(r.Context(), `select code from permission_role_tracks where code=$1 for update`, code).Scan(&lockedCode); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "权限组线路不存在")
		} else {
			writeError(w, http.StatusInternalServerError, "读取权限组线路失败")
		}
		return false
	}

	// Read the active configuration in a new statement after the row lock.
	var breaksLevels bool
	err := tx.QueryRow(r.Context(), `select exists(
		select 1 from level_system_config where role_track_code=$1 and
		($2::text<>$1 or $3::text[] is distinct from array(
			select role.code from permission_role_track_roles member join roles role on role.id=member.role_id
			where member.track_code=$1 order by member.position)))`, lockedCode, nextCode, nextRoles).Scan(&breaksLevels)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取等级配置失败")
		return false
	}
	if breaksLevels {
		writeAPIError(w, http.StatusConflict, "ROLE_TRACK_USED_BY_LEVELS", "请先在等级设置中切换或停用此线路，再更改线路代码、成员或删除线路", 0, nil)
		return false
	}
	return true
}

func (s *Server) deleteRoleTrack(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.PathValue("code"))
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "删除权限组线路失败")
		return
	}
	defer tx.Rollback(r.Context())
	if !lockRoleTrackAccounts(w, r, tx, 0) || !lockRoleTrackForEdit(w, r, tx, code, "", nil) {
		return
	}
	if _, err = tx.Exec(r.Context(), `delete from permission_role_tracks where code=$1`, code); err != nil {
		writeError(w, http.StatusInternalServerError, "删除权限组线路失败")
		return
	}
	if _, ok := requireCurrentWritePermission(w, r, tx, "permission.write"); !ok {
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "删除权限组线路失败")
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
	if !lockRoleTrackAccounts(w, r, tx, userID) {
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
	actor, ok := requireCurrentWritePermission(w, r, tx, "permission.write")
	if !ok {
		return
	}
	targets, targetExpiries, changed := shiftRoleTrackRoles(roles, currentExpiries, direction)
	if !changed {
		skipRequestActivity(r)
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "调整用户权限组失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"changed": false, "roles": targets})
		return
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
	if err := s.auditPermissionChangeTx(r.Context(), tx, actor.Subject, &userID, action, map[string]any{"track": trackCode, "from": current, "to": targets}); err != nil {
		writeError(w, http.StatusInternalServerError, "记录权限组调整失败")
		return
	}
	if err = enqueueRequestActivityTx(r, tx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist role track activity")
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

func shiftRoleTrackRoles(track []string, current map[string]*time.Time, direction int) (targets []string, expiries map[string]*time.Time, changed bool) {
	targets = make([]string, 0, len(current))
	expiries = make(map[string]*time.Time, len(current))
	for index, role := range track {
		expiresAt, enrolled := current[role]
		if !enrolled {
			continue
		}
		target := track[max(0, min(index+direction, len(track)-1))]
		if target != role {
			changed = true
		}
		previous, exists := expiries[target]
		if !exists {
			targets = append(targets, target)
		}
		// A merge keeps permanent membership or the longest remaining grant.
		if !exists || previous != nil && (expiresAt == nil || expiresAt.After(*previous)) {
			expiries[target] = expiresAt
		}
	}
	return targets, expiries, changed
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
