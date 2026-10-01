package httpapi

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/security"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestModContentCannotExposeUnapprovedParentIntegration(t *testing.T) {
	url := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("requires dedicated migrated PostgreSQL via MCMODS_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	key := "apia_content_" + randomHex(8)
	var userID, modID, versionID, sectionID, resourceID int64
	var versionPublicID, sectionPublicID, resourcePublicID string
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		for _, q := range []struct {
			sql string
			id  int64
		}{{`delete from mods where id=$1`, modID}, {`delete from catalog_entities where id=$1`, resourceID}, {`delete from users where id=$1`, userID}} {
			if q.id > 0 {
				if _, err := pool.Exec(cleanup, q.sql, q.id); err != nil {
					t.Errorf("allocated content fixture cleanup: %v", err)
				}
			}
		}
	}()
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash) values($1,$2,'fixture') returning id`, key, key+"@example.test").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,submitted_by,review_status) values(new_public_id(),$1,'Synthetic unpublished',$2,'pending') returning id`, key, userID).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,minecraft_versions,loaders,status) values($1,'Synthetic hidden version',array['1.21.1'],array['neoforge'],'active') returning id,public_id`, modID).Scan(&versionID, &versionPublicID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mod_content_sections(mod_id,version_id,template_id,default_locale,display_mode,status) select $1,$2,id,'en-US','compact','active' from mod_content_templates where builtin order by id limit 1 returning id,public_id`, modID, versionID).Scan(&sectionID, &sectionPublicID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into catalog_entities(identity_key,public_id,entity_type,status) values($1,new_public_id(),'resource','active') returning id,public_id`, key).Scan(&resourceID, &resourcePublicID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,owner_mod_id,resolved) values($1,'minecraft.item',$2,'fixture',$3,$4,true)`, resourceID, "fixture:"+key, key, modID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_resource_bindings(resource_id,mod_id) values($1,$2)`, resourceID, modID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_resource_version_details(resource_id,version_id,default_locale,definition,status) values($1,$2,'en-US','{}','active')`, resourceID, versionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_content_section_resources(section_id,version_id,resource_id,ordinal,similar_group_id) values($1,$2,$3,0,'')`, sectionID, versionID, resourceID); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	entries := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"versions", server.modContentVersions}, {"templates", server.modContentTemplates}, {"sections", server.modContentSections}, {"revision_history", server.modRevisionHistory},
		{"resources", server.modContentResources}, {"section_resources", server.modContentSectionResources}, {"resource_detail", server.modContentResource}, {"similar", server.modContentSimilarResources},
	}
	check := func(label string, claims security.Claims, want int) {
		t.Helper()
		for _, entry := range entries {
			t.Run(label+"/"+entry.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/content?version="+versionPublicID, nil).WithContext(context.WithValue(ctx, claimsContextKey, claims))
				req.SetPathValue("siteId", key)
				req.SetPathValue("sectionId", sectionPublicID)
				req.SetPathValue("resourceId", resourcePublicID)
				res := httptest.NewRecorder()
				entry.handler(res, req)
				if res.Code != want {
					t.Fatalf("content parent permission returned %d, want %d: %s", res.Code, want, res.Body.String())
				}
				if want == http.StatusNotFound && strings.Contains(res.Body.String(), "Synthetic hidden") {
					t.Fatal("unpublished content leaked")
				}
			})
		}
	}
	check("guest_pending", security.Claims{}, http.StatusNotFound)
	check("other_pending", security.Claims{Subject: userID + 1}, http.StatusNotFound)
	check("submitter_pending", security.Claims{Subject: userID}, http.StatusOK)
	check("reviewer_pending", security.Claims{PermissionRules: []security.PermissionRule{{Code: "content.review", Allow: true}}}, http.StatusOK)
	if _, err = pool.Exec(ctx, `update mods set review_status='approved' where id=$1`, modID); err != nil {
		t.Fatal(err)
	}
	check("guest_approved", security.Claims{}, http.StatusOK)
	var templateID int64
	var templatePublicID string
	if err = pool.QueryRow(ctx, `select template.id,template.public_id from mod_content_templates template join mod_content_sections section on section.template_id=template.id where section.id=$1`, sectionID).Scan(&templateID, &templatePublicID); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		name, table, keyColumn, field string
		id                            int64
		handler                       http.HandlerFunc
	}{
		{"versions", "mod_content_versions", "id", "label", versionID, server.modContentVersions},
		{"templates", "mod_content_templates", "id", "code", templateID, server.modContentTemplates},
		{"sections", "mod_content_sections", "id", "public_id", sectionID, server.modContentSections},
		{"resources", "game_resources", "entity_id", "canonical_id", resourceID, server.modContentResources},
		{"section_resources", "resource_import_snapshots", "id", "data", resourceID, server.modContentSectionResources},
		{"version_read", "mod_content_versions", "id", "status", versionID, server.modContentVersion},
		{"template_read", "mod_content_templates", "id", "status", templateID, server.modContentTemplate},
		{"section_read", "mod_content_sections", "id", "status", sectionID, server.modContentSection},
		{"resource_delete", "mod_resource_version_details", "resource_id", "published_revision_id", resourceID, server.modContentResource},
		{"layout_prepare", "mod_content_section_resources", "resource_id", "similar_group_id", resourceID, server.modContentSectionLayout},
		{"resource_get", "game_resources", "entity_id", "canonical_id", resourceID, server.modContentResource},
		{"layout_resource_prepare", "game_resources", "entity_id", "canonical_id", resourceID, server.modContentSectionLayout},
	} {
		t.Run("stream_error/"+entry.name, func(t *testing.T) {
			schema := pgx.Identifier{"apia_stream_" + randomHex(8)}.Sanitize()
			if _, err := pool.Exec(ctx, "create schema "+schema); err != nil {
				t.Fatal(err)
			}
			defer func() {
				cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
				defer stop()
				if _, err := pool.Exec(cleanup, "drop schema "+schema+" cascade"); err != nil {
					t.Errorf("allocated stream fixture cleanup: %v", err)
				}
			}()
			if _, err := pool.Exec(ctx, "create function "+schema+".fault() returns text language plpgsql volatile as $$ begin raise exception 'synthetic content row stream failure'; end $$"); err != nil {
				t.Fatal(err)
			}
			if entry.name == "section_resources" {
				// Fault only in the imported resource data, which the count does
				// not read. This reaches the resource stream after header/count.
				if _, err = pool.Exec(ctx, fmt.Sprintf("create view %s.catalog_import_revisions as select 'synthetic-stream-revision'::text id,%d::bigint target_version_id,true is_active,now() created_at", schema, versionID)); err != nil {
					t.Fatal(err)
				}
				if _, err = pool.Exec(ctx, fmt.Sprintf("create view %s.resource_import_snapshots as select %d::bigint resource_id,'synthetic-stream-revision'::text revision_id,''::text icon_path,'{}'::jsonb names,%s.fault()::jsonb data", schema, resourceID, schema)); err != nil {
					t.Fatal(err)
				}
			} else {
				columns, err := pool.Query(ctx, `select attname from pg_attribute where attrelid=$1::regclass and attnum>0 and not attisdropped order by attnum`, "public."+entry.table)
				if err != nil {
					t.Fatal(err)
				}
				selects := make([]string, 0)
				for columns.Next() {
					var column string
					if err = columns.Scan(&column); err != nil {
						columns.Close()
						t.Fatal(err)
					}
					quoted := pgx.Identifier{column}.Sanitize()
					if column == entry.field {
						cast := ""
						if entry.name == "resource_delete" {
							cast = "::bigint"
						}
						selects = append(selects, schema+".fault()"+cast+" as "+quoted)
					} else if entry.name == "template_read" && column == "owner_mod_id" {
						selects = append(selects, fmt.Sprintf("%d::bigint as %s", modID, quoted))
					} else {
						selects = append(selects, "original."+quoted)
					}
				}
				if err = columns.Err(); err != nil {
					columns.Close()
					t.Fatal(err)
				}
				columns.Close()
				view := fmt.Sprintf("create view %s.%s as select %s from public.%s original where original.%s=%d", schema, pgx.Identifier{entry.table}.Sanitize(), strings.Join(selects, ","), pgx.Identifier{entry.table}.Sanitize(), pgx.Identifier{entry.keyColumn}.Sanitize(), entry.id)
				if _, err = pool.Exec(ctx, view); err != nil {
					t.Fatal(err)
				}
			}
			parsed := pool.Config().Copy()
			parsed.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			failurePool, err := pgxpool.NewWithConfig(ctx, parsed)
			if err != nil {
				t.Fatal(err)
			}
			defer failurePool.Close()
			probe, err := failurePool.Query(ctx, "select "+pgx.Identifier{entry.field}.Sanitize()+" from "+pgx.Identifier{entry.table}.Sanitize())
			if err != nil {
				t.Fatalf("fixture did not reach PostgreSQL row streaming: %v", err)
			}
			for probe.Next() {
			}
			streamErr := probe.Err()
			probe.Close()
			if streamErr == nil || !strings.Contains(streamErr.Error(), "synthetic content row stream failure") {
				t.Fatalf("fixture missing actual Rows.Err: %v", streamErr)
			}
			failureServer := &Server{db: failurePool}
			handler := entry.handler
			switch entry.name {
			case "versions":
				handler = failureServer.modContentVersions
			case "templates":
				handler = failureServer.modContentTemplates
			case "sections":
				handler = failureServer.modContentSections
			case "resources":
				handler = failureServer.modContentResources
			case "section_resources":
				handler = failureServer.modContentSectionResources
			case "version_read":
				handler = failureServer.modContentVersion
			case "template_read":
				handler = failureServer.modContentTemplate
			case "section_read":
				handler = failureServer.modContentSection
			case "resource_delete", "resource_get":
				handler = failureServer.modContentResource
			case "layout_prepare", "layout_resource_prepare":
				handler = failureServer.modContentSectionLayout
			}
			method := http.MethodGet
			requestContext := ctx
			if strings.HasSuffix(entry.name, "_read") || entry.name == "resource_delete" {
				method = http.MethodDelete
				requestContext = context.WithValue(ctx, claimsContextKey, security.Claims{Subject: userID, PermissionRules: []security.PermissionRule{{Code: "admin.*", Allow: true}}})
			}
			var body *strings.Reader
			if entry.name == "layout_prepare" || entry.name == "layout_resource_prepare" {
				method = http.MethodPut
				requestContext = context.WithValue(ctx, claimsContextKey, security.Claims{Subject: userID, PermissionRules: []security.PermissionRule{{Code: "admin.*", Allow: true}}})
				body = strings.NewReader(fmt.Sprintf(`{"versionPublicId":%q,"rootSectionPublicId":%q,"displayMode":"compact","categories":[],"resources":[]}`, versionPublicID, sectionPublicID))
				if entry.name == "layout_resource_prepare" {
					body = strings.NewReader(fmt.Sprintf(`{"versionPublicId":%q,"rootSectionPublicId":%q,"displayMode":"compact","categories":[],"resources":[{"resourcePublicId":%q,"sectionPublicId":%q,"ordinal":0}]}`, versionPublicID, sectionPublicID, resourcePublicID, sectionPublicID))
				}
			} else {
				body = strings.NewReader("")
			}
			req := httptest.NewRequest(method, "/content?version="+versionPublicID, body).WithContext(requestContext)
			req.SetPathValue("siteId", key)
			req.SetPathValue("sectionId", sectionPublicID)
			req.SetPathValue("versionId", versionPublicID)
			req.SetPathValue("templateId", templatePublicID)
			req.SetPathValue("resourceId", resourcePublicID)
			res := httptest.NewRecorder()
			handler(res, req)
			if res.Code != http.StatusInternalServerError {
				t.Fatalf("stream failed but returned %d: %s", res.Code, res.Body.String())
			}
		})
	}
}
