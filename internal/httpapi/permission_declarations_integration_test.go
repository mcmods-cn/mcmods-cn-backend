package httpapi

import (
	"net/http"
	"testing"
)

func TestWarmRouteRegistrationPreservesRBACVersionIntegration(t *testing.T) {
	ctx := t.Context()
	db := newTEST018IsolatedDatabase(t, ctx)
	readVersion := func() int64 {
		t.Helper()
		var v int64
		if err := db.QueryRow(ctx, "select version from runtime_versions where name='rbac'").Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	registerRoutes := func() {
		t.Helper()
		s := &Server{mux: http.NewServeMux(), db: db}
		codes := s.routes()
		if err := registerDeclaredPermissions(ctx, db, codes); err != nil {
			t.Fatal(err)
		}
		var allPresent bool
		if err := db.QueryRow(ctx, "select not exists(select unnest($1::text[]) except select code from permissions)", codes).Scan(&allPresent); err != nil || !allPresent {
			t.Fatalf("incomplete route permission catalogue err=%v", err)
		}
	}
	registerRoutes()
	version := readVersion()
	registerRoutes()
	if next := readVersion(); next != version {
		t.Fatalf("unchanged route catalogue advanced RBAC version from %d to %d", version, next)
	}
	const a, b = "fixture.ordinary.write", "fixture.ordinary.read"
	for _, persist := range []bool{false, true} {
		tx, err := db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = registerDeclaredPermissions(ctx, tx, []string{a, b, a}); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
		if persist {
			err = tx.Commit(ctx)
		} else {
			err = tx.Rollback(ctx)
		}
		if err != nil {
			t.Fatal(err)
		}
		var count int
		if err = db.QueryRow(ctx, "select count(*) from permissions where code in ($1,$2) and module='fixture'", a, b).Scan(&count); err != nil || (persist && count != 2) || (!persist && count != 0) {
			t.Fatalf("commit=%v count=%d err=%v", persist, count, err)
		}
		if persist {
			version++
		}
		if next := readVersion(); next != version {
			t.Fatalf("commit=%v version=%d want %d", persist, next, version)
		}
	}
}
