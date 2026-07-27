package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// normalizedExportFallbackAssetPaths returns only source documents needed to
// diagnose entries that the exporter explicitly marked as partially normalized.
// Complete normalized catalogs are represented by regular relational rows and
// must not be copied into PostgreSQL a second time as large JSON blobs.
func normalizedExportFallbackAssetPaths(files map[string]*zip.File) (map[string]struct{}, error) {
	available := make(map[string]struct{}, len(files))
	for name := range files {
		available[name] = struct{}{}
	}
	result := make(map[string]struct{})
	for name, file := range files {
		if file == nil || exportDocumentKind(name) == "" {
			continue
		}
		raw, err := readExportZIPFile(file, maxExportJSONSize)
		if err != nil {
			return nil, fmt.Errorf("read normalized fallback catalog %s: %w", name, err)
		}
		if !bytes.Contains(raw, []byte(`"partial"`)) {
			continue
		}
		var document any
		if err = json.Unmarshal(raw, &document); err != nil {
			return nil, fmt.Errorf("decode normalized fallback catalog %s: %w", name, err)
		}
		if collectPartialNormalizationFallbacks(document, available, result) {
			result[name] = struct{}{}
		}
	}
	return result, nil
}

func collectPartialNormalizationFallbacks(value any, available, result map[string]struct{}) bool {
	switch typed := value.(type) {
	case []any:
		found := false
		for _, item := range typed {
			found = collectPartialNormalizationFallbacks(item, available, result) || found
		}
		return found
	case map[string]any:
		if strings.EqualFold(strings.TrimSpace(exportString(typed["normalization_status"])), "partial") {
			collectAvailableArchivePaths(typed, available, result)
			return true
		}
		found := false
		for _, item := range typed {
			found = collectPartialNormalizationFallbacks(item, available, result) || found
		}
		return found
	default:
		return false
	}
}

func collectAvailableArchivePaths(value any, available, result map[string]struct{}) {
	switch typed := value.(type) {
	case string:
		if _, exists := available[typed]; exists {
			result[typed] = struct{}{}
		}
	case []any:
		for _, item := range typed {
			collectAvailableArchivePaths(item, available, result)
		}
	case map[string]any:
		for _, item := range typed {
			collectAvailableArchivePaths(item, available, result)
		}
	}
}

func retainExportTextAsset(name string, fallbackPaths map[string]struct{}) bool {
	if _, required := fallbackPaths[name]; required {
		return true
	}
	return !isNormalizedExportSourceJSON(name)
}

func isNormalizedExportSourceJSON(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	switch {
	case isExportRegistryFile(name):
		return true
	case exportDocumentKind(name) != "":
		return true
	case name == "tags/tags.json",
		name == "recipes/recipes.json",
		name == "resources/index.json",
		name == modExportBlockEntityIndexPath:
		return true
	case strings.HasPrefix(name, "recipes/jei/"):
		return true
	case strings.HasPrefix(name, "advancements/"):
		return true
	case strings.HasPrefix(name, "data/"):
		return true
	case strings.HasPrefix(name, "block_entities/models/") && strings.HasSuffix(name, ".mesh.json"):
		return true
	default:
		return false
	}
}
