package httpapi

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type slugAuditQuery struct {
	calls          int
	firstCollision bool
	err            error
}

func (q *slugAuditQuery) QueryRow(context.Context, string, ...any) pgx.Row {
	q.calls++
	if q.firstCollision && q.calls == 1 {
		return slugAuditRow{exists: true}
	}
	return slugAuditRow{err: q.err}
}

type slugAuditRow struct {
	exists bool
	err    error
}

func (r slugAuditRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*bool) = r.exists
	return nil
}
func TestSimpleProjectSlugStopsOnDatabaseFailure(t *testing.T) {
	failure := errors.New("synthetic database unavailable")
	for _, collision := range []bool{false, true} {
		t.Run(map[bool]string{false: "first-query", true: "after-collision"}[collision], func(t *testing.T) {
			query := &slugAuditQuery{firstCollision: collision, err: failure}
			_, err := availableSimpleProjectSiteID(context.Background(), query, "plugin", "Project")
			if !errors.Is(err, failure) {
				t.Fatalf("database failure replaced with allocation conflict: %v", err)
			}
			expected := 1
			if collision {
				expected = 2
			}
			if query.calls != expected {
				t.Fatalf("database failure retried %d times wanted%d", query.calls, expected)
			}
		})
	}
}
