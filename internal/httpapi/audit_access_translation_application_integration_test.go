package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestAuditDGR001TranslationResultsRecheckCurrentSourceFullHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "content.translate", "notification.translate")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "content.translate")
	if _, err := f.db.Exec(f.ctx, `insert into content_subjects(subject_type,subject_id,default_locale) values('mod',$1,'en-US') on conflict do nothing`, f.modID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into content_localizations(subject_type,subject_id,locale,name,provenance,review_status) values('mod',$1,'en-US','Synthetic source','human','approved') on conflict do nothing`, f.modID); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(contentTranslationTaskPayload{EntityID: f.modID, PublicID: f.modCode, EntityType: "mod", SourceLocale: "en-US", SourceRevisionNo: 1, TargetLocale: "zh-CN"})
	if _, err := f.db.Exec(f.ctx, `insert into ai_tasks(task_uid,task_type,provider,model,status,payload,result,created_by) values('audit-catalog-visible',$1,'fixture','fixture','completed',$2,'{"items":[{"key":"name","text":"Synthetic translated name"}]}',$3)`, aiTaskContentTranslation, payload, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/content/translations/audit-catalog-visible"
	f.require(t, f.editor, http.MethodGet, path, nil, 200)
	f.require(t, f.otherEditor, http.MethodGet, path, nil, 403)
	if _, err := f.db.Exec(f.ctx, `update mods set review_status='rejected' where id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodGet, path, nil, 404)
	if _, err := f.db.Exec(f.ctx, `update mods set review_status='approved' where id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodGet, path, nil, 200)
	postID, _, revisionID := createTEST043ApprovedCommunityPost(t, f.ctx, f.db, f.userIDs[f.editor])
	communityPayload, _ := json.Marshal(communityPostTranslationTaskPayload{PostInternalID: postID, TargetLocale: "zh-CN", SourceRevision: revisionID})
	if _, err := f.db.Exec(f.ctx, `insert into ai_tasks(task_uid,task_type,provider,model,status,payload,created_by) values('audit-community-visible',$1,'fixture','fixture','completed',$2,$3)`, aiTaskContentTranslation, communityPayload, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into community_post_translations(post_id,locale,source_revision_id,title,body_markdown) values($1,'zh-CN',$2,'Synthetic translated title','Synthetic translated body')`, postID, revisionID); err != nil {
		t.Fatal(err)
	}
	path = "/api/v1/community/translations/audit-community-visible"
	f.require(t, f.editor, http.MethodGet, path, nil, 200)
	f.require(t, f.otherEditor, http.MethodGet, path, nil, 404)
	if _, err := f.db.Exec(f.ctx, `update community_posts set status='deleted' where id=$1`, postID); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodGet, path, nil, 404)
	if _, err := f.db.Exec(f.ctx, `update community_posts set status='active' where id=$1`, postID); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodGet, path, nil, 200)
	const marker = "synthetic worker diagnostic"
	if _, err := f.db.Exec(f.ctx, `insert into ai_tasks(task_uid,task_type,provider,model,status,payload,created_by)
		values('audit-notification-visible',$1,'fixture','fixture','failed','{}',$2)`, aiTaskNotificationTranslation, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	for scope, task := range map[string]string{"content": "audit-catalog-visible", "community": "audit-community-visible", "notifications": "audit-notification-visible"} {
		if _, err := f.db.Exec(f.ctx, `update ai_tasks set status='failed',error=$2 where task_uid=$1`, task, marker); err != nil {
			t.Fatal(err)
		}
		raw := f.require(t, f.editor, http.MethodGet, "/api/v1/"+scope+"/translations/"+task, nil, http.StatusOK)
		result := decodeTEST022Data[struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		}](t, raw)
		if result.Status != "failed" || result.Error == "" || bytes.Contains(raw, []byte(marker)) {
			t.Errorf("%s did not return a safe, actionable failed result: %s", scope, raw)
		}
	}

	const catalogTask = "audit-catalog-visible"
	if _, err := f.db.Exec(f.ctx, `update ai_tasks set created_by=null where task_uid=$1`, catalogTask); err != nil {
		t.Fatal(err)
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "content.translate", "content.review")
	f.require(t, f.reviewer, http.MethodGet, "/api/v1/content/translations/"+catalogTask, nil, http.StatusOK)
	f.require(t, f.editor, http.MethodGet, "/api/v1/content/translations/"+catalogTask, nil, http.StatusForbidden)

}

func TestAuditREM001HiddenEditorApplicationTargetsFullHTTPIntegration(t *testing.T) {
	f := newTEST039Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "project.editor.apply")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "project.editor.review")
	file := f.upload(t, f.editor, "editor-application.png", test039PNG(t, 64, 64, 90))
	skin := f.asset(t, f.editor, file, "Synthetic confidential skin", "private")
	f.review(t, skin.PublicID, "approved")
	path := "/api/v1/projects/skin/" + skin.PublicID + "/editor-applications"
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		f.require(t, f.otherEditor, method, path, map[string]any{"proofMarkdown": "Synthetic proof"}, 404)
	}
	postID, postPublicID, _ := createTEST043ApprovedCommunityPost(t, f.ctx, f.db, f.userIDs[f.editor])
	if _, err := f.db.Exec(f.ctx, `update community_posts set status='deleted' where id=$1`, postID); err != nil {
		t.Fatal(err)
	}
	postPath := "/api/v1/projects/community_post/" + postPublicID + "/editor-applications"
	f.require(t, f.otherEditor, http.MethodGet, postPath, nil, 404)
	f.require(t, f.otherEditor, http.MethodPost, postPath, map[string]any{"proofMarkdown": "Synthetic proof"}, 404)
	var count int
	if err := f.db.QueryRow(f.ctx, `select count(*) from project_editor_applications`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("hidden applications=%d err=%v", count, err)
	}
	if _, err := f.db.Exec(f.ctx, `update community_posts set status='active' where id=$1`, postID); err != nil {
		t.Fatal(err)
	}
	result := decodeTEST022Data[projectEditorApplicationResponse](t, f.require(t, f.otherEditor, http.MethodPost, postPath, map[string]any{"proofMarkdown": "Synthetic proof"}, 201))
	if _, err := f.db.Exec(f.ctx, `update community_posts set status='deleted' where id=$1`, postID); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/admin/project-editor-applications/"+result.ID, map[string]any{"status": "approved"}, 409)
	if err := f.db.QueryRow(f.ctx, `select count(*) from project_editor_assignments`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("hidden target assignments=%d err=%v", count, err)
	}
}
