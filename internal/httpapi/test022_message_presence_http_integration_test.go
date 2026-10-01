package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/queue"
)

type test022Fixture struct {
	test013Fixture
	servers   []*Server
	origins   []*httptest.Server
	redis     *miniredis.Miniredis
	publicIDs map[string]string
}

// Full mounted HTTP, ordinary non-admin sessions and an owned complete schema.
// Redis is explicitly a Lua-capable protocol double, not a production load claim.
func newTEST022Fixture(t *testing.T, shared bool) test022Fixture {
	t.Helper()
	base := newTEST013Fixture(t)
	f := test022Fixture{test013Fixture: base, publicIDs: make(map[string]string)}
	cfg := base.origin.Config.Handler.(*Server).cfg
	cfg.Redis.AuthSessionCacheEnabled = false
	cfg.Redis.RBACCacheEnabled = false
	cfg.Redis.Enabled = shared
	cfg.Redis.PresenceEnabled = true
	cfg.Redis.UnreadCounterEnabled = true
	cfg.Redis.RateLimitFailClosed = false
	cfg.Redis.Prefix, cfg.Redis.Namespace = "test022", "http"
	cfg.Redis.PresenceTTL = 150 * time.Second
	cfg.Redis.PresenceSnapshotInterval = 10 * time.Minute
	cfg.Redis.UnreadTTL = 5 * time.Minute
	if shared {
		f.redis = miniredis.RunT(t)
		cfg.Redis.Addr, cfg.Redis.Password = f.redis.Addr(), ""
		cfg.Redis.DialTimeout = 100 * time.Millisecond
		cfg.Redis.ReadTimeout, cfg.Redis.WriteTimeout = 100*time.Millisecond, 100*time.Millisecond
	}
	for range 2 {
		q := queue.New(f.ctx, cfg.NATS)
		t.Cleanup(q.Close)
		server := NewServer(f.ctx, cfg, f.db, q, nil, nil, nil)
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := server.Shutdown(ctx); err != nil {
				t.Error(err)
			}
			if err := server.cache.Close(); err != nil {
				t.Error(err)
			}
		})
		origin := httptest.NewServer(server)
		t.Cleanup(origin.Close)
		f.servers = append(f.servers, server)
		f.origins = append(f.origins, origin)
	}
	for _, token := range []string{f.editor, f.otherEditor, f.reviewer, f.denied} {
		var publicID string
		if err := f.db.QueryRow(f.ctx, "select public_id from users where id=$1", f.userIDs[token]).Scan(&publicID); err != nil {
			t.Fatal(err)
		}
		f.publicIDs[token] = publicID
	}
	for _, token := range []string{f.editor, f.otherEditor, f.reviewer} {
		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[token], "user.message.send", "user.message.receive")
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "admin.config.write")
	return f
}

func decodeTEST022Data[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var response struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	return response.Data
}
func (f test022Fixture) requireAt(t *testing.T, index int, token, method, path string, body any, want int) []byte {
	t.Helper()
	fixture := f.test013Fixture
	fixture.origin = f.origins[index]
	return fixture.require(t, token, method, path, body, want)
}
func (f test022Fixture) start(t *testing.T, index int, token, target string) string {
	t.Helper()
	raw := f.requireAt(t, index, token, http.MethodPost, "/api/v1/messages/conversations", map[string]string{"userId": f.publicIDs[target]}, http.StatusOK)
	value := decodeTEST022Data[map[string]string](t, raw)["id"]
	if !validCatalogPublicID(value) {
		t.Fatalf("invalid conversation identity: %s", raw)
	}
	return value
}
func (f test022Fixture) send(t *testing.T, index int, token, conversation, body string) map[string]json.RawMessage {
	t.Helper()
	raw := f.requireAt(t, index, token, http.MethodPost, "/api/v1/messages/conversations/"+conversation, sendDirectMessageRequest{Body: body}, http.StatusCreated)
	return decodeTEST022Data[map[string]json.RawMessage](t, raw)
}
func (f test022Fixture) unread(t *testing.T, index int, token string, want int64) {
	t.Helper()
	raw := f.requireAt(t, index, token, http.MethodGet, "/api/v1/me/unread-summary", nil, http.StatusOK)
	summary := decodeTEST022Data[map[string]int64](t, raw)
	if summary["messages"] != want || summary["total"] != summary["messages"]+summary["notifications"] {
		t.Fatalf("unread=%s want messages=%d", raw, want)
	}
}
func (f test022Fixture) facts(t *testing.T) map[string]string {
	t.Helper()
	facts := f.test013Fixture.facts(t)
	for _, table := range []string{"direct_conversations", "direct_messages", "direct_conversation_unread_counts", "user_presence_sessions", "user_blocks"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, "select coalesce(jsonb_agg(to_jsonb(fact_row) order by to_jsonb(fact_row)::text),'[]'::jsonb)::text from "+pgx.Identifier{table}.Sanitize()+" fact_row").Scan(&raw); err != nil {
			t.Fatal(err)
		}
		facts[table] = raw
	}
	return facts
}
func (f test022Fixture) unchanged(t *testing.T, before map[string]string) {
	t.Helper()
	after := f.facts(t)
	if !reflect.DeepEqual(before, after) {
		for table, raw := range before {
			if after[table] != raw {
				t.Errorf("failed/denied request mutated %s", table)
			}
		}
		t.FailNow()
	}
}
func (f test022Fixture) emailCount(t *testing.T, want int) {
	t.Helper()
	var count int
	if err := f.db.QueryRow(f.ctx, "select count(*) from nats_outbox where event_type='notification.direct_message_email.requested'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("email intents=%d want=%d", count, want)
	}
}

