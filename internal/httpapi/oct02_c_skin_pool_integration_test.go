package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/security"
)

func TestOCT02CSkinUpdateReadsReviewConfigurationOnTransactionIntegration(t *testing.T) {
	f := newTEST039Fixture(t)
	file := f.upload(t, f.editor, "oct02-pool.png", test039PNG(t, 64, 64, 35))
	asset := f.asset(t, f.editor, file, "Before update", "private")
	f.review(t, asset.PublicID, "approved")
	connection := f.db.Config().Copy()
	connection.MaxConns, connection.MinConns = 1, 0
	pool, err := pgxpool.NewWithConfig(f.ctx, connection)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	server := &Server{db: pool, cfg: f.server.cfg}
	ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, claimsContextKey, security.Claims{Subject: f.userIDs[f.editor]})
	request := httptest.NewRequest(http.MethodPut, "/api/v1/skins/"+asset.PublicID, bytes.NewBufferString(`{"name":"After update"}`)).WithContext(ctx)
	response := httptest.NewRecorder()
	server.updateSkinDetail(response, request, asset.PublicID)
	if err = ctx.Err(); err != nil {
		t.Fatalf("skin update exhausted its transaction's only connection while reading review configuration: %v", err)
	}
	if response.Code != http.StatusOK {
		t.Fatalf("single-connection skin update status=%d body=%s", response.Code, response.Body.String())
	}
	var name string
	if err = pool.QueryRow(ctx, `select display_name from skin_assets where public_id=$1`, asset.PublicID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "After update" {
		t.Fatalf("successful skin update was not persisted: %q", name)
	}
}
