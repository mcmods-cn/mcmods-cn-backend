package httpapi

import (
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
)

// modExportRevisionDetail is an import review record, not a project metadata
// revision. Every child API repeats the same current authorization check.
func (s *Server) modExportRevisionDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("revisionId")
	if !s.canReadModExportRevision(w, r, id) {
		return
	}
	var revisionNo, submittedBy int64
	var status, siteID, projectID, sourceKind, namespace, mc, loader, versionID, versionLabel, digest, archiveName, previous string
	var active bool
	err := s.db.QueryRow(r.Context(), `select revision.revision_no,revision.status,revision.is_active,
 mod.slug,mod.project_code,revision.source_kind,revision.source_namespace,revision.minecraft_version,revision.loader,
 version.public_id,version.label,package.sha256,package.archive_name,coalesce(revision.submitted_by,job.created_by,0),
 coalesce((select prior.id from catalog_import_revisions prior where prior.mod_id=revision.mod_id
 and prior.target_version_id=revision.target_version_id and prior.source_namespace=revision.source_namespace
 and prior.source_kind=revision.source_kind and prior.is_active and prior.id<>revision.id order by prior.revision_no desc limit 1),'')
 from catalog_import_revisions revision join mods mod on mod.id=revision.mod_id
 join mod_content_versions version on version.id=revision.target_version_id
 join catalog_import_packages package on package.id=revision.package_id
 join catalog_import_jobs job on job.id=revision.job_id where revision.id=$1`, id).Scan(
		&revisionNo, &status, &active, &siteID, &projectID, &sourceKind, &namespace, &mc, &loader, &versionID, &versionLabel, &digest, &archiveName, &submittedBy, &previous)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "export revision not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read export review detail")
		return
	}
	var added, changed, removed, resourceCount int64
	err = s.db.QueryRow(r.Context(), `with incoming as (
 select resource_id,data,names from resource_import_snapshots where revision_id=$1
 ), prior as (select resource_id,data,names from resource_import_snapshots where revision_id=$2)
 select (select count(*) from incoming),
 count(*) filter(where prior.resource_id is null),
 count(*) filter(where incoming.resource_id is not null and prior.resource_id is not null
 and (incoming.data is distinct from prior.data or incoming.names is distinct from prior.names)),
 count(*) filter(where incoming.resource_id is null)
 from incoming full join prior using(resource_id)`, id, previous).Scan(&resourceCount, &added, &changed, &removed)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to compare export review resources")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"id": id, "revisionNo": revisionNo, "status": status, "isActive": active, "modSiteId": siteID, "modPublicId": projectID,
		"sourceKind": sourceKind, "namespace": namespace, "minecraftVersion": mc, "loader": loader,
		"targetVersionPublicId": versionID, "targetVersionLabel": versionLabel, "package": map[string]string{"sha256": digest, "archiveName": archiveName},
		"previousRevisionId": previous, "counts": map[string]int64{"resources": resourceCount},
		"resourceChanges": map[string]int64{"added": added, "changed": changed, "removed": removed},
		"canReview":       canReviewProjectSubmission(currentClaims(r), projectID, submittedBy) && (status == "ready" || status == "partial") && !active,
	})
}
