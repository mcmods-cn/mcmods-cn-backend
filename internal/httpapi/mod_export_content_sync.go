package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/activity"
)

// syncImportedResourcesToContentVersionTx materializes importer-owned fields in
// the selected, neutral content version. Human summaries and Markdown bodies
// are intentionally stored in different columns and are never replaced here.
func syncImportedResourcesToContentVersionTx(ctx context.Context, tx pgx.Tx, revisionIDs []string, versionPublicID string, overwrite bool, actorID int64) error {
	if len(revisionIDs) == 0 || versionPublicID == "" {
		return nil
	}
	var versionID, modID int64
	if err := tx.QueryRow(ctx, `select id,mod_id from mod_content_versions where public_id=$1 and status='active'`, versionPublicID).Scan(&versionID, &modID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `insert into mod_resource_bindings(resource_id,mod_id)
		select distinct snapshot.resource_id,$2::bigint from resource_import_snapshots snapshot
		where snapshot.revision_id=any($1::text[]) on conflict(resource_id) do nothing`, revisionIDs, modID); err != nil {
		return fmt.Errorf("bind imported resources to mod: %w", err)
	}
	if _, err := tx.Exec(ctx, `insert into mod_resource_version_details(
		resource_id,version_id,default_locale,definition,icon_file_id,render_file_id,status,created_by,updated_by)
		select distinct on (snapshot.resource_id) snapshot.resource_id,$2::bigint,'en',snapshot.data,icon.oss_file_id,preview.oss_file_id,'active',nullif($4::bigint,0),nullif($4::bigint,0)
		from resource_import_snapshots snapshot
		left join catalog_import_media icon on icon.revision_id=snapshot.revision_id and icon.asset_path=snapshot.icon_path
		left join catalog_import_media preview on preview.revision_id=snapshot.revision_id and preview.asset_path=snapshot.preview_path
		where snapshot.revision_id=any($1::text[])
		order by snapshot.resource_id,(snapshot.icon_path<>'') desc,(snapshot.preview_path<>'') desc
		on conflict(resource_id,version_id) do update set
			definition=case when $3 then mod_resource_version_details.definition||excluded.definition else mod_resource_version_details.definition end,
			icon_file_id=case when $3 and excluded.icon_file_id is not null then excluded.icon_file_id else mod_resource_version_details.icon_file_id end,
			render_file_id=case when $3 and excluded.render_file_id is not null then excluded.render_file_id else mod_resource_version_details.render_file_id end,
			updated_by=case when $3 then excluded.updated_by else mod_resource_version_details.updated_by end,
			updated_at=case when $3 then now() else mod_resource_version_details.updated_at end`, revisionIDs, versionID, overwrite, actorID); err != nil {
		return fmt.Errorf("sync imported resource version details: %w", err)
	}
	if _, err := tx.Exec(ctx, `insert into mod_resource_version_detail_localizations(
		resource_id,version_id,locale,name,summary,content_markdown,provenance)
		select distinct on (snapshot.resource_id,replace(lower(name.locale),'_','-'))
			snapshot.resource_id,$2::bigint,replace(lower(name.locale),'_','-'),name.value,'','','import'
		from resource_import_snapshots snapshot cross join lateral jsonb_each_text(snapshot.names) name(locale,value)
		where snapshot.revision_id=any($1::text[]) and name.value<>''
		order by snapshot.resource_id,replace(lower(name.locale),'_','-'),name.locale
		on conflict(resource_id,version_id,locale) do update set
			name=case when $3 then excluded.name else mod_resource_version_detail_localizations.name end`, revisionIDs, versionID, overwrite); err != nil {
		return fmt.Errorf("sync imported resource localizations: %w", err)
	}
	if err := ensureImportedContentSectionsTx(ctx, tx, revisionIDs, versionID, modID, actorID); err != nil {
		return fmt.Errorf("sync imported content sections: %w", err)
	}
	return nil
}

