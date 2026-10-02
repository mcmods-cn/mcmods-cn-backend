package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *Server) catalogRecipeSourceVersions(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := boundedLimit(r.URL.Query().Get("limit"), 500, 1000)
	claims := currentClaims(r)
	rows, err := s.db.Query(r.Context(), `select version.public_id,mod.project_code,mod.slug,mod.primary_name,
		version.label,version.minecraft_versions,version.loaders,version.mod_version
		from mod_content_versions version join mods mod on mod.id=version.mod_id
		where version.status='active' and (mod.review_status='approved' or mod.submitted_by=$1)
		and ($2='' or mod.primary_name ilike '%'||$2||'%' or mod.secondary_name ilike '%'||$2||'%'
			or mod.slug ilike '%'||$2||'%' or version.label ilike '%'||$2||'%' or version.mod_version ilike '%'||$2||'%')
		order by lower(mod.primary_name),mod.id,version.updated_at desc,version.id desc limit $3`, claims.Subject, query, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read recipe source versions")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var publicID, modPublicID, modSiteID, modName, label, modVersion string
		var minecraftVersions, loaders []string
		if err = rows.Scan(&publicID, &modPublicID, &modSiteID, &modName, &label, &minecraftVersions, &loaders, &modVersion); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode recipe source versions")
			return
		}
		items = append(items, map[string]any{"publicId": publicID, "modPublicId": modPublicID, "modSiteId": modSiteID,
			"modName": modName, "label": label, "minecraftVersions": minecraftVersions, "loaders": loaders, "modVersion": modVersion})
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read recipe source versions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createCatalogRecipe(w http.ResponseWriter, r *http.Request) {
	var edit catalogRecipeEdit
	if decodeJSON(r, &edit) != nil || edit.BaseRevisionID != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	typePublicID := strings.TrimSpace(r.PathValue("typePublicId"))
	if typePublicID == "" {
		typePublicID = strings.TrimSpace(edit.RecipeTypePublicID)
	}
	if typePublicID == "" || edit.RecipeTypePublicID != "" && !strings.EqualFold(typePublicID, edit.RecipeTypePublicID) {
		writeError(w, http.StatusUnprocessableEntity, "recipeTypePublicId is invalid")
		return
	}
	typeEntity, err := s.catalogEditorEntityByPublicID(r.Context(), typePublicID, "recipe_type")
	if err != nil || typeEntity.Status != "active" {
		writeError(w, http.StatusNotFound, "recipe type not found")
		return
	}
	edit.RecipeTypePublicID = typeEntity.PublicID
	if err = s.normalizeCatalogRecipeEdit(r.Context(), typeEntity.ID, 0, &edit); err != nil {
		writeError(w, catalogEditorHTTPStatus(err), catalogEditorErrorMessage(err))
		return
	}
	if edit.ApplicableVersionIDs == nil && edit.SourceVersionPublicID != "" {
		var versions []string
		if err = s.db.QueryRow(r.Context(), `select minecraft_versions from mod_content_versions where public_id=$1 and status='active'`, edit.SourceVersionPublicID).Scan(&versions); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "sourceVersionPublicId is invalid")
			return
		}
		versions = uniqueNonEmpty(versions)
		edit.ApplicableVersionIDs = &versions
	}
	if edit.ApplicableVersionIDs == nil || len(*edit.ApplicableVersionIDs) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "at least one applicable Minecraft version is required")
		return
	}
	if err = requireCatalogCreateDefaultLocalization(edit.DefaultLocale, edit.Localizations); err != nil {
		writeError(w, catalogEditorHTTPStatus(err), catalogEditorErrorMessage(err))
		return
	}
	identity := catalogEditorIdentityForRecipe(typeEntity.IdentityKey, edit.CanonicalSourceID)
	snapshot := catalogEditorSnapshot{Operation: "create", Reason: edit.Reason, Kind: "recipe", IdentityKey: identity.ID, PublicID: identity.PublicID,
		ParentEntityID: typeEntity.ID, ParentPublicID: typeEntity.PublicID,
		DefaultLocale: edit.DefaultLocale, Localizations: edit.Localizations, Recipe: &edit}
	result, submitErr := s.submitCatalogEditorMutation(r, snapshot, nil)
	writeCatalogMutationResult(w, result, submitErr)
}

