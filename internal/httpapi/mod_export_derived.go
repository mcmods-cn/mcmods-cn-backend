package httpapi

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// refreshModExportRevisionStats materializes values used by the version and
// catalog pages. It runs once after an import instead of rebuilding the same
// aggregates on every request.
func refreshModExportRevisionStats(ctx context.Context, tx pgx.Tx, revisionIDs []string) error {
	if len(revisionIDs) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `insert into mod_export_revision_stats(
			revision_id,registry_counts,document_counts,asset_count,structure_count,
			advancement_count,key_mapping_count,recipe_count,tag_count,capability_statuses,updated_at
		)
		select revision.id,
			coalesce((select jsonb_object_agg(registry,total) from (
				select registry,count(*)::int total from mod_export_registry_entries
				where revision_id=revision.id group by registry
			) counts),'{}'::jsonb),
			coalesce((select jsonb_object_agg(kind,total) from (
				select kind,count(*)::int total from mod_export_document_entries
				where revision_id=revision.id group by kind
			) counts),'{}'::jsonb),
			(select count(*)::int from (
				select asset_path from mod_export_text_assets where revision_id=revision.id
				union all select asset_path from mod_export_binary_assets where revision_id=revision.id
				union all select asset_path from mod_export_media where revision_id=revision.id
			) assets),
			(select count(*)::int from mod_export_structures where revision_id=revision.id),
			coalesce((select jsonb_array_length(json_content->'advancements') from mod_export_text_assets where revision_id=revision.id and asset_path='advancements/advancements.json'),0),
			(select count(*)::int from mod_export_registry_entries where revision_id=revision.id and registry='key_mappings'),
			(select count(*)::int from mod_export_recipe_layouts where revision_id=revision.id),
			(select count(*)::int from mod_export_tags where revision_id=revision.id),
			coalesce((select jsonb_object_agg(capability_id,jsonb_build_object(
				'status',status,'source',source
			)) from mod_export_capabilities where revision_id=revision.id),'{}'::jsonb),now()
		from mod_export_revisions revision where revision.id=any($1::text[])
		on conflict(revision_id) do update set
			registry_counts=excluded.registry_counts,document_counts=excluded.document_counts,
			asset_count=excluded.asset_count,structure_count=excluded.structure_count,
			advancement_count=excluded.advancement_count,key_mapping_count=excluded.key_mapping_count,
			recipe_count=excluded.recipe_count,tag_count=excluded.tag_count,
			capability_statuses=excluded.capability_statuses,updated_at=excluded.updated_at`, revisionIDs)
	return err
}
