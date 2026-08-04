package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

const (
	catalogAggregateResource       = "catalog_editor_resource"
	catalogAggregateTag            = "catalog_editor_tag"
	catalogAggregateRecipeType     = "catalog_editor_recipe_type"
	catalogAggregateRecipeTemplate = "catalog_editor_recipe_template"
	catalogAggregateRecipe         = "catalog_editor_recipe"
	catalogAggregateLocalization   = "catalog_localization"
)

var (
	errCatalogEditorInvalid   = errors.New("invalid catalog edit")
	errCatalogEditorNotFound  = errors.New("catalog entity not found")
	errCatalogEditorConflict  = errors.New("catalog edit conflict")
	errCatalogEditorReference = errors.New("invalid catalog reference")
)

type catalogEditBase struct {
	BaseRevisionID *string                   `json:"baseRevisionId,omitempty"`
	Reason         string                    `json:"reason"`
	DefaultLocale  string                    `json:"defaultLocale"`
	Localizations  []catalogLocalizationEdit `json:"localizations"`
}

type catalogLocalizationEdit struct {
	Locale          string `json:"locale"`
	Name            string `json:"name"`
	Summary         string `json:"summary"`
	ContentMarkdown string `json:"contentMarkdown"`
	Provenance      string `json:"provenance,omitempty"`
	SourceLocale    string `json:"sourceLocale,omitempty"`
	Editable        *bool  `json:"editable,omitempty"`
	ReviewStatus    string `json:"reviewStatus,omitempty"`
}

type catalogResourceEdit struct {
	catalogEditBase
	KindCode       string         `json:"kindCode"`
	CanonicalID    string         `json:"canonicalId"`
	RawCanonicalID string         `json:"rawCanonicalId,omitempty"`
	Definition     map[string]any `json:"definition"`
	IconFileID     *string        `json:"iconFileId,omitempty"`
	RenderFileID   *string        `json:"renderFileId,omitempty"`
}

type catalogTagEdit struct {
	catalogEditBase
	Registry                string   `json:"registry"`
	CanonicalID             string   `json:"canonicalId"`
	MemberResourcePublicIDs []string `json:"memberResourcePublicIds"`
}

type catalogRecipeTypeEdit struct {
	catalogEditBase
	CanonicalID               string         `json:"canonicalId"`
	Definition                map[string]any `json:"definition"`
	CatalystResourcePublicIDs []string       `json:"catalystResourcePublicIds"`
}

type catalogCanvas struct {
	Width      int            `json:"width"`
	Height     int            `json:"height"`
	ImageScale int            `json:"imageScale"`
	Definition map[string]any `json:"definition,omitempty"`
}

type catalogSlotRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type catalogTemplateSlotEdit struct {
	SlotKey     string          `json:"slotKey"`
	Role        string          `json:"role"`
	OutputIndex *int            `json:"outputIndex,omitempty"`
	Ordinal     int             `json:"ordinal"`
	Rect        catalogSlotRect `json:"rect"`
	Definition  map[string]any  `json:"definition,omitempty"`
}

type catalogRecipeTemplateEdit struct {
	catalogEditBase
	TemplateKey      string                    `json:"templateKey"`
	BackgroundFileID *string                   `json:"backgroundFileId,omitempty"`
	Canvas           catalogCanvas             `json:"canvas"`
	Definition       map[string]any            `json:"definition"`
	Slots            []catalogTemplateSlotEdit `json:"slots"`
}

type catalogRecipeCandidateEdit struct {
	ResourcePublicID string         `json:"resourcePublicId"`
	RawResourceID    string         `json:"rawResourceId,omitempty"`
	KindCode         string         `json:"kindCode,omitempty"`
	Amount           float64        `json:"amount"`
	Probability      *float64       `json:"probability,omitempty"`
	Byproduct        bool           `json:"byproduct"`
	Definition       map[string]any `json:"definition,omitempty"`
}

type catalogRecipeBindingEdit struct {
	Candidates []catalogRecipeCandidateEdit `json:"candidates"`
	Definition map[string]any               `json:"definition,omitempty"`
}

type catalogRecipeEdit struct {
	catalogEditBase
	RecipeTypePublicID    string                              `json:"recipeTypePublicId,omitempty"`
	TemplatePublicID      string                              `json:"templatePublicId"`
	SourceVersionPublicID string                              `json:"sourceVersionPublicId,omitempty"`
	CanonicalSourceID     string                              `json:"canonicalSourceId"`
	Definition            map[string]any                      `json:"definition"`
	Bindings              map[string]catalogRecipeBindingEdit `json:"bindings"`
}

