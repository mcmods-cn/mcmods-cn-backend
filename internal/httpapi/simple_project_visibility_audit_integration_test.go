package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/security"
)

// Every mutation runs inside a new, marker-owned database created by the
// existing isolated test helper. No shared or production rows are modified.
func TestSimpleProjectAssociationVisibilityAuditIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	server := &Server{db: pool, cfg: cfg}
	exec := func(t *testing.T, sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	var ownerID, outsiderID, addonID, creatorID, fileID int64
	var addonSlug string = "visibility_addon"
	for i, id := range []*int64{&ownerID, &outsiderID} {
		if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values($1,$2,'fixture') returning id`, fmt.Sprintf("assoc_owner_%d", i), fmt.Sprintf("assoc%d@example.invalid", i)).Scan(id); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("stale-crawler-lease-cannot-create-project", func(t *testing.T) {
		requestCtx := withSeedCrawlerLease(ctx, 1, "synthetic-stale-lease")
		requestCtx = context.WithValue(requestCtx, claimsContextKey, security.Claims{Subject: ownerID, PermissionRules: []security.PermissionRule{{Code: "project.create.datapack", Allow: true}}})
		body := `{"siteId":"synthetic_stale_crawler","defaultLocale":"en-US","localizations":[{"locale":"en-US","name":"Synthetic stale crawler"}],"minecraftVersions":["1.20.1"],"officialStatus":"active","sourceStatus":"open","license":"MIT","submissionMethod":"manual","links":[{"type":"official","url":"https://invalid.example/synthetic"}]}`
		request := httptest.NewRequest(http.MethodPost, "/api/v1/datapacks", strings.NewReader(body)).WithContext(requestCtx)
		response := httptest.NewRecorder()
		server.createSimpleProject(response, request, "datapack")
		if response.Code != http.StatusConflict {
			t.Fatalf("stale crawler creation status=%d body=%s", response.Code, response.Body.String())
		}
		var count int
		if err := pool.QueryRow(ctx, `select count(*) from simple_projects where slug='synthetic_stale_crawler'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("stale crawler created %d projects", count)
		}
	})
	if err := pool.QueryRow(ctx, `insert into simple_projects(project_type,slug,primary_name,review_status,submitted_by) values('addon',$1,'Visible Addon','approved',$2) returning id`, addonSlug, ownerID).Scan(&addonID); err != nil {
		t.Fatal(err)
	}
	type parentFixture struct {
		kind, slug string
		id         int64
		pending    bool
	}
	parents := []parentFixture{}
	for _, kind := range []string{"mod", "modpack", "plugin"} {
		for _, status := range []string{"pending", "approved"} {
			parent := parentFixture{kind: kind, slug: kind + "_" + status, pending: status == "pending"}
			var sql string
			switch kind {
			case "mod":
				sql = `insert into mods(project_code,slug,primary_name,review_status,submitted_by,icon_url) values(new_public_id(),$1,$2,$3,$4,'https://example.invalid/parent.png') returning id`
			case "modpack":
				sql = `insert into modpacks(slug,primary_name,review_status,submitted_by,icon_url) values($1,$2,$3,$4,'https://example.invalid/parent.png') returning id`
			default:
				sql = `insert into simple_projects(project_type,slug,primary_name,review_status,submitted_by,icon_url) values('plugin',$1,$2,$3,$4,'https://example.invalid/parent.png') returning id`
			}
			if err := pool.QueryRow(ctx, sql, parent.slug, "Synthetic "+parent.slug, status, ownerID).Scan(&parent.id); err != nil {
				t.Fatal(err)
			}
			exec(t, `insert into simple_project_parent_refs(project_id,target_type,target_id) values($1,$2,$3)`, addonID, kind, parent.id)
			parents = append(parents, parent)
		}
	}
	exec(t, `insert into simple_project_parent_refs(project_id,target_type,raw_identifier) values($1,'mod','external:unresolved')`, addonID)
	for _, viewer := range []struct {
		name     string
		id       int64
		expected int
	}{{"guest", 0, 4}, {"other-user", outsiderID, 4}, {"parent-owner", ownerID, 7}} {
		t.Run(viewer.name, func(t *testing.T) {
			item, err := server.simpleProjectBySiteID(ctx, "addon", addonSlug, viewer.id, false)
			if err != nil {
				t.Fatal(err)
			}
			if len(item.ParentProjects) != viewer.expected {
				t.Fatalf("visible parents=%d wanted=%d", len(item.ParentProjects), viewer.expected)
			}
			for _, parent := range item.ParentProjects {
				if strings.Contains(parent.SiteID, "pending") && viewer.id != ownerID {
					t.Fatalf("non-owner received unpublished parent metadata: %s", parent.Type)
				}
			}
		})
	}
	for _, parent := range parents {
		if !parent.pending {
			continue
		}
		t.Run("filter/"+parent.kind, func(t *testing.T) {
			for _, viewer := range []struct {
				id       int64
				expected int
			}{{0, 0}, {outsiderID, 0}, {ownerID, 1}} {
				empty := []string{}
				var count int
				err := pool.QueryRow(ctx, `select count(*) from simple_projects project `+simpleProjectCatalogFilter, "addon", viewer.id, "", empty, empty, empty, false, []int64{}, empty, empty, empty, empty, []string{parent.kind + ":" + parent.slug}, empty, empty, empty, 0, "any").Scan(&count)
				if err != nil {
					t.Fatal(err)
				}
				if count != viewer.expected {
					t.Fatalf("pending parent filter count=%d wanted=%d for viewer=%d", count, viewer.expected, viewer.id)
				}
			}
		})
	}
	t.Run("reject-foreign-pending-parent-write", func(t *testing.T) {
		for _, parent := range parents {
			if !parent.pending {
				continue
			}
			var publicID string
			if err := pool.QueryRow(ctx, `select public_id from public_routes where entity_type=$1 and internal_id=$2`, parent.kind, parent.id).Scan(&publicID); err != nil {
				t.Fatal(err)
			}
			for _, actor := range []struct {
				id      int64
				allowed bool
			}{{outsiderID, false}, {ownerID, true}} {
				tx, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				snapshot := simpleProjectSnapshot{ProjectType: "addon", ParentProjects: []simpleProjectParent{{Type: parent.kind, PublicID: publicID}}}
				err = replaceSimpleProjectAssociationsTx(ctx, tx, addonID, "addon", 0, actor.id, false, false, false, snapshot)
				if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
					t.Fatal(rollbackErr)
				}
				if (err == nil) != actor.allowed {
					t.Fatalf("parent %s binding allowed=%v wanted=%v", parent.kind, err == nil, actor.allowed)
				}
			}
		}
	})
	t.Run("authenticated-cache-privacy", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: ownerID}))
		request.SetPathValue("projectType", "addon")
		request.SetPathValue("siteId", addonSlug)
		recorder := httptest.NewRecorder()
		server.simpleProjectItem(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("authenticated detail status=%d", recorder.Code)
		}
		if recorder.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("viewer-specific response may enter shared cache")
		}
	})
	if err := pool.QueryRow(ctx, `insert into creators(kind,name,normalized_name,review_status) values('author','Fixture','fixture','approved') returning id`).Scan(&creatorID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into oss_files(bucket,object_key,original_name,content_type,size_bytes,uploader_id,status,scan_status) values('fixture','audit/parent.png','audit.png','image/png',1,$1,'active','clean') returning id`, ownerID).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	var galleryID string
	if err := pool.QueryRow(ctx, `insert into simple_project_gallery_images(project_id,oss_file_id) values($1,$2) returning public_id`, addonID, fileID).Scan(&galleryID); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"pending", "rejected", "clean", "trusted_generated"} {
		t.Run("gallery/"+status, func(t *testing.T) {
			exec(t, `update oss_files set scan_status=$2 where id=$1`, fileID, status)
			item, err := server.simpleProjectBySiteID(ctx, "addon", addonSlug, 0, false)
			if err != nil {
				t.Fatal(err)
			}
			trusted := status == "clean" || status == "trusted_generated"
			if (len(item.GalleryImages) > 0) != trusted {
				t.Fatalf("gallery visible=%v for scan=%s", len(item.GalleryImages) > 0, status)
			}
			if !trusted {
				request := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
				request.SetPathValue("projectType", "addon")
				request.SetPathValue("siteId", addonSlug)
				request.SetPathValue("publicId", galleryID)
				recorder := httptest.NewRecorder()
				server.simpleProjectGalleryImage(recorder, request)
				if recorder.Code != http.StatusNotFound {
					t.Fatalf("untrusted gallery route status=%d wanted404", recorder.Code)
				}
			}
		})
	}
	exec(t, `update oss_files set scan_status='clean' where id=$1`, fileID)
	t.Run("gallery/reject-svg", func(t *testing.T) {
		exec(t, `update oss_files set content_type='image/svg+xml' where id=$1`, fileID)
		item, err := server.simpleProjectBySiteID(ctx, "addon", addonSlug, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(item.GalleryImages) != 0 {
			t.Fatal("non-raster gallery file remains visible")
		}
		request := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
		request.SetPathValue("projectType", "addon")
		request.SetPathValue("siteId", addonSlug)
		request.SetPathValue("publicId", galleryID)
		recorder := httptest.NewRecorder()
		server.simpleProjectGalleryImage(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("SVG route status=%d wanted404", recorder.Code)
		}
		exec(t, `update oss_files set content_type='image/png' where id=$1`, fileID)
	})
	exec(t, `create function audit_association_stream_failure() returns text language plpgsql volatile as $$ begin raise exception 'synthetic association stream failure'; end $$`)
	failureViews := map[string]string{
		"localizations": fmt.Sprintf(`create view %%s.simple_project_localizations as select %d::bigint project_id,'en-US'::text locale,public.audit_association_stream_failure() name,''::text summary,''::text body_markdown`, addonID),
		"links":         fmt.Sprintf(`create view %%s.simple_project_links as select 1::bigint id,%d::bigint project_id,'wiki'::text link_type,'https://example.invalid'::text url,public.audit_association_stream_failure() note,0::int display_order`, addonID),
		"authors":       fmt.Sprintf(`create view %%s.content_creator_bindings as select 1::bigint id,%d::bigint subject_id,'addon'::text subject_type,%d::bigint creator_id,null::bigint role_id,public.audit_association_stream_failure() role_snapshot,'approved'::text status,0::int display_order`, addonID, creatorID),
		"gallery":       fmt.Sprintf(`create view %%s.simple_project_gallery_images as select 1::bigint id,%d::bigint project_id,%d::bigint oss_file_id,public.audit_association_stream_failure() public_id,0::int display_order`, addonID, fileID),
	}
	for name, view := range failureViews {
		t.Run("stream-error/"+name, func(t *testing.T) {
			schema := pgx.Identifier{"audit_fail_" + name}.Sanitize()
			exec(t, "create schema "+schema)
			exec(t, fmt.Sprintf(view, schema))
			parsed := pool.Config().Copy()
			parsed.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			failurePool, err := pgxpool.NewWithConfig(ctx, parsed)
			if err != nil {
				t.Fatal(err)
			}
			defer failurePool.Close()
			failureServer := &Server{db: failurePool, cfg: cfg}
			_, err = failureServer.simpleProjectBySiteID(ctx, "addon", addonSlug, 0, false)
			if err == nil || !strings.Contains(err.Error(), "synthetic association stream failure") {
				t.Fatalf("row stream failure was lost: %v", err)
			}
			request := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
			request.SetPathValue("projectType", "addon")
			request.SetPathValue("siteId", addonSlug)
			recorder := httptest.NewRecorder()
			failureServer.simpleProjectItem(recorder, request)
			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("database failure status=%d wanted500", recorder.Code)
			}
		})
	}
}
