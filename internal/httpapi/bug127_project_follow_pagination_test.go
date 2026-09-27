package httpapi

import (
	"net/url"
	"testing"
	"time"
)

func TestProjectFollowPageCursorIsBoundToUserAndFilters(t *testing.T) {
	request, err := parseProjectFollowPageRequest(url.Values{
		"q": {" Mekanism "}, "type": {"mod"}, "limit": {"40"},
	}, 17)
	if err != nil || request.Query != "mekanism" || request.TargetType != "mod" || request.Limit != 40 {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	cursor := encodeProjectFollowPageCursor(projectFollowPageCursor{
		Version: projectFollowPageCursorVersion, Scope: request.Scope,
		CreatedAt: time.Date(2026, 8, 24, 1, 2, 3, 0, time.UTC), RouteID: 99,
	})
	values := url.Values{"q": {"mekanism"}, "type": {"mod"}, "limit": {"40"}, "cursor": {cursor}}
	parsed, err := parseProjectFollowPageRequest(values, 17)
	if err != nil || parsed.Cursor == nil || parsed.Cursor.RouteID != 99 {
		t.Fatalf("parsed=%+v err=%v", parsed, err)
	}
	for label, mutate := range map[string]func(url.Values){
		"user":   func(url.Values) {},
		"query":  func(values url.Values) { values.Set("q", "fabric") },
		"type":   func(values url.Values) { values.Set("type", "plugin") },
		"limit":  func(values url.Values) { values.Set("limit", "41") },
		"offset": func(values url.Values) { values.Set("offset", "40") },
	} {
		copyValues := make(url.Values, len(values))
		for key, items := range values {
			copyValues[key] = append([]string(nil), items...)
		}
		mutate(copyValues)
		userID := int64(17)
		if label == "user" {
			userID = 18
		}
		if _, parseErr := parseProjectFollowPageRequest(copyValues, userID); parseErr == nil {
			t.Errorf("%s scope accepted a foreign cursor", label)
		}
	}
}

func TestProjectFollowPageRequestRejectsInvalidParameters(t *testing.T) {
	for label, values := range map[string]url.Values{
		"unknown":       {"sort": {"created"}},
		"duplicate":     {"limit": {"20", "30"}},
		"invalid limit": {"limit": {"101"}},
		"invalid type":  {"type": {"future_project"}},
		"long query":    {"q": {string(make([]byte, 801))}},
	} {
		if _, err := parseProjectFollowPageRequest(values, 1); err == nil {
			t.Errorf("%s request was accepted", label)
		}
	}
}
