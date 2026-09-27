package httpapi

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestConversationCursorIsStrictAndBoundToUserAndLimit(t *testing.T) {
	for name, values := range map[string]url.Values{
		"offset":      {"offset": {"1"}},
		"page":        {"page": {"2"}},
		"zero limit":  {"limit": {"0"}},
		"large limit": {"limit": {"101"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseConversationPageRequest(values, 42); err == nil {
				t.Fatal("invalid conversation page request was accepted")
			}
		})
	}
	first, err := parseConversationPageRequest(url.Values{"limit": {"25"}}, 42)
	if err != nil {
		t.Fatal(err)
	}
	cursor := encodeConversationPageCursor(conversationPageCursor{
		Version: conversationCursorVersion, Scope: first.Scope, UpdatedAt: time.Now().UTC(), ID: 99,
	})
	for _, test := range []struct {
		name   string
		userID int64
		limit  string
	}{
		{"other user", 43, "25"},
		{"other limit", 42, "26"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, parseErr := parseConversationPageRequest(url.Values{"limit": {test.limit}, "cursor": {cursor}}, test.userID); parseErr == nil {
				t.Fatal("cross-scope conversation cursor was accepted")
			}
		})
	}
	unknownField := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"s":"` + first.Scope + `","updatedAt":"2026-01-01T00:00:00Z","id":99,"extra":true}`))
	if _, err = parseConversationPageRequest(url.Values{"limit": {"25"}, "cursor": {unknownField}}, 42); err == nil {
		t.Fatal("conversation cursor with an unknown field was accepted")
	}
}

func TestConversationPageSQLUsesBoundedMemberKeysetsAndPersistedSummaries(t *testing.T) {
	request, err := parseConversationPageRequest(url.Values{"limit": {"25"}}, 42)
	if err != nil {
		t.Fatal(err)
	}
	request.Cursor = &conversationPageCursor{UpdatedAt: time.Now().UTC(), ID: 99}
	query, args := conversationPageSQL(42, request)
	for _, required := range []string{
		"where c.user_low_id=$1 and (c.updated_at,c.id)<($2,$3)",
		"where c.user_high_id=$1 and (c.updated_at,c.id)<($2,$3)",
		"union all", "limit $4", "last_message_id", "direct_conversation_unread_counts",
		"order by c.updated_at desc,c.id desc",
	} {
		if !strings.Contains(query, required) {
			t.Errorf("conversation page query is missing %q: %s", required, query)
		}
	}
	lower := strings.ToLower(query)
	if strings.Contains(lower, "select count(*)") || strings.Contains(lower, "join lateral") {
		t.Fatal("conversation page still performs per-row message work")
	}
	if len(args) != 4 || args[0] != int64(42) || args[3] != 26 {
		t.Fatalf("unexpected conversation page args %v", args)
	}
}

func TestDirectMessageHistoryCursorIsStrictAndBoundToMemberConversationAndLimit(t *testing.T) {
	for name, values := range map[string]url.Values{
		"offset":               {"offset": {"1"}},
		"page":                 {"page": {"2"}},
		"zero limit":           {"limit": {"0"}},
		"large limit":          {"limit": {"101"}},
		"cursor plus after":    {"cursor": {"bogus"}, "after": {"m00000001"}},
		"malformed after":      {"after": {"not-valid-anchor"}},
		"duplicate after":      {"after": {"m00000001", "m00000002"}},
		"duplicate page limit": {"limit": {"20", "30"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseDirectMessagePageRequest(values, 42, 7); err == nil {
				t.Fatal("invalid direct-message page request was accepted")
			}
		})
	}
	first, err := parseDirectMessagePageRequest(url.Values{"limit": {"25"}}, 42, 7)
	if err != nil {
		t.Fatal(err)
	}
	cursor := encodeDirectMessagePageCursor(directMessagePageCursor{
		Version: directMessageCursorVersion, Scope: first.Scope, ID: 99,
	})
	for _, test := range []struct {
		name            string
		userID, convoID int64
		limit           string
	}{
		{"other user", 43, 7, "25"},
		{"other conversation", 42, 8, "25"},
		{"other limit", 42, 7, "26"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, parseErr := parseDirectMessagePageRequest(url.Values{"limit": {test.limit}, "cursor": {cursor}}, test.userID, test.convoID); parseErr == nil {
				t.Fatal("cross-scope direct-message cursor was accepted")
			}
		})
	}
}

func TestDirectMessagePageSQLUsesIDKeysetsForHistoryAndIncrementalReads(t *testing.T) {
	history, err := parseDirectMessagePageRequest(url.Values{"limit": {"25"}}, 42, 7)
	if err != nil {
		t.Fatal(err)
	}
	history.Cursor = &directMessagePageCursor{ID: 99}
	historyQuery, historyArgs := directMessagePageSQL(7, 42, history)
	for _, required := range []string{
		"message.conversation_id=$1 and message.id<$3",
		"order by message.id desc limit $4",
		"order by recent.id",
	} {
		if !strings.Contains(historyQuery, required) {
			t.Errorf("history query is missing %q: %s", required, historyQuery)
		}
	}
	if len(historyArgs) != 4 || historyArgs[3] != 26 {
		t.Fatalf("unexpected history args %v", historyArgs)
	}

	incremental, err := parseDirectMessagePageRequest(url.Values{"limit": {"25"}, "after": {"m00000001"}}, 42, 7)
	if err != nil {
		t.Fatal(err)
	}
	incrementalQuery, incrementalArgs := directMessagePageSQL(7, 42, incremental)
	for _, required := range []string{
		"message.id>(select id from direct_messages where public_id=$3 and conversation_id=$1)",
		"order by message.id limit $4",
	} {
		if !strings.Contains(incrementalQuery, required) {
			t.Errorf("incremental query is missing %q: %s", required, incrementalQuery)
		}
	}
	if len(incrementalArgs) != 4 || incrementalArgs[2] != "m00000001" || incrementalArgs[3] != 26 {
		t.Fatalf("unexpected incremental args %v", incrementalArgs)
	}
}

func TestDirectConversationProjectionAdvancesMonotonicallyByMessageID(t *testing.T) {
	for _, required := range []string{
		"last_message_id<$2 then clock_timestamp() else updated_at",
		"last_message_id<$2 then $2 else last_message_id",
	} {
		if !strings.Contains(advanceDirectConversationSQL, required) {
			t.Errorf("conversation projection SQL is missing %q", required)
		}
	}
}
