package httpapi

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestSimpleProjectParentFacetCursorIsScopeBound(t *testing.T) {
	request, err := parseSimpleProjectParentFacetRequest(url.Values{
		"limit": {"40"}, "selected": {"mod:alpha,modpack:beta"},
	}, "addon", 42)
	if err != nil {
		t.Fatal(err)
	}
	if request.Limit != 40 || len(request.Selected) != 2 || request.Scope == "" {
		t.Fatalf("unexpected request: %#v", request)
	}
	raw := encodeSimpleProjectParentFacetCursor(simpleProjectParentFacetCursor{
		Version: simpleProjectParentFacetCursorVersion,
		Scope:   request.Scope,
		Label:   "alpha",
		Key:     "mod:alpha",
	})
	if _, err = decodeSimpleProjectParentFacetCursor(raw, request.Scope); err != nil {
		t.Fatalf("valid cursor rejected: %v", err)
	}
	foreign, err := parseSimpleProjectParentFacetRequest(url.Values{"limit": {"40"}}, "addon", 43)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decodeSimpleProjectParentFacetCursor(raw, foreign.Scope); err == nil {
		t.Fatal("cursor was reusable across viewer visibility scopes")
	}
	if _, err = parseSimpleProjectParentFacetRequest(url.Values{"limit": {"101"}}, "addon", 42); err == nil {
		t.Fatal("unbounded parent facet page was accepted")
	}
}

func TestSimpleProjectParentFacetEndpointIsBoundedAndIndependentOfCatalogPage(t *testing.T) {
	serverSource, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	handlerSource, err := os.ReadFile("simple_project_parent_facets.go")
	if err != nil {
		t.Fatal(err)
	}
	server := string(serverSource)
	handler := string(handlerSource)
	for _, required := range []string{
		`GET /api/v1/content-projects/{projectType}/facets/parents`,
		"request.Limit+1", "nextCursor", "hasMore", "project.review_status='approved' or project.submitted_by=$2",
	} {
		if !strings.Contains(server+handler, required) {
			t.Fatalf("missing parent facet contract %q", required)
		}
	}
	if strings.Contains(handler, "offset") {
		t.Fatal("parent facet endpoint must use stable keyset pagination")
	}
}

func TestSimpleProjectParentFacetTraversesVisibleDistinctOptionsIntegration(t *testing.T) {
	db := openGlobalCatalogTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `create temp table simple_projects(
		id bigint primary key,project_type text not null,slug text not null,primary_name text not null,
		review_status text not null,submitted_by bigint not null
	); create temp table simple_project_parent_refs(
		id bigint primary key,project_id bigint not null,target_type text not null,target_id bigint,raw_identifier text not null
	); create temp table mods(id bigint primary key,slug text not null,primary_name text not null,review_status text not null default 'approved');
	create temp table modpacks(id bigint primary key,slug text not null,primary_name text not null,review_status text not null default 'approved');
	insert into simple_projects(id,project_type,slug,primary_name,review_status,submitted_by)
	select value,'addon','addon-'||value,'Addon '||value,'approved',7 from generate_series(1,125) value;
	insert into mods(id,slug,primary_name)
	select value,'parent-'||lpad(value::text,3,'0'),'Parent '||lpad(value::text,3,'0') from generate_series(1,125) value;
	insert into simple_project_parent_refs(id,project_id,target_type,target_id,raw_identifier)
	select value,value,'mod',value,'' from generate_series(1,125) value;
	insert into simple_project_parent_refs values(1001,1,'mod',1,'');
	insert into simple_projects values(1001,'addon','own-pending','Own pending','pending',42),(1002,'addon','hidden-pending','Hidden pending','pending',77);
	insert into simple_project_parent_refs values(1002,1001,'plugin',null,'own-parent'),(1003,1002,'plugin',null,'hidden-parent')`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: db}
	seen := map[string]struct{}{}
	cursor := ""
	pages := 0
	selectedSeen := false
	for {
		request, err := parseSimpleProjectParentFacetRequest(url.Values{
			"limit": {"50"}, "cursor": {cursor}, "selected": {"mod:parent-120"},
		}, "addon", 42)
		if err != nil {
			t.Fatal(err)
		}
		page, err := server.loadSimpleProjectParentFacetPage(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, item := range page.Items {
			if _, duplicate := seen[item.Key]; duplicate {
				t.Fatalf("parent facet repeated across pages: %s", item.Key)
			}
			seen[item.Key] = struct{}{}
		}
		for _, item := range page.SelectedItems {
			selectedSeen = selectedSeen || item.Key == "mod:parent-120" && item.Label == "Parent 120"
		}
		if page.NextCursor == "" {
			if page.HasMore {
				t.Fatal("terminal facet page still reports more rows")
			}
			break
		}
		if !page.HasMore {
			t.Fatal("non-terminal facet page omitted hasMore")
		}
		cursor = page.NextCursor
	}
	if pages != 3 || len(seen) != 126 {
		t.Fatalf("pages=%d visible distinct parents=%d, want 3/126", pages, len(seen))
	}
	if _, exists := seen["plugin:hidden-parent"]; exists {
		t.Fatal("another user's pending parent facet was exposed")
	}
	if _, exists := seen["plugin:own-parent"]; !exists {
		t.Fatal("viewer's own pending parent facet was omitted")
	}
	if !selectedSeen {
		t.Fatal("selected parent outside the first page was not echoed with its label")
	}
}
