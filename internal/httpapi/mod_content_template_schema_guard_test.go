package httpapi

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestTemplateSchemaGuardIsWiredToBuiltinAndCustomPublication(t *testing.T) {
	adminSource, err := os.ReadFile("admin_mod_content_attribute_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	contentSource, err := os.ReadFile("mod_content_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(adminSource), "validateModContentTemplateSchemaChangeTx(") != 1 {
		t.Fatal("built-in template update does not use the transactional schema reference guard")
	}
	resourceSource, err := os.ReadFile("mod_content_resource_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	combinedContentSource := string(contentSource) + string(resourceSource)
	if strings.Count(combinedContentSource, "validateModContentTemplateSchemaChangeTx(") != 1 ||
		strings.Count(combinedContentSource, "validateModContentTemplateDeletionTx(") != 1 {
		t.Fatal("custom template publish/delete does not use both transactional schema reference guards")
	}
}

func TestRemovedModContentEntryTypeCodesAreNormalized(t *testing.T) {
	current := modContentTemplateDefinition{EntryTypes: []modContentEntryTypeDefinition{
		{Code: " Machine "}, {Code: "item"},
	}}
	next := modContentTemplateDefinition{EntryTypes: []modContentEntryTypeDefinition{{Code: "ITEM"}}}
	removed := removedModContentEntryTypeCodes(current, next)
	if len(removed) != 1 || removed[0] != "machine" {
		t.Fatalf("removed entry types=%#v want=[machine]", removed)
	}
}

func TestDisabledEntryTypeIsReadableOnlyForExistingDetails(t *testing.T) {
	enabled := false
	entryTypes := []modContentEntryTypeDefinition{{Code: "machine", Enabled: &enabled}}
	if _, err := selectModContentEntryType(entryTypes, "machine", "minecraft.item", false, false); !errors.Is(err, errCatalogEditorReference) {
		t.Fatalf("disabled type remained selectable for a new detail: %v", err)
	}
	if _, err := selectModContentEntryType(entryTypes, "machine", "minecraft.item", false, true); err != nil {
		t.Fatalf("disabled type could not interpret an existing detail: %v", err)
	}
}
