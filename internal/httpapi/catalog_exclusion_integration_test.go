package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestProjectCatalogExclusionOwnsTotalAndEveryPageIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify project catalog exclusion against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
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
		{name: "simple_project", candidateSQL: `select slug,project_type from simple_projects where review_status='approved' order by id limit 1`, basePath: "/api/v1/content-projects/placeholder", handler: server.simpleProjects, identityField: "siteId",
			prepare: func(request *http.Request, values []string) { request.SetPathValue("projectType", values[1]) }},
	}
	tested := 0
	for _, endpoint := range endpoints {
		endpoint := endpoint
		t.Run(endpoint.name, func(t *testing.T) {
			values := make([]string, 1)
			if endpoint.name == "simple_project" {
				values = make([]string, 2)
			}
			row := pool.QueryRow(ctx, endpoint.candidateSQL)
			var scanErr error
			if len(values) == 2 {
				scanErr = row.Scan(&values[0], &values[1])
			} else {
				scanErr = row.Scan(&values[0])
			}
			if scanErr == pgx.ErrNoRows {
				t.Skip("development database has no approved candidate")
			}
			if scanErr != nil {
				t.Fatal(scanErr)
			}
			tested++

			baseline := loadCatalogExclusionIntegrationPage(t, endpoint, values, "")
			excluded := loadCatalogExclusionIntegrationPage(t, endpoint, values, values[0])
			if excluded.Total != baseline.Total-1 {
				t.Fatalf("total did not remove exactly one row: baseline=%d excluded=%d", baseline.Total, excluded.Total)
			}
			for _, item := range excluded.Items {
				if catalogExclusionItemIdentity(item, endpoint.identityField) == values[0] {
					t.Fatalf("excluded identity %q remained on first page", values[0])
				}
			}
			second := loadCatalogExclusionIntegrationPageAt(t, endpoint, values, values[0], 1)
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
	if tested == 0 {
		t.Skip("development database has no approved exclusion candidate")
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
