package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	siteLogoPendingSource      = "site_logo_pending"
	siteLogoRetention          = time.Hour
	siteLogoMaximumInputBytes  = 1024 * 1024
	siteLogoMaximumOutputBytes = 2 * 1024 * 1024
	siteLogoMaximumEdge        = 512
)

var errSiteLogoUnavailable = errors.New("shared site logo is unavailable")

func (s *Server) uploadSiteLogo(w http.ResponseWriter, r *http.Request) {
	contentType := normalizeRasterContentType(r.Header.Get("Content-Type"))
	if contentType != "image/png" && contentType != "image/webp" {
		writeAPIError(w, http.StatusUnsupportedMediaType, "SITE_LOGO_FORMAT_INVALID", "logo derivative must be PNG or WebP", 0, nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, siteLogoMaximumInputBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "SITE_LOGO_SIZE_LIMIT", "logo derivative is too large or incomplete", 0, nil)
		return
	}
	data, err := sanitizeSharedSiteLogo(raw, contentType)
	if err != nil {
		writeAPIError(w, http.StatusUnprocessableEntity, "SITE_LOGO_IMAGE_INVALID", "logo must be a complete bounded static raster", 0, nil)
		return
	}
	_, cfg, err := s.ossClient(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "SITE_LOGO_STORAGE_UNAVAILABLE", "shared logo storage is unavailable", 0, nil)
		return
	}
	objectKey := buildOSSObjectKeyForFile(cfg.Prefix, "site/logo", "logo.png")
	fileID, err := s.writeGeneratedOSSObject(r.Context(), objectKey, "site-logo.png", "image/png", data, currentClaims(r).Subject, siteLogoPendingSource)
	if err != nil {
		status := http.StatusBadGateway
		var databaseError *pgconn.PgError
		if errors.As(err, &databaseError) {
			status = http.StatusInternalServerError
		}
		slog.Warn("shared site logo upload failed", "error", err)
		writeAPIError(w, status, "SITE_LOGO_UPLOAD_FAILED", "failed to store the shared logo", 0, nil)
		return
	}
	var publicID string
	if err = s.db.QueryRow(r.Context(), `select public_id from oss_files where id=$1`, fileID).Scan(&publicID); err != nil {
		// Registered temporary assets retain a bounded TTL even when a client
		// disappears or the final response lookup fails.
		writeAPIError(w, http.StatusInternalServerError, "SITE_LOGO_UPLOAD_FAILED", "failed to read the stored logo identity", 0, nil)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"url": "/site-assets/site-logo-" + publicID + ".png"})
}

func sanitizeSharedSiteLogo(data []byte, contentType string) ([]byte, error) {
	if len(data) == 0 || len(data) > siteLogoMaximumInputBytes {
		return nil, errors.New("invalid logo byte budget")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > siteLogoMaximumEdge || config.Height > siteLogoMaximumEdge {
		return nil, errors.New("invalid logo dimensions")
	}
	if _, err = validateRasterImageBytes(data, contentType); err != nil {
		return nil, err
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if err = png.Encode(&output, decoded); err != nil {
		return nil, err
	}
	if output.Len() > siteLogoMaximumOutputBytes {
		return nil, errors.New("invalid logo output budget")
	}
	return output.Bytes(), nil
}

func siteLogoPublicID(value string) string {
	match := publicSiteLogoPathPattern.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func (s *Server) publicSiteLogo(w http.ResponseWriter, r *http.Request) {
	publicID := r.PathValue("id")
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusNotFound, "site logo not found")
		return
	}
	var objectKey string
	err := s.db.QueryRow(r.Context(), `select object_key from oss_files where public_id=$1
		and status='active' and scan_status in ('clean','trusted_generated') and content_type='image/png'
		and (source='site_logo' or (source='site_logo_pending' and created_at>now()-make_interval(secs=>$2)))`, publicID, int(siteLogoRetention/time.Second)).Scan(&objectKey)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "site logo not found")
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "SITE_LOGO_UNAVAILABLE", "shared logo is temporarily unavailable", 0, nil)
		return
	}
	s.redirectOSSObjectAccess(w, r, objectKey, ossObjectAccessOptions{ContentDisposition: `inline; filename="site-logo.png"`})
}

func (s *Server) saveSiteGeneralConfig(ctx context.Context, payload siteGeneralConfig, raw []byte, actorID int64) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	initial, err := json.Marshal(defaultSiteGeneralConfig())
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `insert into system_settings(key,value) values($1,$2::jsonb) on conflict(key) do nothing`, siteGeneralSettingKey, initial); err != nil {
		return err
	}
	var previousRaw []byte
	if err = tx.QueryRow(ctx, `select value from system_settings where key=$1 for update`, siteGeneralSettingKey).Scan(&previousRaw); err != nil {
		return err
	}
	var previous siteGeneralConfig
	if err = json.Unmarshal(previousRaw, &previous); err != nil {
		return err
	}
	newID, oldID := siteLogoPublicID(payload.LogoURL), siteLogoPublicID(previous.LogoURL)
	if newID != "" {
		var fileID int64
		if err = tx.QueryRow(ctx, `select id from oss_files where public_id=$1 and status='active'
			and scan_status in ('clean','trusted_generated') and content_type='image/png'
			and (source='site_logo' or (source='site_logo_pending' and created_at>now()-make_interval(secs=>$2))) for update`, newID, int(siteLogoRetention/time.Second)).Scan(&fileID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errSiteLogoUnavailable
			}
			return err
		}
		if _, err = tx.Exec(ctx, `update oss_files set source='site_logo',updated_at=now() where id=$1`, fileID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `update system_settings set value=$2::jsonb,updated_by=$3,updated_at=now() where key=$1`, siteGeneralSettingKey, raw, actorID); err != nil {
		return err
	}
	if oldID != "" && oldID != newID {
		var fileID int64
		err = tx.QueryRow(ctx, `select id from oss_files where public_id=$1 and source='site_logo' for update`, oldID).Scan(&fileID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil {
			if err = s.tombstoneOSSFileTx(ctx, tx, fileID, "site_logo_replaced"); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

func (worker *MaintenanceWorker) pruneSiteLogoUploads(ctx context.Context) {
	server := &Server{db: worker.db}
	for batch := 0; batch < maintenanceMaxBatches && ctx.Err() == nil; batch++ {
		tx, err := worker.db.Begin(ctx)
		if err != nil {
			slog.Warn("begin shared logo cleanup failed", "error", err)
			return
		}
		rows, err := tx.Query(ctx, `select id from oss_files where source='site_logo_pending' and status='active'
			and created_at<=now()-make_interval(secs=>$2) order by created_at,id for update skip locked limit $1`, maintenanceBatchSize, int(siteLogoRetention/time.Second))
		if err != nil {
			_ = tx.Rollback(ctx)
			slog.Warn("claim shared logo cleanup failed", "error", err)
			return
		}
		ids := make([]int64, 0, maintenanceBatchSize)
		for rows.Next() {
			var id int64
			if err = rows.Scan(&id); err != nil {
				break
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err == nil {
			err = rows.Err()
		}
		if err == nil {
			for _, id := range ids {
				if err = server.tombstoneOSSFileTx(ctx, tx, id, "site_logo_upload_expired"); err != nil {
					break
				}
			}
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			slog.Warn("queue shared logo cleanup failed", "error", err)
			return
		}
		if err = tx.Commit(ctx); err != nil {
			slog.Warn("commit shared logo cleanup failed", "error", err)
			return
		}
		if len(ids) < maintenanceBatchSize {
			return
		}
	}
}
