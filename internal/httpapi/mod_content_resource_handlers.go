package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *Server) submitNewModContentSection(w http.ResponseWriter, r *http.Request, identity modIdentityRecord, edit modContentSectionEdit) {
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start section revision")
		return
	}
	defer tx.Rollback(r.Context())
	var versionID int64
	if err = tx.QueryRow(r.Context(), `select id from mod_content_versions where public_id=$1 and mod_id=$2 and status='active'`, edit.VersionPublicID, identity.ID).Scan(&versionID); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "mod content version not found")
		return
	}
	var templateID int64
	err = tx.QueryRow(r.Context(), `select id from mod_content_templates where public_id=$1 and status='active' and (builtin or owner_mod_id=$2)`, edit.TemplatePublicID, identity.ID).Scan(&templateID)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "content template not found")
		return
	}
	var parentID *int64
	if edit.ParentPublicID != "" {
		var value int64
		if err = tx.QueryRow(r.Context(), `select id from mod_content_sections where public_id=$1 and mod_id=$2 and version_id=$3 and status='active'`, edit.ParentPublicID, identity.ID, versionID).Scan(&value); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "parent section not found")
			return
		}
		parentID = &value
	}
	var publicID string
	err = tx.QueryRow(r.Context(), `insert into mod_content_sections(mod_id,version_id,template_id,parent_id,default_locale,display_mode,ordinal,status,created_by,updated_by)
		values($1,$2,$3,$4,$5,$6,$7,'pending',$8,$8) returning public_id`, identity.ID, versionID, templateID, parentID, edit.DefaultLocale, edit.DisplayMode, edit.Ordinal, currentClaims(r).Subject).Scan(&publicID)
	if err != nil {
		writeError(w, http.StatusConflict, "failed to reserve content section")
		return
	}
	snapshot := modContentSnapshot{Kind: "section", Operation: "create", ModID: identity.ID, ModSiteID: identity.SiteID, PublicID: publicID, Section: &edit}
	result, err := s.createModContentRevisionTx(r, tx, identity, snapshot, nil)
	if err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, http.StatusInternalServerError, "failed to submit content section")
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) deleteModContentSection(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requireEditableMod(w, r)
	if !ok {
		return
	}
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("sectionId")))
	var status string
	var publishedRevisionID *int64
	if err := s.db.QueryRow(r.Context(), `select status,published_revision_id from mod_content_sections where public_id=$1 and mod_id=$2`, publicID, identity.ID).
		Scan(&status, &publishedRevisionID); err != nil {
		writeError(w, http.StatusNotFound, "content section not found")
		return
	}
	if status != "active" {
		writeError(w, http.StatusConflict, "content section is not editable")
		return
	}
	s.submitExistingModContentMutation(w, r, identity, modContentSnapshot{Kind: "section", Operation: "delete", ModID: identity.ID, ModSiteID: identity.SiteID, PublicID: publicID}, publishedRevisionID)
}

