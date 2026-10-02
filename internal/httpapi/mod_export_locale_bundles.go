package httpapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
)

const (
	exportLocaleBundleKind        = "translation_bundle"
	exportLocaleBundleContentType = "application/gzip"
	maxExportLocaleBundleSize     = int64(16 << 20)
)

type modExportLocaleBundleMedia struct {
	RevisionID       string
	Locale           string
	TranslationCount int
	AssetPath        string
	ObjectKey        string
	Original         string
	Digest           string
	ContentType      string
	ByteLength       int64
	FileID           int64
}

func prepareExportLocaleBundles(
	cfg ossConfigPayload,
	projectPublicID string,
	revisions map[string]string,
	translation preparedExportTranslation,
) ([]modExportLocaleBundleMedia, []byte, error) {
	var compressed bytes.Buffer
	writer, err := gzip.NewWriterLevel(&compressed, gzip.BestCompression)
	if err != nil {
		return nil, nil, err
	}
	if err = json.NewEncoder(writer).Encode(translation.Values); err != nil {
		_ = writer.Close()
		return nil, nil, fmt.Errorf("encode locale bundle %s: %w", translation.Locale, err)
	}
	if err = writer.Close(); err != nil {
		return nil, nil, fmt.Errorf("compress locale bundle %s: %w", translation.Locale, err)
	}
	payload := compressed.Bytes()
	assetPath := exportLocaleBundleAssetPath(translation.Locale)
	original := strings.ToLower(translation.Locale) + ".json.gz"
	revisionIDs := make([]string, 0, len(revisions))
	seenRevisionIDs := make(map[string]struct{}, len(revisions))
	for _, revisionID := range revisions {
		if _, exists := seenRevisionIDs[revisionID]; exists {
			continue
		}
		seenRevisionIDs[revisionID] = struct{}{}
		revisionIDs = append(revisionIDs, revisionID)
	}
	sort.Strings(revisionIDs)
	result := make([]modExportLocaleBundleMedia, 0, len(revisionIDs))
	for _, revisionID := range revisionIDs {
		category := ossProjectTextCategory("mod", projectPublicID, revisionID, "translations")
		result = append(result, modExportLocaleBundleMedia{
			RevisionID: revisionID, Locale: translation.Locale, TranslationCount: len(translation.Values),
			AssetPath: assetPath, ObjectKey: path.Join(ossObjectPrefix(cfg.Prefix, category), original),
			Original: original, Digest: sha256Hex(payload), ContentType: exportLocaleBundleContentType,
			ByteLength: int64(len(payload)),
		})
	}
	return result, payload, nil
}

func exportLocaleBundleAssetPath(locale string) string {
	if canonical, valid := canonicalExportLocaleTag(locale); valid {
		locale = canonical
	}
	return path.Join("_locales", locale+".json.gz")
}

func persistExportLocaleBundles(
	ctx context.Context,
	tx pgx.Tx,
	bundles []modExportLocaleBundleMedia,
) error {
	if len(bundles) == 0 {
		return nil
	}
	deduplicated := make([]modExportLocaleBundleMedia, 0, len(bundles))
	seen := make(map[string]string, len(bundles))
	for _, item := range bundles {
		key := item.RevisionID + "\x00" + item.Locale
		if digest, exists := seen[key]; exists {
			if digest != item.Digest {
				return fmt.Errorf("conflicting locale bundles for revision %s locale %s", item.RevisionID, item.Locale)
			}
			continue
		}
		seen[key] = item.Digest
		deduplicated = append(deduplicated, item)
	}
	bundles = deduplicated
	sort.Slice(bundles, func(left, right int) bool {
		if bundles[left].RevisionID == bundles[right].RevisionID {
			return bundles[left].Locale < bundles[right].Locale
		}
		return bundles[left].RevisionID < bundles[right].RevisionID
	})
	for _, item := range bundles {
		if item.FileID <= 0 {
			return fmt.Errorf("locale artifact is not registered: %s", item.ObjectKey)
		}
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"catalog_import_locales"},
		[]string{"revision_id", "locale", "translation_count"},
		pgx.CopyFromSlice(len(bundles), func(index int) ([]any, error) {
			item := bundles[index]
			return []any{item.RevisionID, item.Locale, item.TranslationCount}, nil
		})); err != nil {
		return fmt.Errorf("persist locale bundle metadata: %w", err)
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"catalog_import_media"},
		[]string{"revision_id", "asset_path", "media_kind", "oss_file_id", "sha256", "content_type", "byte_length", "width", "height", "has_alpha"},
		pgx.CopyFromSlice(len(bundles), func(index int) ([]any, error) {
			item := bundles[index]
			return []any{item.RevisionID, item.AssetPath, exportLocaleBundleKind, item.FileID, item.Digest,
				item.ContentType, item.ByteLength, nil, nil, nil}, nil
		})); err != nil {
		return fmt.Errorf("persist locale bundle objects: %w", err)
	}
	return nil
}

