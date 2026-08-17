package httpapi

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
)

var redisPublicSettingKeys = map[string]bool{
	"site.general":             true,
	"markdown.rendering":       true,
	"profile.config":           true,
	"minecraft.versions":       true,
	"review.config":            true,
	"permission.default_roles": true,
}

func settingsCacheKey(version int64, key string) string {
	return "settings:v" + strconv.FormatInt(version, 10) + ":" + strings.ReplaceAll(key, " ", "_")
}

func (s *Server) loadCachedPublicSetting(ctx context.Context, key string) ([]byte, error) {
	if !redisPublicSettingKeys[key] || !s.cache.Config().SettingsCacheEnabled {
		var raw []byte
		err := s.db.QueryRow(ctx, `select value from system_settings where key=$1`, key).Scan(&raw)
		return raw, err
	}
	versionRaw, err := s.cache.GetOrLoadTTL(ctx, "versions:settings", 10*time.Second, func(loadCtx context.Context) ([]byte, error) {
		var version int64
		if scanErr := s.db.QueryRow(loadCtx, `select version from runtime_versions where name='settings'`).Scan(&version); scanErr != nil {
			return nil, scanErr
		}
		return []byte(strconv.FormatInt(version, 10)), nil
	})
	if err != nil {
		return nil, err
	}
	version, err := strconv.ParseInt(string(versionRaw), 10, 64)
	if err != nil || version < 1 {
		s.cache.Delete(ctx, "versions:settings")
		return nil, errors.New("invalid settings version")
	}
	return s.cache.GetOrLoadTTL(ctx, settingsCacheKey(version, key), time.Minute, func(loadCtx context.Context) ([]byte, error) {
		var raw []byte
		err := s.db.QueryRow(loadCtx, `select value from system_settings where key=$1`, key).Scan(&raw)
		return raw, err
	})
}

func (s *Server) invalidateSettingsCache(ctx context.Context) {
	s.cache.Delete(ctx, "versions:settings")
}