func (s *Server) modContentResources(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requireEditableMod(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		var edit modContentResourceEdit
		if decodeJSON(r, &edit) != nil || edit.BaseRevisionID != nil || normalizeModContentResourceEdit(&edit) != nil {
			writeError(w, http.StatusUnprocessableEntity, "invalid mod resource detail")
			return
		}
		s.submitNewModContentResource(w, r, identity, edit)
		return
	}
	rows, err := s.db.Query(r.Context(), `select entity.public_id,resource.kind_code,resource.canonical_id,
		coalesce((select jsonb_agg(jsonb_build_object('versionPublicId',version.public_id,'entryTypeCode',detail.entry_type_code,'definitionSchemaVersion',detail.definition_schema_version,'defaultLocale',detail.default_locale,
		'sectionPublicId',coalesce((select section.public_id from mod_content_section_resources member
		 join mod_content_sections section on section.id=member.section_id and section.version_id=member.version_id
		 where member.resource_id=resource.entity_id and member.version_id=version.id and section.status='active'
		 order by section.ordinal,section.id limit 1),''),
		'definition',detail.definition,'status',detail.status,'publishedRevisionId',
		(select revision.public_id from content_revisions revision where revision.id=detail.published_revision_id),
		'iconSmallFilePublicId',coalesce((select file.public_id from oss_files file where file.id=detail.icon_small_file_id and file.status='active'
		 and file.scan_status in ('clean','trusted_generated')),''),
		'iconFilePublicId',coalesce((select file.public_id from oss_files file where file.id=detail.icon_file_id and file.status='active'
		 and file.scan_status in ('clean','trusted_generated')),''),
		'renderFilePublicId',coalesce((select file.public_id from oss_files file where file.id=detail.render_file_id and file.status='active'
		 and file.scan_status in ('clean','trusted_generated')),''),
		'localizations',coalesce((select jsonb_agg(jsonb_build_object('locale',localization.locale,'name',localization.name,
		'summary',localization.summary,'contentMarkdown',localization.content_markdown,'provenance',localization.provenance)
		order by localization.locale) from mod_resource_version_detail_localizations localization
		where localization.resource_id=resource.entity_id and localization.version_id=version.id),'[]'::jsonb)) order by version.updated_at desc)
		from mod_resource_version_details detail join mod_content_versions version on version.id=detail.version_id and version.status='active'
		where detail.resource_id=resource.entity_id and detail.status='active'),'[]'::jsonb)
		from mod_resource_bindings binding join game_resources resource on resource.entity_id=binding.resource_id
		join catalog_entities entity on entity.id=resource.entity_id where binding.mod_id=$1
		and entity.status='active' and entity.archived_at is null
		and exists(select 1 from mod_resource_version_details detail where detail.resource_id=binding.resource_id and detail.status='active')
		order by resource.updated_at desc`, identity.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read mod resource details")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var publicID, kindCode, canonicalID string
		var details []byte
		if err = rows.Scan(&publicID, &kindCode, &canonicalID, &details); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode mod resource details")
			return
		}
		items = append(items, map[string]any{"publicId": publicID, "kindCode": kindCode, "canonicalId": canonicalID,
			"details": json.RawMessage(details)})
	}
	if err = finishRows(rows); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read mod resource details")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) submitNewModContentResource(w http.ResponseWriter, r *http.Request, identity modIdentityRecord, edit modContentResourceEdit) {
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start resource detail revision")
		return
	}
	defer tx.Rollback(r.Context())
	var resourceID int64
	publicID := edit.ResourcePublicID
	createdIdentity := false
	var validKind bool
	if err = tx.QueryRow(r.Context(), `select exists(select 1 from resource_kinds where code=$1 and user_visible)`, edit.KindCode).Scan(&validKind); err != nil || !validKind {
		writeError(w, http.StatusUnprocessableEntity, "resource kind is unavailable")
		return
	}
	if publicID != "" {
		err = tx.QueryRow(r.Context(), `select resource.entity_id,resource.kind_code,resource.canonical_id
			from catalog_entities entity join game_resources resource on resource.entity_id=entity.id
			where entity.public_id=$1 and entity.status='active' and (resource.owner_mod_id=$2 or resource.owner_mod_id is null)`,
			publicID, identity.ID).Scan(&resourceID, &edit.KindCode, &edit.CanonicalID)
	} else {
		resolver, resolveErr := loadCatalogResourceIdentityResolver(r.Context(), tx)
		if resolveErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve resource identity")
			return
		}
		resolved := resolver.resolve(edit.KindCode, edit.CanonicalID)
		publicID = resolved.PublicID
		var entityStatus string
		err = tx.QueryRow(r.Context(), `insert into catalog_entities(identity_key,public_id,entity_type,status)
			values($1,$2,'resource','placeholder')
			on conflict(identity_key) do update set updated_at=catalog_entities.updated_at
			returning id,public_id,status`, resolved.ID, publicID).Scan(&resourceID, &publicID, &entityStatus)
		if err == nil {
			createdIdentity = entityStatus == "placeholder"
			_, err = tx.Exec(r.Context(), `insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,owner_mod_id,resolved)
				values($1,$2,$3,$4,$5,$6,true) on conflict(entity_id) do nothing`, resourceID, edit.KindCode, resolved.CanonicalID, resolved.Namespace, resolved.ResourcePath, identity.ID)
		}
		if err == nil {
			err = tx.QueryRow(r.Context(), `select entity.public_id,entity.status from catalog_entities entity join game_resources resource on resource.entity_id=entity.id
				where entity.id=$1 and entity.entity_type='resource' and resource.kind_code=$2 and resource.canonical_id=$3
				and (resource.owner_mod_id=$4 or resource.owner_mod_id is null)`, resourceID, edit.KindCode, resolved.CanonicalID, identity.ID).Scan(&publicID, &entityStatus)
			createdIdentity = createdIdentity || entityStatus == "placeholder"
		}
	}
	if err != nil || resourceID <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "resource identity is unavailable for this mod")
		return
	}
	var versionID int64
	if err = tx.QueryRow(r.Context(), `select id from mod_content_versions where mod_id=$1 and status='active' and public_id=$2`, identity.ID, edit.VersionPublicID).Scan(&versionID); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "mod content version is invalid")
		return
	}
	if err = validateModContentResourceSection(r.Context(), tx, identity.ID, versionID, edit.KindCode, edit.SectionPublicID); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "the selected content category cannot contain this resource")
		return
	}
	if err = validateModContentEditableDefinitionPatch(
		r.Context(), tx, identity.ID, versionID, edit.KindCode, edit.SectionPublicID, edit.EntryTypeCode, edit.Definition, false,
	); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "the selected resource subtype or its fields are invalid")
		return
	}
	canonicalDefinition, normalizeErr := normalizeModContentEntryDefinition(
		r.Context(), tx, identity.ID, versionID, edit.KindCode, edit.SectionPublicID, edit.EntryTypeCode, edit.Definition, false, false,
	)
	if normalizeErr != nil {
		writeError(w, http.StatusUnprocessableEntity, "the selected resource subtype or its fields are invalid")
		return
	}
	edit.Definition = canonicalDefinition
	if _, err = tx.Exec(r.Context(), `insert into mod_resource_bindings(resource_id,mod_id) values($1,$2) on conflict(resource_id) do nothing`, resourceID, identity.ID); err != nil {
		writeError(w, http.StatusConflict, "resource identity cannot be bound to this mod")
		return
	}
	var boundModID int64
	if err = tx.QueryRow(r.Context(), `select mod_id from mod_resource_bindings where resource_id=$1`, resourceID).Scan(&boundModID); err != nil || boundModID != identity.ID {
		writeError(w, http.StatusConflict, "resource identity belongs to another mod")
		return
	}
	actorID := currentClaims(r).Subject
	iconSmallFileID, err := resolveModContentImageFileID(r.Context(), tx, edit.IconSmallFilePublicID, actorID, resourceID, versionID, false, "icon_32")
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "resource 32px icon is unavailable")
		return
	}
	iconFileID, err := resolveModContentImageFileID(r.Context(), tx, edit.IconFilePublicID, actorID, resourceID, versionID, false, "icon_128")
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "resource icon is unavailable")
		return
	}
	renderFileID, err := resolveModContentImageFileID(r.Context(), tx, edit.RenderFilePublicID, actorID, resourceID, versionID, false, "render")
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "resource render image is unavailable")
		return
	}
	definition, _ := json.Marshal(edit.Definition)
	err = reserveModContentResourceDetailWithSubtypeTx(r.Context(), tx, resourceID, versionID, identity.ID, edit.EntryTypeCode, edit.DefaultLocale, definition, iconSmallFileID, iconFileID, renderFileID, actorID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "this resource already has detail content for the selected version")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reserve resource detail")
		return
	}
	edit.ResourcePublicID = publicID
	snapshot := modContentSnapshot{Kind: "resource", Operation: "create", ModID: identity.ID, ModSiteID: identity.SiteID, PublicID: publicID, Resource: &edit, CreatedIdentity: createdIdentity}
	result, err := s.createModContentRevisionTx(r, tx, identity, snapshot, nil)
	if err != nil {
		log.Printf("submit new mod resource failed: site=%s public_id=%s canonical_id=%s: %v",
			identity.SiteID, publicID, edit.CanonicalID, err)
		writeError(w, http.StatusInternalServerError, "failed to submit resource detail")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		log.Printf("commit new mod resource failed: site=%s public_id=%s canonical_id=%s: %v",
			identity.SiteID, publicID, edit.CanonicalID, err)
		writeError(w, http.StatusInternalServerError, "failed to submit resource detail")
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) modContentSimilarResources(w http.ResponseWriter, r *http.Request) {
	identity, err := s.modIdentity(r.Context(), r.PathValue("siteId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "mod not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read mod")
		return
	}
	resourcePublicID := strings.ToLower(strings.TrimSpace(r.PathValue("resourceId")))
	versionPublicID := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("version")))
	if !modContentPublicIDPattern.MatchString(resourcePublicID) || !modContentPublicIDPattern.MatchString(versionPublicID) {
		writeError(w, http.StatusBadRequest, "invalid resource version")
		return
	}
	var versionID int64
	var similarGroupID string
	err = s.db.QueryRow(r.Context(), `select version.id,placement.similar_group_id
		from catalog_entities entity
		join mod_content_section_resources placement on placement.resource_id=entity.id
		join mod_content_versions version on version.id=placement.version_id and version.status='active'
		join mod_content_sections section on section.id=placement.section_id and section.mod_id=version.mod_id and section.status='active'
		where entity.public_id=$1 and version.public_id=$2 and version.mod_id=$3
		order by section.ordinal,section.id limit 1`, resourcePublicID, versionPublicID, identity.ID).
		Scan(&versionID, &similarGroupID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "mod resource version placement not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read similar resource group")
		return
	}
	if similarGroupID == "" {
		writeJSON(w, http.StatusOK, map[string]any{"groupId": "", "items": []map[string]any{}})
		return
	}
	primary, secondary := s.requestContentLocales(r)
	localeCandidates := []string{strings.ToLower(normalizeContentLocale(primary)), strings.ToLower(normalizeContentLocale(secondary)), "en", "en-us", "zh-cn", "zh-tw"}
	rows, err := s.db.Query(r.Context(), `select entity.public_id,resource.kind_code,resource.canonical_id,
		coalesce(icon_file.public_id,''),coalesce(imported.revision_id,''),coalesce(imported.icon_path,''),
		coalesce(nullif(version_names.names,'{}'::jsonb),nullif((select jsonb_object_agg(name.key,name.value)
		 from jsonb_each_text(coalesce(imported.names,'{}'::jsonb)) name
		 where replace(lower(name.key),'_','-')=any($3::text[])),'{}'::jsonb),'{}'::jsonb)
		from mod_content_section_resources placement
		join mod_content_sections section on section.id=placement.section_id and section.status='active'
		join catalog_entities entity on entity.id=placement.resource_id and entity.status='active' and entity.archived_at is null
		join game_resources resource on resource.entity_id=placement.resource_id
		left join mod_resource_version_details detail on detail.resource_id=placement.resource_id and detail.version_id=$1 and detail.status='active'
		left join oss_files icon_file on icon_file.id=detail.icon_file_id and icon_file.status='active'
		 and icon_file.scan_status in ('clean','trusted_generated')
		left join lateral (select revision.public_id revision_id,snapshot.icon_path,snapshot.names
		 from resource_import_snapshots snapshot join catalog_import_revisions revision on revision.id=snapshot.revision_id
		 where snapshot.resource_id=placement.resource_id and revision.target_version_id=$1 and revision.is_active
		 and revision.status in ('ready','partial') order by (snapshot.icon_path<>'') desc,revision.created_at desc limit 1) imported on true
		left join lateral (select jsonb_object_agg(localization.locale,localization.name) names
		 from mod_resource_version_detail_localizations localization
		 where localization.resource_id=placement.resource_id and localization.version_id=$1
		 and localization.name<>'' and replace(lower(localization.locale),'_','-')=any($3::text[])) version_names on true
		where placement.version_id=$1 and placement.similar_group_id=$2 and section.mod_id=$4
		order by section.ordinal,placement.ordinal,resource.canonical_id`, versionID, similarGroupID, localeCandidates, identity.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read similar resources")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var publicID, kindCode, canonicalID, iconFileID, revisionID, iconPath string
		var names []byte
		if err = rows.Scan(&publicID, &kindCode, &canonicalID, &iconFileID, &revisionID, &iconPath, &names); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode similar resources")
			return
		}
		items = append(items, map[string]any{"resourcePublicId": publicID, "versionPublicId": versionPublicID,
			"kindCode": kindCode, "canonicalId": canonicalID, "iconFileId": iconFileID,
			"revisionId": revisionID, "iconPath": iconPath, "names": json.RawMessage(names)})
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read similar resources")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groupId": similarGroupID, "items": items})
}