func TestTEST022ConversationMembershipPermissionsAndBlocksFullHTTPIntegration(t *testing.T) {
	f := newTEST022Fixture(t, true)
	endpoint := "/api/v1/messages/conversations"
	before := f.facts(t)
	f.requireAt(t, 0, "", http.MethodPost, endpoint, map[string]string{"userId": f.publicIDs[f.otherEditor]}, 401)
	f.requireAt(t, 0, f.denied, http.MethodPost, endpoint, map[string]string{"userId": f.publicIDs[f.otherEditor]}, 403)
	f.requireAt(t, 0, f.editor, http.MethodPost, endpoint, map[string]string{"userId": f.publicIDs[f.editor]}, 400)
	f.requireAt(t, 0, f.editor, http.MethodPost, endpoint, map[string]string{"userId": randomCatalogPublicID()}, 404)
	f.requireAt(t, 0, f.editor, http.MethodPost, endpoint, map[string]string{"userId": f.publicIDs[f.denied]}, 403)
	unknownConversation := endpoint + "/" + randomCatalogPublicID()
	f.requireAt(t, 0, f.editor, http.MethodGet, unknownConversation, nil, 404)
	f.requireAt(t, 0, f.editor, http.MethodPost, unknownConversation, sendDirectMessageRequest{Body: "unknown conversation"}, 404)
	f.requireAt(t, 0, f.editor, http.MethodPut, unknownConversation+"/presence", nil, 404)
	f.unchanged(t, before)
	conversation := f.start(t, 0, f.editor, f.otherEditor)
	if got := f.start(t, 1, f.otherEditor, f.editor); got != conversation {
		t.Fatalf("pair duplicated %s/%s", conversation, got)
	}
	path := endpoint + "/" + conversation
	before = f.facts(t)
	f.requireAt(t, 1, f.reviewer, http.MethodGet, path, nil, 403)
	f.requireAt(t, 1, f.reviewer, http.MethodPost, path, sendDirectMessageRequest{Body: "foreign"}, 403)
	f.requireAt(t, 1, f.reviewer, http.MethodPut, path+"/presence", nil, 403)
	f.requireAt(t, 0, f.editor, http.MethodPost, path, sendDirectMessageRequest{Body: "  "}, 400)
	f.requireAt(t, 0, f.editor, http.MethodPost, path, sendDirectMessageRequest{Body: strings.Repeat("界", 4001)}, 400)
	f.unchanged(t, before)
	f.requireAt(t, 1, f.otherEditor, http.MethodPut, "/api/v1/users/"+f.publicIDs[f.editor]+"/block", nil, 200)
	before = f.facts(t)
	f.requireAt(t, 0, f.editor, http.MethodPost, path, sendDirectMessageRequest{Body: "blocked"}, 403)
	f.requireAt(t, 1, f.otherEditor, http.MethodPost, path, sendDirectMessageRequest{Body: "reverse blocked"}, 403)
	f.requireAt(t, 0, f.editor, http.MethodPost, endpoint, map[string]string{"userId": f.publicIDs[f.otherEditor]}, 403)
	f.unchanged(t, before)
	list := f.requireAt(t, 0, f.editor, http.MethodGet, endpoint, nil, 200)
	var page struct {
		Items []directConversationSummary `json:"items"`
	}
	page = decodeTEST022Data[struct {
		Items []directConversationSummary `json:"items"`
	}](t, list)
	if len(page.Items) != 1 || page.Items[0].CanMessage {
		t.Fatalf("block summary leaks messaging permission: %s", list)
	}
	f.requireAt(t, 1, f.otherEditor, http.MethodDelete, "/api/v1/users/"+f.publicIDs[f.editor]+"/block", nil, 200)
	f.requireAt(t, 1, f.otherEditor, http.MethodPut, "/api/v1/users/me/profile-settings", map[string]bool{"messageReceive": false}, 200)
	before = f.facts(t)
	f.requireAt(t, 0, f.editor, http.MethodPost, path, sendDirectMessageRequest{Body: "disabled receive"}, 403)
	f.requireAt(t, 0, f.editor, http.MethodPost, endpoint, map[string]string{"userId": f.publicIDs[f.otherEditor]}, 403)
	f.unchanged(t, before)
}

