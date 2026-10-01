package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/security"
)

func TestCommunityPublishedReferencesCannotExposePrivateResourceIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	var actorID, modID, versionID, resourceID, postID int64
	var resourcePublicID string
	for _, setup := range []struct {
		sql string
		id  *int64
	}{
		{`insert into users(username,email,password_hash) values('synthetic-community-resource','synthetic-community-resource@example.test','fixture') returning id`, &actorID},
		{`insert into mods(project_code,slug,primary_name,review_status) values(new_public_id(),'synthetic-community-resource','Synthetic private resource parent','pending') returning id`, &modID},
	} {
		if err := pool.QueryRow(ctx, setup.sql).Scan(setup.id); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `insert into mod_content_versions(mod_id,label,status) values($1,'Synthetic private version','active') returning id`, modID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into catalog_entities(identity_key,public_id,entity_type,status) values('synthetic-community-resource',new_public_id(),'resource','active') returning id,public_id`).Scan(&resourceID, &resourcePublicID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into game_resources(entity_id,kind_code,canonical_id,namespace,resource_path,owner_mod_id,resolved) values($1,'minecraft.item','synthetic:private-resource','synthetic','private-resource',$2,true)`, resourceID, modID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into mod_resource_bindings(resource_id,mod_id) values($1,$2)`, resourceID, modID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into mod_resource_version_details(resource_id,version_id,default_locale,definition,status) values($1,$2,'en-US','{}','active')`, resourceID, versionID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into community_posts(author_id,kind,category,title,source_locale,body_markdown,review_status) values($1,'discussion','help','Synthetic published discussion','en-US','Synthetic','approved') returning id`, actorID).Scan(&postID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into community_post_resource_refs(post_id,resource_id,kind_code,raw_resource_id) values($1,$2,'minecraft.item','')`, postID, resourceID); err != nil {
		t.Fatal(err)
	}
	parsed := pool.Config().Copy()
	parsed.MaxConns = 1
	one, err := pgxpool.NewWithConfig(ctx, parsed)
	if err != nil {
		t.Fatal(err)
	}
	defer one.Close()
	server := &Server{db: one, cfg: cfg}
	check := func(t *testing.T, public bool) {
		t.Helper()
		visible, err := server.catalogPublicEntityVisible(ctx, resourceID)
		if err != nil || visible != public {
			t.Fatalf("catalog public fixture visibility=%v want=%v err=%v", visible, public, err)
		}
		_, resources, err := server.communityPostReferences(ctx, postID)
		if err != nil || len(resources) != 1 {
			t.Fatalf("resource references=%+v err=%v", resources, err)
		}
		ref := resources[0]
		if !public && (ref.PublicID != "" || ref.Name != "" || ref.VersionID != "" || ref.RevisionID != "" || ref.IconPath != "" || ref.IconURL != "" || len(ref.Names) != 0 || !ref.Unresolved) {
			t.Fatalf("published discussion disclosed private resource metadata: %+v", ref)
		}
		if public && (ref.PublicID != resourcePublicID || ref.Name != "synthetic:private-resource" || ref.VersionID == "" || ref.Unresolved) {
			t.Fatalf("approved resource reference was lost: %+v", ref)
		}
		requestContext, stop := context.WithTimeout(ctx, 3*time.Second)
		defer stop()
		request := httptest.NewRequest(http.MethodGet, "/community/posts?kind=discussion&resourceId="+resourcePublicID, nil).WithContext(requestContext)
		response := httptest.NewRecorder()
		server.communityPosts(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("resource filter returned %d: %s", response.Code, response.Body.String())
		}
		var body struct {
			Data struct {
				Total int                     `json:"total"`
				Items []communityPostResponse `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		want := 0
		if public {
			want = 1
		}
		if body.Data.Total != want || len(body.Data.Items) != want {
			t.Fatalf("resource filter visibility oracle: total=%d items=%d want=%d", body.Data.Total, len(body.Data.Items), want)
		}
	}
	t.Run("pending_hidden", func(t *testing.T) { check(t, false) })
	if _, err := pool.Exec(ctx, `update mods set review_status='approved' where id=$1`, modID); err != nil {
		t.Fatal(err)
	}
	t.Run("approved_kept", func(t *testing.T) { check(t, true) })
	if _, err := pool.Exec(ctx, `update mods set review_status='pending' where id=$1`, modID); err != nil {
		t.Fatal(err)
	}
	t.Run("withdrawn_hidden", func(t *testing.T) { check(t, false) })
}

func TestCommunityCreateUsesItsTransactionWithOneConnectionIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	var actorID int64
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values('synthetic-community-single-connection','synthetic-community-single-connection@example.test','fixture') returning id`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	parsed := pool.Config().Copy()
	parsed.MaxConns = 1
	one, err := pgxpool.NewWithConfig(ctx, parsed)
	if err != nil {
		t.Fatal(err)
	}
	defer one.Close()
	server := &Server{db: one, cfg: cfg}
	work, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	claims := security.Claims{Subject: actorID, PermissionRules: []security.PermissionRule{{Code: "admin.*", Allow: true}}}
	request := httptest.NewRequest(http.MethodPost, "/community/posts", strings.NewReader(`{"kind":"discussion","category":"help","title":"Synthetic complete community transaction","bodyMarkdown":"Synthetic"}`)).WithContext(context.WithValue(work, claimsContextKey, claims))
	response := httptest.NewRecorder()
	server.createCommunityPost(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("single-connection community create returned %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Data.ID == "" {
		t.Fatalf("missing created identity: %v %s", err, response.Body.String())
	}
	var published bool
	if err = one.QueryRow(ctx, `select review_status='approved' and published_revision_id is not null from community_posts where public_id=$1`, body.Data.ID).Scan(&published); err != nil || !published {
		t.Fatalf("community post not persisted/published: %v %v", published, err)
	}
	updateCtx, updateStop := context.WithTimeout(ctx, 3*time.Second)
	defer updateStop()
	update := httptest.NewRequest(http.MethodPut, "/community/posts", strings.NewReader(fmt.Sprintf(`{"title":%q,"bodyMarkdown":"Synthetic revised","category":"help"}`, "Synthetic updated transaction"))).WithContext(context.WithValue(updateCtx, claimsContextKey, claims))
	updated := httptest.NewRecorder()
	server.updateCommunityPost(updated, update, body.Data.ID)
	if updated.Code != http.StatusOK {
		t.Fatalf("single-connection community update returned %d: %s", updated.Code, updated.Body.String())
	}
	var revised bool
	if err = one.QueryRow(ctx, `select title='Synthetic updated transaction' and body_markdown='Synthetic revised' from community_posts where public_id=$1`, body.Data.ID).Scan(&revised); err != nil || !revised {
		t.Fatalf("community edit not persisted: %v %v", revised, err)
	}
}
