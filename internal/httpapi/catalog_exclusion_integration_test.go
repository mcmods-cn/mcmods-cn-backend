package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestProjectCatalogExclusionOwnsTotalAndEveryPageIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	ctx, pool := f.ctx, f.db
	if _, err := pool.Exec(ctx, `insert into mods(project_code,slug,primary_name,review_status) values($1,'test014-exclusion-mod','Exclusion mod','approved')`, randomCatalogPublicID()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into modpacks(slug,primary_name,default_locale,environment,primary_category,official_status,source_status,license,review_status)
		select 'exclusion-pack-'||value,'Exclusion pack '||value,'en-US','bothRequired','adventure','active','open','MIT','approved'
		from generate_series(1,3) value;
		insert into simple_projects(project_type,slug,primary_name,review_status,minecraft_versions)
		select kind,'exclusion-'||kind||'-'||value,'Exclusion '||kind||' '||value,'approved',array['1.21.1']
		from unnest(array['plugin','map','resource_pack','shader_pack','datapack','addon']) kind cross join generate_series(1,3) value`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}

	type endpoint struct {
		name          string
		candidateSQL  string
		basePath      string
		handler       http.HandlerFunc
		prepare       func(*http.Request, []string)
		identityField string
	}
	endpoints := []endpoint{
		{name: "mod", candidateSQL: `select slug from mods where review_status='approved' order by id limit 1`, basePath: "/api/v1/mods", handler: server.publicMods, identityField: "siteId"},
		{name: "modpack", candidateSQL: `select slug from modpacks where review_status='approved' order by id limit 1`, basePath: "/api/v1/modpacks", handler: server.modpacks, identityField: "siteId"},
	}
	for _, kind := range simpleProjectTypeValues() {
		endpoints = append(endpoints, endpoint{name: "simple_project_" + kind, candidateSQL: fmt.Sprintf(`select slug,project_type from simple_projects where project_type='%s' and review_status='approved' order by id limit 1`, kind), basePath: "/api/v1/content-projects/" + kind, handler: server.simpleProjects, identityField: "siteId", prepare: func(request *http.Request, values []string) { request.SetPathValue("projectType", values[1]) }})
	}
	tested := 0
	for _, endpoint := range endpoints {
		endpoint := endpoint
		t.Run(endpoint.name, func(t *testing.T) {
			values := make([]string, 1)
			if endpoint.prepare != nil {
				values = make([]string, 2)
			}
			row := pool.QueryRow(ctx, endpoint.candidateSQL)
			var scanErr error
			if len(values) == 2 {
				scanErr = row.Scan(&values[0], &values[1])
			} else {
				scanErr = row.Scan(&values[0])
			}
			if scanErr != nil {
				t.Fatal(scanErr)
			}
			tested++

			baseline := loadCatalogExclusionIntegrationPage(t, endpoint, values, "")
			excluded := loadCatalogExclusionIntegrationPage(t, endpoint, values, values[0])
			if baseline.Total != 3 || len(baseline.Items) != 1 || len(excluded.Items) != 1 {
				t.Fatalf("owned fixture did not exercise a nonempty page: baseline=%+v excluded=%+v", baseline, excluded)
			}
			if excluded.Total != baseline.Total-1 {
				t.Fatalf("total did not remove exactly one row: baseline=%d excluded=%d", baseline.Total, excluded.Total)
			}
			for _, item := range excluded.Items {
				if catalogExclusionItemIdentity(item, endpoint.identityField) == values[0] {
					t.Fatalf("excluded identity %q remained on first page", values[0])
				}
			}
			second := loadCatalogExclusionIntegrationPageAt(t, endpoint, values, values[0], 1)
			if len(second.Items) != 1 || second.Items[0].SiteID == excluded.Items[0].SiteID {
				t.Fatalf("owned fixture second page missing/distinctness failure: first=%+v second=%+v", excluded, second)
			}
			if second.Total != excluded.Total {
				t.Fatalf("total changed across pages: first=%d second=%d", excluded.Total, second.Total)
			}
			for _, item := range second.Items {
				if catalogExclusionItemIdentity(item, endpoint.identityField) == values[0] {
					t.Fatalf("excluded identity %q remained on second page", values[0])
				}
			}
		})
	}
	if tested != len(endpoints) {
		t.Fatalf("exclusion endpoints invoked=%d expected=%d", tested, len(endpoints))
	}
}

type catalogExclusionIntegrationPage struct {
	Items []struct {
		ID     string `json:"id"`
		SiteID string `json:"siteId"`
	} `json:"items"`
	Total int `json:"total"`
}

func catalogExclusionItemIdentity(item struct {
	ID     string `json:"id"`
	SiteID string `json:"siteId"`
}, field string) string {
	if field == "id" {
		return item.ID
	}
	return item.SiteID
}

func loadCatalogExclusionIntegrationPage(t *testing.T, endpoint struct {
	name          string
	candidateSQL  string
	basePath      string
	handler       http.HandlerFunc
	prepare       func(*http.Request, []string)
	identityField string
}, values []string, exclusion string) catalogExclusionIntegrationPage {
	t.Helper()
	return loadCatalogExclusionIntegrationPageAt(t, endpoint, values, exclusion, 0)
}

func loadCatalogExclusionIntegrationPageAt(t *testing.T, endpoint struct {
	name          string
	candidateSQL  string
	basePath      string
	handler       http.HandlerFunc
	prepare       func(*http.Request, []string)
	identityField string
}, values []string, exclusion string, pageOffset int) catalogExclusionIntegrationPage {
	t.Helper()
	parameters := url.Values{"limit": {"1"}, "sort": {"updated"}, "order": {"desc"}}
	if exclusion != "" {
		parameters.Set("excludeSiteId", exclusion)
	}
	parameters.Set("offset", fmt.Sprint(pageOffset))
	request := httptest.NewRequest(http.MethodGet, endpoint.basePath+"?"+parameters.Encode(), nil)
	if endpoint.prepare != nil {
		endpoint.prepare(request, values)
	}
	response := httptest.NewRecorder()
	endpoint.handler(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("%s returned %d: %s", request.URL.String(), response.Code, response.Body.String())
	}
	var envelope struct {
		Data catalogExclusionIntegrationPage `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}