func TestTEST022PresenceSuppressionAndReadReceiptPrivacyFullHTTPIntegration(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprintf("shared_%t", shared), func(t *testing.T) {
			f := newTEST022Fixture(t, shared)
			conversation := f.start(t, 0, f.editor, f.otherEditor)
			path := "/api/v1/messages/conversations/" + conversation
			f.requireAt(t, 1, f.otherEditor, http.MethodPut, "/api/v1/users/me/profile-settings", map[string]bool{"showOnlineStatus": true}, 200)
			f.unread(t, 0, f.otherEditor, 0)
			response := f.send(t, 0, f.editor, conversation, strings.Repeat("界", 4000))
			item := decodeTEST022Data[directMessageItem](t, []byte(`{"data":`+string(response["message"])+`}`))
			if len([]rune(item.Body)) != 4000 || item.ReadAt != nil || string(response["notificationQueued"]) != "true" || string(response["suppressed"]) != "false" {
				t.Fatalf("inactive message=%v item=%#v", response, item)
			}
			f.emailCount(t, 1)
			var preview string
			if err := f.db.QueryRow(f.ctx, "select payload#>>'{templateValues,preview}' from nats_outbox where aggregate_id=$1", item.ID).Scan(&preview); err != nil {
				t.Fatal(err)
			}
			if preview != strings.Repeat("界", 160)+"..." {
				t.Fatalf("unicode preview len=%d", len([]rune(preview)))
			}
			f.unread(t, 0, f.otherEditor, 1)
			// Same replica works locally; shared mode must suppress across replicas.
			presencePeer := 0
			if shared {
				presencePeer = 1
			}
			f.requireAt(t, presencePeer, f.otherEditor, http.MethodPut, path+"/presence", nil, 200)
			response = f.send(t, 0, f.editor, conversation, "active visible")
			if string(response["notificationQueued"]) != "false" || string(response["suppressed"]) != "true" || !bytes.Contains(response["message"], []byte(`"readAt"`)) {
				t.Fatalf("active contract=%v", response)
			}
			f.emailCount(t, 1)
			f.unread(t, 0, f.otherEditor, 1)
			f.requireAt(t, 1, f.otherEditor, http.MethodPut, "/api/v1/users/me/profile-settings", map[string]bool{"showOnlineStatus": false}, 200)
			response = f.send(t, 0, f.editor, conversation, "active hidden")
			if _, ok := response["notificationQueued"]; ok {
				t.Fatalf("hidden recipient reveals notification: %v", response)
			}
			if _, ok := response["suppressed"]; ok || bytes.Contains(response["message"], []byte(`"readAt"`)) {
				t.Fatalf("hidden recipient reveals presence: %v", response)
			}
			f.emailCount(t, 1)
			history := f.requireAt(t, 0, f.editor, http.MethodGet, path, nil, 200)
			if bytes.Contains(history, []byte(`"readAt"`)) {
				t.Fatalf("hidden read receipts in history: %s", history)
			}
			// Recipient opening marks all own unread messages, independently of privacy.
			f.requireAt(t, 0, f.otherEditor, http.MethodGet, path, nil, 200)
			f.unread(t, 0, f.otherEditor, 0)
		})
	}
}

func TestTEST022OfflineMailFailureRollsBackMessageAnchorAndUnreadFullHTTPIntegration(t *testing.T) {
	f := newTEST022Fixture(t, true)
	conversation := f.start(t, 0, f.editor, f.otherEditor)
	f.unread(t, 0, f.otherEditor, 0)
	before := f.facts(t)
	if _, err := f.db.Exec(f.ctx, "alter table nats_outbox add constraint test022_reject_email check(event_type<>'notification.direct_message_email.requested') not valid"); err != nil {
		t.Fatal(err)
	}
	f.requireAt(t, 0, f.editor, http.MethodPost, "/api/v1/messages/conversations/"+conversation, sendDirectMessageRequest{Body: "must rollback"}, 500)
	f.unchanged(t, before)
	f.unread(t, 0, f.otherEditor, 0)
	if _, err := f.db.Exec(f.ctx, "alter table nats_outbox drop constraint test022_reject_email"); err != nil {
		t.Fatal(err)
	}
	f.send(t, 0, f.editor, conversation, "retry succeeds once")
	f.emailCount(t, 1)
	f.unread(t, 0, f.otherEditor, 1)
}

