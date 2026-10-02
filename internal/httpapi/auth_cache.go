package httpapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

const authCacheSchemaVersion = 1

type cachedSessionSubject struct {
	SchemaVersion int    `json:"schemaVersion"`
	UserID        int64  `json:"userId"`
	PublicID      string `json:"publicId"`
	AuthVersion   int64  `json:"authVersion"`
	ExpiresAt     int64  `json:"expiresAt"`
}

func sessionCacheKey(sessionID string) string {
	return "session:" + hex.EncodeToString(security.SessionFingerprint(sessionID))
}

func sessionNegativeCacheKey(sessionID string) string {
	return "session-invalid:" + hex.EncodeToString(security.SessionFingerprint(sessionID))
}

func userAuthVersionCacheKey(publicID string) string {
	return "auth:user-version:" + publicID
}

func rbacCacheKey(version, projectACLVersion, userID, permissionVersion int64) string {
	return fmt.Sprintf("authz:v%d:acl%d:user:%d:p%d", version, projectACLVersion, userID, permissionVersion)
}

func (s *Server) resolveCachedSessionSubject(ctx context.Context, claims security.Claims) (cachedSessionSubject, error) {
	var record cachedSessionSubject
	cacheConfig := s.cache.Config()
	if !cacheConfig.AuthSessionCacheEnabled {
		return s.loadSessionSubject(ctx, claims)
	}
	if _, invalid := s.cache.Get(ctx, sessionNegativeCacheKey(claims.SessionID)); invalid {
		return record, pgx.ErrNoRows
	}
	key := sessionCacheKey(claims.SessionID)
	ttl := cacheConfig.SessionTTL
	remaining := time.Until(time.Unix(claims.ExpiresAt, 0))
	if ttl <= 0 || ttl > remaining {
		ttl = remaining
	}
	if ttl <= 0 {
		return record, pgx.ErrNoRows
	}
	raw, err := s.cache.GetOrLoadTTL(ctx, key, ttl, func(loadCtx context.Context) ([]byte, error) {
		loaded, loadErr := s.loadSessionSubject(loadCtx, claims)
		if loadErr != nil {
			return nil, loadErr
		}
		return json.Marshal(loaded)
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.cache.Set(ctx, sessionNegativeCacheKey(claims.SessionID), []byte("1"), 5*time.Second)
		}
		return record, err
	}
	if err = json.Unmarshal(raw, &record); err != nil || !validCachedSession(record, claims) {
		s.cache.Delete(ctx, key)
		if err == nil {
			err = errors.New("invalid cached session")
		}
		// Cache corruption is fail-closed, then recovered from PostgreSQL.
		record, err = s.loadSessionSubject(ctx, claims)
		if err != nil {
			return cachedSessionSubject{}, err
		}
		encoded, _ := json.Marshal(record)
		s.cache.Set(ctx, key, encoded, ttl)
	}
	versionRaw, versionErr := s.cache.GetSharedOrLoadTTL(ctx, userAuthVersionCacheKey(record.PublicID), min(ttl, 10*time.Second), func(loadCtx context.Context) ([]byte, error) {
		var version int64
		if scanErr := s.db.QueryRow(loadCtx, `select auth_version from users where public_id=$1 and status='active'`, record.PublicID).Scan(&version); scanErr != nil {
			return nil, scanErr
		}
		return []byte(strconv.FormatInt(version, 10)), nil
	})
	if versionErr != nil {
		s.cache.Delete(ctx, key)
		return cachedSessionSubject{}, versionErr
	}
	if string(versionRaw) != strconv.FormatInt(record.AuthVersion, 10) {
		s.cache.Delete(ctx, key)
		return cachedSessionSubject{}, pgx.ErrNoRows
	}
	return record, nil
}

func (s *Server) loadSessionSubject(ctx context.Context, claims security.Claims) (cachedSessionSubject, error) {
	record := cachedSessionSubject{SchemaVersion: authCacheSchemaVersion}
	err := s.db.QueryRow(ctx, `select users.id,users.public_id,users.auth_version,extract(epoch from session.expires_at)::bigint
		from auth_sessions session join users on users.id=session.user_id
		where session.session_hash=$1 and session.revoked_at is null and session.expires_at>now()
		  and session.auth_version=$2 and users.auth_version=$2
		  and users.public_id=$3 and users.status='active'`,
		security.SessionFingerprint(claims.SessionID), claims.AuthVersion, claims.PublicSubject,
	).Scan(&record.UserID, &record.PublicID, &record.AuthVersion, &record.ExpiresAt)
	return record, err
}

func validCachedSession(record cachedSessionSubject, claims security.Claims) bool {
	return record.SchemaVersion == authCacheSchemaVersion && record.UserID > 0 &&
		record.PublicID == claims.PublicSubject && record.AuthVersion == claims.AuthVersion &&
		record.ExpiresAt > time.Now().Unix()
}

