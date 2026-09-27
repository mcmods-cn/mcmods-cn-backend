package httpapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/activity"
)

// syncImportedResourcesToContentVersionTx materializes importer-owned fields in
// the selected, neutral content version. Human summaries and Markdown bodies
// are intentionally stored in different columns and are never replaced here.
func syncImportedResourcesToContentVersionTx(ctx context.Context, tx pgx.Tx, revisionIDs []string, versionID int64, _ bool, actorID int64) error {
	if len(revisionIDs) == 0 || versionID <= 0 {
		return nil
	}
	modID, err := lockImportedContentVersionTx(ctx, tx, versionID)
	if err != nil {
		return err
	}
	if err = archiveStaleImportedResourceDetailsTx(ctx, tx, revisionIDs, versionID, actorID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `insert into mod_resource_bindings(resource_id,mod_id)
		select distinct snapshot.resource_id,$2::bigint from resource_import_snapshots snapshot
		where snapshot.revision_id=any($1::text[]) on conflict(resource_id) do nothing`, revisionIDs, modID); err != nil {
		return fmt.Errorf("bind imported resources to mod: %w", err)
	}
	if _, err := tx.Exec(ctx, `insert into mod_resource_version_details(
		resource_id,version_id,entry_type_code,definition_schema_version,default_locale,definition,icon_file_id,render_file_id,
		projection_source,import_source_namespace,import_source_kind,import_revision_id,status,created_by,updated_by)
		select distinct on (snapshot.resource_id) snapshot.resource_id,$2::bigint,
			snapshot.entry_type_code,snapshot.definition_schema_version,
			'en-US',snapshot.data,icon.oss_file_id,preview.oss_file_id,
			'import',revision.source_namespace,revision.source_kind,revision.id,'active',nullif($3::bigint,0),nullif($3::bigint,0)
		from resource_import_snapshots snapshot
		join catalog_import_revisions revision on revision.id=snapshot.revision_id
		join game_resources resource on resource.entity_id=snapshot.resource_id
		left join catalog_import_media icon on icon.revision_id=snapshot.revision_id and icon.asset_path=snapshot.icon_path
		left join catalog_import_media preview on preview.revision_id=snapshot.revision_id and preview.asset_path=snapshot.preview_path
		where snapshot.revision_id=any($1::text[])
		order by snapshot.resource_id,revision.revision_no desc,revision.id,(snapshot.icon_path<>'') desc,(snapshot.preview_path<>'') desc
		on conflict(resource_id,version_id) do update set
			entry_type_code=excluded.entry_type_code,
			definition_schema_version=excluded.definition_schema_version,
			default_locale=excluded.default_locale,
			definition=excluded.definition,
			icon_file_id=excluded.icon_file_id,
			render_file_id=excluded.render_file_id,
			projection_source='import',
			import_source_namespace=excluded.import_source_namespace,
			import_source_kind=excluded.import_source_kind,
			import_revision_id=excluded.import_revision_id,
			status='active',published_revision_id=null,
			updated_by=excluded.updated_by,updated_at=now()
		where mod_resource_version_details.projection_source='import'`, revisionIDs, versionID, actorID); err != nil {
		return fmt.Errorf("sync imported resource version details: %w", err)
	}
	if _, err := tx.Exec(ctx, `delete from mod_resource_version_detail_localizations localization
		using mod_resource_version_details detail
		where localization.resource_id=detail.resource_id and localization.version_id=detail.version_id
		  and detail.version_id=$2 and detail.projection_source='import' and localization.provenance='import'
		  and exists(select 1 from resource_import_snapshots snapshot
			join catalog_import_revisions revision on revision.id=snapshot.revision_id
			where snapshot.revision_id=any($1::text[]) and snapshot.resource_id=detail.resource_id
			  and revision.source_namespace=detail.import_source_namespace
			  and revision.source_kind=detail.import_source_kind)`, revisionIDs, versionID); err != nil {
		return fmt.Errorf("clear replaced imported resource localizations: %w", err)
	}
	if _, err := tx.Exec(ctx, `insert into mod_resource_version_detail_localizations(
		resource_id,version_id,locale,name,summary,content_markdown,provenance)
		select distinct on (snapshot.resource_id,replace(lower(name.locale),'_','-'))
			snapshot.resource_id,$2::bigint,replace(lower(name.locale),'_','-'),name.value,'','','import'
		from resource_import_snapshots snapshot
		join catalog_import_revisions revision on revision.id=snapshot.revision_id
		join mod_resource_version_details detail on detail.resource_id=snapshot.resource_id and detail.version_id=$2
		  and detail.projection_source='import' and detail.import_source_namespace=revision.source_namespace
		  and detail.import_source_kind=revision.source_kind
		cross join lateral jsonb_each_text(snapshot.names) name(locale,value)
		where snapshot.revision_id=any($1::text[]) and name.value<>''
		order by snapshot.resource_id,replace(lower(name.locale),'_','-'),revision.revision_no desc,revision.id,name.locale
		on conflict(resource_id,version_id,locale) do update set
			name=excluded.name
		where mod_resource_version_detail_localizations.provenance='import'`, revisionIDs, versionID); err != nil {
		return fmt.Errorf("sync imported resource localizations: %w", err)
	}
	if err := ensureImportedContentSectionsTx(ctx, tx, revisionIDs, versionID, modID, actorID); err != nil {
		return fmt.Errorf("sync imported content sections: %w", err)
	}
	return nil
}

