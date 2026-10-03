package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOCT02AntiAbuseAdministrationStorageFailuresRemainServerErrors(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://synthetic:synthetic@127.0.0.1:1/synthetic?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	s := &Server{db: pool}
	for _, c := range []struct {
		name, method, body string
		handle             http.HandlerFunc
	}{
		{"review event", http.MethodPatch, `{"disposition":"acknowledged"}`, s.adminReviewAntiAbuseEvent},
		{"user risk state", http.MethodPatch, `{"trustLevel":"normal"}`, s.adminUpdateAntiAbuseUserState},
		{"restrict user", http.MethodPost, `{"userId":"synthetic","mode":"no_comment","reason":"Synthetic restriction"}`, s.adminAntiAbuseRestrictions},
		{"lift restriction", http.MethodPatch, `{"reason":"Synthetic recovery"}`, s.adminLiftAntiAbuseRestriction},
		{"delete bot rule", http.MethodDelete, "", s.adminDeleteAntiAbuseBotRule},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(c.method, "/synthetic", strings.NewReader(c.body))
			r.SetPathValue("id", "synthetic")
			w := httptest.NewRecorder()
			c.handle(w, r)
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("closed storage status=%d want500 body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestOCT02AntiAbuseEventRowDecodeFailureDoesNotReturnEmptySuccessIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	if _, err := f.db.Exec(f.ctx, `create temp view anti_abuse_events as select
 null::text public_id,null::bigint user_id,'synthetic'::text action,''::text object_type,''::text object_key,
 'allow'::text outcome,0::integer risk_score,array[]::text[] rule_codes,''::text ip_hash,''::text subnet_hash,
 ''::text device_hash,''::text crawler_class,''::text content_hash,0::integer similarity,
 ''::text disposition,''::text review_note,now() created_at,1::bigint id`); err != nil {
		t.Fatal(err)
	}
	// This synthetic broken storage projection triggers actual pgx Scan failure;
	// a legitimate empty result is not used as a substitute for a failed read.
	r := httptest.NewRequest(http.MethodGet, "/synthetic-events", nil).WithContext(f.ctx)
	w := httptest.NewRecorder()
	(&Server{db: f.db}).adminAntiAbuseEvents(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("failed event row status=%d want500 body=%s", w.Code, w.Body.String())
	}
}
