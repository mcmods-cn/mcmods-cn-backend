package httpapi

import (
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSiteChangelogCursorIsStrictAndScopeBound(t *testing.T) {
	t.Parallel()
	publicRequest, err := parseSiteChangelogPageRequest(url.Values{"locale": {"en-US"}, "limit": {"7"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	cursor := encodeSiteChangelogPageCursor(siteChangelogPageCursor{
		Version: siteChangelogPageCursorVersion, Scope: publicRequest.Scope, Limit: publicRequest.Limit,
		ChangeDate: time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC), ID: 91,
	})
	if _, err = parseSiteChangelogPageRequest(url.Values{"locale": {"en-US"}, "limit": {"7"}, "cursor": {cursor}}, false); err != nil {
		t.Fatalf("valid cursor rejected: %v", err)
	}
	for label, values := range map[string]url.Values{
		"offset":       {"offset": {"1"}},
		"page":         {"page": {"2"}},
		"unknown":      {"extra": {"1"}},
		"duplicate":    {"limit": {"7", "8"}},
		"cross locale": {"locale": {"zh-CN"}, "limit": {"7"}, "cursor": {cursor}},
		"cross limit":  {"locale": {"en-US"}, "limit": {"8"}, "cursor": {cursor}},
		"cross admin":  {"limit": {"7"}, "cursor": {cursor}},
	} {
		admin := label == "cross admin"
		if _, parseErr := parseSiteChangelogPageRequest(values, admin); parseErr == nil {
			t.Fatalf("%s request was accepted", label)
		}
	}
}

func TestSiteChangelogQueriesUseBoundedKeysetsBeforeTranslationAssembly(t *testing.T) {
	t.Parallel()
	for name, query := range map[string]string{
		"public": siteChangelogPublicPageSQL,
		"admin":  siteChangelogAdminPageSQL,
	} {
		lower := strings.ToLower(query)
		for _, required := range []string{"(changelog.change_date,changelog.id)<", "order by changelog.change_date desc,changelog.id desc", "limit $", "join lateral"} {
			if !strings.Contains(lower, required) {
				t.Fatalf("%s page SQL is missing %q", name, required)
			}
		}
		for _, forbidden := range []string{" offset ", "count(*) over"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("%s page SQL retains %q", name, forbidden)
			}
		}
	}
}

func TestSiteChangelogHandlersRemoveOffsetAndFixedAdminWindow(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("site_affairs_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	pagination, err := os.ReadFile("site_changelog_pagination.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(source) + string(pagination))
	for _, forbidden := range []string{"boundedoffset", "limit 100`"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("site changelog handler retains %q", forbidden)
		}
	}
	if !strings.Contains(text, "rows.err()") {
		t.Fatal("site changelog page iteration errors are not checked")
	}
}