type test022MessagePage struct {
	Items      []directMessageItem `json:"items"`
	HasMore    bool                `json:"hasMore"`
	NextCursor string              `json:"nextCursor"`
	Limit      int                 `json:"limit"`
}

func TestTEST022ConcurrentMessagesPreserveAnchorHistoryUnreadAndCalibrationFullHTTPIntegration(t *testing.T) {
	f := newTEST022Fixture(t, true)
	conversation := f.start(t, 0, f.editor, f.otherEditor)
	path := "/api/v1/messages/conversations/" + conversation
	for index := range 2 {
		f.unread(t, index, f.otherEditor, 0)
	}
	const sends = 64
	type result struct {
		status int
		raw    []byte
		err    error
	}
	results := make(chan result, sends)
	start := make(chan struct{})
	var group sync.WaitGroup
	for index := range sends {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			<-start
			body, _ := json.Marshal(sendDirectMessageRequest{Body: fmt.Sprintf("parallel-%02d", index)})
			origin := f.origins[index%2]
			request, err := http.NewRequestWithContext(f.ctx, http.MethodPost, origin.URL+path, bytes.NewReader(body))
			if err != nil {
				results <- result{err: err}
				return
			}
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+f.editor)
			response, err := origin.Client().Do(request)
			if err != nil {
				results <- result{err: err}
				return
			}
			defer response.Body.Close()
			raw, err := io.ReadAll(io.LimitReader(response.Body, 8192))
			results <- result{status: response.StatusCode, raw: raw, err: err}
		}(index)
	}
	close(start)
	group.Wait()
	close(results)
	identities := make(map[string]bool)
	for result := range results {
		if result.err != nil || result.status != 201 {
			t.Fatalf("concurrent send status=%d err=%v body=%s", result.status, result.err, result.raw)
		}
		response := decodeTEST022Data[struct {
			Message directMessageItem `json:"message"`
		}](t, result.raw)
		if response.Message.ID == "" || identities[response.Message.ID] {
			t.Fatalf("duplicate/empty message id: %s", result.raw)
		}
		identities[response.Message.ID] = true
	}
	var messages, unread int
	var anchor, maximum int64
	if err := f.db.QueryRow(f.ctx, `select count(*),count(*) filter(where read_at is null),max(id),
 (select last_message_id from direct_conversations where public_id=$1) from direct_messages
 where conversation_id=(select id from direct_conversations where public_id=$1)`, conversation).Scan(&messages, &unread, &maximum, &anchor); err != nil {
		t.Fatal(err)
	}
	if messages != sends || unread != sends || anchor != maximum {
		t.Fatalf("messages=%d unread=%d anchor=%d max=%d", messages, unread, anchor, maximum)
	}
	f.emailCount(t, sends)
	// Per-replica local derivatives may drift until the documented reconciler;
	// never confuse this explicit repair with an immediate coherence guarantee.
	for index := range 2 {
		raw := f.requireAt(t, index, f.reviewer, http.MethodPost, "/api/v1/admin/infrastructure/unread/reconcile", unreadReconcileRequest{UserIDs: []string{f.publicIDs[f.otherEditor]}}, 200)
		if !bytes.Contains(raw, []byte(`"messages":64`)) {
			t.Fatalf("calibration omitted PG truth: %s", raw)
		}
		f.unread(t, index, f.otherEditor, sends)
	}
	seen := make(map[string]bool)
	cursor := ""
	firstID := ""
	for pageNumber := 0; pageNumber < sends/2; pageNumber++ {
		query := "?limit=2"
		if cursor != "" {
			query += "&cursor=" + url.QueryEscape(cursor)
		}
		raw := f.requireAt(t, 0, f.editor, http.MethodGet, path+query, nil, 200)
		page := decodeTEST022Data[test022MessagePage](t, raw)
		if len(raw) > 16384 || len(page.Items) != 2 || page.Limit != 2 {
			t.Fatalf("bounded history failed: %s", raw)
		}
		for _, item := range page.Items {
			if !identities[item.ID] || seen[item.ID] {
				t.Fatalf("history lost/duplicated id=%s", item.ID)
			}
			seen[item.ID] = true
			if pageNumber == sends/2-1 && firstID == "" {
				firstID = item.ID
			}
		}
		if page.HasMore != (pageNumber < sends/2-1) || page.HasMore != (page.NextCursor != "") {
			t.Fatalf("history cursor termination: %s", raw)
		}
		if pageNumber == 0 {
			before := f.facts(t)
			f.requireAt(t, 1, f.otherEditor, http.MethodGet, path+"?limit=2&cursor="+url.QueryEscape(page.NextCursor), nil, 400)
			f.requireAt(t, 0, f.editor, http.MethodGet, path+"?limit=3&cursor="+url.QueryEscape(page.NextCursor), nil, 400)
			f.requireAt(t, 0, f.editor, http.MethodGet, path+"?limit=2&after="+page.Items[0].ID+"&cursor="+url.QueryEscape(page.NextCursor), nil, 400)
			f.unchanged(t, before)
		}
		cursor = page.NextCursor
	}
	if len(seen) != sends {
		t.Fatalf("history returned %d of %d", len(seen), sends)
	}
	after := decodeTEST022Data[test022MessagePage](t, f.requireAt(t, 0, f.editor, http.MethodGet, path+"?after="+firstID, nil, 200))
	if len(after.Items) != sends-1 || after.HasMore || after.NextCursor != "" {
		t.Fatalf("incremental history=%#v", after)
	}
	otherConversation := f.start(t, 1, f.editor, f.reviewer)
	foreign := f.send(t, 1, f.editor, otherConversation, "foreign anchor")
	var foreignMessage directMessageItem
	if err := json.Unmarshal(foreign["message"], &foreignMessage); err != nil {
		t.Fatal(err)
	}
	before := f.facts(t)
	f.requireAt(t, 1, f.otherEditor, http.MethodGet, path+"?after="+foreignMessage.ID, nil, 400)
	f.requireAt(t, 1, f.otherEditor, http.MethodGet, path+"?after="+randomCatalogPublicID(), nil, 400)
	f.unchanged(t, before)
	f.requireAt(t, 0, f.otherEditor, http.MethodGet, path+"?limit=2", nil, 200)
	f.unread(t, 0, f.otherEditor, 0)
	if err := f.db.QueryRow(f.ctx, "select count(*) from direct_messages where conversation_id=(select id from direct_conversations where public_id=$1) and read_at is null", conversation).Scan(&unread); err != nil || unread != 0 {
		t.Fatalf("read-all truth=%d err=%v", unread, err)
	}
}

