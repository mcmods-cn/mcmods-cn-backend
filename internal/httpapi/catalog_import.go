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
	entityIDs := make([]string, len(rows))
	publicIDs := make([]string, len(rows))
	kinds := make([]string, len(rows))
	canonicalIDs := make([]string, len(rows))
	rawIDs := make([]string, len(rows))
	namespaces := make([]string, len(rows))
	resourcePaths := make([]string, len(rows))
	revisionIDs := make([]string, len(rows))
	snapshotIDs := make([]string, len(rows))
	registries := make([]string, len(rows))
	translationKeys := make([]string, len(rows))
	names := make([]string, len(rows))
	data := make([]string, len(rows))
	iconPaths := make([]string, len(rows))
	previewPaths := make([]string, len(rows))
	for index, row := range rows {
		entityIDs[index] = row.EntityID
		publicIDs[index] = row.PublicID
		kinds[index] = row.KindCode
		canonicalIDs[index] = row.CanonicalID
		rawIDs[index] = row.RawID
		if rawIDs[index] == "" {
			rawIDs[index] = row.CanonicalID
		}
		namespaces[index] = row.Namespace
		resourcePaths[index] = row.ResourcePath
		revisionIDs[index] = row.RevisionID
		snapshotIDs[index] = row.SnapshotID
		registries[index] = row.Registry
		translationKeys[index] = row.TranslationKey
		names[index] = nonEmptyJSONObject(row.Names)
		data[index] = nonEmptyJSONObject(row.Data)
		iconPaths[index] = row.IconPath
		previewPaths[index] = row.PreviewPath
	}
	if _, err := tx.Exec(ctx, `insert into resource_kinds(code,family,user_visible)
		select distinct kind,split_part(kind,'.',1),true from unnest($1::text[]) kind
		on conflict(code) do nothing`, kinds); err != nil {
		return fmt.Errorf("upsert resource kinds: %w", err)
	}
	if _, err := tx.Exec(ctx, `insert into catalog_entities(id,public_id,entity_type,status)
		select distinct on (id) id,public_id,'resource','active'
		from unnest($1::text[],$2::text[]) row(id,public_id)
		on conflict(id) do update set status='active',updated_at=now()`, entityIDs, publicIDs); err != nil {
		return fmt.Errorf("upsert resource entities: %w", err)
	}
	if _, err := tx.Exec(ctx, `insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,owner_mod_id,created_from_revision_id,resolved)
		select distinct on (row.entity_id) row.entity_id,row.kind_code,row.canonical_id,row.namespace,row.resource_path,revision.mod_id,row.revision_id,true
		from unnest($1::text[],$2::text[],$3::text[],$4::text[],$5::text[],$6::text[])
			row(entity_id,kind_code,canonical_id,namespace,resource_path,revision_id)
		join catalog_import_revisions revision on revision.id=row.revision_id
		on conflict(kind_code,canonical_id) do update set namespace=excluded.namespace,resource_path=excluded.resource_path,
			owner_mod_id=coalesce(game_resources.owner_mod_id,excluded.owner_mod_id),
			created_from_revision_id=coalesce(game_resources.created_from_revision_id,excluded.created_from_revision_id),
			resolved=true,updated_at=now()`, entityIDs, kinds, canonicalIDs, namespaces, resourcePaths, revisionIDs); err != nil {
		return fmt.Errorf("upsert game resources: %w", err)
	}
	if _, err := tx.Exec(ctx, `insert into game_resource_aliases(kind_code,alias_id,resource_id,source)
		select distinct row.kind_code,row.alias_id,row.entity_id,'mod_id'
		from unnest($1::text[],$2::text[],$3::text[]) row(kind_code,alias_id,entity_id)
		where row.alias_id<>'' on conflict(kind_code,alias_id) do update set resource_id=excluded.resource_id,source=excluded.source,updated_at=now()`, kinds, rawIDs, entityIDs); err != nil {
		return fmt.Errorf("upsert resource aliases: %w", err)
	}
	if _, err := tx.Exec(ctx, `insert into game_resource_aliases(kind_code,alias_id,resource_id,source)
		select distinct row.kind_code,row.alias_id,row.entity_id,'canonical'
		from unnest($1::text[],$2::text[],$3::text[]) row(kind_code,alias_id,entity_id)
		where row.alias_id<>'' on conflict(kind_code,alias_id) do update set resource_id=excluded.resource_id,source=excluded.source,updated_at=now()`, kinds, canonicalIDs, entityIDs); err != nil {
		return fmt.Errorf("upsert canonical resource aliases: %w", err)
	}
	if _, err := tx.Exec(ctx, `insert into resource_import_snapshots(
		id,resource_id,revision_id,registry,translation_key,names,data,icon_path,preview_path)
		select row.snapshot_id,row.entity_id,row.revision_id,row.registry,row.translation_key,row.names::jsonb,row.data::jsonb,row.icon_path,row.preview_path
		from unnest($1::text[],$2::text[],$3::text[],$4::text[],$5::text[],$6::text[],$7::text[],$8::text[],$9::text[])
			row(snapshot_id,entity_id,revision_id,registry,translation_key,names,data,icon_path,preview_path)
		on conflict(resource_id,revision_id) do update set
			registry=case when excluded.registry<>'' then excluded.registry else resource_import_snapshots.registry end,
			translation_key=case when excluded.translation_key<>'' then excluded.translation_key else resource_import_snapshots.translation_key end,
			names=resource_import_snapshots.names||excluded.names,
			data=resource_import_snapshots.data||excluded.data,
			icon_path=case when excluded.icon_path<>'' then excluded.icon_path else resource_import_snapshots.icon_path end,
			preview_path=case when excluded.preview_path<>'' then excluded.preview_path else resource_import_snapshots.preview_path end`,
		snapshotIDs, entityIDs, revisionIDs, registries, translationKeys, names, data, iconPaths, previewPaths); err != nil {
		return fmt.Errorf("upsert resource snapshots: %w", err)
	}
	if _, err := tx.Exec(ctx, `update unresolved_resource_references unresolved
		set resolved_resource_id=resource.entity_id,status='resolved',resolved_at=now()
		from game_resource_aliases alias join game_resources resource on resource.entity_id=alias.resource_id
		where unresolved.status='pending' and unresolved.kind_code=alias.kind_code
			and unresolved.raw_resource_id=alias.alias_id and resource.entity_id=any($1::text[])`, entityIDs); err != nil {
		return fmt.Errorf("resolve resource references: %w", err)
	}
	return nil
}