func (s *Server) catalogRecipes(w http.ResponseWriter, r *http.Request) {
	typeEntity, err := s.catalogEditorEntityByPublicID(r.Context(), r.PathValue("typePublicId"), "recipe_type")
	if err != nil || typeEntity.Status != "active" {
		writeError(w, http.StatusNotFound, "recipe type not found")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := boundedLimit(r.URL.Query().Get("limit"), 40, 100)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	var total int
	if err = s.db.QueryRow(r.Context(), `select count(*)::int from recipes recipe
		join catalog_entities entity on entity.id=recipe.entity_id
		where recipe.recipe_type_id=$1 and entity.status='active' and ($2='' or coalesce(recipe.canonical_source_id,'') ilike '%'||$2||'%')`,
		typeEntity.ID, query).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count recipes")
		return
	}
	rows, err := s.db.Query(r.Context(), `select entity.public_id,coalesce(recipe.canonical_source_id,''),recipe.identity_source,
		(select revision.public_id from content_revisions revision where revision.id=entity.published_revision_id),
		coalesce(template_entity.public_id,imported_template_entity.public_id,''),
		coalesce(definition.definition,'{}'::jsonb),coalesce(observation.id,''),coalesce(observation.revision_id,''),
		case when definition.recipe_id is not null then (select count(*)::int from recipe_bindings binding where binding.recipe_id=recipe.entity_id)
		 else coalesce(observation.binding_count,0) end,
		coalesce((select jsonb_object_agg(localization.locale,localization.name) from content_localizations localization
		 where localization.catalog_entity_id=entity.id and localization.name<>''),'{}'::jsonb),definition.recipe_id is not null,
		coalesce(effective_source_version.public_id,''),coalesce(source_mod.project_code,''),coalesce(source_mod.slug,''),
		coalesce(source_mod.primary_name,''),coalesce(effective_source_version.label,''),
		coalesce(effective_source_version.minecraft_versions,'{}'::text[]),coalesce(effective_source_version.loaders,'{}'::text[]),
		coalesce(effective_source_version.mod_version,''),coalesce(applicable.versions,'[]'::jsonb)
		from recipes recipe join catalog_entities entity on entity.id=recipe.entity_id
		left join recipe_definitions definition on definition.recipe_id=recipe.entity_id
		left join catalog_entities template_entity on template_entity.id=definition.template_id
		left join lateral (select snapshot.*,version.public_id as source_version_public_id from recipe_import_snapshots snapshot
		 join catalog_import_revisions revision on revision.id=snapshot.revision_id
		 join mod_content_versions version on version.id=revision.target_version_id
		 where snapshot.recipe_id=recipe.entity_id and revision.is_active and revision.status in ('ready','partial')
		 order by coalesce(revision.activated_at,revision.created_at) desc,revision.created_at desc limit 1) observation on true
		left join recipe_template_import_snapshots import_template on import_template.id=observation.template_id
		left join catalog_entities imported_template_entity on imported_template_entity.id=import_template.canonical_template_id
		left join mod_content_versions canonical_source_version on canonical_source_version.id=definition.source_mod_content_version_id
		left join mod_content_versions effective_source_version on effective_source_version.public_id=
			case when definition.recipe_id is not null then canonical_source_version.public_id else observation.source_version_public_id end
		left join mods source_mod on source_mod.id=effective_source_version.mod_id
		left join lateral (select jsonb_agg(binding.version_code order by binding.created_at,binding.version_code) versions
			from recipe_version_bindings binding where binding.recipe_id=recipe.entity_id) applicable on true
		where recipe.recipe_type_id=$1 and entity.status='active' and ($2='' or coalesce(recipe.canonical_source_id,'') ilike '%'||$2||'%')
		order by coalesce(recipe.canonical_source_id,''),entity.public_id limit $3 offset $4`, typeEntity.ID, query, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read recipes")
		return
	}
	defer rows.Close()
	primary, secondary := s.catalogRequestedLocales(r)
	versionConfig, err := loadMinecraftVersionConfig(r.Context(), s.db)
	if err != nil {
		logCatalogEditorReadFailure("recipe list Minecraft versions", err)
		writeError(w, http.StatusInternalServerError, "failed to read Minecraft versions")
		return
	}
	versionOrder := minecraftVersionOrder(versionConfig)
	items := make([]map[string]any, 0, limit)
	for rows.Next() {
		var publicID, canonicalID, identitySource, templatePublicID, observationID, importRevisionID string
		var sourceVersionPublicID, sourceModPublicID, sourceModSiteID, sourceModName, sourceVersionLabel, sourceModVersion string
		var sourceMinecraftVersions, sourceLoaders []string
		var publishedRevisionID sql.NullString
		var definition, names, applicableVersions []byte
		var bindingCount int
		var canonicalDefinition bool
		if err = rows.Scan(&publicID, &canonicalID, &identitySource, &publishedRevisionID, &templatePublicID, &definition,
			&observationID, &importRevisionID, &bindingCount, &names, &canonicalDefinition,
			&sourceVersionPublicID, &sourceModPublicID, &sourceModSiteID, &sourceModName, &sourceVersionLabel,
			&sourceMinecraftVersions, &sourceLoaders, &sourceModVersion, &applicableVersions); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode recipes")
			return
		}
		locale, name := catalogResolvedName(names, primary, secondary, "en-US")
		source := "canonical"
		if !canonicalDefinition && observationID != "" {
			source = "import"
		}
		var sourceVersion any
		if sourceVersionPublicID != "" {
			sourceVersion = map[string]any{"publicId": sourceVersionPublicID, "modPublicId": sourceModPublicID, "modSiteId": sourceModSiteID,
				"modName": sourceModName, "label": sourceVersionLabel, "minecraftVersions": sourceMinecraftVersions,
				"loaders": sourceLoaders, "modVersion": sourceModVersion}
		}
		items = append(items, map[string]any{"publicId": publicID, "canonicalSourceId": canonicalID,
			"identitySource": identitySource, "templatePublicId": templatePublicID, "publishedRevisionId": nullableCatalogString(publishedRevisionID),
			"definition": json.RawMessage(definition), "bindingCount": bindingCount, "source": source,
			"sourceVersionPublicId": sourceVersionPublicID, "sourceVersion": sourceVersion,
			"applicableVersions": catalogApplicableVersions(applicableVersions, versionOrder),
			"importRevisionId":   importRevisionID, "locale": locale, "name": name, "names": json.RawMessage(names)})
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read recipes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) normalizeCatalogRecipeEdit(ctx context.Context, typeID, recipeID int64, edit *catalogRecipeEdit) error {
	var err error
	edit.TemplatePublicID, err = canonicalCatalogString(edit.TemplatePublicID, 32)
	if err != nil {
		return err
	}
	edit.SourceVersionPublicID, err = normalizeCatalogOptionalPublicID(edit.SourceVersionPublicID)
	if err != nil {
		return err
	}
	if edit.ApplicableVersionIDs != nil {
		versions := uniqueNonEmpty(*edit.ApplicableVersionIDs)
		if len(versions) == 0 {
			return errCatalogEditorInvalid
		}
		versionConfig, configErr := loadMinecraftVersionConfig(ctx, s.db)
		if configErr != nil {
			return configErr
		}
		valid := make(map[string]struct{})
		for _, option := range versionConfig.Versions {
			valid[option.Code] = struct{}{}
		}
		for _, version := range versions {
			if _, exists := valid[version]; !exists {
				return errCatalogEditorReference
			}
		}
		edit.ApplicableVersionIDs = &versions
	}
	edit.DefaultLocale, edit.Localizations, err = normalizeCatalogLocalizations(edit.DefaultLocale, edit.Localizations)
	if err != nil {
		return err
	}
	// Top-level definition can contain server-authored fields which this visual
	// editor cannot express. Preserve the authoritative value and only accept an
	// exact client echo; bindings and candidates remain represented by the
	// normalized relational structure below.
	edit.Definition, err = s.catalogRecipeDefinitionForMutation(ctx, recipeID, edit.Definition)
	if err != nil {
		return err
	}
	for slotKey, binding := range edit.Bindings {
		binding.Definition = map[string]any{}
		for index := range binding.Candidates {
			binding.Candidates[index].Definition = map[string]any{}
		}
		edit.Bindings[slotKey] = binding
	}
	rows, err := s.db.Query(ctx, `select slot.slot_key,slot.role from recipe_layout_templates template
		join catalog_entities entity on entity.id=template.entity_id and entity.status='active'
		join recipe_template_slots slot on slot.template_id=template.entity_id
		where entity.public_id=$1 and template.recipe_type_id=$2`, edit.TemplatePublicID, typeID)
	if err != nil {
		return err
	}
	defer rows.Close()
	roles := map[string]string{}
	for rows.Next() {
		var key, role string
		if err = rows.Scan(&key, &role); err != nil {
			return err
		}
		roles[key] = role
	}
	if len(roles) == 0 {
		return errCatalogEditorReference
	}
	return validateCatalogRecipeBindings(edit, roles)
}

