package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestCreatorImportPreviewDoesNotPersistMembersIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify creator import preview isolation against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/organization/preview-team" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"Preview Team","members":[
			{"user":{"username":"supporter","name":"Supporter"},"role":"Supporter","is_owner":false},
			{"user":{"username":"maintainer","name":"Maintainer"},"role":"Maintainer","is_owner":false}
		]}`))
	}))
	defer provider.Close()

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
	if _, err = pool.Exec(ctx, `
		create temporary table system_settings(key text primary key,value jsonb not null);
		create temporary table creators(id bigserial primary key);
		create temporary table creator_role_definitions(code text primary key,name text not null,permission_granting boolean not null);
		insert into creator_role_definitions(code,name,permission_granting) values
			('contributor','Contributor',false),('maintainer','Maintainer',true)`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool, cfg: loaded}
	cfg := defaultModImportConfig()
	cfg.Modrinth.BaseURL = provider.URL + "/v2"
	cfg.Modrinth.Token = ""
	cfg.Modrinth.Enabled = true
	sealed, err := server.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values($1,$2::jsonb)`, modImportConfigSettingKey, sealed); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/creator-imports",
		strings.NewReader(`{"kind":"team","url":"https://modrinth.com/organization/preview-team"}`)).WithContext(ctx)
	response := httptest.NewRecorder()
	server.importCreator(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("preview returned %d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data creatorImportResponse `json:"data"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	preview := envelope.Data
	if len(preview.Members) != 2 || preview.Members[0].SuggestedRoleCode != "contributor" ||
		preview.Members[0].PermissionGranting || preview.Members[1].SuggestedRoleCode != "maintainer" ||
		!preview.Members[1].PermissionGranting {
		t.Fatalf("unexpected member preview: %#v", preview.Members)
	}
	var creatorCount int
	if err = pool.QueryRow(ctx, `select count(*)::int from creators`).Scan(&creatorCount); err != nil {
		t.Fatal(err)
	}
	if creatorCount != 0 {
		t.Fatalf("preview persisted %d creator rows", creatorCount)
	}
	for _, forbidden := range []string{"creatorId", "roleId", "avatarFileId", "createdMembers"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Errorf("preview response leaked persistent field %q: %s", forbidden, response.Body.String())
		}
	}
}