func queueCatalogResource(batch *modExportWriteBatch, row catalogResourceImportRow) {
	batch.queue(`insert into resource_kinds(code,family,user_visible) values($1,split_part($1,'.',1),true) on conflict(code) do nothing`, 0, row.KindCode)
	batch.queue(`insert into catalog_entities(id,public_id,entity_type,status) values($1,$2,'resource','active')
		on conflict(id) do update set status='active',updated_at=now()`, 0, row.EntityID, row.PublicID)
	batch.queue(`insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,owner_mod_id,created_from_revision_id,resolved)
		select $1,$2,$3,$4,$5,revision.mod_id,$6,true from catalog_import_revisions revision where revision.id=$6
		on conflict(kind_code,canonical_id) do update set namespace=excluded.namespace,resource_path=excluded.resource_path,
			owner_mod_id=coalesce(game_resources.owner_mod_id,excluded.owner_mod_id),
			created_from_revision_id=coalesce(game_resources.created_from_revision_id,excluded.created_from_revision_id),
			resolved=true,updated_at=now()`,
		0, row.EntityID, row.KindCode, row.CanonicalID, row.Namespace, row.ResourcePath, row.RevisionID)
	rawID := row.RawID
	if rawID == "" {
		rawID = row.CanonicalID
	}
	batch.queue(`insert into game_resource_aliases(kind_code,alias_id,resource_id,source) values($1,$2,$3,'mod_id')
		on conflict(kind_code,alias_id) do update set resource_id=excluded.resource_id,source=excluded.source,updated_at=now()`, 0,
		row.KindCode, rawID, row.EntityID)
	batch.queue(`insert into game_resource_aliases(kind_code,alias_id,resource_id,source) values($1,$2,$3,'canonical')
		on conflict(kind_code,alias_id) do update set resource_id=excluded.resource_id,source=excluded.source,updated_at=now()`, 0,
		row.KindCode, row.CanonicalID, row.EntityID)
	batch.queue(`insert into resource_import_snapshots(id,resource_id,revision_id,registry,translation_key,names,data,icon_path,preview_path)
		values($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,$9)
		on conflict(resource_id,revision_id) do update set
			registry=case when excluded.registry<>'' then excluded.registry else resource_import_snapshots.registry end,
			translation_key=case when excluded.translation_key<>'' then excluded.translation_key else resource_import_snapshots.translation_key end,
			names=resource_import_snapshots.names||excluded.names,data=resource_import_snapshots.data||excluded.data,
			icon_path=case when excluded.icon_path<>'' then excluded.icon_path else resource_import_snapshots.icon_path end,
			preview_path=case when excluded.preview_path<>'' then excluded.preview_path else resource_import_snapshots.preview_path end`,
		int64(len(row.Names)+len(row.Data)), row.SnapshotID, row.EntityID, row.RevisionID, row.Registry, row.TranslationKey,
		nonEmptyJSONObject(row.Names), nonEmptyJSONObject(row.Data), row.IconPath, row.PreviewPath)
	batch.queue(`update unresolved_resource_references set resolved_resource_id=$1,status='resolved',resolved_at=now()
		where status='pending' and kind_code=$2 and raw_resource_id in ($3,$4)`, 0, row.EntityID, row.KindCode, rawID, row.CanonicalID)
}

func nonEmptyJSONObject(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "null" || !json.Valid([]byte(trimmed)) {
		return "{}"
	}
	return trimmed
}
