package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

func TestOCT02MetricDatabaseFailureReleasesOnlyItsOwnDedupeReservationIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	cache := querycache.New(config.RedisConfig{})
	t.Cleanup(func() { _ = cache.Close() })
	s := &Server{db: f.db, cache: cache}
	target := metricTarget{Type: "mod", InternalID: f.modID, PublicID: f.modCode}
	if err := f.db.QueryRow(f.ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, f.modID).Scan(&target.RouteID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `alter table content_view_daily add constraint oct02_reject_view check(views<0) not valid`); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/content/"+f.modCode+"/view", nil)
	r = r.WithContext(context.WithValue(f.ctx, claimsContextKey, security.Claims{Subject: f.userIDs[f.editor]}))
	if err := s.recordContentRouteView(r.Context(), r, target); err == nil {
		t.Fatal("injected storage failure returned success")
	}
	if _, err := f.db.Exec(f.ctx, `alter table content_view_daily drop constraint oct02_reject_view`); err != nil {
		t.Fatal(err)
	}
	if err := s.recordContentRouteView(r.Context(), r, target); err != nil {
		t.Fatalf("retry after storage repair failed: %v", err)
	}
	var views int
	if err := f.db.QueryRow(f.ctx, `select coalesce(sum(views),0) from content_view_daily where object_route_id=$1`, target.RouteID).Scan(&views); err != nil || views != 1 {
		t.Fatalf("repaired retry silently suppressed views=%d err=%v", views, err)
	}
	if err := s.recordContentRouteView(r.Context(), r, target); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `select coalesce(sum(views),0) from content_view_daily where object_route_id=$1`, target.RouteID).Scan(&views); err != nil || views != 1 {
		t.Fatalf("committed dedupe was lost views=%d err=%v", views, err)
	}
}
