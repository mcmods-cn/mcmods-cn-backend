package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type skinMutationObservationKey struct{}
type skinMutationCommitKey struct{}

type skinMutationObservation struct {
	committed       bool
	lateSQLAttempts int
}

type skinMutationResultTracer struct{}

func (skinMutationResultTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	observation, _ := ctx.Value(skinMutationObservationKey{}).(*skinMutationObservation)
	if observation == nil {
		return ctx
	}
	if observation.committed {
		observation.lateSQLAttempts++
		// Simulate loss of a new query after an acknowledged commit. Keep the
		// HTTP request and the independent facts query usable.
		failed, cancel := context.WithCancel(ctx)
		cancel()
		return failed
	}
	if strings.EqualFold(strings.TrimSpace(data.SQL), "commit") {
		return context.WithValue(ctx, skinMutationCommitKey{}, observation)
	}
	return ctx
}

func (skinMutationResultTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if observation, ok := ctx.Value(skinMutationCommitKey{}).(*skinMutationObservation); ok && data.Err == nil {
		observation.committed = true
	}
}

func TestSkinMutationRespondsFromCommittedFactsIntegration(t *testing.T) {
	f := newTEST039Fixture(t)
	file := f.upload(t, f.editor, "approved-control.png", test039PNG(t, 64, 64, 106))
	asset := f.asset(t, f.editor, file, "Approved control", "public")
	f.review(t, asset.PublicID, "approved")
	profile := f.profile(t, f.editor, "ResponseControl", "private")
	newFile := f.upload(t, f.editor, "new-image.png", test039PNG(t, 64, 64, 107))
	profilePath := "/api/v1/users/me/player-profiles/" + profile.PublicID

	cfg := f.db.Config()
	cfg.ConnConfig.Tracer = skinMutationResultTracer{}
	pool, err := pgxpool.NewWithConfig(f.ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	server := NewServer(f.ctx, f.server.cfg, pool, nil, f.server.cache, nil, nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})

	for _, mutation := range []struct {
		name, method, path string
		body               any
		wantStatus         int
		query              string
		args               []any
		field, value       string
	}{
		{
			"submit skin", http.MethodPost, "/api/v1/skins", skinAssetCreateRequest{
				FileID: newFile, Kind: "skin", Name: "New pending image", Visibility: "public",
				DefaultLocale: "en-US", Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "New pending image"}},
			}, http.StatusCreated,
			`select count(*) from skin_assets asset join skin_texture_blobs blob on blob.hash=asset.blob_hash
			 join oss_files file on file.id=blob.oss_file_id and file.status='active'
			 join change_requests request on request.aggregate_type='skin' and request.aggregate_key=asset.public_id and request.status='pending'
			 where asset.owner_id=$1 and asset.display_name=$2 and asset.review_status='pending'`,
			[]any{f.userIDs[f.editor], "New pending image"}, "name", "New pending image",
		},
		{
			"publish skin edit", http.MethodPut, "/api/v1/skins/" + asset.PublicID,
			map[string]string{"name": "Approved update"}, http.StatusOK,
			`select count(*) from skin_assets asset join change_requests request
			 on request.proposed_revision_id=asset.published_revision_id and request.status='approved'
			 where asset.public_id=$1 and asset.display_name=$2`,
			[]any{asset.PublicID, "Approved update"}, "name", "Approved update",
		},
		{
			"create profile", http.MethodPost, "/api/v1/users/me/player-profiles",
			playerProfileCreateRequest{Name: "ResponseCreated", Visibility: "private"}, http.StatusCreated,
			`select count(*) from player_profiles where user_id=$1 and name=$2 and status='active'`,
			[]any{f.userIDs[f.editor], "ResponseCreated"}, "name", "ResponseCreated",
		},
		{
			"edit profile", http.MethodPut, profilePath,
			map[string]string{"bio": "Saved profile bio"}, http.StatusOK,
			`select count(*) from player_profiles where public_id=$1 and bio=$2`,
			[]any{profile.PublicID, "Saved profile bio"}, "bio", "Saved profile bio",
		},
		{
			"equip skin", http.MethodPut, profilePath + "/textures",
			map[string]string{"skinPublicId": asset.PublicID}, http.StatusOK,
			`select count(*) from player_profile_textures texture join player_profiles profile on profile.id=texture.profile_id
			 join skin_assets asset on asset.id=texture.asset_id where profile.public_id=$1 and asset.public_id=$2 and texture.kind='skin'`,
			[]any{profile.PublicID, asset.PublicID}, "skin.publicId", asset.PublicID,
		},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			var before, after int
			activities := func() int {
				t.Helper()
				var count int
				if err := f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where user_id=$1`, f.userIDs[f.editor]).Scan(&count); err != nil {
					t.Fatal(err)
				}
				return count
			}
			before = activities()
			body, err := json.Marshal(mutation.body)
			if err != nil {
				t.Fatal(err)
			}
			var observed skinMutationObservation
			ctx := context.WithValue(f.ctx, skinMutationObservationKey{}, &observed)
			request := httptest.NewRequestWithContext(ctx, mutation.method, mutation.path, bytes.NewReader(body))
			request.Header.Set("Authorization", "Bearer "+f.editor)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			var facts int
			if err = f.db.QueryRow(f.ctx, mutation.query, mutation.args...).Scan(&facts); err != nil {
				t.Fatal(err)
			}
			after = activities()
			if response.Code != mutation.wantStatus || !observed.committed || observed.lateSQLAttempts != 0 || facts != 1 || after-before != 1 {
				t.Fatalf("status=%d want=%d committed=%v post-commit SQL attempts=%d durable facts=%d durable activities=%d response=%s",
					response.Code, mutation.wantStatus, observed.committed, observed.lateSQLAttempts, facts, after-before, response.Body)
			}
			result := decodeTEST022Data[map[string]any](t, response.Body.Bytes())
			var value any = result
			for _, part := range strings.Split(mutation.field, ".") {
				row, _ := value.(map[string]any)
				value = row[part]
			}
			if result["publicId"] == "" || result["publicId"] == nil || value != mutation.value {
				t.Fatalf("mutation response does not identify the committed record: %s", response.Body)
			}
		})
	}
}
