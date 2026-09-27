package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func execImportStatement(
	ctx context.Context,
	tx pgx.Tx,
	query string,
	args ...any,
) (pgconn.CommandTag, error) {
	started := time.Now()
	tag, err := tx.Exec(ctx, query, args...)
	elapsed := time.Since(started)
	if elapsed >= time.Second {
		label := strings.Join(strings.Fields(query), " ")
		if len(label) > 96 {
			label = label[:96]
		}
		slog.Info("slow import statement", "duration", elapsed.Round(time.Millisecond), "statement", label)
	}
	return tag, err
}

func copyImportRows(
	ctx context.Context,
	tx pgx.Tx,
	table string,
	columns []string,
	rowCount int,
	row func(int) ([]any, error),
) error {
	started := time.Now()
	copied, err := tx.CopyFrom(ctx, pgx.Identifier{table}, columns, pgx.CopyFromSlice(rowCount, row))
	elapsed := time.Since(started)
	if elapsed >= time.Second {
		slog.Info("slow import copy", "duration", elapsed.Round(time.Millisecond), "table", table, "rows", rowCount)
	}
	if err != nil {
		return err
	}
	if copied != int64(rowCount) {
		return fmt.Errorf("copied %d of %d rows into %s", copied, rowCount, table)
	}
	return nil
}

func recipeImportJSONValue(value json.RawMessage) string {
	if len(value) == 0 {
		return "{}"
	}
	return string(value)
}

func recipeImportOptional[T any](value *T) any {
	if value == nil {
		return nil
	}
	return *value
}

type recipeImportRecipeWrite struct {
	TypeIdentity               string          `json:"type_identity"`
	TypePublicID               string          `json:"type_public_id"`
	RecipeTypeID               string          `json:"recipe_type_id"`
	RecipeIdentity             string          `json:"recipe_identity"`
	RecipePublicID             string          `json:"recipe_public_id"`
	CanonicalSourceID          string          `json:"canonical_source_id"`
	SemanticFingerprint        string          `json:"semantic_fingerprint"`
	IdentitySource             string          `json:"identity_source"`
	RevisionID                 string          `json:"revision_id"`
	SnapshotID                 string          `json:"snapshot_id"`
	SourceRecipeID             string          `json:"source_recipe_id"`
	SourceRecipeKey            string          `json:"source_recipe_key"`
	CollectionPath             string          `json:"collection_path"`
	OriginKind                 string          `json:"origin_kind"`
	UnderlyingRecipeTypeID     string          `json:"underlying_recipe_type_id"`
	SourceModID                string          `json:"source_mod_id"`
	SourceModVersion           string          `json:"source_mod_version"`
	SourceModIDSource          string          `json:"source_mod_id_source"`
	RenderLocale               string          `json:"render_locale"`
	TemplateID                 string          `json:"template_id"`
	LayoutKind                 string          `json:"layout_kind"`
	Ordered                    *bool           `json:"ordered"`
	LayoutClassificationSource string          `json:"layout_classification_source"`
	Width                      *int            `json:"width"`
	Height                     *int            `json:"height"`
	Parameters                 json.RawMessage `json:"parameters"`
	BindingCount               int             `json:"binding_count"`
}

type recipeImportBindingWrite struct {
	ID                string `json:"id"`
	RecipeSnapshotID  string `json:"recipe_snapshot_id"`
	TemplateSlotID    string `json:"template_slot_id"`
	SourceSlotID      string `json:"source_slot_id"`
	Ordinal           int    `json:"ordinal"`
	IngredientPresent bool   `json:"ingredient_present"`
	Clickable         bool   `json:"clickable"`
	PlaceholderItem   string `json:"placeholder_item"`
	ItemTagEquivalent string `json:"item_tag_equivalent"`
	SemanticRole      string `json:"semantic_role"`
	RoleSource        string `json:"role_source"`
	TagIdentity       string `json:"tag_identity"`
	TagPublicID       string `json:"tag_public_id"`
	TagCanonicalID    string `json:"tag_canonical_id"`
}

