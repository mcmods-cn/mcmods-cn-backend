package httpapi

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestFavoriteMembershipReadsHaveNoPartialSuccessPath(t *testing.T) {
	handlerRaw, err := os.ReadFile("favorite_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	paginationRaw, err := os.ReadFile("favorite_pagination.go")
	if err != nil {
		t.Fatal(err)
	}
	routesRaw, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	handlers, pagination, routes := string(handlerRaw), string(paginationRaw), string(routesRaw)
	if strings.Contains(handlers, "func (s *Server) favoriteMembership(") || strings.Contains(routes, `GET /api/v1/users/me/favorites`) {
		t.Fatal("legacy unbounded favorite membership read path remains reachable")
	}
	for function, requiredCollector := range map[string]string{
		"writeFavoriteCollectionPage":  "collectFavoriteCollectionPageRows",
		"writeFavoriteCollectionItems": "collectFavoriteItemPageRows",
		"favoriteMembershipSummary":    "collectFavoriteMembershipSummaryRows",
	} {
		body := goFunctionBody(t, handlers, function)
		for _, required := range []string{requiredCollector, "if err != nil", "http.StatusInternalServerError"} {
			if !strings.Contains(body, required) {
				t.Fatalf("%s is missing strict favorite read boundary %q", function, required)
			}
		}
	}
	for function, scanToken := range map[string]string{
		"collectFavoriteCollectionPageRows":    "scanFavoriteCollectionPageRow",
		"collectFavoriteItemPageRows":          "scanFavoriteItemPageRow",
		"collectFavoriteMembershipSummaryRows": "rows.Scan",
	} {
		body := goFunctionBody(t, pagination, function)
		for _, required := range []string{"defer rows.Close()", scanToken, "rows.Err()"} {
			if !strings.Contains(body, required) {
				t.Fatalf("%s can return partial membership data; missing %q", function, required)
			}
		}
	}
}

func TestFavoriteMembershipCollectorsRejectScanAndTerminalFailures(t *testing.T) {
	wantErr := errors.New("favorite row stream interrupted")
	tests := []struct {
		name    string
		collect func(*simpleRowsFailureStub) error
	}{
		{name: "collections", collect: func(rows *simpleRowsFailureStub) error {
			result, err := collectFavoriteCollectionPageRows(rows, 2)
			if result != nil {
				t.Fatalf("collection failure returned partial rows: %#v", result)
			}
			return err
		}},
		{name: "items", collect: func(rows *simpleRowsFailureStub) error {
			result, err := collectFavoriteItemPageRows(rows, 2)
			if result != nil {
				t.Fatalf("item failure returned partial rows: %#v", result)
			}
			return err
		}},
		{name: "summary", collect: func(rows *simpleRowsFailureStub) error {
			result, err := collectFavoriteMembershipSummaryRows(rows, true, 2)
			if result.PublicIDs != nil || result.CollectionIDsByEntity != nil {
				t.Fatalf("summary failure returned partial rows: %#v", result)
			}
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name+"_scan", func(t *testing.T) {
			rows := &simpleRowsFailureStub{next: true}
			if err := test.collect(rows); err == nil || !rows.closed {
				t.Fatalf("scan failure = %v/closed=%t", err, rows.closed)
			}
		})
		t.Run(test.name+"_terminal", func(t *testing.T) {
			rows := &simpleRowsFailureStub{terminalErr: wantErr}
			if err := test.collect(rows); !errors.Is(err, wantErr) || !rows.closed {
				t.Fatalf("terminal failure = %v/closed=%t", err, rows.closed)
			}
		})
	}
}
