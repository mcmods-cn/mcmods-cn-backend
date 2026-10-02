package httpapi

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSEC022PublicContentMetricsNeverSerializeVisitorHistory(t *testing.T) {
	response := reflect.New(reflect.TypeOf(contentMetricsResponse{})).Elem()
	if field := response.FieldByName("RecentViewers"); field.IsValid() {
		field.Set(reflect.ValueOf([]contentMetricActor{{
			ID: "visitor-public-id", Name: "private-visitor", OccurredAt: time.Unix(1_700_000_000, 0), URL: "/visitor-public-id",
		}}))
	}
	raw, err := json.Marshal(response.Interface())
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(raw)
	for _, forbidden := range []string{"recentViewers", "visitor-public-id", "private-visitor", "2023-11-14"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("public content metrics serialized visitor history %q in %s", forbidden, serialized)
		}
	}
}

func TestSEC022ContentMetricsHandlerHasNoVisitorIdentityQuery(t *testing.T) {
	raw, err := os.ReadFile("content_metrics_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, forbidden := range []string{"loadRecentMetricViewers", "RecentViewers", "view.last_seen_at"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("public content metrics retains visitor-history path %q", forbidden)
		}
	}
}