type recipeImportCandidateWrite struct {
	BindingID            string          `json:"binding_id"`
	AlternativeIndex     int             `json:"alternative_index"`
	ResourceIdentity     string          `json:"resource_identity"`
	ResourcePublicID     string          `json:"resource_public_id"`
	ResourceCanonicalID  string          `json:"resource_canonical_id"`
	ResourceRawID        string          `json:"resource_raw_id"`
	ResourceAliasID      string          `json:"resource_alias_id"`
	ResourceNamespace    string          `json:"resource_namespace"`
	ResourcePath         string          `json:"resource_path"`
	KindCode             string          `json:"kind_code"`
	Amount               float64         `json:"amount"`
	IngredientKind       string          `json:"ingredient_kind"`
	IngredientType       string          `json:"ingredient_type"`
	UniqueID             string          `json:"unique_id"`
	NBTSNBT              string          `json:"nbt_snbt"`
	ChanceAvailable      bool            `json:"chance_available"`
	Chance               *float64        `json:"chance"`
	ChancePercent        *float64        `json:"chance_percent"`
	ChanceComparator     string          `json:"chance_comparator"`
	ChanceSource         string          `json:"chance_source"`
	ChanceText           string          `json:"chance_text"`
	ChanceTexts          json.RawMessage `json:"chance_texts"`
	ChanceTranslationKey string          `json:"chance_translation_key"`
	ChanceRenderX        *float64        `json:"chance_render_x"`
	ChanceRenderY        *float64        `json:"chance_render_y"`
	Byproduct            bool            `json:"byproduct"`
}

type recipeImportCandidateResourceWrite struct {
	Identity    string
	PublicID    string
	CanonicalID string
	Namespace   string
	Path        string
	KindCode    string
}

func uniqueRecipeImportCandidateResources(candidates []recipeImportCandidateWrite) ([]recipeImportCandidateResourceWrite, error) {
	resources := make([]recipeImportCandidateResourceWrite, 0)
	indexes := make(map[string]int, len(candidates))
	for _, candidate := range candidates {
		if candidate.ResourceIdentity == "" || candidate.ResourceCanonicalID == "" || candidate.KindCode == "" {
			return nil, errors.New("recipe candidate is missing its normalized resource identity")
		}
		if index, exists := indexes[candidate.ResourceIdentity]; exists {
			existing := resources[index]
			if existing.CanonicalID != candidate.ResourceCanonicalID || existing.KindCode != candidate.KindCode {
				return nil, errors.New("recipe candidate resource identity collision")
			}
			continue
		}
		indexes[candidate.ResourceIdentity] = len(resources)
		resources = append(resources, recipeImportCandidateResourceWrite{
			Identity: candidate.ResourceIdentity, PublicID: candidate.ResourcePublicID,
			CanonicalID: candidate.ResourceCanonicalID, Namespace: candidate.ResourceNamespace,
			Path: candidate.ResourcePath, KindCode: candidate.KindCode,
		})
	}
	return resources, nil
}