func (f test022Fixture) presenceRaw(t *testing.T, index int, token, agent, body string, want int) (http.Header, []byte) {
	t.Helper()
	request, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.origins[index].URL+"/api/v1/site/presence", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", agent)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := f.origins[index].Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8192))
	if err != nil || response.StatusCode != want {
		t.Fatalf("presence status=%d want=%d err=%v body=%s", response.StatusCode, want, err, raw)
	}
	if want >= 400 && bytes.Contains(raw, []byte(`"data":`)) {
		t.Fatalf("presence failure leaked success: %s", raw)
	}
	return response.Header, raw
}

func TestTEST022PresenceRejectsMalformedAndOversizedJSONFullHTTPIntegration(t *testing.T) {
	for _, body := range []string{`{"visitorId":`, `{"visitorId":1}`, `{"visitorId":"x","unexpected":true}`, `{"visitorId":"x"}{}`, `{"visitorId":"` + strings.Repeat("a", int(maxJSONRequestBodyBytes)) + `"}`} {
		t.Run(fmt.Sprintf("bytes_%d", len(body)), func(t *testing.T) {
			f := newTEST022Fixture(t, false)
			before := f.facts(t)
			f.presenceRaw(t, 0, "", "test022-malformed", body, 400)
			f.unchanged(t, before)
			if count := f.servers[0].cache.OnlinePresenceCount(f.ctx, time.Now()); count != 0 {
				t.Fatalf("invalid heartbeat created %d visitors", count)
			}
		})
	}
}

