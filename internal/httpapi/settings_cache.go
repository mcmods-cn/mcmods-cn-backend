package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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
			if errors.Is(scanErr, pgx.ErrNoRows) {
				return nil, errors.New("settings version is missing")
			}
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

// Existing installations can omit a setting or individual fields. A stored
// value of the wrong type or a database error cannot supply an editable form.
func decodeOptionalSetting(raw []byte, readErr error, key string, defaults any) error {
	if errors.Is(readErr, pgx.ErrNoRows) {
		return nil
	}
	if readErr != nil {
		return readErr
	}
	fields, err := decodeStoredJSONObject(raw, key)
	if err != nil {
		return err
	}
	// The three editable settings use flat JSON objects. encoding/json
	// otherwise accepts null for a scalar and leaves the prefilled value.
	for name, value := range fields {
		if value == nil && nonNullableSettingField(defaults, name) {
			return fmt.Errorf("%s.%s cannot be null", key, name)
		}
	}
	return json.Unmarshal(raw, defaults)
}

func nonNullableSettingField(config any, name string) bool {
	t := reflect.TypeOf(config).Elem()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if !field.IsExported() || tag == "-" {
			continue
		}
		if tag == "" {
			tag = field.Name
		}
		if !strings.EqualFold(tag, name) {
			continue
		}
		switch field.Type.Kind() {
		case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
			return false
		default:
			return true
		}
	}
	return false
}

func (s *Server) readEditableSetting(ctx context.Context, key string, defaults any) error {
	var raw []byte
	err := s.db.QueryRow(ctx, `select value from system_settings where key=$1`, key).Scan(&raw)
	return decodeOptionalSetting(raw, err, key, defaults)
}

func checkEditableSettingTx(ctx context.Context, tx pgx.Tx, key string, defaults any) error {
	var raw []byte
	err := tx.QueryRow(ctx, `select value from system_settings where key=$1 for update`, key).Scan(&raw)
	return decodeOptionalSetting(raw, err, key, defaults)
}