func (s *Server) modContentResource(w http.ResponseWriter, r *http.Request) {
	var identity modIdentityRecord
	if r.Method == http.MethodGet {
		var err error
		identity, err = s.modIdentity(r.Context(), r.PathValue("siteId"))
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "mod not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read mod")
			return
		}
	} else {
		var ok bool
		identity, ok = s.requireEditableMod(w, r)
		if !ok {
			return
		}
	}
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("resourceId")))
	if r.Method == http.MethodGet {
		var entityID int64
		var kindCode, canonicalID string
		var details []byte
		claims := currentClaims(r)
		canPreviewInactive := canEditMod(claims, identity) || claimsAllow(claims, "content.review")
		if err := s.db.QueryRow(r.Context(), `select resource.entity_id,resource.kind_code,resource.canonical_id,
			coalesce((select jsonb_agg(jsonb_build_object('versionPublicId',version.public_id,'entryTypeCode',detail.entry_type_code,'definitionSchemaVersion',detail.definition_schema_version,'defaultLocale',detail.default_locale,
			'schemaDefinition',coalesce((with recursive lineage as (
				select section.id,section.parent_id,section.template_id
				from mod_content_section_resources member
				join mod_content_sections section on section.id=member.section_id and section.version_id=member.version_id
				where member.resource_id=resource.entity_id and member.version_id=version.id and section.status='active'
				union all
				select parent.id,parent.parent_id,parent.template_id
				from mod_content_sections parent join lineage child on child.parent_id=parent.id
				where parent.status='active'
			) select template.definition
				from lineage root join mod_content_templates template on template.id=root.template_id and template.status='active'
				where root.parent_id is null order by root.id limit 1),'{}'::jsonb),
			'sourceRevisionId',coalesce((select import_revision.id from catalog_import_revisions import_revision
			 where import_revision.target_version_id=version.id and import_revision.is_active and import_revision.status in ('ready','partial')
			 order by coalesce(import_revision.activated_at,import_revision.created_at) desc limit 1),''),
			'sectionPublicId',coalesce((select section.public_id from mod_content_section_resources member
			 join mod_content_sections section on section.id=member.section_id and section.version_id=member.version_id
			 where member.resource_id=resource.entity_id and member.version_id=version.id and section.status='active'
			 order by section.ordinal,section.id limit 1),''),
			'definition',detail.definition,'status',detail.status,'publishedRevisionId',
			(select revision.public_id from content_revisions revision where revision.id=detail.published_revision_id),
			'iconSmallFilePublicId',coalesce((select file.public_id from oss_files file where file.id=detail.icon_small_file_id and file.status='active'
			 and file.scan_status in ('clean','trusted_generated')),''),
			'iconFilePublicId',coalesce((select file.public_id from oss_files file where file.id=detail.icon_file_id and file.status='active'
			 and file.scan_status in ('clean','trusted_generated')),''),
			'renderFilePublicId',coalesce((select file.public_id from oss_files file where file.id=detail.render_file_id and file.status='active'
			 and file.scan_status in ('clean','trusted_generated')),''),
			'localizations',coalesce((select jsonb_agg(jsonb_build_object('locale',localization.locale,'name',localization.name,
			'summary',localization.summary,'contentMarkdown',localization.content_markdown,'provenance',localization.provenance)
			order by localization.locale) from mod_resource_version_detail_localizations localization
			where localization.resource_id=resource.entity_id and localization.version_id=version.id),'[]'::jsonb)) order by version.updated_at desc)
			from mod_resource_version_details detail join mod_content_versions version on version.id=detail.version_id
			where detail.resource_id=resource.entity_id
			  and (detail.status='active' or ($3 and detail.status='pending'))
			  and (version.status='active' or $3)),'[]'::jsonb)
			from mod_resource_bindings binding join game_resources resource on resource.entity_id=binding.resource_id
			join catalog_entities entity on entity.id=resource.entity_id where entity.public_id=$1 and binding.mod_id=$2
			  and ((entity.status='active' and entity.archived_at is null and exists(
			    select 1 from mod_resource_version_details published_detail
			    join mod_content_versions published_version on published_version.id=published_detail.version_id and published_version.status='active'
			    where published_detail.resource_id=resource.entity_id and published_detail.status='active'
			  )) or (entity.status='active' and entity.archived_at is null and exists(
			    select 1 from resource_import_snapshots imported
			    join catalog_import_revisions import_revision on import_revision.id=imported.revision_id and import_revision.is_active
			    join mod_content_versions import_version on import_version.id=import_revision.target_version_id
			      and import_version.status='active' and import_version.mod_id=binding.mod_id
			    where imported.resource_id=resource.entity_id
			  )) or $3)`, publicID, identity.ID, canPreviewInactive).
			Scan(&entityID, &kindCode, &canonicalID, &details); err != nil {
			writeError(w, http.StatusNotFound, "mod resource detail not found")
			return
		}
		primary, secondary := s.requestContentLocales(r)
		decoratedDetails, decorationErr := s.decorateModContentResourceDetailReferences(r.Context(), details, primary, secondary)
		if decorationErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve resource detail references")
			return
		}
		details = decoratedDetails
		carrier := map[string]any{"entityId": publicID, "publicId": publicID, "versions": []map[string]any{}}
		if err := s.decorateResourceVersionRows(r.Context(), []map[string]any{carrier}, primary, secondary); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve resource versions")
			return
		}
		markModContentCapabilityResponse(w)
		writeJSON(w, http.StatusOK, map[string]any{"entityId": publicID, "publicId": publicID, "kindCode": kindCode,
			"canonicalId": canonicalID, "details": json.RawMessage(details), "versions": carrier["versions"],
			"capabilities": modContentCapabilitiesFor(currentClaims(r), identity)})
		return
	}
	if r.Method == http.MethodDelete {
		versionPublicID := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("version")))
		var publishedRevisionID *int64
		if err := s.db.QueryRow(r.Context(), `select detail.published_revision_id from mod_resource_version_details detail
			join mod_content_versions version on version.id=detail.version_id and version.status='active'
			join catalog_entities entity on entity.id=detail.resource_id
			where entity.public_id=$1 and version.public_id=$2 and version.mod_id=$3 and detail.status='active'`, publicID, versionPublicID, identity.ID).Scan(&publishedRevisionID); err != nil {
			writeError(w, http.StatusNotFound, "mod resource version detail not found")
			return
		}
		edit := modContentResourceEdit{ResourcePublicID: publicID, VersionPublicID: versionPublicID}
		s.submitExistingModContentMutation(w, r, identity, modContentSnapshot{Kind: "resource", Operation: "delete", ModID: identity.ID, ModSiteID: identity.SiteID, PublicID: publicID, Resource: &edit}, publishedRevisionID)
		return
	}
	var edit modContentResourceEdit
	if decodeJSON(r, &edit) != nil || normalizeModContentResourceUpdateEdit(&edit) != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid or stale mod resource detail")
		return
	}
	var publishedRevisionID *int64
	var resourceID int64
	var actualKindCode, actualCanonicalID, currentIconSmallFilePublicID, currentIconFilePublicID, currentRenderFilePublicID string
	var currentDefinition []byte
	if err := s.db.QueryRow(r.Context(), `select detail.published_revision_id,resource.entity_id,resource.kind_code,resource.canonical_id,
		coalesce(icon_small_file.public_id,''),coalesce(icon_file.public_id,''),coalesce(render_file.public_id,''),detail.definition
		from mod_resource_version_details detail
		join mod_content_versions version on version.id=detail.version_id and version.status='active'
		join catalog_entities entity on entity.id=detail.resource_id
		join game_resources resource on resource.entity_id=detail.resource_id
		left join oss_files icon_small_file on icon_small_file.id=detail.icon_small_file_id and icon_small_file.status='active'
		 and icon_small_file.scan_status in ('clean','trusted_generated')
		left join oss_files icon_file on icon_file.id=detail.icon_file_id and icon_file.status='active'
		 and icon_file.scan_status in ('clean','trusted_generated')
		left join oss_files render_file on render_file.id=detail.render_file_id and render_file.status='active'
		 and render_file.scan_status in ('clean','trusted_generated')
		where entity.public_id=$1 and version.public_id=$2 and version.mod_id=$3 and detail.status='active'`,
		publicID, edit.VersionPublicID, identity.ID).Scan(&publishedRevisionID, &resourceID, &actualKindCode, &actualCanonicalID, &currentIconSmallFilePublicID, &currentIconFilePublicID, &currentRenderFilePublicID, &currentDefinition); err != nil {
		writeError(w, http.StatusNotFound, "mod resource version detail not found")
		return
	}
	requestedBaseRevisionID, baseErr := resolveRevisionPublicID(r.Context(), s.db, edit.BaseRevisionID)
	if baseErr != nil || !sameRevision(requestedBaseRevisionID, publishedRevisionID) {
		writeError(w, http.StatusConflict, "mod resource version detail changed; reload the editor")
		return
	}
	edit.KindCode, edit.CanonicalID = actualKindCode, actualCanonicalID
	var versionID int64
	if err := s.db.QueryRow(r.Context(), `select id from mod_content_versions where public_id=$1 and mod_id=$2 and status='active'`, edit.VersionPublicID, identity.ID).Scan(&versionID); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "mod content version is invalid")
		return
	}
	existingDefaultLocale, existingLocalizations, err := loadModContentResourceLocalizationState(r.Context(), s.db, resourceID, versionID, false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read existing resource localizations")
		return
	}
	if err = preserveImmutableModContentResourceLocales(existingDefaultLocale, existingLocalizations, &edit); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "non-editable resource localizations must be preserved unchanged")
		return
	}
	if edit.IconSmallFilePublicID == nil {
		edit.IconSmallFilePublicID = &currentIconSmallFilePublicID
	}
	if edit.IconFilePublicID == nil {
		edit.IconFilePublicID = &currentIconFilePublicID
	}
	if edit.RenderFilePublicID == nil {
		edit.RenderFilePublicID = &currentRenderFilePublicID
	}
	actorID := currentClaims(r).Subject
	if _, err := resolveModContentImageFileID(r.Context(), s.db, edit.IconSmallFilePublicID, actorID, resourceID, versionID, false, "icon_32"); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "resource 32px icon is unavailable")
		return
	}
	if _, err := resolveModContentImageFileID(r.Context(), s.db, edit.IconFilePublicID, actorID, resourceID, versionID, false, "icon_128"); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "resource icon is unavailable")
		return
	}
	if _, err := resolveModContentImageFileID(r.Context(), s.db, edit.RenderFilePublicID, actorID, resourceID, versionID, false, "render"); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "resource render image is unavailable")
		return
	}
	if err := validateModContentResourceSection(r.Context(), s.db, identity.ID, versionID, edit.KindCode, edit.SectionPublicID); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "the selected content category cannot contain this resource")
		return
	}
	if err := validateModContentEditableDefinitionPatch(
		r.Context(), s.db, identity.ID, versionID, edit.KindCode, edit.SectionPublicID, edit.EntryTypeCode, edit.Definition, true,
	); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "the selected resource subtype or its fields are invalid")
		return
	}
	var storedDefinition map[string]any
	if err := json.Unmarshal(currentDefinition, &storedDefinition); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decode current resource definition")
		return
	}
	effectiveDefinition := mergeModContentDefinitionPatch(storedDefinition, edit.Definition)
	canonicalDefinition, normalizeErr := normalizeModContentEntryDefinition(
		r.Context(), s.db, identity.ID, versionID, edit.KindCode, edit.SectionPublicID, edit.EntryTypeCode, effectiveDefinition, false, true,
	)
	if normalizeErr != nil {
		writeError(w, http.StatusUnprocessableEntity, "the selected resource subtype or its fields are invalid")
		return
	}
	edit.Definition = canonicalDefinition
	edit.ResourcePublicID = publicID
	s.submitExistingModContentMutation(w, r, identity, modContentSnapshot{Kind: "resource", Operation: "edit", ModID: identity.ID, ModSiteID: identity.SiteID, PublicID: publicID, Resource: &edit}, publishedRevisionID)
}

