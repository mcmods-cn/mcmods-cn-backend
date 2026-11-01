package httpapi

// Imported source archives are private upload evidence. They are not a registry
// of public assets: only the purposes below may create derived asset rows.
import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
)

type exportAssetPurpose string

const (
	exportAssetNormalized  exportAssetPurpose = "normalized_catalog"
	exportAssetDiagnostic  exportAssetPurpose = "private_source_diagnostic"
	exportAssetImage       exportAssetPurpose = "render_image"
	exportAssetModel       exportAssetPurpose = "minecraft_model"
	exportAssetBlockstate  exportAssetPurpose = "minecraft_blockstate"
	exportAssetTexture     exportAssetPurpose = "texture_metadata"
	exportAssetOBJ         exportAssetPurpose = "wavefront_geometry"
	exportAssetMTL         exportAssetPurpose = "wavefront_material"
	exportAssetFallback    exportAssetPurpose = "partial_definition"
	exportAssetEntityModel exportAssetPurpose = "entity_model_metadata"
	// The complete normalized partial catalog is projected with its registered
	// catalog fields; partial does not confer permission to publish raw JSON.
	exportAssetPartialCatalog exportAssetPurpose = "partial_catalog"
)

type exportAssetPlan map[string]exportAssetPurpose

func exportAssetRegistrationError(purpose exportAssetPurpose, name string) error {
	// Paths and field names are untrusted and may themselves contain private
	// values. A digest lets the owner match its local archive without persisting
	// arbitrary exception text or input data in job diagnostics.
	return fmt.Errorf("unregistered_export_asset: purpose=%s path_sha256=%s; register a typed projection for this asset/extension before importing; see docs/import-asset-registry.md", purpose, sha256Hex([]byte(name)))
}

func buildExportAssetPlan(files map[string]*zip.File) (exportAssetPlan, error) {
	fallbacks, err := normalizedExportFallbackAssetPaths(files)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	plan := make(exportAssetPlan, len(files))
	for _, name := range names {
		if files[name].FileInfo().IsDir() {
			continue
		}
		purpose := registeredExportAssetPurpose(name)
		if _, needed := fallbacks[name]; needed {
			if exportDocumentKind(name) != "" {
				purpose = exportAssetPartialCatalog
			} else if strings.HasPrefix(name, "data/") && strings.HasSuffix(name, ".json") {
				purpose = exportAssetFallback
			} else {
				return nil, exportAssetRegistrationError("partial_reference", name)
			}
		}
		if purpose == "" {
			return nil, exportAssetRegistrationError("unknown_path", name)
		}
		plan[name] = purpose
		if !exportPurposeRetainsText(purpose) && name != modExportBlockEntityIndexPath && !(strings.HasPrefix(name, "block_entities/models/") && strings.HasSuffix(name, ".mesh.json")) {
			continue
		}
		data, err := readExportZIPFile(files[name], maxExportJSONSize)
		if err != nil {
			return nil, fmt.Errorf("read registered asset: %w", err)
		}
		if _, err = projectExportAsset(name, data, purpose); err != nil {
			return nil, err
		}
	}
	return plan, nil
}

func registeredExportAssetPurpose(name string) exportAssetPurpose {
	switch {
	case name == "manifest.json", name == modExportCapabilitiesPath,
		isExportRegistryFile(name), isExportTranslationFile(name), exportDocumentKind(name) != "":
		return exportAssetNormalized
	case name == "tags/tags.json", name == "recipes/recipes.json", name == "resources/index.json",
		name == modExportBlockEntityIndexPath, name == "translations/languages.json",
		name == "worldgen/registries.json", name == "worldgen/entity_spawns.json",
		strings.HasPrefix(name, "recipes/jei/") && strings.HasSuffix(name, ".json"),
		strings.HasPrefix(name, "advancements/localized/") && strings.HasSuffix(name, ".json"),
		strings.HasPrefix(name, "data/") && strings.HasSuffix(name, ".json"),
		strings.HasPrefix(name, "block_entities/models/") && strings.HasSuffix(name, ".mesh.json"):
		return exportAssetNormalized
	case strings.HasSuffix(name, ".log"):
		return exportAssetDiagnostic
	case strings.HasSuffix(name, ".png") && (strings.HasPrefix(name, "assets/") || strings.HasPrefix(name, "icons/") || strings.HasPrefix(name, "ingredients/") || strings.HasPrefix(name, "recipes/jei/backgrounds/") || strings.HasPrefix(name, "entities/") || strings.HasPrefix(name, "block_entities/")):
		return exportAssetImage
	case strings.HasPrefix(name, "assets/") && strings.Contains(name, "/models/"):
		switch strings.ToLower(path.Ext(name)) {
		case ".json":
			return exportAssetModel
		case ".obj":
			return exportAssetOBJ
		case ".mtl":
			return exportAssetMTL
		}
	case strings.HasPrefix(name, "assets/") && strings.Contains(name, "/blockstates/") && strings.HasSuffix(name, ".json"):
		return exportAssetBlockstate
	case strings.HasPrefix(name, "assets/") && strings.Contains(name, "/textures/") && strings.HasSuffix(name, ".png.mcmeta"):
		return exportAssetTexture
	case strings.HasPrefix(name, "entities/models/") || strings.HasPrefix(name, "block_entities/models/"):
		if strings.HasSuffix(name, ".obj") {
			return exportAssetOBJ
		}
		if strings.HasPrefix(name, "entities/models/") && strings.HasSuffix(name, ".json") {
			return exportAssetEntityModel
		}
	}
	return ""
}

func exportPurposeRetainsText(purpose exportAssetPurpose) bool {
	return purpose != exportAssetNormalized && purpose != exportAssetDiagnostic && purpose != exportAssetImage && purpose != ""
}

func projectExportAsset(name string, data []byte, purpose exportAssetPurpose) ([]byte, error) {
	if purpose == exportAssetOBJ || purpose == exportAssetMTL {
		return projectExportWavefront(name, data, purpose)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, exportAssetRegistrationError(purpose, name)
	}
	var rule *exportJSONRule
	switch purpose {
	case exportAssetNormalized:
		if name == modExportBlockEntityIndexPath {
			rule = exportBlockEntityIndexJSONRule()
		} else if strings.HasSuffix(name, ".mesh.json") {
			rule = exportBlockEntityMeshJSONRule()
		}
	case exportAssetModel:
		rule = exportModelJSONRule()
	case exportAssetBlockstate:
		rule = exportBlockstateJSONRule()
	case exportAssetTexture:
		rule = exportTextureJSONRule()
	case exportAssetEntityModel:
		rule = exportObjectRule(map[string]*exportJSONRule{"schema_version": exportStringRule(), "id": exportStringRule(), "texture": exportStringRule(), "obj": exportStringRule(), "snapshot_pose": exportStringRule(), "base_model_only": exportBoolRule(), "vertex_count": exportNumberRule(), "quad_count": exportNumberRule()})
	case exportAssetFallback:
		parts := strings.SplitN(name, "/", 4)
		if len(parts) == 4 && (parts[2] == "advancement" || parts[2] == "advancements") {
			rule = exportAdvancementJSONRule(false)
		} else {
			rule = exportWorldgenDefinitionJSONRule()
		}
	case exportAssetPartialCatalog:
		rule = exportPartialCatalogJSONRule(name)
	}
	if rule == nil {
		return nil, exportAssetRegistrationError(purpose, name)
	}
	projected, err := rule.project(value)
	if err != nil {
		return nil, exportAssetRegistrationError(purpose, name)
	}
	return json.Marshal(projected)
}
