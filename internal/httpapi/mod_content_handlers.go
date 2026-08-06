package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
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
	BaseRevisionID    *string  `json:"baseRevisionId,omitempty"`
}

type modContentTemplateEdit struct {
	Code               string                    `json:"code"`
	DefaultLocale      string                    `json:"defaultLocale"`
	DefaultDisplayMode string                    `json:"defaultDisplayMode"`
	Definition         map[string]any            `json:"definition"`
	Localizations      []catalogLocalizationEdit `json:"localizations"`
	Reason             string                    `json:"reason"`
	BaseRevisionID     *string                   `json:"baseRevisionId,omitempty"`
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
	BaseRevisionID   *string                         `json:"baseRevisionId,omitempty"`
}

type modContentResourceEdit struct {
	ResourcePublicID      string                    `json:"resourcePublicId"`
	KindCode              string                    `json:"kindCode"`
	CanonicalID           string                    `json:"canonicalId"`
	VersionPublicID       string                    `json:"versionPublicId"`
	SectionPublicID       *string                   `json:"sectionPublicId,omitempty"`
	EntryTypeCode         string                    `json:"entryTypeCode"`
	DefaultLocale         string                    `json:"defaultLocale"`
	Definition            map[string]any            `json:"definition"`
	IconSmallFilePublicID *string                   `json:"iconSmallFilePublicId,omitempty"`
	IconFilePublicID      *string                   `json:"iconFilePublicId,omitempty"`
	RenderFilePublicID    *string                   `json:"renderFilePublicId,omitempty"`
	Localizations         []catalogLocalizationEdit `json:"localizations"`
	Reason                string                    `json:"reason"`
	BaseRevisionID        *string                   `json:"baseRevisionId,omitempty"`
}

type modContentResourceLocalizationState struct {
	Name            string
	Summary         string
	ContentMarkdown string
	Provenance      string
}

type modContentSnapshot struct {
	Kind            string                  `json:"kind"`
	Operation       string                  `json:"operation"`
	ModID           int64                   `json:"-"`
	ModPublicID     string                  `json:"modId"`
	ModSiteID       string                  `json:"modSiteId"`
	PublicID        string                  `json:"publicId"`
	Version         *modContentVersionEdit  `json:"version,omitempty"`
	Template        *modContentTemplateEdit `json:"template,omitempty"`
	Section         *modContentSectionEdit  `json:"section,omitempty"`
	Layout          *modContentLayoutEdit   `json:"layout,omitempty"`
	Resource        *modContentResourceEdit `json:"resource,omitempty"`
	CreatedIdentity bool                    `json:"createdIdentity,omitempty"`
}

type modContentMutationResult struct {
	PublicID        string `json:"publicId"`
	RevisionID      string `json:"revisionId"`
	ChangeRequestID string `json:"changeRequestId"`
	ReviewStatus    string `json:"reviewStatus"`
	ActivityEventID string `json:"activityEventId"`
}

func modContentAggregate(aggregateType string) bool {
	return aggregateType == modContentAggregateVersion || aggregateType == modContentAggregateTemplate || aggregateType == modContentAggregateSection || aggregateType == modContentAggregateResource
}

func normalizeModContentResourceEdit(edit *modContentResourceEdit) error {
	return normalizeModContentResourceEditWithPolicy(edit, false)
}

func normalizeModContentResourceUpdateEdit(edit *modContentResourceEdit) error {
	return normalizeModContentResourceEditWithPolicy(edit, true)
}

func normalizeModContentResourceEditWithPolicy(edit *modContentResourceEdit, allowImmutableNonEditableLocales bool) error {
	edit.ResourcePublicID = strings.ToLower(strings.TrimSpace(edit.ResourcePublicID))
	edit.KindCode = strings.ToLower(strings.TrimSpace(edit.KindCode))
	edit.CanonicalID = strings.ToLower(strings.TrimSpace(edit.CanonicalID))
	edit.VersionPublicID = strings.ToLower(strings.TrimSpace(edit.VersionPublicID))
	edit.EntryTypeCode = strings.ToLower(strings.TrimSpace(edit.EntryTypeCode))
	if edit.EntryTypeCode == "" {
		edit.EntryTypeCode = "default"
	}
	if edit.SectionPublicID != nil {
		value := strings.ToLower(strings.TrimSpace(*edit.SectionPublicID))
		edit.SectionPublicID = &value
	}
	for _, filePublicID := range []*string{edit.IconSmallFilePublicID, edit.IconFilePublicID, edit.RenderFilePublicID} {
		if filePublicID == nil {
			continue
		}
		*filePublicID = strings.ToLower(strings.TrimSpace(*filePublicID))
		if *filePublicID != "" && !validCatalogPublicID(*filePublicID) {
			return errCatalogEditorInvalid
		}
	}
	edit.Reason = strings.TrimSpace(edit.Reason)
	if len(edit.Reason) > 500 || edit.VersionPublicID == "" || !modContentTemplateCodePattern.MatchString(edit.EntryTypeCode) ||
		(edit.ResourcePublicID == "" && (edit.KindCode == "" || edit.CanonicalID == "")) {
		return errCatalogEditorInvalid
	}
	if edit.Definition == nil {
		edit.Definition = map[string]any{}
	}
	if encoded, err := json.Marshal(edit.Definition); err != nil || len(encoded) > 512*1024 {
		return errCatalogEditorInvalid
	}
	defaultLocale, localizations, err := normalizeModContentResourceLocalizations(edit.DefaultLocale, edit.Localizations, allowImmutableNonEditableLocales)
	if err != nil || requireCatalogCreateDefaultLocalization(defaultLocale, localizations) != nil {
		return errCatalogEditorInvalid
	}
	edit.DefaultLocale, edit.Localizations = defaultLocale, localizations
	return nil
}

