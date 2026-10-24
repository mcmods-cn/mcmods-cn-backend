package httpapi

import (
	"net/http"
	"testing"
)

func TestAuditRPH002HiddenParentHistoryFullHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	var revisionID int64
	var revisionPublicID string
	if err := f.db.QueryRow(f.ctx, `insert into content_revisions(entity_type,entity_id,aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash,created_by,source) values('mod',$1,'mod',$2,1,'{"siteId":"test013-mod","primaryName":"Synthetic confidential history","bodyMarkdown":"Synthetic historical body"}','synthetic-history-hash',$3,'audit') returning id,public_id`, f.modID, f.modCode, f.userIDs[f.editor]).Scan(&revisionID, &revisionPublicID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into change_requests(entity_type,entity_id,aggregate_type,aggregate_key,proposed_revision_id,status,reason,submitted_by) values('mod',$1,'mod',$2,$3,'approved','Synthetic old reason',$4)`, f.modID, f.modCode, revisionID, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	paths := []string{"/api/v1/mods/test013-mod/revisions", "/api/v1/mods/test013-mod/revisions/compare?before=" + revisionPublicID + "&after=" + revisionPublicID}
	// Author-scoped reviewer permissions do not grant a different project's history.
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "project.review."+f.otherModCode)
	for _, state := range []string{"approved", "pending", "rejected"} {
		if _, err := f.db.Exec(f.ctx, `update mods set published_revision_id=$1,review_status=$2 where id=$3`, revisionID, state, f.modID); err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			for _, token := range []string{"", f.otherEditor, f.denied} {
				want := 200
				if state != "approved" {
					want = 404
				}
				f.require(t, token, http.MethodGet, path, nil, want)
			}
			f.require(t, f.editor, http.MethodGet, path, nil, 200)
			f.require(t, f.reviewer, http.MethodGet, path, nil, 200)
		}
	}
}