func (s *Server) catalogRecipeDetail(w http.ResponseWriter, r *http.Request) {
	entity, err := s.catalogEditorEntityByPublicID(r.Context(), r.PathValue("publicId"), "recipe")
	if errors.Is(err, errCatalogEditorNotFound) || err == nil && entity.Status == "archived" && r.Method == http.MethodGet {
		writeError(w, http.StatusNotFound, "recipe not found")
		return
	}
	if err != nil {
		logCatalogEditorReadFailure("recipe entity", err)
		writeError(w, http.StatusInternalServerError, "failed to read recipe")
		return
	}
	var typeID int64
	if err = s.db.QueryRow(r.Context(), `select recipe_type_id from recipes where entity_id=$1`, entity.ID).Scan(&typeID); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "recipe not found")
		return
	}
	if err != nil {
		logCatalogEditorReadFailure("recipe parent", err)
		writeError(w, http.StatusInternalServerError, "failed to read recipe")
		return
	}
	if r.Method == http.MethodGet {
		active, activeErr := s.catalogRecipeTypeIsActive(r.Context(), typeID)
		if activeErr != nil {
			logCatalogEditorReadFailure("recipe parent activity", activeErr)
			writeError(w, http.StatusInternalServerError, "failed to read recipe type")
			return
		}
		if !active {
			writeError(w, http.StatusNotFound, "recipe not found")
			return
		}
	}
	if r.Method == http.MethodPut {
		var edit catalogRecipeEdit
		if decodeJSON(r, &edit) != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err = s.normalizeCatalogRecipeEdit(r.Context(), typeID, entity.ID, &edit); err != nil {
			writeError(w, catalogEditorHTTPStatus(err), catalogEditorErrorMessage(err))
			return
		}
		var typePublicID string
		if err = s.db.QueryRow(r.Context(), `select public_id from catalog_entities where id=$1 and entity_type='recipe_type'`, typeID).Scan(&typePublicID); err != nil {
			writeError(w, http.StatusNotFound, "recipe type not found")
			return
		}
		snapshot := catalogEditorSnapshot{Operation: "edit", Reason: edit.Reason, Kind: "recipe", EntityID: entity.ID, PublicID: entity.PublicID,
			ParentEntityID: typeID, ParentPublicID: typePublicID,
			DefaultLocale: edit.DefaultLocale, Localizations: edit.Localizations, Recipe: &edit}
		result, submitErr := s.submitCatalogEditorMutation(r, snapshot, edit.BaseRevisionID)
		writeCatalogMutationResult(w, result, submitErr)
		return
	}
	if r.Method == http.MethodDelete {
		s.deleteCatalogEntity(w, r, entity, "recipe")
		return
	}
	var typePublicID, templatePublicID, canonicalID, observationID, importRevisionID, importTemplateKey string
	var sourceVersionPublicID, sourceModPublicID, sourceModSiteID, sourceModName, sourceVersionLabel, sourceModVersion string
	var sourceMinecraftVersions, sourceLoaders []string
	var canonicalDefinition bool
	var definition, applicableVersions []byte
	if err = s.db.QueryRow(r.Context(), `select type_entity.public_id,coalesce(template_entity.public_id,imported_template_entity.public_id,''),coalesce(recipe.canonical_source_id,''),
		coalesce(definition.definition,'{}'::jsonb),coalesce(observation.id,''),coalesce(observation.revision_id,''),
		coalesce(import_template.source_template_id,''),definition.recipe_id is not null,
		coalesce(effective_source_version.public_id,''),coalesce(source_mod.project_code,''),coalesce(source_mod.slug,''),
		coalesce(source_mod.primary_name,''),coalesce(effective_source_version.label,''),
		coalesce(effective_source_version.minecraft_versions,'{}'::text[]),coalesce(effective_source_version.loaders,'{}'::text[]),
		coalesce(effective_source_version.mod_version,''),coalesce(applicable.versions,'[]'::jsonb)
		from recipes recipe join catalog_entities type_entity on type_entity.id=recipe.recipe_type_id
		left join recipe_definitions definition on definition.recipe_id=recipe.entity_id
		left join catalog_entities template_entity on template_entity.id=definition.template_id
		left join lateral (select snapshot.*,version.public_id as source_version_public_id from recipe_import_snapshots snapshot
		 join catalog_import_revisions revision on revision.id=snapshot.revision_id
		 join mod_content_versions version on version.id=revision.target_version_id
			where snapshot.recipe_id=recipe.entity_id and revision.is_active and revision.status in ('ready','partial')
			order by coalesce(revision.activated_at,revision.created_at) desc,revision.created_at desc limit 1) observation on true
		left join recipe_template_import_snapshots import_template on import_template.id=observation.template_id
		left join catalog_entities imported_template_entity on imported_template_entity.id=import_template.canonical_template_id
		left join mod_content_versions canonical_source_version on canonical_source_version.id=definition.source_mod_content_version_id
		left join mod_content_versions effective_source_version on effective_source_version.public_id=
			case when definition.recipe_id is not null then canonical_source_version.public_id else observation.source_version_public_id end
		left join mods source_mod on source_mod.id=effective_source_version.mod_id
		left join lateral (select jsonb_agg(binding.version_code order by binding.created_at,binding.version_code) versions
			from recipe_version_bindings binding where binding.recipe_id=recipe.entity_id) applicable on true
		where recipe.entity_id=$1`, entity.ID).
		Scan(&typePublicID, &templatePublicID, &canonicalID, &definition, &observationID, &importRevisionID, &importTemplateKey, &canonicalDefinition,
			&sourceVersionPublicID, &sourceModPublicID, &sourceModSiteID, &sourceModName, &sourceVersionLabel,
			&sourceMinecraftVersions, &sourceLoaders, &sourceModVersion, &applicableVersions); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "recipe not found")
		return
	}
	if err != nil {
		logCatalogEditorReadFailure("recipe definition", err)
		writeError(w, http.StatusInternalServerError, "failed to read recipe")
		return
	}
	bindings, bindingErr := s.catalogRecipeBindingRows(r.Context(), entity.ID)
	if bindingErr == nil && len(bindings) == 0 && observationID != "" {
		bindings, bindingErr = s.catalogImportedRecipeBindingRows(r.Context(), observationID)
	}
	if bindingErr != nil {
		logCatalogEditorReadFailure("recipe bindings", bindingErr)
		writeError(w, http.StatusInternalServerError, "failed to read recipe bindings")
		return
	}
	localizations, localeErr := s.catalogLocalizationRows(r.Context(), entity.ID)
	if localeErr != nil {
		logCatalogEditorReadFailure("recipe localizations", localeErr)
		writeError(w, http.StatusInternalServerError, "failed to read localizations")
		return
	}
	defaultLocale := entity.DefaultLocale
	if entity.PublishedRevisionID == nil && len(localizations) == 0 {
		fallbackName := canonicalID
		if fallbackName == "" {
			fallbackName = entity.PublicID
		}
		localizations, defaultLocale = catalogImportedEditorLocalizations(nil, defaultLocale, fallbackName)
	}
	source := "canonical"
	if !canonicalDefinition && observationID != "" {
		source = "import"
	}
	var sourceVersion any
	if sourceVersionPublicID != "" {
		sourceVersion = map[string]any{"publicId": sourceVersionPublicID, "modPublicId": sourceModPublicID, "modSiteId": sourceModSiteID,
			"modName": sourceModName, "label": sourceVersionLabel, "minecraftVersions": sourceMinecraftVersions,
			"loaders": sourceLoaders, "modVersion": sourceModVersion}
	}
	publishedRevisionPublicID, revisionErr := revisionPublicIDValue(r.Context(), s.db, entity.PublishedRevisionID)
	if revisionErr != nil {
		logCatalogEditorReadFailure("recipe published revision", revisionErr)
		writeError(w, http.StatusInternalServerError, "failed to resolve published revision")
		return
	}
	reviewStatus, reviewErr := s.catalogPendingReviewStatus(r.Context(), entity.ID)
	if reviewErr != nil {
		logCatalogEditorReadFailure("recipe review status", reviewErr)
		writeError(w, http.StatusInternalServerError, "failed to read review status")
		return
	}
	versionConfig, versionErr := loadMinecraftVersionConfig(r.Context(), s.db)
	if versionErr != nil {
		logCatalogEditorReadFailure("recipe detail Minecraft versions", versionErr)
		writeError(w, http.StatusInternalServerError, "failed to read Minecraft versions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"publicId": entity.PublicID, "recipeTypePublicId": typePublicID,
		"templatePublicId": templatePublicID, "sourceVersionPublicId": sourceVersionPublicID, "sourceVersion": sourceVersion,
		"canonicalSourceId": canonicalID, "definition": json.RawMessage(definition), "bindings": bindings,
		"defaultLocale": defaultLocale, "localizations": localizations, "publishedRevisionId": publishedRevisionPublicID,
		"reviewStatus": reviewStatus, "source": source,
		"applicableVersions": catalogApplicableVersions(applicableVersions, minecraftVersionOrder(versionConfig)),
		"importRevisionId":   importRevisionID, "importTemplateKey": importTemplateKey})
}

