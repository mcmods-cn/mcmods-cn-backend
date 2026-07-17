package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"

	"archive/zip"

	"github.com/jackc/pgx/v5"
)

const maxExportRecipeParseConcurrency = 6

type exportJEICategoryDocument struct {
	SchemaVersion string                    `json:"schema_version"`
	Categories    []exportJEICategory       `json:"categories"`
	Recipes       []exportJEIRecipeIndexRow `json:"recipes"`
}

type exportJEICategory struct {
	RecipeTypeID        string          `json:"recipe_type_id"`
	TitleTranslationKey string          `json:"title_translation_key"`
	TitleNames          json.RawMessage `json:"title_names"`
	Width               int             `json:"width"`
	Height              int             `json:"height"`
	ImageScale          int             `json:"image_scale"`
	Canvas              json.RawMessage `json:"canvas"`
	Catalysts           json.RawMessage `json:"catalysts"`
	RecipeCount         int             `json:"recipe_count"`
	ExportedRecipeCount int             `json:"exported_recipe_count"`
	TemplateCount       int             `json:"template_count"`
	BackgroundCount     int             `json:"background_count"`
	TemplateCollection  string          `json:"template_collection"`
	RecipeCollection    string          `json:"recipe_collection"`
}

type exportJEIRecipeIndexRow struct {
	RecipeTypeID               string `json:"recipe_type_id"`
	RecipeID                   string `json:"recipe_id"`
	RecipeIDSource             string `json:"recipe_id_source"`
	RecipeIDCanonical          *bool  `json:"recipe_id_canonical"`
	RecipeKey                  string `json:"recipe_key"`
	TemplateID                 string `json:"template_id"`
	RecipeCollection           string `json:"recipe_collection"`
	LayoutAvailable            bool   `json:"layout_available"`
	LayoutKind                 string `json:"layout_kind"`
	LayoutClassificationSource string `json:"layout_classification_source"`
	Ordered                    *bool  `json:"ordered"`
	Width                      *int   `json:"width"`
	Height                     *int   `json:"height"`
	OriginKind                 string `json:"origin_kind"`
	UnderlyingRecipeTypeID     string `json:"underlying_recipe_type_id"`
	SourceModID                string `json:"source_mod_id"`
	SourceModVersion           string `json:"source_mod_version"`
	SourceModIDSource          string `json:"source_mod_id_source"`
}

type exportBaseRecipeDocument struct {
	SchemaVersion string             `json:"schema_version"`
	Count         int                `json:"count"`
	Recipes       []exportBaseRecipe `json:"recipes"`
}

type exportBaseRecipe struct {
	RecipeID                   string `json:"id"`
	RecipeTypeID               string `json:"type"`
	SerializerID               string `json:"serializer"`
	SourceModID                string `json:"source_mod_id"`
	SourceModVersion           string `json:"source_mod_version"`
	SourceModIDSource          string `json:"source_mod_id_source"`
	LayoutKind                 string `json:"layout_kind"`
	LayoutClassificationSource string `json:"layout_classification_source"`
	Ordered                    *bool  `json:"ordered"`
	Width                      *int   `json:"width"`
	Height                     *int   `json:"height"`
}

type exportJEITemplateCollection struct {
	SchemaVersion   string              `json:"schema_version"`
	RecipeTypeID    string              `json:"recipe_type_id"`
	CoordinateSpace string              `json:"coordinate_space"`
	ImageScale      int                 `json:"image_scale"`
	Canvas          json.RawMessage     `json:"canvas"`
	ImagePixels     json.RawMessage     `json:"image_pixels"`
	Content         json.RawMessage     `json:"content"`
	TemplateCount   int                 `json:"template_count"`
	Templates       []exportJEITemplate `json:"templates"`
}

type exportJEITemplate struct {
	SchemaVersion                 string                  `json:"schema_version"`
	TemplateID                    string                  `json:"template_id"`
	Background                    string                  `json:"background"`
	BackgroundContainsIngredients bool                    `json:"background_contains_ingredients"`
	SlotCount                     int                     `json:"slot_count"`
	Slots                         []exportJEITemplateSlot `json:"slots"`
}

type exportJEITemplateSlot struct {
	SlotID               string          `json:"slot_id"`
	Role                 string          `json:"role"`
	JEIRole              string          `json:"jei_role"`
	OutputIndex          *int            `json:"output_index"`
	CoordinatesAvailable bool            `json:"coordinates_available"`
	Rect                 json.RawMessage `json:"rect"`
	VisualRect           json.RawMessage `json:"visual_rect"`
}

type exportJEIRecipeCollection struct {
	SchemaVersion      string                   `json:"schema_version"`
	RecipeTypeID       string                   `json:"recipe_type_id"`
	TemplateCollection string                   `json:"template_collection"`
	Count              int                      `json:"count"`
	Recipes            []exportJEIRecipeBinding `json:"recipes"`
}

type exportJEIRecipeBinding struct {
	SchemaVersion              string             `json:"schema_version"`
	RecipeTypeID               string             `json:"recipe_type_id"`
	RecipeID                   string             `json:"recipe_id"`
	RecipeIDSource             string             `json:"recipe_id_source"`
	RecipeIDCanonical          *bool              `json:"recipe_id_canonical"`
	RecipeKey                  string             `json:"recipe_key"`
	TemplateID                 string             `json:"template_id"`
	LayoutKind                 string             `json:"layout_kind"`
	Ordered                    *bool              `json:"ordered"`
	LayoutClassificationSource string             `json:"layout_classification_source"`
	Width                      *int               `json:"width"`
	Height                     *int               `json:"height"`
	Parameters                 json.RawMessage    `json:"parameters"`
	BindingCount               int                `json:"binding_count"`
	Bindings                   []exportJEIBinding `json:"bindings"`
	OriginKind                 string             `json:"origin_kind"`
	UnderlyingRecipeTypeID     string             `json:"underlying_recipe_type_id"`
	SourceModID                string             `json:"source_mod_id"`
	SourceModVersion           string             `json:"source_mod_version"`
	SourceModIDSource          string             `json:"source_mod_id_source"`
	RenderLocale               string             `json:"render_locale"`
	Raw                        json.RawMessage    `json:"-"`
}

