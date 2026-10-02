package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestProjectAutomationGETIsReadOnlyAndEnableRequiresConfigureIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify read-only automation configuration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns, poolConfig.MinConns = 1, 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	userIDs := make([]int64, 2)
	stamp := time.Now().UnixNano()
	for index := range userIDs {
		if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
			values($1,$2,'sec015',true) returning id`, fmt.Sprintf("sec015-%d-%d", stamp, index),
			fmt.Sprintf("sec015-%d-%d@example.invalid", stamp, index)).Scan(&userIDs[index]); err != nil {
			t.Fatal(err)
		}
	}
	var projectID, routeID int64
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values('s015m0001','sec015-mod','SEC015 Mod','approved',$1) returning id`, userIDs[0]).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, projectID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into project_external_sources(project_route_id,source_type,external_project_id,external_project_url,verified_at,verified_by)
		values($1,'modrinth','sec015-external','https://modrinth.com/mod/sec015-external',now(),$2)`, routeID, userIDs[0]); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	viewClaims := security.Claims{Subject: userIDs[1], PermissionRules: []security.PermissionRule{{Code: "project.auto_update.view", Allow: true}}}
	for attempt := 0; attempt < 2; attempt++ {
		response := invokeSEC015ProjectAutomation(t, ctx, server, http.MethodGet, nil, viewClaims)
		if response.Code != http.StatusOK {
			t.Fatalf("GET attempt %d status=%d body=%s", attempt+1, response.Code, response.Body.String())
		}
		assertProjectAutomationResponseUsesCamelCase(t, response.Body.String())
		var envelope struct {
			Data struct {
				Settings []struct {
					Kind       string `json:"updateKind"`
					SourceType string `json:"sourceType"`
					Enabled    bool   `json:"enabled"`
				} `json:"settings"`
			} `json:"data"`
		}
		if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if len(envelope.Data.Settings) != 3 {
			t.Fatalf("GET returned %d synthesized settings, want 3: %s", len(envelope.Data.Settings), response.Body.String())
		}
		for _, setting := range envelope.Data.Settings {
			if setting.Enabled {
				t.Fatalf("GET enabled synthesized %s setting", setting.Kind)
			}
			if setting.Kind == "minecraft_versions" && setting.SourceType != "modrinth" {
				t.Fatalf("synthesized source=%q, want modrinth", setting.SourceType)
			}
		}
	}
	var settings, enabled, configuredByViewer int
	if err = pool.QueryRow(ctx, `select count(*)::int,count(*) filter(where enabled)::int,
		count(*) filter(where configured_by=$2)::int from project_auto_update_settings where project_route_id=$1`, routeID, userIDs[1]).
		Scan(&settings, &enabled, &configuredByViewer); err != nil {
		t.Fatal(err)
	}
	if settings != 0 || enabled != 0 || configuredByViewer != 0 {
		t.Fatalf("GET persisted settings/enabled/viewer=%d/%d/%d, want 0/0/0", settings, enabled, configuredByViewer)
	}

	body := []byte(`{"items":[{"kind":"minecraft_versions","sourceType":"modrinth","interval":"quarter","enabled":true},{"kind":"changelog","sourceType":"modrinth","interval":"never","enabled":false},{"kind":"site_downloads","sourceType":"","interval":"never","enabled":false}]}`)
	viewOnlyPUT := invokeSEC015ProjectAutomation(t, ctx, server, http.MethodPut, body, viewClaims)
	if viewOnlyPUT.Code != http.StatusForbidden {
		t.Fatalf("view-only PUT status=%d body=%s", viewOnlyPUT.Code, viewOnlyPUT.Body.String())
	}
	configureClaims := security.Claims{Subject: userIDs[0], PermissionRules: []security.PermissionRule{
		{Code: "project.auto_update.view", Allow: true}, {Code: "project.auto_update.configure", Allow: true},
	}}
	configured := invokeSEC015ProjectAutomation(t, ctx, server, http.MethodPut, body, configureClaims)
	if configured.Code != http.StatusOK {
		t.Fatalf("configured PUT status=%d body=%s", configured.Code, configured.Body.String())
	}
	assertProjectAutomationResponseUsesCamelCase(t, configured.Body.String())
	var configuredByOwner int
	if err = pool.QueryRow(ctx, `select count(*)::int,count(*) filter(where enabled)::int,
		count(*) filter(where configured_by=$2)::int from project_auto_update_settings where project_route_id=$1`, routeID, userIDs[0]).
		Scan(&settings, &enabled, &configuredByOwner); err != nil {
		t.Fatal(err)
	}
	if settings != 3 || enabled != 1 || configuredByOwner != 3 {
		t.Fatalf("PUT settings/enabled/configurer=%d/%d/%d, want 3/1/3", settings, enabled, configuredByOwner)
	}

	var runID string
	if err = pool.QueryRow(ctx, `insert into project_auto_update_runs(setting_id)
		select id from project_auto_update_settings where project_route_id=$1 and update_kind='minecraft_versions' returning public_id`, routeID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	logClaims := security.Claims{Subject: userIDs[0], PermissionRules: []security.PermissionRule{{Code: "project.auto_update.view_logs", Allow: true}}}
	runsRequest := httptest.NewRequest(http.MethodGet, "/api/v1/projects/mod/s015m0001/automation/runs", nil)
	runsRequest.SetPathValue("projectType", "mod")
	runsRequest.SetPathValue("projectId", "s015m0001")
	runsRequest = runsRequest.WithContext(context.WithValue(ctx, claimsContextKey, logClaims))
	runsResponse := httptest.NewRecorder()
	server.projectAutomationRuns(runsResponse, runsRequest)
	if runsResponse.Code != http.StatusOK {
		t.Fatalf("runs status=%d body=%s", runsResponse.Code, runsResponse.Body.String())
	}
	assertProjectAutomationResponseUsesCamelCase(t, runsResponse.Body.String())
	if !strings.Contains(runsResponse.Body.String(), `"id":"`+runID+`"`) || !strings.Contains(runsResponse.Body.String(), `"updateKind":"minecraft_versions"`) {
		t.Fatalf("runs response is missing canonical identity fields: %s", runsResponse.Body.String())
	}

	overviewRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/project-automation", nil).WithContext(ctx)
	overviewResponse := httptest.NewRecorder()
	server.adminProjectAutomationOverview(overviewResponse, overviewRequest)
	if overviewResponse.Code != http.StatusOK {
		t.Fatalf("overview status=%d body=%s", overviewResponse.Code, overviewResponse.Body.String())
	}
	assertProjectAutomationResponseUsesCamelCase(t, overviewResponse.Body.String())
	for _, canonical := range []string{`"projectType":"mod"`, `"projectId":"s015m0001"`, `"canonicalPath":"/mods/sec015-mod"`, `"interval":"quarter"`} {
		if !strings.Contains(overviewResponse.Body.String(), canonical) {
			t.Fatalf("overview response is missing %s: %s", canonical, overviewResponse.Body.String())
		}
	}
}

func TestProjectAutomationReadResponseContainsNoMutation(t *testing.T) {
	raw, err := os.ReadFile("project_automation_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	body := goFunctionBody(t, string(raw), "writeProjectAutomation")
	for _, forbidden := range []string{"s.db.Begin", "tx.Exec", "insert into project_auto_update_settings", "configured_by"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("GET response path still contains mutation boundary %q", forbidden)
		}
	}
	if !strings.Contains(body, "completeProjectAutomationSettings") {
		t.Fatal("GET response no longer synthesizes missing disabled settings")
	}
}

func invokeSEC015ProjectAutomation(t *testing.T, ctx context.Context, server *Server, method string, body []byte, claims security.Claims) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "/api/v1/projects/mod/s015m0001/automation", bytes.NewReader(body))
	request.SetPathValue("projectType", "mod")
	request.SetPathValue("projectId", "s015m0001")
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	response := httptest.NewRecorder()
	server.projectAutomation(response, request)
	return response
}

func assertProjectAutomationResponseUsesCamelCase(t *testing.T, response string) {
	t.Helper()
	for _, legacy := range []string{
		`"coalesce"`,
		`"public_id"`, `"project_type"`, `"project_id"`, `"canonical_path"`, `"update_kind"`, `"source_type"`,
		`"external_project_id"`, `"external_project_url"`, `"interval_code"`, `"next_run_at"`, `"last_run_at"`,
		`"last_status"`, `"last_error_code"`, `"last_error"`, `"license_override"`, `"license_override_reason"`,
		`"license_override_source"`, `"created_at"`, `"started_at"`, `"finished_at"`, `"updated_at"`,
	} {
		if strings.Contains(response, legacy) {
			t.Fatalf("project automation response contains legacy field %s: %s", legacy, response)
		}
	}
}