func minecraftVersionOrder(config minecraftVersionConfig) map[string]int {
	order := make(map[string]int, len(config.Versions))
	for index, version := range config.Versions {
		order[version.Code] = index
	}
	return order
}

func catalogApplicableVersions(raw []byte, order map[string]int) []map[string]string {
	codes := make([]string, 0)
	_ = json.Unmarshal(raw, &codes)
	sortMinecraftVersionCodes(codes, order)
	items := make([]map[string]string, 0, len(codes))
	for _, code := range codes {
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		items = append(items, map[string]string{"id": code, "name": code, "group": minecraftVersionGroupName(code)})
	}
	return items
}

func sortMinecraftVersionCodes(codes []string, order map[string]int) {
	sort.SliceStable(codes, func(left, right int) bool {
		leftRank, leftKnown := order[codes[left]]
		rightRank, rightKnown := order[codes[right]]
		if leftKnown != rightKnown {
			return leftKnown
		}
		if leftKnown && leftRank != rightRank {
			return leftRank < rightRank
		}
		return codes[left] < codes[right]
	})
}

func minecraftVersionGroupName(code string) string {
	parts := strings.Split(code, ".")
	if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
		return parts[0] + "." + parts[1] + ".X"
	}
	return "其他"
}

func (s *Server) deleteCatalogEntity(w http.ResponseWriter, r *http.Request, entity catalogEditorEntity, kind string) {
	var request catalogDeleteRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	snapshot := catalogEditorSnapshot{Operation: "delete", Reason: request.Reason, Kind: kind, EntityID: entity.ID, PublicID: entity.PublicID,
		DefaultLocale: entity.DefaultLocale}
	result, err := s.submitCatalogEditorMutation(r, snapshot, request.BaseRevisionID)
	writeCatalogMutationResult(w, result, err)
}

