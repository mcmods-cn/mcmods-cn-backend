package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"mcmods-cn-backend/internal/activity"

	"github.com/jackc/pgx/v5"
)

const (
	modContentAggregateVersion  = "mod_content_version"
	modContentAggregateTemplate = "mod_content_template"
	modContentAggregateSection  = "mod_content_section"
	modContentAggregateResource = "mod_content_resource"
)

type modContentVersionEdit struct {
	Label             string   `json:"label"`
	MinecraftVersions []string `json:"minecraftVersions"`
	Loaders           []string `json:"loaders"`
	ModVersion        string   `json:"modVersion"`
	Reason            string   `json:"reason"`
	BaseRevisionID    *int64   `json:"baseRevisionId,omitempty"`
}

type modContentTemplateEdit struct {
	Code               string                    `json:"code"`
	DefaultLocale      string                    `json:"defaultLocale"`
	DefaultDisplayMode string                    `json:"defaultDisplayMode"`
	Definition         map[string]any            `json:"definition"`
	Localizations      []catalogLocalizationEdit `json:"localizations"`
	Reason             string                    `json:"reason"`
	BaseRevisionID     *int64                    `json:"baseRevisionId,omitempty"`
}

type modContentSectionResourceEdit struct {
	VersionPublicID  string `json:"versionPublicId"`
	ResourcePublicID string `json:"resourcePublicId"`
	Ordinal          int    `json:"ordinal"`
}

type modContentSectionEdit struct {
	VersionPublicID  string                          `json:"versionPublicId"`
	TemplatePublicID string                          `json:"templatePublicId"`
	ParentPublicID   string                          `json:"parentPublicId"`
	DefaultLocale    string                          `json:"defaultLocale"`
	DisplayMode      string                          `json:"displayMode"`
	Ordinal          int                             `json:"ordinal"`
	Localizations    []catalogLocalizationEdit       `json:"localizations"`
	Resources        []modContentSectionResourceEdit `json:"resources"`
	Reason           string                          `json:"reason"`
	BaseRevisionID   *int64                          `json:"baseRevisionId,omitempty"`
}

type modContentResourceEdit struct {
	ResourcePublicID string                    `json:"resourcePublicId"`
	KindCode         string                    `json:"kindCode"`
	CanonicalID      string                    `json:"canonicalId"`
	VersionPublicID  string                    `json:"versionPublicId"`
	DefaultLocale    string                    `json:"defaultLocale"`
	Definition       map[string]any            `json:"definition"`
	Localizations    []catalogLocalizationEdit `json:"localizations"`
	Reason           string                    `json:"reason"`
	BaseRevisionID   *int64                    `json:"baseRevisionId,omitempty"`
}

type modContentSnapshot struct {
	Kind            string                  `json:"kind"`
	Operation       string                  `json:"operation"`
	ModID           int64                   `json:"modId"`
	ModSiteID       string                  `json:"modSiteId"`
	PublicID        string                  `json:"publicId"`
	Version         *modContentVersionEdit  `json:"version,omitempty"`
	Template        *modContentTemplateEdit `json:"template,omitempty"`
	Section         *modContentSectionEdit  `json:"section,omitempty"`
	Resource        *modContentResourceEdit `json:"resource,omitempty"`
	CreatedIdentity bool                    `json:"createdIdentity,omitempty"`
}

type modContentMutationResult struct {
	PublicID        string `json:"publicId"`
	RevisionID      int64  `json:"revisionId"`
	ChangeRequestID int64  `json:"changeRequestId"`
	ReviewStatus    string `json:"reviewStatus"`
	ActivityEventID int64  `json:"activityEventId"`
}

func modContentAggregate(aggregateType string) bool {
	return aggregateType == modContentAggregateVersion || aggregateType == modContentAggregateTemplate || aggregateType == modContentAggregateSection || aggregateType == modContentAggregateResource
}

func normalizeModContentResourceEdit(edit *modContentResourceEdit) error {
	edit.ResourcePublicID = strings.ToLower(strings.TrimSpace(edit.ResourcePublicID))
	edit.KindCode = strings.ToLower(strings.TrimSpace(edit.KindCode))
	edit.CanonicalID = strings.ToLower(strings.TrimSpace(edit.CanonicalID))
	edit.VersionPublicID = strings.ToLower(strings.TrimSpace(edit.VersionPublicID))
	edit.Reason = strings.TrimSpace(edit.Reason)
	if len(edit.Reason) > 500 || edit.VersionPublicID == "" || (edit.ResourcePublicID == "" && (edit.KindCode == "" || edit.CanonicalID == "")) {
		return errCatalogEditorInvalid
	}
	if edit.Definition == nil {
		edit.Definition = map[string]any{}
	}
	defaultLocale, localizations, err := normalizeCatalogLocalizations(edit.DefaultLocale, edit.Localizations)
	if err != nil || requireCatalogCreateDefaultLocalization(defaultLocale, localizations) != nil {
		return errCatalogEditorInvalid
	}
	edit.DefaultLocale, edit.Localizations = defaultLocale, localizations
	return nil
}

