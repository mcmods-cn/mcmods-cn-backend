package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func auditP2PublishedGlobalResource(t *testing.T, f test013Fixture, name string) catalogEditResult {
	t.Helper()
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "content.write")
	body := map[string]any{"kindCode": "minecraft.item", "canonicalId": "audit:" + name, "defaultLocale": "en-US", "localizations": []map[string]string{{"locale": "en-US", "name": name}}, "definition": map[string]any{}}
	result := decodeTEST022Data[catalogEditResult](t, f.require(t, f.editor, http.MethodPost, "/api/v1/catalog/resources", body, 202))
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+result.RevisionID, map[string]string{"status": "approved", "note": "Independent resource fixture"}, 200)
	return result
}

func TestAuditP2DGR001ResourceTranslationCurrentViewPermissionHTTPPostgresIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	resource := auditP2PublishedGlobalResource(t, f, "p2-translation-revocation")
	for _, token := range []string{f.editor, f.otherEditor, f.reviewer} {
		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[token], "content.translate")
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "global_resource.view")
	var entityID int64
	if err := f.db.QueryRow(f.ctx, `select id from catalog_entities where public_id=$1`, resource.ObjectPublicID).Scan(&entityID); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(contentTranslationTaskPayload{EntityID: entityID, PublicID: resource.ObjectPublicID, EntityType: "resource", SourceLocale: "en-US", SourceRevisionNo: 1, TargetLocale: "zh-CN"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `insert into ai_tasks(task_uid,task_type,provider,model,status,payload,result,created_by) values('p2-resource-translation',$1,'fixture','fixture','completed',$2,'{"items":[{"key":"name","text":"Synthetic translated resource"}]}',$3)`, aiTaskContentTranslation, payload, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/content/translations/p2-resource-translation"
	f.require(t, f.editor, http.MethodGet, path, nil, 200)
	f.require(t, "", http.MethodGet, path, nil, 401)
	f.require(t, f.otherEditor, http.MethodGet, path, nil, 403)
	// An independent reviewer may inspect somebody else's task, but still
	// needs the separate resource-view permission of this entity type.
	f.require(t, f.reviewer, http.MethodGet, path, nil, 404)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "global_resource.view")
	server := f.origin.Config.Handler.(*Server)
	if err = server.refreshPermissionVersion(f.ctx, f.userIDs[f.reviewer]); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.reviewer, http.MethodGet, path, nil, 200)
	if _, err = f.db.Exec(f.ctx, `update user_permissions set allow=false where user_id=$1 and permission_id=(select id from permissions where code='global_resource.view')`, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	if err = server.refreshPermissionVersion(f.ctx, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodGet, path, nil, 404)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "global_resource.view")
	if err = server.refreshPermissionVersion(f.ctx, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodGet, path, nil, 200)
	if _, err = f.db.Exec(f.ctx, `update catalog_entities set status='archived',archived_at=now() where id=$1`, entityID); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{f.editor, f.reviewer} {
		f.require(t, token, http.MethodGet, path, nil, 404)
	}
	if _, err = f.db.Exec(f.ctx, `update catalog_entities set status='active',archived_at=null where id=$1`, entityID); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodGet, path, nil, 200)
	var state string
	if err = f.db.QueryRow(f.ctx, `select status from ai_tasks where task_uid='p2-resource-translation'`).Scan(&state); err != nil || state != "completed" {
		t.Fatalf("visibility reads changed task state=%s err=%v", state, err)
	}
}

