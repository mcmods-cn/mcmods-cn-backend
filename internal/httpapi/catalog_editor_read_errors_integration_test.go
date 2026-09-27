package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestCatalogRecipeTypeDetailFailsOnEveryRequiredReadIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify catalog detail read failures")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
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

	suffix := time.Now().UnixNano()
	var entityID int64
	var publicID string
	if err = pool.QueryRow(ctx, `insert into catalog_entities(identity_key,entity_type,status,default_locale)
		values($1,'recipe_type','active','en-US') returning id,public_id`, fmt.Sprintf("arch010:type:%d", suffix)).Scan(&entityID, &publicID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into recipe_types(entity_id,canonical_id) values($1,$2)`, entityID, fmt.Sprintf("arch010:type_%d", suffix)); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	if status, body := invokeCatalogRecipeTypeDetail(t, server, publicID); status != http.StatusOK {
		t.Fatalf("healthy detail status=%d body=%s", status, body)
	}
	if status, err := server.catalogPendingReviewStatus(ctx, entityID); err != nil || status != "approved" {
		t.Fatalf("absent review status=(%q,%v), want approved/nil", status, err)
	}

	for _, testCase := range []struct {
		name  string
		table string
	}{
		{name: "template count", table: "recipe_layout_templates"},
		{name: "localizations", table: "content_localizations"},
		{name: "catalysts", table: "recipe_type_catalysts"},
		{name: "review status", table: "change_requests"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			brokenName := "arch010_broken_" + testCase.table
			if _, renameErr := pool.Exec(ctx, `alter table `+testCase.table+` rename to `+brokenName); renameErr != nil {
				t.Fatal(renameErr)
			}
			defer func() {
				if _, restoreErr := pool.Exec(context.Background(), `alter table `+brokenName+` rename to `+testCase.table); restoreErr != nil {
					t.Errorf("restore %s: %v", testCase.table, restoreErr)
				}
			}()
			status, body := invokeCatalogRecipeTypeDetail(t, server, publicID)
			if status != http.StatusInternalServerError {
				t.Fatalf("detail with unavailable %s status=%d body=%s; want 500", testCase.table, status, body)
			}
			if testCase.table == "change_requests" {
				if status, reviewErr := server.catalogPendingReviewStatus(ctx, entityID); reviewErr == nil || status != "" {
					t.Fatalf("review database failure=(%q,%v), want empty/error", status, reviewErr)
				}
			}
		})
	}
}

func invokeCatalogRecipeTypeDetail(t *testing.T, server *Server, publicID string) (int, string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/recipe-types/"+publicID, nil)
	request.SetPathValue("publicId", publicID)
	response := httptest.NewRecorder()
	server.catalogRecipeTypeDetail(response, request)
	return response.Code, response.Body.String()
}
