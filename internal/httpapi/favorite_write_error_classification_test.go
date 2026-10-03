package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestFavoriteCollectionWritesSeparateBusinessAndDatabaseFailures(t *testing.T) {
	raw, err := os.ReadFile("favorite_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	create := favoriteWriteHandlerSource(t, source, "createFavoriteCollection", "updateFavoriteCollection")
	update := favoriteWriteHandlerSource(t, source, "updateFavoriteCollection", "deleteFavoriteCollection")
	deleteHandler := favoriteWriteHandlerSource(t, source, "deleteFavoriteCollection", "favoriteMembershipSummary")

	for label, contract := range map[string][]string{
		"create": {
			"if isUniqueViolation(err)",
			`logFavoriteCollectionWriteFailure("create", "", userID, err)`,
			"http.StatusInternalServerError",
		},
		"update": {
			"if isUniqueViolation(err)",
			"errors.Is(err, pgx.ErrNoRows)",
			`logFavoriteCollectionWriteFailure("update", publicID, claims.Subject, err)`,
			"http.StatusInternalServerError",
		},
		"delete": {
			"s.beginFavoriteMembershipTx(r.Context(), claims.Subject)",
			"defer membershipTx.close()",
			"tx.QueryRow(r.Context()",
			"tx.Commit(r.Context())",
			"errors.Is(err, pgx.ErrNoRows)",
			`logFavoriteCollectionWriteFailure("delete", publicID, claims.Subject, err)`,
			"http.StatusInternalServerError",
		},
	} {
		body := map[string]string{"create": create, "update": update, "delete": deleteHandler}[label]
		for _, required := range contract {
			if !strings.Contains(body, required) {
				t.Errorf("%s handler does not enforce %q", label, required)
			}
		}
	}

	for _, required := range []string{
		"func logFavoriteCollectionWriteFailure(",
		`slog.Error("favorite collection write failed"`,
		`"module", "favorite"`,
		`"operation", operation`,
		`"collection_id", publicID`,
		`"user_id", userID`,
		`"error", err`,
	} {
		if !strings.Contains(source, required) {
			t.Errorf("favorite collection structured failure log is missing %q", required)
		}
	}
}

func favoriteWriteHandlerSource(t *testing.T, source, startName, endName string) string {
	t.Helper()
	start := strings.Index(source, "func (s *Server) "+startName+"(")
	end := strings.Index(source, "func (s *Server) "+endName+"(")
	if start < 0 || end <= start {
		t.Fatalf("cannot isolate %s handler", startName)
	}
	return source[start:end]
}
