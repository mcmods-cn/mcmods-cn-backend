package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

func TestOCT02ChangelogCategoriesRemainAccessibleAfterOneHundredIntegration(t *testing.T) {
	requireOCT02DatabaseIntegration(t)
	f := newTEST017Fixture(t)
	var routeID int64
	if err := f.db.QueryRow(f.ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, f.modID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `with categories as (
 insert into project_changelog_categories(object_route_id,default_locale,created_by,created_at)
 select $1,'en-US',$2,'2026-01-01T00:00:00Z'::timestamptz from generate_series(1,101)
 returning id
) insert into project_changelog_category_localizations(category_id,locale,name)
 select id,'en-US','Category '||id from categories`, routeID, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	baseQuery := fmt.Sprintf("?targetType=mod&targetId=%s&locale=en-US", f.modCode)
	raw := f.require(t, "", http.MethodGet, "/api/v1/changelogs"+baseQuery, nil, http.StatusOK)
	var first struct {
		Data struct {
			Categories           []projectChangelogCategoryResponse `json:"categories"`
			CategoriesHasMore    bool                               `json:"categoriesHasMore"`
			CategoriesNextCursor string                             `json:"categoriesNextCursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Data.Categories) != 100 || !first.Data.CategoriesHasMore || first.Data.CategoriesNextCursor == "" {
		t.Fatal("first changelog page did not preserve 100 categories and continuation")
	}
	// Cursor position remains valid after the last displayed category is removed.
	if _, err := f.db.Exec(f.ctx, `delete from project_changelog_categories where public_id=$1`, first.Data.Categories[99].ID); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/changelogs/categories" + baseQuery + "&cursor=" + url.QueryEscape(first.Data.CategoriesNextCursor)
	raw = f.require(t, "", http.MethodGet, path, nil, http.StatusOK)
	var next struct {
		Data struct {
			Categories []projectChangelogCategoryResponse `json:"categories"`
			Limit      int                                `json:"limit"`
			HasMore    bool                               `json:"hasMore"`
			NextCursor string                             `json:"nextCursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &next); err != nil {
		t.Fatal(err)
	}
	if len(next.Data.Categories) != 1 || next.Data.Limit != 100 || next.Data.HasMore || next.Data.NextCursor != "" {
		t.Fatal("remaining category missing or continuation did not terminate")
	}
	for _, item := range first.Data.Categories {
		if item.ID == next.Data.Categories[0].ID {
			t.Fatal("category pagination repeated an earlier row")
		}
	}
	f.require(t, "", http.MethodGet, "/api/v1/changelogs/categories"+baseQuery+"&cursor=invalid", nil, http.StatusBadRequest)
	f.require(t, "", http.MethodGet, "/api/v1/changelogs/categories?targetType=mod&targetId="+f.otherModCode+"&locale=en-US&cursor="+url.QueryEscape(first.Data.CategoriesNextCursor), nil, http.StatusBadRequest)
	f.require(t, "", http.MethodGet, "/api/v1/changelogs/categories?targetType=mod&targetId="+f.modCode+"&locale=zh-CN&cursor="+url.QueryEscape(first.Data.CategoriesNextCursor), nil, http.StatusBadRequest)
	if _, err := f.db.Exec(f.ctx, `update mods set review_status='pending' where id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	f.require(t, "", http.MethodGet, "/api/v1/changelogs/categories"+baseQuery, nil, http.StatusNotFound)
	// Changelog collection targets already require publication, including for
	// editors; the new category route preserves that existing boundary.
	f.require(t, f.editor, http.MethodGet, "/api/v1/changelogs/categories"+baseQuery, nil, http.StatusNotFound)
	if _, err := f.db.Exec(f.ctx, `update mods set review_status='approved' where id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodGet, "/api/v1/changelogs/categories"+baseQuery, nil, http.StatusOK)
}

func TestChangelogCategoryLinksAndEntryPaginationFollowParentIntegration(t *testing.T) {
	f := newTEST017Fixture(t)
	var category string
	if err := f.db.QueryRow(f.ctx, `insert into project_changelog_categories(object_route_id,default_locale)
		select id,'en-US' from public_routes where entity_type='mod' and internal_id=$1 returning public_id`, f.modID).Scan(&category); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into project_changelogs(object_route_id,category_id,event_at,
		project_version,default_locale,review_status,created_by)
		select category.object_route_id,case when version=4 then null else category.id end,
			'2026-01-01'::timestamptz + version*interval '1 day',version::text,'en-US',
			case when version=3 then 'pending' else 'approved' end,$2
		from project_changelog_categories category cross join generate_series(1,4) version where category.public_id=$1`, category, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	linkPath := "/api/v1/public-links/" + category
	var initialSlug string
	if err := f.db.QueryRow(f.ctx, `select slug from mods where id=$1`, f.modID).Scan(&initialSlug); err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{initialSlug, "renamed-changelog-project"} {
		if _, err := f.db.Exec(f.ctx, `update mods set slug=$2 where id=$1`, f.modID, slug); err != nil {
			t.Fatal(err)
		}
		raw := f.require(t, "", http.MethodGet, linkPath, nil, http.StatusOK)
		var result struct{ Data struct{ Target string } }
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		if want := "/mods/" + slug + "?tab=changelog&categoryId=" + category + "#changelog"; result.Data.Target != want {
			t.Fatalf("category link = %q, want %q", result.Data.Target, want)
		}
	}
	base := "/api/v1/changelogs?targetType=mod&targetId=" + f.modCode + "&limit=1"
	pagePath := base + "&categoryId=" + category
	var cursor string
	for _, wantVersion := range []string{"2", "1"} {
		raw := f.require(t, "", http.MethodGet, pagePath+"&cursor="+url.QueryEscape(cursor), nil, http.StatusOK)
		var response struct{ Data projectChangelogPage }
		if err := json.Unmarshal(raw, &response); err != nil {
			t.Fatal(err)
		}
		page := response.Data
		if len(page.Items) != 1 || page.Items[0].ProjectVersion != wantVersion || page.Items[0].Category == nil ||
			page.Items[0].Category.ID != category || page.HasMore != (wantVersion == "2") || page.HasMore != (page.NextCursor != "") {
			t.Fatalf("filtered page for version %s: %+v", wantVersion, page)
		}
		cursor = page.NextCursor
		if page.HasMore {
			for _, changedFilter := range []string{"", "&categoryId=unknown01"} {
				f.require(t, "", http.MethodGet, base+changedFilter+"&cursor="+url.QueryEscape(cursor), nil, http.StatusBadRequest)
			}
		}
	}
	f.require(t, "", http.MethodGet, base+"&categoryId=invalid", nil, http.StatusBadRequest)
	raw := f.require(t, "", http.MethodGet, "/api/v1/changelogs?targetType=mod&targetId="+f.otherModCode+"&categoryId="+category, nil, http.StatusOK)
	var other struct{ Data projectChangelogPage }
	if err := json.Unmarshal(raw, &other); err != nil || len(other.Data.Items) != 0 || other.Data.HasMore {
		t.Fatalf("other project returned entries for category: %+v, err %v", other, err)
	}
	if _, err := f.db.Exec(f.ctx, `update mods set review_status='pending' where id=$1`, f.modID); err != nil {
		t.Fatal(err)
	}
	f.require(t, "", http.MethodGet, linkPath, nil, http.StatusNotFound)
	f.require(t, "", http.MethodGet, pagePath, nil, http.StatusNotFound)
}