func normalizeStringList(values []string, maximum int) ([]string, error) {
	if len(values) > maximum {
		return nil, errCatalogEditorInvalid
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 80 {
			return nil, errCatalogEditorInvalid
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func normalizeModContentVersionEdit(edit *modContentVersionEdit) error {
	edit.ModVersion = strings.TrimSpace(edit.ModVersion)
	edit.Reason = strings.TrimSpace(edit.Reason)
	if len(edit.ModVersion) > 160 || len(edit.Reason) > 500 {
		return errCatalogEditorInvalid
	}
	var err error
	if edit.MinecraftVersions, err = normalizeStringList(edit.MinecraftVersions, 100); err != nil || len(edit.MinecraftVersions) == 0 {
		return errCatalogEditorInvalid
	}
	if edit.Loaders, err = normalizeStringList(edit.Loaders, 30); err != nil || len(edit.Loaders) == 0 {
		return errCatalogEditorInvalid
	}
	edit.Label = strings.Join(edit.MinecraftVersions, ", ") + " / " + strings.Join(edit.Loaders, ", ")
	if len(edit.Label) > 160 {
		return errCatalogEditorInvalid
	}
	return nil
}

func modContentVersionSelectionSupported(edit modContentVersionEdit, compatibilities []modLoaderCompatibilityPayload) bool {
	allowed := make(map[string]map[string]struct{}, len(compatibilities))
	for _, compatibility := range compatibilities {
		loader := strings.ToLower(strings.TrimSpace(compatibility.Loader))
		if loader == "" {
			continue
		}
		if allowed[loader] == nil {
			allowed[loader] = make(map[string]struct{}, len(compatibility.Versions))
		}
		for _, version := range compatibility.Versions {
			allowed[loader][strings.ToLower(strings.TrimSpace(version))] = struct{}{}
		}
	}
	for _, loader := range edit.Loaders {
		versions := allowed[strings.ToLower(loader)]
		if len(versions) == 0 {
			return false
		}
		for _, version := range edit.MinecraftVersions {
			if _, ok := versions[strings.ToLower(version)]; !ok {
				return false
			}
		}
	}
	return true
}

func globalModContentCompatibilities(config minecraftVersionConfig) []modLoaderCompatibilityPayload {
	versions := make([]string, 0, len(config.Versions))
	for _, version := range config.Versions {
		if code := strings.TrimSpace(version.Code); code != "" {
			versions = append(versions, code)
		}
	}
	compatibilities := make([]modLoaderCompatibilityPayload, 0, len(config.Loaders))
	for _, loader := range config.Loaders {
		if code := strings.TrimSpace(loader.Code); code != "" {
			compatibilities = append(compatibilities, modLoaderCompatibilityPayload{Loader: code, Versions: append([]string(nil), versions...)})
		}
	}
	return compatibilities
}

func (s *Server) validateModContentVersionSelection(ctx context.Context, modID int64, edit modContentVersionEdit) error {
	rows, err := s.db.Query(ctx, `select loader,array_agg(minecraft_version order by minecraft_version desc)
		from mod_loader_compatibilities where mod_id=$1 group by loader order by loader`, modID)
	if err != nil {
		return err
	}
	defer rows.Close()
	compatibilities := make([]modLoaderCompatibilityPayload, 0)
	for rows.Next() {
		var compatibility modLoaderCompatibilityPayload
		if err = rows.Scan(&compatibility.Loader, &compatibility.Versions); err != nil {
			return err
		}
		compatibilities = append(compatibilities, compatibility)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(compatibilities) == 0 {
		compatibilities = globalModContentCompatibilities(loadMinecraftVersionConfig(ctx, s.db))
	}
	if !modContentVersionSelectionSupported(edit, compatibilities) {
		return errCatalogEditorInvalid
	}
	return nil
}

var modContentTemplateCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)

func normalizeModContentTemplateEdit(edit *modContentTemplateEdit) error {
	edit.Code = strings.ToLower(strings.TrimSpace(edit.Code))
	edit.DefaultDisplayMode = strings.ToLower(strings.TrimSpace(edit.DefaultDisplayMode))
	edit.Reason = strings.TrimSpace(edit.Reason)
	if !modContentTemplateCodePattern.MatchString(edit.Code) || (edit.DefaultDisplayMode != "compact" && edit.DefaultDisplayMode != "large") || len(edit.Reason) > 500 {
		return errCatalogEditorInvalid
	}
	if edit.Definition == nil {
		edit.Definition = map[string]any{}
	}
	defaultLocale, localizations, err := normalizeCatalogLocalizations(edit.DefaultLocale, edit.Localizations)
	if err != nil || len(localizations) == 0 || defaultLocale == "" || requireCatalogCreateDefaultLocalization(defaultLocale, localizations) != nil {
		return errCatalogEditorInvalid
	}
	edit.Localizations = localizations
	edit.DefaultLocale = defaultLocale
	return nil
}

func normalizeModContentSectionEdit(edit *modContentSectionEdit) error {
	edit.VersionPublicID = strings.ToLower(strings.TrimSpace(edit.VersionPublicID))
	edit.TemplatePublicID = strings.ToLower(strings.TrimSpace(edit.TemplatePublicID))
	edit.ParentPublicID = strings.ToLower(strings.TrimSpace(edit.ParentPublicID))
	edit.DisplayMode = strings.ToLower(strings.TrimSpace(edit.DisplayMode))
	edit.Reason = strings.TrimSpace(edit.Reason)
	if edit.VersionPublicID == "" || edit.TemplatePublicID == "" || (edit.DisplayMode != "compact" && edit.DisplayMode != "large") || edit.Ordinal < 0 || len(edit.Reason) > 500 || len(edit.Resources) > 20000 {
		return errCatalogEditorInvalid
	}
	defaultLocale, localizations, err := normalizeCatalogLocalizations(edit.DefaultLocale, edit.Localizations)
	if err != nil || defaultLocale == "" || (len(localizations) > 0 && requireCatalogCreateDefaultLocalization(defaultLocale, localizations) != nil) {
		return errCatalogEditorInvalid
	}
	edit.Localizations = localizations
	edit.DefaultLocale = defaultLocale
	seen := make(map[string]struct{}, len(edit.Resources))
	for index := range edit.Resources {
		resource := &edit.Resources[index]
		resource.VersionPublicID = strings.ToLower(strings.TrimSpace(resource.VersionPublicID))
		resource.ResourcePublicID = strings.ToLower(strings.TrimSpace(resource.ResourcePublicID))
		if resource.VersionPublicID == "" || resource.ResourcePublicID == "" || resource.Ordinal < 0 {
			return errCatalogEditorInvalid
		}
		if resource.VersionPublicID != edit.VersionPublicID {
			return errCatalogEditorInvalid
		}
		key := resource.VersionPublicID + "\x00" + resource.ResourcePublicID
		if _, ok := seen[key]; ok {
			return errCatalogEditorInvalid
		}
		seen[key] = struct{}{}
	}
	return nil
}

func lockedModContentDisplayMode(templateCode, defaultMode, requestedMode string) string {
	if templateCode == "advancement" {
		return defaultMode
	}
	return requestedMode
}

func (s *Server) enforceModContentSectionDisplayMode(ctx context.Context, modID int64, edit *modContentSectionEdit) error {
	var templateCode, defaultMode string
	if err := s.db.QueryRow(ctx, `select code,default_display_mode from mod_content_templates
		where public_id=$1 and status='active' and (builtin or owner_mod_id=$2)`, edit.TemplatePublicID, modID).
		Scan(&templateCode, &defaultMode); err != nil {
		return err
	}
	edit.DisplayMode = lockedModContentDisplayMode(templateCode, defaultMode, edit.DisplayMode)
	return nil
}

func (s *Server) modContentVersions(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requireEditableMod(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		var edit modContentVersionEdit
		if decodeJSON(r, &edit) != nil || edit.BaseRevisionID != nil || normalizeModContentVersionEdit(&edit) != nil {
			writeError(w, http.StatusUnprocessableEntity, "invalid mod content version")
			return
		}
		if err := s.validateModContentVersionSelection(r.Context(), identity.ID, edit); err != nil {
			if errors.Is(err, errCatalogEditorInvalid) {
				writeError(w, http.StatusUnprocessableEntity, "the selected Minecraft versions and loaders are not supported by this mod")
			} else {
				writeError(w, http.StatusInternalServerError, "failed to validate mod compatibility")
			}
			return
		}
		s.submitNewModContentVersion(w, r, identity, edit)
		return
	}
	includePending := canEditMod(currentClaims(r), identity) || hasPermission(currentClaims(r).Permissions, "content.review")
	rows, err := s.db.Query(r.Context(), `select public_id,label,minecraft_versions,loaders,mod_version,status,
		published_revision_id,created_at,updated_at from mod_content_versions where mod_id=$1 and ($2 or status='active')
		order by status='active' desc,updated_at desc,id desc`, identity.ID, includePending)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read mod content versions")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var publicID, label, modVersion, status string
		var minecraftVersions, loaders []string
		var publishedRevisionID *int64
		var createdAt, updatedAt time.Time
		if err = rows.Scan(&publicID, &label, &minecraftVersions, &loaders, &modVersion, &status, &publishedRevisionID, &createdAt, &updatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode mod content versions")
			return
		}
		items = append(items, map[string]any{"publicId": publicID, "label": label, "minecraftVersions": minecraftVersions,
			"loaders": loaders, "modVersion": modVersion, "status": status,
			"publishedRevisionId": publishedRevisionID, "createdAt": createdAt, "updatedAt": updatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) modContentVersion(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requireEditableMod(w, r)
	if !ok {
		return
	}
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("versionId")))
	var status string
	var publishedRevisionID *int64
	if err := s.db.QueryRow(r.Context(), `select status,published_revision_id from mod_content_versions where mod_id=$1 and public_id=$2`, identity.ID, publicID).
		Scan(&status, &publishedRevisionID); err != nil {
		writeError(w, http.StatusNotFound, "mod content version not found")
		return
	}
	if status != "active" {
		writeError(w, http.StatusConflict, "mod content version is not editable")
		return
	}
	if r.Method == http.MethodDelete {
		s.submitExistingModContentMutation(w, r, identity, modContentSnapshot{Kind: "version", Operation: "delete", ModID: identity.ID, ModSiteID: identity.SiteID, PublicID: publicID}, publishedRevisionID)
		return
	}
	var edit modContentVersionEdit
	if decodeJSON(r, &edit) != nil || normalizeModContentVersionEdit(&edit) != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid mod content version")
		return
	}
	if err := s.validateModContentVersionSelection(r.Context(), identity.ID, edit); err != nil {
		if errors.Is(err, errCatalogEditorInvalid) {
			writeError(w, http.StatusUnprocessableEntity, "the selected Minecraft versions and loaders are not supported by this mod")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to validate mod compatibility")
		}
		return
	}
	if !sameRevision(edit.BaseRevisionID, publishedRevisionID) {
		writeError(w, http.StatusConflict, "mod content version changed; reload the editor")
		return
	}
	s.submitExistingModContentMutation(w, r, identity, modContentSnapshot{Kind: "version", Operation: "edit", ModID: identity.ID, ModSiteID: identity.SiteID, PublicID: publicID, Version: &edit}, publishedRevisionID)
}

func (s *Server) submitNewModContentVersion(w http.ResponseWriter, r *http.Request, identity modIdentityRecord, edit modContentVersionEdit) {
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start mod content revision")
		return
	}
	defer tx.Rollback(r.Context())
	var publicID string
	err = tx.QueryRow(r.Context(), `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,mod_version,status,created_by,updated_by)
		values($1,$2,$3,$4,$5,'pending',$6,$6) returning public_id`, identity.ID, edit.Label, edit.MinecraftVersions, edit.Loaders, edit.ModVersion, currentClaims(r).Subject).Scan(&publicID)
	if err != nil {
		writeError(w, http.StatusConflict, "failed to reserve mod content version")
		return
	}
	snapshot := modContentSnapshot{Kind: "version", Operation: "create", ModID: identity.ID, ModSiteID: identity.SiteID, PublicID: publicID, Version: &edit}
	result, err := s.createModContentRevisionTx(r, tx, identity, snapshot, nil)
	if err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, http.StatusInternalServerError, "failed to submit mod content version")
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) submitExistingModContentMutation(w http.ResponseWriter, r *http.Request, identity modIdentityRecord, snapshot modContentSnapshot, baseRevisionID *int64) {
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start mod content revision")
		return
	}
	defer tx.Rollback(r.Context())
	result, err := s.createModContentRevisionTx(r, tx, identity, snapshot, baseRevisionID)
	if err != nil {
		writeError(w, catalogEditorHTTPStatus(err), catalogEditorErrorMessage(err))
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit mod content revision")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) createModContentRevisionTx(r *http.Request, tx pgx.Tx, identity modIdentityRecord, snapshot modContentSnapshot, baseRevisionID *int64) (modContentMutationResult, error) {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return modContentMutationResult{}, err
	}
	claims := currentClaims(r)
	config := loadReviewConfig(r.Context(), s.db)
	reviewRequired := modContentReviewRequired(config, snapshot)
	status := "approved"
	if reviewRequired && !catalogMutationBypassesReview(claims.Permissions) && !canSkipProjectReview(claims, identity) {
		status = "pending"
	}
	aggregateType := modContentAggregateVersion
	if snapshot.Kind == "template" {
		aggregateType = modContentAggregateTemplate
	} else if snapshot.Kind == "section" {
		aggregateType = modContentAggregateSection
	} else if snapshot.Kind == "resource" {
		aggregateType = modContentAggregateResource
	}
	reason := "Mod content " + snapshot.Operation
	if snapshot.Version != nil && snapshot.Version.Reason != "" {
		reason = snapshot.Version.Reason
	} else if snapshot.Template != nil && snapshot.Template.Reason != "" {
		reason = snapshot.Template.Reason
	} else if snapshot.Section != nil && snapshot.Section.Reason != "" {
		reason = snapshot.Section.Reason
	} else if snapshot.Resource != nil && snapshot.Resource.Reason != "" {
		reason = snapshot.Resource.Reason
	}
	aggregateKey := snapshot.PublicID
	if snapshot.Kind == "resource" && snapshot.Resource != nil {
		aggregateKey += ":" + snapshot.Resource.VersionPublicID
	}
	created, err := createContentRevisionTx(r.Context(), tx, createContentRevisionParams{
		AggregateType: aggregateType, AggregateKey: aggregateKey, BaseRevision: baseRevisionID, Snapshot: raw,
		Reason: reason, ActorID: claims.Subject, Source: "user", Status: status,
		Metadata: map[string]any{"kind": snapshot.Kind, "operation": snapshot.Operation, "modId": identity.ID, "siteId": identity.SiteID, "publicId": snapshot.PublicID}, Request: r,
	})
	if err != nil {
		return modContentMutationResult{}, err
	}
	if status == "approved" {
		if err = publishModContentSnapshotTx(r.Context(), tx, created.RevisionID, snapshot, claims.Subject); err != nil {
			return modContentMutationResult{}, err
		}
		if err = appendReviewResolutionTx(r.Context(), tx, created.ChangeRequestID, "approved", claims.Subject, "automatic approval", r); err != nil {
			return modContentMutationResult{}, err
		}
		if err = appendCatalogPublishedReviewEventTx(r.Context(), tx, created.ChangeRequestID, claims.Subject, "automatic publication", r); err != nil {
			return modContentMutationResult{}, err
		}
	}
	activityID, err := insertModContentActivityTx(r.Context(), tx, claims.Subject, snapshot, created, status)
	if err != nil {
		return modContentMutationResult{}, err
	}
	skipRequestActivity(r)
	return modContentMutationResult{PublicID: snapshot.PublicID, RevisionID: created.RevisionID, ChangeRequestID: created.ChangeRequestID, ReviewStatus: status, ActivityEventID: activityID}, nil
}

