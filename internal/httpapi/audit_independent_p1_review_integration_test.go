package httpapi

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Independent acceptance cases fill specific holes found while comparing the
// original P1 counterexamples with implementation authors' evidence.
func TestIndependentP1FID001EmailLookupAmbiguityHasZeroWrites(t *testing.T) {
	f := newTEST013Fixture(t)
	sender := f.userIDs[f.editor]
	grantTEST044Permissions(t, f.ctx, f.db, sender, "economy.transfer")
	var currency int64
	if err := f.db.QueryRow(f.ctx, `update currencies set transfer_tax_bps=0 where code='diamond' returning id`).Scan(&currency); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into user_currency_balances(user_id,currency_id,balance) values($1,$2,1000)`, sender, currency); err != nil {
		t.Fatal(err)
	}
	var target, decoy int64
	if err := f.db.QueryRow(f.ctx, `insert into users(public_id,username,email,password_hash,status) values('mailtarg1','independent-mail-target','independent@example.test','unused','active') returning id`).Scan(&target); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `insert into users(public_id,username,email,password_hash,status) values('maildecoy','independent-mail-decoy','decoy@example.test','unused','active') returning id`).Scan(&decoy); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/users/me/economy/transfer"
	f.require(t, f.editor, http.MethodPost, path, map[string]any{"recipient": "INDEPENDENT@EXAMPLE.TEST", "currency": "diamond", "amount": 100}, 200)
	assertTEST044Balances(t, f.ctx, f.db, currency, map[int64]int64{sender: 900, target: 100})
	assertTEST044TransferLedger(t, f.ctx, f.db, currency, 2)
	// This valid legacy DB state deliberately collides across two public lookup
	// fields; it does not claim the current registration UI admits '@' names.
	if _, err := f.db.Exec(f.ctx, `update users set username='independent@example.test' where id=$1`, decoy); err != nil {
		t.Fatal(err)
	}
	raw := f.require(t, f.editor, http.MethodPost, path, map[string]any{"recipient": "independent@example.test", "currency": "diamond", "amount": 100}, 409)
	if !bytes.Contains(raw, []byte("TRANSFER_RECIPIENT_AMBIGUOUS")) {
		t.Fatalf("wrong conflict contract: %s", raw)
	}
	assertTEST044Balances(t, f.ctx, f.db, currency, map[int64]int64{sender: 900, target: 100})
	assertTEST044TransferLedger(t, f.ctx, f.db, currency, 2)
	f.require(t, f.editor, http.MethodPost, path, map[string]any{"recipientPublicId": "mailtarg1", "currency": "diamond", "amount": 100}, 200)
	assertTEST044Balances(t, f.ctx, f.db, currency, map[int64]int64{sender: 800, target: 200})
	var wrongBalance int64
	if err := f.db.QueryRow(f.ctx, `select coalesce((select balance from user_currency_balances where user_id=$1 and currency_id=$2),0)`, decoy, currency).Scan(&wrongBalance); err != nil || wrongBalance != 0 {
		t.Fatalf("wrong recipient funded: %d/%v", wrongBalance, err)
	}
	assertTEST044TransferLedger(t, f.ctx, f.db, currency, 4)
}

func TestIndependentP1CMP001PublicProtocolAndAuthorizedPrivateJoinRemainCompatible(t *testing.T) {
	f := newTEST039Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "skin.launcher.login")
	profile := f.profile(t, f.editor, "PublicJoinCheck", "public")
	const password = "independent-synthetic-launcher-password"
	f.require(t, f.editor, http.MethodPut, "/api/v1/users/me/launcher-credential", map[string]string{"password": password}, 200)
	var auth yggdrasilAuthenticationResponse
	if err := json.Unmarshal(f.require(t, "", http.MethodPost, "/api/yggdrasil/authserver/authenticate", map[string]any{"username": profile.Name, "password": password, "clientToken": "independent-public-client", "requestUser": true}, 200), &auth); err != nil {
		t.Fatal(err)
	}
	if auth.AccessToken == "" || auth.SelectedProfile == nil || auth.SelectedProfile.Name != profile.Name {
		t.Fatal("real ordinary launcher authentication did not select public profile")
	}
	const serverID = "independent-controlled-server-hash"
	f.require(t, "", http.MethodPost, "/api/yggdrasil/sessionserver/session/minecraft/join", map[string]string{"accessToken": auth.AccessToken, "selectedProfile": auth.SelectedProfile.ID, "serverId": serverID}, 204)
	_, service := f.server.yggdrasilRuntimeSnapshot()
	checkSigned := func(path string) {
		var got yggdrasilProfile
		if err := json.Unmarshal(f.require(t, "", http.MethodGet, path, nil, 200), &got); err != nil {
			t.Fatal(err)
		}
		if got.ID != auth.SelectedProfile.ID || got.Name != profile.Name || len(got.Properties) == 0 {
			t.Fatal("protocol identity changed")
		}
		signature, err := base64.StdEncoding.DecodeString(got.Properties[0].Signature)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha1.Sum([]byte(got.Properties[0].Value))
		if err = rsa.VerifyPKCS1v15(&service.privateKey.PublicKey, crypto.SHA1, digest[:], signature); err != nil {
			t.Fatal("actual RSA textures signature is invalid")
		}
	}
	publicPath := "/api/yggdrasil/sessionserver/session/minecraft/profile/" + auth.SelectedProfile.ID + "?unsigned=false"
	joinPath := "/api/yggdrasil/sessionserver/session/minecraft/hasJoined?username=" + profile.Name + "&serverId=" + serverID
	checkSigned(publicPath)
	checkSigned(joinPath)
	f.require(t, f.editor, http.MethodPut, "/api/v1/users/me/player-profiles/"+profile.PublicID, map[string]any{"visibility": "private"}, 200)
	f.require(t, "", http.MethodGet, publicPath, nil, 204)
	// A valid join proof remains scoped to the owner-issued access token and
	// server hash. It is distinct from public anonymous UUID enumeration.
	checkSigned(joinPath)
	f.require(t, "", http.MethodGet, "/api/yggdrasil/sessionserver/session/minecraft/hasJoined?username="+profile.Name+"&serverId=unrelated-server", nil, 204)
	t.Log("Real HTTP ordinary password authentication -> join -> hasJoined + signed public profile, RSA verified; private withdrawal denies public UUID while existing authorized join proof stays valid, unrelated server204; no real Minecraft launcher claimed")
}

func TestIndependentP1CMP001PublicUUIDDoesNotFetchNewPrivateNameAfterVisibilityCheck(t *testing.T) {
	f := newTEST039Fixture(t)
	profile := f.profile(t, f.editor, "PublicRaceName", "public")
	var withdrew atomic.Bool
	var once sync.Once
	cfg := f.db.Config()
	cfg.ConnConfig.Tracer = independentQueryStartHook(func(data pgx.TraceQueryStartData) {
		if strings.Contains(data.SQL, "select id,uuid::text,name from player_profiles where id=$1") {
			once.Do(func() {
				f.require(t, f.editor, http.MethodPut, "/api/v1/users/me/player-profiles/"+profile.PublicID, map[string]any{"name": "PrivateRaceName", "visibility": "private"}, 200)
				withdrew.Store(true)
			})
		}
	})
	pool, err := pgxpool.NewWithConfig(f.ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	s := NewServer(f.ctx, f.server.cfg, pool, nil, f.server.cache, nil, nil)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	}()
	origin := httptest.NewServer(s)
	defer origin.Close()
	copy := f.test013Fixture
	copy.origin = origin
	raw := copy.require(t, "", http.MethodGet, "/api/yggdrasil/sessionserver/session/minecraft/profile/"+strings.ReplaceAll(profile.UUID, "-", "")+"?unsigned=false", nil, 204)
	if !withdrew.Load() || bytes.Contains(raw, []byte("PrivateRaceName")) {
		t.Fatal("visibility transition barrier missing or private name exposed")
	}
	t.Log("Actual owner HTTP rename+private commits between UUID visibility lookup and record construction; anonymous signedprofile must remain204, never return newly private name")
}

func TestIndependentP1RPH002SinglePreviewCannotReadHistoricalHiddenSnapshot(t *testing.T) {
	f := newTEST013Fixture(t)
	var revisionID int64
	var publicID string
	if err := f.db.QueryRow(f.ctx, `insert into content_revisions(entity_type,entity_id,aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash,created_by,source) values('mod',$1,'mod',$2,1,'{"siteId":"test013-mod","primaryName":"Independent hidden history","bodyMarkdown":"INDEPENDENT_HIDDEN_BODY"}','independent-review-hash',$3,'audit') returning id,public_id`, f.modID, f.modCode, f.userIDs[f.editor]).Scan(&revisionID, &publicID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into change_requests(entity_type,entity_id,aggregate_type,aggregate_key,proposed_revision_id,status,submitted_by) values('mod',$1,'mod',$2,$3,'approved',$4)`, f.modID, f.modCode, revisionID, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "project.review."+f.otherModCode)
	for _, state := range []string{"approved", "pending", "rejected"} {
		if _, err := f.db.Exec(f.ctx, `update mods set published_revision_id=$1,review_status=$2 where id=$3`, revisionID, state, f.modID); err != nil {
			t.Fatal(err)
		}
		// There is no GET /mods/.../revisions/{id} endpoint. The sole single
		// proposal endpoint is authenticated and restricted to pending review.
		f.require(t, "", http.MethodGet, "/api/v1/content-revisions/"+publicID, nil, 401)
		for _, token := range []string{f.editor, f.otherEditor, f.reviewer, f.denied} {
			raw := f.require(t, token, http.MethodGet, "/api/v1/content-revisions/"+publicID, nil, 404)
			if bytes.Contains(raw, []byte("INDEPENDENT_HIDDEN_BODY")) {
				t.Error("single-revision response leaked history")
			}
		}
	}
}

