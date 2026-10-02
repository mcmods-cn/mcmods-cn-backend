package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestFavoriteExportQueriesAndWorkerDoNotHideFailures(t *testing.T) {
	querySource, err := os.ReadFile("favorite_modpack_export_query.go")
	if err != nil {
		t.Fatal(err)
	}
	queryText := string(querySource)
	if strings.Count(queryText, "rows.Err()") < 2 {
		t.Fatal("export history/detail do not both check terminal row errors")
	}
	if strings.Contains(queryText, "_ = json.Unmarshal(dependencies") {
		t.Fatal("export detail still ignores dependency JSON corruption")
	}
	if !strings.Contains(queryText, "errors.Is(err, pgx.ErrNoRows)") {
		t.Fatal("export detail/download do not distinguish missing rows from database failures")
	}

	workerSource, err := os.ReadFile("favorite_modpack_export_worker.go")
	if err != nil {
		t.Fatal(err)
	}
	workerText := string(workerSource)
	for _, forbidden := range []string{
		"_ = worker.process(ctx, id)",
		"_ = worker.server.db.QueryRow",
		"_, _ = worker.server.db.Exec",
		"rows.Scan(&id) == nil",
		"rows.Scan(&taskID, &ownerID, &name) == nil",
		"if rows.Scan(&artifact.taskID",
	} {
		if strings.Contains(workerText, forbidden) {
			t.Fatalf("favorite export worker still hides failure through %q", forbidden)
		}
	}
	for _, required := range []string{
		"func (worker *FavoriteModpackExportWorker) processPending(ctx context.Context) error",
		"func (worker *FavoriteModpackExportWorker) failExhaustedLeases(ctx context.Context) error",
		"func (worker *FavoriteModpackExportWorker) expireCompleted(ctx context.Context) error",
		"if err = rows.Err(); err != nil",
		"tag.RowsAffected() != 1",
	} {
		if !strings.Contains(workerText, required) {
			t.Fatalf("favorite export worker is missing observable boundary %q", required)
		}
	}
}
