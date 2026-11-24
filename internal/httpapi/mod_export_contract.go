package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const (
	modExportSchemaVersion    = "mcmods-export/v1"
	modExportCapabilitySchema = "mcmods-capabilities/v1"
	modExportCapabilitiesPath = "compatibility/capabilities.json"
	modExportExpectedPackage  = "zip"
)

type modExportCapability struct {
	ID     string
	Status string
	Source string
	Data   json.RawMessage
}

type modExportCapabilityDocument struct {
	SchemaVersion    string            `json:"schema_version"`
	MinecraftVersion string            `json:"minecraft_version"`
	Loader           string            `json:"loader"`
	FormatContract   string            `json:"format_contract"`
	Capabilities     []json.RawMessage `json:"capabilities"`
}

func validateModExportManifest(manifest modExportManifest) error {
	if manifest.SchemaVersion != modExportSchemaVersion {
		return catalogImportInputMessage("unsupported_schema")
	}
	if manifest.Status != "complete" {
		return withCatalogImportHint(fmt.Errorf("manifest status is %s", manifest.Status), "Use a completed export package.")
	}
	if !strings.EqualFold(strings.TrimSpace(manifest.PackageFormat), modExportExpectedPackage) {
		return withCatalogImportHint(fmt.Errorf("unsupported package format: %s", manifest.PackageFormat), "Use an exporter ZIP package.")
	}
	if manifest.Configuration.AllNamespaces {
		return catalogImportInputMessage("this website import targets one project: export explicit namespace(s) for that project instead of all_namespaces=true")
	}
	if strings.TrimSpace(manifest.MinecraftVersion) == "" || strings.TrimSpace(manifest.ExporterVersion) == "" || strings.TrimSpace(manifest.Loader) == "" || len(manifest.Configuration.Namespaces) == 0 {
		return catalogImportInputMessage("manifest required fields are missing")
	}
	if len(normalizeExportNamespaces(manifest.Configuration.Namespaces)) == 0 {
		return catalogImportInputMessage("manifest contains no valid namespace")
	}
	return nil
}

func decodeModExportCapabilities(raw []byte, manifest modExportManifest) ([]modExportCapability, error) {
	var document modExportCapabilityDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("decode %s: %w", modExportCapabilitiesPath, err)
	}
	if document.SchemaVersion != modExportCapabilitySchema {
		return nil, fmt.Errorf("unsupported capability schema: %s", document.SchemaVersion)
	}
	if document.FormatContract != manifest.SchemaVersion {
		return nil, fmt.Errorf("capability format contract %q does not match manifest", document.FormatContract)
	}
	if document.MinecraftVersion != manifest.MinecraftVersion || !strings.EqualFold(strings.TrimSpace(document.Loader), strings.TrimSpace(manifest.Loader)) {
		return nil, catalogImportInputMessage("capability environment does not match manifest")
	}

	result := make([]modExportCapability, 0, len(document.Capabilities))
	seen := make(map[string]struct{}, len(document.Capabilities))
	for index, rawCapability := range document.Capabilities {
		var value struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Source string `json:"source"`
		}
		if err := json.Unmarshal(rawCapability, &value); err != nil {
			return nil, fmt.Errorf("decode capability %d: %w", index, err)
		}
		value.ID = strings.ToLower(strings.TrimSpace(value.ID))
		value.Status = strings.ToLower(strings.TrimSpace(value.Status))
		value.Source = strings.TrimSpace(value.Source)
		if value.ID == "" {
			return nil, fmt.Errorf("capability %d has no id", index)
		}
		if _, duplicate := seen[value.ID]; duplicate {
			return nil, fmt.Errorf("duplicate capability id: %s", value.ID)
		}
		switch value.Status {
		case "available", "degraded", "unavailable":
		default:
			return nil, fmt.Errorf("capability %s has unsupported status %q", value.ID, value.Status)
		}
		seen[value.ID] = struct{}{}
		projected, _ := json.Marshal(value)
		result = append(result, modExportCapability{ID: value.ID, Status: value.Status, Source: value.Source, Data: projected})
	}
	return result, nil
}

func importExportCapabilities(ctx context.Context, tx pgx.Tx, revisions map[string]string, capabilities []modExportCapability) error {
	if len(revisions) == 0 || len(capabilities) == 0 {
		return nil
	}
	// Namespace aliases can share a revision. Capabilities belong to the
	// revision, not to each alias; COPY must receive each primary key once.
	revisionIDs := sortedExportRevisionIDs(revisions)
	rows := len(revisionIDs) * len(capabilities)
	copied, err := tx.CopyFrom(ctx, pgx.Identifier{"catalog_import_capabilities"}, []string{
		"revision_id", "capability_id", "status", "source", "data",
	}, pgx.CopyFromSlice(rows, func(index int) ([]any, error) {
		revisionID := revisionIDs[index/len(capabilities)]
		capability := capabilities[index%len(capabilities)]
		return []any{revisionID, capability.ID, capability.Status, capability.Source, string(capability.Data)}, nil
	}))
	if err != nil {
		return err
	}
	if copied != int64(rows) {
		return fmt.Errorf("copy export capabilities: copied %d of %d rows", copied, rows)
	}
	return nil
}
