package httpapi

import "testing"

func TestFavoriteExportCompletionCountsRejectPartialReportsAndIndexes(t *testing.T) {
	healthy := favoriteExportResultCounts{
		CollectionItems:  4,
		Exported:         2,
		AutoDependencies: 1,
		Skipped:          1,
		Failed:           1,
		FinalFiles:       3,
	}
	if err := validateFavoriteExportCompletionCounts(healthy, healthy, 3); err != nil {
		t.Fatalf("healthy result counts were rejected: %v", err)
	}

	testCases := []struct {
		name       string
		task       favoriteExportResultCounts
		report     favoriteExportResultCounts
		indexFiles int
	}{
		{
			name:       "missing exported report row",
			task:       healthy,
			report:     favoriteExportResultCounts{CollectionItems: 3, Exported: 1, AutoDependencies: 1, Skipped: 1, Failed: 1, FinalFiles: 2},
			indexFiles: 2,
		},
		{
			name:       "task final count contradicts task categories",
			task:       favoriteExportResultCounts{CollectionItems: 4, Exported: 2, AutoDependencies: 1, Skipped: 1, Failed: 1, FinalFiles: 2},
			report:     healthy,
			indexFiles: 3,
		},
		{
			name:       "report classification drift",
			task:       healthy,
			report:     favoriteExportResultCounts{CollectionItems: 4, Exported: 2, AutoDependencies: 1, Skipped: 2, FinalFiles: 3},
			indexFiles: 3,
		},
		{
			name:       "serialized index count drift",
			task:       healthy,
			report:     healthy,
			indexFiles: 2,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validateFavoriteExportCompletionCounts(testCase.task, testCase.report, testCase.indexFiles); err == nil {
				t.Fatal("inconsistent export completion was accepted")
			}
		})
	}
}

func TestMRPackBuildReportsTheSerializedIndexFileCount(t *testing.T) {
	result, err := buildMRPack(mrpackBuildInput{
		Name: "BUG-069 pack", VersionID: "1.0.0", MinecraftVersion: "1.21.1",
		Loader: "fabric", LoaderVersion: "0.16.14",
		Files: []mrpackFile{validMRPackTestFile("one.jar"), validMRPackTestFile("two.jar")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IndexFileCount != 2 {
		t.Fatalf("index file count = %d, want 2", result.IndexFileCount)
	}
}