func normalizeModContentResourceLocalizations(defaultLocale string, localizations []catalogLocalizationEdit, allowNonEditable bool) (string, []catalogLocalizationEdit, error) {
	if !allowNonEditable {
		return normalizeCatalogLocalizations(defaultLocale, localizations)
	}
	seen := make(map[string]struct{}, len(localizations))
	for index := range localizations {
		locale, err := normalizeCatalogLocale(localizations[index].Locale)
		if err != nil {
			return "", nil, fmt.Errorf("%w: invalid locale", errCatalogEditorInvalid)
		}
		if _, exists := seen[locale]; exists {
			return "", nil, fmt.Errorf("%w: duplicate locale", errCatalogEditorInvalid)
		}
		seen[locale] = struct{}{}
		localizations[index].Locale = locale
		if isEditableContentLocale(locale) {
			localizations[index].Name = strings.TrimSpace(localizations[index].Name)
			localizations[index].Summary = strings.TrimSpace(localizations[index].Summary)
			localizations[index].Provenance = "human"
			localizations[index].SourceLocale = ""
			editable := true
			localizations[index].Editable = &editable
			localizations[index].ReviewStatus = "approved"
		}
		if len(localizations[index].Name) > 512 || len(localizations[index].Summary) > 4096 || len(localizations[index].ContentMarkdown) > maxModExportEntryMarkdownBytes {
			return "", nil, fmt.Errorf("%w: localized content is too large", errCatalogEditorInvalid)
		}
	}
	if strings.TrimSpace(defaultLocale) == "" {
		defaultLocale = "en-US"
	}
	normalizedDefault, err := normalizeCatalogLocale(defaultLocale)
	if err != nil {
		return "", nil, fmt.Errorf("%w: invalid default locale", errCatalogEditorInvalid)
	}
	return normalizedDefault, localizations, nil
}

func preserveImmutableModContentResourceLocales(existingDefaultLocale string, existing map[string]modContentResourceLocalizationState, edit *modContentResourceEdit) error {
	existingDefaultLocale = normalizeContentLocale(existingDefaultLocale)
	if !isEditableContentLocale(existingDefaultLocale) && edit.DefaultLocale != existingDefaultLocale {
		return fmt.Errorf("%w: non-editable default locale cannot be changed", errCatalogEditorInvalid)
	}
	submitted := make(map[string]int, len(edit.Localizations))
	for index := range edit.Localizations {
		localization := &edit.Localizations[index]
		submitted[localization.Locale] = index
		if isEditableContentLocale(localization.Locale) {
			continue
		}
		current, exists := existing[localization.Locale]
		if !exists ||
			current.Name != localization.Name ||
			current.Summary != localization.Summary ||
			current.ContentMarkdown != localization.ContentMarkdown {
			return fmt.Errorf("%w: non-editable localization cannot be added or changed", errCatalogEditorInvalid)
		}
		localization.Provenance = current.Provenance
		localization.SourceLocale = ""
		editable := false
		localization.Editable = &editable
		localization.ReviewStatus = "approved"
	}
	for locale := range existing {
		if isEditableContentLocale(locale) {
			continue
		}
		if _, exists := submitted[locale]; !exists {
			return fmt.Errorf("%w: non-editable localization cannot be removed", errCatalogEditorInvalid)
		}
	}
	if !isEditableContentLocale(edit.DefaultLocale) && edit.DefaultLocale != existingDefaultLocale {
		return fmt.Errorf("%w: non-editable default locale cannot be selected", errCatalogEditorInvalid)
	}
	return nil
}

type modContentResourceStateQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func loadModContentResourceLocalizationState(ctx context.Context, query modContentResourceStateQuerier, resourceID, versionID int64, lock bool) (string, map[string]modContentResourceLocalizationState, error) {
	detailQuery := `select default_locale from mod_resource_version_details where resource_id=$1 and version_id=$2`
	localizationQuery := `select locale,name,summary,content_markdown,provenance
		from mod_resource_version_detail_localizations where resource_id=$1 and version_id=$2`
	if lock {
		detailQuery += ` for update`
		localizationQuery += ` for update`
	}
	var defaultLocale string
	if err := query.QueryRow(ctx, detailQuery, resourceID, versionID).Scan(&defaultLocale); err != nil {
		return "", nil, err
	}
	rows, err := query.Query(ctx, localizationQuery, resourceID, versionID)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	localizations := make(map[string]modContentResourceLocalizationState)
	for rows.Next() {
		var locale string
		var localization modContentResourceLocalizationState
		if err = rows.Scan(&locale, &localization.Name, &localization.Summary, &localization.ContentMarkdown, &localization.Provenance); err != nil {
			return "", nil, err
		}
		localizations[normalizeContentLocale(locale)] = localization
	}
	if err = rows.Err(); err != nil {
		return "", nil, err
	}
	return normalizeContentLocale(defaultLocale), localizations, nil
}

func reserveModContentResourceDetailWithSubtypeTx(ctx context.Context, tx pgx.Tx, resourceID, versionID, modID int64, entryTypeCode, defaultLocale string, definition []byte, iconSmallFileID, iconFileID, renderFileID *int64, actorID int64) error {
	var reservedResourceID int64
	err := tx.QueryRow(ctx, `insert into mod_resource_version_details(resource_id,version_id,entry_type_code,default_locale,definition,icon_small_file_id,icon_file_id,render_file_id,status,created_by,updated_by)
		values($1,$2,$3,$4,$5::jsonb,$6,$7,$8,'pending',$9,$9)
		on conflict(resource_id,version_id) do update set
			default_locale=excluded.default_locale,
			entry_type_code=excluded.entry_type_code,
			definition=excluded.definition,
			icon_small_file_id=excluded.icon_small_file_id,
			icon_file_id=excluded.icon_file_id,
			render_file_id=excluded.render_file_id,
			status='pending',
			published_revision_id=null,
			updated_by=excluded.updated_by,
			updated_at=now()
		where mod_resource_version_details.status='archived'
		  and exists(select 1 from mod_resource_bindings binding
			where binding.resource_id=excluded.resource_id and binding.mod_id=$10)
		returning resource_id`, resourceID, versionID, entryTypeCode, defaultLocale, string(definition), iconSmallFileID, iconFileID, renderFileID, actorID, modID).Scan(&reservedResourceID)
	if err != nil {
		return err
	}
	if reservedResourceID != resourceID {
		return errCatalogEditorInvalid
	}
	_, err = tx.Exec(ctx, `delete from mod_resource_version_detail_localizations where resource_id=$1 and version_id=$2`, resourceID, versionID)
	return err
}

type modContentImageQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type modContentEntryTypeDefinition struct {
	Code      string                     `json:"code"`
	KindCodes []string                   `json:"kindCodes"`
	Names     map[string]string          `json:"names"`
	Groups    []modContentEntryTypeGroup `json:"groups"`
	Enabled   *bool                      `json:"enabled,omitempty"`
}

type modContentEntryTypeGroup struct {
	Code         string                     `json:"code"`
	Names        map[string]string          `json:"names"`
	Descriptions map[string]string          `json:"descriptions"`
	Fields       []modContentEntryTypeField `json:"fields"`
}

type modContentEntryTypeField struct {
	Code              string            `json:"code"`
	Type              string            `json:"type"`
	Format            string            `json:"format,omitempty"`
	Names             map[string]string `json:"names"`
	ReferenceKind     string            `json:"referenceKind"`
	ReferenceRegistry string            `json:"referenceRegistry"`
	Paths             [][]string        `json:"paths"`
	Editable          *bool             `json:"editable,omitempty"`
}

type modContentTemplateDefinition struct {
	ResourceKinds []string                        `json:"resourceKinds"`
	EntryTypes    []modContentEntryTypeDefinition `json:"entryTypes"`
}

func validateModContentTemplateDefinition(definition map[string]any) error {
	encoded, err := json.Marshal(definition)
	if err != nil || len(encoded) > 512*1024 || catalogJSONDepth(definition, 0) > 20 {
		return errCatalogEditorInvalid
	}
	var schema modContentTemplateDefinition
	if err = json.Unmarshal(encoded, &schema); err != nil {
		return errCatalogEditorInvalid
	}
	if len(schema.ResourceKinds) == 0 || len(schema.ResourceKinds) > 64 {
		return errCatalogEditorInvalid
	}
	seenResourceKinds := make(map[string]struct{}, len(schema.ResourceKinds))
	for _, kindCode := range schema.ResourceKinds {
		kindCode = strings.ToLower(strings.TrimSpace(kindCode))
		if !validModContentReferenceKind(kindCode) {
			return errCatalogEditorInvalid
		}
		if _, exists := seenResourceKinds[kindCode]; exists {
			return errCatalogEditorInvalid
		}
		seenResourceKinds[kindCode] = struct{}{}
	}
	seenTypes := make(map[string]struct{}, len(schema.EntryTypes))
	for _, entryType := range schema.EntryTypes {
		code := strings.ToLower(strings.TrimSpace(entryType.Code))
		if !modContentTemplateCodePattern.MatchString(code) {
			return errCatalogEditorInvalid
		}
		if _, exists := seenTypes[code]; exists {
			return errCatalogEditorInvalid
		}
		seenTypes[code] = struct{}{}
		if len(entryType.Names) == 0 || !validModContentSchemaNames(entryType.Names) || len(entryType.KindCodes) > 64 {
			return errCatalogEditorInvalid
		}
		seenKinds := make(map[string]struct{}, len(entryType.KindCodes))
		for _, kindCode := range entryType.KindCodes {
			kindCode = strings.ToLower(strings.TrimSpace(kindCode))
			if !validModContentReferenceKind(kindCode) {
				return errCatalogEditorInvalid
			}
			if _, exists := seenKinds[kindCode]; exists {
				return errCatalogEditorInvalid
			}
			seenKinds[kindCode] = struct{}{}
		}
		seenGroups := make(map[string]struct{}, len(entryType.Groups))
		seenFields := make(map[string]struct{})
		for _, group := range entryType.Groups {
			groupCode := strings.ToLower(strings.TrimSpace(group.Code))
			if !modContentTemplateCodePattern.MatchString(groupCode) {
				return errCatalogEditorInvalid
			}
			if _, exists := seenGroups[groupCode]; exists {
				return errCatalogEditorInvalid
			}
			seenGroups[groupCode] = struct{}{}
			if len(group.Names) == 0 || !validModContentSchemaNames(group.Names) || !validModContentSchemaNames(group.Descriptions) {
				return errCatalogEditorInvalid
			}
			for _, field := range group.Fields {
				fieldCode := strings.TrimSpace(field.Code)
				if fieldCode == "" || len(fieldCode) > 64 || !catalogStringIn(field.Type, "number", "range", "text", "boolean", "list", "reference", "reference-list", "json") ||
					len(field.Paths) == 0 || len(field.Paths) > 8 {
					return errCatalogEditorInvalid
				}
				if len(field.Names) == 0 || !validModContentSchemaNames(field.Names) || !validModContentFieldFormat(field) {
					return errCatalogEditorInvalid
				}
				if _, exists := seenFields[fieldCode]; exists {
					return errCatalogEditorInvalid
				}
				seenFields[fieldCode] = struct{}{}
				if (field.Type == "reference" || field.Type == "reference-list") && !validModContentReferenceKind(field.ReferenceKind) {
					return errCatalogEditorInvalid
				}
				if field.ReferenceRegistry != "" && (field.ReferenceKind != "tag" || len(field.ReferenceRegistry) > 128) {
					return errCatalogEditorInvalid
				}
				for _, path := range field.Paths {
					if len(path) == 0 || len(path) > 8 {
						return errCatalogEditorInvalid
					}
					for _, segment := range path {
						if strings.TrimSpace(segment) == "" || len(segment) > 80 {
							return errCatalogEditorInvalid
						}
					}
				}
			}
		}
	}
	return nil
}