func archiveStaleImportedResourceDetailsTx(ctx context.Context, tx pgx.Tx, revisionIDs []string, versionID, actorID int64) error {
	if _, err := tx.Exec(ctx, `with incoming_scopes as materialized (
		select distinct source_kind,source_namespace from catalog_import_revisions where id=any($1::text[])
	), incoming_resources as materialized (
		select distinct snapshot.resource_id,revision.source_kind,revision.source_namespace
		from resource_import_snapshots snapshot join catalog_import_revisions revision on revision.id=snapshot.revision_id
		where snapshot.revision_id=any($1::text[])
	)
	update mod_resource_version_details detail set status='archived',updated_by=nullif($3::bigint,0),updated_at=now()
	where detail.version_id=$2 and detail.projection_source='import'
	  and exists(select 1 from incoming_scopes scope where scope.source_kind=detail.import_source_kind and scope.source_namespace=detail.import_source_namespace)
	  and not exists(select 1 from incoming_resources incoming where incoming.resource_id=detail.resource_id
		and incoming.source_kind=detail.import_source_kind and incoming.source_namespace=detail.import_source_namespace)`,
		revisionIDs, versionID, actorID); err != nil {
		return fmt.Errorf("archive stale imported resource details: %w", err)
	}
	if _, err := tx.Exec(ctx, `with incoming_scopes as materialized (
		select distinct source_kind,source_namespace from catalog_import_revisions where id=any($1::text[])
	)
	delete from mod_content_section_resources placement using mod_resource_version_details detail
	where placement.version_id=$2 and placement.placement_source='import'
	  and detail.version_id=placement.version_id and detail.resource_id=placement.resource_id
	  and detail.projection_source='import' and detail.status='archived'
	  and exists(select 1 from incoming_scopes scope where scope.source_kind=detail.import_source_kind and scope.source_namespace=detail.import_source_namespace)`,
		revisionIDs, versionID); err != nil {
		return fmt.Errorf("remove stale imported resource placements: %w", err)
	}
	return nil
}

