package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const favoriteModpackExportPreviewTTL = 15 * time.Minute

var (
	errFavoriteModpackExportPreviewNotFound  = errors.New("favorite modpack export preview not found")
	errFavoriteModpackExportPreviewExpired   = errors.New("favorite modpack export preview expired")
	errFavoriteModpackExportPreviewConsumed  = errors.New("favorite modpack export preview already consumed")
	errFavoriteModpackExportPreviewMismatch  = errors.New("favorite modpack export preview does not match the confirmed selection")
	errFavoriteModpackExportPreviewAuthority = errors.New("favorite modpack export preview authority is unavailable")
)

type favoriteModpackExportStoredPreview struct {
	Preview               favoriteModpackExportPreviewSnapshot `json:"preview"`
	SourceProjectRouteIDs []*int64                             `json:"sourceProjectRouteIds"`
}

func storedFavoriteModpackExportPreview(snapshot favoriteModpackExportPreviewSnapshot) favoriteModpackExportStoredPreview {
	stored := favoriteModpackExportStoredPreview{Preview: snapshot,
		SourceProjectRouteIDs: make([]*int64, len(snapshot.Items))}
	for index := range snapshot.Items {
		stored.SourceProjectRouteIDs[index] = snapshot.Items[index].SourceProjectRouteID
	}
	return stored
}

func favoriteModpackExportPreviewHash(snapshot favoriteModpackExportPreviewSnapshot) (string, []byte, error) {
	raw, err := json.Marshal(storedFavoriteModpackExportPreview(snapshot))
	if err != nil {
		return "", nil, err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), raw, nil
}