func TestTEST022AnonymousVisitorForgeryHasSharedAndFallbackAdmissionBoundsFullHTTPIntegration(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprintf("shared_%t", shared), func(t *testing.T) {
			f := newTEST022Fixture(t, shared)
			limit := presenceLocalRequestsPerWindow
			if shared {
				limit = presenceSharedRequestsPerWindow
			}
			identity := ""
			for index := 0; index < limit+5; index++ {
				peer := 0
				if shared {
					peer = index % 2
				}
				want := 200
				if index >= limit {
					want = 429
				}
				header, raw := f.presenceRaw(t, peer, "", "test022-fixed-agent", fmt.Sprintf(`{"visitorId":"attacker-%d"}`, index), want)
				if want == 200 {
					value := decodeTEST022Data[map[string]any](t, raw)["visitorId"].(string)
					if !strings.HasPrefix(value, "p1.") || len(value) != 67 || (identity != "" && value != identity) {
						t.Fatalf("client multiplied signed visitor: %s", raw)
					}
					identity = value
				} else if !bytes.Contains(raw, []byte("PRESENCE_RATE_LIMIT")) || header.Get("Retry-After") == "" {
					t.Fatalf("missing stable admission contract: headers=%v body=%s", header, raw)
				}
			}
			// Changing the UA after source exhaustion does not get a new source budget.
			f.presenceRaw(t, 0, "", "a-new-attacker-agent", `{"visitorId":"new"}`, 429)
			for _, server := range f.servers {
				if count := server.cache.OnlinePresenceCount(f.ctx, time.Now()); (shared && count != 1) || (!shared && count > 1) {
					t.Fatalf("client IDs created cardinality=%d", count)
				}
			}
			var snapshots int
			if err := f.db.QueryRow(f.ctx, "select count(*) from user_presence_sessions").Scan(&snapshots); err != nil || snapshots != 0 {
				t.Fatalf("anonymous PG snapshots=%d err=%v", snapshots, err)
			}
		})
	}
}

func TestTEST022AuthenticatedPresenceSnapshotsSessionsPrivacyAndRedisFailureFullHTTPIntegration(t *testing.T) {
	f := newTEST022Fixture(t, true)
	conversation := f.start(t, 0, f.editor, f.otherEditor)
	f.requireAt(t, 1, f.otherEditor, http.MethodPut, "/api/v1/users/me/profile-settings", map[string]bool{"showOnlineStatus": true}, 200)
	for index := range 5 {
		f.presenceRaw(t, index%2, f.otherEditor, "test022-user", `{}`, 200)
	}
	var count int
	var snapshot string
	if err := f.db.QueryRow(f.ctx, "select count(*),coalesce(jsonb_agg(to_jsonb(p) order by session_hash)::text,'[]') from user_presence_sessions p").Scan(&count, &snapshot); err != nil || count != 1 {
		t.Fatalf("snapshots=%d err=%v", count, err)
	}
	f.presenceRaw(t, 1, f.otherEditor, "test022-user", `{}`, 200)
	var after string
	if err := f.db.QueryRow(f.ctx, "select coalesce(jsonb_agg(to_jsonb(p) order by session_hash)::text,'[]') from user_presence_sessions p").Scan(&after); err != nil || after != snapshot {
		t.Fatalf("heartbeat became request-level PG writes: err=%v", err)
	}
	status := func(want publicOnlineStatus) {
		t.Helper()
		raw := f.requireAt(t, 0, f.editor, http.MethodGet, "/api/v1/messages/conversations", nil, 200)
		page := decodeTEST022Data[struct {
			Items []directConversationSummary `json:"items"`
		}](t, raw)
		if len(page.Items) != 1 || page.Items[0].ID != conversation || page.Items[0].OnlineStatus != want {
			t.Fatalf("online privacy want=%s body=%s", want, raw)
		}
	}
	status(publicOnlineStatusOnline)
	second := mintTEST048Token(t, f.ctx, f.db, f.servers[0].cfg, f.userIDs[f.otherEditor])
	f.presenceRaw(t, 1, second, "test022-second-session", `{}`, 200)
	f.requireAt(t, 1, f.otherEditor, http.MethodPost, "/api/v1/auth/logout", nil, 200)
	status(publicOnlineStatusOnline)
	f.requireAt(t, 0, f.otherEditor, http.MethodGet, "/api/v1/me/unread-summary", nil, 401)
	f.requireAt(t, 1, second, http.MethodPost, "/api/v1/auth/logout", nil, 200)
	status(publicOnlineStatusOffline)
	// Logout removed both ordinary session-presence facts and shared presence.
	if err := f.db.QueryRow(f.ctx, "select count(*) from user_presence_sessions").Scan(&count); err != nil || count != 0 {
		t.Fatalf("logout snapshots=%d err=%v", count, err)
	}
	replacement := mintTEST048Token(t, f.ctx, f.db, f.servers[0].cfg, f.userIDs[f.otherEditor])
	f.presenceRaw(t, 0, replacement, "test022-replacement", `{}`, 200)
	f.redis.Close()
	f.presenceRaw(t, 0, replacement, "test022-replacement", `{}`, 200)
	status(publicOnlineStatusOnline) // same-replica bounded fallback, not global Redis truth
	f.requireAt(t, 0, replacement, http.MethodPut, "/api/v1/users/me/profile-settings", map[string]bool{"showOnlineStatus": false}, 200)
	status(publicOnlineStatusHidden)
	if err := f.db.QueryRow(f.ctx, "select count(*) from user_presence_sessions").Scan(&count); err != nil || count != 1 {
		t.Fatalf("fallback multiplied snapshots=%d err=%v", count, err)
	}
}