type exportJEIBinding struct {
	SlotID               string           `json:"slot_id"`
	IngredientPresent    bool             `json:"ingredient_present"`
	Clickable            bool             `json:"clickable"`
	PlaceholderItem      string           `json:"placeholder_item"`
	ItemTagEquivalent    string           `json:"item_tag_equivalent"`
	SemanticRole         string           `json:"semantic_role"`
	RoleSource           string           `json:"role_source"`
	ChanceAvailable      bool             `json:"chance_available"`
	Chance               *float64         `json:"chance"`
	ChancePercent        *float64         `json:"chance_percent"`
	ChanceComparator     string           `json:"chance_comparator"`
	ChanceSource         string           `json:"chance_source"`
	ChanceText           string           `json:"chance_text"`
	ChanceTexts          json.RawMessage  `json:"chance_texts"`
	ChanceTranslationKey string           `json:"chance_translation_key"`
	ChanceRenderX        *float64         `json:"chance_render_x"`
	ChanceRenderY        *float64         `json:"chance_render_y"`
	Byproduct            bool             `json:"byproduct"`
	Alternatives         []map[string]any `json:"alternatives"`
	Raw                  json.RawMessage  `json:"-"`
}

func (binding *exportJEIRecipeBinding) UnmarshalJSON(data []byte) error {
	type bindingAlias exportJEIRecipeBinding
	var decoded bindingAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*binding = exportJEIRecipeBinding(decoded)
	binding.Raw = append(binding.Raw[:0], data...)
	return nil
}

func (binding *exportJEIBinding) UnmarshalJSON(data []byte) error {
	type bindingAlias exportJEIBinding
	var decoded bindingAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*binding = exportJEIBinding(decoded)
	binding.Raw = append(binding.Raw[:0], data...)
	return nil
}