func modContentReviewRequired(config reviewConfig, snapshot modContentSnapshot) bool {
	if snapshot.Operation == "create" {
		if snapshot.Kind == "section" {
			return config.ModContentSectionCreate
		}
		return config.CatalogCreate
	}
	if snapshot.Operation == "delete" {
		return config.CatalogDelete
	}
	return config.CatalogEdit
}

func insertModContentActivityTx(ctx context.Context, tx pgx.Tx, actorID int64, snapshot modContentSnapshot, created createdContentRevision, reviewStatus string) (int64, error) {
	actionID := activity.ActionEdit
	if snapshot.Operation == "create" {
		actionID = activity.ActionCreate
	} else if snapshot.Operation == "delete" {
		actionID = activity.ActionDelete
	}
	metadata, err := json.Marshal(map[string]any{"revisionId": created.RevisionID, "changeRequestId": created.ChangeRequestID,
		"kind": snapshot.Kind, "operation": snapshot.Operation, "reviewStatus": reviewStatus, "modId": snapshot.ModID})
	if err != nil {
		return 0, err
	}
	var id int64
	err = tx.QueryRow(ctx, `insert into user_activity_events(user_id,action_id,object_type_id,object_public_id,metadata,occurred_at)
		values($1,$2,$3,$4,$5::jsonb,$6) returning id`, actorID, actionID, activity.ObjectMod, snapshot.PublicID, string(metadata), time.Now().UTC()).Scan(&id)
	return id, err
}

