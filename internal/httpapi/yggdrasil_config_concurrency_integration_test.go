package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

// Both instances start with the same environment signing key and use different
// pools against the same owned database. Only synthetic administrator settings
// and the public metadata endpoint participate.
func TestYggdrasilRotationAndOrdinarySavesAcrossInstancesIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	base := newOCT02IsolatedDatabase(t, ctx)
	other, err := pgxpool.NewWithConfig(ctx, base.Config())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	cfg := config.Load()
	cfg.SettingsEncryptionKey = "yggdrasil-config-fixture-encryption"
	cfg.Yggdrasil.Enabled = true
	cfg.Yggdrasil.PrivateKeyBase64, err = generateYggdrasilPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	a := &Server{cfg: cfg, db: base, ygg: newYggdrasilService(cfg)}
	b := &Server{cfg: cfg, db: other, ygg: newYggdrasilService(cfg)}
	actor := newCurrentWriteTestActor(t, ctx, base, "config-rotation", "admin.config.write")

	type view struct {
		Revision, ServerName                string
		HasPrivateKey, PersistentPrivateKey bool
		PrivateKeyBase64                    *string
	}
	read := func(s *Server) view {
		w := httptest.NewRecorder()
		s.getYggdrasilConfig(w, httptest.NewRequestWithContext(ctx, http.MethodGet, "/settings", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("settings read returned %d", w.Code)
		}
		v := decodeTEST022Data[view](t, w.Body.Bytes())
		if v.PrivateKeyBase64 != nil {
			t.Fatal("administrative read exposed signing material")
		}
		return v
	}
	save := func(s *Server, revision, name string, rotate bool) *httptest.ResponseRecorder {
		next := yggdrasilPayloadFromConfig(cfg.Yggdrasil)
		next.ServerName, next.PrivateKeyBase64 = name, ""
		body, err := json.Marshal(next)
		if err != nil {
			t.Fatal(err)
		}
		var update map[string]any
		if err = json.Unmarshal(body, &update); err != nil {
			t.Fatal(err)
		}
		if revision != "" {
			update["revision"] = revision
		}
		update["rotatePrivateKey"] = rotate
		w := httptest.NewRecorder()
		s.updateYggdrasilConfig(w, newProjectHandlerRequest(t, ctx, actor, http.MethodPut, "/settings", update))
		return w
	}
	publicKey := func(s *Server) (string, int) {
		w := httptest.NewRecorder()
		handler := s.requireYggdrasilService(s.yggdrasilMetadata)
		s.yggdrasilALI(handler).ServeHTTP(w, httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/yggdrasil/", nil))
		if w.Code != http.StatusOK {
			return "", w.Code
		}
		var metadata struct{ SignaturePublickey string }
		if err := json.Unmarshal(w.Body.Bytes(), &metadata); err != nil || metadata.SignaturePublickey == "" {
			t.Fatal("invalid public metadata")
		}
		return metadata.SignaturePublickey, w.Code
	}
	beforeA, beforeB := read(a), read(b)
	old, code := publicKey(b)
	if code != 200 {
		t.Fatal("initial public metadata unavailable")
	}
	rotated := save(a, beforeA.Revision, "Rotated", true)
	if rotated.Code != 200 {
		t.Fatalf("rotation returned %d", rotated.Code)
	}
	newKey, code := publicKey(a)
	if code != 200 || newKey == old {
		t.Fatal("rotation did not publish a new key")
	}
	if otherKey, status := publicKey(b); status != 200 || otherKey != newKey {
		t.Error("second instance continued serving the pre-rotation public metadata")
	}
	stale := save(b, beforeB.Revision, "Old snapshot", false)
	if stale.Code != 409 || !strings.Contains(stale.Body.String(), "YGGDRASIL_CONFIG_CONFLICT") {
		t.Errorf("saving a pre-rotation snapshot returned %d", stale.Code)
	}
	current := read(b)
	if current.ServerName != "Rotated" || len(current.Revision) != 64 {
		t.Error("second instance did not read the latest redacted settings")
	}
	saved := save(b, current.Revision, "After rotation", false)
	if saved.Code != 200 {
		t.Fatalf("ordinary save after refresh returned %d", saved.Code)
	}
	receipt := decodeTEST022Data[view](t, saved.Body.Bytes())
	if receipt.PrivateKeyBase64 != nil || !receipt.HasPrivateKey || !receipt.PersistentPrivateKey || receipt.Revision == current.Revision {
		t.Error("ordinary save did not return a new redacted revision")
	}
	for _, server := range []*Server{a, b} {
		if key, status := publicKey(server); status != 200 || key != newKey || read(server).ServerName != "After rotation" {
			t.Error("ordinary save reverted the key or left a stale administrative view")
		}
	}
	// Both writers validated the same revision. The database makes exactly one
	// settings write durable; the other client keeps its draft on conflict.
	type result struct {
		code int
		name string
	}
	out := make(chan result, 2)
	for _, candidate := range []struct {
		node *Server
		name string
	}{{a, "First editor"}, {b, "Second editor"}} {
		go func() {
			out <- result{save(candidate.node, receipt.Revision, candidate.name, false).Code, candidate.name}
		}()
	}
	winner := ""
	conflicts := 0
	for range 2 {
		r := <-out
		switch r.code {
		case 200:
			if winner != "" {
				t.Error("both settings writes were accepted")
			}
			winner = r.name
		case 409:
			conflicts++
		default:
			t.Errorf("concurrent settings write returned %d", r.code)
		}
	}
	if conflicts != 1 || winner == "" || read(a).ServerName != winner || read(b).ServerName != winner {
		t.Error("competing edits did not converge on the committed settings")
	}
	if missing := save(a, "", "Missing precondition", false); missing.Code != 428 {
		t.Errorf("save without revision returned %d", missing.Code)
	}
	// A node with an incorrect deployment encryption key must not present its
	// startup key or defaults as the currently active persistent configuration.
	badConfig := cfg
	badConfig.SettingsEncryptionKey = "different-test-deployment-key"
	bad := &Server{cfg: badConfig, db: other, ygg: newYggdrasilService(badConfig)}
	if _, code = publicKey(bad); code != 503 {
		t.Errorf("unreadable persistent key returned public metadata status %d", code)
	}
	w := httptest.NewRecorder()
	bad.getYggdrasilConfig(w, httptest.NewRequestWithContext(ctx, http.MethodGet, "/settings", nil))
	if w.Code != 503 {
		t.Errorf("unreadable persistent settings returned administrative status %d", w.Code)
	}
}