func (s *Server) decorateModContentResourceDetailReferences(ctx context.Context, raw []byte, locales ...string) ([]byte, error) {
	var details []map[string]any
	if err := json.Unmarshal(raw, &details); err != nil {
		return nil, err
	}
	for _, detail := range details {
		definition, _ := detail["definition"].(map[string]any)
		if definition == nil {
			continue
		}
		revisionID, _ := detail["sourceRevisionId"].(string)
		items := []map[string]any{{"data": definition}}
		if err := s.decorateCanonicalDefinitionReferences(ctx, revisionID, items, locales...); err != nil {
			return nil, err
		}
		detail["definition"] = items[0]["data"]
		delete(detail, "sourceRevisionId")
	}
	return json.Marshal(details)
}

func publishModContentSnapshotTx(ctx context.Context, tx pgx.Tx, revisionID int64, snapshot modContentSnapshot, actorID int64) error {
	if snapshot.Operation == "delete" {
		table := "mod_content_versions"
		if snapshot.Kind == "template" {
			table = "mod_content_templates"
			var templateID int64
			if err := tx.QueryRow(ctx, `select id from mod_content_templates
				where public_id=$1 and owner_mod_id=$2 and not builtin for update`, snapshot.PublicID, snapshot.ModID).Scan(&templateID); err != nil {
				return err
			}
			if err := validateModContentTemplateDeletionTx(ctx, tx, templateID); err != nil {
				return err
			}
		} else if snapshot.Kind == "section" {
			return archiveModContentSectionTreeTx(ctx, tx, snapshot.PublicID, snapshot.ModID, revisionID, actorID)
		} else if snapshot.Kind == "resource" {
			if snapshot.Resource == nil {
				return errCatalogEditorInvalid
			}
			_, err := tx.Exec(ctx, `update mod_resource_version_details detail set status='archived',published_revision_id=$3,updated_at=now()
				from catalog_entities entity,mod_content_versions version where entity.public_id=$1 and detail.resource_id=entity.id
				and version.public_id=$2 and detail.version_id=version.id and version.mod_id=$4`, snapshot.PublicID, snapshot.Resource.VersionPublicID, revisionID, snapshot.ModID)
			return err
		}
		if snapshot.Kind == "version" {
			var versionID int64
			if err := tx.QueryRow(ctx, `update mod_content_versions set status='archived',published_revision_id=$2,updated_at=now()
				where public_id=$1 and mod_id=$3 returning id`, snapshot.PublicID, revisionID, snapshot.ModID).Scan(&versionID); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `update catalog_import_revisions set is_active=false,status='superseded'
				where target_version_id=$1 and is_active`, versionID)
			return err
		}
		_, err := tx.Exec(ctx, `update `+table+` set status='archived',published_revision_id=$2,updated_at=now() where public_id=$1`, snapshot.PublicID, revisionID)
		return err
	}
	switch snapshot.Kind {
	case "version":
		if snapshot.Version == nil {
			return errCatalogEditorInvalid
		}
		_, err := tx.Exec(ctx, `update mod_content_versions set label=$2,minecraft_versions=$3,loaders=$4,mod_version=$5,
			status='active',published_revision_id=$6,updated_by=$7,updated_at=now() where public_id=$1 and mod_id=$8`,
			snapshot.PublicID, snapshot.Version.Label, snapshot.Version.MinecraftVersions, snapshot.Version.Loaders, snapshot.Version.ModVersion, revisionID, actorID, snapshot.ModID)
		return err
	case "template":
		if snapshot.Template == nil {
			return errCatalogEditorInvalid
		}
		definition, _ := json.Marshal(snapshot.Template.Definition)
		var nextSchema modContentTemplateDefinition
		if err := json.Unmarshal(definition, &nextSchema); err != nil {
			return errCatalogEditorInvalid
		}
		var templateID int64
		var currentDefinition []byte
		if err := tx.QueryRow(ctx, `select id,definition from mod_content_templates
			where public_id=$1 and owner_mod_id=$2 and not builtin for update`, snapshot.PublicID, snapshot.ModID).Scan(&templateID, &currentDefinition); err != nil {
			return err
		}
		var currentSchema modContentTemplateDefinition
		if err := json.Unmarshal(currentDefinition, &currentSchema); err != nil {
			return err
		}
		if err := validateModContentTemplateSchemaChangeTx(ctx, tx, templateID, currentSchema, nextSchema); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `update mod_content_templates set code=$2,default_locale=$3,default_display_mode=$4,definition=$5::jsonb,status='active',published_revision_id=$6,updated_at=now()
			where id=$1`, templateID, snapshot.Template.Code, snapshot.Template.DefaultLocale, snapshot.Template.DefaultDisplayMode, string(definition), revisionID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `delete from mod_content_template_localizations where template_id=$1`, templateID); err != nil {
			return err
		}
		for _, localization := range snapshot.Template.Localizations {
			if _, err := tx.Exec(ctx, `insert into mod_content_template_localizations(template_id,locale,name,description) values($1,$2,$3,$4)`, templateID, localization.Locale, localization.Name, localization.Summary); err != nil {
				return err
			}
		}
		return nil
	case "section":
		if snapshot.Operation != "create" || snapshot.Section == nil || snapshot.Section.ParentPublicID != "" || len(snapshot.Section.Resources) != 0 {
			return errCatalogEditorInvalid
		}
		var versionID int64
		if err := tx.QueryRow(ctx, `select id from mod_content_versions where public_id=$1 and mod_id=$2 and status='active'`, snapshot.Section.VersionPublicID, snapshot.ModID).Scan(&versionID); err != nil {
			return err
		}
		var templateID int64
		var templateCode string
		if err := tx.QueryRow(ctx, `select id,code from mod_content_templates where public_id=$1 and status='active' and (builtin or owner_mod_id=$2)`, snapshot.Section.TemplatePublicID, snapshot.ModID).Scan(&templateID, &templateCode); err != nil {
			return err
		}
		var sectionID int64
		if err := tx.QueryRow(ctx, `select id from mod_content_sections
			where public_id=$1 and mod_id=$2 and version_id=$3 and status='pending' for update`,
			snapshot.PublicID, snapshot.ModID, versionID).Scan(&sectionID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `update mod_content_sections set template_id=$2,parent_id=null,default_locale=$3,display_mode=$4,ordinal=$5,status='active',
			published_revision_id=$6,updated_by=$7,updated_at=now() where id=$1`, sectionID,
			templateID, snapshot.Section.DefaultLocale, snapshot.Section.DisplayMode, snapshot.Section.Ordinal, revisionID, actorID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `delete from mod_content_section_localizations where section_id=$1`, sectionID); err != nil {
			return err
		}
		for _, localization := range snapshot.Section.Localizations {
			if _, err := tx.Exec(ctx, `insert into mod_content_section_localizations(section_id,locale,name,description) values($1,$2,$3,$4)`, sectionID, localization.Locale, localization.Name, localization.Summary); err != nil {
				return err
			}
		}
		if templateCode == "item_block" {
			if err := ensureItemBlockSystemCategoriesTx(
				ctx, tx, sectionID, versionID, snapshot.ModID, templateID, actorID,
				snapshot.Section.DefaultLocale, snapshot.Section.DisplayMode,
			); err != nil {
				return err
			}
		}
		return nil
	case "layout":
		if snapshot.Layout == nil {
			return errCatalogEditorInvalid
		}
		return publishModContentLayoutTx(ctx, tx, revisionID, snapshot, actorID)
	case "resource":
		if snapshot.Resource == nil {
			return errCatalogEditorInvalid
		}
		var resourceID int64
		var versionID int64
		if err := tx.QueryRow(ctx, `select id from mod_content_versions where public_id=$1 and mod_id=$2 and status='active'`, snapshot.Resource.VersionPublicID, snapshot.ModID).Scan(&versionID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `select entity.id from catalog_entities entity
			join mod_resource_version_details detail on detail.resource_id=entity.id and detail.version_id=$2
			where entity.public_id=$1`, snapshot.PublicID, versionID).Scan(&resourceID); err != nil {
			return err
		}
		existingDefaultLocale, existingLocalizations, err := loadModContentResourceLocalizationState(ctx, tx, resourceID, versionID, true)
		if err != nil {
			return err
		}
		if err = preserveImmutableModContentResourceLocales(existingDefaultLocale, existingLocalizations, snapshot.Resource); err != nil {
			return err
		}
		canonicalDefinition, normalizeErr := normalizeModContentEntryDefinition(
			ctx, tx, snapshot.ModID, versionID, snapshot.Resource.KindCode, snapshot.Resource.SectionPublicID,
			snapshot.Resource.EntryTypeCode, snapshot.Resource.Definition, false, snapshot.Operation == "edit",
		)
		if normalizeErr != nil {
			return normalizeErr
		}
		snapshot.Resource.Definition = canonicalDefinition
		definition, _ := json.Marshal(canonicalDefinition)
		if err = validateModContentEntryType(ctx, tx, snapshot.ModID, versionID, snapshot.Resource.KindCode, snapshot.Resource.SectionPublicID, snapshot.Resource.EntryTypeCode, canonicalDefinition); err != nil {
			return err
		}
		iconSmallFileID, iconSmallErr := resolveModContentImageFileID(ctx, tx, snapshot.Resource.IconSmallFilePublicID, actorID, resourceID, versionID, true, "icon_32")
		if iconSmallErr != nil {
			return iconSmallErr
		}
		iconFileID, iconErr := resolveModContentImageFileID(ctx, tx, snapshot.Resource.IconFilePublicID, actorID, resourceID, versionID, true, "icon_128")
		if iconErr != nil {
			return iconErr
		}
		renderFileID, renderErr := resolveModContentImageFileID(ctx, tx, snapshot.Resource.RenderFilePublicID, actorID, resourceID, versionID, true, "render")
		if renderErr != nil {
			return renderErr
		}
		if _, err := tx.Exec(ctx, `update mod_resource_version_details detail set entry_type_code=$3,definition_schema_version=$4,default_locale=$5,definition=$6::jsonb,
			icon_small_file_id=case when $7 then $8 else detail.icon_small_file_id end,
			icon_file_id=case when $9 then $10 else detail.icon_file_id end,
			render_file_id=case when $11 then $12 else detail.render_file_id end,
			projection_source='manual',import_source_namespace='',import_source_kind='',import_revision_id='',
			status='active',published_revision_id=$13,updated_by=$14,updated_at=now()
			from catalog_entities entity where entity.public_id=$1 and detail.resource_id=entity.id and detail.version_id=$2`,
			snapshot.PublicID, versionID, snapshot.Resource.EntryTypeCode, modContentDefinitionSchemaVersion, snapshot.Resource.DefaultLocale, string(definition),
			snapshot.Resource.IconSmallFilePublicID != nil, iconSmallFileID,
			snapshot.Resource.IconFilePublicID != nil, iconFileID,
			snapshot.Resource.RenderFilePublicID != nil, renderFileID,
			revisionID, actorID); err != nil {
			return err
		}
		if snapshot.CreatedIdentity {
			if _, err := tx.Exec(ctx, `update catalog_entities set status='active',updated_at=now() where id=$1 and status='placeholder'`, resourceID); err != nil {
				return err
			}
		}
		if _, err = tx.Exec(ctx, `delete from mod_resource_version_detail_localizations where resource_id=$1 and version_id=$2`, resourceID, versionID); err != nil {
			return err
		}
		for _, localization := range snapshot.Resource.Localizations {
			existing, exists := existingLocalizations[localization.Locale]
			unchanged := exists &&
				existing.Name == localization.Name &&
				existing.Summary == localization.Summary &&
				existing.ContentMarkdown == localization.ContentMarkdown
			provenance := modContentResourceLocalizationProvenance(existing.Provenance, unchanged)
			if _, err := tx.Exec(ctx, `insert into mod_resource_version_detail_localizations(resource_id,version_id,locale,name,summary,content_markdown,provenance)
				values($1,$2,$3,$4,$5,$6,$7)`, resourceID, versionID, localization.Locale, localization.Name, localization.Summary, localization.ContentMarkdown, provenance); err != nil {
				return err
			}
		}
		if snapshot.Resource.SectionPublicID != nil {
			if err := moveModContentResourceToSectionTx(ctx, tx, revisionID, snapshot.ModID, versionID, resourceID, *snapshot.Resource.SectionPublicID, actorID); err != nil {
				return err
			}
		}
		return syncModContentResourceUnresolvedReferencesTx(
			ctx, tx, resourceID, versionID, snapshot.Resource.Definition,
		)
	default:
		return errCatalogEditorInvalid
	}
}