func validModContentSchemaNames(names map[string]string) bool {
	if len(names) > 32 {
		return false
	}
	for locale, value := range names {
		if strings.TrimSpace(locale) == "" || len(locale) > 32 || len([]rune(strings.TrimSpace(value))) > 160 {
			return false
		}
	}
	return true
}

func validModContentFieldFormat(field modContentEntryTypeField) bool {
	format := strings.ToLower(strings.TrimSpace(field.Format))
	if format == "" {
		return true
	}
	switch field.Type {
	case "number":
		return catalogStringIn(format, "integer", "float", "health", "armor")
	case "range":
		return format == "range"
	case "text":
		return format == "text"
	case "boolean":
		return format == "boolean"
	case "list":
		return format == "text-list"
	case "reference", "reference-list":
		return catalogStringIn(format, "resource", "entity", "tag", "enchantment", "item")
	case "json":
		return format == "json"
	default:
		return false
	}
}

func validModContentReferenceKind(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "tag" || value == "enchantment" {
		return true
	}
	if len(value) < 3 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '.' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func validateModContentEntryType(ctx context.Context, query modContentImageQuerier, modID, versionID int64, kindCode string, sectionPublicID *string, entryTypeCode string, definition map[string]any) error {
	_, err := normalizeModContentEntryDefinition(ctx, query, modID, versionID, kindCode, sectionPublicID, entryTypeCode, definition, false)
	return err
}

func modContentDefinitionValue(definition map[string]any, paths [][]string) (any, bool) {
	for _, path := range paths {
		var current any = definition
		found := true
		for _, segment := range path {
			object, ok := current.(map[string]any)
			if !ok {
				found = false
				break
			}
			current, ok = object[segment]
			if !ok {
				found = false
				break
			}
		}
		if found {
			return current, true
		}
	}
	return nil, false
}

func resolveModContentImageFileID(ctx context.Context, query modContentImageQuerier, filePublicID *string, actorID, resourceID, versionID int64, trustedSnapshot bool, variant string) (*int64, error) {
	if filePublicID == nil || strings.TrimSpace(*filePublicID) == "" {
		return nil, nil
	}
	var fileID int64
	err := query.QueryRow(ctx, `select file.id
		from oss_files file
		where file.public_id=$1 and file.status='active'
		  and file.scan_status in ('clean','trusted_generated')
		  and ($2 or (
		    lower(split_part(file.content_type,';',1))='image/png'
		    and file.uploader_id=$3
		    and lower(file.source) like 'mod_resource:%:'||$6
		  ) or exists(
			select 1 from mod_resource_version_details detail
			where detail.resource_id=$4 and detail.version_id=$5
			  and (
			    ($6='icon_32' and detail.icon_small_file_id=file.id)
			    or ($6='icon_128' and detail.icon_file_id=file.id)
			    or ($6='render' and detail.render_file_id=file.id)
			  )
		  ))`,
		*filePublicID, trustedSnapshot, actorID, resourceID, versionID, variant).Scan(&fileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errCatalogEditorReference
	}
	if err != nil {
		return nil, err
	}
	return &fileID, nil
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
	if err := validateModContentTemplateDefinition(edit.Definition); err != nil {
		return err
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
	includePending := canEditMod(currentClaims(r), identity) || claimsAllow(currentClaims(r), "content.review")
	rows, err := s.db.Query(r.Context(), `select public_id,label,minecraft_versions,loaders,mod_version,status,
		(select revision.public_id from content_revisions revision where revision.id=mod_content_versions.published_revision_id),
		created_at,updated_at from mod_content_versions where mod_id=$1 and ($2 or status='active')
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
		var publishedRevisionID *string
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
	requestedBaseRevisionID, baseErr := resolveRevisionPublicID(r.Context(), s.db, edit.BaseRevisionID)
	if baseErr != nil || !sameRevision(requestedBaseRevisionID, publishedRevisionID) {
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
		log.Printf("submit mod content mutation failed: site=%s kind=%s operation=%s public_id=%s: %v",
			identity.SiteID, snapshot.Kind, snapshot.Operation, snapshot.PublicID, err)
		writeError(w, catalogEditorHTTPStatus(err), catalogEditorErrorMessage(err))
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		log.Printf("commit mod content mutation failed: site=%s kind=%s operation=%s public_id=%s: %v",
			identity.SiteID, snapshot.Kind, snapshot.Operation, snapshot.PublicID, err)
		writeError(w, http.StatusInternalServerError, "failed to commit mod content revision")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) createModContentRevisionTx(r *http.Request, tx pgx.Tx, identity modIdentityRecord, snapshot modContentSnapshot, baseRevisionID *int64) (modContentMutationResult, error) {
	snapshot.ModID = identity.ID
	snapshot.ModPublicID = identity.UniqueID
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return modContentMutationResult{}, err
	}
	claims := currentClaims(r)
	config := loadReviewConfig(r.Context(), s.db)
	reviewRequired := modContentReviewRequired(config, snapshot)
	status := "approved"
	if reviewRequired && !catalogMutationBypassesReview(claims) && !canSkipProjectReview(claims, identity) {
		status = "pending"
	}
	aggregateType := modContentAggregateVersion
	if snapshot.Kind == "template" {
		aggregateType = modContentAggregateTemplate
	} else if snapshot.Kind == "section" || snapshot.Kind == "layout" {
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
	} else if snapshot.Layout != nil && snapshot.Layout.Reason != "" {
		reason = snapshot.Layout.Reason
	} else if snapshot.Resource != nil && snapshot.Resource.Reason != "" {
		reason = snapshot.Resource.Reason
	}
	aggregateKey := modContentSnapshotAggregateKey(snapshot)
	entityType := aggregateType
	if snapshot.Kind == "resource" {
		entityType = "resource"
	}
	var entityID int64
	if err = tx.QueryRow(r.Context(), `select internal_id from public_routes where public_id=$1 and entity_type=$2`,
		snapshot.PublicID, entityType).Scan(&entityID); err != nil {
		return modContentMutationResult{}, fmt.Errorf("resolve mod content public identity: %w", err)
	}
	created, err := createContentRevisionTx(r.Context(), tx, createContentRevisionParams{
		EntityType: entityType, EntityID: entityID,
		AggregateType: aggregateType, AggregateKey: aggregateKey, BaseRevision: baseRevisionID, Snapshot: raw,
		Reason: reason, ActorID: claims.Subject, Source: "user", Status: status,
		Metadata: map[string]any{"kind": snapshot.Kind, "operation": snapshot.Operation, "modId": identity.UniqueID, "siteId": identity.SiteID, "publicId": snapshot.PublicID}, Request: r,
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
	return modContentMutationResult{PublicID: snapshot.PublicID, RevisionID: created.RevisionPublicID, ChangeRequestID: created.ChangeRequestPublicID, ReviewStatus: status, ActivityEventID: activityID}, nil
}

func modContentSnapshotAggregateKey(snapshot modContentSnapshot) string {
	aggregateKey := snapshot.PublicID
	if snapshot.Kind == "resource" && snapshot.Resource != nil {
		aggregateKey += ":" + snapshot.Resource.VersionPublicID
	}
	return aggregateKey
}

func modContentReviewRequired(config reviewConfig, snapshot modContentSnapshot) bool {
	if snapshot.Operation == "create" {
		if snapshot.Kind == "section" {
			if snapshot.Section != nil && snapshot.Section.ParentPublicID != "" {
				return config.CatalogCreate
			}
			return config.ModContentSectionCreate
		}
		return config.CatalogCreate
	}
	if snapshot.Operation == "delete" {
		return config.CatalogDelete
	}
	return config.CatalogEdit
}

func insertModContentActivityTx(ctx context.Context, tx pgx.Tx, actorID int64, snapshot modContentSnapshot, created createdContentRevision, reviewStatus string) (string, error) {
	actionID := activity.ActionEdit
	if snapshot.Operation == "create" {
		actionID = activity.ActionCreate
	} else if snapshot.Operation == "delete" {
		actionID = activity.ActionDelete
	}
	metadata, err := json.Marshal(map[string]any{"revisionId": created.RevisionPublicID, "changeRequestId": created.ChangeRequestPublicID,
		"kind": snapshot.Kind, "operation": snapshot.Operation, "reviewStatus": reviewStatus, "modPublicId": snapshot.ModPublicID})
	if err != nil {
		return "", err
	}
	var publicID string
	err = tx.QueryRow(ctx, `insert into user_activity_events(user_id,action_id,object_type_id,object_public_id,metadata,occurred_at)
		values($1,$2,$3,$4,$5::jsonb,$6) returning public_id`, actorID, actionID, activity.ObjectMod, snapshot.PublicID, string(metadata), time.Now().UTC()).Scan(&publicID)
	return publicID, err
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
	if r.Method != http.MethodGet {
		var pending bool
		if err = s.db.QueryRow(r.Context(), `select exists(select 1 from change_requests
			where entity_type='mod' and entity_id=$1 and status='pending')`, identity.ID).Scan(&pending); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to inspect project review lock")
			return identity, false
		}
		if pending {
			writeError(w, http.StatusConflict, errReviewInProgress.Error())
			return identity, false
		}
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
		template.default_display_mode,template.definition,template.status,
		(select revision.public_id from content_revisions revision where revision.id=template.published_revision_id),
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
		var revisionID *string
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
	requestedBaseRevisionID, baseErr := resolveRevisionPublicID(r.Context(), s.db, edit.BaseRevisionID)
	if baseErr != nil || !sameRevision(requestedBaseRevisionID, publishedRevisionID) {
		writeError(w, http.StatusConflict, "content template changed; reload the editor")
		return
	}
	s.submitExistingModContentMutation(w, r, identity, modContentSnapshot{Kind: "template", Operation: "edit", ModID: identity.ID, ModSiteID: identity.SiteID, PublicID: publicID, Template: &edit}, publishedRevisionID)
}

func (s *Server) modContentSections(w http.ResponseWriter, r *http.Request) {
	var identity modIdentityRecord
	if r.Method == http.MethodPost {
		var ok bool
		identity, ok = s.requireEditableMod(w, r)
		if !ok {
			return
		}
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
	rows, err := s.db.Query(r.Context(), `select section.public_id,version.public_id,template.public_id,template.code,template.builtin,template.i18n_key,
		coalesce(parent.public_id,''),section.system_key,section.default_locale,section.display_mode,
		section.ordinal,section.status,
		case when section.parent_id is null then template.definition end,
		(select revision.public_id from content_revisions revision where revision.id=section.published_revision_id),
		coalesce(
			(select jsonb_agg(jsonb_build_object('locale',localization.locale,'name',localization.name,'summary',localization.description,'contentMarkdown','') order by localization.locale)
			 from mod_content_section_localizations localization where localization.section_id=section.id),
			(select jsonb_agg(jsonb_build_object('locale',localization.locale,'name',localization.name,'summary',localization.description,'contentMarkdown','') order by localization.locale)
			 from mod_content_template_localizations localization where localization.template_id=template.id),
			'[]'::jsonb),
		coalesce((with recursive subtree as (
			select id from mod_content_sections where id=section.id and status='active'
			union all select child.id from mod_content_sections child join subtree parent on child.parent_id=parent.id where child.status='active'
		) select count(*)::int from mod_content_section_resources resource where resource.section_id in(select id from subtree)),0)
		from mod_content_sections section join mod_content_versions version on version.id=section.version_id
		join mod_content_templates template on template.id=section.template_id
		left join mod_content_sections parent on parent.id=section.parent_id
		where section.mod_id=$1 and section.status='active' and version.status='active'
		order by section.ordinal,section.id`, identity.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read content sections")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var publicID, versionPublicID, templatePublicID, templateCode, templateI18nKey, parentPublicID, systemKey, defaultLocale, displayMode, status string
		var templateBuiltin bool
		var ordinal, resourceCount int
		var revisionID *string
		var definition, localizations []byte
		if err = rows.Scan(&publicID, &versionPublicID, &templatePublicID, &templateCode, &templateBuiltin, &templateI18nKey, &parentPublicID, &systemKey, &defaultLocale, &displayMode, &ordinal, &status, &definition, &revisionID, &localizations, &resourceCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode content sections")
			return
		}
		items = append(items, map[string]any{"publicId": publicID, "versionPublicId": versionPublicID, "templatePublicId": templatePublicID,
			"templateCode": templateCode, "templateBuiltin": templateBuiltin, "templateI18nKey": templateI18nKey, "parentPublicId": parentPublicID, "systemKey": systemKey, "defaultLocale": defaultLocale,
			"displayMode": displayMode, "ordinal": ordinal, "status": status, "publishedRevisionId": revisionID,
			"definition": json.RawMessage(definition), "localizations": json.RawMessage(localizations), "resourceCount": resourceCount})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) modContentSectionResources(w http.ResponseWriter, r *http.Request) {
	identity, err := s.modIdentity(r.Context(), r.PathValue("siteId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "mod not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read mod")
		return
	}
	sectionPublicID := strings.ToLower(strings.TrimSpace(r.PathValue("sectionId")))
	primary, secondary := s.requestContentLocales(r)
	if requestedLocale := normalizeContentLocale(r.URL.Query().Get("locale")); requestedLocale != "" {
		primary = requestedLocale
	}
	localeCandidates := []string{strings.ToLower(normalizeContentLocale(primary)), strings.ToLower(normalizeContentLocale(secondary)), "en", "en-us", "zh-cn", "zh-tw"}
	// Advancement boards need the complete parent graph in one response. The
	// public UI still requests 120 rows for ordinary sections, while explicitly
	// requesting the larger bound only for the dedicated tree presentation.
	limit := boundedLimit(r.URL.Query().Get("limit"), 120, maxModContentResources)
	offset, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("offset")))
	if r.URL.Query().Get("all") == "1" {
		limit = maxModContentResources
		offset = 0
	}
	if offset < 0 {
		offset = 0
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	var sectionID, versionID int64
	var publicID, versionPublicID, versionLabel, templatePublicID, templateCode, templateI18nKey, parentPublicID, systemKey, defaultLocale, displayMode, status string
	var templateBuiltin bool
	var ordinal int
	var revisionID *string
	var localizations []byte
	err = s.db.QueryRow(r.Context(), `select section.id,section.version_id,section.public_id,version.public_id,version.label,
		template.public_id,template.code,template.builtin,template.i18n_key,coalesce(parent.public_id,''),section.system_key,section.default_locale,
		section.display_mode,section.ordinal,section.status,
		(select revision.public_id from content_revisions revision where revision.id=section.published_revision_id),
		coalesce(
		 (select jsonb_agg(jsonb_build_object('locale',localization.locale,'name',localization.name,
		  'summary',localization.description,'contentMarkdown','') order by localization.locale)
		  from mod_content_section_localizations localization where localization.section_id=section.id),
		 (select jsonb_agg(jsonb_build_object('locale',localization.locale,'name',localization.name,
		  'summary',localization.description,'contentMarkdown','') order by localization.locale)
		  from mod_content_template_localizations localization where localization.template_id=template.id),
		 '[]'::jsonb)
		from mod_content_sections section join mod_content_versions version on version.id=section.version_id
		join mod_content_templates template on template.id=section.template_id
		left join mod_content_sections parent on parent.id=section.parent_id
		where section.mod_id=$1 and section.public_id=$2 and section.status='active' and version.status='active'`, identity.ID, sectionPublicID).
		Scan(&sectionID, &versionID, &publicID, &versionPublicID, &versionLabel, &templatePublicID, &templateCode,
			&templateBuiltin, &templateI18nKey, &parentPublicID, &systemKey, &defaultLocale, &displayMode, &ordinal, &status, &revisionID, &localizations)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "content section not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read content section")
		return
	}

	var total int
	err = s.db.QueryRow(r.Context(), `with recursive subtree as (
			select id from mod_content_sections where id=$1 and status='active'
			union all
			select child.id from mod_content_sections child join subtree parent on child.parent_id=parent.id where child.status='active'
		)
		select count(*)::int from subtree
		join mod_content_section_resources section_resource on section_resource.section_id=subtree.id
		join game_resources resource on resource.entity_id=section_resource.resource_id
		join catalog_entities entity on entity.id=resource.entity_id and entity.status='active' and entity.archived_at is null
		where section_resource.version_id=$2 and ($3='' or resource.canonical_id ilike '%'||$3||'%'
		 or exists(select 1 from mod_resource_version_detail_localizations localization
		  join mod_resource_version_details active_detail on active_detail.resource_id=localization.resource_id
		   and active_detail.version_id=localization.version_id and active_detail.status='active'
		  where localization.resource_id=section_resource.resource_id and localization.version_id=$2 and localization.name ilike '%'||$3||'%')
		 or exists(select 1 from resource_import_snapshots snapshot join catalog_import_revisions revision on revision.id=snapshot.revision_id
		  where snapshot.resource_id=section_resource.resource_id and revision.target_version_id=$2 and revision.is_active
		   and snapshot.names::text ilike '%'||$3||'%'))`, sectionID, versionID, query).Scan(&total)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count content section resources")
		return
	}

	rows, err := s.db.Query(r.Context(), `with recursive subtree as (
			select id,public_id,parent_id,array[ordinal::bigint,id] sort_path from mod_content_sections where id=$1 and status='active'
			union all
			select child.id,child.public_id,child.parent_id,parent.sort_path||array[child.ordinal::bigint,child.id]
			from mod_content_sections child join subtree parent on child.parent_id=parent.id where child.status='active'
		)
		select entity.public_id,resource.kind_code,resource.canonical_id,subtree.public_id,section_resource.ordinal,
		section_resource.similar_group_id,
		coalesce(imported.revision_id,''),coalesce(imported.icon_path,''),coalesce(icon_file.public_id,''),
		coalesce(nullif(version_names.names,'{}'::jsonb),nullif((select jsonb_object_agg(name.key,name.value)
		 from jsonb_each_text(coalesce(imported.names,'{}'::jsonb)) name
		 where replace(lower(name.key),'_','-')=any($3::text[])),'{}'::jsonb),'{}'::jsonb),
		case when resource.kind_code='minecraft.advancement' then jsonb_strip_nulls(jsonb_build_object(
		 'parent',coalesce(effective.data->'parentId',effective.data->'parent'),'display',jsonb_strip_nulls(jsonb_build_object(
		  'x',effective.data#>'{display,x}','y',effective.data#>'{display,y}','frame',effective.data#>'{display,frame}'))))
		 when resource.kind_code='minecraft.loot_table' then jsonb_strip_nulls(jsonb_build_object(
		  'possible_item_ids',effective.data->'possible_item_ids')) else '{}'::jsonb end
		from subtree join mod_content_section_resources section_resource on section_resource.section_id=subtree.id
		join catalog_entities entity on entity.id=section_resource.resource_id
		join game_resources resource on resource.entity_id=section_resource.resource_id
		left join mod_resource_version_details detail on detail.resource_id=section_resource.resource_id and detail.version_id=$2 and detail.status='active'
		left join oss_files icon_file on icon_file.id=detail.icon_file_id and icon_file.status='active'
		 and icon_file.scan_status in ('clean','trusted_generated')
		left join lateral (select snapshot.revision_id,snapshot.icon_path,snapshot.names,snapshot.data from resource_import_snapshots snapshot
		 join catalog_import_revisions revision on revision.id=snapshot.revision_id
		 where snapshot.resource_id=section_resource.resource_id and revision.target_version_id=$2 and revision.is_active
		 order by (snapshot.icon_path<>'') desc,revision.created_at desc limit 1) imported on true
		left join lateral (select case when detail.definition is not null and detail.definition<>'{}'::jsonb
		 then detail.definition else coalesce(imported.data,'{}'::jsonb) end data) effective on true
		left join lateral (select jsonb_object_agg(localization.locale,localization.name) names
		 from mod_resource_version_detail_localizations localization
		 where detail.resource_id is not null and localization.resource_id=section_resource.resource_id and localization.version_id=$2
		  and coalesce(localization.name,'')<>'' and replace(lower(localization.locale),'_','-')=any($3::text[])) version_names on true
		where section_resource.version_id=$2 and entity.status='active' and entity.archived_at is null
		 and ($4='' or resource.canonical_id ilike '%'||$4||'%'
		 or exists(select 1 from mod_resource_version_detail_localizations localization
		  join mod_resource_version_details active_detail on active_detail.resource_id=localization.resource_id
		   and active_detail.version_id=localization.version_id and active_detail.status='active'
		  where localization.resource_id=section_resource.resource_id and localization.version_id=$2 and localization.name ilike '%'||$4||'%')
		 or coalesce(imported.names,'{}'::jsonb)::text ilike '%'||$4||'%')
		order by subtree.sort_path,section_resource.ordinal,resource.canonical_id limit $5 offset $6`, sectionID, versionID, localeCandidates, query, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read content section resources")
		return
	}
	defer rows.Close()
	resources := make([]map[string]any, 0, min(limit, total))
	for rows.Next() {
		var resourcePublicID, kindCode, canonicalID, resourceSectionPublicID, similarGroupID, sourceRevisionID, iconPath, iconFileID string
		var resourceOrdinal int
		var names, definition []byte
		if err = rows.Scan(&resourcePublicID, &kindCode, &canonicalID, &resourceSectionPublicID, &resourceOrdinal, &similarGroupID, &sourceRevisionID, &iconPath, &iconFileID, &names, &definition); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode content section resources")
			return
		}
		resources = append(resources, map[string]any{"versionPublicId": versionPublicID, "resourcePublicId": resourcePublicID,
			"sectionPublicId": resourceSectionPublicID, "kindCode": kindCode, "canonicalId": canonicalID, "ordinal": resourceOrdinal, "revisionId": sourceRevisionID, "iconPath": iconPath,
			"iconFileId": iconFileID, "similarGroupId": similarGroupID, "names": json.RawMessage(names), "definition": json.RawMessage(definition)})
	}
	lootItems := make([]map[string]any, 0)
	lootResourceIndexes := make([]int, 0)
	lootRevisionIDs := make([]string, 0)
	for index, resource := range resources {
		if resource["kindCode"] != "minecraft.loot_table" {
			continue
		}
		var data map[string]any
		raw, _ := resource["definition"].(json.RawMessage)
		if err = json.Unmarshal(raw, &data); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode loot table summary")
			return
		}
		lootItems = append(lootItems, map[string]any{"data": data})
		lootResourceIndexes = append(lootResourceIndexes, index)
		sourceRevisionID, _ := resource["revisionId"].(string)
		lootRevisionIDs = append(lootRevisionIDs, sourceRevisionID)
	}
	if len(lootItems) > 0 {
		if err = s.decorateLootTableResourcesByRevision(r.Context(), lootRevisionIDs, lootItems, primary, secondary); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve loot table preview icons")
			return
		}
		for index, item := range lootItems {
			data, _ := item["data"].(map[string]any)
			data["previewResources"] = lootTableIconPreviews(data)
			delete(data, "resourceSources")
			resources[lootResourceIndexes[index]]["definition"] = data
		}
	}
	categories, err := readModContentDescendantSections(r.Context(), s.db, identity.ID, sectionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read content categories")
		return
	}
	section := map[string]any{"publicId": publicID, "versionPublicId": versionPublicID, "templatePublicId": templatePublicID,
		"templateCode": templateCode, "templateBuiltin": templateBuiltin, "templateI18nKey": templateI18nKey,
		"parentPublicId": parentPublicID, "systemKey": systemKey, "defaultLocale": defaultLocale, "displayMode": displayMode, "ordinal": ordinal,
		"status": status, "publishedRevisionId": revisionID, "localizations": json.RawMessage(localizations),
		"resourceCount": total}
	writeJSON(w, http.StatusOK, map[string]any{"section": section, "versionLabel": versionLabel, "categories": categories, "items": resources, "total": total, "limit": limit, "offset": offset})
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
	requestedBaseRevisionID, baseErr := resolveRevisionPublicID(r.Context(), s.db, edit.BaseRevisionID)
	if baseErr != nil || !sameRevision(requestedBaseRevisionID, publishedRevisionID) {
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
		resolver, resolveErr := loadCatalogResourceIdentityResolver(r.Context(), s.db)
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
		r.Context(), tx, identity.ID, versionID, edit.KindCode, edit.SectionPublicID, edit.EntryTypeCode, edit.Definition,
	); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "the selected resource subtype or its fields are invalid")
		return
	}
	canonicalDefinition, normalizeErr := normalizeModContentEntryDefinition(
		r.Context(), tx, identity.ID, versionID, edit.KindCode, edit.SectionPublicID, edit.EntryTypeCode, edit.Definition, false,
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
		carrier := map[string]any{"entityId": publicID, "versions": []map[string]any{}}
		if err := s.decorateResourceVersionRows(r.Context(), []map[string]any{carrier}, primary, secondary); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve resource versions")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"entityId": publicID, "publicId": publicID, "kindCode": kindCode,
			"canonicalId": canonicalID, "details": json.RawMessage(details), "versions": carrier["versions"]})
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
		r.Context(), s.db, identity.ID, versionID, edit.KindCode, edit.SectionPublicID, edit.EntryTypeCode, edit.Definition,
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
		r.Context(), s.db, identity.ID, versionID, edit.KindCode, edit.SectionPublicID, edit.EntryTypeCode, effectiveDefinition, false,
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
		var templateCode string
		if err := tx.QueryRow(ctx, `select id,code from mod_content_templates where public_id=$1 and status='active' and (builtin or owner_mod_id=$2)`, snapshot.Section.TemplatePublicID, snapshot.ModID).Scan(&templateID, &templateCode); err != nil {
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
		if parentID == nil && templateCode == "item_block" {
			if err := ensureItemBlockSystemCategoriesTx(
				ctx, tx, sectionID, versionID, snapshot.ModID, templateID, actorID,
				snapshot.Section.DefaultLocale, snapshot.Section.DisplayMode,
			); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `delete from mod_content_section_resources where section_id=$1`, sectionID); err != nil {
			return err
		}
		for _, resource := range snapshot.Section.Resources {
			var versionID int64
			var resourceID int64
			if err := tx.QueryRow(ctx, `select id from mod_content_versions where public_id=$1 and mod_id=$2 and status='active'`, resource.VersionPublicID, snapshot.ModID).Scan(&versionID); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `select resource.entity_id from catalog_entities entity join game_resources resource on resource.entity_id=entity.id
				where entity.public_id=$1 and entity.status='active' and resource.owner_mod_id=$2`, resource.ResourcePublicID, snapshot.ModID).Scan(&resourceID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `delete from mod_content_section_resources placement using mod_content_sections section
				where section.id=placement.section_id and section.mod_id=$1 and placement.version_id=$2 and placement.resource_id=$3`,
				snapshot.ModID, versionID, resourceID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `insert into mod_content_section_resources(section_id,version_id,resource_id,ordinal,placement_source)
				values($1,$2,$3,$4,'manual')`, sectionID, versionID, resourceID, resource.Ordinal); err != nil {
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
			snapshot.Resource.EntryTypeCode, snapshot.Resource.Definition, false,
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
	entryType, err := selectModContentEntryType(template.EntryTypes, entryTypeCode, "", false)
	if err != nil {
		return err
	}
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
					var exists bool
					if err = tx.QueryRow(ctx, `select exists(select 1
						from catalog_tags tag join catalog_entities entity on entity.id=tag.entity_id
						where entity.status='active' and lower(tag.canonical_id)=$1
						  and ($2='' or lower(tag.registry)=lower($2)))`, normalized, field.ReferenceRegistry).Scan(&exists); err != nil {
						return err
					}
					if exists {
						continue
					}
					if _, err = tx.Exec(ctx, `insert into unresolved_references(
						source_type,source_id,field_path,reference_type,raw_identifier,normalized_identifier,metadata
					) values('mod_content_resource',$1,$2,'tag',$3,$4,jsonb_build_object('versionId',$5,'registry',$6))
					on conflict(source_type,source_id,field_path,reference_type,normalized_identifier) do update
					set raw_identifier=excluded.raw_identifier,status='pending',resolved_type='',resolved_id=null,
						resolved_at=null,metadata=excluded.metadata,updated_at=now()`,
						resourceID, fieldPath, identifier, normalized, versionID, field.ReferenceRegistry); err != nil {
						return err
					}
					continue
				}
				kindCode := normalizedModContentReferenceKind(field.ReferenceKind)
				var resolvedID int64
				err = tx.QueryRow(ctx, `select resource.entity_id
					from game_resources resource
					join catalog_entities entity on entity.id=resource.entity_id
					left join game_resource_aliases alias on alias.resource_id=resource.entity_id and alias.kind_code=resource.kind_code
					where resource.kind_code=$1 and entity.status='active'
					  and (lower(resource.canonical_id)=lower($2) or lower(alias.alias_id)=lower($2))
					order by resource.resolved desc,resource.entity_id limit 1`, kindCode, identifier).Scan(&resolvedID)
				if err == nil {
					continue
				}
				if !errors.Is(err, pgx.ErrNoRows) {
					return err
				}
				if _, err = tx.Exec(ctx, `insert into unresolved_resource_references(
					source_entity_id,field_path,kind_code,raw_resource_id,status
				) values($1,$2,$3,$4,'pending')`, resourceID, fieldPath, kindCode, identifier); err != nil {
					return err
				}
			}
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
