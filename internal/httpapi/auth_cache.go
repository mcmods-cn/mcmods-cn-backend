package httpapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

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

func rbacCacheKey(version, userID, authVersion int64) string {
	return fmt.Sprintf("authz:v%d:user:%d:authVersion:%d", version, userID, authVersion)
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
	versionRaw, ok := s.cache.Get(ctx, userAuthVersionCacheKey(record.PublicID))
	if !ok {
		// An evicted version key cannot be treated as authorization. Revalidate
		// once against PostgreSQL and repopulate the bounded version marker.
		revalidated, loadErr := s.loadSessionSubject(ctx, claims)
		if loadErr != nil {
			s.cache.Delete(ctx, key)
			return cachedSessionSubject{}, loadErr
		}
		record = revalidated
		s.cache.Set(ctx, userAuthVersionCacheKey(record.PublicID), []byte(strconv.FormatInt(record.AuthVersion, 10)), min(ttl, 10*time.Second))
	} else if string(versionRaw) != strconv.FormatInt(record.AuthVersion, 10) {
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
		s.cache.Set(ctx, userAuthVersionCacheKey(claims.PublicSubject), []byte(strconv.FormatInt(claims.AuthVersion, 10)), min(ttl, 10*time.Second))
	}
}

func (s *Server) invalidateSessionCache(ctx context.Context, claims security.Claims) {
	s.cache.Delete(ctx, sessionCacheKey(claims.SessionID), sessionNegativeCacheKey(claims.SessionID))
}

func (s *Server) loadRBACVersion(ctx context.Context) (int64, error) {
	raw, err := s.cache.GetOrLoadTTL(ctx, "versions:rbac", 10*time.Second, func(loadCtx context.Context) ([]byte, error) {
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