func writeCatalogMutationResult(w http.ResponseWriter, result catalogEditResult, err error) {
	if err != nil {
		writeError(w, catalogEditorHTTPStatus(err), catalogEditorErrorMessage(err))
		return
	}
	writeJSON(w, http.StatusAccepted, result)
}

func nullableCatalogString(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func catalogResolvedName(raw []byte, primary, secondary, defaultLocale string) (string, string) {
	var encoded map[string]string
	if len(raw) == 0 || json.Unmarshal(raw, &encoded) != nil {
		return "", ""
	}
	names := make(map[string]string, len(encoded))
	available := make([]string, 0, len(encoded))
	for rawLocale, name := range encoded {
		locale := normalizeContentLocale(rawLocale)
		if locale == "" || strings.TrimSpace(name) == "" {
			continue
		}
		names[locale] = name
		available = append(available, locale)
	}
	resolution := resolveContentLocale(primary, secondary, defaultLocale, available)
	return resolution.ResolvedLocale, names[resolution.ResolvedLocale]
}

func (s *Server) catalogRequestedLocales(r *http.Request) (string, string) {
	primary, secondary := s.requestContentLocales(r)
	if requested := normalizeContentLocale(r.URL.Query().Get("locale")); requested != "" {
		primary = requested
	}
	if requested := normalizeContentLocale(r.URL.Query().Get("secondaryLocale")); requested != "" {
		secondary = requested
	}
	return primary, secondary
}

func (s *Server) catalogRecipeTypeIsActive(ctx context.Context, typeID int64) (bool, error) {
	var active bool
	err := s.db.QueryRow(ctx, `select exists(select 1 from catalog_entities
		where id=$1 and entity_type='recipe_type' and status='active' and archived_at is null)`, typeID).Scan(&active)
	return active, err
}

func (s *Server) catalogLocalizationRows(ctx context.Context, entityID int64) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `select locale,name,summary,content_markdown,provenance,source_locale,
		(select task_uid from ai_tasks where id=content_localizations.ai_task_id),revision_no,
		editable,review_status,
		(select revision.public_id from content_revisions revision where revision.id=content_localizations.published_revision_id)
		from content_localizations where catalog_entity_id=$1 order by locale`, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var locale, name, summary, markdown, provenance, sourceLocale, reviewStatus string
		var aiTaskID sql.NullString
		var publishedRevisionID sql.NullString
		var revisionNo int64
		var editable bool
		if err = rows.Scan(&locale, &name, &summary, &markdown, &provenance, &sourceLocale, &aiTaskID, &revisionNo, &editable,
			&reviewStatus, &publishedRevisionID); err != nil {
			return nil, err
		}
		result = append(result, map[string]any{"locale": locale, "name": name, "summary": summary, "contentMarkdown": markdown,
			"provenance": provenance, "sourceLocale": sourceLocale, "aiTaskId": nullableCatalogString(aiTaskID), "revisionNo": revisionNo,
			"editable": editable, "reviewStatus": reviewStatus, "publishedRevisionId": nullableCatalogString(publishedRevisionID)})
	}
	return result, rows.Err()
}

func (s *Server) catalogPendingReviewStatus(ctx context.Context, entityID int64) (string, error) {
	var status string
	err := s.db.QueryRow(ctx, `select status from change_requests where entity_id=$1 order by id desc limit 1`, entityID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "approved", nil
	}
	if err != nil {
		return "", err
	}
	return status, nil
}