func (s *Server) requireEditableMod(w http.ResponseWriter, r *http.Request) (modIdentityRecord, bool) {
	identity, err := s.modIdentity(r.Context(), r.PathValue("siteId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "mod not found")
		return identity, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read mod")
		return identity, false
	}
	if !canEditMod(currentClaims(r), identity) && r.Method != http.MethodGet {
		writeError(w, http.StatusForbidden, "permission denied")
		return identity, false
	}
	return identity, true
}

func (s *Server) modContentTemplates(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requireEditableMod(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		var edit modContentTemplateEdit
		if decodeJSON(r, &edit) != nil || edit.BaseRevisionID != nil || normalizeModContentTemplateEdit(&edit) != nil {
			writeError(w, http.StatusUnprocessableEntity, "invalid content template")
			return
		}
		s.submitNewModContentTemplate(w, r, identity, edit)
		return
	}
	rows, err := s.db.Query(r.Context(), `select template.public_id,template.code,template.builtin,template.i18n_key,template.default_locale,
		template.default_display_mode,template.definition,template.status,template.published_revision_id,
		coalesce((select jsonb_agg(jsonb_build_object('locale',localization.locale,'name',localization.name,'summary',localization.description,'contentMarkdown','') order by localization.locale)
		 from mod_content_template_localizations localization where localization.template_id=template.id),'[]'::jsonb)
		from mod_content_templates template where template.builtin or (template.owner_mod_id=$1 and template.status='active')
		order by template.builtin desc,template.code`, identity.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read content templates")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var publicID, code, i18nKey, defaultLocale, displayMode, status string
		var builtin bool
		var definition, localizations []byte
		var revisionID *int64
		if err = rows.Scan(&publicID, &code, &builtin, &i18nKey, &defaultLocale, &displayMode, &definition, &status, &revisionID, &localizations); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode content templates")
			return
		}
		items = append(items, map[string]any{"publicId": publicID, "code": code, "builtin": builtin, "i18nKey": i18nKey, "defaultLocale": defaultLocale,
			"defaultDisplayMode": displayMode, "definition": json.RawMessage(definition), "status": status,
			"publishedRevisionId": revisionID, "localizations": json.RawMessage(localizations)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) submitNewModContentTemplate(w http.ResponseWriter, r *http.Request, identity modIdentityRecord, edit modContentTemplateEdit) {
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start template revision")
		return
	}
	defer tx.Rollback(r.Context())
	definition, _ := json.Marshal(edit.Definition)
	var publicID string
	err = tx.QueryRow(r.Context(), `insert into mod_content_templates(owner_mod_id,code,builtin,default_locale,default_display_mode,definition,status,created_by)
		values($1,$2,false,$3,$4,$5::jsonb,'pending',$6) returning public_id`, identity.ID, edit.Code, edit.DefaultLocale, edit.DefaultDisplayMode, string(definition), currentClaims(r).Subject).Scan(&publicID)
	if err != nil {
		writeError(w, http.StatusConflict, "content template code already exists")
		return
	}
	snapshot := modContentSnapshot{Kind: "template", Operation: "create", ModID: identity.ID, ModSiteID: identity.SiteID, PublicID: publicID, Template: &edit}
	result, err := s.createModContentRevisionTx(r, tx, identity, snapshot, nil)
	if err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, http.StatusInternalServerError, "failed to submit content template")
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) modContentTemplate(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requireEditableMod(w, r)
	if !ok {
		return
	}
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("templateId")))
	var builtin bool
	var status string
	var publishedRevisionID *int64
	if err := s.db.QueryRow(r.Context(), `select builtin,status,published_revision_id from mod_content_templates
		where public_id=$1 and owner_mod_id=$2`, publicID, identity.ID).Scan(&builtin, &status, &publishedRevisionID); err != nil {
		writeError(w, http.StatusNotFound, "custom content template not found")
		return
	}
	if builtin || status != "active" {
		writeError(w, http.StatusConflict, "content template is not editable")
		return
	}
	if r.Method == http.MethodDelete {
		s.submitExistingModContentMutation(w, r, identity, modContentSnapshot{Kind: "template", Operation: "delete", ModID: identity.ID, ModSiteID: identity.SiteID, PublicID: publicID}, publishedRevisionID)
		return
	}
	var edit modContentTemplateEdit
	if decodeJSON(r, &edit) != nil || normalizeModContentTemplateEdit(&edit) != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid content template")
		return
	}
	if !sameRevision(edit.BaseRevisionID, publishedRevisionID) {
		writeError(w, http.StatusConflict, "content template changed; reload the editor")
		return
	}
	s.submitExistingModContentMutation(w, r, identity, modContentSnapshot{Kind: "template", Operation: "edit", ModID: identity.ID, ModSiteID: identity.SiteID, PublicID: publicID, Template: &edit}, publishedRevisionID)
}

