package httpapi

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestNormalizeCatalogLocalizationsRestrictsHumanEditing(t *testing.T) {
	editable := false
	defaultLocale, localizations, err := normalizeCatalogLocalizations("zh_cn", []catalogLocalizationEdit{{
		Locale: "zh_cn", Name: "  名称  ", Provenance: "ai", SourceLocale: "en", Editable: &editable,
	}})
	if err != nil {
		t.Fatalf("normalize supported localization: %v", err)
	}
	if defaultLocale != "zh-CN" || len(localizations) != 1 || localizations[0].Locale != "zh-CN" {
		t.Fatalf("unexpected normalized locales: default=%q values=%+v", defaultLocale, localizations)
	}
	if localizations[0].Name != "名称" || localizations[0].Provenance != "human" || localizations[0].SourceLocale != "" || localizations[0].Editable == nil || !*localizations[0].Editable {
		t.Fatalf("user localization was not normalized as human-editable: %+v", localizations[0])
	}
	if _, _, err = normalizeCatalogLocalizations("pt-BR", []catalogLocalizationEdit{{Locale: "pt-BR", Name: "Nome"}}); !errors.Is(err, errCatalogEditorInvalid) {
		t.Fatalf("unsupported human-editable locale should fail, got %v", err)
	}
}

func TestNormalizeCatalogLocalizationsAllowsPartialEditPatch(t *testing.T) {
	defaultLocale, localizations, err := normalizeCatalogLocalizations("en", []catalogLocalizationEdit{{Locale: "ja", Name: "圧縮"}})
	if err != nil {
		t.Fatalf("partial non-default locale edit should be accepted: %v", err)
	}
	if defaultLocale != "en" || len(localizations) != 1 || localizations[0].Locale != "ja" {
		t.Fatalf("unexpected normalized partial patch: default=%q values=%+v", defaultLocale, localizations)
	}
	if err = requireCatalogCreateDefaultLocalization(defaultLocale, localizations); !errors.Is(err, errCatalogEditorInvalid) {
		t.Fatalf("create must still require its default locale, got %v", err)
	}
	if err = requireCatalogCreateDefaultLocalization(defaultLocale, []catalogLocalizationEdit{{Locale: "en", Name: "Compressing"}}); err != nil {
		t.Fatalf("create with its default locale should pass: %v", err)
	}
	if err = requireCatalogCreateDefaultLocalization(defaultLocale, []catalogLocalizationEdit{{Locale: "en", Name: "   "}}); !errors.Is(err, errCatalogEditorInvalid) {
		t.Fatalf("create with an empty default-locale name should fail, got %v", err)
	}
}

func TestValidateCatalogTemplateGeometry(t *testing.T) {
	valid := &catalogRecipeTemplateEdit{
		TemplateKey: "machine", Canvas: catalogCanvas{Width: 176, Height: 84, ImageScale: 1},
		Slots: []catalogTemplateSlotEdit{
			{SlotKey: "input", Role: "input", Ordinal: 0, Rect: catalogSlotRect{X: 10, Y: 10, Width: 18, Height: 18}},
			{SlotKey: "output", Role: "output", Ordinal: 1, Rect: catalogSlotRect{X: 140, Y: 10, Width: 18, Height: 18}},
		},
	}
	if err := validateCatalogTemplate(valid); err != nil {
		t.Fatalf("valid template rejected: %v", err)
	}
	invalid := *valid
	invalid.Slots = append([]catalogTemplateSlotEdit(nil), valid.Slots...)
	invalid.Slots[1].Rect.X = 170
	if err := validateCatalogTemplate(&invalid); !errors.Is(err, errCatalogEditorInvalid) {
		t.Fatalf("out-of-canvas slot should fail, got %v", err)
	}
}

func TestRecipeProbabilityBelongsToEachOutputCandidate(t *testing.T) {
	first, second := 0.12, 0.75
	edit := &catalogRecipeEdit{TemplatePublicID: "abc234567", Bindings: map[string]catalogRecipeBindingEdit{
		"input": {Candidates: []catalogRecipeCandidateEdit{{ResourcePublicID: "def234567", Amount: 1}}},
		"output": {Candidates: []catalogRecipeCandidateEdit{
			{ResourcePublicID: "ghi234567", Amount: 1, Probability: &first},
			{ResourcePublicID: "jkm234567", Amount: 1, Probability: &second, Byproduct: true},
		}},
	}}
	roles := map[string]string{"input": "input", "output": "output"}
	if err := validateCatalogRecipeBindings(edit, roles); err != nil {
		t.Fatalf("independent output probabilities were rejected: %v", err)
	}
	edit.Bindings["input"] = catalogRecipeBindingEdit{Candidates: []catalogRecipeCandidateEdit{{
		ResourcePublicID: "def234567", Amount: 1, Probability: &first,
	}}}
	if err := validateCatalogRecipeBindings(edit, roles); !errors.Is(err, errCatalogEditorInvalid) {
		t.Fatalf("input probability should fail, got %v", err)
	}
}

func TestCatalogMutationBaseSupportsFirstManualEditOfImport(t *testing.T) {
	if !catalogMutationBaseMatches("create", "placeholder", nil, nil) {
		t.Fatal("an unpublished placeholder must allow a create submission")
	}
	placeholderRevision := int64(1)
	if catalogMutationBaseMatches("create", "placeholder", nil, &placeholderRevision) || catalogMutationBaseMatches("create", "active", nil, nil) {
		t.Fatal("a create submission must not overwrite published or active content")
	}
	if !catalogMutationBaseMatches("edit", "active", nil, nil) {
		t.Fatal("an imported active entity without a canonical revision must allow its first manual edit")
	}
	published, stale := int64(42), int64(41)
	if catalogMutationBaseMatches("edit", "active", nil, &published) || catalogMutationBaseMatches("edit", "active", &stale, &published) {
		t.Fatal("published canonical content must reject missing or stale bases")
	}
	if !catalogMutationBaseMatches("edit", "active", &published, &published) {
		t.Fatal("matching published base should be accepted")
	}
}

func TestCatalogMutationResponseIncludesActivityIdentity(t *testing.T) {
	raw, err := json.Marshal(catalogEditResult{PublicID: "abc234567", ObjectPublicID: "abc234567", ActivityEventID: 91})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err = json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["objectPublicId"] != "abc234567" || payload["activityEventId"] != float64(91) {
		t.Fatalf("mutation response is missing durable activity identity: %s", raw)
	}
}
