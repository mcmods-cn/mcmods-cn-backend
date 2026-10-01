package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCommunityReferencesDoNotExposeUnpublishedProjectIntegration(t *testing.T) {
	url := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("requires dedicated migrated PostgreSQL via MCMODS_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	key := "apia_ref_" + randomHex(8)
	var userID, modID, postID int64
	var modPublicID string
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		for _, q := range []struct {
			sql string
			id  int64
		}{
			{`delete from community_posts where id=$1`, postID},
			{`delete from mods where id=$1`, modID},
			{`delete from users where id=$1`, userID},
		} {
			if q.id != 0 {
				if _, err := pool.Exec(cleanup, q.sql, q.id); err != nil {
					t.Errorf("allocated reference fixture cleanup: %v", err)
				}
			}
		}
	}()
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash) values($1,$2,'fixture') returning id`, key, key+"@example.test").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,submitted_by,review_status) values(new_public_id(),$1,'Synthetic secret',$2,'pending') returning id`, key, userID).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into community_posts(author_id,kind,category,title,source_locale,body_markdown,review_status) values($1,'discussion','general','Fixture public discussion','en-US','Fixture','approved') returning id`, userID).Scan(&postID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into community_post_project_refs(post_id,target_type,target_id,raw_identifier) values($1,'mod',$2,'')`, postID, modID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select public_id from public_routes where entity_type='mod' and internal_id=$1`, modID).Scan(&modPublicID); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	checkFilter := func(want int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/community/posts?kind=discussion&modId="+modPublicID, nil).WithContext(ctx)
		res := httptest.NewRecorder()
		server.communityPosts(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("project filter failed: %d %s", res.Code, res.Body.String())
		}
		var response struct {
			Data struct {
				Total int                     `json:"total"`
				Items []communityPostResponse `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Data.Total != want || len(response.Data.Items) != want {
			t.Fatalf("project filter exposed unpublished association or lost approved association: total=%d items=%d want=%d", response.Data.Total, len(response.Data.Items), want)
		}
	}
	checkFilter(0)
	projects, _, err := server.communityPostReferences(ctx, postID)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].PublicID != "" || projects[0].SiteID != "" || projects[0].Name != "" || !projects[0].Unresolved {
		t.Fatalf("pending project metadata leaked through a published discussion: %#v", projects)
	}
	if _, err = pool.Exec(ctx, `update mods set review_status='approved' where id=$1`, modID); err != nil {
		t.Fatal(err)
	}
	checkFilter(1)
	projects, _, err = server.communityPostReferences(ctx, postID)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].Name != "Synthetic secret" || projects[0].PublicID == "" || projects[0].SiteID != key || projects[0].Unresolved {
		t.Fatalf("approved project association was lost: %#v", projects)
	}
}
