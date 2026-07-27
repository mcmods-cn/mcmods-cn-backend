package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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

type exportJEIRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type exportJEICanvas struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  int     `json:"width"`
	Height int     `json:"height"`
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
		batch.Queue(`insert into catalog_entities(identity_key,public_id,entity_type,status) values($1,$2,'recipe_type','active')
			on conflict(identity_key) do update set status='active',updated_at=now()`, identity.ID, identity.PublicID)
		batch.Queue(`insert into recipe_types(entity_id,canonical_id) values(catalog_entity_internal_id($1),$2) on conflict(canonical_id) do nothing`, identity.ID, category.RecipeTypeID)
		batch.Queue(`insert into recipe_type_import_snapshots(id,recipe_type_id,revision_id,title_translation_key,title_names,width,height,
			image_scale,canvas,catalysts,recipe_count,exported_recipe_count,template_count,background_count,
			template_collection_path,recipe_collection_path)
			values($1,catalog_entity_internal_id($2),$3,$4,$5::jsonb,$6,$7,$8,$9::jsonb,$10::jsonb,$11,$12,$13,$14,$15,$16)
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
		batch.Queue(`insert into catalog_entities(identity_key,public_id,entity_type,status) values($1,$2,'recipe','active')
			on conflict(identity_key) do update set status='active',updated_at=now()`, recipeIdentityValue.ID, recipeIdentityValue.PublicID)
		batch.Queue(`insert into recipes(entity_id,recipe_type_id,canonical_source_id,semantic_fingerprint,owner_mod_id,identity_source)
			select catalog_entity_internal_id($1),catalog_entity_internal_id($2),$3,$4,revision.mod_id,$5 from catalog_import_revisions revision where revision.id=$6
			on conflict(entity_id) do update set semantic_fingerprint=excluded.semantic_fingerprint`, recipeIdentityValue.ID,
			typeIdentity.ID, canonicalSourceID, sha256Hex(fingerprintRaw), recipeIndex.RecipeIDSource, revisionID)
		batch.Queue(`insert into recipe_import_snapshots(id,recipe_id,revision_id,source_recipe_id,source_id_kind,source_recipe_key,
			recipe_collection_path,origin_kind,underlying_recipe_type_id,source_mod_id,source_mod_version,source_mod_id_source,
			render_locale,source_data,template_id,layout_available,layout_kind,ordered,layout_classification_source,width,height,parameters,binding_count)
			values($1,catalog_entity_internal_id($2),$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'',$13::jsonb,null,false,$14,$15,$16,$17,$18,'{}'::jsonb,0)
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

func canonicalImportedTemplateCanvas(document exportJEITemplateCollection) (exportJEICanvas, error) {
	var canvas exportJEICanvas
	if err := json.Unmarshal(document.Canvas, &canvas); err != nil {
		return canvas, fmt.Errorf("decode JEI template canvas: %w", err)
	}
	if canvas.Width <= 0 || canvas.Width > 8192 || canvas.Height <= 0 || canvas.Height > 8192 || document.ImageScale < 1 || document.ImageScale > 32 {
		return canvas, errors.New("JEI template collection has an invalid canonical canvas")
	}
	return canvas, nil
}

func canonicalImportedTemplateSlotRole(sourceRole string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(sourceRole)) {
	case "input":
		return "input", true
	case "output", "byproduct":
		return "output", true
	case "catalyst":
		return "catalyst", true
	default:
		// render_only and unknown JEI roles remain in the immutable import
		// observation and template definition, but they are not selectable
		// ingredient slots in the canonical editor.
		return "", false
	}
}

