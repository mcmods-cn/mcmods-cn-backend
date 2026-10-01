package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Server) catalogResourceAsset(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	assetKind := strings.ToLower(strings.TrimSpace(r.PathValue("assetKind")))
	if !validCatalogPublicID(publicID) || (assetKind != "icon" && assetKind != "icon-small" && assetKind != "render") {
		writeError(w, http.StatusBadRequest, "catalog resource asset path is invalid")
		return
	}
	versionColumn := "detail.icon_file_id"
	definitionColumn := "definition.icon_file_id"
	if assetKind == "icon-small" {
		versionColumn = "detail.icon_small_file_id"
	} else if assetKind == "render" {
		versionColumn = "detail.render_file_id"
		definitionColumn = "definition.render_file_id"
	}
	var objectKey, contentType string
	versionPublicID := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("version")))
	if versionPublicID != "" && !validCatalogPublicID(versionPublicID) {
		writeError(w, http.StatusBadRequest, "catalog resource version is invalid")
		return
	}
	err := s.db.QueryRow(r.Context(), `select file.object_key,file.content_type
		from catalog_entities entity
		join mod_resource_version_details detail on detail.resource_id=entity.id and detail.status='active'
		join mod_content_versions version on version.id=detail.version_id and version.status='active'
		join mods source_mod on source_mod.id=version.mod_id and source_mod.review_status='approved'
		join oss_files file on file.id=`+versionColumn+` and file.status='active'
		  and file.scan_status in ('clean','trusted_generated')
		where entity.public_id=$1 and entity.entity_type='resource' and entity.status='active'
		  and entity.archived_at is null and ($2='' or version.public_id=$2)
		order by case when version.public_id=$2 then 0 else 1 end,detail.updated_at desc
		limit 1`, publicID, versionPublicID).Scan(&objectKey, &contentType)
	if errors.Is(err, pgx.ErrNoRows) && assetKind == "icon-small" {
		err = s.db.QueryRow(r.Context(), `select file.object_key,file.content_type
			from catalog_entities entity
			join mod_resource_version_details detail on detail.resource_id=entity.id and detail.status='active'
			join mod_content_versions version on version.id=detail.version_id and version.status='active'
		join mods source_mod on source_mod.id=version.mod_id and source_mod.review_status='approved'
			join oss_files file on file.id=detail.icon_file_id and file.status='active'
			  and file.scan_status in ('clean','trusted_generated')
			where entity.public_id=$1 and entity.entity_type='resource' and entity.status='active'
			  and entity.archived_at is null and ($2='' or version.public_id=$2)
			order by case when version.public_id=$2 then 0 else 1 end,detail.updated_at desc
			limit 1`, publicID, versionPublicID).Scan(&objectKey, &contentType)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.db.QueryRow(r.Context(), `select file.object_key,file.content_type
		from catalog_entities entity
		join catalog_resource_definitions definition on definition.resource_id=entity.id
		join oss_files file on file.id=`+definitionColumn+`
		where entity.public_id=$1 and entity.entity_type='resource' and entity.status='active'
		  and entity.archived_at is null and file.status='active'
		  and file.scan_status in ('clean','trusted_generated')`, publicID).Scan(&objectKey, &contentType)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		snapshotColumn := "snapshot.icon_path"
		if assetKind == "render" {
			snapshotColumn = "snapshot.preview_path"
		}
		var revisionID, assetPath string
		err = s.db.QueryRow(r.Context(), `with `+latestGlobalExportScopeCTE+`, `+latestGlobalResourceSnapshotCTE+`
			select snapshot.revision_id::text,`+snapshotColumn+`
			from catalog_entities entity
			join latest_resource_snapshots snapshot on snapshot.resource_id=entity.id
			where entity.public_id=$1 and entity.entity_type='resource' and entity.status='active'
			  and entity.archived_at is null and `+snapshotColumn+`<>''`, publicID).Scan(&revisionID, &assetPath)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "catalog resource asset does not exist")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load imported catalog resource asset")
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		s.redirectModExportMedia(w, r, revisionID, assetPath)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load catalog resource asset")
		return
	}
	s.redirectCatalogOSSAsset(w, r, objectKey, contentType)
}

func (s *Server) catalogRecipeTemplateBackground(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "recipe template publicId is invalid")
		return
	}
	var objectKey, contentType string
	err := s.db.QueryRow(r.Context(), `select file.object_key,file.content_type
		from catalog_entities entity
		join recipe_layout_templates template on template.entity_id=entity.id
		join catalog_entities type_entity on type_entity.id=template.recipe_type_id
		join oss_files file on file.id=template.background_file_id
		where entity.public_id=$1 and entity.entity_type='recipe_template' and entity.status='active'
		  and entity.archived_at is null and type_entity.status='active' and type_entity.archived_at is null
		  and file.status='active' and file.scan_status in ('clean','trusted_generated')`, publicID).Scan(&objectKey, &contentType)
	if errors.Is(err, pgx.ErrNoRows) {
		var revisionID, assetPath string
		err = s.db.QueryRow(r.Context(), `select snapshot.revision_id,snapshot.background_path
			from catalog_entities entity join recipe_layout_templates template on template.entity_id=entity.id
			join catalog_entities type_entity on type_entity.id=template.recipe_type_id
			join recipe_template_import_snapshots snapshot on snapshot.id=template.import_snapshot_id
			join catalog_import_revisions revision on revision.id=snapshot.revision_id and revision.status in ('ready','partial')
			join mods source_mod on source_mod.id=revision.mod_id and source_mod.review_status='approved'
			where entity.public_id=$1 and entity.entity_type='recipe_template' and entity.status='active'
			  and entity.archived_at is null and type_entity.status='active' and type_entity.archived_at is null
			  and template.background_file_id is null`, publicID).
			Scan(&revisionID, &assetPath)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "recipe template background does not exist")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load imported recipe template background")
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		s.redirectModExportMedia(w, r, revisionID, assetPath)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load recipe template background")
		return
	}
	s.redirectCatalogOSSAsset(w, r, objectKey, contentType)
}

func (s *Server) redirectCatalogOSSAsset(w http.ResponseWriter, r *http.Request, objectKey, contentType string) {
	if !safeRasterContentType(contentType) {
		writeError(w, http.StatusUnsupportedMediaType, "catalog asset is not an image")
		return
	}
	cfg := s.ossConfigFromSettings(r.Context())
	ttl := time.Duration(cfg.DownloadURLTTLMinutes) * time.Minute
	if ttl <= 0 || ttl > time.Hour {
		ttl = 15 * time.Minute
	}
	s.redirectOSSObjectAccess(w, r, objectKey, ossObjectAccessOptions{Expires: ttl})
}