func (s *Server) catalogTagMemberRows(ctx context.Context, tagID int64, primary, secondary string, allowImportFallback bool) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `select entity.id,entity.public_id,resource.kind_code,resource.canonical_id,resource.namespace,member.ordinal,
		entity.default_locale,(select public_id from oss_files where id=definition.icon_file_id),coalesce(imported.revision_id,''),coalesce(imported.icon_path,''),coalesce(imported.mod_site_id,''),
		coalesce((select jsonb_object_agg(localization.locale,localization.name) from content_localizations localization
		 where localization.catalog_entity_id=entity.id and localization.name<>''),imported.names,'{}'::jsonb)
		from catalog_tag_members member join game_resources resource on resource.entity_id=member.resource_id
		left join catalog_resource_definitions definition on definition.resource_id=resource.entity_id
		left join lateral (select snapshot.names,snapshot.revision_id,snapshot.icon_path,mod.slug mod_site_id
		 from resource_import_snapshots snapshot join catalog_import_revisions revision on revision.id=snapshot.revision_id
		 join mods mod on mod.id=revision.mod_id where snapshot.resource_id=resource.entity_id
		 and revision.is_active and revision.status in ('ready','partial')
		 order by (snapshot.icon_path<>'') desc,coalesce(revision.activated_at,revision.created_at) desc limit 1) imported on true
		join catalog_entities entity on entity.id=resource.entity_id where member.tag_id=$1 order by member.ordinal`, tagID)
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	for rows.Next() {
		var entityID int64
		var publicID, kindCode, canonicalID, registry, defaultLocale, revisionID, iconPath, modSiteID string
		var iconID sql.NullString
		var names []byte
		var ordinal int
		if err = rows.Scan(&entityID, &publicID, &kindCode, &canonicalID, &registry, &ordinal, &defaultLocale, &iconID, &revisionID, &iconPath, &modSiteID, &names); err != nil {
			return nil, err
		}
		locale, name := catalogResolvedName(names, primary, secondary, defaultLocale)
		iconURL := ""
		if iconID.Valid {
			iconURL = "/api/v1/catalog/resources/" + publicID + "/icon"
		}
		items = append(items, map[string]any{"publicId": publicID, "id": canonicalID, "kind": kindCode,
			"registry": registry, "ordinal": ordinal, "locale": locale, "name": name,
			"names": json.RawMessage(names), "iconFileId": nullableCatalogString(iconID), "iconUrl": iconURL,
			"revisionId": revisionID, "iconPath": iconPath, "modSiteId": modSiteID})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(items) > 0 || !allowImportFallback {
		if err = s.decorateResourceVersionRows(ctx, items, primary, secondary); err != nil {
			return nil, err
		}
		return items, nil
	}
	rows, err = s.db.Query(ctx, `with selected as (
		select snapshot.id,snapshot.revision_id from tag_import_snapshots snapshot
		join catalog_import_revisions revision on revision.id=snapshot.revision_id
		where snapshot.tag_id=$1 and revision.is_active and revision.status in ('ready','partial')
	), selected_members as (
		select distinct on(member.resource_id) selected.revision_id,member.resource_id,member.ordinal
		from selected join tag_import_members member on member.tag_snapshot_id=selected.id
		order by member.resource_id,member.ordinal
	)
	select entity.id,entity.public_id,resource.kind_code,resource.canonical_id,resource.namespace,member.ordinal,entity.default_locale,
		(select public_id from oss_files where id=definition.icon_file_id),coalesce(resource_snapshot.revision_id,''),coalesce(resource_snapshot.icon_path,''),coalesce(mod.slug,''),
		coalesce((select jsonb_object_agg(localization.locale,localization.name)
		 from content_localizations localization where localization.catalog_entity_id=entity.id and localization.name<>''),
		 resource_snapshot.names,'{}'::jsonb)
	from selected_members member
	join game_resources resource on resource.entity_id=member.resource_id
	join catalog_entities entity on entity.id=resource.entity_id
	left join catalog_resource_definitions definition on definition.resource_id=resource.entity_id
	left join lateral (select candidate.names,candidate.revision_id,candidate.icon_path from resource_import_snapshots candidate where candidate.resource_id=resource.entity_id
	 order by (candidate.revision_id=member.revision_id) desc,(candidate.icon_path<>'') desc,candidate.created_at desc limit 1) resource_snapshot on true
	left join catalog_import_revisions revision on revision.id=resource_snapshot.revision_id
	left join mods mod on mod.id=revision.mod_id
	order by member.ordinal`, tagID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var entityID int64
		var publicID, kindCode, canonicalID, registry, defaultLocale, revisionID, iconPath, modSiteID string
		var iconID sql.NullString
		var names []byte
		var ordinal int
		if err = rows.Scan(&entityID, &publicID, &kindCode, &canonicalID, &registry, &ordinal, &defaultLocale, &iconID, &revisionID, &iconPath, &modSiteID, &names); err != nil {
			return nil, err
		}
		locale, name := catalogResolvedName(names, primary, secondary, defaultLocale)
		iconURL := ""
		if iconID.Valid {
			iconURL = "/api/v1/catalog/resources/" + publicID + "/icon"
		}
		items = append(items, map[string]any{"publicId": publicID, "id": canonicalID, "kind": kindCode,
			"registry": registry, "ordinal": ordinal, "locale": locale, "name": name,
			"names": json.RawMessage(names), "iconFileId": nullableCatalogString(iconID), "iconUrl": iconURL,
			"revisionId": revisionID, "iconPath": iconPath, "modSiteId": modSiteID, "source": "import"})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = s.decorateResourceVersionRows(ctx, items, primary, secondary); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Server) catalogRecipeTypeCatalysts(ctx context.Context, typeID int64, primary, secondary string, allowImportFallback bool) ([]map[string]any, error) {
	grouped, err := s.catalogRecipeTypesCatalysts(ctx, []int64{typeID}, primary, secondary, allowImportFallback)
	return grouped[typeID], err
}

func (s *Server) catalogRecipeTypesCatalysts(ctx context.Context, typeIDs []int64, primary, secondary string, allowImportFallback bool) (map[int64][]map[string]any, error) {
	result := make(map[int64][]map[string]any, len(typeIDs))
	if len(typeIDs) == 0 {
		return result, nil
	}
	for _, typeID := range typeIDs {
		result[typeID] = []map[string]any{}
	}
	rows, err := s.db.Query(ctx, `select entity.public_id,resource.kind_code,resource.canonical_id,resource.namespace,catalyst.ordinal,
		catalyst.recipe_type_id,
		entity.default_locale,(select public_id from oss_files where id=definition.icon_file_id),
		coalesce((select jsonb_object_agg(localization.locale,localization.name) from content_localizations localization
		 where localization.catalog_entity_id=entity.id and localization.name<>''),'{}'::jsonb)
		from recipe_type_catalysts catalyst join game_resources resource on resource.entity_id=catalyst.resource_id
		left join catalog_resource_definitions definition on definition.resource_id=resource.entity_id
		join catalog_entities entity on entity.id=resource.entity_id
		where catalyst.recipe_type_id=any($1::bigint[]) order by catalyst.recipe_type_id,catalyst.ordinal`, typeIDs)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var publicID, kindCode, canonicalID, registry, defaultLocale string
		var iconID sql.NullString
		var names []byte
		var ordinal int
		var typeID int64
		if err = rows.Scan(&publicID, &kindCode, &canonicalID, &registry, &ordinal, &typeID, &defaultLocale, &iconID, &names); err != nil {
			return nil, err
		}
		locale, name := catalogResolvedName(names, primary, secondary, defaultLocale)
		iconURL := ""
		if iconID.Valid {
			iconURL = "/api/v1/catalog/resources/" + publicID + "/icon"
		}
		result[typeID] = append(result[typeID], map[string]any{"publicId": publicID, "id": canonicalID, "kind": kindCode,
			"registry": registry, "ordinal": ordinal, "locale": locale, "name": name,
			"names": json.RawMessage(names), "iconFileId": nullableCatalogString(iconID), "iconUrl": iconURL})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if !allowImportFallback {
		return result, nil
	}
	missing := make([]int64, 0, len(typeIDs))
	for _, typeID := range typeIDs {
		if len(result[typeID]) == 0 {
			missing = append(missing, typeID)
		}
	}
	if len(missing) == 0 {
		return result, nil
	}
	rows, err = s.db.Query(ctx, `select distinct on (snapshot.recipe_type_id)
		snapshot.recipe_type_id,snapshot.catalysts,snapshot.revision_id from recipe_type_import_snapshots snapshot
		join catalog_import_revisions revision on revision.id=snapshot.revision_id
		where snapshot.recipe_type_id=any($1::bigint[]) and revision.is_active and revision.status in ('ready','partial')
		order by snapshot.recipe_type_id,coalesce(revision.activated_at,revision.created_at) desc,
			revision.created_at desc,snapshot.id desc`, missing)
	if err != nil {
		return nil, err
	}
	batches := make([]catalogCatalystBatch, 0, len(missing))
	for rows.Next() {
		var batch catalogCatalystBatch
		if err = rows.Scan(&batch.TypeID, &batch.Raw, &batch.RevisionID); err != nil {
			rows.Close()
			return nil, err
		}
		batches = append(batches, batch)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	decorated, err := s.decorateCatalystBatches(ctx, batches)
	if err != nil {
		return nil, err
	}
	for typeID, catalysts := range decorated {
		result[typeID] = catalysts
	}
	return result, nil
}

func (s *Server) catalogTemplateSlotRows(ctx context.Context, templateID int64) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `select slot_key,role,output_index,ordinal,x::float8,y::float8,width::float8,height::float8,definition
		from recipe_template_slots where template_id=$1 order by ordinal`, templateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var key, role string
		var outputIndex sql.NullInt64
		var ordinal int
		var x, y, width, height float64
		var definition []byte
		if err = rows.Scan(&key, &role, &outputIndex, &ordinal, &x, &y, &width, &height, &definition); err != nil {
			return nil, err
		}
		var outputIndexValue any
		if outputIndex.Valid {
			outputIndexValue = outputIndex.Int64
		}
		items = append(items, map[string]any{"slotKey": key, "role": role, "outputIndex": outputIndexValue, "ordinal": ordinal,
			"rect": map[string]any{"x": x, "y": y, "width": width, "height": height}, "definition": json.RawMessage(definition)})
	}
	return items, rows.Err()
}