func ensureImportedContentSectionsTx(ctx context.Context, tx pgx.Tx, revisionIDs []string, versionID, modID, actorID int64) error {
	rootSectionSQL := strings.ReplaceAll(`with imported_templates as (
		select distinct {{template_code}} template_code
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
		from missing cross join offset_value`, "{{template_code}}", importedContentTemplateCodeSQL("resource"))
	if _, err := tx.Exec(ctx, rootSectionSQL, revisionIDs, versionID, modID, actorID); err != nil {
		return fmt.Errorf("create imported content sections: %w", err)
	}

	// Item/block is one documentation page with two real, arrangeable child
	// classifications. Resolve the imported root, then use the same category
	// and localization authority as manual layout publication.
	var itemBlockRootID, itemBlockVersionID, itemBlockModID, itemBlockTemplateID int64
	var itemBlockDefaultLocale, itemBlockDisplayMode string
	itemBlockErr := tx.QueryRow(ctx, `select section.id,section.version_id,section.mod_id,section.template_id,
			section.default_locale,section.display_mode
		from mod_content_sections section
		join mod_content_templates template on template.id=section.template_id
		where section.version_id=$1 and section.parent_id is null and section.status='active'
		  and template.code='item_block' and template.builtin
		order by section.ordinal,section.id limit 1`, versionID).Scan(
		&itemBlockRootID,
		&itemBlockVersionID,
		&itemBlockModID,
		&itemBlockTemplateID,
		&itemBlockDefaultLocale,
		&itemBlockDisplayMode,
	)
	if itemBlockErr == nil {
		if itemBlockErr = ensureItemBlockSystemCategoriesTx(
			ctx, tx, itemBlockRootID, itemBlockVersionID, itemBlockModID, itemBlockTemplateID, actorID,
			itemBlockDefaultLocale, itemBlockDisplayMode,
		); itemBlockErr != nil {
			return fmt.Errorf("initialize imported item and block categories: %w", itemBlockErr)
		}
	} else if !errors.Is(itemBlockErr, pgx.ErrNoRows) {
		return fmt.Errorf("resolve imported item and block root: %w", itemBlockErr)
	}

	// Loot tables are imported into stable, real child classifications. The
	// exporter historically used singular values (block/chest/entity), so the
	// SQL also normalizes existing snapshots while new imports store the plural
	// category directly.
	lootCategorySectionSQL := strings.ReplaceAll(`with root as (
		select section.id,section.mod_id,section.version_id,section.template_id,section.default_locale,section.display_mode
		from mod_content_sections section
		join mod_content_templates template on template.id=section.template_id
		where section.version_id=$1 and section.parent_id is null and section.status='active'
		  and template.code='loot_table' and template.builtin
		order by section.ordinal,section.id limit 1
	), imported_categories as materialized (
		select distinct {{loot_category}} category
		from resource_import_snapshots snapshot
		join game_resources resource on resource.entity_id=snapshot.resource_id
		where snapshot.revision_id=any($2::text[]) and resource.kind_code='minecraft.loot_table'
	), requested(category,requested_ordinal) as (
		values ('blocks'::text,0),('chests',1),('entities',2),('fishing',3),
			('archaeology',4),('equipment',5),('gameplay',6),('other',7)
	), missing as (
		select requested.category,row_number() over(order by requested.requested_ordinal)-1 ordinal
		from root cross join requested join imported_categories imported using(category)
		where not exists(select 1 from mod_content_sections child
			where child.version_id=root.version_id and child.parent_id=root.id
			  and child.system_key='loot:'||requested.category and child.status='active')
	), offset_value as (
		select coalesce(max(child.ordinal)+1,0) value
		from root left join mod_content_sections child on child.parent_id=root.id and child.status='active'
	)
	insert into mod_content_sections(mod_id,version_id,template_id,parent_id,system_key,default_locale,display_mode,ordinal,status,created_by,updated_by)
	select root.mod_id,root.version_id,root.template_id,root.id,'loot:'||missing.category,root.default_locale,root.display_mode,
		offset_value.value+missing.ordinal,'active',nullif($3::bigint,0),nullif($3::bigint,0)
	from root cross join missing cross join offset_value`, "{{loot_category}}", importedLootCategorySQL("snapshot", "resource"))
	if _, err := tx.Exec(ctx, lootCategorySectionSQL, versionID, revisionIDs, actorID); err != nil {
		return fmt.Errorf("create imported loot table categories: %w", err)
	}
	if _, err := tx.Exec(ctx, `insert into mod_content_section_localizations(section_id,locale,name,description)
		select section.id,localization.locale,localization.name,''
		from mod_content_sections section
		cross join (values
			('loot:blocks'::text,'en-US'::text,'Block loot tables'::text),('loot:blocks','zh-CN','方块战利品表'),('loot:blocks','zh-TW','方塊戰利品表'),
			('loot:chests','en-US','Chest loot tables'),('loot:chests','zh-CN','宝藏箱战利品表'),('loot:chests','zh-TW','寶藏箱戰利品表'),
			('loot:entities','en-US','Entity loot tables'),('loot:entities','zh-CN','生物战利品表'),('loot:entities','zh-TW','生物戰利品表'),
			('loot:fishing','en-US','Fishing loot tables'),('loot:fishing','zh-CN','钓鱼战利品表'),('loot:fishing','zh-TW','釣魚戰利品表'),
			('loot:archaeology','en-US','Archaeology loot tables'),('loot:archaeology','zh-CN','考古战利品表'),('loot:archaeology','zh-TW','考古戰利品表'),
			('loot:equipment','en-US','Equipment loot tables'),('loot:equipment','zh-CN','装备战利品表'),('loot:equipment','zh-TW','裝備戰利品表'),
			('loot:gameplay','en-US','Gameplay loot tables'),('loot:gameplay','zh-CN','玩法战利品表'),('loot:gameplay','zh-TW','玩法戰利品表'),
			('loot:other','en-US','Other loot tables'),('loot:other','zh-CN','其他战利品表'),('loot:other','zh-TW','其他戰利品表')
		) localization(system_key,locale,name)
		where section.version_id=$1 and section.status='active'
		  and section.system_key=localization.system_key
		on conflict(section_id,locale) do nothing`, versionID); err != nil {
		return fmt.Errorf("localize imported loot table categories: %w", err)
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

	placementSQL := strings.NewReplacer(
		"{{template_code}}", importedContentTemplateCodeSQL("snapshot"),
		"{{loot_category}}", importedLootCategorySQL("snapshot", "snapshot"),
	).Replace(`with imported_snapshots as materialized (
		select snapshot.resource_id,resource.canonical_id,resource.kind_code,snapshot.data
		from resource_import_snapshots snapshot
		join catalog_import_revisions revision on revision.id=snapshot.revision_id
		join game_resources resource on resource.entity_id=snapshot.resource_id
		join mod_resource_version_details detail on detail.resource_id=snapshot.resource_id and detail.version_id=$2
		  and detail.projection_source='import' and detail.status='active'
		  and detail.import_source_namespace=revision.source_namespace and detail.import_source_kind=revision.source_kind
		where snapshot.revision_id=any($1::text[])
	), bound_item_ids as materialized (
		select distinct binding.item_resource_id
		from game_resource_asset_bindings binding
		join resource_import_snapshots block_snapshot on block_snapshot.id=binding.snapshot_id
		where block_snapshot.revision_id=any($1::text[]) and binding.item_resource_id is not null
	), imported_block_ids as materialized (
		select distinct canonical_id from imported_snapshots where kind_code='minecraft.block'
	), imported as (
		select distinct snapshot.resource_id,snapshot.canonical_id,snapshot.kind_code,{{template_code}} template_code,
		case when snapshot.kind_code='minecraft.loot_table' then {{loot_category}} else '' end loot_category
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
		  and child.system_key=case imported.kind_code
			when 'minecraft.block' then 'blocks'
			when 'minecraft.item' then 'items'
			when 'minecraft.loot_table' then 'loot:'||imported.loot_category
			else '' end
		where imported.template_code is not null
		  and (imported.template_code not in ('item_block','loot_table') or child.id is not null)
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
	on conflict(version_id,resource_id) do nothing`)
	_, err := tx.Exec(ctx, placementSQL, revisionIDs, versionID)
	if err != nil {
		return fmt.Errorf("attach imported resources to content sections: %w", err)
	}
	return nil
}

func lockImportedContentVersionTx(ctx context.Context, tx pgx.Tx, versionID int64) (int64, error) {
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(
		hashtextextended('mod-content-import:'||$1::bigint::text,0))`, versionID); err != nil {
		return 0, fmt.Errorf("lock imported content version: %w", err)
	}
	var modID int64
	if err := tx.QueryRow(ctx, `select mod_id from mod_content_versions
		where id=$1 and status='active' for update`, versionID).Scan(&modID); err != nil {
		return 0, err
	}
	return modID, nil
}

const importedContentTemplateCodeExpression = `case
	when {{resource}}.kind_code in ('minecraft.item','minecraft.block') then 'item_block'
	when {{resource}}.kind_code='minecraft.fluid' then 'fluid'
	when {{resource}}.kind_code='minecraft.dimension' then 'dimension'
	when {{resource}}.kind_code='minecraft.biome' then 'biome'
	when {{resource}}.kind_code='minecraft.entity_type' then 'entity'
	when {{resource}}.kind_code='minecraft.enchantment' then 'enchantment'
	when {{resource}}.kind_code in ('minecraft.mob_effect','minecraft.potion') then 'mob_effect'
	when {{resource}}.kind_code='minecraft.natural_generation' then 'natural_generation'
	when {{resource}}.kind_code='minecraft.structure' then 'world_structure'
	when {{resource}}.kind_code='minecraft.key_mapping' then 'key_mapping'
	when {{resource}}.kind_code='minecraft.advancement' then 'advancement'
	when {{resource}}.kind_code='minecraft.loot_table' then 'loot_table'
	when {{resource}}.kind_code='minecraft.game_setting' then 'game_setting'
	when {{resource}}.kind_code like 'mekanism.%' then 'chemical'
end`

const importedLootCategoryExpression = `case
	when lower(coalesce(nullif({{data}}.data->>'path',''),split_part({{resource}}.canonical_id,':',2))) like 'gameplay/fishing%'
	  or lower(coalesce({{data}}.data->>'category',''))='fishing' then 'fishing'
	when lower(coalesce({{data}}.data->>'category','')) in ('block','blocks')
	  or lower(coalesce(nullif({{data}}.data->>'path',''),split_part({{resource}}.canonical_id,':',2))) like 'blocks/%' then 'blocks'
	when lower(coalesce({{data}}.data->>'category','')) in ('chest','chests')
	  or lower(coalesce(nullif({{data}}.data->>'path',''),split_part({{resource}}.canonical_id,':',2))) like 'chests/%' then 'chests'
	when lower(coalesce({{data}}.data->>'category','')) in ('entity','entities')
	  or lower(coalesce(nullif({{data}}.data->>'path',''),split_part({{resource}}.canonical_id,':',2))) like 'entities/%' then 'entities'
	when lower(coalesce({{data}}.data->>'category',''))='archaeology'
	  or lower(coalesce(nullif({{data}}.data->>'path',''),split_part({{resource}}.canonical_id,':',2))) like 'archaeology/%' then 'archaeology'
	when lower(coalesce({{data}}.data->>'category',''))='equipment'
	  or lower(coalesce(nullif({{data}}.data->>'path',''),split_part({{resource}}.canonical_id,':',2))) like 'equipment/%' then 'equipment'
	when lower(coalesce({{data}}.data->>'category',''))='gameplay'
	  or lower(coalesce(nullif({{data}}.data->>'path',''),split_part({{resource}}.canonical_id,':',2))) like 'gameplay/%' then 'gameplay'
	else 'other'
end`

func importedContentTemplateCodeSQL(resourceAlias string) string {
	return strings.ReplaceAll(importedContentTemplateCodeExpression, "{{resource}}", checkedImportedContentSQLAlias(resourceAlias))
}

func importedLootCategorySQL(dataAlias, resourceAlias string) string {
	return strings.NewReplacer(
		"{{data}}", checkedImportedContentSQLAlias(dataAlias),
		"{{resource}}", checkedImportedContentSQLAlias(resourceAlias),
	).Replace(importedLootCategoryExpression)
}

func checkedImportedContentSQLAlias(value string) string {
	switch value {
	case "resource", "snapshot":
		return value
	default:
		panic("unsupported imported content SQL alias")
	}
}

func insertModExportImportActivityTx(ctx context.Context, tx pgx.Tx, actorID int64, versionPublicID string) error {
	_, err := tx.Exec(ctx, `insert into user_activity_events(user_id,action_id,object_type_id,object_route_id,occurred_at)
		select nullif($1,0),$2,$3,route.id,$5
		from mod_content_versions version
		join public_routes route on route.entity_type='mod' and route.internal_id=version.mod_id
		where version.public_id=$4`, actorID, activity.ActionUpload, activity.ObjectMod, versionPublicID, time.Now().UTC())
	return err
}
