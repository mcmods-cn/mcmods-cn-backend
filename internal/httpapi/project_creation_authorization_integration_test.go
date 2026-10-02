package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
	"mcmods-cn-backend/internal/serverprobe"
)

func TestProjectCreationAndImportEntrypointsDoNotGrantAuthorizationIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify project creation authorization isolation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	loaded := config.Load()
	poolConfig, err := pgxpool.ParseConfig(loaded.DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
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

	nonce := strconv.FormatInt(time.Now().UnixNano(), 36)
	var actorID, authVersion, permissionVersion int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'test-only',true) returning id,auth_version,permission_version`,
		"creation_"+nonce, nonce+"@creation.test").Scan(&actorID, &authVersion, &permissionVersion); err != nil {
		t.Fatal(err)
	}
	var baselineRoleBindings int
	if err = pool.QueryRow(ctx, `select count(*)::int from user_role_bindings`).Scan(&baselineRoleBindings); err != nil {
		t.Fatal(err)
	}
	claims := security.Claims{Subject: actorID, PermissionRules: []security.PermissionRule{
		{Code: "project.create", Allow: true},
		{Code: "project.create.*", Allow: true},
		{Code: "modpack.create", Allow: true},
		{Code: "server.create", Allow: true},
		{Code: "server.create.no-review", Allow: true},
	}}
	server := &Server{
		cfg: loaded, db: pool, cache: querycache.New(config.RedisConfig{}),
		serverProbe: func(_ context.Context, address string) (serverprobe.Result, error) {
			return serverprobe.Result{
				Address: address, NormalizedAddress: "creation-" + nonce + ":25565",
				HandshakeHost: "creation-" + nonce + ".test", ConnectHost: "203.0.113.10", ConnectPort: 25565,
				Online: true, PlayersMax: 20, MOTD: "Creation authorization test", MinecraftVersion: "1.21.1", Protocol: 767,
				Mods: []serverprobe.Mod{},
			}, nil
		},
	}

	for _, submissionMethod := range []string{"manual", "modrinth"} {
		t.Run("mod_"+submissionMethod, func(t *testing.T) {
			siteID := "auth_mod_" + submissionMethod + "_" + nonce
			result := invokeProjectHandler[modResponse](t, ctx, claims, http.MethodPost, "/api/v1/mods", createModRequest{
				SiteID: siteID, PrimaryName: "Authorization Mod " + submissionMethod,
				Environment: "bothRequired", PrimaryCategory: "technology", OfficialStatus: "active",
				SourceStatus: "open", License: "MIT", SubmissionMethod: submissionMethod,
			}, http.StatusCreated, server.createMod)
			if result.SiteID != siteID {
				t.Fatalf("created mod siteId=%q want=%q", result.SiteID, siteID)
			}
			var projectID int64
			if err = pool.QueryRow(ctx, `select id from mods where slug=$1`, siteID).Scan(&projectID); err != nil {
				t.Fatal(err)
			}
			assertProjectCreationAuthorizationUnchanged(t, ctx, pool, actorID, authVersion, permissionVersion,
				baselineRoleBindings, "mod", projectID)
			assertProjectManagementDenied(t, ctx, claims, "/api/v1/mods/"+siteID+"/editor", func(request *http.Request) {
				request.SetPathValue("siteId", siteID)
			}, server.modEditorDetail)
		})
	}

	for _, submissionMethod := range []string{"manual", "modrinth"} {
		t.Run("modpack_"+submissionMethod, func(t *testing.T) {
			siteID := "auth_pack_" + submissionMethod + "_" + nonce
			result := invokeProjectHandler[modpackResponse](t, ctx, claims, http.MethodPost, "/api/v1/modpacks", createModpackRequest{
				SiteID: siteID, PrimaryName: "Authorization Modpack " + submissionMethod, DefaultLocale: "en-US",
				Environment: "bothRequired", PrimaryCategory: "adventure", PackType: "native", PackagingMethod: "other",
				OfficialStatus: "active", SourceStatus: "open", License: "MIT", SubmissionMethod: submissionMethod,
				Compatibilities: []modLoaderCompatibilityPayload{{Loader: "Fabric", Versions: []string{"1.21.1"}}},
			}, http.StatusCreated, server.createModpack)
			if result.SiteID != siteID || result.CanEdit {
				t.Fatalf("created modpack siteId=%q canEdit=%t", result.SiteID, result.CanEdit)
			}
			var projectID int64
			if err = pool.QueryRow(ctx, `select id from modpacks where slug=$1`, siteID).Scan(&projectID); err != nil {
				t.Fatal(err)
			}
			assertProjectCreationAuthorizationUnchanged(t, ctx, pool, actorID, authVersion, permissionVersion,
				baselineRoleBindings, "modpack", projectID)
			assertProjectManagementDenied(t, ctx, claims, "/api/v1/modpacks/"+siteID+"/editor", func(request *http.Request) {
				request.SetPathValue("siteId", siteID)
			}, server.modpackEditor)
		})
	}

	for _, projectType := range simpleProjectTypeValues() {
		for _, submissionMethod := range []string{"manual", simpleProjectImportProvider(projectType)} {
			name := projectType + "_" + submissionMethod
			t.Run(name, func(t *testing.T) {
				siteID := "auth_" + stringsForSiteID(projectType) + "_" + submissionMethod + "_" + nonce
				snapshot := projectCreationTestSnapshot(projectType, siteID, submissionMethod)
				result := invokeProjectHandler[simpleProjectResponse](t, ctx, claims, http.MethodPost,
					"/api/v1/content-projects/"+projectType, snapshot, http.StatusCreated,
					func(w http.ResponseWriter, r *http.Request) { server.createSimpleProject(w, r, projectType) })
				if result.SiteID != siteID || result.CanEdit {
					t.Fatalf("created %s siteId=%q canEdit=%t", projectType, result.SiteID, result.CanEdit)
				}
				var projectID int64
				if err = pool.QueryRow(ctx, `select id from simple_projects where project_type=$1 and slug=$2`,
					projectType, siteID).Scan(&projectID); err != nil {
					t.Fatal(err)
				}
				assertProjectCreationAuthorizationUnchanged(t, ctx, pool, actorID, authVersion, permissionVersion,
					baselineRoleBindings, projectType, projectID)
				assertProjectManagementDenied(t, ctx, claims,
					"/api/v1/content-projects/"+projectType+"/"+siteID+"/editor", func(request *http.Request) {
						request.SetPathValue("projectType", projectType)
						request.SetPathValue("siteId", siteID)
					}, server.simpleProjectEditor)
			})
		}
	}

	t.Run("minecraft_server", func(t *testing.T) {
		result := invokeProjectHandler[struct {
			ID           string `json:"id"`
			ReviewStatus string `json:"reviewStatus"`
		}](t, ctx, claims, http.MethodPost, "/api/v1/servers", createMinecraftServerRequest{
			Address: "creation-" + nonce + ".test", Name: "Authorization Server",
			MinecraftVersions: []string{"1.21.1"}, Languages: []string{"zh-CN"}, PrimaryTag: "survival",
		}, http.StatusCreated, server.createMinecraftServer)
		if result.ID == "" || result.ReviewStatus != "approved" {
			t.Fatalf("created server id=%q status=%q", result.ID, result.ReviewStatus)
		}
		var projectID int64
		if err = pool.QueryRow(ctx, `select id from minecraft_servers where public_id=$1`, result.ID).Scan(&projectID); err != nil {
			t.Fatal(err)
		}
		assertProjectCreationAuthorizationUnchanged(t, ctx, pool, actorID, authVersion, permissionVersion,
			baselineRoleBindings, "minecraft_server", projectID)
		request := newProjectHandlerRequest(t, ctx, claims, http.MethodPut, "/api/v1/servers/"+result.ID,
			updateMinecraftServerRequest{Name: "Authorization Server", MinecraftVersions: []string{"1.21.1"},
				Languages: []string{"zh-CN"}, PrimaryTag: "survival"})
		request.SetPathValue("serverId", result.ID)
		response := httptest.NewRecorder()
		server.updateMinecraftServer(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("server submitter management status=%d body=%s", response.Code, response.Body.String())
		}
	})

	configureProjectImportTest(t, ctx, server)
	var projectsBefore, jobsBefore int
	if err = pool.QueryRow(ctx, `select
		(select count(*) from mods)+(select count(*) from modpacks)+(select count(*) from simple_projects)+(select count(*) from minecraft_servers),
		(select count(*) from mod_metadata_import_jobs)`).Scan(&projectsBefore, &jobsBefore); err != nil {
		t.Fatal(err)
	}
	importCases := []struct {
		name, projectType, provider, sourceURL string
		invoke                                 func(http.ResponseWriter, *http.Request)
	}{
		{name: "mod", projectType: "mod", provider: "modrinth", sourceURL: "https://modrinth.com/mod/auth-test", invoke: server.createModMetadataImport},
		{name: "modpack", projectType: "modpack", provider: "modrinth", sourceURL: "https://modrinth.com/modpack/auth-test", invoke: server.createModpackMetadataImport},
	}
	for _, projectType := range simpleProjectTypeValues() {
		projectType := projectType
		provider := simpleProjectImportProvider(projectType)
		section := projectImportSourceSpecs[projectType].ModrinthSections
		sourceURL := ""
		if provider == "modrinth" {
			sourceURL = "https://modrinth.com/" + section[0] + "/auth-test"
		} else {
			sourceURL = "https://www.curseforge.com/minecraft/" + projectImportSourceSpecs[projectType].CurseForgeSections[0] + "/auth-test"
		}
		importCases = append(importCases, struct {
			name, projectType, provider, sourceURL string
			invoke                                 func(http.ResponseWriter, *http.Request)
		}{name: projectType, projectType: projectType, provider: provider, sourceURL: sourceURL,
			invoke: server.createSimpleProjectMetadataImport})
	}
	for index, testCase := range importCases {
		t.Run("metadata_import_"+testCase.name, func(t *testing.T) {
			request := newProjectHandlerRequest(t, ctx, claims, http.MethodPost, "/api/v1/imports",
				modMetadataImportRequest{Provider: testCase.provider, URL: testCase.sourceURL})
			request.SetPathValue("projectType", testCase.projectType)
			response := httptest.NewRecorder()
			testCase.invoke(response, request)
			if response.Code != http.StatusAccepted {
				t.Fatalf("metadata import status=%d body=%s", response.Code, response.Body.String())
			}
			var projectsAfter, jobsAfter int
			if err = pool.QueryRow(ctx, `select
				(select count(*) from mods)+(select count(*) from modpacks)+(select count(*) from simple_projects)+(select count(*) from minecraft_servers),
				(select count(*) from mod_metadata_import_jobs)`).Scan(&projectsAfter, &jobsAfter); err != nil {
				t.Fatal(err)
			}
			if projectsAfter != projectsBefore || jobsAfter != jobsBefore+index+1 {
				t.Fatalf("metadata import changed projects %d -> %d or jobs=%d", projectsBefore, projectsAfter, jobsAfter)
			}
			assertProjectCreationAuthorizationUnchanged(t, ctx, pool, actorID, authVersion, permissionVersion,
				baselineRoleBindings, "", 0)
		})
	}
}

func invokeProjectHandler[T any](t *testing.T, ctx context.Context, claims security.Claims, method, path string,
	payload any, wantStatus int, handler http.HandlerFunc) T {
	t.Helper()
	request := newProjectHandlerRequest(t, ctx, claims, method, path, payload)
	response := httptest.NewRecorder()
	handler(response, request)
	if response.Code != wantStatus {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, path, response.Code, wantStatus, response.Body.String())
	}
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

func newProjectHandlerRequest(t *testing.T, ctx context.Context, claims security.Claims, method, path string, payload any) *http.Request {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(raw))
	return request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
}

func assertProjectManagementDenied(t *testing.T, ctx context.Context, claims security.Claims, path string,
	setPathValues func(*http.Request), handler http.HandlerFunc) {
	t.Helper()
	request := newProjectHandlerRequest(t, ctx, claims, http.MethodGet, path, struct{}{})
	setPathValues(request)
	response := httptest.NewRecorder()
	handler(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("submitter management status=%d body=%s", response.Code, response.Body.String())
	}
}

func assertProjectCreationAuthorizationUnchanged(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	actorID, wantAuthVersion, wantPermissionVersion int64, wantRoleBindings int, projectType string, projectID int64) {
	t.Helper()
	var authVersion, permissionVersion int64
	var actorRoleBindings, allRoleBindings, directPermissions, editorAssignments, creatorClaims, effectiveAccess int
	err := pool.QueryRow(ctx, `select
		(select auth_version from users where id=$1),(select permission_version from users where id=$1),
		(select count(*) from user_role_bindings where user_id=$1),(select count(*) from user_role_bindings),
		(select count(*) from user_permissions where user_id=$1),(select count(*) from project_editor_assignments where user_id=$1),
		(select count(*) from creator_claims where user_id=$1),
		(select count(*) from effective_project_access where user_id=$1 and ($2='' or project_type=$2 and project_id=$3))`,
		actorID, projectType, projectID).Scan(&authVersion, &permissionVersion, &actorRoleBindings, &allRoleBindings,
		&directPermissions, &editorAssignments, &creatorClaims, &effectiveAccess)
	if err != nil {
		t.Fatal(err)
	}
	if authVersion != wantAuthVersion || permissionVersion != wantPermissionVersion || actorRoleBindings != 0 ||
		allRoleBindings != wantRoleBindings || directPermissions != 0 || editorAssignments != 0 || creatorClaims != 0 || effectiveAccess != 0 {
		t.Fatalf("authorization changed: auth=%d permission=%d actorRoles=%d allRoles=%d direct=%d editors=%d claims=%d effective=%d",
			authVersion, permissionVersion, actorRoleBindings, allRoleBindings, directPermissions, editorAssignments, creatorClaims, effectiveAccess)
	}
}

func projectCreationTestSnapshot(projectType, siteID, submissionMethod string) simpleProjectSnapshot {
	snapshot := simpleProjectSnapshot{
		ProjectType: projectType, SiteID: siteID, DefaultLocale: "en-US",
		Localizations:     []simpleProjectLocalization{{Locale: "en-US", Name: "Authorization " + projectType}},
		MinecraftVersions: []string{"1.21.1"}, OfficialStatus: "active", SourceStatus: "open", License: "MIT",
		SubmissionMethod: submissionMethod, Links: []modLinkPayload{{Type: "official", URL: "https://example.test/" + siteID}},
	}
	switch projectType {
	case "plugin":
		snapshot.Loaders = []string{"paper"}
	case "map":
		snapshot.MapSize = "medium"
	case "resource_pack":
		snapshot.Resolution = "16x"
	case "shader_pack":
		snapshot.Loaders = []string{"iris"}
		snapshot.Performance = "medium"
	case "datapack":
		snapshot.Loaders = []string{"vanilla"}
	case "addon":
		snapshot.ParentProjects = []simpleProjectParent{{Type: "mod", Identifier: "authorization-parent"}}
	}
	return snapshot
}

func simpleProjectImportProvider(projectType string) string {
	if projectType == "map" {
		return "curseforge"
	}
	return "modrinth"
}

func stringsForSiteID(value string) string {
	if value == "resource_pack" {
		return "resource"
	}
	if value == "shader_pack" {
		return "shader"
	}
	return value
}

func configureProjectImportTest(t *testing.T, ctx context.Context, server *Server) {
	t.Helper()
	cfg := defaultModImportConfig()
	cfg.Modrinth.Enabled = true
	cfg.CurseForge.Enabled = true
	cfg.CurseForge.APIKey = "integration-test-only"
	sealed, err := server.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = server.db.Exec(ctx, `insert into system_settings(key,value) values($1,$2::jsonb)`,
		modImportConfigSettingKey, sealed); err != nil {
		t.Fatal(err)
	}
}
