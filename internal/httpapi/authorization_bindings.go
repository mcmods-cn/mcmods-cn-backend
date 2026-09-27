package httpapi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	authorizationSourceManual        = "manual"
	authorizationSourceAccountStatus = "account_status"
	authorizationSourceGovernanceBan = "governance_ban"
	authorizationSourceLevelTrack    = "level_track"
	directPermissionSourcePreference = "user_preference"
	directPermissionSourceSeed       = "system_seed"
)

type manualRoleGrant struct {
	Code      string
	ExpiresAt *time.Time
}

type manualPermissionGrant struct {
	Code      string
	Allow     bool
	ExpiresAt *time.Time
}

func normalizeManualAuthorizationEntries(entries []userPermissionEntry) ([]manualRoleGrant, []manualPermissionGrant, error) {
	roles := make([]manualRoleGrant, 0)
	permissions := make([]manualPermissionGrant, 0)
	seenCodes := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		entry.Code = normalizeCode(entry.Code)
		entry.Source = normalizeCode(entry.Source)
		entry.SourceKey = strings.TrimSpace(entry.SourceKey)
		if entry.Code == "" {
			continue
		}
		if entry.Source != "" && entry.Source != authorizationSourceManual || entry.SourceKey != "" {
			return nil, nil, &requestError{message: "only manual authorization entries can be edited"}
		}
		if _, exists := seenCodes[entry.Code]; exists {
			return nil, nil, &requestError{message: "duplicate authorization entry: " + entry.Code}
		}
		seenCodes[entry.Code] = struct{}{}
		expiresAt, err := parseOptionalTime(entry.ExpiresAt)
		if err != nil {
			return nil, nil, &requestError{message: "有效期格式不正确: " + entry.Code}
		}
		if strings.HasPrefix(entry.Code, "group.") {
			role := strings.TrimPrefix(entry.Code, "group.")
			if !roleCodePattern.MatchString(role) {
				return nil, nil, &requestError{message: "权限组代码格式不正确: " + role}
			}
			if !entry.Allow {
				return nil, nil, &requestError{message: "permission groups cannot be assigned with allow=false: " + role}
			}
			roles = append(roles, manualRoleGrant{Code: role, ExpiresAt: expiresAt})
			continue
		}
		if !permissionCodePattern.MatchString(entry.Code) {
			return nil, nil, &requestError{message: "权限节点格式不正确: " + entry.Code}
		}
		permissions = append(permissions, manualPermissionGrant{
			Code: entry.Code, Allow: entry.Allow, ExpiresAt: expiresAt,
		})
	}
	return roles, permissions, nil
}

func replaceAccountStatusRoleTx(ctx context.Context, tx pgx.Tx, userID int64, kind, roleCode string) error {
	if _, err := tx.Exec(ctx, `delete from user_role_bindings
		where user_id=$1 and source=$2 and source_key=$3`,
		userID, authorizationSourceAccountStatus, kind); err != nil {
		return err
	}
	if roleCode == "" {
		return nil
	}
	tag, err := tx.Exec(ctx, `insert into user_role_bindings(user_id,role_id,source,source_key)
		select $1,id,$3,$4 from roles where code=$2 and status='active'`,
		userID, roleCode, authorizationSourceAccountStatus, kind)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("configured %s role %s is unavailable", kind, roleCode)
	}
	return nil
}

func replaceManualUserAuthorizationTx(
	ctx context.Context,
	tx pgx.Tx,
	userID int64,
	roles []manualRoleGrant,
	permissions []manualPermissionGrant,
) error {
	var lockedUserID int64
	if err := tx.QueryRow(ctx, `select id from users where id=$1 for update`, userID).Scan(&lockedUserID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from user_role_bindings where user_id=$1 and source=$2`,
		userID, authorizationSourceManual); err != nil {
		return err
	}
	for _, role := range roles {
		tag, err := tx.Exec(ctx, `insert into user_role_bindings(user_id,role_id,source,source_key,expires_at)
			select $1,id,$3,'',$4 from roles where code=$2 and status='active'`,
			userID, role.Code, authorizationSourceManual, role.ExpiresAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("role %s is unavailable", role.Code)
		}
	}
	if _, err := tx.Exec(ctx, `delete from user_permissions where user_id=$1 and source=$2`,
		userID, authorizationSourceManual); err != nil {
		return err
	}
	for _, permission := range permissions {
		tag, err := tx.Exec(ctx, `insert into user_permissions(
			user_id,permission_id,allow,source,source_key,expires_at,updated_at
		) select $1,id,$3,$4,'',$5,now() from permissions where code=$2`,
			userID, permission.Code, permission.Allow, authorizationSourceManual, permission.ExpiresAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("permission %s is unavailable", permission.Code)
		}
	}
	return nil
}
