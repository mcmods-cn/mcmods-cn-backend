package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestItemBlockSystemCategoriesHaveOneProductionAuthority(t *testing.T) {
	importSource, err := os.ReadFile("mod_export_content_sync.go")
	if err != nil {
		t.Fatal(err)
	}
	content := string(importSource)
	if !strings.Contains(content, "ensureItemBlockSystemCategoriesTx(") {
		t.Error("import synchronization does not reuse the item/block category transaction helper")
	}
	for _, duplicate := range []string{
		"create imported item and block categories",
		"localize imported item and block categories",
		"('blocks'::text,'en-US'::text,'Blocks'::text)",
		"('items','en-US','Items')",
	} {
		if strings.Contains(content, duplicate) {
			t.Errorf("import synchronization still duplicates the system-category authority: %q", duplicate)
		}
	}
}
