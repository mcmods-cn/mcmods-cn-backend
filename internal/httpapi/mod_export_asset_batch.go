package httpapi

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type exportTextAssetWrite struct {
	RevisionID  string
	AssetPath   string
	AssetKind   string
	ContentType string
	Digest      string
	ByteLength  int64
	Content     string
	JSON        bool
}

func persistExportTextAssets(ctx context.Context, tx pgx.Tx, rows []exportTextAssetWrite) error {
	if len(rows) == 0 {
		return nil
	}
	if _, err := execImportStatement(ctx, tx, `create temporary table if not exists catalog_import_text_asset_stage (
		ordinal bigint not null,
		revision_id text not null,
		asset_path text not null,
		asset_kind text not null,
		content_type text not null,
		sha256 text not null,
		byte_length bigint not null,
		text_content text,
		json_content text
	) on commit drop`); err != nil {
		return fmt.Errorf("create text asset import stage: %w", err)
	}
	if _, err := execImportStatement(ctx, tx, `truncate catalog_import_text_asset_stage`); err != nil {
		return fmt.Errorf("truncate text asset import stage: %w", err)
	}
	columns := []string{
		"ordinal", "revision_id", "asset_path", "asset_kind", "content_type",
		"sha256", "byte_length", "text_content", "json_content",
	}
	if err := copyImportRows(ctx, tx, "catalog_import_text_asset_stage", columns, len(rows), func(index int) ([]any, error) {
		row := rows[index]
		var textContent any
		var jsonContent any
		if row.JSON {
			jsonContent = row.Content
		} else {
			textContent = row.Content
		}
		return []any{
			index, row.RevisionID, row.AssetPath, row.AssetKind, row.ContentType,
			row.Digest, row.ByteLength, textContent, jsonContent,
		}, nil
	}); err != nil {
		return fmt.Errorf("stage text asset imports: %w", err)
	}
	if _, err := execImportStatement(ctx, tx, `insert into catalog_import_text_assets(
		revision_id,asset_path,asset_kind,content_type,sha256,byte_length,text_content,json_content)
		select revision_id,asset_path,asset_kind,content_type,sha256,byte_length,text_content,
			case when json_content is null then null else json_content::jsonb end
		from catalog_import_text_asset_stage
		order by ordinal
		on conflict(revision_id,asset_path) do nothing`); err != nil {
		return fmt.Errorf("merge text asset imports: %w", err)
	}
	return nil
}