func TestIndependentP1CMP001PublicProfileOmitsNonpublicSkinAndRechecksCache(t *testing.T) {
	f := newTEST039Fixture(t)
	yc, service := f.server.yggdrasilRuntimeSnapshot()
	yc.PublicBaseURL = f.origin.URL + "/api/yggdrasil/"
	yc.TextureBaseURL = f.origin.URL + "/api/yggdrasil/textures/"
	f.server.replaceYggdrasilRuntime(yc, service)
	raw := test039PNG(t, 64, 64, 126)
	file := f.upload(t, f.editor, "independent-texture.png", raw)
	asset := f.asset(t, f.editor, file, "Independent public skin", "public")
	f.review(t, asset.PublicID, "approved")
	profile := f.profile(t, f.editor, "IndependentP1", "public")
	f.require(t, f.editor, http.MethodPut, "/api/v1/users/me/player-profiles/"+profile.PublicID+"/textures", map[string]any{"skinPublicId": asset.PublicID}, 200)
	profilePath := "/api/yggdrasil/sessionserver/session/minecraft/profile/" + strings.ReplaceAll(profile.UUID, "-", "") + "?unsigned=false"
	decode := func() yggdrasilTexturePayload {
		t.Helper()
		var p yggdrasilProfile
		if err := json.Unmarshal(f.require(t, "", http.MethodGet, profilePath, nil, 200), &p); err != nil {
			t.Fatal(err)
		}
		if len(p.Properties) == 0 || p.Properties[0].Signature == "" {
			t.Fatal("public profile lost signature")
		}
		decoded, err := base64.StdEncoding.DecodeString(p.Properties[0].Value)
		if err != nil {
			t.Fatal(err)
		}
		var textures yggdrasilTexturePayload
		if err = json.Unmarshal(decoded, &textures); err != nil {
			t.Fatal(err)
		}
		return textures
	}
	if decode().Textures["SKIN"].URL == "" {
		t.Fatal("public profile skin missing")
	}
	for _, state := range []string{"pending", "rejected", "approved"} {
		// Precisely reach persisted valid review states on an already-equipped
		// fixture, independently of administrative transition policy.
		if _, err := f.db.Exec(f.ctx, `update skin_assets set review_status=$2 where public_id=$1`, asset.PublicID, state); err != nil {
			t.Fatal(err)
		}
		textures := decode()
		want := state == "approved"
		if (textures.Textures["SKIN"].URL != "") != want {
			t.Errorf("skin state %s profile exposure=%+v", state, textures.Textures)
		}
		wantStatus := 404
		if want {
			wantStatus = 304
		}
		f.conditionalTexture(t, asset.TextureURL, asset.TextureHash, wantStatus)
	}
	f.require(t, f.editor, http.MethodPut, "/api/v1/users/me/player-profiles/"+profile.PublicID, map[string]any{"visibility": "private"}, 200)
	f.require(t, f.editor, http.MethodPut, "/api/v1/skins/"+asset.PublicID, map[string]any{"visibility": "private"}, 200)
	f.conditionalTexture(t, asset.TextureURL, asset.TextureHash, 404)
	f.require(t, "", http.MethodGet, profilePath, nil, 204)
	assertTEST039TextureBytes(t, f.require(t, f.editor, http.MethodGet, asset.TextureURL, nil, 200), asset.TextureHash)
	t.Log("Independent actualPNG/public signedprofile -> pending/rejected omit skin, conditional404; approved304; real profile+skin withdrawal denies anonymous but preserves owner")
}