func importExportRecipeTypes(ctx context.Context, tx pgx.Tx, packageID string, revisions map[string]string, raw []byte) error {
	var document exportJEICategoryDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return fmt.Errorf("decode recipes/jei/categories.json: %w", err)
	}
	if document.SchemaVersion != "mcmods-jei-categories/v6" {
		return fmt.Errorf("unsupported JEI category schema: %s", document.SchemaVersion)
	}
	batch := &pgx.Batch{}
	queued := 0
	seenCategories := make(map[string]struct{}, len(document.Categories))
	for _, category := range document.Categories {
		category.RecipeTypeID = strings.TrimSpace(category.RecipeTypeID)
		if category.RecipeTypeID == "" {
			return errors.New("JEI category has no recipe_type_id")
		}
		if _, exists := seenCategories[category.RecipeTypeID]; exists {
			return fmt.Errorf("duplicate JEI category %s", category.RecipeTypeID)
		}
		seenCategories[category.RecipeTypeID] = struct{}{}
		if category.TemplateCount != category.BackgroundCount {
			return fmt.Errorf("JEI category %s declares %d templates but %d backgrounds", category.RecipeTypeID, category.TemplateCount, category.BackgroundCount)
		}
		if category.ExportedRecipeCount > category.RecipeCount {
			return fmt.Errorf("JEI category %s exported recipe count exceeds discovered count", category.RecipeTypeID)
		}
		if !validExportCollectionPath(category.TemplateCollection, "recipes/jei/templates/") ||
			!validExportCollectionPath(category.RecipeCollection, "recipes/jei/recipes/") {
			return fmt.Errorf("JEI category %s contains an invalid collection path", category.RecipeTypeID)
		}
		revisionID := exportRevisionForRecipeType(revisions, category.RecipeTypeID)
		if revisionID == "" {
			continue
		}
		identity := recipeTypeIdentity(category.RecipeTypeID)
		snapshotID := catalogSnapshotID("recipe-type", revisionID, identity.ID, "")
		batch.Queue(`insert into catalog_entities(id,public_id,entity_type,status) values($1,$2,'recipe_type','active')
			on conflict(id) do update set status='active',updated_at=now()`, identity.ID, identity.PublicID)
		batch.Queue(`insert into recipe_types(entity_id,canonical_id) values($1,$2) on conflict(canonical_id) do nothing`, identity.ID, category.RecipeTypeID)
		batch.Queue(`insert into recipe_type_snapshots(id,recipe_type_id,revision_id,title_translation_key,title_names,width,height,
			image_scale,canvas,catalysts,recipe_count,exported_recipe_count,template_count,background_count,
			template_collection_path,recipe_collection_path)
			values($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9::jsonb,$10::jsonb,$11,$12,$13,$14,$15,$16)
			on conflict(recipe_type_id,revision_id) do update set title_translation_key=excluded.title_translation_key,
			title_names=excluded.title_names,width=excluded.width,height=excluded.height,image_scale=excluded.image_scale,
			canvas=excluded.canvas,catalysts=excluded.catalysts,recipe_count=excluded.recipe_count,
			exported_recipe_count=excluded.exported_recipe_count,template_count=excluded.template_count,
			background_count=excluded.background_count,template_collection_path=excluded.template_collection_path,
			recipe_collection_path=excluded.recipe_collection_path`,
			snapshotID, identity.ID, revisionID, category.TitleTranslationKey, nonEmptyJSON(category.TitleNames, `{}`),
			category.Width, category.Height, max(1, category.ImageScale), nonEmptyJSON(category.Canvas, `{}`),
			nonEmptyJSON(category.Catalysts, `[]`), category.RecipeCount, category.ExportedRecipeCount,
			category.TemplateCount, category.BackgroundCount, category.TemplateCollection, category.RecipeCollection)
		queued += 3
	}
	for _, recipeIndex := range document.Recipes {
		if err := validateExportRecipeSource(recipeIndex.OriginKind, recipeIndex.SourceModID, recipeIndex.SourceModVersion, recipeIndex.SourceModIDSource); err != nil {
			return fmt.Errorf("JEI recipe index %q: %w", recipeIndex.RecipeKey, err)
		}
		if err := validateExportRecipeLayoutKind(recipeIndex.LayoutKind, recipeIndex.Ordered); err != nil {
			return fmt.Errorf("JEI recipe index %q: %w", recipeIndex.RecipeKey, err)
		}
		if recipeIndex.LayoutAvailable {
			if strings.TrimSpace(recipeIndex.TemplateID) == "" {
				return fmt.Errorf("JEI recipe index %q has a layout but no template_id", recipeIndex.RecipeKey)
			}
			continue
		}
		revisionID := exportRevisionForRecipeType(revisions, recipeIndex.RecipeTypeID)
		if revisionID == "" {
			continue
		}
		canonical, err := validateExportRecipeIdentity(recipeIndex.RecipeID, recipeIndex.RecipeIDSource, recipeIndex.RecipeIDCanonical)
		if err != nil {
			return fmt.Errorf("JEI recipe index %q: %w", recipeIndex.RecipeKey, err)
		}
		recipeKey := strings.TrimSpace(recipeIndex.RecipeKey)
		if recipeKey == "" {
			recipeKey = strings.TrimSpace(recipeIndex.RecipeID)
		}
		typeIdentity := recipeTypeIdentity(recipeIndex.RecipeTypeID)
		recipeIdentityValue := recipeIdentity(packageID, recipeIndex.RecipeTypeID, recipeIndex.RecipeID, canonical)
		snapshotID := catalogSnapshotID("recipe", revisionID, recipeIdentityValue.ID, recipeKey)
		fingerprintRaw, _ := json.Marshal(recipeIndex)
		var canonicalSourceID any
		if canonical {
			canonicalSourceID = strings.TrimSpace(recipeIndex.RecipeID)
		}
		batch.Queue(`insert into catalog_entities(id,public_id,entity_type,status) values($1,$2,'recipe','active')
			on conflict(id) do update set status='active',updated_at=now()`, recipeIdentityValue.ID, recipeIdentityValue.PublicID)
		batch.Queue(`insert into recipes(entity_id,recipe_type_id,canonical_source_id,semantic_fingerprint,owner_mod_id,identity_source)
			select $1,$2,$3,$4,revision.mod_id,$5 from mod_export_revisions revision where revision.id=$6
			on conflict(entity_id) do update set semantic_fingerprint=excluded.semantic_fingerprint`, recipeIdentityValue.ID,
			typeIdentity.ID, canonicalSourceID, sha256Hex(fingerprintRaw), recipeIndex.RecipeIDSource, revisionID)
		batch.Queue(`insert into recipe_snapshots(id,recipe_id,revision_id,source_recipe_id,source_id_kind,source_recipe_key,
			recipe_collection_path,origin_kind,underlying_recipe_type_id,source_mod_id,source_mod_version,source_mod_id_source,
			render_locale,source_data,template_id,layout_available,layout_kind,ordered,layout_classification_source,width,height,parameters,binding_count)
			values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'',$13::jsonb,null,false,$14,$15,$16,$17,$18,'{}'::jsonb,0)
			on conflict(recipe_id,revision_id,source_recipe_key) do update set layout_available=false,layout_kind=excluded.layout_kind,
			ordered=excluded.ordered,layout_classification_source=excluded.layout_classification_source,
			origin_kind=excluded.origin_kind,underlying_recipe_type_id=excluded.underlying_recipe_type_id,
			source_mod_id=excluded.source_mod_id,source_mod_version=excluded.source_mod_version,
			source_mod_id_source=excluded.source_mod_id_source,source_data=excluded.source_data`, snapshotID,
			recipeIdentityValue.ID, revisionID, recipeIndex.RecipeID, recipeIndex.RecipeIDSource, recipeKey,
			recipeIndex.RecipeCollection, recipeIndex.OriginKind, recipeIndex.UnderlyingRecipeTypeID,
			recipeIndex.SourceModID, recipeIndex.SourceModVersion, recipeIndex.SourceModIDSource, string(fingerprintRaw),
			recipeIndex.LayoutKind, nullableRecipeOrdered(recipeIndex.Ordered),
			recipeIndex.LayoutClassificationSource, recipeIndex.Width, recipeIndex.Height)
		queued += 3
	}
	if queued == 0 {
		return nil
	}
	results := tx.SendBatch(ctx, batch)
	for index := 0; index < queued; index++ {
		if _, err := results.Exec(); err != nil {
			_ = results.Close()
			return fmt.Errorf("persist JEI category index: %w", err)
		}
	}
	return results.Close()
}