func ensureImportedContentSectionsTx(ctx context.Context, tx pgx.Tx, revisionIDs []string, versionID, modID, actorID int64) error {
	if _, err := tx.Exec(ctx, `with imported_templates as (
		select distinct case
			when resource.kind_code in ('minecraft.item','minecraft.block') then 'item_block'
			when resource.kind_code='minecraft.fluid' then 'fluid'
			when resource.kind_code='minecraft.dimension' then 'dimension'
			when resource.kind_code='minecraft.biome' then 'biome'
			when resource.kind_code='minecraft.entity_type' then 'entity'
			when resource.kind_code='minecraft.enchantment' then 'enchantment'
			when resource.kind_code in ('minecraft.mob_effect','minecraft.potion') then 'mob_effect'
			when resource.kind_code='minecraft.natural_generation' then 'natural_generation'
			when resource.kind_code='minecraft.structure' then 'world_structure'
			when resource.kind_code='minecraft.key_mapping' then 'key_mapping'
			when resource.kind_code='minecraft.advancement' then 'advancement'
			when resource.kind_code like 'mekanism.%' then 'chemical'
		end template_code
		from resource_import_snapshots snapshot join game_resources resource on resource.entity_id=snapshot.resource_id
		where snapshot.revision_id=any($1::text[])
	), missing as (
		select template.id,template.default_locale,template.default_display_mode,
			row_number() over(order by template.id)-1 ordinal
		from imported_templates imported join mod_content_templates template on template.code=imported.template_code and template.builtin
		where imported.template_code is not null and not exists(select 1 from mod_content_sections section
			where section.version_id=$2 and section.template_id=template.id and section.parent_id is null and section.status='active')
	), offset_value as (
		select coalesce(max(ordinal)+1,0) value from mod_content_sections where version_id=$2 and parent_id is null
	)
	insert into mod_content_sections(mod_id,version_id,template_id,parent_id,default_locale,display_mode,ordinal,status,created_by,updated_by)
		select $3::bigint,$2::bigint,missing.id,null,missing.default_locale,missing.default_display_mode,offset_value.value+missing.ordinal,'active',nullif($4::bigint,0),nullif($4::bigint,0)
		from missing cross join offset_value`, revisionIDs, versionID, modID, actorID); err != nil {
		return fmt.Errorf("create imported content sections: %w", err)
	}
	_, err := tx.Exec(ctx, `with imported as (
		select distinct snapshot.resource_id,resource.canonical_id,case
			when resource.kind_code in ('minecraft.item','minecraft.block') then 'item_block'
			when resource.kind_code='minecraft.fluid' then 'fluid'
			when resource.kind_code='minecraft.dimension' then 'dimension'
			when resource.kind_code='minecraft.biome' then 'biome'
			when resource.kind_code='minecraft.entity_type' then 'entity'
			when resource.kind_code='minecraft.enchantment' then 'enchantment'
			when resource.kind_code in ('minecraft.mob_effect','minecraft.potion') then 'mob_effect'
			when resource.kind_code='minecraft.natural_generation' then 'natural_generation'
			when resource.kind_code='minecraft.structure' then 'world_structure'
			when resource.kind_code='minecraft.key_mapping' then 'key_mapping'
			when resource.kind_code='minecraft.advancement' then 'advancement'
			when resource.kind_code like 'mekanism.%' then 'chemical'
		end template_code
		from resource_import_snapshots snapshot join game_resources resource on resource.entity_id=snapshot.resource_id
		where snapshot.revision_id=any($1::text[])
	), target as (
		select imported.resource_id,imported.canonical_id,section.id section_id,
			row_number() over(partition by section.id order by imported.canonical_id)-1 ordinal
		from imported join mod_content_templates template on template.code=imported.template_code and template.builtin
		join lateral (select id from mod_content_sections where version_id=$2 and template_id=template.id and parent_id is null and status='active' order by ordinal,id limit 1) section on true
		where imported.template_code is not null
	), offsets as (
		select section_id,coalesce(max(ordinal)+1,0) value from mod_content_section_resources where version_id=$2 group by section_id
	)
	insert into mod_content_section_resources(section_id,version_id,resource_id,ordinal)
	select target.section_id,$2::bigint,target.resource_id,coalesce(offsets.value,0)+target.ordinal from target left join offsets on offsets.section_id=target.section_id
	where not exists(select 1 from mod_content_section_resources existing where existing.section_id=target.section_id and existing.version_id=$2 and existing.resource_id=target.resource_id)
	on conflict do nothing`, revisionIDs, versionID)
	if err != nil {
		return fmt.Errorf("attach imported resources to content sections: %w", err)
	}
	return nil
}

func insertModExportImportActivityTx(ctx context.Context, tx pgx.Tx, actorID int64, jobID, versionPublicID string, overwrite bool) error {
	metadata, err := json.Marshal(map[string]any{"jobId": jobID, "targetVersionPublicId": versionPublicID, "overwriteExistingImportData": overwrite, "source": "mcmods_exporter"})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `insert into user_activity_events(user_id,action_id,object_type_id,object_public_id,metadata,occurred_at)
		values(nullif($1,0),$2,$3,$4,$5::jsonb,$6)`, actorID, activity.ActionUpload, activity.ObjectMod, versionPublicID, string(metadata), time.Now().UTC())
	return err
}
