package httpapi

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMetricPageKeyUsesClosedServerRegistry(t *testing.T) {
	for _, value := range []string{"", "detail"} {
		kind, id, err := parseMetricPageKey(value)
		if err != nil || kind != "detail" || id != "" {
			t.Fatalf("parseMetricPageKey(%q) = %q/%q/%v", value, kind, id, err)
		}
	}
	kind, id, err := parseMetricPageKey("version:abc234567")
	if err != nil || kind != "version" || id != "abc234567" {
		t.Fatalf("valid version page = %q/%q/%v", kind, id, err)
	}
	for _, value := range []string{"attacker", "version:anything", "version:abc234567:extra", strings.Repeat("x", 201)} {
		if _, _, err = parseMetricPageKey(value); !errors.Is(err, errMetricPageInvalid) {
			t.Fatalf("arbitrary page key %q was accepted: %v", value, err)
		}
	}
}

func TestMetricViewPathHasSourceAndDailyViewerBudgets(t *testing.T) {
	source, err := os.ReadFile("content_metrics_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, required := range []string{"ConsumeRateLimitPolicy", "ClaimThrottle", "metricViewDeduplicationWindow", "resolveMetricPageKey"} {
		if !strings.Contains(text, required) {
			t.Fatalf("metric view path is missing %q", required)
		}
	}
	if strings.Contains(text, "pageKey = pageKey[:200]") {
		t.Fatal("arbitrary page keys are still truncated into persistent identities")
	}
}

func TestMetricVersionPageMustBelongToTargetResource(t *testing.T) {
	db := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	for _, statement := range []string{
		`create temp table mod_content_versions(id bigint primary key,public_id text not null,status text not null) on commit drop`,
		`create temp table mod_resource_version_details(resource_id bigint not null,version_id bigint not null,status text not null) on commit drop`,
		`insert into mod_content_versions values(1,'abc234567','active'),(2,'def234567','active')`,
		`insert into mod_resource_version_details values(10,1,'active'),(11,2,'active')`,
	} {
		if _, err = tx.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	target := metricTarget{Type: "resource", InternalID: 10}
	if key, err := resolveMetricPageKey(ctx, tx, target, "version:abc234567"); err != nil || key != "version:abc234567" {
		t.Fatalf("owned version page = %q/%v", key, err)
	}
	if _, err = resolveMetricPageKey(ctx, tx, target, "version:def234567"); !errors.Is(err, errMetricPageInvalid) {
		t.Fatalf("foreign version page was accepted: %v", err)
	}
}