func (s *Server) catalogRecipeBindingRows(ctx context.Context, recipeID int64) (map[string]any, error) {
	rows, err := s.db.Query(ctx, `select slot.slot_key,binding.definition,(candidate.id is not null),
		coalesce(entity.public_id,''),coalesce(resource.kind_code,''),coalesce(resource.canonical_id,''),coalesce(resource.resolved,false),
		coalesce(candidate.amount::float8,0),candidate.probability::float8,coalesce(candidate.byproduct,false),
		coalesce(candidate.definition,'{}'::jsonb),coalesce(icon_file.public_id,''),
		coalesce(imported.revision_id,''),coalesce(imported.icon_path,'')
		from recipe_bindings binding join recipe_template_slots slot on slot.id=binding.template_slot_id
		left join recipe_binding_candidates candidate on candidate.binding_id=binding.id
		left join game_resources resource on resource.entity_id=candidate.resource_id
		left join catalog_entities entity on entity.id=resource.entity_id
		left join catalog_resource_definitions resource_definition on resource_definition.resource_id=resource.entity_id
		left join oss_files icon_file on icon_file.id=resource_definition.icon_file_id and icon_file.status='active'
		left join lateral (select source.revision_id,source.icon_path from resource_import_snapshots source
		 where source.resource_id=resource.entity_id order by (source.icon_path<>'') desc,source.created_at desc limit 1) imported on true
		where binding.recipe_id=$1 order by binding.ordinal,binding.id,candidate.candidate_index`, recipeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]any{}
	for rows.Next() {
		var slotKey, publicID, kindCode, canonicalID, iconFileID, revisionID, iconPath string
		var bindingDefinition, candidateDefinition []byte
		var hasCandidate, resolved, byproduct bool
		var amount float64
		var probability sql.NullFloat64
		if err = rows.Scan(&slotKey, &bindingDefinition, &hasCandidate, &publicID, &kindCode, &canonicalID, &resolved,
			&amount, &probability, &byproduct, &candidateDefinition, &iconFileID, &revisionID, &iconPath); err != nil {
			return nil, err
		}
		binding, exists := result[slotKey].(map[string]any)
		if !exists {
			binding = map[string]any{"definition": json.RawMessage(bindingDefinition), "candidates": []map[string]any{}}
			result[slotKey] = binding
		}
		if !hasCandidate {
			continue
		}
		var probabilityValue any
		if probability.Valid {
			probabilityValue = probability.Float64
		}
		candidate := map[string]any{"resourcePublicId": publicID, "kindCode": kindCode, "canonicalId": canonicalID,
			"unresolved": !resolved, "rawResourceId": catalogUnresolvedRawID(resolved, canonicalID),
			"amount": amount, "probability": probabilityValue, "byproduct": byproduct, "definition": json.RawMessage(candidateDefinition)}
		if resolved {
			candidate["iconUrl"] = catalogRecipeResourceIconURL(publicID, iconFileID, revisionID, iconPath)
		}
		candidates, _ := binding["candidates"].([]map[string]any)
		binding["candidates"] = append(candidates, candidate)
	}
	return result, rows.Err()
}