func (s *Server) cacheIssuedSession(ctx context.Context, claims security.Claims, userID int64) {
	if !s.cache.Config().AuthSessionCacheEnabled {
		return
	}
	record := cachedSessionSubject{
		SchemaVersion: authCacheSchemaVersion, UserID: userID, PublicID: claims.PublicSubject,
		AuthVersion: claims.AuthVersion, ExpiresAt: claims.ExpiresAt,
	}
	raw, _ := json.Marshal(record)
	ttl := min(s.cache.Config().SessionTTL, time.Until(time.Unix(claims.ExpiresAt, 0)))
	if ttl > 0 {
		s.cache.Set(ctx, sessionCacheKey(claims.SessionID), raw, ttl)
		s.cache.SetShared(ctx, userAuthVersionCacheKey(claims.PublicSubject), []byte(strconv.FormatInt(claims.AuthVersion, 10)), min(ttl, 10*time.Second))
	}
}

func (s *Server) invalidateSessionCache(ctx context.Context, claims security.Claims) {
	s.cache.Delete(ctx, sessionCacheKey(claims.SessionID), sessionNegativeCacheKey(claims.SessionID))
}

func (s *Server) loadRBACVersion(ctx context.Context) (int64, error) {
	raw, err := s.cache.GetSharedOrLoadTTL(ctx, "versions:rbac", 10*time.Second, func(loadCtx context.Context) ([]byte, error) {
		var version int64
		if scanErr := s.db.QueryRow(loadCtx, `select version from runtime_versions where name='rbac'`).Scan(&version); scanErr != nil {
			return nil, scanErr
		}
		return []byte(strconv.FormatInt(version, 10)), nil
	})
	if err != nil {
		return 0, err
	}
	version, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || version < 1 {
		s.cache.Delete(ctx, "versions:rbac")
		return 0, errors.New("invalid RBAC version cache")
	}
	return version, nil
}

func (s *Server) refreshRBACVersion(ctx context.Context) error {
	return s.refreshSharedVersionPointer(ctx, "versions:rbac", func() (int64, error) {
		var version int64
		err := s.db.QueryRow(ctx, `select version from runtime_versions where name='rbac'`).Scan(&version)
		return version, err
	})
}

func (s *Server) loadProjectACLVersion(ctx context.Context) (int64, error) {
	raw, err := s.cache.GetSharedOrLoadTTL(ctx, "versions:project-acl", 10*time.Second, func(loadCtx context.Context) ([]byte, error) {
		var version int64
		if scanErr := s.db.QueryRow(loadCtx, `select version from runtime_versions where name='project_acl'`).Scan(&version); scanErr != nil {
			return nil, scanErr
		}
		return []byte(strconv.FormatInt(version, 10)), nil
	})
	if err != nil {
		return 0, err
	}
	version, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || version < 1 {
		s.cache.Delete(ctx, "versions:project-acl")
		return 0, errors.New("invalid project ACL version cache")
	}
	return version, nil
}

func (s *Server) refreshProjectACLVersion(ctx context.Context) error {
	return s.refreshSharedVersionPointer(ctx, "versions:project-acl", func() (int64, error) {
		var version int64
		err := s.db.QueryRow(ctx, `select version from runtime_versions where name='project_acl'`).Scan(&version)
		return version, err
	})
}

func (s *Server) loadPermissionVersion(ctx context.Context, userID int64) (int64, error) {
	key := querycache.UserPermissionVersionKey(userID)
	raw, err := s.cache.GetSharedOrLoadTTL(ctx, key, 10*time.Second, func(loadCtx context.Context) ([]byte, error) {
		var version int64
		if scanErr := s.db.QueryRow(loadCtx, `select permission_version from users where id=$1`, userID).Scan(&version); scanErr != nil {
			return nil, scanErr
		}
		return []byte(strconv.FormatInt(version, 10)), nil
	})
	if err != nil {
		return 0, err
	}
	version, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || version < 1 {
		s.cache.Delete(ctx, key)
		return 0, errors.New("invalid permission version cache")
	}
	return version, nil
}

// refreshPermissionVersion publishes the authoritative post-transaction
// version for a user. The database trigger owns the increment; this method
// only updates the shared pointer used by other API instances.
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
	invalidateErr := s.cache.DeleteShared(ctx, key)
	version, loadErr := load()
	if loadErr != nil {
		return errors.Join(invalidateErr, fmt.Errorf("load authoritative version for %s: %w", key, loadErr))
	}
	if !s.cache.Enabled() {
		return nil
	}
	if s.cache.SetShared(ctx, key, []byte(strconv.FormatInt(version, 10)), 10*time.Second) {
		// A successful SET overwrites any stale value even when the preceding
		// DEL reported a transient error.
		return nil
	}
	return errors.Join(invalidateErr, fmt.Errorf("publish authoritative version for %s", key))
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
