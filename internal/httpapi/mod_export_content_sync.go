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
func syncImportedResourcesToContentVersionTx(ctx context.Context, tx pgx.Tx, revisionIDs []string, versionID int64, overwrite bool, actorID int64) error {
	if len(revisionIDs) == 0 || versionID <= 0 {
		return nil
	}
	var modID int64
	if err := tx.QueryRow(ctx, `select mod_id from mod_content_versions where id=$1 and status='active'`, versionID).Scan(&modID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `insert into mod_resource_bindings(resource_id,mod_id)
		select distinct snapshot.resource_id,$2::bigint from resource_import_snapshots snapshot
		where snapshot.revision_id=any($1::text[]) on conflict(resource_id) do nothing`, revisionIDs, modID); err != nil {
		return fmt.Errorf("bind imported resources to mod: %w", err)
	}
	if _, err := tx.Exec(ctx, `insert into mod_resource_version_details(
		resource_id,version_id,default_locale,definition,icon_file_id,render_file_id,status,created_by,updated_by)
		select distinct on (snapshot.resource_id) snapshot.resource_id,$2::bigint,'en-US',snapshot.data,icon.oss_file_id,preview.oss_file_id,'active',nullif($4::bigint,0),nullif($4::bigint,0)
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

	// Item/block is one documentation page with two real, arrangeable child
	// classifications. These are data-model categories, not presentation-only
	// headings, so a later manual layout starts with the imported classification.
	if _, err := tx.Exec(ctx, `with root as (
		select section.id,section.mod_id,section.version_id,section.template_id,section.default_locale,section.display_mode
		from mod_content_sections section
		join mod_content_templates template on template.id=section.template_id
		where section.version_id=$1 and section.parent_id is null and section.status='active'
		  and template.code='item_block' and template.builtin
		order by section.ordinal,section.id limit 1
	), requested(system_key) as (values ('blocks'::text),('items'::text)),
	missing as (
		select requested.system_key,row_number() over(order by case requested.system_key when 'blocks' then 0 else 1 end)-1 ordinal
		from root cross join requested
		where not exists(select 1 from mod_content_sections child
			where child.version_id=root.version_id and child.parent_id=root.id
			  and child.system_key=requested.system_key and child.status='active')
	), offset_value as (
		select coalesce(max(child.ordinal)+1,0) value
		from root left join mod_content_sections child on child.parent_id=root.id and child.status='active'
	)
	insert into mod_content_sections(mod_id,version_id,template_id,parent_id,system_key,default_locale,display_mode,ordinal,status,created_by,updated_by)
	select root.mod_id,root.version_id,root.template_id,root.id,missing.system_key,root.default_locale,root.display_mode,
		offset_value.value+missing.ordinal,'active',nullif($2::bigint,0),nullif($2::bigint,0)
	from root cross join missing cross join offset_value`, versionID, actorID); err != nil {
		return fmt.Errorf("create imported item and block categories: %w", err)
	}
	if _, err := tx.Exec(ctx, `insert into mod_content_section_localizations(section_id,locale,name,description)
		select section.id,localization.locale,localization.name,''
		from mod_content_sections section
		cross join (values
			('blocks'::text,'en-US'::text,'Blocks'::text),('blocks','zh-CN','方块'),('blocks','zh-TW','方塊'),
			('items','en-US','Items'),('items','zh-CN','物品'),('items','zh-TW','物品')
		) localization(system_key,locale,name)
		where section.version_id=$1 and section.status='active'
		  and section.system_key=localization.system_key
		on conflict(section_id,locale) do nothing`, versionID); err != nil {
		return fmt.Errorf("localize imported item and block categories: %w", err)
	}

	// Reimport owns only its previous automatic placements. Human layout is
	// preserved, while stale imported placements (including block-item aliases)
	// are removed before the canonical set is materialized again.
	if _, err := tx.Exec(ctx, `delete from mod_content_section_resources placement
		using resource_import_snapshots snapshot
		where placement.version_id=$2 and placement.placement_source='import'
		  and placement.resource_id=snapshot.resource_id
		  and snapshot.revision_id=any($1::text[])`, revisionIDs, versionID); err != nil {
		return fmt.Errorf("clear previous imported resource placements: %w", err)
	}
	if _, err := tx.Exec(ctx, `with bound_item_ids as materialized (
			select distinct binding.item_resource_id
			from game_resource_asset_bindings binding
			join resource_import_snapshots block_snapshot on block_snapshot.id=binding.snapshot_id
			where block_snapshot.revision_id=any($1::text[]) and binding.item_resource_id is not null
		), imported_block_ids as materialized (
			select distinct block_resource.canonical_id
			from resource_import_snapshots block_snapshot
			join game_resources block_resource on block_resource.entity_id=block_snapshot.resource_id
			where block_snapshot.revision_id=any($1::text[])
			  and block_resource.kind_code='minecraft.block'
		)
		delete from mod_content_section_resources placement using game_resources item_resource
		where placement.version_id=$2 and placement.placement_source='import'
		  and item_resource.entity_id=placement.resource_id
		  and item_resource.kind_code='minecraft.item'
		  and (
			placement.resource_id in (select item_resource_id from bound_item_ids)
			or item_resource.canonical_id in (select canonical_id from imported_block_ids)
		  )`, revisionIDs, versionID); err != nil {
		return fmt.Errorf("clear imported block-item alias placements: %w", err)
	}

	_, err := tx.Exec(ctx, `with imported_snapshots as materialized (
		select snapshot.resource_id,resource.canonical_id,resource.kind_code
		from resource_import_snapshots snapshot
		join game_resources resource on resource.entity_id=snapshot.resource_id
		where snapshot.revision_id=any($1::text[])
	), bound_item_ids as materialized (
		select distinct binding.item_resource_id
		from game_resource_asset_bindings binding
		join resource_import_snapshots block_snapshot on block_snapshot.id=binding.snapshot_id
		where block_snapshot.revision_id=any($1::text[]) and binding.item_resource_id is not null
	), imported_block_ids as materialized (
		select distinct canonical_id from imported_snapshots where kind_code='minecraft.block'
	), imported as (
		select distinct snapshot.resource_id,snapshot.canonical_id,snapshot.kind_code,case
			when snapshot.kind_code in ('minecraft.item','minecraft.block') then 'item_block'
			when snapshot.kind_code='minecraft.fluid' then 'fluid'
			when snapshot.kind_code='minecraft.dimension' then 'dimension'
			when snapshot.kind_code='minecraft.biome' then 'biome'
			when snapshot.kind_code='minecraft.entity_type' then 'entity'
			when snapshot.kind_code='minecraft.enchantment' then 'enchantment'
			when snapshot.kind_code in ('minecraft.mob_effect','minecraft.potion') then 'mob_effect'
			when snapshot.kind_code='minecraft.natural_generation' then 'natural_generation'
			when snapshot.kind_code='minecraft.structure' then 'world_structure'
			when snapshot.kind_code='minecraft.key_mapping' then 'key_mapping'
			when snapshot.kind_code='minecraft.advancement' then 'advancement'
			when snapshot.kind_code like 'mekanism.%' then 'chemical'
		end template_code
		from imported_snapshots snapshot
		where snapshot.kind_code<>'minecraft.item'
		   or (
			snapshot.resource_id not in (select item_resource_id from bound_item_ids)
			and snapshot.canonical_id not in (select canonical_id from imported_block_ids)
		   )
	), target as (
		select imported.resource_id,imported.canonical_id,coalesce(child.id,section.id) section_id
		from imported join mod_content_templates template on template.code=imported.template_code and template.builtin
		join lateral (select id from mod_content_sections where version_id=$2 and template_id=template.id and parent_id is null and status='active' order by ordinal,id limit 1) section on true
		left join mod_content_sections child on child.parent_id=section.id and child.version_id=$2 and child.status='active'
		  and child.system_key=case imported.kind_code when 'minecraft.block' then 'blocks' when 'minecraft.item' then 'items' else '' end
		where imported.template_code is not null
		  and (imported.template_code<>'item_block' or child.id is not null)
		  and not exists(select 1 from mod_content_section_resources existing
			where existing.version_id=$2 and existing.resource_id=imported.resource_id)
	), numbered as (
		select target.*,row_number() over(partition by target.section_id order by target.canonical_id,target.resource_id)-1 ordinal
		from target
	), offsets as (
		select section_id,coalesce(max(ordinal)+1,0) value from mod_content_section_resources where version_id=$2 group by section_id
	)
	insert into mod_content_section_resources(section_id,version_id,resource_id,ordinal,placement_source)
	select numbered.section_id,$2::bigint,numbered.resource_id,coalesce(offsets.value,0)+numbered.ordinal,'import'
	from numbered left join offsets on offsets.section_id=numbered.section_id
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