func (s *Server) catalogImportedRecipeBindingRows(ctx context.Context, snapshotID string) (map[string]any, error) {
	rows, err := s.db.Query(ctx, `select binding.id,binding.source_slot_id,coalesce(nullif(binding.semantic_role,''),slot.role,''),
		candidate.alternative_index,coalesce(resource_entity.public_id,''),coalesce(candidate.raw_resource_id,''),candidate.amount,
		coalesce(candidate.chance,candidate.chance_percent/100.0),candidate.byproduct,
		coalesce(icon_file.public_id,''),coalesce(imported.revision_id,''),coalesce(imported.icon_path,'')
		from recipe_import_bindings binding
		join recipe_template_import_slots slot on slot.id=binding.template_slot_id
		left join recipe_import_binding_candidates candidate on candidate.binding_id=binding.id
		left join game_resources resource on resource.entity_id=candidate.resource_id
		left join catalog_entities resource_entity on resource_entity.id=resource.entity_id
		left join catalog_resource_definitions definition on definition.resource_id=resource.entity_id
		left join oss_files icon_file on icon_file.id=definition.icon_file_id and icon_file.status='active'
		left join lateral (select source.revision_id,source.icon_path from resource_import_snapshots source
		 where source.resource_id=resource.entity_id order by (source.icon_path<>'') desc,source.created_at desc limit 1) imported on true
		where binding.recipe_snapshot_id=$1 order by binding.ordinal,candidate.alternative_index`, snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]any{}
	for rows.Next() {
		var bindingID, slotKey, role, resourcePublicID, rawResourceID, iconFileID, revisionID, iconPath string
		var candidateIndex sql.NullInt64
		var amount, probability sql.NullFloat64
		var byproduct sql.NullBool
		if err = rows.Scan(&bindingID, &slotKey, &role, &candidateIndex, &resourcePublicID, &rawResourceID,
			&amount, &probability, &byproduct, &iconFileID, &revisionID, &iconPath); err != nil {
			return nil, err
		}
		binding, exists := result[slotKey].(map[string]any)
		if !exists {
			binding = map[string]any{"role": role, "definition": map[string]any{}, "candidates": []map[string]any{}}
			result[slotKey] = binding
		}
		if !candidateIndex.Valid {
			continue
		}
		candidate := map[string]any{"resourcePublicId": resourcePublicID, "rawResourceId": rawResourceID,
			"amount": nullableSQLFloat(amount), "probability": nullableSQLFloat(probability), "byproduct": byproduct.Valid && byproduct.Bool,
			"definition": map[string]any{}}
		if rawResourceID == "" {
			candidate["iconUrl"] = catalogRecipeResourceIconURL(resourcePublicID, iconFileID, revisionID, iconPath)
		}
		candidates, _ := binding["candidates"].([]map[string]any)
		binding["candidates"] = append(candidates, candidate)
	}
	return result, rows.Err()
}

func catalogRecipeResourceIconURL(publicID, iconFileID, revisionID, iconPath string) string {
	if publicID != "" && (iconFileID != "" || iconPath != "") {
		return "/api/v1/catalog/resources/" + url.PathEscape(publicID) + "/icon"
	}
	if revisionID != "" && iconPath != "" {
		return "/api/v1/export-revisions/" + url.PathEscape(revisionID) + "/assets/content?path=" + url.QueryEscape(iconPath)
	}
	return ""
}

func catalogUnresolvedRawID(resolved bool, canonicalID string) string {
	if resolved {
		return ""
	}
	return canonicalID
}
