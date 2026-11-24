package httpapi

import (
	"net/http"
	"reflect"
	"testing"
)

func TestOCT02LogRetentionNormalizesKeysWithoutMutatingInput(t *testing.T) {
	original := map[string]int{" api_access ": 45, " custom ": 5000, " ": 7, " invalid ": -1}
	wantOriginal := map[string]int{" api_access ": 45, " custom ": 5000, " ": 7, " invalid ": -1}
	got := normalizeLogConfig(logRetentionConfig{DefaultDays: 5000, CategoryDays: original})
	if got.DefaultDays != 3650 || got.CategoryDays["api_access"] != 45 || got.CategoryDays["custom"] != 3650 {
		t.Fatalf("trimmed retention overrides or bounds were lost: %+v", got)
	}
	for _, key := range []string{" api_access ", " custom ", " ", " invalid ", "invalid"} {
		if _, exists := got.CategoryDays[key]; exists {
			t.Errorf("normalization retained invalid key %q", key)
		}
	}
	if !reflect.DeepEqual(original, wantOriginal) {
		t.Fatal("normalization changed the caller's configuration map")
	}
}

func TestOCT02LogRetentionCanonicalKeyWinsAndAliasesAreDeterministic(t *testing.T) {
	for range 100 {
		got := normalizeLogConfig(logRetentionConfig{CategoryDays: map[string]int{
			"system": 80, " system ": 15, "\tsystem": 20, " custom ": 25, "\tcustom ": 30,
		}})
		if got.CategoryDays["system"] != 80 || got.CategoryDays["custom"] != 30 {
			t.Fatalf("ambiguous aliases did not use stable precedence: %+v", got.CategoryDays)
		}
		if len(got.CategoryDays) != len(defaultLogConfig().CategoryDays)+1 {
			t.Fatalf("aliases became duplicate persisted categories: %+v", got.CategoryDays)
		}
	}
}

func TestSavedLogCategoryCanInheritTheSiteRetentionIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "log.write", "log.read")
	const settings = "/api/v1/admin/logs/config"
	f.require(t, f.reviewer, http.MethodPut, settings, logRetentionConfig{
		Enabled: true, DefaultDays: 365, CategoryDays: map[string]int{"api_access": 0},
	}, http.StatusOK)
	config := decodeTEST022Data[logRetentionConfig](t, f.require(t, f.reviewer, http.MethodGet, settings, nil, http.StatusOK))
	if days, explicit := config.CategoryDays["api_access"]; !explicit || days != 0 {
		t.Fatalf("saved inheritance was replaced with %d days; present=%v", days, explicit)
	}
	for _, days := range []int{180, 400} {
		if _, err := f.db.Exec(f.ctx, `insert into app_logs(category,action,created_at)
			values('api_access','inherit-retention',now()-make_interval(days=>$1::int))`, days); err != nil {
			t.Fatal(err)
		}
	}
	f.require(t, f.reviewer, http.MethodPost, "/api/v1/admin/logs/cleanup", nil, http.StatusOK)
	var within, expired int
	err := f.db.QueryRow(f.ctx, `select count(*) filter(where created_at>now()-interval '365 days'),
		count(*) filter(where created_at<now()-interval '365 days') from app_logs where action='inherit-retention'`).Scan(&within, &expired)
	if err != nil || within != 1 || expired != 0 {
		t.Fatalf("within policy=%d expired=%d err=%v", within, expired, err)
	}
}
