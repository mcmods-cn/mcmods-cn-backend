package httpapi

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

// A read from the PostgreSQL primary supplies the cache address for a resolved
// permission set. Both global generations and the user's version are observed
// in the same statement snapshot as the session.
type permissionVersions struct {
	User       int64
	RBAC       int64
	ProjectACL int64
}

const permissionVersionColumns = `users.permission_version,
    (select version from runtime_versions where name='rbac'),
    (select version from runtime_versions where name='project_acl')`

func readPermissionVersions(ctx context.Context, db databaseQuery, userID int64) (permissionVersions, error) {
	var v permissionVersions
	err := db.QueryRow(ctx, `select `+permissionVersionColumns+` from users where id=$1`, userID).Scan(&v.User, &v.RBAC, &v.ProjectACL)
	return v, err
}

func sessionCacheKey(sessionID string) string {
	return "session:" + hex.EncodeToString(security.SessionFingerprint(sessionID))
}

func sessionNegativeCacheKey(sessionID string) string {
	return "session-invalid:" + hex.EncodeToString(security.SessionFingerprint(sessionID))
}

func sessionRevokedCacheKey(sessionID string) string {
	return "session-revoked:" + hex.EncodeToString(security.SessionFingerprint(sessionID))
}

func userAuthVersionCacheKey(publicID string) string {
	return "auth:user-version:" + publicID
}

func rbacCacheKey(version, projectACLVersion, userID, permissionVersion int64) string {
	return fmt.Sprintf("authz:2:v%d:acl%d:user:%d:p%d", version, projectACLVersion, userID, permissionVersion)
}

// Only negative information can short-circuit a session read. A Redis restart,
// eviction or a late publisher cannot establish a session or choose its grants.
func (s *Server) resolveSessionSubject(ctx context.Context, claims security.Claims, versions *permissionVersions) (int64, error) {
	if claims.ExpiresAt <= time.Now().Unix() {
		return 0, pgx.ErrNoRows
	}
	cacheDenials := s.cache.Config().AuthSessionCacheEnabled
	if cacheDenials {
		if s.cache.HasMarker(ctx, sessionNegativeCacheKey(claims.SessionID)) {
			return 0, pgx.ErrNoRows
		}
	}
	userID, err := readSessionSubject(ctx, s.db, claims, false, versions)
	if err != nil {
		if cacheDenials && errors.Is(err, pgx.ErrNoRows) {
			ttl := min(5*time.Second, time.Until(time.Unix(claims.ExpiresAt, 0)))
			if configured := s.cache.Config().SessionTTL; configured > 0 {
				ttl = min(ttl, configured)
			}
			if ttl > 0 {
				s.cache.Set(ctx, sessionNegativeCacheKey(claims.SessionID), []byte("1"), ttl)
			}
		}
		return 0, err
	}
	// The statement may have started before a concurrent logout. Keep the
	// existing post-read revocation check when the shared marker is available.
	if cacheDenials {
		if s.cache.HasMarker(ctx, sessionRevokedCacheKey(claims.SessionID)) {
			return 0, pgx.ErrNoRows
		}
	}
	return userID, nil
}

func (s *Server) revokeSessionCache(ctx context.Context, claims security.Claims) error {
	if s.cache != nil && s.cache.Enabled() {
		remaining := time.Until(time.Unix(claims.ExpiresAt, 0))
		if remaining > 0 && !s.cache.SetShared(ctx, sessionRevokedCacheKey(claims.SessionID), []byte("1"), remaining) {
			return errors.New("publish shared session revocation failed")
		}
	}
	return s.cache.DeleteShared(ctx, sessionCacheKey(claims.SessionID), sessionNegativeCacheKey(claims.SessionID))
}