func (s *Server) loadExportLocaleBundle(ctx context.Context, revisionID, locale string) (map[string]string, error) {
	var valid bool
	locale, valid = canonicalExportLocaleTag(locale)
	if !valid {
		return map[string]string{}, nil
	}
	cacheKey := "export-locale-bundle:v1:" + revisionID + ":" + locale
	raw, err := s.cache.GetOrLoad(ctx, cacheKey, func(loadContext context.Context) ([]byte, error) {
		var bucket, objectKey string
		queryErr := s.db.QueryRow(loadContext, `select file.bucket,file.object_key
			from catalog_import_locales imported_locale
			join catalog_import_media media on media.revision_id=imported_locale.revision_id
			 and media.asset_path=('_locales/'||imported_locale.locale||'.json.gz')
			 and media.media_kind=$3
			join oss_files file on file.id=media.oss_file_id
			where imported_locale.revision_id=$1
			  and (imported_locale.locale=$2 or split_part(imported_locale.locale,'-',1)=split_part($2,'-',1))
			order by (imported_locale.locale=$2) desc,imported_locale.locale limit 1`,
			revisionID, locale, exportLocaleBundleKind).Scan(&bucket, &objectKey)
		if errors.Is(queryErr, pgx.ErrNoRows) {
			return []byte("{}"), nil
		}
		if queryErr != nil {
			return nil, queryErr
		}
		client, _, clientErr := s.ossClient(loadContext)
		if clientErr != nil {
			return nil, clientErr
		}
		result, getErr := client.GetObject(loadContext, &aliyunoss.GetObjectRequest{
			Bucket: aliyunoss.Ptr(bucket), Key: aliyunoss.Ptr(objectKey),
		})
		if getErr != nil {
			return nil, getErr
		}
		defer result.Body.Close()
		reader, gzipErr := gzip.NewReader(result.Body)
		if gzipErr != nil {
			return nil, gzipErr
		}
		defer reader.Close()
		value, readErr := io.ReadAll(io.LimitReader(reader, maxExportLocaleBundleSize+1))
		if readErr != nil {
			return nil, readErr
		}
		if int64(len(value)) > maxExportLocaleBundleSize || !json.Valid(value) {
			return nil, fmt.Errorf("invalid locale bundle %s/%s", revisionID, locale)
		}
		return value, nil
	})
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	if err = json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	return values, nil
}

func (s *Server) decorateExportTranslationNames(ctx context.Context, revisionID, locale string, items []map[string]any) error {
	canonical, valid := canonicalExportLocaleTag(locale)
	if !valid || len(items) == 0 {
		return nil
	}
	locale = canonical
	values, err := s.loadExportLocaleBundle(ctx, revisionID, locale)
	if err != nil {
		return err
	}
	for _, item := range items {
		translationKey, _ := item["translationKey"].(string)
		name := strings.TrimSpace(values[translationKey])
		if name == "" {
			continue
		}
		names, _ := item["names"].(map[string]any)
		if names == nil {
			switch current := item["names"].(type) {
			case map[string]string:
				names = make(map[string]any, len(current)+1)
				for key, value := range current {
					names[key] = value
				}
			default:
				names = map[string]any{}
			}
		}
		names[locale] = name
		item["names"] = names
	}
	return nil
}
