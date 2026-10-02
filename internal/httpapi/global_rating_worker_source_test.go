package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestPopularityWorkerNeverRefreshesGlobalRatingsPerTask(t *testing.T) {
	source, err := os.ReadFile("popularity_worker.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(source))
	for _, forbidden := range []string{
		"refresh_content_rating_global_stats",
		"global_stats.updated_at<now()-interval '5 seconds'",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("popularity worker retained global rating rescan path %q", forbidden)
		}
	}
}
