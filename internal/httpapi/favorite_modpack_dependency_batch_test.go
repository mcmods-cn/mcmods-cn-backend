package httpapi

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFavoriteExportDependencyTraversalUsesOneBoundedGraphAndBatchedFiles(t *testing.T) {
	payload, err := os.ReadFile("favorite_modpack_export.go")
	if err != nil {
		t.Fatal(err)
	}
	graphPayload, err := os.ReadFile("favorite_modpack_dependency_graph.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(payload) + string(graphPayload)
	for _, forbidden := range []string{
		"cursor < 100",
		"s.resolveFavoriteModrinthFile(ctx, routeID",
		"if err != nil {\n\t\t\tcontinue",
		"if rows.Scan(&routeID, &publicID, &name) != nil {\n\t\t\t\tcontinue",
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("dependency traversal still contains serial/truncating path %q", forbidden)
		}
	}
	for _, required := range []string{
		"favoriteExportDependencyGraphSQL",
		"maxFavoriteExportDependencyNodes",
		"maxFavoriteExportDependencyEdges",
		"maxFavoriteExportFileConcurrency",
		"resolveFavoriteExportFileCandidates",
		"errFavoriteExportDependencyLimit",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("dependency traversal is missing bounded batch contract %q", required)
		}
	}
}

func TestResolveFavoriteExportFileCandidatesIsBoundedAndOrderStable(t *testing.T) {
	candidates := make([]favoriteExportFileCandidate, 40)
	for index := range candidates {
		candidates[index] = favoriteExportFileCandidate{RouteID: int64(index + 1), ProjectID: fmt.Sprintf("project-%d", index+1)}
	}
	var active atomic.Int64
	var maximum atomic.Int64
	resolutions, err := resolveFavoriteExportFileCandidates(context.Background(), candidates, func(_ context.Context, candidate favoriteExportFileCandidate) favoriteExportFileResolution {
		current := active.Add(1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		active.Add(-1)
		return favoriteExportFileResolution{File: providerProjectFile{ProviderProjectID: candidate.ProjectID}}
	})
	if err != nil {
		t.Fatal(err)
	}
	if maximum.Load() > maxFavoriteExportFileConcurrency {
		t.Fatalf("maximum candidate concurrency = %d, budget = %d", maximum.Load(), maxFavoriteExportFileConcurrency)
	}
	if maximum.Load() < 2 {
		t.Fatalf("test did not exercise concurrent candidate resolution: %d", maximum.Load())
	}
	for index, resolution := range resolutions {
		if want := candidates[index].ProjectID; resolution.File.ProviderProjectID != want {
			t.Fatalf("resolution %d = %q, want %q", index, resolution.File.ProviderProjectID, want)
		}
	}
}

func TestFavoriteExportRequiredDependencyFailureCannotBeConfirmedAway(t *testing.T) {
	if favoriteExportHasRequiredDependencyFailure([]favoriteModpackExportItem{{ResultType: "failed", ReasonCode: exportReasonExternalAPIError}}) {
		t.Fatal("ordinary item failure was treated as a required dependency failure")
	}
	if !favoriteExportHasRequiredDependencyFailure([]favoriteModpackExportItem{{ResultType: "failed", ReasonCode: exportReasonRequiredDependencyUnresolved}}) {
		t.Fatal("required dependency failure was not detected")
	}
	payload, err := os.ReadFile("favorite_modpack_export.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "MODPACK_EXPORT_REQUIRED_DEPENDENCY_UNRESOLVED") {
		t.Fatal("create handler does not fail closed on an unresolved required dependency")
	}
}