func decodeExportJEITemplateCollection(raw []byte) (exportJEITemplateCollection, error) {
	var document exportJEITemplateCollection
	if err := json.Unmarshal(raw, &document); err != nil {
		return document, err
	}
	if document.SchemaVersion != "mcmods-jei-template-collection/v2" {
		return document, fmt.Errorf("unsupported JEI template collection schema: %s", document.SchemaVersion)
	}
	if strings.TrimSpace(document.RecipeTypeID) == "" {
		return document, fmt.Errorf("JEI template collection has no recipe_type_id")
	}
	if document.CoordinateSpace != "logical_pixels" || document.ImageScale < 1 {
		return document, fmt.Errorf("JEI template collection %s has invalid coordinate metadata", document.RecipeTypeID)
	}
	if document.TemplateCount != len(document.Templates) {
		return document, fmt.Errorf("JEI template count mismatch: declared %d, found %d", document.TemplateCount, len(document.Templates))
	}
	return document, nil
}

func queueExportJEITemplateCollection(batch *modExportWriteBatch, revisions map[string]string, name string, document exportJEITemplateCollection) error {
	recipeTypeID := strings.TrimSpace(document.RecipeTypeID)
	revisionID := exportRevisionForRecipeType(revisions, recipeTypeID)
	if revisionID == "" {
		return nil
	}
	typeIdentity := recipeTypeIdentity(recipeTypeID)
	typeSnapshotID := catalogSnapshotID("recipe-type", revisionID, typeIdentity.ID, "")
	seenTemplates := make(map[string]struct{}, len(document.Templates))
	seenBackgrounds := make(map[string]struct{}, len(document.Templates))
	for _, template := range document.Templates {
		template.TemplateID = strings.TrimSpace(template.TemplateID)
		if template.SchemaVersion != "mcmods-jei-layout-template/v2" || template.TemplateID == "" {
			return fmt.Errorf("invalid JEI template in %s", name)
		}
		if _, exists := seenTemplates[template.TemplateID]; exists {
			return fmt.Errorf("duplicate JEI template %s in %s", template.TemplateID, name)
		}
		seenTemplates[template.TemplateID] = struct{}{}
		if template.SlotCount != len(template.Slots) {
			return fmt.Errorf("JEI template %s slot count mismatch", template.TemplateID)
		}
		if !validExportPath(template.Background, "recipes/jei/backgrounds/", ".png") {
			return fmt.Errorf("JEI template %s has an invalid background path", template.TemplateID)
		}
		if _, exists := seenBackgrounds[template.Background]; exists {
			return fmt.Errorf("JEI templates in %s reuse background %s", name, template.Background)
		}
		seenBackgrounds[template.Background] = struct{}{}
		templateID := exportRecipeTemplateID(revisionID, recipeTypeID, template.TemplateID)
		batch.queue(`insert into recipe_layout_templates(id,recipe_type_snapshot_id,revision_id,recipe_type_id,source_template_id,
			schema_version,template_collection_path,background_path,background_contains_ingredients,coordinate_space,image_scale,
			canvas,image_pixels,content_rect,slot_count)
			values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13::jsonb,$14::jsonb,$15)
			on conflict(recipe_type_snapshot_id,source_template_id) do update set background_path=excluded.background_path,
			background_contains_ingredients=excluded.background_contains_ingredients,coordinate_space=excluded.coordinate_space,
			image_scale=excluded.image_scale,canvas=excluded.canvas,image_pixels=excluded.image_pixels,
			content_rect=excluded.content_rect,slot_count=excluded.slot_count`, int64(len(document.Canvas)+len(document.ImagePixels)+len(document.Content)),
			templateID, typeSnapshotID, revisionID, typeIdentity.ID, template.TemplateID, template.SchemaVersion, name,
			template.Background, template.BackgroundContainsIngredients, document.CoordinateSpace, max(1, document.ImageScale),
			nonEmptyJSON(document.Canvas, `{}`), nonEmptyJSON(document.ImagePixels, `{}`), nonEmptyJSON(document.Content, `{}`), len(template.Slots))
		seenSlots := make(map[string]struct{}, len(template.Slots))
		for ordinal, slot := range template.Slots {
			slot.SlotID = strings.TrimSpace(slot.SlotID)
			slot.Role = strings.TrimSpace(slot.Role)
			if slot.SlotID == "" || slot.Role == "" {
				return fmt.Errorf("JEI template %s contains an unnamed slot", template.TemplateID)
			}
			if _, exists := seenSlots[slot.SlotID]; exists {
				return fmt.Errorf("JEI template %s contains duplicate slot %s", template.TemplateID, slot.SlotID)
			}
			seenSlots[slot.SlotID] = struct{}{}
			slotID := exportRecipeTemplateSlotID(templateID, slot.SlotID)
			slotData, _ := json.Marshal(slot)
			batch.queue(`insert into recipe_template_slots(id,template_id,source_slot_id,role,jei_role,output_index,ordinal,coordinates_available,rect,visual_rect,data)
				values($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10::jsonb,$11::jsonb)
				on conflict(template_id,source_slot_id) do update set role=excluded.role,jei_role=excluded.jei_role,
				output_index=excluded.output_index,ordinal=excluded.ordinal,
				coordinates_available=excluded.coordinates_available,rect=excluded.rect,visual_rect=excluded.visual_rect,data=excluded.data`,
				int64(len(slotData)), slotID, templateID, slot.SlotID, slot.Role, slot.JEIRole, slot.OutputIndex, ordinal, slot.CoordinatesAvailable,
				nonEmptyJSON(slot.Rect, `{}`), nonEmptyJSON(slot.VisualRect, `{}`), string(slotData))
		}
	}
	return nil
}

