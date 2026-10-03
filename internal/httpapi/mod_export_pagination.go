package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const maxModExportCursorLength = 1024

type modExportPageCursor struct {
	RevisionID string `json:"r"`
	Scope      string `json:"s"`
	First      string `json:"f"`
	Second     string `json:"n,omitempty"`
	Ordinal    int    `json:"o,omitempty"`
}

func encodeModExportPageCursor(cursor modExportPageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeModExportPageCursor(raw, revisionID, scope string) (modExportPageCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return modExportPageCursor{RevisionID: revisionID, Scope: scope}, nil
	}
	if len(raw) > maxModExportCursorLength {
		return modExportPageCursor{}, errors.New("export cursor is too large")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return modExportPageCursor{}, errors.New("invalid export cursor encoding")
	}
	var cursor modExportPageCursor
	if err = json.Unmarshal(decoded, &cursor); err != nil || cursor.RevisionID != revisionID || cursor.Scope != scope || cursor.First == "" || cursor.Ordinal < 0 {
		return modExportPageCursor{}, errors.New("invalid export cursor")
	}
	return cursor, nil
}

func decodeModExportJSON(raw []byte, target any, label string) error {
	if len(raw) == 0 {
		return fmt.Errorf("%s is empty", label)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode %s: %w", label, err)
	}
	return nil
}

func decodeModExportJSONObjectValue(raw []byte, label string) (map[string]any, error) {
	return decodeStoredJSONObject(raw, label)
}

type modExportTagListRow struct {
	PublicID    string
	Registry    string
	ID          string
	MemberCount int
}

func loadModExportTagPage(
	ctx context.Context,
	db catalogResourceResolverQuery,
	revisionID, registry, query, afterRegistry, afterID string,
	limit int,
) ([]modExportTagListRow, bool, error) {
	searchPrefix := ""
	if query != "" {
		searchPrefix = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query) + "%"
	}
	rows, err := db.Query(ctx, `select entity.public_id,tag.registry,tag.canonical_id,snapshot.member_count
		from catalog_tags tag
		join tag_import_snapshots snapshot on snapshot.tag_id=tag.entity_id and snapshot.revision_id=$1
		join catalog_entities entity on entity.id=tag.entity_id
		where ($2='' or tag.registry=$2)
		  and ($3='' or lower(tag.canonical_id) like lower($3) escape '\')
		  and ($4='' or (tag.registry,tag.canonical_id)>($4,$5))
		order by tag.registry,tag.canonical_id limit $6`, revisionID, registry, searchPrefix, afterRegistry, afterID, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := make([]modExportTagListRow, 0, limit+1)
	for rows.Next() {
		var item modExportTagListRow
		if err = rows.Scan(&item.PublicID, &item.Registry, &item.ID, &item.MemberCount); err != nil {
			return nil, false, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}

type modExportTagMemberRow struct {
	ID             string
	PublicID       string
	Registry       string
	TranslationKey string
	Names          map[string]string
	IconPath       string
}

func loadModExportTagMemberPage(
	ctx context.Context,
	db catalogResourceResolverQuery,
	revisionID string,
	tagInternalID int64,
	after string,
	limit int,
) ([]modExportTagMemberRow, bool, error) {
	rows, err := db.Query(ctx, `select member.raw_member_id,coalesce(entity.public_id,''),
		coalesce(snapshot.registry,''),coalesce(snapshot.translation_key,''),
		coalesce(snapshot.names,'{}'::jsonb),coalesce(snapshot.icon_path,'')
		from tag_import_members member
		join tag_import_snapshots tag_snapshot on tag_snapshot.id=member.tag_snapshot_id
		left join game_resources resource on resource.entity_id=member.resource_id
		left join catalog_entities entity on entity.id=resource.entity_id
		left join lateral (select candidate.* from resource_import_snapshots candidate
			where candidate.resource_id=resource.entity_id
			order by (candidate.revision_id=$1) desc,candidate.created_at desc limit 1) snapshot on true
		where tag_snapshot.revision_id=$1 and tag_snapshot.tag_id=$2 and ($3='' or member.raw_member_id>$3)
		order by member.raw_member_id limit $4`, revisionID, tagInternalID, after, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := make([]modExportTagMemberRow, 0, limit+1)
	for rows.Next() {
		var item modExportTagMemberRow
		var names []byte
		if err = rows.Scan(&item.ID, &item.PublicID, &item.Registry, &item.TranslationKey, &names, &item.IconPath); err != nil {
			return nil, false, err
		}
		if err = decodeModExportJSON(names, &item.Names, "tag member names"); err != nil {
			return nil, false, fmt.Errorf("tag member %q: %w", item.ID, err)
		}
		if item.Names == nil {
			return nil, false, fmt.Errorf("tag member %q names must be a JSON object", item.ID)
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}

type modExportAssetPageRow struct {
	Path        string
	Kind        string
	ContentType string
	SHA256      string
	ByteLength  int64
	Media       bool
	SourceOrder int
}

func loadModExportAssetPage(
	ctx context.Context,
	db catalogResourceResolverQuery,
	revisionID string,
	pathsOnly bool,
	afterPath string,
	afterSource, limit int,
) ([]modExportAssetPageRow, bool, error) {
	var rowsQuery string
	var arguments []any
	if pathsOnly {
		rowsQuery = `with paths as (
			select asset_path from catalog_import_text_assets where revision_id=$1
			union select asset_path from catalog_import_binary_assets where revision_id=$1
			union select asset_path from catalog_import_media where revision_id=$1
		) select asset_path,'','','',0::bigint,false,0 from paths
		where ($2='' or asset_path>$2) order by asset_path limit $3`
		arguments = []any{revisionID, afterPath, limit + 1}
	} else {
		rowsQuery = `with assets as (
			select asset_path,asset_kind kind,content_type,sha256,byte_length,false media,0 source_order
			from catalog_import_text_assets where revision_id=$1
			union all select asset_path,asset_kind,content_type,sha256,byte_length,false,1
			from catalog_import_binary_assets where revision_id=$1
			union all select asset_path,media_kind,content_type,sha256,byte_length,true,2
			from catalog_import_media where revision_id=$1
		) select asset_path,kind,content_type,sha256,byte_length,media,source_order from assets
		where ($2='' or (asset_path,source_order)>($2,$3)) order by asset_path,source_order limit $4`
		arguments = []any{revisionID, afterPath, afterSource, limit + 1}
	}
	rows, err := db.Query(ctx, rowsQuery, arguments...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := make([]modExportAssetPageRow, 0, limit+1)
	for rows.Next() {
		var item modExportAssetPageRow
		if err = rows.Scan(&item.Path, &item.Kind, &item.ContentType, &item.SHA256, &item.ByteLength, &item.Media, &item.SourceOrder); err != nil {
			return nil, false, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	return items, hasMore, nil
}