func TestAuditP2REM001PublicApplicationIndependentApprovalAndHiddenZeroWritesHTTPPostgresIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "project.editor.apply")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "project.editor.review")
	postID, postPublicID, _ := createTEST043ApprovedCommunityPost(t, f.ctx, f.db, f.userIDs[f.editor])
	path := "/api/v1/projects/community_post/" + postPublicID + "/editor-applications"
	counts := func() [4]int64 {
		t.Helper()
		var result [4]int64
		for i, table := range []string{"project_editor_applications", "project_editor_assignments", "permission_audit_logs", "nats_outbox"} {
			if err := f.db.QueryRow(f.ctx, `select count(*) from `+table).Scan(&result[i]); err != nil {
				t.Fatal(err)
			}
		}
		return result
	}
	if _, err := f.db.Exec(f.ctx, `update community_posts set status='deleted' where id=$1`, postID); err != nil {
		t.Fatal(err)
	}
	before := counts()
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		f.require(t, f.otherEditor, method, path, map[string]string{"proofMarkdown": "Ordinary proof"}, 404)
	}
	if after := counts(); after != before {
		t.Fatalf("hidden target requests changed application/assignment/audit/notification intents: %v -> %v", before, after)
	}
	if _, err := f.db.Exec(f.ctx, `update community_posts set status='active' where id=$1`, postID); err != nil {
		t.Fatal(err)
	}
	application := decodeTEST022Data[projectEditorApplicationResponse](t, f.require(t, f.otherEditor, http.MethodPost, path, map[string]string{"proofMarkdown": "Ordinary proof"}, 201))
	f.require(t, f.otherEditor, http.MethodPatch, "/api/v1/admin/project-editor-applications/"+application.ID, map[string]string{"status": "approved"}, 403)
	f.require(t, f.denied, http.MethodPatch, "/api/v1/admin/project-editor-applications/"+application.ID, map[string]string{"status": "approved"}, 403)
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/admin/project-editor-applications/"+application.ID, map[string]string{"status": "approved", "note": "Independent legal approval"}, 200)
	var owner, grantedBy int64
	var target string
	if err := f.db.QueryRow(f.ctx, `select assignment.user_id,assignment.granted_by,route.public_id from project_editor_assignments assignment join public_routes route on route.id=assignment.target_route_id where assignment.status='active'`).Scan(&owner, &grantedBy, &target); err != nil {
		t.Fatal(err)
	}
	if owner != f.userIDs[f.otherEditor] || grantedBy != f.userIDs[f.reviewer] || target != postPublicID {
		t.Fatalf("approval scope owner=%d reviewer=%d target=%s", owner, grantedBy, target)
	}
	var exact, other bool
	// Effective project access is derived from the assignment view, separately
	// from global RBAC rules. Inspect its actual project scope.
	if err := f.db.QueryRow(f.ctx, `select exists(select 1 from effective_project_access where user_id=$1 and project_public_id=$2 and access_level='editor'),exists(select 1 from effective_project_access where user_id=$1 and project_public_id=$3)`, owner, postPublicID, f.modCode).Scan(&exact, &other); err != nil {
		t.Fatal(err)
	}
	if !exact || other {
		t.Fatalf("derived permission exact=%v unrelated-project=%v", exact, other)
	}
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/admin/project-editor-applications/"+application.ID, map[string]string{"status": "approved"}, 409)
}

func TestAuditP2RPH003PendingImportAndIndependentGlobalDirectoryHTTPPostgresIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	independent := auditP2PublishedGlobalResource(t, f.test013Fixture, "p2-independent-directory")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "global_resource.list")
	raw := func(name string) []byte {
		return auditFixGRZip(t, map[string]string{"registries/items.json": `{"registry":"items","entries":[{"id":"example:p2_directory","namespace":"example","names":{"en-US":"` + name + `"},"max_stack_size":64}]}`})
	}
	_, revision := f.imported(t, raw("P2 pending imported resource"))
	var globalID, importedID int64
	if err := f.db.QueryRow(f.ctx, `select id from catalog_entities where public_id=$1`, independent.ObjectPublicID).Scan(&globalID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `select entity_id from game_resources where canonical_id='example:p2_directory' and kind_code='minecraft.item'`).Scan(&importedID); err != nil {
		t.Fatal(err)
	}
	check := func(label string, expected int) {
		t.Helper()
		for _, indexed := range []bool{false, true} {
			if indexed {
				f.server.search = auditAccessStaleSearch(t, f.test013Fixture, []int64{globalID, importedID})
			} else {
				f.server.search = nil
			}
			page := decodeTEST022Data[struct {
				Total int              `json:"total"`
				Items []map[string]any `json:"items"`
			}](t, f.require(t, f.denied, http.MethodGet, "/api/v1/catalog/resources?q=p2", nil, 200))
			if page.Total != expected || len(page.Items) != expected {
				t.Fatalf("%s controlled_index=%v total=%d/items=%d want=%d", label, indexed, page.Total, len(page.Items), expected)
			}
			foundIndependent := false
			for _, item := range page.Items {
				if item["publicId"] == independent.ObjectPublicID {
					foundIndependent = true
				}
			}
			if !foundIndependent {
				t.Fatalf("%s hid the independent approved global resource", label)
			}
		}
	}
	check("pending import", 1)
	f.reviewGR(t, revision, "rejected")
	check("rejected import", 1)
	_, second := f.imported(t, raw("P2 approved imported resource"))
	check("second pending import", 1)
	f.reviewGR(t, second, "approved")
	check("approved source and import", 2)
	for _, state := range []string{"pending", "rejected", "approved"} {
		if _, err := f.db.Exec(f.ctx, `update mods set review_status=$2 where id=$1`, f.modID, state); err != nil {
			t.Fatal(err)
		}
		want := 1
		if state == "approved" {
			want = 2
		}
		check("current parent "+state, want)
	}
}