func decodeExportJEIRecipeCollection(raw []byte) (exportJEIRecipeCollection, error) {
	var document exportJEIRecipeCollection
	if err := json.Unmarshal(raw, &document); err != nil {
		return document, err
	}
	if document.SchemaVersion != "mcmods-jei-recipe-collection/v2" {
		return document, fmt.Errorf("unsupported JEI recipe collection schema: %s", document.SchemaVersion)
	}
	if strings.TrimSpace(document.RecipeTypeID) == "" {
		return document, fmt.Errorf("JEI recipe collection has no recipe_type_id")
	}
	if !validExportCollectionPath(document.TemplateCollection, "recipes/jei/templates/") {
		return document, fmt.Errorf("JEI recipe collection %s has an invalid template collection path", document.RecipeTypeID)
	}
	if document.Count != len(document.Recipes) {
		return document, fmt.Errorf("JEI recipe count mismatch: declared %d, found %d", document.Count, len(document.Recipes))
	}
	return document, nil
}

func queueExportJEIRecipeCollection(batch *modExportWriteBatch, packageID string, revisions map[string]string, name string, document exportJEIRecipeCollection) error {
	recipeTypeID := strings.TrimSpace(document.RecipeTypeID)
	revisionID := exportRevisionForRecipeType(revisions, recipeTypeID)
	if revisionID == "" {
		return nil
	}
	typeIdentity := recipeTypeIdentity(recipeTypeID)
	for _, recipeBinding := range document.Recipes {
		if strings.TrimSpace(recipeBinding.RecipeTypeID) == "" {
			recipeBinding.RecipeTypeID = recipeTypeID
		}
		if strings.TrimSpace(recipeBinding.RecipeTypeID) != recipeTypeID || recipeBinding.SchemaVersion != "mcmods-jei-recipe-bindings/v2" {
			return fmt.Errorf("invalid JEI recipe binding in %s", name)
		}
		if err := validateExportRecipeSource(recipeBinding.OriginKind, recipeBinding.SourceModID, recipeBinding.SourceModVersion, recipeBinding.SourceModIDSource); err != nil {
			return fmt.Errorf("JEI recipe %q: %w", recipeBinding.RecipeKey, err)
		}
		canonical, err := validateExportRecipeIdentity(recipeBinding.RecipeID, recipeBinding.RecipeIDSource, recipeBinding.RecipeIDCanonical)
		if err != nil {
			return fmt.Errorf("JEI recipe %q: %w", recipeBinding.RecipeKey, err)
		}
		if err = validateExportRecipeLayoutKind(recipeBinding.LayoutKind, recipeBinding.Ordered); err != nil {
			return fmt.Errorf("JEI recipe %q: %w", recipeBinding.RecipeKey, err)
		}
		recipeBinding.TemplateID = strings.TrimSpace(recipeBinding.TemplateID)
		if recipeBinding.TemplateID == "" {
			return fmt.Errorf("JEI recipe %q has no template_id", recipeBinding.RecipeKey)
		}
		if recipeBinding.BindingCount != len(recipeBinding.Bindings) {
			return fmt.Errorf("JEI recipe %q binding count mismatch", recipeBinding.RecipeKey)
		}
		recipeKey := strings.TrimSpace(recipeBinding.RecipeKey)
		if recipeKey == "" {
			recipeKey = strings.TrimSpace(recipeBinding.RecipeID)
		}
		recipeIdentityValue := recipeIdentity(packageID, recipeTypeID, strings.TrimSpace(recipeBinding.RecipeID), canonical)
		recipeSnapshotID := catalogSnapshotID("recipe", revisionID, recipeIdentityValue.ID, recipeKey)
		templateID := exportRecipeTemplateID(revisionID, recipeTypeID, recipeBinding.TemplateID)
		fingerprintRaw, err := canonicalExportRecipeBinding(recipeBinding)
		if err != nil {
			return err
		}
		var canonicalSourceID any
		if canonical {
			canonicalSourceID = strings.TrimSpace(recipeBinding.RecipeID)
		}
		batch.queue(`insert into catalog_entities(id,public_id,entity_type,status) values($1,$2,'recipe_type','active') on conflict(id) do nothing`, 0, typeIdentity.ID, typeIdentity.PublicID)
		batch.queue(`insert into recipe_types(entity_id,canonical_id) values($1,$2) on conflict(canonical_id) do nothing`, 0, typeIdentity.ID, recipeTypeID)
		batch.queue(`insert into catalog_entities(id,public_id,entity_type,status) values($1,$2,'recipe','active')
			on conflict(id) do update set status='active',updated_at=now()`, 0, recipeIdentityValue.ID, recipeIdentityValue.PublicID)
		batch.queue(`insert into recipes(entity_id,recipe_type_id,canonical_source_id,semantic_fingerprint,owner_mod_id,identity_source)
			select $1,$2,$3,$4,revision.mod_id,$5 from mod_export_revisions revision where revision.id=$6
			on conflict(entity_id) do update set semantic_fingerprint=excluded.semantic_fingerprint`, 0, recipeIdentityValue.ID,
			typeIdentity.ID, canonicalSourceID, sha256Hex(fingerprintRaw), recipeBinding.RecipeIDSource, revisionID)
		batch.queue(`insert into recipe_snapshots(id,recipe_id,revision_id,source_recipe_id,source_id_kind,source_recipe_key,
			recipe_collection_path,origin_kind,underlying_recipe_type_id,source_mod_id,source_mod_version,source_mod_id_source,
			render_locale,source_data,template_id,layout_available,layout_kind,ordered,layout_classification_source,width,height,parameters,binding_count)
			values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::jsonb,$15,true,$16,$17,$18,$19,$20,$21::jsonb,$22)
			on conflict(recipe_id,revision_id,source_recipe_key) do update set template_id=excluded.template_id,layout_available=true,
			layout_kind=excluded.layout_kind,ordered=excluded.ordered,layout_classification_source=excluded.layout_classification_source,
			width=excluded.width,height=excluded.height,parameters=excluded.parameters,binding_count=excluded.binding_count,
			origin_kind=excluded.origin_kind,underlying_recipe_type_id=excluded.underlying_recipe_type_id,
			source_mod_id=excluded.source_mod_id,source_mod_version=excluded.source_mod_version,
			source_mod_id_source=excluded.source_mod_id_source,render_locale=excluded.render_locale,source_data=excluded.source_data`,
			int64(len(recipeBinding.Raw)+len(recipeBinding.Parameters)), recipeSnapshotID, recipeIdentityValue.ID, revisionID, recipeBinding.RecipeID,
			recipeBinding.RecipeIDSource, recipeKey, name, recipeBinding.OriginKind, recipeBinding.UnderlyingRecipeTypeID,
			recipeBinding.SourceModID, recipeBinding.SourceModVersion, recipeBinding.SourceModIDSource,
			recipeBinding.RenderLocale, nonEmptyJSON(recipeBinding.Raw, `{}`), templateID, recipeBinding.LayoutKind,
			nullableRecipeOrdered(recipeBinding.Ordered), recipeBinding.LayoutClassificationSource, recipeBinding.Width,
			recipeBinding.Height, nonEmptyJSON(recipeBinding.Parameters, `{}`), len(recipeBinding.Bindings))
		if err = queueExportRecipeBindings(batch, revisionID, recipeIdentityValue.ID, recipeSnapshotID, templateID, recipeBinding.Bindings); err != nil {
			return fmt.Errorf("JEI recipe %q: %w", recipeKey, err)
		}
	}
	return nil
}