const modContentTagReferenceBatchSQL = `with candidates as materialized (
	select input.field_path,input.raw_identifier,input.normalized_identifier,input.registry,input.ordinal
	from unnest($3::text[],$4::text[],$5::text[],$6::text[])
		with ordinality as input(field_path,raw_identifier,normalized_identifier,registry,ordinal)
), matches as (
	select distinct candidate.ordinal
	from candidates candidate
	join catalog_tags tag on lower(tag.canonical_id)=candidate.normalized_identifier
		and (candidate.registry='' or lower(tag.registry)=candidate.registry)
	join catalog_entities entity on entity.id=tag.entity_id and entity.status='active'
)
insert into unresolved_references(
	source_type,source_id,field_path,reference_type,raw_identifier,normalized_identifier,metadata
)
select 'mod_content_resource',$1,candidate.field_path,'tag',candidate.raw_identifier,candidate.normalized_identifier,
	jsonb_build_object('versionId',$2::bigint,'registry',candidate.registry)
from candidates candidate left join matches on matches.ordinal=candidate.ordinal
where matches.ordinal is null
on conflict(source_type,source_id,field_path,reference_type,normalized_identifier) do update
set raw_identifier=excluded.raw_identifier,status='pending',resolved_type='',resolved_id=null,
	resolved_at=null,metadata=excluded.metadata,updated_at=now()`

