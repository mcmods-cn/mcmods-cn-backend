package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

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
			projection_source='manual',
			import_source_namespace='',
			import_source_kind='',
			import_revision_id='',
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
	_, err := normalizeModContentEntryDefinition(ctx, query, modID, versionID, kindCode, sectionPublicID, entryTypeCode, definition, false, false)
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
	compatibilities := make([]modLoaderCompatibilityPayload, 0, len(config.Loaders))
	for _, loader := range config.Loaders {
		if code := strings.TrimSpace(loader.Code); code != "" {
			compatibilities = append(compatibilities, modLoaderCompatibilityPayload{Loader: code, Versions: append([]string{}, loader.Versions...)})
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
		versionConfig, loadErr := loadMinecraftVersionConfig(ctx, s.db)
		if loadErr != nil {
			return loadErr
		}
		compatibilities = globalModContentCompatibilities(versionConfig)
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
	if edit.VersionPublicID == "" || edit.TemplatePublicID == "" || edit.ParentPublicID != "" || len(edit.Resources) != 0 ||
		(edit.DisplayMode != "compact" && edit.DisplayMode != "large") || edit.Ordinal < 0 || len(edit.Reason) > 500 {
		return errCatalogEditorInvalid
	}
	defaultLocale, localizations, err := normalizeCatalogLocalizations(edit.DefaultLocale, edit.Localizations)
	if err != nil || defaultLocale == "" || (len(localizations) > 0 && requireCatalogCreateDefaultLocalization(defaultLocale, localizations) != nil) {
		return errCatalogEditorInvalid
	}
	edit.Localizations = localizations
	edit.DefaultLocale = defaultLocale
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
	if err = finishRows(rows); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read mod content versions")
		return
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
	config := loadReviewConfig(r.Context(), tx)
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
	activityID, err := insertModContentActivityTx(r.Context(), tx, claims.Subject, snapshot)
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

func insertModContentActivityTx(ctx context.Context, tx pgx.Tx, actorID int64, snapshot modContentSnapshot) (string, error) {
	actionID := activity.ActionEdit
	if snapshot.Operation == "create" {
		actionID = activity.ActionCreate
	} else if snapshot.Operation == "delete" {
		actionID = activity.ActionDelete
	}
	var eventID string
	err := tx.QueryRow(ctx, `insert into user_activity_events(user_id,action_id,object_type_id,object_route_id,occurred_at)
		select $1,$2,$3,route.id,$5 from public_routes route
		where route.entity_type='mod' and route.internal_id=$4 returning id::text`, actorID, actionID, activity.ObjectMod, snapshot.ModID, time.Now().UTC()).Scan(&eventID)
	return eventID, err
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
	if err = finishRows(rows); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read content templates")
		return
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
	if err = finishRows(rows); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read content sections")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) modContentSectionResources(w http.ResponseWriter, r *http.Request) {
	if !s.allowModContentRead(w, r, "mod-content-section-cards", 60) {
		return
	}
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
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if r.URL.Query().Has("offset") || r.URL.Query().Has("all") {
		writeAPIError(w, http.StatusBadRequest, "MOD_CONTENT_SECTION_CURSOR_REQUIRED", "offset and all pagination are no longer supported", 0, nil)
		return
	}
	if query != "" && (utf8.RuneCountInString(query) < 3 || utf8.RuneCountInString(query) > 100) {
		writeAPIError(w, http.StatusBadRequest, "MOD_CONTENT_SECTION_QUERY_INVALID", "resource search must contain between 3 and 100 characters", 0, nil)
		return
	}
	limit := boundedLimit(r.URL.Query().Get("limit"), 120, maxModContentSectionPageSize)

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
	revisionScope := ""
	if revisionID != nil {
		revisionScope = *revisionID
	}
	cursorScope := modContentSectionCursorScope(identity.ID, sectionID, versionID, revisionScope, query, limit, "cards")
	cursor, cursorErr := decodeModContentSectionCursor(r.URL.Query().Get("cursor"), cursorScope)
	if cursorErr != nil {
		writeAPIError(w, http.StatusBadRequest, "MOD_CONTENT_SECTION_CURSOR_INVALID", "invalid resource page cursor", 0, nil)
		return
	}

	total := 0
	if cursor != nil {
		total = cursor.Total
	} else {
		err = s.db.QueryRow(r.Context(), `with recursive subtree as (
			select id from mod_content_sections where id=$1 and status='active'
			union all
			select child.id from mod_content_sections child join subtree parent on child.parent_id=parent.id where child.status='active'
		)
		select count(*)::int from subtree
		join mod_content_section_resources section_resource on section_resource.section_id=subtree.id
		join game_resources resource on resource.entity_id=section_resource.resource_id
		join catalog_entities entity on entity.id=resource.entity_id and entity.status='active' and entity.archived_at is null
		where section_resource.version_id=$2 and ($3='' or
		 section_resource.search_document@@websearch_to_tsquery('simple',$3))`, sectionID, versionID, query).Scan(&total)
		if err != nil {
			log.Printf("count mod-content section resources: %v", err)
			writeError(w, http.StatusInternalServerError, "failed to count content section resources")
			return
		}
	}
	cursorActive := cursor != nil
	cursorPath := []int64{}
	cursorOrdinal := 0
	cursorResourceID := int64(0)
	if cursor != nil {
		cursorPath, cursorOrdinal, cursorResourceID = cursor.SortPath, cursor.Ordinal, cursor.ResourceID
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
		exists(select 1 from mod_resource_version_detail_localizations localization
		 where localization.resource_id=section_resource.resource_id and localization.version_id=$2
		  and replace(lower(localization.locale),'_','-')=any($3::text[])
		  and regexp_replace(regexp_replace(coalesce(localization.content_markdown,''),'<[^>]*>','','g'),
		   '[[:space:]#*_>\[\]()~-]+','','g')<>''),
		case when resource.kind_code='minecraft.advancement' then jsonb_strip_nulls(jsonb_build_object(
		 'parent',coalesce(effective.data->'parentId',effective.data->'parent'),'display',jsonb_strip_nulls(jsonb_build_object(
		  'x',effective.data#>'{display,x}','y',effective.data#>'{display,y}','frame',effective.data#>'{display,frame}'))))
		 when resource.kind_code='minecraft.loot_table' then jsonb_strip_nulls(jsonb_build_object(
		 'possible_item_ids',effective.data->'possible_item_ids')) else '{}'::jsonb end,
		subtree.sort_path,section_resource.resource_id
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
		 and ($4='' or section_resource.search_document@@websearch_to_tsquery('simple',$4))
		 and (not $5 or subtree.sort_path>$6::bigint[] or
		  (subtree.sort_path=$6::bigint[] and (section_resource.ordinal,section_resource.resource_id)>
		   ($7::integer,$8::bigint)))
		order by subtree.sort_path,section_resource.ordinal,section_resource.resource_id limit $9`,
		sectionID, versionID, localeCandidates, query, cursorActive, cursorPath, cursorOrdinal, cursorResourceID, limit+1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read content section resources")
		return
	}
	defer rows.Close()
	resources := make([]map[string]any, 0, min(limit+1, total))
	resourceCursors := make([]modContentSectionPageCursor, 0, min(limit+1, total))
	for rows.Next() {
		var resourcePublicID, kindCode, canonicalID, resourceSectionPublicID, similarGroupID, sourceRevisionID, iconPath, iconFileID string
		var resourceOrdinal int
		var internalResourceID int64
		var sortPath []int64
		var hasDetailDescription bool
		var names, definition []byte
		if err = rows.Scan(&resourcePublicID, &kindCode, &canonicalID, &resourceSectionPublicID, &resourceOrdinal, &similarGroupID, &sourceRevisionID, &iconPath, &iconFileID, &names, &hasDetailDescription, &definition, &sortPath, &internalResourceID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode content section resources")
			return
		}
		resources = append(resources, map[string]any{"versionPublicId": versionPublicID, "resourcePublicId": resourcePublicID,
			"sectionPublicId": resourceSectionPublicID, "kindCode": kindCode, "canonicalId": canonicalID, "ordinal": resourceOrdinal, "revisionId": sourceRevisionID, "iconPath": iconPath,
			"iconFileId": iconFileID, "similarGroupId": similarGroupID, "names": json.RawMessage(names), "hasDetailDescription": hasDetailDescription, "definition": json.RawMessage(definition)})
		resourceCursors = append(resourceCursors, modContentSectionPageCursor{
			Version: modContentSectionCursorVersion, Scope: cursorScope, SortPath: sortPath,
			Ordinal: resourceOrdinal, ResourceID: internalResourceID, Total: total,
		})
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read content section resources")
		return
	}
	hasMore := len(resources) > limit
	if hasMore {
		resources = resources[:limit]
		resourceCursors = resourceCursors[:limit]
	}
	nextCursor := ""
	if hasMore && len(resourceCursors) > 0 {
		nextCursor = encodeModContentSectionCursor(resourceCursors[len(resourceCursors)-1])
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
	categories := make([]map[string]any, 0)
	if cursor == nil {
		categories, err = readModContentDescendantSections(r.Context(), s.db, identity.ID, sectionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read content categories")
			return
		}
	}
	section := map[string]any{"publicId": publicID, "versionPublicId": versionPublicID, "templatePublicId": templatePublicID,
		"templateCode": templateCode, "templateBuiltin": templateBuiltin, "templateI18nKey": templateI18nKey,
		"parentPublicId": parentPublicID, "systemKey": systemKey, "defaultLocale": defaultLocale, "displayMode": displayMode, "ordinal": ordinal,
		"status": status, "publishedRevisionId": revisionID, "localizations": json.RawMessage(localizations),
		"resourceCount": total}
	markModContentCapabilityResponse(w)
	writeJSON(w, http.StatusOK, map[string]any{"section": section, "versionLabel": versionLabel, "categories": categories, "items": resources, "total": total, "limit": limit,
		"hasMore": hasMore, "nextCursor": nextCursor,
		"capabilities": modContentCapabilitiesFor(currentClaims(r), identity)})
}