func persistRecipeImports(ctx context.Context, tx pgx.Tx, recipes []recipeImportRecipeWrite) error {
	if len(recipes) == 0 {
		return nil
	}
	var err error
	if _, err = execImportStatement(ctx, tx, `create temporary table if not exists recipe_import_stage (
		type_identity text,type_public_id text,recipe_type_id text,recipe_identity text,recipe_public_id text,
		canonical_source_id text,semantic_fingerprint text,identity_source text,revision_id text,snapshot_id text,
		source_recipe_id text,source_recipe_key text,collection_path text,origin_kind text,
		underlying_recipe_type_id text,source_mod_id text,source_mod_version text,source_mod_id_source text,
		render_locale text,template_id text,layout_kind text,ordered boolean,
		layout_classification_source text,width integer,height integer,parameters jsonb,binding_count integer
	) on commit drop`); err != nil {
		return fmt.Errorf("create recipe import stage: %w", err)
	}
	if _, err = execImportStatement(ctx, tx, `truncate recipe_import_stage`); err != nil {
		return fmt.Errorf("truncate recipe import stage: %w", err)
	}
	columns := []string{
		"type_identity", "type_public_id", "recipe_type_id", "recipe_identity", "recipe_public_id",
		"canonical_source_id", "semantic_fingerprint", "identity_source", "revision_id", "snapshot_id",
		"source_recipe_id", "source_recipe_key", "collection_path", "origin_kind", "underlying_recipe_type_id",
		"source_mod_id", "source_mod_version", "source_mod_id_source", "render_locale",
		"template_id", "layout_kind", "ordered", "layout_classification_source", "width", "height",
		"parameters", "binding_count",
	}
	if err = copyImportRows(ctx, tx, "recipe_import_stage", columns, len(recipes), func(index int) ([]any, error) {
		item := recipes[index]
		return []any{
			item.TypeIdentity, item.TypePublicID, item.RecipeTypeID, item.RecipeIdentity, item.RecipePublicID,
			item.CanonicalSourceID, item.SemanticFingerprint, item.IdentitySource, item.RevisionID, item.SnapshotID,
			item.SourceRecipeID, item.SourceRecipeKey, item.CollectionPath, item.OriginKind, item.UnderlyingRecipeTypeID,
			item.SourceModID, item.SourceModVersion, item.SourceModIDSource, item.RenderLocale,
			item.TemplateID, item.LayoutKind, recipeImportOptional(item.Ordered), item.LayoutClassificationSource,
			recipeImportOptional(item.Width), recipeImportOptional(item.Height), recipeImportJSONValue(item.Parameters),
			item.BindingCount,
		}, nil
	}); err != nil {
		return fmt.Errorf("stage recipes: %w", err)
	}
	if _, err = execImportStatement(ctx, tx, `insert into catalog_entities(identity_key,public_id,entity_type,status)
		select distinct on (type_identity) type_identity,type_public_id,'recipe_type','active'
		from recipe_import_stage order by type_identity,type_public_id
		on conflict(identity_key) do update set status='active',archived_at=null,updated_at=now()
		where catalog_entities.status='placeholder'`); err != nil {
		return fmt.Errorf("upsert imported recipe type entities: %w", err)
	}
	if _, err = execImportStatement(ctx, tx, `insert into recipe_types(entity_id,canonical_id)
		select distinct on (stage.recipe_type_id) entity.id,stage.recipe_type_id
		from recipe_import_stage stage
		join catalog_entities entity on entity.identity_key=stage.type_identity
		order by stage.recipe_type_id,stage.type_identity
		on conflict(canonical_id) do nothing`); err != nil {
		return fmt.Errorf("upsert imported recipe types: %w", err)
	}
	if _, err = execImportStatement(ctx, tx, `insert into catalog_entities(identity_key,public_id,entity_type,status)
		select distinct on (recipe_identity) recipe_identity,recipe_public_id,'recipe','active'
		from recipe_import_stage order by recipe_identity,recipe_public_id
		on conflict(identity_key) do update set status='active',archived_at=null,updated_at=now()
		where catalog_entities.status='placeholder'`); err != nil {
		return fmt.Errorf("upsert imported recipe entities: %w", err)
	}
	if _, err = execImportStatement(ctx, tx, `insert into recipes(
			entity_id,recipe_type_id,canonical_source_id,semantic_fingerprint,owner_mod_id,identity_source)
		select distinct on (stage.recipe_identity)
			recipe_entity.id,type_entity.id,
			nullif(stage.canonical_source_id,''),stage.semantic_fingerprint,revision.mod_id,stage.identity_source
		from recipe_import_stage stage
		join catalog_import_revisions revision on revision.id=stage.revision_id
		join catalog_entities recipe_entity on recipe_entity.identity_key=stage.recipe_identity
		join catalog_entities type_entity on type_entity.identity_key=stage.type_identity
		order by stage.recipe_identity,stage.snapshot_id
		on conflict(entity_id) do update set semantic_fingerprint=excluded.semantic_fingerprint`); err != nil {
		return fmt.Errorf("upsert imported recipes: %w", err)
	}
	if _, err = execImportStatement(ctx, tx, `insert into recipe_import_snapshots(
			id,recipe_id,revision_id,source_recipe_id,source_id_kind,source_recipe_key,recipe_collection_path,
			origin_kind,underlying_recipe_type_id,source_mod_id,source_mod_version,source_mod_id_source,
			render_locale,definition_schema_version,template_id,layout_available,layout_kind,ordered,
			layout_classification_source,width,height,parameters,binding_count)
		select stage.snapshot_id,recipe_entity.id,stage.revision_id,stage.source_recipe_id,stage.identity_source,
			stage.source_recipe_key,stage.collection_path,stage.origin_kind,stage.underlying_recipe_type_id,stage.source_mod_id,
			stage.source_mod_version,stage.source_mod_id_source,stage.render_locale,1,stage.template_id,true,stage.layout_kind,
			stage.ordered,stage.layout_classification_source,stage.width,stage.height,stage.parameters,stage.binding_count
		from recipe_import_stage stage
		join catalog_entities recipe_entity on recipe_entity.identity_key=stage.recipe_identity
		on conflict(recipe_id,revision_id,source_recipe_key) do update set
			template_id=excluded.template_id,layout_available=true,layout_kind=excluded.layout_kind,
			ordered=excluded.ordered,layout_classification_source=excluded.layout_classification_source,
			width=excluded.width,height=excluded.height,parameters=excluded.parameters,
			binding_count=excluded.binding_count,origin_kind=excluded.origin_kind,
			underlying_recipe_type_id=excluded.underlying_recipe_type_id,source_mod_id=excluded.source_mod_id,
			source_mod_version=excluded.source_mod_version,source_mod_id_source=excluded.source_mod_id_source,
			render_locale=excluded.render_locale,definition_schema_version=excluded.definition_schema_version`); err != nil {
		return fmt.Errorf("persist imported recipe snapshots: %w", err)
	}
	// A recipe is a version-independent semantic identity. Every successful
	// recipe import, including recipes without ingredient bindings, adds the
	// concrete Minecraft versions of its target content version.
	if _, err = execImportStatement(ctx, tx, `insert into recipe_version_bindings(recipe_id,version_code,created_by,source)
		select distinct snapshot.recipe_id,btrim(version_code),revision.submitted_by,'import'
		from recipe_import_snapshots snapshot
		join catalog_import_revisions revision on revision.id=snapshot.revision_id
		left join mod_content_versions content_version on content_version.id=revision.target_version_id
		cross join lateral unnest(case when cardinality(content_version.minecraft_versions)>0
			then content_version.minecraft_versions else array[revision.minecraft_version] end) version_code
		where snapshot.revision_id in (select distinct revision_id from recipe_import_stage)
		  and btrim(version_code)<>''
		on conflict(recipe_id,version_code) do nothing`); err != nil {
		return fmt.Errorf("persist recipe version bindings: %w", err)
	}
	return nil
}

