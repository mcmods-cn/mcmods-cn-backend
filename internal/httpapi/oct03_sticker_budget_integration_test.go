package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

// Real nonce PostgreSQL schema and production handlers/OSS SDK; only the
// external object store is the existing owned loopback protocol peer.
func TestOCT03StickerConcurrentBudgetAdmissionAndBoundedPublicCatalogIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	var databaseName string
	if err := f.db.QueryRow(f.ctx, "select current_database()").Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(databaseName, "test018_project_update_") {
		t.Fatalf("refuse fixture writes outside the nonce child database: %q", databaseName)
	}
	var initial int
	if err := f.db.QueryRow(f.ctx, "select count(*) from sticker_packs").Scan(&initial); err != nil || initial != 0 {
		t.Fatalf("fresh catalog packs=%d error=%v", initial, err)
	}
	f.server.cfg.Sticker = config.StickerConfig{MaxPacks: 3, MaxStickersPerPack: 2, MaxCatalogItems: 3}
	claims := security.Claims{Subject: f.userIDs[f.editor], PermissionRules: []security.PermissionRule{{Code: "sticker.upload", Allow: true, Priority: 100}}}
	names := stickerTranslationMap{}
	for _, locale := range editableStickerLocales() {
		names[locale] = "Synthetic budget fixture"
	}
	invoke := func(pack string, body any, handler func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		if err != nil {
			panic(err) // Fixed typed synthetic payloads; no testing.Fatal from a worker.
		}
		r := httptest.NewRequest(http.MethodPost, "/owned-sticker-budget", bytes.NewReader(raw))
		r.SetPathValue("packCode", pack)
		r = r.WithContext(context.WithValue(f.ctx, claimsContextKey, claims))
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}
	concurrent := func(t *testing.T, count int, run func(int) *httptest.ResponseRecorder) {
		t.Helper()
		start := make(chan struct{})
		results := make(chan *httptest.ResponseRecorder, count)
		for index := 0; index < count; index++ {
			go func(index int) { <-start; results <- run(index) }(index)
		}
		close(start)
		created, rejected := 0, 0
		var failures []string
		for index := 0; index < count; index++ {
			result := <-results
			switch result.Code {
			case http.StatusCreated:
				created++
			case http.StatusConflict:
				if !strings.Contains(result.Body.String(), "STICKER_CATALOG_LIMIT") {
					failures = append(failures, fmt.Sprintf("unexpected conflict: %s", result.Body.String()))
				}
				rejected++
			default:
				failures = append(failures, fmt.Sprintf("admission status=%d body=%s", result.Code, result.Body.String()))
			}
		}
		// Drain every writer before failing so fixture cleanup cannot race an in-flight handler.
		if len(failures) > 0 {
			t.Fatalf("admission errors after all writers completed: %s", strings.Join(failures, "; "))
		}
		if created != 3 || rejected != count-3 {
			t.Fatalf("concurrent admission created/rejected=%d/%d; want 3/%d", created, rejected, count-3)
		}
	}
	t.Run("pack limit serializes concurrent production creates", func(t *testing.T) {
		concurrent(t, 8, func(index int) *httptest.ResponseRecorder {
			return invoke("", stickerPackMutation{Code: fmt.Sprintf("oct03_pack_%d", index), Status: "active", Translations: names}, f.server.createStickerPack)
		})
	})
	rows, err := f.db.Query(f.ctx, "select code from sticker_packs order by code")
	if err != nil {
		t.Fatal(err)
	}
	var packs []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		packs = append(packs, code)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(packs) != 3 {
		t.Fatalf("pack facts=%v error=%v", packs, err)
	}
	files := make([]string, 8)
	for index := range files {
		canvas := image.NewNRGBA(image.Rect(0, 0, 1, 1))
		canvas.SetNRGBA(0, 0, color.NRGBA{R: uint8(index + 1), G: 50, B: 150, A: 255})
		var raw bytes.Buffer
		if err := png.Encode(&raw, canvas); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(raw.Bytes())
		hash := hex.EncodeToString(digest[:])
		key := fmt.Sprintf("oct03/sticker-source-%d.png", index)
		f.store.mu.Lock()
		f.store.objects[key] = test036Object{data: bytes.Clone(raw.Bytes()), contentType: "image/png", hash: hash}
		f.store.mu.Unlock()
		if err := f.db.QueryRow(f.ctx, `insert into oss_files(object_key,original_name,content_type,size_bytes,sha256,uploader_id,source,bucket,endpoint,region,status,scan_status)
			values($1,'source.png','image/png',$2,$3,$4,'sticker-upload:oct03',$5,$6,$7,'active','clean') returning public_id`,
			key, raw.Len(), hash, claims.Subject, f.cfg.Bucket, f.cfg.Endpoint, f.cfg.Region).Scan(&files[index]); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("global and per-pack limits fence concurrent image creates", func(t *testing.T) {
		concurrent(t, len(files), func(index int) *httptest.ResponseRecorder {
			return invoke(packs[index%2], stickerMutation{Code: fmt.Sprintf("oct03_sticker_%d", index), ImageFileID: files[index], Status: "active", Translations: names}, f.server.createSticker)
		})
		var total, maximum int
		if err := f.db.QueryRow(f.ctx, `select (select count(*) from stickers),coalesce((select max(n) from (select count(*) n from stickers group by pack_id) counts),0)`).Scan(&total, &maximum); err != nil || total != 3 || maximum > 2 {
			t.Fatalf("persisted catalog total/maxPack=%d/%d error=%v", total, maximum, err)
		}
	})
	t.Run("public response is bounded and over-budget rows fail closed", func(t *testing.T) {
		read := func() *httptest.ResponseRecorder {
			w := httptest.NewRecorder()
			f.server.publicStickerCatalog(w, httptest.NewRequest(http.MethodGet, "/api/v1/stickers?locale=en-US", nil).WithContext(f.ctx))
			return w
		}
		good := read()
		if good.Code != http.StatusOK || good.Body.Len() > 4096 {
			t.Fatalf("public response=%d/%d bytes: %s", good.Code, good.Body.Len(), good.Body.String())
		}
		var envelope struct {
			Data struct {
				Packs []struct {
					Stickers []json.RawMessage `json:"stickers"`
				} `json:"packs"`
			} `json:"data"`
		}
		if err := json.Unmarshal(good.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, pack := range envelope.Data.Packs {
			if len(pack.Stickers) > 2 {
				t.Fatal("public pack overflow")
			}
			count += len(pack.Stickers)
		}
		if count != 3 || len(envelope.Data.Packs) > 3 {
			t.Fatalf("public catalog count=%d packs=%d", count, len(envelope.Data.Packs))
		}
		f.server.cfg.Sticker.MaxCatalogItems = 2
		bad := read()
		if bad.Code != http.StatusInternalServerError || strings.Contains(bad.Body.String(), "imageURL") {
			t.Fatalf("over-budget public response must be failure without a partial catalog: %d %s", bad.Code, bad.Body.String())
		}
	})
}