type catalogEditorSnapshot struct {
	Operation         string                     `json:"operation"`
	Reason            string                     `json:"reason,omitempty"`
	Kind              string                     `json:"kind"`
	EntityID          int64                      `json:"-"`
	IdentityKey       string                     `json:"-"`
	PublicID          string                     `json:"publicId"`
	ParentEntityID    int64                      `json:"-"`
	ParentPublicID    string                     `json:"parentPublicId,omitempty"`
	OwnerModID        *int64                     `json:"-"`
	OwnerModPublicID  string                     `json:"ownerModId,omitempty"`
	AllowForeignFiles bool                       `json:"allowForeignFiles,omitempty"`
	DefaultLocale     string                     `json:"defaultLocale"`
	Localizations     []catalogLocalizationEdit  `json:"localizations,omitempty"`
	Resource          *catalogResourceEdit       `json:"resource,omitempty"`
	Tag               *catalogTagEdit            `json:"tag,omitempty"`
	RecipeType        *catalogRecipeTypeEdit     `json:"recipeType,omitempty"`
	Template          *catalogRecipeTemplateEdit `json:"template,omitempty"`
	Recipe            *catalogRecipeEdit         `json:"recipe,omitempty"`
}

type catalogLocalizationSnapshot struct {
	SubjectPublicID  string  `json:"subjectPublicId"`
	SubjectType      string  `json:"subjectType"`
	EntityID         int64   `json:"-"`
	Locale           string  `json:"locale"`
	Name             string  `json:"name"`
	Summary          string  `json:"summary"`
	ContentMarkdown  string  `json:"contentMarkdown"`
	Provenance       string  `json:"provenance"`
	SourceLocale     string  `json:"sourceLocale"`
	SourceRevisionNo int64   `json:"sourceRevisionNo,omitempty"`
	Editable         bool    `json:"editable"`
	ReviewStatus     string  `json:"reviewStatus"`
	AITaskID         *int64  `json:"-"`
	AITaskPublicID   *string `json:"aiTaskId,omitempty"`
}

type catalogEditResult struct {
	PublicID        string `json:"publicId"`
	ObjectPublicID  string `json:"objectPublicId"`
	Operation       string `json:"operation"`
	RevisionID      string `json:"revisionId"`
	ChangeRequestID string `json:"changeRequestId"`
	ReviewStatus    string `json:"reviewStatus"`
	ActivityEventID string `json:"activityEventId"`
}

func normalizeCatalogLocale(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "_", "-"))
	if !validContentLocaleTag(value) {
		return "", errCatalogEditorInvalid
	}
	return normalizeContentLocale(value), nil
}

func normalizeCatalogLocalizations(defaultLocale string, localizations []catalogLocalizationEdit) (string, []catalogLocalizationEdit, error) {
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
		if !isEditableContentLocale(locale) {
			return "", nil, fmt.Errorf("%w: locale is not editable", errCatalogEditorInvalid)
		}
		localizations[index].Name = strings.TrimSpace(localizations[index].Name)
		localizations[index].Summary = strings.TrimSpace(localizations[index].Summary)
		if len(localizations[index].Name) > 512 || len(localizations[index].Summary) > 4096 || len(localizations[index].ContentMarkdown) > maxModExportEntryMarkdownBytes {
			return "", nil, fmt.Errorf("%w: localized content is too large", errCatalogEditorInvalid)
		}
		// User-authored mutations cannot claim AI provenance or lock a locale.
		localizations[index].Provenance = "human"
		localizations[index].SourceLocale = ""
		editable := true
		localizations[index].Editable = &editable
		localizations[index].ReviewStatus = "approved"
	}
	if strings.TrimSpace(defaultLocale) == "" {
		defaultLocale = "en-US"
	}
	normalizedDefault, err := normalizeCatalogLocale(defaultLocale)
	if err != nil {
		return "", nil, fmt.Errorf("%w: invalid default locale", errCatalogEditorInvalid)
	}
	if !isEditableContentLocale(normalizedDefault) {
		return "", nil, fmt.Errorf("%w: default locale is not editable", errCatalogEditorInvalid)
	}
	return normalizedDefault, localizations, nil
}

func requireCatalogCreateDefaultLocalization(defaultLocale string, localizations []catalogLocalizationEdit) error {
	if len(localizations) == 0 {
		return fmt.Errorf("%w: default locale is missing", errCatalogEditorInvalid)
	}
	for _, localization := range localizations {
		if localization.Locale == defaultLocale {
			if strings.TrimSpace(localization.Name) == "" {
				return fmt.Errorf("%w: default locale name is missing", errCatalogEditorInvalid)
			}
			return nil
		}
	}
	return fmt.Errorf("%w: default locale is missing", errCatalogEditorInvalid)
}

func normalizeCatalogOptionalPublicID(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", nil
	}
	if !validCatalogPublicID(value) {
		return "", errCatalogEditorInvalid
	}
	return value, nil
}