func canonicalImportedTemplateSlotRect(slot exportJEITemplateSlot, canvas exportJEICanvas, ordinal int) (exportJEIRect, error) {
	var rect exportJEIRect
	if len(slot.Rect) > 0 && json.Unmarshal(slot.Rect, &rect) == nil {
		rect.X -= canvas.X
		rect.Y -= canvas.Y
		if !math.IsNaN(rect.X) && !math.IsNaN(rect.Y) && !math.IsNaN(rect.Width) && !math.IsNaN(rect.Height) &&
			!math.IsInf(rect.X, 0) && !math.IsInf(rect.Y, 0) && !math.IsInf(rect.Width, 0) && !math.IsInf(rect.Height, 0) &&
			rect.X >= 0 && rect.Y >= 0 && rect.Width > 0 && rect.Height > 0 &&
			rect.X+rect.Width <= float64(canvas.Width) && rect.Y+rect.Height <= float64(canvas.Height) {
			return rect, nil
		}
	}
	if slot.CoordinatesAvailable {
		return rect, fmt.Errorf("JEI template slot %s has invalid canonical coordinates", slot.SlotID)
	}
	// Some JEI plugins expose a logical ingredient without coordinates. Keep
	// it editable by placing a deterministic 16px placeholder inside the
	// canvas; the unavailable source geometry is retained in definition.
	size := math.Min(16, math.Min(float64(canvas.Width), float64(canvas.Height)))
	if size <= 0 {
		return rect, fmt.Errorf("JEI template slot %s has no usable canvas", slot.SlotID)
	}
	columns := max(1, canvas.Width/18)
	rect = exportJEIRect{X: float64((ordinal % columns) * 18), Y: float64((ordinal / columns) * 18), Width: size, Height: size}
	if rect.X+rect.Width > float64(canvas.Width) {
		rect.X = float64(canvas.Width) - rect.Width
	}
	if rect.Y+rect.Height > float64(canvas.Height) {
		rect.Y = float64(canvas.Height) - rect.Height
	}
	return rect, nil
}

func canonicalImportedTemplateDefinition(document exportJEITemplateCollection, template exportJEITemplate, snapshotID, revisionID, collectionPath string) string {
	value := map[string]any{
		"source": "import",
		"import": map[string]any{
			"snapshotId": snapshotID, "revisionId": revisionID, "schemaVersion": template.SchemaVersion,
			"templateCollectionPath": collectionPath, "backgroundPath": template.Background,
			"backgroundContainsIngredients": template.BackgroundContainsIngredients, "coordinateSpace": document.CoordinateSpace,
			"canvas": jsonValue(document.Canvas), "imagePixels": jsonValue(document.ImagePixels), "contentRect": jsonValue(document.Content),
			"sourceSlotCount": len(template.Slots),
		},
	}
	raw, _ := json.Marshal(value)
	return string(raw)
}

func canonicalImportedTemplateSlotDefinition(slot exportJEITemplateSlot, snapshotID string) string {
	value := map[string]any{
		"source": "import", "importSnapshotId": snapshotID, "sourceRole": slot.Role, "jeiRole": slot.JEIRole,
		"coordinatesAvailable": slot.CoordinatesAvailable, "sourceRect": jsonValue(slot.Rect), "visualRect": jsonValue(slot.VisualRect),
	}
	raw, _ := json.Marshal(value)
	return string(raw)
}

