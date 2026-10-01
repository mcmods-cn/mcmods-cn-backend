package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestConversationReadFailureIsNotReportedAsSuccessIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	server := &Server{cfg: cfg, db: pool}
	var first, second int64
	for index, dest := range []*int64{&first, &second} {
		if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
			values('message-read-audit-'||$1::text,'message-read-audit-'||$1::text||'@example.invalid','synthetic',true) returning id`, strconv.Itoa(index)).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	var conversationID int64
	var publicID string
	if err := pool.QueryRow(ctx, `insert into direct_conversations(user_low_id,user_high_id) values($1,$2) returning id,public_id`, first, second).Scan(&conversationID, &publicID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into direct_messages(conversation_id,sender_id,recipient_id,body) values($1,$2,$3,'synthetic unread message')`, conversationID, first, second); err != nil {
		t.Fatal(err)
	}
	// Inject an update failure only into the identity-verified disposable DB.
	if _, err := pool.Exec(ctx, `create function audit_fail_message_read() returns trigger language plpgsql as $$
		begin raise exception 'synthetic read receipt outage'; end $$;
		create trigger audit_fail_message_read before update of read_at on direct_messages
		for each row execute function audit_fail_message_read()`); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/messages/conversations/"+publicID, nil)
	request.SetPathValue("id", publicID)
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{Subject: second}))
	response := httptest.NewRecorder()
	server.conversationMessages(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("read receipt outage was hidden by status %d", response.Code)
	}
	var unread int
	if err := pool.QueryRow(ctx, `select count(*) from direct_messages where conversation_id=$1 and read_at is null`, conversationID).Scan(&unread); err != nil || unread != 1 {
		t.Fatal("failed read changed unread message state")
	}
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{Subject: 0}))
	response = httptest.NewRecorder()
	server.conversationMessages(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-member read status: %d", response.Code)
	}
}
