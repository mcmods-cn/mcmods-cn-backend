package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type catalogResourceImportRow struct {
	EntityID       string
	PublicID       string
	KindCode       string
	CanonicalID    string
	RawID          string
	Namespace      string
	ResourcePath   string
	RevisionID     string
	SnapshotID     string
	Registry       string
	TranslationKey string
	Names          string
	Data           string
	IconPath       string
	PreviewPath    string
}

func persistCatalogResources(ctx context.Context, tx pgx.Tx, rows []catalogResourceImportRow) error {
	if len(rows) == 0 {
		return nil
	}
	if _, err := execImportStatement(ctx, tx, `create temporary table if not exists catalog_resource_import_stage (
		ordinal bigint not null,
		identity_key text not null,
		public_id text not null,
		kind_code text not null,
		canonical_id text not null,
		raw_id text not null,
		namespace text not null,
		resource_path text not null,
		revision_id text not null,
		snapshot_id text not null,
		registry text not null,
		translation_key text not null,
		names jsonb not null,
		data jsonb not null,
		icon_path text not null,
		preview_path text not null
	) on commit drop`); err != nil {
		return fmt.Errorf("create resource import stage: %w", err)
	}
	if _, err := execImportStatement(ctx, tx, `truncate catalog_resource_import_stage`); err != nil {
		return fmt.Errorf("truncate resource import stage: %w", err)
	}
	columns := []string{
		"ordinal", "identity_key", "public_id", "kind_code", "canonical_id", "raw_id",
		"namespace", "resource_path", "revision_id", "snapshot_id", "registry",
		"translation_key", "names", "data", "icon_path", "preview_path",
	}
	if err := copyImportRows(ctx, tx, "catalog_resource_import_stage", columns, len(rows), func(index int) ([]any, error) {
		row := rows[index]
		rawID := row.RawID
		if rawID == "" {
			rawID = row.CanonicalID
		}
		return []any{
			index, row.EntityID, row.PublicID, row.KindCode, row.CanonicalID, rawID,
			row.Namespace, row.ResourcePath, row.RevisionID, row.SnapshotID, row.Registry,
			row.TranslationKey, nonEmptyJSONObject(row.Names), nonEmptyJSONObject(row.Data),
			row.IconPath, row.PreviewPath,
		}, nil
	}); err != nil {
		return fmt.Errorf("stage resource imports: %w", err)
	}
	if _, err := execImportStatement(ctx, tx, `insert into resource_kinds(code,family,user_visible)
		select distinct kind_code,split_part(kind_code,'.',1),true
		from catalog_resource_import_stage
		on conflict(code) do nothing`); err != nil {
		return fmt.Errorf("upsert resource kinds: %w", err)
	}
	if _, err := execImportStatement(ctx, tx, `insert into catalog_entities(identity_key,public_id,entity_type,status)
		select distinct on (stage.identity_key) stage.identity_key,stage.public_id,'resource','active'
		from catalog_resource_import_stage stage
		left join game_resources existing
			on existing.kind_code=stage.kind_code and existing.canonical_id=stage.canonical_id
		where existing.entity_id is null
		order by stage.identity_key,stage.ordinal
		on conflict(identity_key) do update set status='active',updated_at=now()`); err != nil {
		return fmt.Errorf("upsert resource entities: %w", err)
	}
	if _, err := execImportStatement(ctx, tx, `insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,owner_mod_id,created_from_revision_id,resolved)
		select distinct on (stage.kind_code,stage.canonical_id)
			coalesce(existing.entity_id,entity.id),stage.kind_code,stage.canonical_id,stage.namespace,stage.resource_path,
			revision.mod_id,stage.revision_id,true
		from catalog_resource_import_stage stage
		left join game_resources existing
			on existing.kind_code=stage.kind_code and existing.canonical_id=stage.canonical_id
		left join catalog_entities entity on entity.identity_key=stage.identity_key
		join catalog_import_revisions revision on revision.id=stage.revision_id
		where existing.entity_id is not null or entity.id is not null
		order by stage.kind_code,stage.canonical_id,stage.ordinal
		on conflict(kind_code,canonical_id) do update set namespace=excluded.namespace,resource_path=excluded.resource_path,
			owner_mod_id=coalesce(game_resources.owner_mod_id,excluded.owner_mod_id),
			created_from_revision_id=coalesce(game_resources.created_from_revision_id,excluded.created_from_revision_id),
			resolved=true,updated_at=now()`); err != nil {
		return fmt.Errorf("upsert game resources: %w", err)
	}
	if _, err := execImportStatement(ctx, tx, `update catalog_entities entity
		set status='active',updated_at=now()
		from game_resources resource
		join catalog_resource_import_stage stage
			on stage.kind_code=resource.kind_code and stage.canonical_id=resource.canonical_id
		where entity.id=resource.entity_id and entity.status<>'active'`); err != nil {
		return fmt.Errorf("activate resource entities: %w", err)
	}
	if _, err := execImportStatement(ctx, tx, `insert into game_resource_aliases(kind_code,alias_id,resource_id,source)
		select distinct on (stage.kind_code,stage.raw_id) stage.kind_code,stage.raw_id,resource.entity_id,'mod_id'
		from catalog_resource_import_stage stage
		join game_resources resource
			on resource.kind_code=stage.kind_code and resource.canonical_id=stage.canonical_id
		where stage.raw_id<>''
		order by stage.kind_code,stage.raw_id,stage.ordinal
		on conflict(kind_code,alias_id) do update set resource_id=excluded.resource_id,source=excluded.source,updated_at=now()`); err != nil {
		return fmt.Errorf("upsert resource aliases: %w", err)
	}
	if _, err := execImportStatement(ctx, tx, `insert into game_resource_aliases(kind_code,alias_id,resource_id,source)
		select distinct on (stage.kind_code,stage.canonical_id) stage.kind_code,stage.canonical_id,resource.entity_id,'canonical'
		from catalog_resource_import_stage stage
		join game_resources resource
			on resource.kind_code=stage.kind_code and resource.canonical_id=stage.canonical_id
		where stage.canonical_id<>''
		order by stage.kind_code,stage.canonical_id,stage.ordinal
		on conflict(kind_code,alias_id) do update set resource_id=excluded.resource_id,source=excluded.source,updated_at=now()`); err != nil {
		return fmt.Errorf("upsert canonical resource aliases: %w", err)
	}
	if _, err := execImportStatement(ctx, tx, `with snapshot_fields as materialized (
			select identity_key,revision_id,
				(array_agg(snapshot_id order by ordinal))[1] as snapshot_id,
				(array_agg(kind_code order by ordinal))[1] as kind_code,
				(array_agg(canonical_id order by ordinal))[1] as canonical_id,
				coalesce((array_agg(registry order by ordinal desc) filter(where registry<>''))[1],'') as registry,
				coalesce((array_agg(translation_key order by ordinal desc) filter(where translation_key<>''))[1],'') as translation_key,
				coalesce((array_agg(icon_path order by ordinal desc) filter(where icon_path<>''))[1],'') as icon_path,
				coalesce((array_agg(preview_path order by ordinal desc) filter(where preview_path<>''))[1],'') as preview_path
			from catalog_resource_import_stage
			group by identity_key,revision_id
		),
		snapshot_names as materialized (
			select stage.identity_key,stage.revision_id,
				jsonb_object_agg(entry.key,entry.value order by stage.ordinal) as names
			from catalog_resource_import_stage stage
			cross join lateral jsonb_each(stage.names) entry
			group by stage.identity_key,stage.revision_id
		),
		snapshot_data as materialized (
			select stage.identity_key,stage.revision_id,
				jsonb_object_agg(entry.key,entry.value order by stage.ordinal) as data
			from catalog_resource_import_stage stage
			cross join lateral jsonb_each(stage.data) entry
			group by stage.identity_key,stage.revision_id
		)
		insert into resource_import_snapshots(
		id,resource_id,revision_id,registry,translation_key,names,data,icon_path,preview_path)
		select fields.snapshot_id,resource.entity_id,fields.revision_id,fields.registry,fields.translation_key,
			coalesce(names.names,'{}'::jsonb),coalesce(data.data,'{}'::jsonb),fields.icon_path,fields.preview_path
		from snapshot_fields fields
		join game_resources resource
			on resource.kind_code=fields.kind_code and resource.canonical_id=fields.canonical_id
		left join snapshot_names names using(identity_key,revision_id)
		left join snapshot_data data using(identity_key,revision_id)
		on conflict(resource_id,revision_id) do update set
			registry=case when excluded.registry<>'' then excluded.registry else resource_import_snapshots.registry end,
			translation_key=case when excluded.translation_key<>'' then excluded.translation_key else resource_import_snapshots.translation_key end,
			names=resource_import_snapshots.names||excluded.names,
			data=resource_import_snapshots.data||excluded.data,
			icon_path=case when excluded.icon_path<>'' then excluded.icon_path else resource_import_snapshots.icon_path end,
			preview_path=case when excluded.preview_path<>'' then excluded.preview_path else resource_import_snapshots.preview_path end`); err != nil {
		return fmt.Errorf("upsert resource snapshots: %w", err)
	}
	// Resolve only aliases represented by this import batch. The previous query
	// joined every pending reference to the complete alias/resource catalog and
	// filtered the imported entities afterwards. On large exporter packages that
	// allowed PostgreSQL to choose a very expensive global join plan.
	if _, err := execImportStatement(ctx, tx, `with imported_aliases as materialized (
			select distinct source.kind_code,source.alias_id,resource.entity_id as resource_id
			from (
				select kind_code,raw_id as alias_id,canonical_id
				from catalog_resource_import_stage
				union
				select kind_code,canonical_id as alias_id,canonical_id
				from catalog_resource_import_stage
			) source
			join game_resources resource
				on resource.kind_code=source.kind_code and resource.canonical_id=source.canonical_id
			where source.alias_id<>''
		)
		update unresolved_resource_references unresolved
		set resolved_resource_id=imported.resource_id,status='resolved',resolved_at=now()
		from imported_aliases imported
		where unresolved.status='pending'
			and unresolved.kind_code=imported.kind_code
			and unresolved.raw_resource_id=imported.alias_id
			and unresolved.resolved_resource_id is distinct from imported.resource_id`); err != nil {
		return fmt.Errorf("resolve resource references: %w", err)
	}
	return nil
}

func queueCatalogResource(batch *modExportWriteBatch, row catalogResourceImportRow) {
	batch.catalogRows = append(batch.catalogRows, row)
	batch.byteCount += int64(len(row.Names) + len(row.Data))
}

func nonEmptyJSONObject(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "null" || !json.Valid([]byte(trimmed)) {
		return "{}"
	}
	return trimmed
}