// Transactional review callers also lock the current user and session. Realtime
// checks need only the identity; HTTP uses the same predicate plus the versions.
func readSessionSubject(ctx context.Context, db databaseQuery, claims security.Claims, lock bool, versions *permissionVersions) (int64, error) {
	var userID int64
	columns := "users.id"
	targets := []any{&userID}
	if versions != nil {
		columns += "," + permissionVersionColumns
		targets = append(targets, &versions.User, &versions.RBAC, &versions.ProjectACL)
	}
	query := `select ` + columns + ` from auth_sessions session join users on users.id=session.user_id
        where session.session_hash=$1 and session.revoked_at is null and session.expires_at>statement_timestamp()
          and session.auth_version=$2 and users.auth_version=$2 and users.public_id=$3 and users.status='active'`
	if lock {
		query += " for share of users,session"
	}
	err := db.QueryRow(ctx, query, security.SessionFingerprint(claims.SessionID), claims.AuthVersion, claims.PublicSubject).Scan(targets...)
	return userID, err
}

func (s *Server) refreshRBACVersion(ctx context.Context) error {
	return s.refreshSharedVersionPointer(ctx, "versions:rbac", func() (int64, error) {
		var version int64
		err := s.db.QueryRow(ctx, `select version from runtime_versions where name='rbac'`).Scan(&version)
		return version, err
	})
}

func (s *Server) refreshProjectACLVersion(ctx context.Context) error {
	return s.refreshSharedVersionPointer(ctx, "versions:project-acl", func() (int64, error) {
		var version int64
		err := s.db.QueryRow(ctx, `select version from runtime_versions where name='project_acl'`).Scan(&version)
		return version, err
	})
}

// refreshPermissionVersion publishes the authoritative post-transaction
// version for a user. The trigger owns the increment; the shared pointer
// remains a compatibility signal for other API and Worker versions.
func (s *Server) refreshPermissionVersion(ctx context.Context, userID int64) error {
	return s.refreshSharedVersionPointer(ctx, querycache.UserPermissionVersionKey(userID), func() (int64, error) {
		var version int64
		err := s.db.QueryRow(ctx, `select permission_version from users where id=$1`, userID).Scan(&version)
		return version, err
	})
}

func (s *Server) refreshAuthVersion(ctx context.Context, userID int64) error {
	var publicID string
	if err := s.db.QueryRow(ctx, `select public_id from users where id=$1`, userID).Scan(&publicID); err != nil {
		return err
	}
	return s.refreshAuthVersionForIdentity(ctx, userID, publicID)
}

func (s *Server) refreshAuthVersionForIdentity(ctx context.Context, userID int64, publicID string) error {
	return s.refreshSharedVersionPointer(ctx, userAuthVersionCacheKey(publicID), func() (int64, error) {
		var version int64
		err := s.db.QueryRow(ctx, `select auth_version from users where id=$1 and public_id=$2`, userID, publicID).Scan(&version)
		return version, err
	})
}

func (s *Server) refreshSharedVersionPointer(ctx context.Context, key string, load func() (int64, error)) error {
	if s == nil || s.cache == nil {
		return errors.New("security version cache is unavailable")
	}
	version, loadErr := load()
	if loadErr != nil {
		return errors.Join(fmt.Errorf("load authoritative version for %s: %w", key, loadErr), s.cache.DeleteShared(ctx, key))
	}
	if !s.cache.Enabled() {
		return nil
	}
	return s.cache.PublishSecurityVersion(ctx, key, version, 10*time.Second)
}

func (s *Server) requireSecurityVersionRefresh(w http.ResponseWriter, r *http.Request, operation string, userID int64, refreshErrors ...error) bool {
	err := errors.Join(refreshErrors...)
	if err == nil {
		return true
	}
	s.securityVersionRefreshFailures.Add(1)
	slog.Error("security version refresh failed after commit",
		"operation", operation, "target_user_id", userID, "error", err)
	writeAPIError(w, http.StatusServiceUnavailable, "SECURITY_VERSION_REFRESH_FAILED",
		"安全状态已提交，但会话或权限缓存刷新失败", 1,
		map[string]any{"committed": true, "operation": operation})
	return false
}