func (s *Server) modContentSections(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requireEditableMod(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		var edit modContentSectionEdit
		if decodeJSON(r, &edit) != nil || edit.BaseRevisionID != nil || normalizeModContentSectionEdit(&edit) != nil {
			writeError(w, http.StatusUnprocessableEntity, "invalid content section")
			return
		}
		if s.enforceModContentSectionDisplayMode(r.Context(), identity.ID, &edit) != nil {
			writeError(w, http.StatusUnprocessableEntity, "content template not found")
			return
		}
		s.submitNewModContentSection(w, r, identity, edit)
		return
	}
	rows, err := s.db.Query(r.Context(), `select section.public_id,version.public_id,template.public_id,template.code,template.builtin,template.i18n_key,
		coalesce(parent.public_id,''),section.default_locale,section.display_mode,
		section.ordinal,section.status,section.published_revision_id,
		coalesce((select jsonb_agg(jsonb_build_object('locale',localization.locale,'name',localization.name,'summary',localization.description,'contentMarkdown','') order by localization.locale)
		 from mod_content_section_localizations localization where localization.section_id=section.id),'[]'::jsonb),
		coalesce((select count(*)::int from mod_content_section_resources resource where resource.section_id=section.id),0)
		from mod_content_sections section join mod_content_versions version on version.id=section.version_id
		join mod_content_templates template on template.id=section.template_id
		left join mod_content_sections parent on parent.id=section.parent_id
		where section.mod_id=$1 and section.status='active' order by section.ordinal,section.id`, identity.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read content sections")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var publicID, versionPublicID, templatePublicID, templateCode, templateI18nKey, parentPublicID, defaultLocale, displayMode, status string
		var templateBuiltin bool
		var ordinal, resourceCount int
		var revisionID *int64
		var localizations []byte
		if err = rows.Scan(&publicID, &versionPublicID, &templatePublicID, &templateCode, &templateBuiltin, &templateI18nKey, &parentPublicID, &defaultLocale, &displayMode, &ordinal, &status, &revisionID, &localizations, &resourceCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode content sections")
			return
		}
		items = append(items, map[string]any{"publicId": publicID, "versionPublicId": versionPublicID, "templatePublicId": templatePublicID,
			"templateCode": templateCode, "templateBuiltin": templateBuiltin, "templateI18nKey": templateI18nKey, "parentPublicId": parentPublicID, "defaultLocale": defaultLocale,
			"displayMode": displayMode, "ordinal": ordinal, "status": status, "publishedRevisionId": revisionID,
			"localizations": json.RawMessage(localizations), "resourceCount": resourceCount})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) modContentSectionResources(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requireEditableMod(w, r)
	if !ok {
		return
	}
	sectionPublicID := strings.ToLower(strings.TrimSpace(r.PathValue("sectionId")))
	primary, secondary := s.requestContentLocales(r)
	localeCandidates := []string{strings.ToLower(normalizeContentLocale(primary)), strings.ToLower(normalizeContentLocale(secondary)), "en", "en-us", "zh-cn", "zh-tw"}
	// Advancement boards need the complete parent graph in one response. The
	// public UI still requests 120 rows for ordinary sections, while explicitly
	// requesting the larger bound only for the dedicated tree presentation.
	limit := boundedLimit(r.URL.Query().Get("limit"), 120, 2000)
	offset, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("offset")))
	if offset < 0 {
		offset = 0
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	var sectionID, versionID int64
	var publicID, versionPublicID, versionLabel, templatePublicID, templateCode, templateI18nKey, parentPublicID, defaultLocale, displayMode, status string
	var templateBuiltin bool
	var ordinal int
	var revisionID *int64
	var localizations []byte
	err := s.db.QueryRow(r.Context(), `select section.id,section.version_id,section.public_id,version.public_id,version.label,
		template.public_id,template.code,template.builtin,template.i18n_key,coalesce(parent.public_id,''),section.default_locale,
		section.display_mode,section.ordinal,section.status,section.published_revision_id,
		coalesce((select jsonb_agg(jsonb_build_object('locale',localization.locale,'name',localization.name,
		 'summary',localization.description,'contentMarkdown','') order by localization.locale)
		 from mod_content_section_localizations localization where localization.section_id=section.id),'[]'::jsonb)
		from mod_content_sections section join mod_content_versions version on version.id=section.version_id
		join mod_content_templates template on template.id=section.template_id
		left join mod_content_sections parent on parent.id=section.parent_id
		where section.mod_id=$1 and section.public_id=$2 and section.status='active'`, identity.ID, sectionPublicID).
		Scan(&sectionID, &versionID, &publicID, &versionPublicID, &versionLabel, &templatePublicID, &templateCode,
			&templateBuiltin, &templateI18nKey, &parentPublicID, &defaultLocale, &displayMode, &ordinal, &status, &revisionID, &localizations)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "content section not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read content section")
		return
	}

	var total int
	err = s.db.QueryRow(r.Context(), `select count(*)::int from mod_content_section_resources section_resource
		join game_resources resource on resource.entity_id=section_resource.resource_id
		where section_resource.section_id=$1 and section_resource.version_id=$2 and ($3='' or resource.canonical_id ilike '%'||$3||'%'
		 or exists(select 1 from mod_resource_version_detail_localizations localization
		  where localization.resource_id=section_resource.resource_id and localization.version_id=$2 and localization.name ilike '%'||$3||'%')
		 or exists(select 1 from resource_import_snapshots snapshot join catalog_import_revisions revision on revision.id=snapshot.revision_id
		  where snapshot.resource_id=section_resource.resource_id and revision.target_version_public_id=$4 and revision.is_active
		   and snapshot.names::text ilike '%'||$3||'%'))`, sectionID, versionID, query, versionPublicID).Scan(&total)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count content section resources")
		return
	}

	rows, err := s.db.Query(r.Context(), `select entity.public_id,resource.kind_code,resource.canonical_id,section_resource.ordinal,
		coalesce(imported.revision_id,''),coalesce(imported.icon_path,''),coalesce(detail.icon_file_id,0),
		coalesce(nullif(version_names.names,'{}'::jsonb),nullif((select jsonb_object_agg(name.key,name.value)
		 from jsonb_each_text(coalesce(imported.names,'{}'::jsonb)) name
		 where replace(lower(name.key),'_','-')=any($4::text[])),'{}'::jsonb),'{}'::jsonb),
		case when resource.kind_code='minecraft.advancement' then jsonb_strip_nulls(jsonb_build_object(
		 'parent',effective.data->'parent','display',jsonb_strip_nulls(jsonb_build_object(
		  'x',effective.data#>'{display,x}','y',effective.data#>'{display,y}','frame',effective.data#>'{display,frame}')))) else '{}'::jsonb end
		from mod_content_section_resources section_resource
		join catalog_entities entity on entity.id=section_resource.resource_id
		join game_resources resource on resource.entity_id=section_resource.resource_id
		left join mod_resource_version_details detail on detail.resource_id=section_resource.resource_id and detail.version_id=$2 and detail.status='active'
		left join lateral (select snapshot.revision_id,snapshot.icon_path,snapshot.names,snapshot.data from resource_import_snapshots snapshot
		 join catalog_import_revisions revision on revision.id=snapshot.revision_id
		 where snapshot.resource_id=section_resource.resource_id and revision.target_version_public_id=$3 and revision.is_active
		 order by (snapshot.icon_path<>'') desc,revision.created_at desc limit 1) imported on true
		left join lateral (select case when detail.definition is not null and detail.definition<>'{}'::jsonb
		 then detail.definition else coalesce(imported.data,'{}'::jsonb) end data) effective on true
		left join lateral (select jsonb_object_agg(localization.locale,localization.name) names
		 from mod_resource_version_detail_localizations localization
		 where localization.resource_id=section_resource.resource_id and localization.version_id=$2
		  and coalesce(localization.name,'')<>'' and replace(lower(localization.locale),'_','-')=any($4::text[])) version_names on true
		where section_resource.section_id=$1 and section_resource.version_id=$2 and ($5='' or resource.canonical_id ilike '%'||$5||'%'
		 or exists(select 1 from mod_resource_version_detail_localizations localization
		  where localization.resource_id=section_resource.resource_id and localization.version_id=$2 and localization.name ilike '%'||$5||'%')
		 or coalesce(imported.names,'{}'::jsonb)::text ilike '%'||$5||'%')
		order by section_resource.ordinal,resource.canonical_id limit $6 offset $7`, sectionID, versionID, versionPublicID, localeCandidates, query, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read content section resources")
		return
	}
	defer rows.Close()
	resources := make([]map[string]any, 0, min(limit, total))
	for rows.Next() {
		var resourcePublicID, kindCode, canonicalID, sourceRevisionID, iconPath string
		var resourceOrdinal int
		var iconFileID int64
		var names, definition []byte
		if err = rows.Scan(&resourcePublicID, &kindCode, &canonicalID, &resourceOrdinal, &sourceRevisionID, &iconPath, &iconFileID, &names, &definition); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode content section resources")
			return
		}
		resources = append(resources, map[string]any{"versionPublicId": versionPublicID, "resourcePublicId": resourcePublicID,
			"kindCode": kindCode, "canonicalId": canonicalID, "ordinal": resourceOrdinal, "revisionId": sourceRevisionID, "iconPath": iconPath,
			"iconFileId": iconFileID, "names": json.RawMessage(names), "definition": json.RawMessage(definition)})
	}
	section := map[string]any{"publicId": publicID, "versionPublicId": versionPublicID, "templatePublicId": templatePublicID,
		"templateCode": templateCode, "templateBuiltin": templateBuiltin, "templateI18nKey": templateI18nKey,
		"parentPublicId": parentPublicID, "defaultLocale": defaultLocale, "displayMode": displayMode, "ordinal": ordinal,
		"status": status, "publishedRevisionId": revisionID, "localizations": json.RawMessage(localizations),
		"resourceCount": total}
	writeJSON(w, http.StatusOK, map[string]any{"section": section, "versionLabel": versionLabel, "items": resources, "total": total, "limit": limit, "offset": offset})
}

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

