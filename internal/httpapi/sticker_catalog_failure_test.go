package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStickerCatalogDoesNotHideVersionReadFailureIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	server := &Server{cfg: cfg, db: pool}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/stickers?locale=en-US", nil)
	response := httptest.NewRecorder()
	server.publicStickerCatalog(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("healthy catalog status: %d", response.Code)
	}
	// The ownership-verified disposable database belongs to this test. Losing
	// this table simulates a failed metadata query after the item query succeeds.
	if _, err := pool.Exec(ctx, `drop table sticker_catalog_state`); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	server.publicStickerCatalog(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("catalog hid metadata query failure with status %d", response.Code)
	}
}