func queueExportJEITemplateCollection(batch *modExportWriteBatch, revisions map[string]string, name string, document exportJEITemplateCollection) error {
	recipeTypeID := strings.TrimSpace(document.RecipeTypeID)
	revisionID := exportRevisionForRecipeType(revisions, recipeTypeID)
	if revisionID == "" {
		return nil
	}
	typeIdentity := recipeTypeIdentity(recipeTypeID)
	typeSnapshotID := catalogSnapshotID("recipe-type", revisionID, typeIdentity.ID, "")
	canvas, err := canonicalImportedTemplateCanvas(document)
	if err != nil {
		return fmt.Errorf("JEI template collection %s: %w", recipeTypeID, err)
	}
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
		canonicalIdentity := catalogEditorIdentityForTemplate(typeIdentity.ID, template.TemplateID)
		batch.queue(`insert into catalog_entities(identity_key,public_id,entity_type,status)
			select $1,$2,'recipe_template',case when revision.is_active then 'active' else 'placeholder' end
			from catalog_import_revisions revision where revision.id=$3
			on conflict(identity_key) do update set status='active',updated_at=now()
			where excluded.status='active' and catalog_entities.status='placeholder'
			and catalog_entities.published_revision_id is null`, 0, canonicalIdentity.ID, canonicalIdentity.PublicID, revisionID)
		batch.queue(`insert into recipe_template_import_snapshots(id,recipe_type_snapshot_id,revision_id,recipe_type_id,canonical_template_id,source_template_id,
			schema_version,template_collection_path,background_path,background_contains_ingredients,coordinate_space,image_scale,
			canvas,image_pixels,content_rect,slot_count)
			values($1,$2,$3,catalog_entity_internal_id($4),catalog_entity_internal_id($5),$6,$7,$8,$9,$10,$11,$12,$13::jsonb,$14::jsonb,$15::jsonb,$16)
			on conflict(recipe_type_snapshot_id,source_template_id) do update set background_path=excluded.background_path,
			background_contains_ingredients=excluded.background_contains_ingredients,coordinate_space=excluded.coordinate_space,
			image_scale=excluded.image_scale,canvas=excluded.canvas,image_pixels=excluded.image_pixels,
			content_rect=excluded.content_rect,slot_count=excluded.slot_count,canonical_template_id=excluded.canonical_template_id`, int64(len(document.Canvas)+len(document.ImagePixels)+len(document.Content)),
			templateID, typeSnapshotID, revisionID, typeIdentity.ID, canonicalIdentity.ID, template.TemplateID, template.SchemaVersion, name,
			template.Background, template.BackgroundContainsIngredients, document.CoordinateSpace, max(1, document.ImageScale),
			nonEmptyJSON(document.Canvas, `{}`), nonEmptyJSON(document.ImagePixels, `{}`), nonEmptyJSON(document.Content, `{}`), len(template.Slots))
		definition := canonicalImportedTemplateDefinition(document, template, templateID, revisionID, name)
		batch.queue(`insert into recipe_layout_templates(entity_id,recipe_type_id,template_key,import_snapshot_id,background_file_id,
			canvas_width,canvas_height,image_scale,definition,published_revision_id,updated_by)
			select catalog_entity_internal_id($1),catalog_entity_internal_id($2),$3,$4,null,$5,$6,$7,$8::jsonb,null,null
			from catalog_entities entity
			where entity.identity_key=$1 and entity.status='active' and entity.published_revision_id is null
			and exists(select 1 from catalog_import_revisions revision where revision.id=$9 and revision.is_active)
			on conflict(entity_id) do update set recipe_type_id=excluded.recipe_type_id,template_key=excluded.template_key,
			import_snapshot_id=excluded.import_snapshot_id,background_file_id=null,canvas_width=excluded.canvas_width,
			canvas_height=excluded.canvas_height,image_scale=excluded.image_scale,definition=excluded.definition,updated_by=null,updated_at=now()
			where recipe_layout_templates.published_revision_id is null and exists(
				select 1 from catalog_entities entity where entity.id=recipe_layout_templates.entity_id
				and entity.published_revision_id is null and entity.status='active')`, int64(len(definition)),
			canonicalIdentity.ID, typeIdentity.ID, template.TemplateID, templateID, canvas.Width, canvas.Height, document.ImageScale, definition, revisionID)
		batch.queue(`update recipe_template_slots slot set ordinal=slot.ordinal+1000000
			where slot.template_id=catalog_entity_internal_id($1) and exists(select 1 from recipe_layout_templates template
				join catalog_entities entity on entity.id=template.entity_id where template.entity_id=catalog_entity_internal_id($1)
				and template.published_revision_id is null and template.import_snapshot_id is not null
				and entity.published_revision_id is null and entity.status='active')
			and exists(select 1 from catalog_import_revisions revision where revision.id=$2 and revision.is_active)`, 0, canonicalIdentity.ID, revisionID)
		batch.queue(`delete from recipe_template_slots slot where slot.template_id=catalog_entity_internal_id($1)
			and not exists(select 1 from recipe_bindings binding where binding.template_slot_id=slot.id)
			and exists(select 1 from recipe_layout_templates template join catalog_entities entity on entity.id=template.entity_id
				where template.entity_id=catalog_entity_internal_id($1) and template.published_revision_id is null and template.import_snapshot_id is not null
				and entity.published_revision_id is null and entity.status='active')
			and exists(select 1 from catalog_import_revisions revision where revision.id=$2 and revision.is_active)`, 0, canonicalIdentity.ID, revisionID)
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
			batch.queue(`insert into recipe_template_import_slots(id,template_id,source_slot_id,role,jei_role,output_index,ordinal,coordinates_available,rect,visual_rect,data)
				values($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10::jsonb,$11::jsonb)
				on conflict(template_id,source_slot_id) do update set role=excluded.role,jei_role=excluded.jei_role,
				output_index=excluded.output_index,ordinal=excluded.ordinal,
				coordinates_available=excluded.coordinates_available,rect=excluded.rect,visual_rect=excluded.visual_rect,data=excluded.data`,
				int64(len(slotData)), slotID, templateID, slot.SlotID, slot.Role, slot.JEIRole, slot.OutputIndex, ordinal, slot.CoordinatesAvailable,
				nonEmptyJSON(slot.Rect, `{}`), nonEmptyJSON(slot.VisualRect, `{}`), string(slotData))
			canonicalRole, editable := canonicalImportedTemplateSlotRole(slot.Role)
			if !editable {
				continue
			}
			rect, rectErr := canonicalImportedTemplateSlotRect(slot, canvas, ordinal)
			if rectErr != nil {
				return fmt.Errorf("JEI template %s: %w", template.TemplateID, rectErr)
			}
			canonicalSlotID := catalogSnapshotID("canonical-template-slot", "stable", canonicalIdentity.ID, slot.SlotID)
			slotDefinition := canonicalImportedTemplateSlotDefinition(slot, templateID)
			batch.queue(`insert into recipe_template_slots(identity_key,template_id,slot_key,role,output_index,ordinal,x,y,width,height,definition)
				select $1,catalog_entity_internal_id($2),$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb from recipe_layout_templates template
				join catalog_entities entity on entity.id=template.entity_id where template.entity_id=catalog_entity_internal_id($2)
				and template.published_revision_id is null and template.import_snapshot_id is not null
				and entity.published_revision_id is null and entity.status='active'
				and exists(select 1 from catalog_import_revisions revision where revision.id=$12 and revision.is_active)
				on conflict(template_id,slot_key) do update set
				role=case when exists(select 1 from recipe_bindings binding where binding.template_slot_id=recipe_template_slots.id)
					then recipe_template_slots.role else excluded.role end,
				output_index=case when exists(select 1 from recipe_bindings binding where binding.template_slot_id=recipe_template_slots.id)
					then recipe_template_slots.output_index else excluded.output_index end,
				ordinal=excluded.ordinal,x=excluded.x,y=excluded.y,width=excluded.width,height=excluded.height,
				definition=excluded.definition`, int64(len(slotDefinition)), canonicalSlotID, canonicalIdentity.ID, slot.SlotID,
				canonicalRole, slot.OutputIndex, ordinal, rect.X, rect.Y, rect.Width, rect.Height, slotDefinition, revisionID)
		}
	}
	return nil
}