func (s *Server) persistFavoriteModpackExportPreview(ctx context.Context, ownerID int64, preview favoriteModpackExportPreview) (favoriteModpackExportPreview, error) {
	if preview.ReportVersion == 0 {
		preview.ReportVersion = favoriteModpackExportReportVersion
	}
	if preview.RebuildSource == "" {
		preview.RebuildSource = favoriteModpackExportSourceCurrent
	}
	if !validCatalogPublicID(preview.CollectionPublicID) || preview.ReportVersion != favoriteModpackExportReportVersion ||
		(preview.RebuildSource != favoriteModpackExportSourceCurrent && preview.RebuildSource != favoriteModpackExportSourceOriginal) ||
		(preview.RebuildSource == favoriteModpackExportSourceCurrent && preview.CollectionID <= 0) {
		return preview, fmt.Errorf("%w: invalid rebuild snapshot metadata", errFavoriteModpackExportPreviewAuthority)
	}
	contentHash, raw, err := favoriteModpackExportPreviewHash(preview.favoriteModpackExportPreviewSnapshot)
	if err != nil {
		return preview, fmt.Errorf("%w: encode snapshot: %v", errFavoriteModpackExportPreviewAuthority, err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return preview, fmt.Errorf("%w: begin snapshot: %v", errFavoriteModpackExportPreviewAuthority, err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `delete from favorite_modpack_export_previews
		where owner_user_id=$1 and (expires_at<=now() or consumed_at<now()-interval '1 hour')`, ownerID); err != nil {
		return preview, fmt.Errorf("%w: expire old snapshots: %v", errFavoriteModpackExportPreviewAuthority, err)
	}
	err = tx.QueryRow(ctx, `insert into favorite_modpack_export_previews(owner_user_id,collection_id,collection_public_id_snapshot,
		source_mode,minecraft_version,loader_type,loader_version,preview_snapshot,content_hash,expires_at)
		values($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,now()+make_interval(secs=>$10)) returning public_id,expires_at`, ownerID,
		nullableFavoriteCollectionID(preview.CollectionID), preview.CollectionPublicID, preview.RebuildSource, preview.MinecraftVersion,
		preview.Loader, preview.LoaderVersion, string(raw), contentHash, int(favoriteModpackExportPreviewTTL/time.Second)).
		Scan(&preview.PreviewID, &preview.ExpiresAt)
	if err != nil {
		return preview, fmt.Errorf("%w: save snapshot: %v", errFavoriteModpackExportPreviewAuthority, err)
	}
	preview.PreviewHash = contentHash
	if err = tx.Commit(ctx); err != nil {
		return preview, fmt.Errorf("%w: commit snapshot: %v", errFavoriteModpackExportPreviewAuthority, err)
	}
	return preview, nil
}

func loadFavoriteModpackExportPreviewForCreate(ctx context.Context, tx pgx.Tx, ownerID int64, collectionPublicID string, request favoriteModpackExportRequest) (favoriteModpackExportPreview, int64, error) {
	var preview favoriteModpackExportPreview
	var previewRowID, collectionID int64
	var collectionPublicIDSnapshot, sourceMode, minecraftVersion, loader, loaderVersion, storedHash string
	var raw []byte
	var consumedAt *time.Time
	err := tx.QueryRow(ctx, `select id,coalesce(collection_id,0),collection_public_id_snapshot,source_mode,
		minecraft_version,loader_type,loader_version,preview_snapshot,
		content_hash,expires_at,consumed_at from favorite_modpack_export_previews
		where public_id=$1 and owner_user_id=$2 for update`, request.PreviewID, ownerID).Scan(&previewRowID, &collectionID,
		&collectionPublicIDSnapshot, &sourceMode, &minecraftVersion, &loader, &loaderVersion, &raw, &storedHash,
		&preview.ExpiresAt, &consumedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return preview, 0, errFavoriteModpackExportPreviewNotFound
	}
	if err != nil {
		return preview, 0, fmt.Errorf("%w: load snapshot: %v", errFavoriteModpackExportPreviewAuthority, err)
	}
	if consumedAt != nil {
		return preview, 0, errFavoriteModpackExportPreviewConsumed
	}
	if !preview.ExpiresAt.After(time.Now()) {
		return preview, 0, errFavoriteModpackExportPreviewExpired
	}
	var stored favoriteModpackExportStoredPreview
	if err = json.Unmarshal(raw, &stored); err != nil {
		return preview, 0, fmt.Errorf("%w: decode snapshot: %v", errFavoriteModpackExportPreviewAuthority, err)
	}
	if len(stored.SourceProjectRouteIDs) != len(stored.Preview.Items) {
		return preview, 0, fmt.Errorf("%w: invalid internal route snapshot", errFavoriteModpackExportPreviewAuthority)
	}
	preview.favoriteModpackExportPreviewSnapshot = stored.Preview
	for index := range preview.Items {
		preview.Items[index].SourceProjectRouteID = stored.SourceProjectRouteIDs[index]
	}
	preview.CollectionID = collectionID
	computedHash, _, err := favoriteModpackExportPreviewHash(preview.favoriteModpackExportPreviewSnapshot)
	if err != nil {
		return preview, 0, fmt.Errorf("%w: hash snapshot: %v", errFavoriteModpackExportPreviewAuthority, err)
	}
	preview.PreviewID, preview.PreviewHash = request.PreviewID, computedHash
	requestHash := strings.ToLower(strings.TrimSpace(request.PreviewHash))
	if subtle.ConstantTimeCompare([]byte(storedHash), []byte(computedHash)) != 1 ||
		subtle.ConstantTimeCompare([]byte(requestHash), []byte(computedHash)) != 1 ||
		collectionPublicIDSnapshot != preview.CollectionPublicID || collectionPublicID != preview.CollectionPublicID ||
		sourceMode != preview.RebuildSource || (sourceMode == favoriteModpackExportSourceCurrent && collectionID <= 0) ||
		minecraftVersion != preview.MinecraftVersion || loader != preview.Loader || loaderVersion != preview.LoaderVersion ||
		request.MinecraftVersion != preview.MinecraftVersion || request.Loader != preview.Loader {
		return preview, 0, errFavoriteModpackExportPreviewMismatch
	}
	return preview, previewRowID, nil
}

func markFavoriteModpackExportPreviewConsumed(ctx context.Context, tx pgx.Tx, previewRowID int64) error {
	tag, err := tx.Exec(ctx, `update favorite_modpack_export_previews set consumed_at=now()
		where id=$1 and consumed_at is null and expires_at>now()`, previewRowID)
	if err != nil {
		return fmt.Errorf("%w: consume snapshot: %v", errFavoriteModpackExportPreviewAuthority, err)
	}
	if tag.RowsAffected() != 1 {
		return errFavoriteModpackExportPreviewConsumed
	}
	return nil
}
