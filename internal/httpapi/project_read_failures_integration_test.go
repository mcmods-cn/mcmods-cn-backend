package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProjectReadsDistinguishMissingRowsAndUnavailableReadsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	base := newOCT02IsolatedDatabase(t, ctx)
	if _, err := base.Exec(ctx, `insert into modpacks(slug,primary_name,review_status) values('read-status','Read status','approved');
 insert into simple_projects(project_type,slug,primary_name,review_status) values('plugin','read-status','Read status','approved')`); err != nil {
		t.Fatal(err)
	}
	counter := &integrationQueryCounter{}
	pc := base.Config()
	pc.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	s := &Server{db: pool}
	unavailable, stop := context.WithCancel(ctx)
	stop()
	for _, tc := range []struct {
		name, method string
		handler      http.HandlerFunc
		history      bool
	}{
		{"modpack detail", "GET", s.modpackItem, false}, {"project detail", "GET", s.simpleProjectItem, false},
		{"modpack editor", "GET", s.modpackEditor, false}, {"project editor", "GET", s.simpleProjectEditor, false},
		{"modpack revision", "PUT", s.modpackItem, false}, {"project revision", "PUT", s.simpleProjectItem, false},
		{"modpack history", "GET", s.modpackHistory, true}, {"project history", "GET", s.simpleProjectHistory, true},
		{"modpack gallery", "GET", s.modpackGalleryImage, false}, {"project gallery", "GET", s.simpleProjectGalleryImage, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read := func(requestCtx context.Context, site string) int {
				r := httptest.NewRequestWithContext(requestCtx, tc.method, "/projects", nil)
				r.SetPathValue("projectType", "plugin")
				r.SetPathValue("siteId", site)
				r.SetPathValue("publicId", "d12445678")
				w := httptest.NewRecorder()
				tc.handler(w, r)
				return w.Code
			}
			if code := read(ctx, "missing-project"); code != http.StatusNotFound {
				t.Errorf("missing target = %d", code)
			}
			if code := read(unavailable, "read-status"); code != http.StatusInternalServerError {
				t.Errorf("failed database read = %d", code)
			}
			if tc.history {
				counter.queries.Store(0)
				if code := read(ctx, "read-status"); code != http.StatusOK {
					t.Fatalf("approved history = %d", code)
				}
				count := counter.queries.Load()
				t.Logf("queries to read approved history: %d", count)
				if count != 2 {
					t.Errorf("history queried %d times; need parent and history page", count)
				}
			}
		})
	}
}