const modContentResourceReferenceBatchSQL = `with candidates as materialized (
	select input.field_path,input.kind_code,input.raw_resource_id,input.normalized_identifier,input.ordinal
	from unnest($2::text[],$3::text[],$4::text[],$5::text[])
		with ordinality as input(field_path,kind_code,raw_resource_id,normalized_identifier,ordinal)
), matches as (
	select candidate.ordinal
	from candidates candidate
	join game_resources resource on resource.kind_code=candidate.kind_code
		and lower(resource.canonical_id)=candidate.normalized_identifier
	join catalog_entities entity on entity.id=resource.entity_id and entity.status='active'
	union
	select candidate.ordinal
	from candidates candidate
	join game_resource_aliases alias on alias.kind_code=candidate.kind_code
		and lower(alias.alias_id)=candidate.normalized_identifier
	join game_resources resource on resource.entity_id=alias.resource_id and resource.kind_code=alias.kind_code
	join catalog_entities entity on entity.id=resource.entity_id and entity.status='active'
)
insert into unresolved_resource_references(
	source_entity_id,field_path,kind_code,raw_resource_id,status
)
select $1,candidate.field_path,candidate.kind_code,candidate.raw_resource_id,'pending'
from candidates candidate left join matches on matches.ordinal=candidate.ordinal
where matches.ordinal is null`

