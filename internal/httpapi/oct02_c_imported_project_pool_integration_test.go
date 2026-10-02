package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestImportedProjectIconPreflightDoesNotHoldTheOnlyDatabaseConnectionIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	base := newOCT02IsolatedDatabase(t, ctx)
	poolConfig := base.Config()
	poolConfig.MaxConns, poolConfig.MinConns = 1, 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var actor int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash)
		values('project-image-pool','project-image-pool@example.invalid','synthetic') returning id`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	claims := security.Claims{Subject: actor, PermissionRules: []security.PermissionRule{
		{Code: "modpack.create", Allow: true}, {Code: "project.create.plugin", Allow: true},
	}}
	server := &Server{db: pool, cfg: config.Config{SettingsEncryptionKey: "synthetic-project-image-pool-key"}}
	for _, test := range []struct {
		name    string
		payload any
		handler http.HandlerFunc
		status  int
	}{
		{"modpack", createModpackRequest{PrimaryName: "Imported modpack image pool", DefaultLocale: "en-US",
			Environment: "bothRequired", PrimaryCategory: "adventure", PackType: "native", PackagingMethod: "other",
			OfficialStatus: "active", SourceStatus: "open", License: "MIT", SubmissionMethod: "modrinth",
			IconURL:         "https://unsupported.example.invalid/icon.png",
			Compatibilities: []modLoaderCompatibilityPayload{{Loader: "Fabric", Versions: []string{"1.21.1"}}}},
			server.createModpack, http.StatusBadGateway},
		{"plugin", simpleProjectSnapshot{ProjectType: "plugin", DefaultLocale: "en-US", MinecraftVersions: []string{"1.21.1"},
			Loaders: []string{"paper"}, OfficialStatus: "active", SourceStatus: "open", License: "MIT", SubmissionMethod: "modrinth",
			IconURL:       "https://unsupported.example.invalid/icon.png",
			Localizations: []simpleProjectLocalization{{Locale: "en-US", Name: "Imported plugin image pool"}},
			Links:         []modLinkPayload{{Type: "official", URL: "https://example.invalid/project"}}},
			func(w http.ResponseWriter, r *http.Request) { server.createSimpleProject(w, r, "plugin") }, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, err := json.Marshal(test.payload)
			if err != nil {
				t.Fatal(err)
			}
			requestCtx, cancelRequest := context.WithTimeout(ctx, time.Second)
			defer cancelRequest()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewReader(body))
			request = request.WithContext(context.WithValue(requestCtx, claimsContextKey, claims))
			response := httptest.NewRecorder()
			// This host is rejected before any network request. Settings, identity
			// allocation and transaction behavior use the actual project schema.
			test.handler(response, request)
			if response.Code != test.status || requestCtx.Err() != nil {
				t.Fatalf("single-connection preflight status=%d want=%d requestError=%v", response.Code, test.status, requestCtx.Err())
			}
			var stored int
			if err = pool.QueryRow(ctx, `select (select count(*) from modpacks where submitted_by=$1)+
				(select count(*) from simple_projects where submitted_by=$1)`, actor).Scan(&stored); err != nil || stored != 0 {
				t.Fatalf("failed preflight committed projects=%d error=%v", stored, err)
			}
		})
	}
}
