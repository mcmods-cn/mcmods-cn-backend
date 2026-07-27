package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

type exportIconCatalogDefinition struct {
	KindCode          string
	TranslationPrefix string
}

var exportIconCatalogDefinitions = map[string]exportIconCatalogDefinition{
	// The current exporter emits status-effect icons even when it does not emit
	// registries/mob_effects.json. Treat that normalized icon directory as a
	// catalog source so effects are real versioned resources, not loose media.
	"mob_effects": {KindCode: "minecraft.mob_effect", TranslationPrefix: "effect"},
}

type exportIconCatalogCandidate struct {
	RevisionID     string
	Registry       string
	CanonicalID    string
	TranslationKey string
	IconPath       string
	PreviewPath    string
}

// importExportIconCatalogResources materializes normalized resources which
// are represented by an exporter icon directory but omitted from registries/.
// The resulting rows use the same resource_import_snapshots table as manual
// and registry imports, so sections, version details, and public pages do not
// need source-specific behavior.
func importExportIconCatalogResources(
	ctx context.Context,
	tx pgx.Tx,
	resolver catalogResourceIdentityResolver,
	media []modExportPNGMedia,
	translations map[string]map[string]string,
) error {
	candidates := collectExportIconCatalogCandidates(media)
	if len(candidates) == 0 {
		return nil
	}
	names := exportIconCatalogNames(candidates, translations)
	rows := make([]catalogResourceImportRow, 0, len(candidates))
	for index, candidate := range candidates {
		definition := exportIconCatalogDefinitions[candidate.Registry]
		identity := resolver.resolve(definition.KindCode, candidate.CanonicalID)
		data, _ := json.Marshal(map[string]any{
			"id": candidate.CanonicalID, "registry": candidate.Registry,
			"translation_key": candidate.TranslationKey, "source": "normalized_icon_catalog",
		})
		parts := strings.SplitN(identity.CanonicalID, ":", 2)
		rows = append(rows, catalogResourceImportRow{
			EntityID: identity.ID, PublicID: identity.PublicID, KindCode: definition.KindCode,
			CanonicalID: identity.CanonicalID, RawID: identity.RawID, Namespace: parts[0], ResourcePath: parts[1],
			RevisionID: candidate.RevisionID, SnapshotID: catalogSnapshotID("resource", candidate.RevisionID, identity.ID, ""),
			Registry: candidate.Registry, TranslationKey: candidate.TranslationKey, Names: names[index], Data: string(data),
			IconPath: candidate.IconPath, PreviewPath: candidate.PreviewPath,
		})
	}
	if err := persistCatalogResources(ctx, tx, rows); err != nil {
		return fmt.Errorf("persist normalized icon catalog resources: %w", err)
	}
	return nil
}

func collectExportIconCatalogCandidates(media []modExportPNGMedia) []exportIconCatalogCandidate {
	type keyedCandidate struct {
		exportIconCatalogCandidate
		iconRank    int
		previewRank int
	}
	byKey := make(map[string]*keyedCandidate)
	for _, item := range media {
		registry, size, canonicalID, translationKey, ok := parseExportIconCatalogPath(item.AssetPath)
		if !ok || item.RevisionID == "" {
			continue
		}
		key := item.RevisionID + "\x00" + registry + "\x00" + canonicalID
		candidate := byKey[key]
		if candidate == nil {
			candidate = &keyedCandidate{exportIconCatalogCandidate: exportIconCatalogCandidate{
				RevisionID: item.RevisionID, Registry: registry, CanonicalID: canonicalID, TranslationKey: translationKey,
			}}
			byKey[key] = candidate
		}
		iconRank := map[int]int{32: 3, 128: 2, 256: 1}[size]
		previewRank := map[int]int{256: 3, 128: 2, 32: 1}[size]
		if iconRank > candidate.iconRank {
			candidate.IconPath, candidate.iconRank = item.AssetPath, iconRank
		}
		if previewRank > candidate.previewRank {
			candidate.PreviewPath, candidate.previewRank = item.AssetPath, previewRank
		}
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]exportIconCatalogCandidate, 0, len(keys))
	for _, key := range keys {
		result = append(result, byKey[key].exportIconCatalogCandidate)
	}
	return result
}

func parseExportIconCatalogPath(assetPath string) (registry string, size int, canonicalID, translationKey string, ok bool) {
	parts := strings.Split(strings.TrimSpace(strings.TrimSuffix(assetPath, ".png")), "/")
	if len(parts) < 5 || parts[0] != "icons" {
		return "", 0, "", "", false
	}
	definition, supported := exportIconCatalogDefinitions[parts[1]]
	if !supported {
		return "", 0, "", "", false
	}
	size, err := strconv.Atoi(parts[2])
	if err != nil || size <= 0 || parts[3] == "" {
		return "", 0, "", "", false
	}
	resourcePath := path.Clean(strings.Join(parts[4:], "/"))
	if resourcePath == "." || resourcePath == ".." || strings.HasPrefix(resourcePath, "../") {
		return "", 0, "", "", false
	}
	canonicalID = parts[3] + ":" + resourcePath
	translationKey = definition.TranslationPrefix + "." + parts[3] + "." + strings.ReplaceAll(resourcePath, "/", ".")
	return parts[1], size, canonicalID, translationKey, true
}

func exportIconCatalogNames(candidates []exportIconCatalogCandidate, translations map[string]map[string]string) []string {
	result := make([]string, len(candidates))
	for index, candidate := range candidates {
		names := make(map[string]string, len(translations))
		for locale, values := range translations {
			if value := values[candidate.TranslationKey]; value != "" {
				names[locale] = value
			}
		}
		encoded, err := json.Marshal(names)
		if err != nil {
			encoded = []byte("{}")
		}
		result[index] = string(encoded)
	}
	return result
}
