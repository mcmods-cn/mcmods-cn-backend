package httpapi

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ImportPrivacyFinding struct {
	Source          string `json:"source"`
	RevisionID      string `json:"revisionId,omitempty"`
	ObjectID        string `json:"objectId"`
	ReferenceSHA256 string `json:"referenceSha256"`
	Reason          string `json:"reason"`
}
type ImportPrivacyDiagnostic struct {
	ReadOnly  bool                   `json:"readOnly"`
	Scanned   int                    `json:"scanned"`
	Truncated bool                   `json:"truncated"`
	Findings  []ImportPrivacyFinding `json:"findings"`
}

// DiagnoseImportPrivacy never changes records or storage. IDs and reference
// digests are sufficient for an authorized operator to scope reimport; raw
// values, machine paths and asset filenames must not enter the report.
func DiagnoseImportPrivacy(ctx context.Context, pool *pgxpool.Pool, revision string, limit int) (ImportPrivacyDiagnostic, error) {
	result := ImportPrivacyDiagnostic{ReadOnly: true, Findings: []ImportPrivacyFinding{}}
	if limit < 1 || limit > 100000 {
		limit = 1000
	}
	rows, err := pool.Query(ctx, `with candidates as (
 select 'text_asset'::text source,revision_id revision,revision_id object_id,asset_path reference,
  coalesce(json_content::text,text_content,'') payload from catalog_import_text_assets where ($1='' or revision_id=$1)
 union all select 'resource_snapshot',revision_id,id,registry,data::text from resource_import_snapshots where ($1='' or revision_id=$1)
 union all select 'block_entity_model',revision_id,id,'block_entity_model',data::text from block_entity_model_snapshots where ($1='' or revision_id=$1)
 union all select 'block_entity_variant',model.revision_id,variant.id,'block_entity_variant',jsonb_build_object('data',variant.data,'textures',variant.textures,'mesh',variant.mesh_data)::text
  from block_entity_model_variants variant join block_entity_model_snapshots model on model.id=variant.model_snapshot_id where ($1='' or model.revision_id=$1)
 union all select 'capability',revision_id,revision_id,capability_id,data::text from catalog_import_capabilities where ($1='' or revision_id=$1)
 union all select 'package','',package.id,'manifest',package.manifest::text from catalog_import_packages package where ($1='' or exists(select 1 from catalog_import_revisions revision where revision.package_id=package.id and revision.id=$1))
 ) select source,revision,object_id,reference,case when octet_length(payload)>1048576 then null else payload end
 from candidates order by source,revision,object_id,reference limit $2`, revision, limit+1)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var source, rev, id, reference string
		var payload *string
		if err := rows.Scan(&source, &rev, &id, &reference, &payload); err != nil {
			return result, err
		}
		if result.Scanned == limit {
			result.Truncated = true
			break
		}
		result.Scanned++
		reason := ""
		if payload == nil {
			reason = "PAYLOAD_EXCEEDS_DIAGNOSTIC_BOUND"
		} else {
			var value any
			if json.Unmarshal([]byte(*payload), &value) == nil && importJSONHasPrivateMetadata(value) {
				reason = "KNOWN_UNRELATED_PRIVATE_METADATA_KEY"
			}
			if source == "text_asset" && reason == "" {
				purpose := registeredExportAssetPurpose(reference)
				if exportDocumentKind(reference) != "" {
					purpose = exportAssetPartialCatalog
				} else if strings.HasPrefix(reference, "data/") {
					purpose = exportAssetFallback
				}
				if purpose == "" || purpose == exportAssetDiagnostic {
					reason = "UNREGISTERED_PUBLIC_ASSET_PURPOSE"
				} else if exportPurposeRetainsText(purpose) {
					projected, err := projectExportAsset(reference, []byte(*payload), purpose)
					if err != nil {
						reason = "REQUIRES_TYPED_EXTENSION_REGISTRATION"
					} else if purpose == exportAssetOBJ || purpose == exportAssetMTL {
						if strings.TrimSpace(string(projected)) != strings.TrimSpace(*payload) {
							reason = "WAVEFRONT_DERIVATION_REVIEW"
						}
					}
				}
			}
		}
		if reason != "" {
			result.Findings = append(result.Findings, ImportPrivacyFinding{Source: source, RevisionID: rev, ObjectID: id, ReferenceSHA256: sha256Hex([]byte(reference)), Reason: reason})
		}
	}
	return result, rows.Err()
}

func importJSONHasPrivateMetadata(value any) bool {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if importJSONHasPrivateMetadata(item) {
				return true
			}
		}
	case map[string]any:
		for key, item := range typed {
			if exportPrivateMetadataKey(key) || importJSONHasPrivateMetadata(item) {
				return true
			}
		}
	}
	return false
}
