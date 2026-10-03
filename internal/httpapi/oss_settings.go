package httpapi

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Configuration mutation uses the strict loader. Legacy object reads keep their
// disabled fallback without turning an unavailable store into usable secrets.
func (s *Server) ossConfigFromSettings(ctx context.Context) ossConfigPayload {
	return s.ossConfigFromSettingsWithQueryer(ctx, s.db)
}

type ossConfigQueryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Server) ossConfigFromSettingsWithQueryer(ctx context.Context, db ossConfigQueryRower) ossConfigPayload {
	payload, err := s.ossConfigFromSettingsStrictWithQueryer(ctx, db)
	if err != nil {
		return defaultOSSConfig()
	}
	return payload
}

func (s *Server) ossConfigFromSettingsStrictWithQueryer(ctx context.Context, db ossConfigQueryRower) (ossConfigPayload, error) {
	payload := defaultOSSConfig()
	var raw []byte
	err := db.QueryRow(ctx, `select value from system_settings where key = 'oss.aliyun'`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return payload, nil
	}
	if err != nil {
		return ossConfigPayload{}, err
	}
	if err := s.openSystemSetting(raw, &payload); err != nil {
		return ossConfigPayload{}, err
	}
	payload = normalizeOSSConfig(payload)
	if payload.DownloadURLTTLMinutes <= 0 {
		payload.DownloadURLTTLMinutes = 10
	}
	return payload, nil
}
