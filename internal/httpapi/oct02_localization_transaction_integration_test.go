package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/security"
)

func TestOCT02HumanLocalizationReadsReviewPolicyWithinSingleConnectionTransactionIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	var id int64
	var publicID string
	if err := f.db.QueryRow(f.ctx, `insert into catalog_entities(identity_key,entity_type,status) values('oct02-localization-tx','tag','active') returning id,public_id`).Scan(&id, &publicID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into catalog_tags(entity_id,registry,canonical_id) values($1,'minecraft:item','oct02:localized')`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into content_subjects(subject_type,subject_id,default_locale) values('tag',$1,'en-US') on conflict(subject_type,subject_id) do nothing`, id); err != nil {
		t.Fatal(err)
	}
	pc := f.db.Config()
	pc.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(f.ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(f.ctx, 2*time.Second)
	defer cancel()
	r := httptest.NewRequest(http.MethodPut, "/api/v1/content/"+publicID, strings.NewReader(`{"locale":"en-US","name":"Synthetic localization","contentMarkdown":"Synthetic content","reason":"Synthetic single connection"}`))
	r.SetPathValue("publicId", publicID)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "content.write", "content.no-review")
	server := f.origin.Config.Handler.(*Server)
	claims, err := security.ParseToken(server.cfg.JWTSecret, f.editor)
	if err != nil {
		t.Fatal(err)
	}
	if err = server.resolveClaimsSubject(f.ctx, &claims); err != nil {
		t.Fatal(err)
	}
	r = r.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	w := httptest.NewRecorder()
	(&Server{db: pool}).updateCatalogEntityContent(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("single connection localization status=%d want200 body=%s", w.Code, w.Body.String())
	}
	var name string
	if err := pool.QueryRow(f.ctx, `select name from content_localizations where subject_type='tag' and subject_id=$1 and locale='en-US'`, id).Scan(&name); err != nil || name != "Synthetic localization" {
		t.Fatalf("localized edit lost name=%q err=%v", name, err)
	}
}
