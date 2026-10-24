package httpapi

import (
	"bytes"
	"net/http"
	"net/url"
	"testing"
)

func auditGRR02PublishedTag(t *testing.T, f test013Fixture) catalogEditResult {
	t.Helper()
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "content.write")
	tag := decodeTEST022Data[catalogEditResult](t, f.require(t, f.editor, http.MethodPost, "/api/v1/tags", map[string]any{"registry": "minecraft.item", "canonicalId": "audit:localization_visibility", "defaultLocale": "en-US", "localizations": []map[string]string{{"locale": "en-US", "name": "APPROVED_ENGLISH_TITLE", "contentMarkdown": "APPROVED_ENGLISH_BODY"}}, "memberResourcePublicIds": []string{}, "reason": "Owned GR-R02 normal workflow fixture"}, 202))
	f.require(t, "", http.MethodGet, "/api/v1/tags/"+tag.ObjectPublicID, nil, 404)
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+tag.RevisionID, map[string]string{"status": "approved"}, 200)
	return tag
}
func auditGRR02PublicResponses(t *testing.T, f test013Fixture, tag string, forbidden ...string) {
	t.Helper()
	for _, path := range []string{"/api/v1/tags/" + tag + "?locale=zh-CN", "/api/v1/tags?locale=zh-CN", "/api/v1/content/" + tag + "?locale=zh-CN", "/api/v1/comment-targets/tag/" + tag + "/comments"} {
		raw := f.require(t, "", http.MethodGet, path, nil, 200)
		for _, marker := range forbidden {
			if bytes.Contains(raw, []byte(marker)) {
				t.Errorf("public response %s exposed synthetic nonapproved marker %s", path, marker)
			}
		}
	}
}

func TestAuditGRR02NormalLocalizationReviewDoesNotProduceNonapprovedPublicProjectionHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	tag := auditGRR02PublishedTag(t, f)
	type editResult struct {
		RevisionID   string `json:"revisionId"`
		ReviewStatus string `json:"reviewStatus"`
	}
	submit := func(name string) editResult {
		t.Helper()
		r := decodeTEST022Data[editResult](t, f.require(t, f.editor, http.MethodPut, "/api/v1/content/"+tag.ObjectPublicID, map[string]string{"locale": "zh-CN", "name": name, "contentMarkdown": name + "_BODY", "reason": "Owned normal localization review reachability"}, 200))
		if r.ReviewStatus != "pending" {
			t.Fatalf("ordinary editor bypassed review: %s", r.ReviewStatus)
		}
		return r
	}
	assertProjection := func(wantApproved int) {
		t.Helper()
		var approved, unapproved int
		if err := f.db.QueryRow(f.ctx, `select count(*) filter(where review_status='approved'),count(*) filter(where review_status<>'approved') from content_localizations where subject_type='tag' and subject_id=(select id from catalog_entities where public_id=$1)`, tag.ObjectPublicID).Scan(&approved, &unapproved); err != nil || approved != wantApproved || unapproved != 0 {
			t.Fatalf("normal projection approved=%d unapproved=%d expected approved=%d err=%v", approved, unapproved, wantApproved, err)
		}
	}
	first := submit("NORMAL_PENDING_ZH")
	assertProjection(1)
	auditGRR02PublicResponses(t, f, tag.ObjectPublicID, "NORMAL_PENDING_ZH")
	// The independent review queue retains the exact pending revision ID. This
	// public-reader hardening does not grant public access to proposed contents.
	queue := f.require(t, f.reviewer, http.MethodGet, "/api/v1/reviews/content?category=content_localization", nil, 200)
	if !bytes.Contains(queue, []byte(first.RevisionID)) {
		t.Fatal("independent reviewer lost pending localization")
	}
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+first.RevisionID, map[string]string{"status": "rejected"}, 200)
	assertProjection(1)
	auditGRR02PublicResponses(t, f, tag.ObjectPublicID, "NORMAL_PENDING_ZH")
	second := submit("NORMAL_APPROVED_ZH")
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+second.RevisionID, map[string]string{"status": "approved"}, 200)
	assertProjection(2)
	raw := f.require(t, "", http.MethodGet, "/api/v1/tags/"+tag.ObjectPublicID+"?locale=zh-CN", nil, 200)
	if !bytes.Contains(raw, []byte("NORMAL_APPROVED_ZH")) {
		t.Fatal("approved localization absent from public detail")
	}
	t.Log("Normal HTTP create/approve/edit/pending/reject/new edit/approve never persisted an unapproved localization projection. This is bounded counterevidence, not proof every writer is unreachable.")
}

func TestAuditGRR02SyntheticNonapprovedLocalizationProjectionIsFilteredHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	tag := auditGRR02PublishedTag(t, f)
	// Explicit abnormal-state injection. This is a reader-defense regression,
	// not evidence that a normal user can insert these rows through product APIs.
	if _, err := f.db.Exec(f.ctx, `insert into content_localizations(subject_type,subject_id,catalog_entity_id,locale,name,summary,content_markdown,provenance,review_status)
 select 'tag',id,id,locale,name,name||'_SUMMARY',name||'_BODY','human',fixture.status from catalog_entities cross join (values('zh-CN','ABNORMAL_PENDING_ZH','pending'),('fr-FR','ABNORMAL_REJECTED_FR','rejected')) fixture(locale,name,status) where public_id=$1`, tag.ObjectPublicID); err != nil {
		t.Fatal(err)
	}
	auditGRR02PublicResponses(t, f, tag.ObjectPublicID, "ABNORMAL_PENDING_ZH", "ABNORMAL_REJECTED_FR")
	for _, marker := range []string{"ABNORMAL_PENDING_ZH", "ABNORMAL_REJECTED_FR"} {
		type page struct {
			Total int   `json:"total"`
			Items []any `json:"items"`
		}
		result := decodeTEST022Data[page](t, f.require(t, "", http.MethodGet, "/api/v1/tags?q="+url.QueryEscape(marker), nil, 200))
		if result.Total != 0 || len(result.Items) != 0 {
			t.Errorf("public search revealed an unapproved localization: total=%d items=%d", result.Total, len(result.Items))
		}
	}
	var unapproved int
	if err := f.db.QueryRow(f.ctx, `select count(*) from content_localizations where subject_type='tag' and subject_id=(select id from catalog_entities where public_id=$1) and review_status<>'approved'`, tag.ObjectPublicID).Scan(&unapproved); err != nil || unapproved != 2 {
		t.Fatalf("reader defense changed injected audit facts: rows=%d err=%v", unapproved, err)
	}
}