func validateCatalogTemplate(edit *catalogRecipeTemplateEdit) error {
	if edit == nil || strings.TrimSpace(edit.TemplateKey) == "" || len(edit.TemplateKey) > 256 || edit.Canvas.Width <= 0 || edit.Canvas.Width > 8192 || edit.Canvas.Height <= 0 || edit.Canvas.Height > 8192 {
		return fmt.Errorf("%w: invalid recipe template", errCatalogEditorInvalid)
	}
	if edit.Canvas.ImageScale == 0 {
		edit.Canvas.ImageScale = 1
	}
	if edit.Canvas.ImageScale < 1 || edit.Canvas.ImageScale > 32 || len(edit.Slots) > 512 {
		return fmt.Errorf("%w: invalid recipe template canvas", errCatalogEditorInvalid)
	}
	keys := make(map[string]struct{}, len(edit.Slots))
	ordinals := make(map[int]struct{}, len(edit.Slots))
	for index := range edit.Slots {
		slot := &edit.Slots[index]
		slot.SlotKey = strings.TrimSpace(slot.SlotKey)
		slot.Role = strings.ToLower(strings.TrimSpace(slot.Role))
		if slot.SlotKey == "" || len(slot.SlotKey) > 128 || (slot.Role != "input" && slot.Role != "output" && slot.Role != "catalyst") || slot.Ordinal < 0 {
			return fmt.Errorf("%w: invalid template slot", errCatalogEditorInvalid)
		}
		if _, duplicate := keys[slot.SlotKey]; duplicate {
			return fmt.Errorf("%w: duplicate template slot", errCatalogEditorInvalid)
		}
		if _, duplicate := ordinals[slot.Ordinal]; duplicate {
			return fmt.Errorf("%w: duplicate template slot ordinal", errCatalogEditorInvalid)
		}
		keys[slot.SlotKey], ordinals[slot.Ordinal] = struct{}{}, struct{}{}
		rect := slot.Rect
		if math.IsNaN(rect.X) || math.IsNaN(rect.Y) || math.IsNaN(rect.Width) || math.IsNaN(rect.Height) ||
			math.IsInf(rect.X, 0) || math.IsInf(rect.Y, 0) || math.IsInf(rect.Width, 0) || math.IsInf(rect.Height, 0) ||
			rect.X < 0 || rect.Y < 0 || rect.Width <= 0 || rect.Height <= 0 || rect.X+rect.Width > float64(edit.Canvas.Width) || rect.Y+rect.Height > float64(edit.Canvas.Height) {
			return fmt.Errorf("%w: template slot is outside the canvas", errCatalogEditorInvalid)
		}
		if slot.Role != "output" && slot.OutputIndex != nil {
			return fmt.Errorf("%w: only output slots may have outputIndex", errCatalogEditorInvalid)
		}
	}
	return nil
}

func validateCatalogRecipeBindings(edit *catalogRecipeEdit, slotRoles map[string]string) error {
	if edit == nil || strings.TrimSpace(edit.TemplatePublicID) == "" || len(edit.Bindings) > 512 {
		return fmt.Errorf("%w: invalid recipe", errCatalogEditorInvalid)
	}
	hasOutput := false
	for slotKey, binding := range edit.Bindings {
		role, exists := slotRoles[slotKey]
		if !exists {
			return fmt.Errorf("%w: binding references an unknown slot", errCatalogEditorReference)
		}
		if len(binding.Candidates) == 0 || len(binding.Candidates) > 256 {
			return fmt.Errorf("%w: every binding requires a candidate", errCatalogEditorInvalid)
		}
		if role == "output" {
			hasOutput = true
		}
		seen := make(map[string]struct{}, len(binding.Candidates))
		for _, candidate := range binding.Candidates {
			candidate.ResourcePublicID = strings.TrimSpace(candidate.ResourcePublicID)
			candidate.RawResourceID = strings.TrimSpace(candidate.RawResourceID)
			candidate.KindCode = strings.TrimSpace(candidate.KindCode)
			unresolved := candidate.RawResourceID != ""
			if candidate.ResourcePublicID == "" && !unresolved ||
				candidate.ResourcePublicID != "" && unresolved ||
				unresolved && candidate.KindCode == "" ||
				math.IsNaN(candidate.Amount) || math.IsInf(candidate.Amount, 0) || candidate.Amount <= 0 {
				return fmt.Errorf("%w: invalid recipe candidate", errCatalogEditorInvalid)
			}
			if role != "output" && (candidate.Probability != nil || candidate.Byproduct) {
				return fmt.Errorf("%w: probability and byproduct require an output item", errCatalogEditorInvalid)
			}
			if candidate.Probability != nil && (math.IsNaN(*candidate.Probability) || math.IsInf(*candidate.Probability, 0) || *candidate.Probability < 0 || *candidate.Probability > 1) {
				return fmt.Errorf("%w: probability must be between zero and one", errCatalogEditorInvalid)
			}
			identity := candidate.ResourcePublicID
			if unresolved {
				identity = "unresolved:" + candidate.KindCode + ":" + strings.ToLower(candidate.RawResourceID)
			}
			if _, duplicate := seen[identity]; duplicate {
				return fmt.Errorf("%w: duplicate recipe candidate", errCatalogEditorInvalid)
			}
			seen[identity] = struct{}{}
		}
	}
	if !hasOutput {
		return fmt.Errorf("%w: recipe requires an output candidate", errCatalogEditorInvalid)
	}
	return nil
}

func nonNilJSONObject(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func catalogJSON(value any) []byte {
	raw, _ := json.Marshal(value)
	return raw
}
