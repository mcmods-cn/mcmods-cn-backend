package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

func TestTEST015ServerConfiguredTextLimitsAreWritableIntegration(t *testing.T) {
	f := newTEST015ServerFixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "admin.config.write")
	settings := defaultServerCatalogSettings()
	settings.NameMaxLength = 160
	settings.SummaryMaxLength = 1000
	f.require(t, f.reviewer, http.MethodPut, "/api/v1/admin/server-settings", settings, http.StatusOK)

	proof := f.proof(t, f.editor, "active", "clean", 100)
	submission := test015Submission("owned-max-text.invalid:25565", proof)
	submission.Name = strings.Repeat("山", settings.NameMaxLength+1)
	f.require(t, f.editor, http.MethodPost, "/api/v1/servers", submission, http.StatusBadRequest)
	submission.Name = strings.Repeat("山", settings.NameMaxLength)
	submission.ShortDescription = strings.Repeat("海", settings.SummaryMaxLength+1)
	f.require(t, f.editor, http.MethodPost, "/api/v1/servers", submission, http.StatusBadRequest)
	submission.ShortDescription = strings.Repeat("海", settings.SummaryMaxLength)
	_, publicID := f.create(t, f.editor, submission)

	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/admin/server-reviews/"+publicID,
		map[string]string{"status": "approved"}, http.StatusOK)
	path := "/api/v1/servers/" + publicID
	read := func(name, summary string) {
		t.Helper()
		result := decodeTEST022Data[minecraftServerDetail](t, f.require(t, "", http.MethodGet, path, nil, http.StatusOK))
		if result.Name != name || result.ShortDescription != summary {
			t.Fatal("public detail did not preserve the configured length and content")
		}
	}
	read(submission.Name, submission.ShortDescription)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.edit."+publicID)
	update := updateMinecraftServerRequest{
		Name:             strings.Repeat("川", settings.NameMaxLength),
		ShortDescription: strings.Repeat("林", settings.SummaryMaxLength),
		BodyMarkdown:     submission.BodyMarkdown, MinecraftVersions: submission.MinecraftVersions,
		Languages: submission.Languages, PrimaryTag: submission.PrimaryTag,
	}
	f.require(t, f.editor, http.MethodPatch, path, update, http.StatusOK)
	read(update.Name, update.ShortDescription)
}