func TestTEST022ReadFailureDoesNotCommitReceiptsOrCacheChangesFullHTTPIntegration(t *testing.T) {
	f := newTEST022Fixture(t, true)
	conversation := f.start(t, 0, f.editor, f.otherEditor)
	f.unread(t, 0, f.otherEditor, 0)
	f.send(t, 0, f.editor, conversation, "still unread after failure")
	f.unread(t, 0, f.otherEditor, 1)
	before := f.facts(t)
	if _, err := f.db.Exec(f.ctx, `create function test022_fail_read_update() returns trigger language plpgsql as $$
 begin raise exception 'owned TEST022 read failure'; end $$;
 create trigger test022_fail_read before update on direct_messages
 for each row execute function test022_fail_read_update()`); err != nil {
		t.Fatal(err)
	}
	f.requireAt(t, 0, f.otherEditor, http.MethodGet, "/api/v1/messages/conversations/"+conversation, nil, 500)
	f.unchanged(t, before)
	f.unread(t, 0, f.otherEditor, 1)
	if _, err := f.db.Exec(f.ctx, "drop trigger test022_fail_read on direct_messages; drop function test022_fail_read_update()"); err != nil {
		t.Fatal(err)
	}
	f.requireAt(t, 0, f.otherEditor, http.MethodGet, "/api/v1/messages/conversations/"+conversation, nil, 200)
	f.unread(t, 0, f.otherEditor, 0)
}

func TestTEST022ConversationDatabaseFailureIsNotMisreportedAsForbiddenFullHTTPIntegration(t *testing.T) {
	f := newTEST022Fixture(t, false)
	conversation := f.start(t, 0, f.editor, f.otherEditor)
	before := f.facts(t)
	if _, err := f.db.Exec(f.ctx, `alter table direct_conversations rename to test022_owned_conversations;
 create function test022_fail_membership(bigint) returns bigint language plpgsql stable as $$
 begin raise exception 'owned TEST022 membership read failure'; end $$;
 create view direct_conversations as select id,public_id,
 test022_fail_membership(user_low_id) as user_low_id,user_high_id,last_message_id,created_at,updated_at
 from test022_owned_conversations`); err != nil {
		t.Fatal(err)
	}
	restored := false
	restore := func() error {
		if restored {
			return nil
		}
		_, err := f.db.Exec(f.ctx, "drop view direct_conversations; drop function test022_fail_membership(bigint); alter table test022_owned_conversations rename to direct_conversations")
		if err == nil {
			restored = true
		}
		return err
	}
	defer func() {
		if err := restore(); err != nil {
			t.Error(err)
		}
	}()
	// Prove the ordinary path lookup succeeds: the injected fault affects only
	// the membership columns, not the initial public-to-internal ID lookup.
	var internalID int64
	if err := f.db.QueryRow(f.ctx, "select id from direct_conversations where public_id=$1", conversation).Scan(&internalID); err != nil || internalID <= 0 {
		t.Fatalf("invalid fault injection: initial lookup=%d err=%v", internalID, err)
	}
	path := "/api/v1/messages/conversations/" + conversation
	f.requireAt(t, 0, f.editor, http.MethodGet, path, nil, 500)
	f.requireAt(t, 0, f.editor, http.MethodPut, path+"/presence", nil, 500)
	f.requireAt(t, 0, f.editor, http.MethodPost, path, sendDirectMessageRequest{Body: "membership SQL failed"}, 500)
	if err := restore(); err != nil {
		t.Fatal(err)
	}
	f.unchanged(t, before)
	f.requireAt(t, 0, f.editor, http.MethodGet, path, nil, 200)
}

type test022CountingBody struct {
	reader    io.Reader
	readBytes int
}

func (body *test022CountingBody) Read(buffer []byte) (int, error) {
	n, err := body.reader.Read(buffer)
	body.readBytes += n
	return n, err
}
func (*test022CountingBody) Close() error { return nil }

