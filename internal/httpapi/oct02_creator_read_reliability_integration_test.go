package httpapi

import (
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

func TestOCT02CreatorClaimReadFailureDoesNotReturnUnclaimedSuccessIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	var creatorID int64
	var publicID string
	if err := f.db.QueryRow(f.ctx, `insert into creators(kind,name,normalized_name,review_status) values('author','Synthetic claimed author','synthetic claimed author','approved') returning id,public_id`).Scan(&creatorID, &publicID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into creator_claims(creator_id,user_id,proof_markdown,status) values($1,$2,'Synthetic proof','approved')`, creatorID, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	f.require(t, "", http.MethodGet, "/api/v1/creators/"+publicID, nil, http.StatusOK)
	// Fail only the user-id projection: EXISTS in the collaborators query still
	// works. This reproduces the old false "unclaimed" success without removing
	// unrelated tables or mocking the handler's storage operations.
	if _, err := f.db.Exec(f.ctx, `alter table creator_claims rename to oct02_claim_store;
 create function oct02_claim_user_failure(bigint) returns bigint language plpgsql as $$ begin raise exception 'synthetic claim storage projection failure'; end $$;
 create view creator_claims as select creator_id,status,oct02_claim_user_failure(user_id) user_id from oct02_claim_store`); err != nil {
		t.Fatal(err)
	}
	f.require(t, "", http.MethodGet, "/api/v1/creators/"+publicID, nil, http.StatusInternalServerError)
}

func TestOCT02CreatorRolesRejectInvalidTranslationShapeIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	if _, err := f.db.Exec(f.ctx, `update creator_role_definitions set translations='"synthetic invalid map"'::jsonb where code='owner'`); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/creator-roles", nil).WithContext(f.ctx)
	w := httptest.NewRecorder()
	(&Server{db: f.db}).creatorRoles(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("malformed stored translation status=%d want500 body=%s", w.Code, w.Body.String())
	}
}

func TestOCT02CreatorDirectoryAuthorizesImageURLsInOneBatchIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	if _, err := f.db.Exec(f.ctx, `insert into creators(kind,name,normalized_name,avatar_url,review_status) select 'author','oct02-directory-'||n,'oct02-directory-'||n,'https://images.example.invalid/private-'||n||'.png','approved' from generate_series(1,100) n`); err != nil {
		t.Fatal(err)
	}
	counter := &integrationQueryCounter{}
	pc := f.db.Config()
	pc.MaxConns = 1
	pc.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(f.ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	s := &Server{db: pool, cfg: config.Config{SettingsEncryptionKey: "synthetic-directory-key-at-least-32-characters"}}
	raw, err := json.Marshal(ossConfigPayload{Bucket: "fixture", PublicEndpoint: "https://images.example.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := security.EncryptSetting(s.cfg.SettingsEncryptionKey, raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(f.ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb) on conflict(key) do update set value=excluded.value`, string(sealed)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
	defer cancel()
	counter.queries.Store(0)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/creators?q=oct02-directory-&limit=100&sort=published", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	s.creators(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("directory status=%d body=%s", w.Code, w.Body.String())
	}
	var response struct {
		Data struct {
			Items []creatorSummary `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data.Items) != 100 || counter.queries.Load() > 4 {
		t.Fatalf("items=%d queries=%d want100 <=4", len(response.Data.Items), counter.queries.Load())
	}
	for _, item := range response.Data.Items {
		if item.AvatarURL != "" {
			t.Fatalf("unbound private avatar leaked %q", item.AvatarURL)
		}
	}
}