func TestAuditP2SL001RecipeParentArchiveRestoreAndIndependentPublicObjectHTTPPostgresIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "content.write")
	submit := func(path string, body map[string]any) catalogEditResult {
		t.Helper()
		body["defaultLocale"] = "en-US"
		body["localizations"] = []map[string]string{{"locale": "en-US", "name": "P2 independent manual recipe"}}
		result := decodeTEST022Data[catalogEditResult](t, f.require(t, f.editor, http.MethodPost, path, body, 202))
		if result.ReviewStatus != "pending" {
			t.Fatal("ordinary manual fixture bypassed review")
		}
		f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+result.RevisionID, map[string]string{"status": "approved", "note": "Independent manual recipe"}, 200)
		return result
	}
	parent := submit("/api/v1/recipe-types", map[string]any{"canonicalId": "audit:p2-standalone", "definition": map[string]any{}})
	template := submit("/api/v1/recipe-types/"+parent.ObjectPublicID+"/templates", map[string]any{"templateKey": "p2-manual-grid", "canvas": map[string]any{"width": 80, "height": 40, "imageScale": 1}, "definition": map[string]any{}, "slots": []map[string]any{{"slotKey": "output", "role": "output", "ordinal": 0, "rect": map[string]any{"x": 10, "y": 10, "width": 16, "height": 16}}}})
	recipe := submit("/api/v1/recipe-types/"+parent.ObjectPublicID+"/recipes", map[string]any{"canonicalSourceId": "audit:p2-manual", "templatePublicId": template.ObjectPublicID, "applicableVersionIds": []string{"1.20.1"}, "definition": map[string]any{}, "bindings": map[string]any{"output": map[string]any{"candidates": []map[string]any{{"rawResourceId": "audit:p2-unresolved", "kindCode": "minecraft.item", "amount": 1}}}}})
	paths := []string{"/api/v1/public-links/" + recipe.ObjectPublicID, "/api/v1/catalog/recipes/" + recipe.ObjectPublicID, "/api/v1/catalog/recipes/" + recipe.ObjectPublicID + "/render", "/api/v1/recipe-templates/" + template.ObjectPublicID, "/api/v1/recipe-types/" + parent.ObjectPublicID + "/templates", "/api/v1/recipe-types/" + parent.ObjectPublicID + "/recipes", "/api/v1/recipe-types/" + parent.ObjectPublicID + "/catalog"}
	check := func(want int) {
		t.Helper()
		for _, path := range paths {
			for _, token := range []string{"", f.editor, f.otherEditor, f.reviewer} {
				f.require(t, token, http.MethodGet, path, nil, want)
			}
		}
	}
	check(200)
	// A genuinely independent global manual object does not inherit an
	// unrelated Mod's unpublished state merely because its author edits it.
	if _, err := f.db.Exec(f.ctx, `update mods set review_status='pending' where id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	check(200)
	if _, err := f.db.Exec(f.ctx, `update catalog_entities set status='archived',archived_at=now() where public_id=$1`, parent.ObjectPublicID); err != nil {
		t.Fatal(err)
	}
	check(404)
	if _, err := f.db.Exec(f.ctx, `update catalog_entities set status='active',archived_at=null where public_id=$1`, parent.ObjectPublicID); err != nil {
		t.Fatal(err)
	}
	check(200)
	// Exercise the real child archive transition as well: submission remains
	// public until independent approval, which advances the dataset watermark
	// and removes the child from previously warmed catalog projections.
	var beforeVersion, afterVersion int64
	if err := f.db.QueryRow(f.ctx, `select version from catalog_dataset_state where singleton`).Scan(&beforeVersion); err != nil {
		t.Fatal(err)
	}
	deletion := decodeTEST022Data[catalogEditResult](t, f.require(t, f.editor, http.MethodDelete, "/api/v1/catalog/recipes/"+recipe.ObjectPublicID, map[string]any{"baseRevisionId": recipe.RevisionID, "reason": "Independent child archive"}, 202))
	f.require(t, "", http.MethodGet, "/api/v1/public-links/"+recipe.ObjectPublicID, nil, 200)
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+deletion.RevisionID, map[string]string{"status": "approved", "note": "Independent archive approval"}, 200)
	for _, path := range paths[:3] {
		f.require(t, "", http.MethodGet, path, nil, 404)
	}
	page := decodeTEST022Data[struct {
		Total   int               `json:"total"`
		Recipes []json.RawMessage `json:"recipes"`
	}](t, f.require(t, "", http.MethodGet, "/api/v1/recipe-types/"+parent.ObjectPublicID+"/catalog", nil, 200))
	if page.Total != 0 || len(page.Recipes) != 0 {
		t.Fatalf("approved child archive remained in warm catalog total=%d/items=%d", page.Total, len(page.Recipes))
	}
	if err := f.db.QueryRow(f.ctx, `select version from catalog_dataset_state where singleton`).Scan(&afterVersion); err != nil || afterVersion <= beforeVersion {
		t.Fatalf("archive dataset watermark %d -> %d err=%v", beforeVersion, afterVersion, err)
	}
}

func TestAuditP2SL001SkinCurrentStateAndRoleMatrixHTTPPostgresIntegration(t *testing.T) {
	f := newTEST039Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "admin.*")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "project.editor.apply")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "project.editor.review")
	file := f.upload(t, f.editor, "p2-shortlink-skin.png", test039PNG(t, 64, 64, 101))
	skin := f.asset(t, f.editor, file, "P2 hidden skin title", "public")
	f.review(t, skin.PublicID, "approved")
	applicationPath := "/api/v1/projects/skin/" + skin.PublicID + "/editor-applications"
	application := decodeTEST022Data[projectEditorApplicationResponse](t, f.require(t, f.otherEditor, http.MethodPost, applicationPath, map[string]string{"proofMarkdown": "Synthetic legal skin application"}, 201))
	for _, state := range []struct {
		visibility, review, status string
		want                       int
	}{
		{"public", "approved", "active", 200},
		{"private", "approved", "active", 404},
		{"public", "pending", "active", 404},
		{"public", "rejected", "active", 404},
		{"public", "approved", "deleted", 404},
		{"public", "approved", "active", 200},
	} {
		if _, err := f.db.Exec(f.ctx, `update skin_assets set visibility=$2,review_status=$3,status=$4 where public_id=$1`, skin.PublicID, state.visibility, state.review, state.status); err != nil {
			t.Fatal(err)
		}
		for _, token := range []string{"", f.editor, f.otherEditor, f.reviewer, f.denied} {
			raw := f.require(t, token, http.MethodGet, "/api/v1/public-links/"+skin.PublicID, nil, state.want)
			if state.want == 404 {
				var problem struct {
					Code string `json:"code"`
				}
				if err := json.Unmarshal(raw, &problem); err != nil || problem.Code != "HTTP_404" {
					t.Fatalf("hidden skin link did not return exact 404 contract: %s", raw)
				}
			}
		}
		if state.visibility == "private" && state.status == "active" && state.review == "approved" {
			counts := func() [3]int64 {
				t.Helper()
				var n [3]int64
				for i, table := range []string{"project_editor_applications", "project_editor_assignments", "nats_outbox"} {
					if err := f.db.QueryRow(f.ctx, `select count(*) from `+table).Scan(&n[i]); err != nil {
						t.Fatal(err)
					}
				}
				return n
			}
			before := counts()
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				f.require(t, f.otherEditor, method, applicationPath, map[string]string{"proofMarkdown": "Private target proof"}, 404)
			}
			f.require(t, f.reviewer, http.MethodPatch, "/api/v1/admin/project-editor-applications/"+application.ID, map[string]string{"status": "approved"}, 409)
			if after := counts(); after != before {
				t.Fatalf("private skin resolution/application/review changed rows %v -> %v", before, after)
			}
			f.require(t, f.editor, http.MethodGet, "/api/v1/skins/"+skin.PublicID, nil, 200)
			f.require(t, f.otherEditor, http.MethodGet, "/api/v1/skins/"+skin.PublicID, nil, 404)
		}
	}
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/admin/project-editor-applications/"+application.ID, map[string]string{"status": "approved", "note": "Independent restored public skin"}, 200)
	var grants int
	if err := f.db.QueryRow(f.ctx, `select count(*) from project_editor_assignments assignment join public_routes route on route.id=assignment.target_route_id where route.public_id=$1 and assignment.user_id=$2 and assignment.status='active'`, skin.PublicID, f.userIDs[f.otherEditor]).Scan(&grants); err != nil || grants != 1 {
		t.Fatalf("restored legal skin grants=%d err=%v", grants, err)
	}
}