func (s *Server) modContentSection(w http.ResponseWriter, r *http.Request) {
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
	if r.Method == http.MethodDelete {
		s.submitExistingModContentMutation(w, r, identity, modContentSnapshot{Kind: "section", Operation: "delete", ModID: identity.ID, ModSiteID: identity.SiteID, PublicID: publicID}, publishedRevisionID)
		return
	}
	var edit modContentSectionEdit
	if decodeJSON(r, &edit) != nil || normalizeModContentSectionEdit(&edit) != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid content section")
		return
	}
	if s.enforceModContentSectionDisplayMode(r.Context(), identity.ID, &edit) != nil {
		writeError(w, http.StatusUnprocessableEntity, "content template not found")
		return
	}
	if edit.ParentPublicID == publicID {
		writeError(w, http.StatusUnprocessableEntity, "content section cannot be its own parent")
		return
	}
	if !sameRevision(edit.BaseRevisionID, publishedRevisionID) {
		writeError(w, http.StatusConflict, "content section changed; reload the editor")
		return
	}
	s.submitExistingModContentMutation(w, r, identity, modContentSnapshot{Kind: "section", Operation: "edit", ModID: identity.ID, ModSiteID: identity.SiteID, PublicID: publicID, Section: &edit}, publishedRevisionID)
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
		coalesce((select jsonb_agg(jsonb_build_object('versionPublicId',version.public_id,'defaultLocale',detail.default_locale,
		'definition',detail.definition,'status',detail.status,'publishedRevisionId',detail.published_revision_id,
		'localizations',coalesce((select jsonb_agg(jsonb_build_object('locale',localization.locale,'name',localization.name,
		'summary',localization.summary,'contentMarkdown',localization.content_markdown,'provenance',localization.provenance)
		order by localization.locale) from mod_resource_version_detail_localizations localization
		where localization.resource_id=resource.entity_id and localization.version_id=version.id),'[]'::jsonb)) order by version.updated_at desc)
		from mod_resource_version_details detail join mod_content_versions version on version.id=detail.version_id
		where detail.resource_id=resource.entity_id and detail.status='active'),'[]'::jsonb)
		from mod_resource_bindings binding join game_resources resource on resource.entity_id=binding.resource_id
		join catalog_entities entity on entity.id=resource.entity_id where binding.mod_id=$1
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
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) submitNewModContentResource(w http.ResponseWriter, r *http.Request, identity modIdentityRecord, edit modContentResourceEdit) {
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start resource detail revision")
		return
	}
	defer tx.Rollback(r.Context())
	resourceID, publicID := "", edit.ResourcePublicID
	createdIdentity := false
	var validKind bool
	if err = tx.QueryRow(r.Context(), `select exists(select 1 from resource_kinds where code=$1 and user_visible)`, edit.KindCode).Scan(&validKind); err != nil || !validKind {
		writeError(w, http.StatusUnprocessableEntity, "resource kind is unavailable")
		return
	}
	if publicID != "" {
		err = tx.QueryRow(r.Context(), `select resource.entity_id from catalog_entities entity join game_resources resource on resource.entity_id=entity.id
			where entity.public_id=$1 and entity.status='active' and (resource.owner_mod_id=$2 or resource.owner_mod_id is null)`, publicID, identity.ID).Scan(&resourceID)
	} else {
		resolver, resolveErr := loadCatalogResourceIdentityResolver(r.Context(), s.db)
		if resolveErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve resource identity")
			return
		}
		resolved := resolver.resolve(edit.KindCode, edit.CanonicalID)
		resourceID, publicID = resolved.ID, resolved.PublicID
		inserted, insertErr := tx.Exec(r.Context(), `insert into catalog_entities(id,public_id,entity_type,status) values($1,$2,'resource','placeholder') on conflict(id) do nothing`, resourceID, publicID)
		err = insertErr
		if err == nil {
			createdIdentity = inserted.RowsAffected() == 1
			_, err = tx.Exec(r.Context(), `insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,owner_mod_id,resolved)
				values($1,$2,$3,$4,$5,$6,true) on conflict(entity_id) do nothing`, resourceID, edit.KindCode, resolved.CanonicalID, resolved.Namespace, resolved.ResourcePath, identity.ID)
		}
		if err == nil {
			var entityStatus string
			err = tx.QueryRow(r.Context(), `select entity.public_id,entity.status from catalog_entities entity join game_resources resource on resource.entity_id=entity.id
				where entity.id=$1 and entity.entity_type='resource' and resource.kind_code=$2 and resource.canonical_id=$3
				and (resource.owner_mod_id=$4 or resource.owner_mod_id is null)`, resourceID, edit.KindCode, resolved.CanonicalID, identity.ID).Scan(&publicID, &entityStatus)
			createdIdentity = createdIdentity || entityStatus == "placeholder"
		}
	}
	if err != nil || resourceID == "" {
		writeError(w, http.StatusUnprocessableEntity, "resource identity is unavailable for this mod")
		return
	}
	var versionID int64
	if err = tx.QueryRow(r.Context(), `select id from mod_content_versions where mod_id=$1 and status='active' and public_id=$2`, identity.ID, edit.VersionPublicID).Scan(&versionID); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "mod content version is invalid")
		return
	}
	if _, err = tx.Exec(r.Context(), `insert into mod_resource_bindings(resource_id,mod_id) values($1,$2) on conflict(resource_id) do nothing`, resourceID, identity.ID); err != nil {
		writeError(w, http.StatusConflict, "resource identity cannot be bound to this mod")
		return
	}
	var boundModID int64
	if err = tx.QueryRow(r.Context(), `select mod_id from mod_resource_bindings where resource_id=$1`, resourceID).Scan(&boundModID); err != nil || boundModID != identity.ID {
		writeError(w, http.StatusConflict, "resource identity belongs to another mod")
		return
	}
	definition, _ := json.Marshal(edit.Definition)
	if _, err = tx.Exec(r.Context(), `insert into mod_resource_version_details(resource_id,version_id,default_locale,definition,status,created_by,updated_by)
		values($1,$2,$3,$4::jsonb,'pending',$5,$5)`, resourceID, versionID, edit.DefaultLocale, string(definition), currentClaims(r).Subject); err != nil {
		writeError(w, http.StatusConflict, "this resource already has detail content for the selected version")
		return
	}
	edit.ResourcePublicID = publicID
	snapshot := modContentSnapshot{Kind: "resource", Operation: "create", ModID: identity.ID, ModSiteID: identity.SiteID, PublicID: publicID, Resource: &edit, CreatedIdentity: createdIdentity}
	result, err := s.createModContentRevisionTx(r, tx, identity, snapshot, nil)
	if err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, http.StatusInternalServerError, "failed to submit resource detail")
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) modContentResource(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requireEditableMod(w, r)
	if !ok {
		return
	}
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("resourceId")))
	if r.Method == http.MethodGet {
		var entityID, kindCode, canonicalID string
		var details []byte
		if err := s.db.QueryRow(r.Context(), `select resource.entity_id,resource.kind_code,resource.canonical_id,
			coalesce((select jsonb_agg(jsonb_build_object('versionPublicId',version.public_id,'defaultLocale',detail.default_locale,
			'definition',detail.definition,'status',detail.status,'publishedRevisionId',detail.published_revision_id,
			'localizations',coalesce((select jsonb_agg(jsonb_build_object('locale',localization.locale,'name',localization.name,
			'summary',localization.summary,'contentMarkdown',localization.content_markdown,'provenance',localization.provenance)
			order by localization.locale) from mod_resource_version_detail_localizations localization
			where localization.resource_id=resource.entity_id and localization.version_id=version.id),'[]'::jsonb)) order by version.updated_at desc)
			from mod_resource_version_details detail join mod_content_versions version on version.id=detail.version_id
			where detail.resource_id=resource.entity_id and detail.status='active'),'[]'::jsonb)
			from mod_resource_bindings binding join game_resources resource on resource.entity_id=binding.resource_id
			join catalog_entities entity on entity.id=resource.entity_id where entity.public_id=$1 and binding.mod_id=$2`, publicID, identity.ID).
			Scan(&entityID, &kindCode, &canonicalID, &details); err != nil {
			writeError(w, http.StatusNotFound, "mod resource detail not found")
			return
		}
		primary, secondary := s.requestContentLocales(r)
		carrier := map[string]any{"entityId": entityID, "versions": []map[string]any{}}
		if err := s.decorateResourceVersionRows(r.Context(), []map[string]any{carrier}, primary, secondary); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve resource versions")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"entityId": entityID, "publicId": publicID, "kindCode": kindCode,
			"canonicalId": canonicalID, "details": json.RawMessage(details), "versions": carrier["versions"]})
		return
	}
	if r.Method == http.MethodDelete {
		versionPublicID := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("version")))
		var publishedRevisionID *int64
		if err := s.db.QueryRow(r.Context(), `select detail.published_revision_id from mod_resource_version_details detail
			join mod_content_versions version on version.id=detail.version_id join catalog_entities entity on entity.id=detail.resource_id
			where entity.public_id=$1 and version.public_id=$2 and version.mod_id=$3 and detail.status='active'`, publicID, versionPublicID, identity.ID).Scan(&publishedRevisionID); err != nil {
			writeError(w, http.StatusNotFound, "mod resource version detail not found")
			return
		}
		edit := modContentResourceEdit{ResourcePublicID: publicID, VersionPublicID: versionPublicID}
		s.submitExistingModContentMutation(w, r, identity, modContentSnapshot{Kind: "resource", Operation: "delete", ModID: identity.ID, ModSiteID: identity.SiteID, PublicID: publicID, Resource: &edit}, publishedRevisionID)
		return
	}
	var edit modContentResourceEdit
	if decodeJSON(r, &edit) != nil || normalizeModContentResourceEdit(&edit) != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid or stale mod resource detail")
		return
	}
	var publishedRevisionID *int64
	if err := s.db.QueryRow(r.Context(), `select detail.published_revision_id from mod_resource_version_details detail
		join mod_content_versions version on version.id=detail.version_id join catalog_entities entity on entity.id=detail.resource_id
		where entity.public_id=$1 and version.public_id=$2 and version.mod_id=$3 and detail.status='active'`, publicID, edit.VersionPublicID, identity.ID).Scan(&publishedRevisionID); err != nil {
		writeError(w, http.StatusNotFound, "mod resource version detail not found")
		return
	}
	if !sameRevision(edit.BaseRevisionID, publishedRevisionID) {
		writeError(w, http.StatusConflict, "mod resource version detail changed; reload the editor")
		return
	}
	edit.ResourcePublicID = publicID
	s.submitExistingModContentMutation(w, r, identity, modContentSnapshot{Kind: "resource", Operation: "edit", ModID: identity.ID, ModSiteID: identity.SiteID, PublicID: publicID, Resource: &edit}, publishedRevisionID)
}

