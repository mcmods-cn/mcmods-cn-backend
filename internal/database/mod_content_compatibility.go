package database

// compatibleModContentLayoutStatements installs the item/block layout identity
// rules on generation-30 development databases created before those rules were
// introduced. The statements are idempotent and preserve human-created trees.
func compatibleModContentLayoutStatements() []string {
	return []string{
		`alter table mod_content_sections add column if not exists system_key text not null default ''`,
		`alter table mod_content_section_resources add column if not exists placement_source text not null default 'manual'`,
		`alter table mod_content_section_resources add column if not exists placement_identity_key text`,
		`create unique index if not exists idx_mod_content_sections_system_key
			on mod_content_sections(version_id,parent_id,system_key)
			where system_key<>'' and status='active'`,
		`with roots as (
			select section.id,section.mod_id,section.version_id,section.template_id,
				section.default_locale,section.display_mode
			from mod_content_sections section
			join mod_content_templates template on template.id=section.template_id
			where section.parent_id is null and section.status='active'
			  and template.code='item_block' and template.builtin
		), requested(system_key,requested_ordinal) as (
			values ('blocks'::text,0),('items'::text,1)
		), missing as (
			select roots.*,requested.system_key,requested.requested_ordinal,
				coalesce((select max(child.ordinal)+1 from mod_content_sections child
					where child.parent_id=roots.id and child.status='active'),0)
				+row_number() over(partition by roots.id order by requested.requested_ordinal)-1 ordinal
			from roots cross join requested
			where not exists (
				select 1 from mod_content_sections child
				where child.version_id=roots.version_id and child.parent_id=roots.id
				  and child.system_key=requested.system_key and child.status='active'
			)
		)
		insert into mod_content_sections(
			mod_id,version_id,template_id,parent_id,system_key,default_locale,
			display_mode,ordinal,status)
		select mod_id,version_id,template_id,id,system_key,default_locale,
			display_mode,ordinal,'active'
		from missing`,
		`insert into public_routes(public_id,entity_type,internal_id)
			select section.public_id,'mod_content_section',section.id
			from mod_content_sections section
			where section.system_key in ('items','blocks')
			on conflict do nothing`,
		`insert into mod_content_section_localizations(section_id,locale,name,description)
			select section.id,localization.locale,localization.name,''
			from mod_content_sections section
			cross join (values
				('blocks'::text,'en-US'::text,'Blocks'::text),
				('blocks','zh-CN','方块'),('blocks','zh-TW','方塊'),
				('items','en-US','Items'),('items','zh-CN','物品'),('items','zh-TW','物品')
			) localization(system_key,locale,name)
			where section.status='active' and section.system_key=localization.system_key
			on conflict(section_id,locale) do nothing`,
		`delete from mod_content_section_resources item_placement
			using game_resources item_resource
			where item_resource.entity_id=item_placement.resource_id
			  and item_resource.kind_code='minecraft.item'
			  and exists (
				select 1
				from mod_content_section_resources block_placement
				join game_resources block_resource on block_resource.entity_id=block_placement.resource_id
				where block_placement.version_id=item_placement.version_id
				  and block_resource.kind_code='minecraft.block'
				  and block_resource.canonical_id=item_resource.canonical_id
			  )`,
		`delete from mod_content_section_resources item_placement
			using game_resources item_resource
			where item_resource.entity_id=item_placement.resource_id
			  and item_resource.kind_code='minecraft.item'
			  and exists (
				select 1
				from game_resource_asset_bindings binding
				join resource_import_snapshots block_snapshot on block_snapshot.id=binding.snapshot_id
				join catalog_import_revisions revision on revision.id=block_snapshot.revision_id
				join mod_content_section_resources block_placement
				  on block_placement.version_id=item_placement.version_id
				 and block_placement.resource_id=binding.block_resource_id
				where binding.item_resource_id=item_placement.resource_id
				  and revision.target_version_id=item_placement.version_id
				  and revision.is_active and revision.status in ('ready','partial')
			  )`,
		`with roots as (
			select section.id,section.version_id
			from mod_content_sections section
			join mod_content_templates template on template.id=section.template_id
			where section.parent_id is null and section.status='active'
			  and template.code='item_block' and template.builtin
		), removed as (
			delete from mod_content_section_resources placement
			using roots
			where placement.section_id=roots.id and placement.version_id=roots.version_id
			returning placement.section_id root_id,placement.version_id,placement.resource_id,
				placement.placement_source,placement.created_at
		), target as (
			select removed.*,child.id section_id
			from removed
			join game_resources resource on resource.entity_id=removed.resource_id
			join mod_content_sections child on child.version_id=removed.version_id
			  and child.parent_id=removed.root_id
			  and child.status='active'
			  and child.system_key=case resource.kind_code
				when 'minecraft.block' then 'blocks'
				when 'minecraft.item' then 'items'
				else ''
			  end
			where resource.kind_code in ('minecraft.item','minecraft.block')
			  and not exists (
				select 1 from mod_content_section_resources existing
				where existing.version_id=removed.version_id
				  and existing.resource_id=removed.resource_id
				  and existing.section_id<>removed.root_id
			  )
		), numbered as (
			select target.*,
				row_number() over(partition by target.section_id order by target.created_at,target.resource_id)-1 new_ordinal
			from target
		), offsets as (
			select child.id section_id,coalesce(max(existing.ordinal)+1,0) value
			from mod_content_sections child
			left join mod_content_section_resources existing on existing.section_id=child.id
			where child.system_key in ('items','blocks') and child.status='active'
			group by child.id
		)
		insert into mod_content_section_resources(
			section_id,version_id,resource_id,ordinal,placement_source,created_at)
		select numbered.section_id,numbered.version_id,numbered.resource_id,
			offsets.value+numbered.new_ordinal,numbered.placement_source,numbered.created_at
		from numbered join offsets on offsets.section_id=numbered.section_id
		on conflict do nothing`,
		`with active_imports as materialized (
			select revision.target_version_id version_id,snapshot.resource_id,
				resource.kind_code,resource.canonical_id
			from catalog_import_revisions revision
			join resource_import_snapshots snapshot on snapshot.revision_id=revision.id
			join game_resources resource on resource.entity_id=snapshot.resource_id
			where revision.is_active and revision.status in ('ready','partial')
			  and resource.kind_code in ('minecraft.item','minecraft.block')
		), canonical_imports as (
			select distinct imported.version_id,imported.resource_id,
				imported.kind_code,imported.canonical_id
			from active_imports imported
			where imported.kind_code<>'minecraft.item'
			   or (
				not exists (
					select 1 from active_imports block
					where block.version_id=imported.version_id
					  and block.kind_code='minecraft.block'
					  and block.canonical_id=imported.canonical_id
				)
				and not exists (
					select 1
					from game_resource_asset_bindings binding
					join resource_import_snapshots block_snapshot on block_snapshot.id=binding.snapshot_id
					join catalog_import_revisions block_revision on block_revision.id=block_snapshot.revision_id
					where binding.item_resource_id=imported.resource_id
					  and block_revision.target_version_id=imported.version_id
					  and block_revision.is_active
					  and block_revision.status in ('ready','partial')
				)
			   )
		), target as (
			select imported.version_id,imported.resource_id,imported.canonical_id,child.id section_id
			from canonical_imports imported
			join mod_content_sections root on root.version_id=imported.version_id
			  and root.parent_id is null and root.status='active'
			join mod_content_templates template on template.id=root.template_id
			  and template.code='item_block' and template.builtin
			join mod_content_sections child on child.version_id=imported.version_id
			  and child.parent_id=root.id and child.status='active'
			  and child.system_key=case imported.kind_code
				when 'minecraft.block' then 'blocks'
				when 'minecraft.item' then 'items'
			  end
			where not exists (
				select 1 from mod_content_section_resources existing
				where existing.version_id=imported.version_id
				  and existing.resource_id=imported.resource_id
			)
		), numbered as (
			select target.*,
				row_number() over(partition by target.section_id order by target.canonical_id,target.resource_id)-1 new_ordinal
			from target
		), offsets as (
			select child.id section_id,coalesce(max(existing.ordinal)+1,0) value
			from mod_content_sections child
			left join mod_content_section_resources existing on existing.section_id=child.id
			where child.system_key in ('items','blocks') and child.status='active'
			group by child.id
		)
		insert into mod_content_section_resources(
			section_id,version_id,resource_id,ordinal,placement_source)
		select numbered.section_id,numbered.version_id,numbered.resource_id,
			offsets.value+numbered.new_ordinal,'import'
		from numbered join offsets on offsets.section_id=numbered.section_id
		on conflict do nothing`,
		`create or replace function assign_mod_content_placement_identity() returns trigger as $$
		declare resource_kind text; resource_canonical_id text; block_representative_id bigint;
		begin
			select kind_code,canonical_id into strict resource_kind,resource_canonical_id
			from game_resources where entity_id=new.resource_id;
			if resource_kind in ('minecraft.item','minecraft.block') then
				select binding.block_resource_id into block_representative_id
				from game_resource_asset_bindings binding
				join resource_import_snapshots snapshot on snapshot.id=binding.snapshot_id
				join catalog_import_revisions revision on revision.id=snapshot.revision_id
				where revision.target_version_id=new.version_id and revision.is_active
				  and revision.status in ('ready','partial')
				  and binding.block_resource_id is not null
				  and new.resource_id in (binding.item_resource_id,binding.block_resource_id)
				order by coalesce(revision.activated_at,revision.created_at) desc,binding.block_resource_id
				limit 1;
				if block_representative_id is not null then
					new.placement_identity_key := 'item-block:resource:'||block_representative_id::text;
				else
					new.placement_identity_key := 'item-block:canonical:'||lower(resource_canonical_id);
				end if;
			else
				new.placement_identity_key := 'resource:'||new.resource_id::text;
			end if;
			return new;
		end;
		$$ language plpgsql`,
		`drop trigger if exists trg_mod_content_placement_identity on mod_content_section_resources`,
		`create trigger trg_mod_content_placement_identity
			before insert or update of resource_id,version_id
			on mod_content_section_resources
			for each row execute function assign_mod_content_placement_identity()`,
		`update mod_content_section_resources set version_id=version_id
			where placement_identity_key is null or placement_identity_key=''`,
		`alter table mod_content_section_resources alter column placement_identity_key set not null`,
		`create unique index if not exists idx_mod_content_section_resources_version_resource
			on mod_content_section_resources(version_id,resource_id)`,
		`create unique index if not exists idx_mod_content_section_resources_version_identity
			on mod_content_section_resources(version_id,placement_identity_key)`,
	}
}
