package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestBUG045DirectMessageAfterAnchorMustBelongToConversationIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify direct-message incremental anchors")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `
		create temporary table users(
			id bigint primary key,public_id text not null,show_online_status boolean not null default false
		);
		create temporary table direct_conversations(
			id bigint primary key,public_id text not null unique,user_low_id bigint not null,user_high_id bigint not null
		);
		create temporary table direct_messages(
			id bigint primary key,public_id text not null unique,conversation_id bigint not null,sender_id bigint not null,
			recipient_id bigint not null,body text not null,read_at timestamptz,created_at timestamptz not null
		);
		create index idx_bug045_messages_conversation_id on direct_messages(conversation_id,id desc);
		insert into users(id,public_id) values(1,'u00000001'),(2,'u00000002'),(3,'u00000003');
		insert into direct_conversations(id,public_id,user_low_id,user_high_id) values
			(1,'c00000001',1,2),(2,'c00000002',1,3);
		insert into direct_messages(id,public_id,conversation_id,sender_id,recipient_id,body,created_at) values
			(10,'m00000010',1,2,1,'first',now()),
			(11,'m00000011',1,2,1,'second',now()),
			(20,'m00000020',2,3,1,'other conversation',now())
	`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool, realtime: newRealtimeHub()}
	invoke := func(anchor string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/messages/conversations/c00000001?limit=25&after="+anchor, nil).WithContext(
			context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 1, PublicSubject: "u00000001"}),
		)
		request.SetPathValue("id", "c00000001")
		response := httptest.NewRecorder()
		server.conversationMessages(response, request)
		return response
	}
	resetUnread := func(t *testing.T) {
		t.Helper()
		if _, resetErr := pool.Exec(ctx, `update direct_messages set read_at=null where conversation_id=1`); resetErr != nil {
			t.Fatal(resetErr)
		}
	}
	assertUnread := func(t *testing.T, want int) {
		t.Helper()
		var unread int
		if queryErr := pool.QueryRow(ctx, `select count(*) from direct_messages where conversation_id=1 and recipient_id=1 and read_at is null`).Scan(&unread); queryErr != nil || unread != want {
			t.Fatalf("unread=%d want=%d err=%v", unread, want, queryErr)
		}
	}

	for name, anchor := range map[string]string{
		"malformed":          "not-valid-anchor",
		"missing":            "m99999999",
		"other conversation": "m00000020",
	} {
		t.Run(name, func(t *testing.T) {
			resetUnread(t)
			response := invoke(anchor)
			if response.Code != http.StatusBadRequest {
				t.Errorf("anchor=%q status=%d body=%s", anchor, response.Code, response.Body.String())
			}
			assertUnread(t, 2)
		})
	}

	t.Run("valid anchor remains incremental and marks read", func(t *testing.T) {
		resetUnread(t)
		response := invoke("m00000010")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "m00000011") || !strings.Contains(response.Body.String(), "second") {
			t.Fatalf("valid anchor status=%d body=%s", response.Code, response.Body.String())
		}
		assertUnread(t, 0)
	})

	t.Run("valid latest anchor is distinguishable from an invalid anchor", func(t *testing.T) {
		resetUnread(t)
		response := invoke("m00000011")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"items":[]`) {
			t.Fatalf("latest anchor status=%d body=%s", response.Code, response.Body.String())
		}
		assertUnread(t, 0)
	})
}