func persistRecipeImportBindings(
	ctx context.Context,
	tx pgx.Tx,
	bindings []recipeImportBindingWrite,
	candidates []recipeImportCandidateWrite,
	onStage func(string) error,
) error {
	var err error
	if len(bindings) > 0 {
		if onStage != nil {
			if err = onStage("recipe_bindings"); err != nil {
				return err
			}
		}
		if _, err = execImportStatement(ctx, tx, `create temporary table if not exists recipe_binding_import_stage (
			id text,recipe_snapshot_id text,template_slot_id text,source_slot_id text,ordinal integer,
			ingredient_present boolean,clickable boolean,placeholder_item text,item_tag_equivalent text,
			semantic_role text,role_source text,tag_identity text,tag_public_id text,tag_canonical_id text
		) on commit drop`); err != nil {
			return fmt.Errorf("create recipe binding import stage: %w", err)
		}
		if _, err = execImportStatement(ctx, tx, `truncate recipe_binding_import_stage`); err != nil {
			return fmt.Errorf("truncate recipe binding import stage: %w", err)
		}
		columns := []string{
			"id", "recipe_snapshot_id", "template_slot_id", "source_slot_id", "ordinal",
			"ingredient_present", "clickable", "placeholder_item", "item_tag_equivalent", "semantic_role",
			"role_source", "tag_identity", "tag_public_id", "tag_canonical_id",
		}
		if err = copyImportRows(ctx, tx, "recipe_binding_import_stage", columns, len(bindings), func(index int) ([]any, error) {
			item := bindings[index]
			return []any{
				item.ID, item.RecipeSnapshotID, item.TemplateSlotID, item.SourceSlotID, item.Ordinal,
				item.IngredientPresent, item.Clickable, item.PlaceholderItem, item.ItemTagEquivalent, item.SemanticRole,
				item.RoleSource, item.TagIdentity, item.TagPublicID, item.TagCanonicalID,
			}, nil
		}); err != nil {
			return fmt.Errorf("stage recipe bindings: %w", err)
		}
		if _, err = execImportStatement(ctx, tx, `insert into catalog_entities(identity_key,public_id,entity_type,status)
			select distinct on (tag_identity) tag_identity,tag_public_id,'tag','placeholder'
			from recipe_binding_import_stage where tag_identity<>''
			order by tag_identity,tag_public_id
			on conflict(identity_key) do nothing`); err != nil {
			return fmt.Errorf("upsert recipe binding tags: %w", err)
		}
		if _, err = execImportStatement(ctx, tx, `insert into catalog_tags(entity_id,registry,canonical_id)
			select distinct on (stage.tag_canonical_id) tag_entity.id,'minecraft:item',stage.tag_canonical_id
			from recipe_binding_import_stage stage
			join catalog_entities tag_entity on tag_entity.identity_key=stage.tag_identity
			where stage.tag_identity<>'' and stage.tag_canonical_id<>''
			order by stage.tag_canonical_id,stage.tag_identity
			on conflict(registry,canonical_id) do nothing`); err != nil {
			return fmt.Errorf("upsert recipe binding tag identities: %w", err)
		}
		if _, err = execImportStatement(ctx, tx, `insert into recipe_import_bindings(
				id,recipe_snapshot_id,template_slot_id,source_slot_id,ordinal,ingredient_present,clickable,
				placeholder_item,item_tag_equivalent,semantic_role,role_source,tag_id)
			select stage.id,stage.recipe_snapshot_id,stage.template_slot_id,stage.source_slot_id,stage.ordinal,
				stage.ingredient_present,stage.clickable,stage.placeholder_item,stage.item_tag_equivalent,
				stage.semantic_role,stage.role_source,tag_entity.id
			from recipe_binding_import_stage stage
			left join catalog_entities tag_entity
				on stage.tag_identity<>'' and tag_entity.identity_key=stage.tag_identity
			on conflict(recipe_snapshot_id,source_slot_id) do update set
				template_slot_id=excluded.template_slot_id,ordinal=excluded.ordinal,
				ingredient_present=excluded.ingredient_present,clickable=excluded.clickable,
				placeholder_item=excluded.placeholder_item,item_tag_equivalent=excluded.item_tag_equivalent,
				semantic_role=excluded.semantic_role,role_source=excluded.role_source,
				tag_id=excluded.tag_id`); err != nil {
			return fmt.Errorf("persist recipe bindings: %w", err)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	if onStage != nil {
		if err = onStage("recipe_candidates"); err != nil {
			return err
		}
	}
	resources, err := uniqueRecipeImportCandidateResources(candidates)
	if err != nil {
		return err
	}
	if _, err = execImportStatement(ctx, tx, `create temporary table if not exists recipe_candidate_resource_stage (
		resource_identity text primary key,resource_public_id text,resource_canonical_id text,
		resource_namespace text,resource_path text,kind_code text
	) on commit drop`); err != nil {
		return fmt.Errorf("create recipe candidate resource stage: %w", err)
	}
	if _, err = execImportStatement(ctx, tx, `create temporary table if not exists recipe_candidate_import_stage (
		binding_id text,alternative_index integer,resource_identity text,resource_raw_id text,
		amount double precision,ingredient_kind text,ingredient_type text,unique_id text,nbt_snbt text,
		chance_available boolean,chance double precision,chance_percent double precision,chance_comparator text,
		chance_source text,chance_text text,chance_texts jsonb,chance_translation_key text,
		chance_render_x double precision,chance_render_y double precision,byproduct boolean
	) on commit drop`); err != nil {
		return fmt.Errorf("create recipe candidate import stage: %w", err)
	}
	if _, err = execImportStatement(ctx, tx, `truncate recipe_candidate_resource_stage`); err != nil {
		return fmt.Errorf("truncate recipe candidate resource stage: %w", err)
	}
	if _, err = execImportStatement(ctx, tx, `truncate recipe_candidate_import_stage`); err != nil {
		return fmt.Errorf("truncate recipe candidate import stage: %w", err)
	}
	resourceColumns := []string{
		"resource_identity", "resource_public_id", "resource_canonical_id",
		"resource_namespace", "resource_path", "kind_code",
	}
	if err = copyImportRows(ctx, tx, "recipe_candidate_resource_stage", resourceColumns, len(resources), func(index int) ([]any, error) {
		item := resources[index]
		return []any{
			item.Identity, item.PublicID, item.CanonicalID, item.Namespace, item.Path, item.KindCode,
		}, nil
	}); err != nil {
		return fmt.Errorf("stage recipe candidate resources: %w", err)
	}
	columns := []string{
		"binding_id", "alternative_index", "resource_identity", "resource_raw_id",
		"amount", "ingredient_kind", "ingredient_type", "unique_id", "nbt_snbt",
		"chance_available", "chance", "chance_percent", "chance_comparator", "chance_source", "chance_text",
		"chance_texts", "chance_translation_key", "chance_render_x", "chance_render_y", "byproduct",
	}
	if err = copyImportRows(ctx, tx, "recipe_candidate_import_stage", columns, len(candidates), func(index int) ([]any, error) {
		item := candidates[index]
		return []any{
			item.BindingID, item.AlternativeIndex, item.ResourceIdentity, item.ResourceRawID,
			item.Amount, item.IngredientKind, item.IngredientType, item.UniqueID, item.NBTSNBT,
			item.ChanceAvailable, recipeImportOptional(item.Chance), recipeImportOptional(item.ChancePercent),
			item.ChanceComparator, item.ChanceSource, item.ChanceText, recipeImportJSONValue(item.ChanceTexts),
			item.ChanceTranslationKey, recipeImportOptional(item.ChanceRenderX), recipeImportOptional(item.ChanceRenderY),
			item.Byproduct,
		}, nil
	}); err != nil {
		return fmt.Errorf("stage recipe candidates: %w", err)
	}
	if _, err = execImportStatement(ctx, tx, `insert into resource_kinds(code,family,user_visible)
		select distinct kind_code,case when kind_code like 'import.document.%' then 'document' else split_part(kind_code,'.',1) end,true
		from recipe_candidate_resource_stage
		on conflict(code) do nothing`); err != nil {
		return fmt.Errorf("upsert candidate resource kinds: %w", err)
	}
	if _, err = execImportStatement(ctx, tx, `insert into catalog_entities(identity_key,public_id,entity_type,status)
		select distinct on (stage.resource_identity)
			stage.resource_identity,stage.resource_public_id,'resource','placeholder'
		from recipe_candidate_resource_stage stage
		left join game_resources existing
			on existing.kind_code=stage.kind_code and existing.canonical_id=stage.resource_canonical_id
		where existing.entity_id is null
		order by stage.resource_identity,stage.resource_public_id
		on conflict(identity_key) do nothing`); err != nil {
		return fmt.Errorf("upsert candidate resource entities: %w", err)
	}
	if _, err = execImportStatement(ctx, tx, `insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,resolved)
		select distinct on (stage.kind_code,stage.resource_canonical_id)
			coalesce(existing.entity_id,resource_entity.id),stage.kind_code,stage.resource_canonical_id,
			stage.resource_namespace,stage.resource_path,false
		from recipe_candidate_resource_stage stage
		left join game_resources existing
			on existing.kind_code=stage.kind_code and existing.canonical_id=stage.resource_canonical_id
		left join catalog_entities resource_entity on resource_entity.identity_key=stage.resource_identity
		where existing.entity_id is not null or resource_entity.id is not null
		order by stage.kind_code,stage.resource_canonical_id,stage.resource_identity
		on conflict(kind_code,canonical_id) do nothing`); err != nil {
		return fmt.Errorf("upsert candidate resources: %w", err)
	}
	if _, err = execImportStatement(ctx, tx, `insert into game_resource_aliases(kind_code,alias_id,resource_id,source)
		select distinct on (definition.kind_code,stage.resource_raw_id)
			definition.kind_code,stage.resource_raw_id,resource.entity_id,'mod_id'
		from recipe_candidate_import_stage stage
		join recipe_candidate_resource_stage definition
			on definition.resource_identity=stage.resource_identity
		join game_resources resource
			on resource.kind_code=definition.kind_code and resource.canonical_id=definition.resource_canonical_id
		where stage.resource_raw_id<>''
		order by definition.kind_code,stage.resource_raw_id,stage.resource_identity
		on conflict(kind_code,alias_id) do update set
			resource_id=excluded.resource_id,source=excluded.source,updated_at=now()`); err != nil {
		return fmt.Errorf("upsert candidate resource aliases: %w", err)
	}
	if _, err = execImportStatement(ctx, tx, `insert into recipe_import_binding_candidates(
			binding_id,alternative_index,resource_id,raw_resource_id,amount,ingredient_kind,ingredient_type,
			unique_id,nbt_snbt,chance_available,chance,chance_percent,chance_comparator,chance_source,chance_text,
			chance_texts,chance_translation_key,chance_render_x,chance_render_y,byproduct)
		select stage.binding_id,stage.alternative_index,resource_entity.entity_id,stage.resource_raw_id,
			stage.amount,stage.ingredient_kind,stage.ingredient_type,stage.unique_id,stage.nbt_snbt,
			stage.chance_available,stage.chance,stage.chance_percent,stage.chance_comparator,
			stage.chance_source,stage.chance_text,stage.chance_texts,stage.chance_translation_key,
			stage.chance_render_x,stage.chance_render_y,stage.byproduct
		from recipe_candidate_import_stage stage
		join recipe_candidate_resource_stage definition
			on definition.resource_identity=stage.resource_identity
		join game_resources resource_entity
			on resource_entity.kind_code=definition.kind_code and resource_entity.canonical_id=definition.resource_canonical_id
		on conflict(binding_id,alternative_index,raw_resource_id) do update set
			resource_id=excluded.resource_id,amount=excluded.amount,ingredient_kind=excluded.ingredient_kind,
			ingredient_type=excluded.ingredient_type,unique_id=excluded.unique_id,nbt_snbt=excluded.nbt_snbt,
			chance_available=excluded.chance_available,chance=excluded.chance,chance_percent=excluded.chance_percent,
			chance_comparator=excluded.chance_comparator,chance_source=excluded.chance_source,
			chance_text=excluded.chance_text,chance_texts=excluded.chance_texts,
			chance_translation_key=excluded.chance_translation_key,chance_render_x=excluded.chance_render_x,
			chance_render_y=excluded.chance_render_y,byproduct=excluded.byproduct`); err != nil {
		return fmt.Errorf("persist recipe candidates: %w", err)
	}
	if _, err = execImportStatement(ctx, tx, `insert into unresolved_resource_references(
			source_entity_id,source_revision_id,field_path,kind_code,raw_resource_id,
			resolved_resource_id,status,resolved_at)
		select snapshot.recipe_id,snapshot.revision_id,
			'bindings.'||binding.source_slot_id||'.alternatives.'||stage.alternative_index,
			definition.kind_code,
			stage.resource_raw_id,resource_entity.entity_id,'pending',null
		from recipe_candidate_import_stage stage
		join recipe_candidate_resource_stage definition
			on definition.resource_identity=stage.resource_identity
		join recipe_import_bindings binding on binding.id=stage.binding_id
		join recipe_import_snapshots snapshot on snapshot.id=binding.recipe_snapshot_id
		join game_resources resource_entity
			on resource_entity.kind_code=definition.kind_code and resource_entity.canonical_id=definition.resource_canonical_id
		left join (
			select distinct resource_id from resource_import_snapshots
		) known on known.resource_id=resource_entity.entity_id
		where known.resource_id is null
		on conflict(source_entity_id,source_revision_id,field_path,kind_code,raw_resource_id) do update set
			resolved_resource_id=excluded.resolved_resource_id,status='pending',resolved_at=null`); err != nil {
		return fmt.Errorf("persist unresolved recipe resources: %w", err)
	}
	return nil
}
