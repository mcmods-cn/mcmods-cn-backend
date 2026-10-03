package httpapi

import (
	"bytes"
	"net/http"
	"testing"
)

func TestOCT02CParentReferencesDoNotExposeUnpublishedProjectIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.create.*")
	parent := test014Create(t, f, test014Snapshot(f, "plugin", "oct02-private-parent", "Private parent metadata", "1.21.1"))
	childSnapshot := test014Snapshot(f, "addon", "oct02-public-child", "Public child", "1.21.1")
	child := test014Create(t, f, childSnapshot)
	test014Approve(t, f, child.SubmissionRevisionID, f.editor)
	var parentID int64
	if err := f.db.QueryRow(f.ctx, `select id from simple_projects where public_id=$1`, parent.PublicID).Scan(&parentID); err != nil {
		t.Fatal(err)
	}
	// Retain a historical reference to a pending parent: output authorization
	// must remain safe even if earlier writers accepted this association.
	if _, err := f.db.Exec(f.ctx, `update simple_project_parent_refs set target_type='plugin',target_id=$2
		where project_id=(select id from simple_projects where public_id=$1)`, child.PublicID, parentID); err != nil {
		t.Fatal(err)
	}
	f.require(t, "", http.MethodGet, "/api/v1/content-projects/plugin/"+parent.SiteID, nil, http.StatusNotFound)
	for _, path := range []string{
		"/api/v1/content-projects/addon/" + child.SiteID,
		"/api/v1/content-projects/addon?sort=name",
		"/api/v1/content-projects/addon/facets/parents",
	} {
		raw := f.require(t, "", http.MethodGet, path, nil, http.StatusOK)
		if bytes.Contains(raw, []byte("Private parent metadata")) || bytes.Contains(raw, []byte(parent.SiteID)) || bytes.Contains(raw, []byte(parent.PublicID)) {
			t.Fatalf("public parent projection exposed unpublished metadata at %s: %s", path, raw)
		}
	}
	childSnapshot.SiteID = "oct02-rejected-child"
	childSnapshot.ParentProjects = []simpleProjectParent{{Type: "plugin", PublicID: parent.PublicID}}
	f.require(t, f.editor, http.MethodPost, "/api/v1/content-projects/addon", childSnapshot, http.StatusBadRequest)
}