func syncModContentResourceUnresolvedReferencesTx(
	ctx context.Context,
	tx pgx.Tx,
	resourceID, versionID int64,
	definition map[string]any,
) error {
	fieldPrefix := fmt.Sprintf("version.%d.", versionID)
	if _, err := tx.Exec(ctx, `delete from unresolved_resource_references
		where source_entity_id=$1 and source_revision_id is null
		  and field_path like $2`, resourceID, fieldPrefix+"%"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from unresolved_references
		where source_type='mod_content_resource' and source_id=$1
		  and field_path like $2`, resourceID, fieldPrefix+"%"); err != nil {
		return err
	}
	var rawTemplate []byte
	var entryTypeCode string
	if err := tx.QueryRow(ctx, `with recursive lineage as (
		select section.id,section.parent_id,section.template_id
		from mod_content_section_resources member
		join mod_content_sections section on section.id=member.section_id and section.version_id=member.version_id
		where member.resource_id=$1 and member.version_id=$2 and section.status='active'
		union all
		select parent.id,parent.parent_id,parent.template_id
		from mod_content_sections parent join lineage child on child.parent_id=parent.id
		where parent.status='active'
	), root as (select * from lineage where parent_id is null order by id limit 1)
		select template.definition,detail.entry_type_code
		from mod_resource_version_details detail
		join root on true
		join mod_content_templates template on template.id=root.template_id and template.status='active'
		where detail.resource_id=$1 and detail.version_id=$2`, resourceID, versionID).Scan(&rawTemplate, &entryTypeCode); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	var template modContentTemplateDefinition
	if err := json.Unmarshal(rawTemplate, &template); err != nil {
		return err
	}
	entryType, err := selectModContentEntryType(template.EntryTypes, entryTypeCode, "", false, true)
	if err != nil {
		return err
	}
	tagFieldPaths := make([]string, 0)
	tagRawIdentifiers := make([]string, 0)
	tagNormalizedIdentifiers := make([]string, 0)
	tagRegistries := make([]string, 0)
	resourceFieldPaths := make([]string, 0)
	resourceKindCodes := make([]string, 0)
	resourceRawIdentifiers := make([]string, 0)
	resourceNormalizedIdentifiers := make([]string, 0)
	for _, group := range entryType.Groups {
		for _, field := range group.Fields {
			if field.Type != "reference" && field.Type != "reference-list" {
				continue
			}
			identifiers := firstDefinitionStringList(definition, []string{field.Code})
			if field.Type == "reference" {
				if identifier, ok := definition[field.Code].(string); ok {
					identifiers = uniqueTrimmed([]string{identifier}, 1)
				}
			}
			for index, identifier := range identifiers {
				fieldPath := fieldPrefix + field.Code
				if field.Type == "reference-list" {
					fieldPath = fmt.Sprintf("%s.%d", fieldPath, index)
				}
				if field.ReferenceKind == "tag" {
					normalized := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(identifier)), "#")
					tagFieldPaths = append(tagFieldPaths, fieldPath)
					tagRawIdentifiers = append(tagRawIdentifiers, identifier)
					tagNormalizedIdentifiers = append(tagNormalizedIdentifiers, normalized)
					tagRegistries = append(tagRegistries, strings.ToLower(strings.TrimSpace(field.ReferenceRegistry)))
					continue
				}
				resourceFieldPaths = append(resourceFieldPaths, fieldPath)
				resourceKindCodes = append(resourceKindCodes, normalizedModContentReferenceKind(field.ReferenceKind))
				resourceRawIdentifiers = append(resourceRawIdentifiers, identifier)
				resourceNormalizedIdentifiers = append(resourceNormalizedIdentifiers, strings.ToLower(strings.TrimSpace(identifier)))
			}
		}
	}
	if len(tagFieldPaths) > 0 {
		if _, err = tx.Exec(ctx, modContentTagReferenceBatchSQL,
			resourceID, versionID, tagFieldPaths, tagRawIdentifiers, tagNormalizedIdentifiers, tagRegistries); err != nil {
			return err
		}
	}
	if len(resourceFieldPaths) > 0 {
		if _, err = tx.Exec(ctx, modContentResourceReferenceBatchSQL, resourceID, resourceFieldPaths, resourceKindCodes,
			resourceRawIdentifiers, resourceNormalizedIdentifiers); err != nil {
			return err
		}
	}
	return nil
}

func normalizedModContentReferenceKind(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "enchantment":
		return "minecraft.enchantment"
	case "item":
		return "minecraft.item"
	case "block":
		return "minecraft.block"
	case "entity", "entity_type":
		return "minecraft.entity_type"
	default:
		return value
	}
}

func firstDefinitionStringList(definition map[string]any, paths ...[]string) []string {
	for _, path := range paths {
		var current any = definition
		for _, part := range path {
			row, ok := current.(map[string]any)
			if !ok {
				current = nil
				break
			}
			current = row[part]
		}
		values, ok := current.([]any)
		if !ok {
			if strings, valid := current.([]string); valid {
				return uniqueTrimmed(strings, 500)
			}
			continue
		}
		result := make([]string, 0, len(values))
		for _, value := range values {
			if text, valid := value.(string); valid {
				result = append(result, text)
			}
		}
		return uniqueTrimmed(result, 500)
	}
	return nil
}

// archiveModContentSectionTreeTx applies soft deletion to the complete
// category subtree. Foreign-key cascades cannot help because the section rows
// are archived rather than physically deleted. Their placements must also be
// removed, or the version-wide resource uniqueness would block a later import
// from rebuilding the deleted documentation pages.
func archiveModContentSectionTreeTx(ctx context.Context, tx pgx.Tx, publicID string, modID, revisionID, actorID int64) error {
	if _, err := tx.Exec(ctx, `with recursive subtree(id) as (
			select id from mod_content_sections where public_id=$1 and mod_id=$2
			union all
			select child.id from mod_content_sections child join subtree parent on child.parent_id=parent.id
		)
		delete from mod_content_section_resources placement
		using subtree where placement.section_id=subtree.id`, publicID, modID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `with recursive subtree(id) as (
			select id from mod_content_sections where public_id=$1 and mod_id=$2
			union all
			select child.id from mod_content_sections child join subtree parent on child.parent_id=parent.id
		)
		update mod_content_sections section set status='archived',published_revision_id=$3,
			updated_by=nullif($4::bigint,0),updated_at=now()
		from subtree where section.id=subtree.id`, publicID, modID, revisionID, actorID)
	return err
}

func modContentResourceLocalizationProvenance(existing string, unchanged bool) string {
	if unchanged {
		switch existing {
		case "ai", "human_corrected", "human", "import":
			return existing
		}
	}
	if existing == "ai" || existing == "human_corrected" {
		return "human_corrected"
	}
	return "human"
}

func publishedModContentRevisionTx(ctx context.Context, tx pgx.Tx, snapshot modContentSnapshot) (*int64, error) {
	table := "mod_content_versions"
	if snapshot.Kind == "template" {
		table = "mod_content_templates"
	} else if snapshot.Kind == "section" || snapshot.Kind == "layout" {
		table = "mod_content_sections"
	} else if snapshot.Kind == "resource" {
		if snapshot.Resource == nil {
			return nil, errCatalogEditorInvalid
		}
		var revisionID *int64
		err := tx.QueryRow(ctx, `select detail.published_revision_id from mod_resource_version_details detail
			join catalog_entities entity on entity.id=detail.resource_id join mod_content_versions version on version.id=detail.version_id
			where entity.public_id=$1 and version.public_id=$2 and version.mod_id=$3 for update of detail`, snapshot.PublicID, snapshot.Resource.VersionPublicID, snapshot.ModID).Scan(&revisionID)
		return revisionID, err
	}
	var revisionID *int64
	err := tx.QueryRow(ctx, `select published_revision_id from `+table+` where public_id=$1 for update`, snapshot.PublicID).Scan(&revisionID)
	return revisionID, err
}

func rejectPendingModContentCreateTx(ctx context.Context, tx pgx.Tx, snapshot modContentSnapshot) error {
	if snapshot.Operation != "create" {
		return nil
	}
	table := "mod_content_versions"
	if snapshot.Kind == "template" {
		table = "mod_content_templates"
	} else if snapshot.Kind == "section" {
		table = "mod_content_sections"
	} else if snapshot.Kind == "resource" {
		if snapshot.Resource == nil {
			return errCatalogEditorInvalid
		}
		_, err := tx.Exec(ctx, `update mod_resource_version_details detail set status='archived',updated_at=now()
			from catalog_entities entity,mod_content_versions version where entity.public_id=$1 and detail.resource_id=entity.id
			and version.public_id=$2 and version.mod_id=$3 and detail.version_id=version.id and detail.status='pending'`, snapshot.PublicID, snapshot.Resource.VersionPublicID, snapshot.ModID)
		return err
	}
	_, err := tx.Exec(ctx, `update `+table+` set status='archived',updated_at=now() where public_id=$1 and status='pending'`, snapshot.PublicID)
	return err
}
