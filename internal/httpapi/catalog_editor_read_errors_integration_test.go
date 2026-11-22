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
	entity := catalogEditorEntity{ID: entityID, EntityType: "recipe_type", PublicID: publicID}
	if status, err := server.catalogPendingReviewStatus(ctx, entity); err != nil || status != "approved" {
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
				if status, reviewErr := server.catalogPendingReviewStatus(ctx, entity); reviewErr == nil || status != "" {
					t.Fatalf("review database failure=(%q,%v), want empty/error", status, reviewErr)
				}
			}
		})
	}
}

func TestCatalogRecipeTypeDetailUsesItsOwnReviewHistoryIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "content.write", "project.create")

	// The tables use independent ID sequences. Align their next values in this
	// fixture's database so ordinary submissions exercise the collision.
	var nextID int64
	if err := f.db.QueryRow(f.ctx, `select greatest(
		(select coalesce(max(id),0) from catalog_entities), (select coalesce(max(id),0) from mods)
	)+1`).Scan(&nextID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `select
		setval(pg_get_serial_sequence('catalog_entities','id'),$1,false),
		setval(pg_get_serial_sequence('mods','id'),$1,false)`, nextID); err != nil {
		t.Fatal(err)
	}

	const catalogPath = "/api/v1/recipe-types"
	edit := map[string]any{
		"canonicalId": "fixture:review-history", "defaultLocale": "en-US",
		"localizations": []map[string]string{{"locale": "en-US", "name": "Review history"}},
		"definition":    map[string]any{},
	}
	created := decodeTEST022Data[catalogEditResult](t, f.require(t, f.editor, http.MethodPost, catalogPath, edit, http.StatusAccepted))
	reviewPath := "/api/v1/content-revisions/"
	f.require(t, f.reviewer, http.MethodPatch, reviewPath+created.RevisionID, map[string]string{"status": "approved"}, http.StatusOK)
	detailPath := catalogPath + "/" + created.ObjectPublicID
	assertDetailStatus := func(want string) {
		t.Helper()
		detail := decodeTEST022Data[struct {
			ReviewStatus string `json:"reviewStatus"`
		}](t, f.require(t, "", http.MethodGet, detailPath, nil, http.StatusOK))
		if detail.ReviewStatus != want {
			t.Fatalf("catalog review status=%q, want %q", detail.ReviewStatus, want)
		}
	}
	assertDetailStatus("approved")

	project := decodeTEST022Data[modResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/mods", createModRequest{
		SiteID: "review-history-mod", PrimaryName: "Review history mod", DefaultLocale: "en-US",
		Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "Review history mod", Summary: "Review history fixture"}},
		Environment:   "bothRequired", PrimaryCategory: "utility", OfficialStatus: "active", SourceStatus: "open",
		License: "MIT", SubmissionMethod: "manual", ModIDs: []modIdentifierPayload{{Identifier: "review_history_mod", Primary: true}},
		Compatibilities: []modLoaderCompatibilityPayload{{Loader: "neoforge", Versions: []string{"1.21.1"}}},
	}, http.StatusCreated))
	var catalogID, projectID int64
	var projectStatus string
	if err := f.db.QueryRow(f.ctx, `select entity.id,mod.id,request.status from catalog_entities entity
		cross join mods mod join change_requests request
		  on request.aggregate_type='mod' and request.aggregate_key=mod.project_code
		where entity.public_id=$1 and mod.project_code=$2`, created.ObjectPublicID, project.UniqueID).
		Scan(&catalogID, &projectID, &projectStatus); err != nil {
		t.Fatal(err)
	}
	if catalogID != nextID || projectID != nextID || projectStatus != "pending" {
		t.Fatalf("expected independent records with one internal ID and a pending project review, got %d/%d %q", catalogID, projectID, projectStatus)
	}
	assertDetailStatus("approved")

	edit["baseRevisionId"] = created.RevisionID
	edit["localizations"] = []map[string]string{{"locale": "en-US", "name": "Revised review history"}}
	pending := decodeTEST022Data[catalogEditResult](t, f.require(t, f.editor, http.MethodPut, detailPath, edit, http.StatusAccepted))
	assertDetailStatus("pending")
	f.require(t, f.reviewer, http.MethodPatch, reviewPath+pending.RevisionID, map[string]string{"status": "rejected"}, http.StatusOK)
	assertDetailStatus("rejected")
}

func invokeCatalogRecipeTypeDetail(t *testing.T, server *Server, publicID string) (int, string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/recipe-types/"+publicID, nil)
	request.SetPathValue("publicId", publicID)
	response := httptest.NewRecorder()
	server.catalogRecipeTypeDetail(response, request)
	return response.Code, response.Body.String()
}