func queueExportRecipeBindings(batch *modExportWriteBatch, revisionID, recipeID, recipeSnapshotID, templateID string, bindings []exportJEIBinding) error {
	seen := make(map[string]struct{}, len(bindings))
	for ordinal, binding := range bindings {
		binding.SlotID = strings.TrimSpace(binding.SlotID)
		if binding.SlotID == "" {
			return fmt.Errorf("binding has no slot_id")
		}
		if _, exists := seen[binding.SlotID]; exists {
			return fmt.Errorf("duplicate binding for slot %s", binding.SlotID)
		}
		seen[binding.SlotID] = struct{}{}
		if !binding.IngredientPresent && len(binding.Alternatives) > 0 {
			return fmt.Errorf("empty slot %s contains alternatives", binding.SlotID)
		}
		if !binding.IngredientPresent && (binding.Clickable || strings.TrimSpace(binding.PlaceholderItem) != "minecraft:air") {
			return fmt.Errorf("empty slot %s must be non-clickable and use minecraft:air", binding.SlotID)
		}
		if err := validateExportRecipeChance(binding); err != nil {
			return fmt.Errorf("slot %s: %w", binding.SlotID, err)
		}
		templateSlotID := exportRecipeTemplateSlotID(templateID, binding.SlotID)
		bindingID := catalogSnapshotID("recipe-binding", recipeSnapshotID, templateSlotID, binding.SlotID)
		tagID := strings.TrimSpace(binding.ItemTagEquivalent)
		var tagEntityID any
		if tagID != "" {
			tag := tagIdentity("minecraft:item", tagID)
			tagEntityID = tag.ID
			batch.queue(`insert into catalog_entities(id,public_id,entity_type,status) values($1,$2,'tag','placeholder') on conflict(id) do nothing`, 0, tag.ID, tag.PublicID)
			batch.queue(`insert into catalog_tags(entity_id,registry,canonical_id) values($1,'minecraft:item',$2) on conflict(registry,canonical_id) do nothing`, 0, tag.ID, tagID)
		}
		bindingData := binding.Raw
		if len(bindingData) == 0 {
			bindingData, _ = json.Marshal(binding)
		}
		byproduct := binding.Byproduct || strings.EqualFold(strings.TrimSpace(binding.SemanticRole), "byproduct")
		batch.queue(`insert into recipe_bindings(id,recipe_snapshot_id,template_slot_id,source_slot_id,ordinal,ingredient_present,
			clickable,placeholder_item,item_tag_equivalent,semantic_role,role_source,chance_available,chance,chance_percent,
			chance_comparator,chance_source,chance_text,chance_texts,chance_translation_key,chance_render_x,chance_render_y,
			byproduct,tag_id,data)
			values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18::jsonb,$19,$20,$21,$22,$23,$24::jsonb)
			on conflict(recipe_snapshot_id,source_slot_id) do update set template_slot_id=excluded.template_slot_id,
			ordinal=excluded.ordinal,ingredient_present=excluded.ingredient_present,clickable=excluded.clickable,
			placeholder_item=excluded.placeholder_item,item_tag_equivalent=excluded.item_tag_equivalent,
			semantic_role=excluded.semantic_role,role_source=excluded.role_source,chance_available=excluded.chance_available,
			chance=excluded.chance,chance_percent=excluded.chance_percent,chance_comparator=excluded.chance_comparator,
			chance_source=excluded.chance_source,chance_text=excluded.chance_text,chance_texts=excluded.chance_texts,
			chance_translation_key=excluded.chance_translation_key,chance_render_x=excluded.chance_render_x,
			chance_render_y=excluded.chance_render_y,byproduct=excluded.byproduct,tag_id=excluded.tag_id,data=excluded.data`,
			int64(len(bindingData)), bindingID, recipeSnapshotID, templateSlotID, binding.SlotID, ordinal,
			binding.IngredientPresent, binding.Clickable, binding.PlaceholderItem, tagID, binding.SemanticRole,
			binding.RoleSource, binding.ChanceAvailable, binding.Chance, binding.ChancePercent, binding.ChanceComparator,
			binding.ChanceSource, binding.ChanceText, nonEmptyJSON(binding.ChanceTexts, `{}`), binding.ChanceTranslationKey,
			binding.ChanceRenderX, binding.ChanceRenderY, byproduct, tagEntityID, string(bindingData))
		for alternativeIndex, alternative := range binding.Alternatives {
			ingredientType := strings.TrimSpace(exportString(alternative["type"]))
			ingredientKind := exportIngredientKind(ingredientType)
			resourceID := strings.TrimSpace(exportString(alternative["item"]))
			if resourceID == "" {
				resourceID = strings.TrimSpace(exportString(alternative["resource_location"]))
			}
			if resourceID == "" || resourceID == "minecraft:air" {
				continue
			}
			kindCode := resourceKindForIngredient(ingredientKind, ingredientType)
			resource := resourceIdentity(kindCode, resourceID)
			namespace, resourcePath := resourceParts(resourceID)
			uniqueID := strings.TrimSpace(exportString(alternative["unique_id"]))
			nbtSNBT := strings.TrimSpace(exportString(alternative["nbt_snbt"]))
			alternativeData, _ := json.Marshal(alternative)
			batch.queue(`insert into resource_kinds(code,family,user_visible) values($1,split_part($1,'.',1),true) on conflict(code) do nothing`, 0, kindCode)
			batch.queue(`insert into catalog_entities(id,public_id,entity_type,status) values($1,$2,'resource','placeholder') on conflict(id) do nothing`, 0, resource.ID, resource.PublicID)
			batch.queue(`insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,resolved)
				values($1,$2,$3,$4,$5,false) on conflict(kind_code,canonical_id) do nothing`, 0, resource.ID, kindCode, resourceID, namespace, resourcePath)
			alternativeID := catalogSnapshotID("recipe-alternative", bindingID, resource.ID, fmt.Sprintf("%d", alternativeIndex))
			batch.queue(`insert into recipe_binding_alternatives(id,binding_id,alternative_index,resource_id,raw_resource_id,amount,
				ingredient_kind,ingredient_type,unique_id,nbt_snbt,data)
				values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb)
				on conflict(binding_id,alternative_index,raw_resource_id) do update set resource_id=excluded.resource_id,
				amount=excluded.amount,ingredient_kind=excluded.ingredient_kind,ingredient_type=excluded.ingredient_type,
				unique_id=excluded.unique_id,nbt_snbt=excluded.nbt_snbt,data=excluded.data`, int64(len(alternativeData)),
				alternativeID, bindingID, alternativeIndex, resource.ID, resourceID, exportRecipeAmount(alternative),
				ingredientKind, ingredientType, uniqueID, nbtSNBT, string(alternativeData))
			unresolvedID := catalogSnapshotID("reference", revisionID, recipeID, alternativeID)
			batch.queue(`insert into unresolved_resource_references(id,source_entity_id,source_revision_id,field_path,kind_code,
				raw_resource_id,resolved_resource_id,status,resolved_at)
				select $1,$2,$3,$4,$5,$6,$7,case when exists(select 1 from game_resource_snapshots where resource_id=$7) then 'resolved' else 'pending' end,
				case when exists(select 1 from game_resource_snapshots where resource_id=$7) then now() else null end
				on conflict(source_entity_id,source_revision_id,field_path,kind_code,raw_resource_id) do update set
				resolved_resource_id=excluded.resolved_resource_id,status=excluded.status,resolved_at=excluded.resolved_at`, 0,
				unresolvedID, recipeID, revisionID, fmt.Sprintf("bindings.%s.alternatives.%d", binding.SlotID, alternativeIndex),
				kindCode, resourceID, resource.ID)
		}
	}
	return nil
}

