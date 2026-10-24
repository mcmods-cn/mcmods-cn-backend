package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"mcmods-cn-backend/internal/security"
)

// Match the original audit's real PostgreSQL target-SELECT fault, separately
// from the closed-pgxpool component test. Authentication is established through
// normal HTTP before fault injection; direct handler calls isolate the precise
// lookup from authentication queries against the same users table.
func TestAuditMSG002RealPostgresLookupFailuresPreserveBusinessState(t *testing.T) {
	f := newTEST022Fixture(t, false)
	server := f.servers[0]
	type businessState struct {
		Conversations, Messages, Notifications, Receipts, Outbox, UnreadMessages int64
		SummaryMessages, SummaryNotifications                                    int
	}
	snapshot := func(t *testing.T) businessState {
		t.Helper()
		var state businessState
		if err := f.db.QueryRow(f.ctx, `select
			(select count(*) from direct_conversations),
			(select count(*) from direct_messages),
			(select count(*) from notifications),
			(select count(*) from notification_receipts),
			(select count(*) from nats_outbox),
			(select count(*) from direct_messages where read_at is null)`).Scan(
			&state.Conversations, &state.Messages, &state.Notifications, &state.Receipts, &state.Outbox, &state.UnreadMessages); err != nil {
			t.Fatal(err)
		}
		body := f.require(t, f.editor, http.MethodGet, "/api/v1/me/unread-summary", nil, http.StatusOK)
		summary := decodeTEST022Data[struct{ Messages, Notifications int }](t, body)
		state.SummaryMessages, state.SummaryNotifications = summary.Messages, summary.Notifications
		return state
	}
	assertUnchanged := func(t *testing.T, before businessState) {
		t.Helper()
		if after := snapshot(t); !reflect.DeepEqual(after, before) {
			t.Fatalf("lookup failure mutated business state: before=%+v after=%+v", before, after)
		}
	}
	assertLookupError := func(t *testing.T, response *httptest.ResponseRecorder) {
		t.Helper()
		var envelope struct{ Code, Error string }
		if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusInternalServerError || envelope.Code != "HTTP_500" || envelope.Error != "读取目标用户失败" {
			t.Fatalf("wrong lookup failure boundary: status=%d body=%s", response.Code, response.Body.String())
		}
	}
	payload, err := json.Marshal(map[string]string{"userId": f.publicIDs[f.otherEditor]})
	if err != nil {
		t.Fatal(err)
	}
	invoke := func(ctx context.Context) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/messages/conversations", bytes.NewReader(payload))
		request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: f.userIDs[f.editor]}))
		response := httptest.NewRecorder()
		server.startConversation(response, request)
		return response
	}

	beforeMissing := snapshot(t)
	missing := f.require(t, f.editor, http.MethodPost, "/api/v1/messages/conversations", map[string]string{"userId": "zzzzzzzzz"}, http.StatusNotFound)
	var absent struct{ Code, Error string }
	if err := json.Unmarshal(missing, &absent); err != nil || absent.Code != "HTTP_404" || absent.Error != "目标用户不存在" {
		t.Fatalf("missing recipient did not use exact404: body=%s err=%v", missing, err)
	}
	assertUnchanged(t, beforeMissing)
	conversationID := f.start(t, 0, f.editor, f.otherEditor)
	beforeFault := snapshot(t)
	if beforeFault.Conversations != beforeMissing.Conversations+1 || beforeFault.Messages != beforeMissing.Messages {
		t.Fatalf("normal real HTTP did not establish exactly one conversation: before=%+v after=%+v", beforeMissing, beforeFault)
	}

	t.Run("same_original_real_postgres_missing_column", func(t *testing.T) {
		if _, err := f.db.Exec(f.ctx, `alter table users rename column public_id to audit_msg002_unavailable_public_id`); err != nil {
			t.Fatal(err)
		}
		restored := false
		restore := func() {
			t.Helper()
			if restored {
				return
			}
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := f.db.Exec(cleanup, `alter table users rename column audit_msg002_unavailable_public_id to public_id`); err != nil {
				t.Fatal(err)
			}
			restored = true
		}
		defer restore()
		assertLookupError(t, invoke(f.ctx))
		restore()
		assertUnchanged(t, beforeFault)
		t.Log("actual PostgreSQL42703 target SELECT returned exact500; original field restored; conversation/message/receipt/outbox/unread unchanged")
	})

	t.Run("actual_postgres_table_lock_reaches_deadline", func(t *testing.T) {
		locker, err := f.db.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer locker.Rollback(context.Background())
		if _, err = locker.Exec(f.ctx, `lock table users in access exclusive mode`); err != nil {
			t.Fatal(err)
		}
		requestContext, cancel := context.WithTimeout(f.ctx, time.Second)
		defer cancel()
		done := make(chan *httptest.ResponseRecorder, 1)
		go func() { done <- invoke(requestContext) }()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		waiting := false
		for !waiting {
			if err := f.db.QueryRow(f.ctx, `select exists(select 1 from pg_stat_activity
				where datname=current_database() and wait_event_type='Lock'
				and query=$1)`, `select id from users where public_id=$1 and status='active'`).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			if waiting {
				break
			}
			select {
			case response := <-done:
				t.Fatalf("request finished before reaching actual PostgreSQL lock: status=%d body=%s", response.Code, response.Body.String())
			case <-ticker.C:
			}
		}
		response := <-done
		if !errors.Is(requestContext.Err(), context.DeadlineExceeded) {
			t.Fatalf("expected the blocked real database query to hit its deadline, got %v", requestContext.Err())
		}
		assertLookupError(t, response)
		if err := locker.Rollback(f.ctx); err != nil {
			t.Fatal(err)
		}
		assertUnchanged(t, beforeFault)
		t.Log("pg_stat_activity confirmed target SELECT waited on actual table lock; deadline returned exact500; all business state unchanged")
	})

	if afterRecovery := f.start(t, 0, f.editor, f.otherEditor); afterRecovery != conversationID {
		t.Fatalf("restored normal HTTP changed existing conversation: before=%s after=%s", conversationID, afterRecovery)
	}
	assertUnchanged(t, beforeFault)
}
