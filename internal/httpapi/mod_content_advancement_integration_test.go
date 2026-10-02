package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestCreateAndArrangeAdvancementIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	version := f.version(t, f.editor, "test013-mod", "1.21.1 / NeoForge")
	section := f.section(t, version.PublicID, f.builtin(t, "advancement"))
	resource := f.resource(t, version.PublicID, section.PublicID, "test013:advancement", "minecraft.advancement", "advancement",
		map[string]any{"display": map[string]any{"x": 0, "y": 0}})
	// Neither no-review nor a direct status UPDATE can stand in for publication.
	before := f.facts(t)
	f.require(t, f.editor, http.MethodPatch, "/api/v1/content-revisions/"+resource.RevisionID, map[string]any{"status": "approved"}, http.StatusForbidden)
	f.unchanged(t, before)
	f.require(t, "", http.MethodGet, "/api/v1/mods/test013-mod/content-resources/"+resource.PublicID, nil, http.StatusNotFound)
	f.require(t, f.reviewer, http.MethodGet, "/api/v1/mods/test013-mod/content-resources/"+resource.PublicID, nil, http.StatusOK)
	f.review(t, resource, f.editor, "approved")
	before = f.facts(t)
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+resource.RevisionID, map[string]any{"status": "approved"}, http.StatusConflict)
	f.unchanged(t, before)
	f.require(t, "", http.MethodGet, "/api/v1/mods/test013-mod/content-resources/"+resource.PublicID, nil, http.StatusOK)
	var sectionBase string
	if err := f.db.QueryRow(f.ctx, `select revision.public_id from mod_content_sections section
		join content_revisions revision on revision.id=section.published_revision_id where section.public_id=$1`, section.PublicID).Scan(&sectionBase); err != nil {
		t.Fatal(err)
	}
	patch := modContentLayoutPatch{VersionPublicID: version.PublicID, RootSectionPublicID: section.PublicID, DisplayMode: "compact", BaseRevisionID: &sectionBase,
		Resources: []modContentLayoutResourceEdit{{ResourcePublicID: resource.PublicID, SectionPublicID: section.PublicID, Ordinal: 0,
			Advancement: &modContentAdvancementLayoutEdit{GroupID: "advancement:test", X: 2, Y: 3}}}, Reason: "advancement layout regression"}
	path := "/api/v1/mods/test013-mod/content-sections/" + section.PublicID + "/layout"
	first := f.submit(t, f.editor, http.MethodPatch, path, patch, http.StatusOK)
	f.review(t, first, f.editor, "approved")
	detailRevision, definition := test013DetailRevision(t, f, resource.PublicID, version.PublicID)
	display, ok := definition["display"].(map[string]any)
	if !ok || display["x"] != float64(2) || display["y"] != float64(3) {
		t.Fatalf("reviewed advancement coordinates not published: %v", definition)
	}
	patch.BaseRevisionID = &first.RevisionID
	unchanged := f.submit(t, f.editor, http.MethodPatch, path, patch, http.StatusOK)
	f.review(t, unchanged, f.editor, "approved")
	unchangedRevision, _ := test013DetailRevision(t, f, resource.PublicID, version.PublicID)
	if unchangedRevision != detailRevision {
		t.Fatalf("unchanged advancement detail revision changed from %d to %d", detailRevision, unchangedRevision)
	}
	public := f.require(t, "", http.MethodGet, "/api/v1/mods/test013-mod/content-resources/"+resource.PublicID, nil, http.StatusOK)
	var payload struct {
		Data struct {
			Details []struct {
				Version  string `json:"versionPublicId"`
				Section  string `json:"sectionPublicId"`
				Revision string `json:"publishedRevisionId"`
			} `json:"details"`
		} `json:"data"`
	}
	if err := json.Unmarshal(public, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data.Details) != 1 || payload.Data.Details[0].Version != version.PublicID || payload.Data.Details[0].Section != section.PublicID || payload.Data.Details[0].Revision == "" {
		t.Fatalf("public advancement disagrees with reviewed version/placement/revision: %s", public)
	}
}