func TestIndependentP1CONT001NumericPublicIDReviewUsesExactObjectHTTP(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "content.write")
	body := map[string]any{"reason": "Independent typed identity", "kindCode": "minecraft.item", "canonicalId": "independent:numeric-review", "defaultLocale": "en-US", "localizations": []map[string]any{{"locale": "en-US", "name": "Independent numeric resource"}}, "definition": map[string]any{}}
	var decoyID, targetID int64
	var revisionID int64
	if err := f.db.QueryRow(f.ctx, `insert into catalog_entities(identity_key,entity_type,status) values('independent:numeric-decoy','resource','placeholder') returning id`).Scan(&decoyID); err != nil {
		t.Fatal(err)
	}
	numeric := fmt.Sprintf("%09d", decoyID)
	// Seed a valid legacy placeholder before HTTP submission. The normal
	// service resolves its existing identity; immutable history is never edited.
	identity := resourceIdentity("minecraft.item", "independent:numeric-review")
	if err := f.db.QueryRow(f.ctx, `insert into catalog_entities(identity_key,public_id,entity_type,status) values($1,$2,'resource','placeholder') returning id`, identity.ID, numeric).Scan(&targetID); err != nil {
		t.Fatal(err)
	}
	created := decodeTEST022Data[catalogEditResult](t, f.require(t, f.editor, http.MethodPost, "/api/v1/catalog/resources", body, 202))
	if created.ObjectPublicID != numeric {
		t.Fatalf("HTTP did not use legacy numeric public identity: %s", created.ObjectPublicID)
	}
	if err := f.db.QueryRow(f.ctx, `select id from content_revisions where public_id=$1`, created.RevisionID).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+created.RevisionID, map[string]any{"status": "approved"}, 200)
	var actual *int64
	var decoy *int64
	if err := f.db.QueryRow(f.ctx, `select published_revision_id from catalog_entities where id=$1`, targetID).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `select published_revision_id from catalog_entities where id=$1`, decoyID).Scan(&decoy); err != nil {
		t.Fatal(err)
	}
	if actual == nil || *actual != revisionID || decoy != nil {
		t.Fatalf("review selected wrong numeric object: target=%v decoy=%v", actual, decoy)
	}
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+created.RevisionID, map[string]any{"status": "approved"}, 409)
}