func processExportRecipeJSONFiles[T any](ctx context.Context, files map[string]*zip.File, prefix string,
	decode func([]byte) (T, error), consume func(string, T) error) error {
	names := make([]string, 0)
	for name, file := range files {
		if !file.FileInfo().IsDir() && strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".json") {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	type result struct {
		name  string
		value T
		err   error
	}
	workerCount := min(len(names), min(maxExportRecipeParseConcurrency, max(1, runtime.GOMAXPROCS(0))))
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan string)
	results := make(chan result, workerCount)
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
		go func() {
			defer workers.Done()
			for name := range jobs {
				raw, err := readExportZIPFile(files[name], maxExportJSONSize)
				var value T
				if err == nil {
					value, err = decode(raw)
				}
				select {
				case results <- result{name: name, value: value, err: err}:
				case <-workCtx.Done():
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, name := range names {
			select {
			case jobs <- name:
			case <-workCtx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()
	var firstErr error
	for parsed := range results {
		if firstErr != nil {
			continue
		}
		if parsed.err != nil {
			firstErr = fmt.Errorf("decode %s: %w", parsed.name, parsed.err)
			cancel()
			continue
		}
		if err := consume(parsed.name, parsed.value); err != nil {
			firstErr = err
			cancel()
		}
	}
	return firstErr
}

func exportRevisionForRecipeType(revisions map[string]string, recipeTypeID string) string {
	revisionID := revisions[exportResourceNamespace(strings.TrimSpace(recipeTypeID))]
	if revisionID == "" && len(revisions) == 1 {
		for _, revisionID = range revisions {
			break
		}
	}
	return revisionID
}

func exportRecipeTemplateID(revisionID, recipeTypeID, sourceTemplateID string) string {
	return catalogSnapshotID("recipe-template", revisionID, recipeTypeIdentity(recipeTypeID).ID, sourceTemplateID)
}

func exportRecipeTemplateSlotID(templateID, sourceSlotID string) string {
	return catalogSnapshotID("recipe-template-slot", templateID, sourceSlotID, "")
}

func validExportCollectionPath(value, prefix string) bool {
	return validExportPath(value, prefix, ".json")
}

func validExportPath(value, prefix, suffix string) bool {
	cleaned, err := normalizeExportPath(strings.TrimSpace(value))
	return err == nil && cleaned == value && strings.HasPrefix(cleaned, prefix) && strings.HasSuffix(cleaned, suffix)
}

func validateExportRecipeIdentity(recipeID, source string, canonical *bool) (bool, error) {
	recipeID = strings.TrimSpace(recipeID)
	source = strings.TrimSpace(source)
	if recipeID == "" || source == "" || canonical == nil {
		return false, fmt.Errorf("incomplete recipe identity")
	}
	switch source {
	case "minecraft_recipe", "jei_category":
		if !*canonical {
			return false, fmt.Errorf("authoritative recipe ID %q is marked non-canonical", recipeID)
		}
	case "generated_index":
		if *canonical {
			return false, fmt.Errorf("generated recipe ID %q is marked canonical", recipeID)
		}
	default:
		return false, fmt.Errorf("unsupported recipe ID source %q", source)
	}
	return *canonical, nil
}

func validateExportRecipeLayoutKind(kind string, ordered *bool) error {
	switch strings.TrimSpace(kind) {
	case "shaped":
		if ordered == nil || !*ordered {
			return fmt.Errorf("shaped recipe must be ordered")
		}
	case "shapeless":
		if ordered == nil || *ordered {
			return fmt.Errorf("shapeless recipe must be unordered")
		}
	case "not_applicable", "unknown":
		if ordered != nil {
			return fmt.Errorf("%s recipe must not define ordered", kind)
		}
	default:
		return fmt.Errorf("unsupported layout_kind %q", kind)
	}
	return nil
}

func validateExportBaseRecipeDocument(raw []byte) error {
	var document exportBaseRecipeDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return fmt.Errorf("decode recipes/recipes.json: %w", err)
	}
	if document.SchemaVersion != "mcmods-recipes/v2" {
		return fmt.Errorf("unsupported base recipe schema: %s", document.SchemaVersion)
	}
	if document.Count != len(document.Recipes) {
		return fmt.Errorf("base recipe count mismatch: declared %d, decoded %d", document.Count, len(document.Recipes))
	}
	seen := make(map[string]struct{}, len(document.Recipes))
	for index, recipe := range document.Recipes {
		recipe.RecipeID = strings.TrimSpace(recipe.RecipeID)
		recipe.RecipeTypeID = strings.TrimSpace(recipe.RecipeTypeID)
		if recipe.RecipeID == "" || recipe.RecipeTypeID == "" || strings.TrimSpace(recipe.SerializerID) == "" {
			return fmt.Errorf("base recipe %d has incomplete identity", index)
		}
		if strings.TrimSpace(recipe.SourceModID) == "" || strings.TrimSpace(recipe.SourceModVersion) == "" || strings.TrimSpace(recipe.SourceModIDSource) == "" {
			return fmt.Errorf("base recipe %s has incomplete source mod identity", recipe.RecipeID)
		}
		if strings.TrimSpace(recipe.LayoutClassificationSource) == "" {
			return fmt.Errorf("base recipe %s has no layout classification source", recipe.RecipeID)
		}
		if err := validateExportRecipeLayoutKind(recipe.LayoutKind, recipe.Ordered); err != nil {
			return fmt.Errorf("base recipe %s: %w", recipe.RecipeID, err)
		}
		if recipe.LayoutKind == "shaped" && (recipe.Width == nil || recipe.Height == nil || *recipe.Width <= 0 || *recipe.Height <= 0) {
			return fmt.Errorf("shaped base recipe %s has invalid dimensions", recipe.RecipeID)
		}
		key := recipe.RecipeID + "\x00" + recipe.RecipeTypeID
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate base recipe %s for type %s", recipe.RecipeID, recipe.RecipeTypeID)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateExportRecipeSource(originKind, modID, modVersion, source string) error {
	if strings.TrimSpace(originKind) == "" {
		return fmt.Errorf("missing origin_kind")
	}
	if strings.TrimSpace(modID) == "" || strings.TrimSpace(modVersion) == "" || strings.TrimSpace(source) == "" {
		return fmt.Errorf("incomplete source mod identity")
	}
	return nil
}

func validateExportRecipeChance(binding exportJEIBinding) error {
	if !binding.ChanceAvailable {
		return nil
	}
	if binding.Chance == nil && binding.ChancePercent == nil {
		return fmt.Errorf("chance_available is true without a numeric chance")
	}
	if binding.Chance != nil && (*binding.Chance < 0 || *binding.Chance > 1) {
		return fmt.Errorf("chance must be between 0 and 1")
	}
	if binding.ChancePercent != nil && (*binding.ChancePercent < 0 || *binding.ChancePercent > 100) {
		return fmt.Errorf("chance_percent must be between 0 and 100")
	}
	return nil
}

func nullableRecipeOrdered(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

func canonicalExportRecipeBinding(binding exportJEIRecipeBinding) ([]byte, error) {
	return json.Marshal(map[string]any{
		"recipe_type_id": binding.RecipeTypeID,
		"recipe_id":      binding.RecipeID,
		"layout_kind":    binding.LayoutKind,
		"ordered":        binding.Ordered,
		"width":          binding.Width,
		"height":         binding.Height,
		"parameters":     json.RawMessage(nonEmptyJSON(binding.Parameters, `{}`)),
		"bindings":       binding.Bindings,
	})
}
