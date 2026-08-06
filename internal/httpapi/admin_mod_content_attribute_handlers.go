package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type adminModContentAttributeTemplateEdit struct {
	Definition    map[string]any            `json:"definition"`
	Localizations []catalogLocalizationEdit `json:"localizations"`
}

// adminModContentAttributeTemplates manages the website-wide built-in data
// page contracts. Mods and their content versions instantiate these templates;
// they do not own separate copies of resource types or field definitions.
func (s *Server) adminModContentAttributeTemplates(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	rows, err := s.db.Query(r.Context(), `select template.public_id,template.code,template.i18n_key,
		template.default_locale,template.default_display_mode,
		coalesce((select jsonb_object_agg(localization.locale,localization.name order by localization.locale)
			from mod_content_template_localizations localization where localization.template_id=template.id),'{}'::jsonb),
		coalesce((select jsonb_object_agg(localization.locale,localization.description order by localization.locale)
			from mod_content_template_localizations localization where localization.template_id=template.id),'{}'::jsonb),
		template.definition,template.updated_at
		from mod_content_templates template
		where template.builtin and template.status='active'
		  and ($1='' or template.code ilike '%'||$1||'%' or template.i18n_key ilike '%'||$1||'%'
		    or exists(select 1 from mod_content_template_localizations localization
		      where localization.template_id=template.id and localization.name ilike '%'||$1||'%'))
		order by template.id`, query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read resource attribute templates")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var templatePublicID, templateCode, templateI18nKey, defaultLocale, defaultDisplayMode string
		var pageNames, pageDescriptions, definition []byte
		var updatedAt time.Time
		if err = rows.Scan(&templatePublicID, &templateCode, &templateI18nKey, &defaultLocale, &defaultDisplayMode,
			&pageNames, &pageDescriptions, &definition, &updatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode resource attribute templates")
			return
		}
		items = append(items, map[string]any{
			"templatePublicId":   templatePublicID,
			"templateCode":       templateCode,
			"templateI18nKey":    templateI18nKey,
			"defaultLocale":      defaultLocale,
			"defaultDisplayMode": defaultDisplayMode,
			"pageNames":          json.RawMessage(pageNames),
			"pageDescriptions":   json.RawMessage(pageDescriptions),
			"definition":         json.RawMessage(definition),
			"updatedAt":          updatedAt,
		})
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read resource attribute templates")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) adminModContentAttributeTemplate(w http.ResponseWriter, r *http.Request) {
	templatePublicID := strings.ToLower(strings.TrimSpace(r.PathValue("templateId")))
	var edit adminModContentAttributeTemplateEdit
	if !modContentPublicIDPattern.MatchString(templatePublicID) || decodeJSON(r, &edit) != nil ||
		edit.Definition == nil || validateModContentTemplateDefinition(edit.Definition) != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid resource attribute template")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start resource attribute template update")
		return
	}
	defer tx.Rollback(r.Context())
	var templateID int64
	var defaultLocale string
	var currentDefinition []byte
	if err = tx.QueryRow(r.Context(), `select id,default_locale,definition from mod_content_templates
		where public_id=$1 and builtin and status='active' for update`, templatePublicID).
		Scan(&templateID, &defaultLocale, &currentDefinition); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "resource data page template not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read resource attribute template")
		return
	}
	_, localizations, localizationErr := normalizeCatalogLocalizations(defaultLocale, edit.Localizations)
	if localizationErr != nil || requireCatalogCreateDefaultLocalization(defaultLocale, localizations) != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid resource data page localizations")
		return
	}
	var currentSchema modContentTemplateDefinition
	if json.Unmarshal(currentDefinition, &currentSchema) != nil {
		writeError(w, http.StatusInternalServerError, "failed to decode resource attribute template")
		return
	}
	encoded, _ := json.Marshal(edit.Definition)
	var nextSchema modContentTemplateDefinition
	if json.Unmarshal(encoded, &nextSchema) != nil || !sameNormalizedStrings(currentSchema.ResourceKinds, nextSchema.ResourceKinds) ||
		preservesModContentAttributeSchema(currentSchema, nextSchema) != nil {
		writeError(w, http.StatusConflict, "resource attribute IDs, types, or page resource kinds cannot be changed")
		return
	}
	var updatedAt time.Time
	if err = tx.QueryRow(r.Context(), `update mod_content_templates
		set definition=$1::jsonb,updated_at=now()
		where id=$2 returning updated_at`, string(encoded), templateID).Scan(&updatedAt); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save resource attribute template")
		return
	}
	if _, err = tx.Exec(r.Context(), `delete from mod_content_template_localizations where template_id=$1`, templateID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save resource data page localizations")
		return
	}
	pageNames := make(map[string]string, len(localizations))
	pageDescriptions := make(map[string]string, len(localizations))
	for _, localization := range localizations {
		if _, err = tx.Exec(r.Context(), `insert into mod_content_template_localizations(template_id,locale,name,description)
			values($1,$2,$3,$4)`, templateID, localization.Locale, localization.Name, localization.Summary); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save resource data page localizations")
			return
		}
		pageNames[localization.Locale] = localization.Name
		pageDescriptions[localization.Locale] = localization.Summary
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save resource attribute template")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"templatePublicId": templatePublicID,
		"definition":       json.RawMessage(encoded),
		"pageNames":        pageNames,
		"pageDescriptions": pageDescriptions,
		"updatedAt":        updatedAt,
	})
}

func preservesModContentAttributeSchema(current, next modContentTemplateDefinition) error {
	nextTypes := make(map[string]modContentEntryTypeDefinition, len(next.EntryTypes))
	for _, entryType := range next.EntryTypes {
		nextTypes[strings.ToLower(strings.TrimSpace(entryType.Code))] = entryType
	}
	for _, currentType := range current.EntryTypes {
		code := strings.ToLower(strings.TrimSpace(currentType.Code))
		nextType, exists := nextTypes[code]
		if !exists {
			continue
		}
		if !sameNormalizedStrings(currentType.KindCodes, nextType.KindCodes) {
			return errCatalogEditorInvalid
		}
		currentFields := modContentFieldsByCode(currentType)
		nextFields := modContentFieldsByCode(nextType)
		for fieldCode, currentField := range currentFields {
			nextField, fieldExists := nextFields[fieldCode]
			if !fieldExists || modContentFieldStorageSignature(currentField) != modContentFieldStorageSignature(nextField) {
				return errCatalogEditorInvalid
			}
		}
	}
	return nil
}

func modContentFieldsByCode(entryType modContentEntryTypeDefinition) map[string]modContentEntryTypeField {
	result := make(map[string]modContentEntryTypeField)
	for _, group := range entryType.Groups {
		for _, field := range group.Fields {
			result[field.Code] = field
		}
	}
	return result
}

func modContentFieldStorageSignature(field modContentEntryTypeField) string {
	return strings.Join([]string{
		strings.ToLower(strings.TrimSpace(field.Type)),
		strings.ToLower(strings.TrimSpace(field.ReferenceKind)),
		strings.ToLower(strings.TrimSpace(field.ReferenceRegistry)),
	}, "\x00")
}

func sameNormalizedStrings(left, right []string) bool {
	normalize := func(values []string) []string {
		result := make([]string, 0, len(values))
		for _, value := range values {
			if value = strings.ToLower(strings.TrimSpace(value)); value != "" {
				result = append(result, value)
			}
		}
		sort.Strings(result)
		return result
	}
	left = normalize(left)
	right = normalize(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