type importedRecipeTemplatePromotion struct {
	RecipeTypeID                  string
	SchemaVersion                 string
	TemplateCollectionPath        string
	SourceTemplateID              string
	BackgroundPath                string
	BackgroundContainsIngredients bool
	CoordinateSpace               string
	ImageScale                    int
	Canvas                        json.RawMessage
	ImagePixels                   json.RawMessage
	ContentRect                   json.RawMessage
	Slots                         []exportJEITemplateSlot
}

// promoteImportedRecipeTemplatesTx replays normalized observations only after
// a revision becomes active. Pending/rejected imports therefore never replace
// the public canonical editor state, while manual publication remains protected
// by the published_revision_id guards in queueExportJEITemplateCollection.
func promoteImportedRecipeTemplatesTx(ctx context.Context, tx pgx.Tx, revisionID string) error {
	rows, err := tx.Query(ctx, `select recipe_type.canonical_id,snapshot.schema_version,snapshot.template_collection_path,
		snapshot.source_template_id,snapshot.background_path,snapshot.background_contains_ingredients,
		snapshot.coordinate_space,snapshot.image_scale,snapshot.canvas,snapshot.image_pixels,snapshot.content_rect
		from recipe_template_import_snapshots snapshot
		join catalog_import_revisions revision on revision.id=snapshot.revision_id
		join recipe_types recipe_type on recipe_type.entity_id=snapshot.recipe_type_id
		where snapshot.revision_id=$1 and revision.is_active and revision.status in ('ready','partial')
		order by recipe_type.canonical_id,snapshot.source_template_id`, revisionID)
	if err != nil {
		return err
	}
	promotions := []importedRecipeTemplatePromotion{}
	for rows.Next() {
		var promotion importedRecipeTemplatePromotion
		if err = rows.Scan(&promotion.RecipeTypeID, &promotion.SchemaVersion, &promotion.TemplateCollectionPath,
			&promotion.SourceTemplateID, &promotion.BackgroundPath, &promotion.BackgroundContainsIngredients,
			&promotion.CoordinateSpace, &promotion.ImageScale, &promotion.Canvas, &promotion.ImagePixels, &promotion.ContentRect); err != nil {
			rows.Close()
			return err
		}
		promotions = append(promotions, promotion)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	batch := newModExportWriteBatch()
	for index := range promotions {
		promotion := &promotions[index]
		templateSnapshotID := exportRecipeTemplateID(revisionID, promotion.RecipeTypeID, promotion.SourceTemplateID)
		slotRows, slotErr := tx.Query(ctx, `select source_slot_id,role,jei_role,output_index,coordinates_available,rect,visual_rect
			from recipe_template_import_slots where template_id=$1 order by ordinal`, templateSnapshotID)
		if slotErr != nil {
			return slotErr
		}
		for slotRows.Next() {
			var slot exportJEITemplateSlot
			var outputIndex sql.NullInt64
			if slotErr = slotRows.Scan(&slot.SlotID, &slot.Role, &slot.JEIRole, &outputIndex, &slot.CoordinatesAvailable,
				&slot.Rect, &slot.VisualRect); slotErr != nil {
				slotRows.Close()
				return slotErr
			}
			if outputIndex.Valid {
				value := int(outputIndex.Int64)
				slot.OutputIndex = &value
			}
			promotion.Slots = append(promotion.Slots, slot)
		}
		if slotErr = slotRows.Err(); slotErr != nil {
			slotRows.Close()
			return slotErr
		}
		slotRows.Close()
		document := exportJEITemplateCollection{
			SchemaVersion: "mcmods-jei-template-collection/v2", RecipeTypeID: promotion.RecipeTypeID,
			CoordinateSpace: promotion.CoordinateSpace, ImageScale: promotion.ImageScale, Canvas: promotion.Canvas,
			ImagePixels: promotion.ImagePixels, Content: promotion.ContentRect, TemplateCount: 1,
			Templates: []exportJEITemplate{{SchemaVersion: promotion.SchemaVersion, TemplateID: promotion.SourceTemplateID,
				Background: promotion.BackgroundPath, BackgroundContainsIngredients: promotion.BackgroundContainsIngredients,
				SlotCount: len(promotion.Slots), Slots: promotion.Slots}},
		}
		revisions := map[string]string{exportResourceNamespace(promotion.RecipeTypeID): revisionID}
		if err = queueExportJEITemplateCollection(batch, revisions, promotion.TemplateCollectionPath, document); err != nil {
			return err
		}
		if batch.shouldFlush() {
			if err = batch.flush(ctx, tx); err != nil {
				return err
			}
		}
	}
	return batch.flush(ctx, tx)
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

func queueExportJEIRecipeCollection(batch *modExportWriteBatch, resolver catalogResourceIdentityResolver, packageID string, revisions map[string]string, name string, document exportJEIRecipeCollection) error {
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
		canonicalSourceID := ""
		if canonical {
			canonicalSourceID = strings.TrimSpace(recipeBinding.RecipeID)
		}
		sourceData := compactImportJSONObject(recipeBinding.Raw, "bindings")
		batch.recipes = append(batch.recipes, recipeImportRecipeWrite{
			TypeIdentity: typeIdentity.ID, TypePublicID: typeIdentity.PublicID, RecipeTypeID: recipeTypeID,
			RecipeIdentity: recipeIdentityValue.ID, RecipePublicID: recipeIdentityValue.PublicID,
			CanonicalSourceID: canonicalSourceID, SemanticFingerprint: sha256Hex(fingerprintRaw),
			IdentitySource: recipeBinding.RecipeIDSource, RevisionID: revisionID, SnapshotID: recipeSnapshotID,
			SourceRecipeID: recipeBinding.RecipeID, SourceRecipeKey: recipeKey, CollectionPath: name,
			OriginKind: recipeBinding.OriginKind, UnderlyingRecipeTypeID: recipeBinding.UnderlyingRecipeTypeID,
			SourceModID: recipeBinding.SourceModID, SourceModVersion: recipeBinding.SourceModVersion,
			SourceModIDSource: recipeBinding.SourceModIDSource, RenderLocale: recipeBinding.RenderLocale,
			SourceData: sourceData, TemplateID: templateID,
			LayoutKind: recipeBinding.LayoutKind, Ordered: recipeBinding.Ordered,
			LayoutClassificationSource: recipeBinding.LayoutClassificationSource,
			Width:                      recipeBinding.Width, Height: recipeBinding.Height,
			Parameters:   json.RawMessage(nonEmptyJSON(recipeBinding.Parameters, `{}`)),
			BindingCount: len(recipeBinding.Bindings),
		})
		batch.byteCount += int64(len(sourceData) + len(recipeBinding.Parameters))
		if err = queueExportRecipeBindings(batch, resolver, recipeSnapshotID, templateID, recipeBinding.Bindings); err != nil {
			return fmt.Errorf("JEI recipe %q: %w", recipeKey, err)
		}
	}
	return nil
}

func queueExportRecipeBindings(batch *modExportWriteBatch, resolver catalogResourceIdentityResolver, recipeSnapshotID, templateID string, bindings []exportJEIBinding) error {
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
		tagIdentityKey := ""
		tagPublicID := ""
		if tagID != "" {
			tag := tagIdentity("minecraft:item", tagID)
			tagIdentityKey = tag.ID
			tagPublicID = tag.PublicID
		}
		bindingData := binding.Raw
		if len(bindingData) == 0 {
			bindingData, _ = json.Marshal(binding)
		}
		bindingData = compactImportJSONObject(bindingData,
			"alternatives", "slot_id", "ingredient_present", "clickable", "placeholder_item",
			"item_tag_equivalent", "semantic_role", "role_source", "chance_available", "chance",
			"chance_percent", "chance_comparator", "chance_source", "chance_text", "chance_texts",
			"chance_translation_key", "chance_render_x", "chance_render_y", "byproduct",
		)
		byproduct := binding.Byproduct || strings.EqualFold(strings.TrimSpace(binding.SemanticRole), "byproduct")
		batch.recipeBindings = append(batch.recipeBindings, recipeImportBindingWrite{
			ID: bindingID, RecipeSnapshotID: recipeSnapshotID, TemplateSlotID: templateSlotID,
			SourceSlotID: binding.SlotID, Ordinal: ordinal, IngredientPresent: binding.IngredientPresent,
			Clickable: binding.Clickable, PlaceholderItem: binding.PlaceholderItem,
			ItemTagEquivalent: tagID, SemanticRole: binding.SemanticRole, RoleSource: binding.RoleSource,
			TagIdentity: tagIdentityKey, TagPublicID: tagPublicID, TagCanonicalID: tagID, Data: bindingData,
		})
		batch.byteCount += int64(len(bindingData))
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
			resource := resolver.resolve(kindCode, resourceID)
			namespace, resourcePath := resource.Namespace, resource.ResourcePath
			uniqueID := strings.TrimSpace(exportString(alternative["unique_id"]))
			nbtSNBT := strings.TrimSpace(exportString(alternative["nbt_snbt"]))
			alternativeData, _ := json.Marshal(alternative)
			alternativeData = compactImportJSONObject(alternativeData,
				"type", "item", "resource_location", "count", "amount", "unique_id", "nbt_snbt",
			)
			resourceAliasID := resource.RawID
			if resourceAliasID == "" {
				resourceAliasID = resourceID
			}
			batch.recipeCandidates = append(batch.recipeCandidates, recipeImportCandidateWrite{
				BindingID: bindingID, AlternativeIndex: alternativeIndex,
				ResourceIdentity: resource.ID, ResourcePublicID: resource.PublicID,
				ResourceCanonicalID: resource.CanonicalID, ResourceRawID: resourceID, ResourceAliasID: resourceAliasID,
				ResourceNamespace: namespace, ResourcePath: resourcePath, KindCode: kindCode,
				Amount: exportRecipeAmount(alternative), IngredientKind: ingredientKind,
				IngredientType: ingredientType, UniqueID: uniqueID, NBTSNBT: nbtSNBT,
				ChanceAvailable: binding.ChanceAvailable, Chance: binding.Chance,
				ChancePercent: binding.ChancePercent, ChanceComparator: binding.ChanceComparator,
				ChanceSource: binding.ChanceSource, ChanceText: binding.ChanceText,
				ChanceTexts:          json.RawMessage(nonEmptyJSON(binding.ChanceTexts, `{}`)),
				ChanceTranslationKey: binding.ChanceTranslationKey, ChanceRenderX: binding.ChanceRenderX,
				ChanceRenderY: binding.ChanceRenderY, Byproduct: byproduct, Data: alternativeData,
			})
			batch.byteCount += int64(len(alternativeData) + len(binding.ChanceTexts))
		}
	}
	return nil
}

// The complete exporter collection is retained once in catalog_import_text_assets.
// Snapshot rows only need metadata that is not already represented in normalized
// columns; retaining duplicated fields and nested children here dominates network
// transfer time when PostgreSQL is remote.
func compactImportJSONObject(value json.RawMessage, omittedFields ...string) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage(`{}`)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(value, &object); err != nil {
		return value
	}
	for _, field := range omittedFields {
		delete(object, field)
	}
	compacted, err := json.Marshal(object)
	if err != nil {
		return value
	}
	return compacted
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
	role := strings.ToLower(strings.TrimSpace(binding.SemanticRole))
	byproduct := binding.Byproduct || role == "byproduct"
	if (binding.ChanceAvailable || binding.Chance != nil || binding.ChancePercent != nil || byproduct) && role != "output" && role != "byproduct" {
		return fmt.Errorf("chance and byproduct are only valid for output items")
	}
	if !binding.ChanceAvailable {
		if binding.Chance != nil || binding.ChancePercent != nil {
			return fmt.Errorf("numeric chance requires chance_available")
		}
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
