package httpapi

import (
	"net/http"
	"testing"
)

func TestReviewedResourceMoveKeepsOtherResourceFamiliesIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	version := f.version(t, f.editor, "test013-mod", "1.21.1 / NeoForge")
	items := f.section(t, version.PublicID, f.builtin(t, "item_block"))
	fluids := f.section(t, version.PublicID, f.builtin(t, "fluid"))
	const projectPath = "/api/v1/mods/test013-mod"
	patch := modContentLayoutPatch{
		VersionPublicID: version.PublicID, RootSectionPublicID: fluids.PublicID,
		DisplayMode: "compact", BaseRevisionID: &fluids.RevisionID,
		Categories: &[]modContentLayoutCategoryEdit{{
			PublicID: "new-fluids", ParentPublicID: fluids.PublicID, DefaultLocale: "en-US",
			Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "Refined fluids"}},
		}},
	}
	layout := f.submit(t, f.editor, http.MethodPatch, projectPath+"/content-sections/"+fluids.PublicID+"/layout", patch, http.StatusOK)
	f.review(t, layout, f.editor, "approved")
	var target string
	if err := f.db.QueryRow(f.ctx, `select child.public_id from mod_content_sections child
		join mod_content_sections parent on child.parent_id=parent.id
		where parent.public_id=$1 and child.status='active'`, fluids.PublicID).Scan(&target); err != nil {
		t.Fatal(err)
	}

	const namedResource = "test013:mercury"
	fluid := f.resource(t, version.PublicID, fluids.PublicID, namedResource, "minecraft.fluid", "default", map[string]any{})
	f.review(t, fluid, f.editor, "approved")
	item := f.resource(t, version.PublicID, items.PublicID, namedResource, "minecraft.item", "default", map[string]any{})
	f.review(t, item, f.editor, "approved")

	assertPlacement := func(resource, wantSection string) {
		t.Helper()
		raw := f.require(t, "", http.MethodGet, projectPath+"/content-resources/"+resource+"?version="+version.PublicID, nil, http.StatusOK)
		result := decodeTEST022Data[struct {
			Details []struct {
				SectionPublicID string `json:"sectionPublicId"`
			}
		}](t, raw)
		if len(result.Details) != 1 || result.Details[0].SectionPublicID != wantSection {
			t.Fatalf("resource %s expected to remain in section %s, got %+v", resource, wantSection, result.Details)
		}
	}
	assertPlacement(item.PublicID, items.PublicID)
	assertPlacement(fluid.PublicID, fluids.PublicID)
	move := modContentResourceEdit{
		ResourcePublicID: fluid.PublicID, VersionPublicID: version.PublicID, SectionPublicID: &target,
		EntryTypeCode: "default", DefaultLocale: "en-US", BaseRevisionID: &fluid.RevisionID,
		Definition: map[string]any{}, Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "TEST013 resource"}},
	}
	updated := f.submit(t, f.editor, http.MethodPut, projectPath+"/content-resources/"+fluid.PublicID, move, http.StatusOK)
	f.review(t, updated, f.editor, "approved")
	assertPlacement(fluid.PublicID, target)
	assertPlacement(item.PublicID, items.PublicID)
}