func publishModContentSnapshotTx(ctx context.Context, tx pgx.Tx, revisionID int64, snapshot modContentSnapshot, actorID int64) error {
	if snapshot.Operation == "delete" {
		table := "mod_content_versions"
		if snapshot.Kind == "template" {
			table = "mod_content_templates"
		} else if snapshot.Kind == "section" {
			table = "mod_content_sections"
		} else if snapshot.Kind == "resource" {
			if snapshot.Resource == nil {
				return errCatalogEditorInvalid
			}
			_, err := tx.Exec(ctx, `update mod_resource_version_details detail set status='archived',published_revision_id=$3,updated_at=now()
				from catalog_entities entity,mod_content_versions version where entity.public_id=$1 and detail.resource_id=entity.id
				and version.public_id=$2 and detail.version_id=version.id and version.mod_id=$4`, snapshot.PublicID, snapshot.Resource.VersionPublicID, revisionID, snapshot.ModID)
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
		var templateID int64
		if err := tx.QueryRow(ctx, `update mod_content_templates set code=$2,default_locale=$3,default_display_mode=$4,definition=$5::jsonb,status='active',published_revision_id=$6,updated_at=now()
			where public_id=$1 and owner_mod_id=$7 and not builtin returning id`, snapshot.PublicID, snapshot.Template.Code, snapshot.Template.DefaultLocale, snapshot.Template.DefaultDisplayMode, string(definition), revisionID, snapshot.ModID).Scan(&templateID); err != nil {
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
		if snapshot.Section == nil {
			return errCatalogEditorInvalid
		}
		var versionID int64
		if err := tx.QueryRow(ctx, `select id from mod_content_versions where public_id=$1 and mod_id=$2 and status='active'`, snapshot.Section.VersionPublicID, snapshot.ModID).Scan(&versionID); err != nil {
			return err
		}
		var templateID int64
		if err := tx.QueryRow(ctx, `select id from mod_content_templates where public_id=$1 and status='active' and (builtin or owner_mod_id=$2)`, snapshot.Section.TemplatePublicID, snapshot.ModID).Scan(&templateID); err != nil {
			return err
		}
		var parentID *int64
		if snapshot.Section.ParentPublicID != "" {
			var value int64
			if err := tx.QueryRow(ctx, `select id from mod_content_sections where public_id=$1 and mod_id=$2 and version_id=$3 and status='active'`, snapshot.Section.ParentPublicID, snapshot.ModID, versionID).Scan(&value); err != nil {
				return err
			}
			parentID = &value
		}
		var sectionID int64
		if err := tx.QueryRow(ctx, `update mod_content_sections set version_id=$2,template_id=$3,parent_id=$4,default_locale=$5,display_mode=$6,ordinal=$7,status='active',
			published_revision_id=$8,updated_by=$9,updated_at=now() where public_id=$1 and mod_id=$10 returning id`, snapshot.PublicID,
			versionID, templateID, parentID, snapshot.Section.DefaultLocale, snapshot.Section.DisplayMode, snapshot.Section.Ordinal, revisionID, actorID, snapshot.ModID).Scan(&sectionID); err != nil {
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
		if _, err := tx.Exec(ctx, `delete from mod_content_section_resources where section_id=$1`, sectionID); err != nil {
			return err
		}
		for _, resource := range snapshot.Section.Resources {
			var versionID int64
			var resourceID string
			if err := tx.QueryRow(ctx, `select id from mod_content_versions where public_id=$1 and mod_id=$2 and status='active'`, resource.VersionPublicID, snapshot.ModID).Scan(&versionID); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `select resource.entity_id from catalog_entities entity join game_resources resource on resource.entity_id=entity.id
				where entity.public_id=$1 and entity.status='active' and resource.owner_mod_id=$2`, resource.ResourcePublicID, snapshot.ModID).Scan(&resourceID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `insert into mod_content_section_resources(section_id,version_id,resource_id,ordinal) values($1,$2,$3,$4)`, sectionID, versionID, resourceID, resource.Ordinal); err != nil {
				return err
			}
		}
		return nil
	case "resource":
		if snapshot.Resource == nil {
			return errCatalogEditorInvalid
		}
		definition, _ := json.Marshal(snapshot.Resource.Definition)
		var resourceID string
		var versionID int64
		if err := tx.QueryRow(ctx, `select id from mod_content_versions where public_id=$1 and mod_id=$2 and status='active'`, snapshot.Resource.VersionPublicID, snapshot.ModID).Scan(&versionID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `update mod_resource_version_details detail set default_locale=$3,definition=$4::jsonb,status='active',published_revision_id=$5,
			updated_by=$6,updated_at=now() from catalog_entities entity where entity.public_id=$1 and detail.resource_id=entity.id
			and detail.version_id=$2 returning detail.resource_id`, snapshot.PublicID, versionID, snapshot.Resource.DefaultLocale, string(definition), revisionID, actorID).Scan(&resourceID); err != nil {
			return err
		}
		if snapshot.CreatedIdentity {
			if _, err := tx.Exec(ctx, `update catalog_entities set status='active',updated_at=now() where id=$1 and status='placeholder'`, resourceID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `delete from mod_resource_version_detail_localizations where resource_id=$1 and version_id=$2`, resourceID, versionID); err != nil {
			return err
		}
		for _, localization := range snapshot.Resource.Localizations {
			if _, err := tx.Exec(ctx, `insert into mod_resource_version_detail_localizations(resource_id,version_id,locale,name,summary,content_markdown,provenance)
				values($1,$2,$3,$4,$5,$6,'human')`, resourceID, versionID, localization.Locale, localization.Name, localization.Summary, localization.ContentMarkdown); err != nil {
				return err
			}
		}
		return nil
	default:
		return errCatalogEditorInvalid
	}
}

func publishedModContentRevisionTx(ctx context.Context, tx pgx.Tx, snapshot modContentSnapshot) (*int64, error) {
	table := "mod_content_versions"
	if snapshot.Kind == "template" {
		table = "mod_content_templates"
	} else if snapshot.Kind == "section" {
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

func sortModContentVersions(items []map[string]any) {
	sort.SliceStable(items, func(i, j int) bool {
		return strings.Compare(strings.ToLower(items[i]["label"].(string)), strings.ToLower(items[j]["label"].(string))) > 0
	})
}