func TestTEST022ExhaustedPresenceAdmissionDoesNotReadBodyThroughMountedMiddleware(t *testing.T) {
	f := newTEST022Fixture(t, false)
	for range presenceLocalRequestsPerWindow {
		f.presenceRaw(t, 0, "", "test022-budget", `{}`, 200)
	}
	body := &test022CountingBody{reader: strings.NewReader(`{"visitorId":"` + strings.Repeat("a", 1<<20) + `"}`)}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/site/presence", nil)
	request.Body = body
	request.RemoteAddr = "127.0.0.1:43123"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "test022-budget")
	response := httptest.NewRecorder()
	// This exact read-count assertion is the mounted handler/middleware boundary,
	// not a claim about net/http's post-handler connection draining.
	f.servers[0].ServeHTTP(response, request)
	if response.Code != 429 || body.readBytes != 0 {
		t.Fatalf("exhausted admission status=%d decoded bytes=%d body=%s", response.Code, body.readBytes, response.Body.String())
	}
}

func TestTEST022ChatPresenceExpiresAtOriginalThirtySecondFullHTTPIntegration(t *testing.T) {
	f := newTEST022Fixture(t, true)
	conversation := f.start(t, 0, f.editor, f.otherEditor)
	path := "/api/v1/messages/conversations/" + conversation
	f.requireAt(t, 1, f.otherEditor, http.MethodPut, "/api/v1/users/me/profile-settings", map[string]bool{"showOnlineStatus": true}, 200)
	f.requireAt(t, 1, f.otherEditor, http.MethodPut, path+"/presence", nil, 200)
	startedAt := time.Now()
	response := f.send(t, 0, f.editor, conversation, "before original expiry")
	if string(response["suppressed"]) != "true" {
		t.Fatalf("cross-replica chat presence not active: %v", response)
	}
	f.emailCount(t, 0)
	timer := time.NewTimer(30*time.Second + 250*time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	}
	// The Redis protocol double only advances TTL explicitly. Advance it by
	// the time already waited; do not shortcut the real local thirty seconds.
	f.redis.FastForward(time.Since(startedAt))
	response = f.send(t, 0, f.editor, conversation, "after original expiry")
	if string(response["notificationQueued"]) != "true" || string(response["suppressed"]) != "false" || bytes.Contains(response["message"], []byte(`"readAt"`)) {
		t.Fatalf("expired chat presence still suppresses: %v", response)
	}
	f.emailCount(t, 1)
	f.unread(t, 0, f.otherEditor, 1)
}

func TestTEST022ConcurrentConversationCreationDeduplicatesBothMemberOrdersFullHTTPIntegration(t *testing.T) {
	f := newTEST022Fixture(t, false)
	const attempts = 32
	type result struct {
		status int
		raw    []byte
		err    error
	}
	results := make(chan result, attempts)
	start := make(chan struct{})
	var group sync.WaitGroup
	for index := range attempts {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			<-start
			sender, target := f.editor, f.otherEditor
			if index%2 != 0 {
				sender, target = target, sender
			}
			body, _ := json.Marshal(startConversationRequest{UserID: f.publicIDs[target]})
			origin := f.origins[index%2]
			request, err := http.NewRequestWithContext(f.ctx, http.MethodPost, origin.URL+"/api/v1/messages/conversations", bytes.NewReader(body))
			if err != nil {
				results <- result{err: err}
				return
			}
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+sender)
			response, err := origin.Client().Do(request)
			if err != nil {
				results <- result{err: err}
				return
			}
			defer response.Body.Close()
			raw, err := io.ReadAll(io.LimitReader(response.Body, 2048))
			results <- result{status: response.StatusCode, raw: raw, err: err}
		}(index)
	}
	close(start)
	group.Wait()
	close(results)
	identity := ""
	for result := range results {
		if result.err != nil || result.status != 200 {
			t.Fatalf("concurrent conversation status=%d err=%v body=%s", result.status, result.err, result.raw)
		}
		got := decodeTEST022Data[map[string]string](t, result.raw)["id"]
		if !validCatalogPublicID(got) || (identity != "" && identity != got) {
			t.Fatalf("pair not deduplicated: %s/%s", identity, got)
		}
		identity = got
	}
	var count int
	var low, high int64
	if err := f.db.QueryRow(f.ctx, "select count(*),min(user_low_id),min(user_high_id) from direct_conversations").Scan(&count, &low, &high); err != nil {
		t.Fatal(err)
	}
	wantLow, wantHigh := orderedUserIDs(f.userIDs[f.editor], f.userIDs[f.otherEditor])
	if count != 1 || low != wantLow || high != wantHigh {
		t.Fatalf("pair rows=%d low/high=%d/%d", count, low, high)
	}
	f.emailCount(t, 0)
	for _, token := range []string{f.editor, f.otherEditor} {
		f.requireAt(t, 1, token, http.MethodGet, "/api/v1/messages/conversations/"+identity, nil, 200)
	}
}
