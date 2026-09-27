package httpapi

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestDirectMessagePagesStayBoundedAtOneMillionMessagesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to validate direct-message cursor scale")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	counter := &integrationQueryCounter{}
	poolConfig.ConnConfig.Tracer = counter
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	fixtureStarted := time.Now()
	if _, err = pool.Exec(ctx, `
		create temporary table users(
			id bigint primary key,public_id text not null,username text not null,avatar_url text not null default '',
			show_online_status boolean not null default false
		);
		create temporary table user_blocks(blocker_id bigint not null,blocked_id bigint not null,primary key(blocker_id,blocked_id));
		create temporary table direct_conversations(
			id bigint primary key,public_id text not null,user_low_id bigint not null,user_high_id bigint not null,
			last_message_id bigint,created_at timestamptz not null,updated_at timestamptz not null
		);
		create index idx_direct_conversations_low_page on direct_conversations(user_low_id,updated_at desc,id desc);
		create index idx_direct_conversations_high_page on direct_conversations(user_high_id,updated_at desc,id desc);
		create temporary table direct_messages(
			id bigint primary key,public_id text not null,conversation_id bigint not null,sender_id bigint not null,
			recipient_id bigint not null,body text not null,read_at timestamptz,created_at timestamptz not null
		);
		create unique index idx_direct_messages_public_id on direct_messages(public_id);
		create index idx_direct_messages_conversation_id on direct_messages(conversation_id,id desc);
		create index idx_direct_messages_unread_conversation on direct_messages(recipient_id,conversation_id,id) where read_at is null;
		create temporary table direct_conversation_unread_counts(
			conversation_id bigint not null,user_id bigint not null,unread_count bigint not null,updated_at timestamptz not null,
			primary key(conversation_id,user_id)
		);
		insert into users
		select value,'u'||lpad(value::text,8,'0'),'Scale user '||value,'',false from generate_series(1,100001) value;
		insert into direct_conversations
		select value,'c'||lpad(value::text,8,'0'),1,value+1,null,
			timestamptz '2026-01-01 00:00:00+00'+value*interval '1 second',
			timestamptz '2026-01-01 00:00:00+00'+value*interval '1 second'
		from generate_series(1,100000) value;
		insert into direct_messages
		select value,'m'||lpad(value::text,8,'0'),value,value+1,1,'message-'||value,null,
			timestamptz '2026-01-01 00:00:00+00'+value*interval '1 millisecond'
		from generate_series(1,100000) value;
		insert into direct_messages
		select value,'m'||lpad(value::text,8,'0'),1,2,1,'message-'||value,null,
			timestamptz '2026-01-01 00:00:00+00'+value*interval '1 millisecond'
		from generate_series(100001,1000000) value;
		update direct_conversations set last_message_id=case when id=1 then 1000000 else id end;
		insert into direct_conversation_unread_counts
		select id,1,case when id=1 then 900001 else 1 end,clock_timestamp() from direct_conversations;
		analyze users;
		analyze user_blocks;
		analyze direct_conversations;
		analyze direct_messages;
		analyze direct_conversation_unread_counts
	`); err != nil {
		t.Fatal(err)
	}
	t.Logf("100k conversation / 1m message synthetic fixture loaded in %s", time.Since(fixtureStarted))

	server := &Server{db: pool}
	conversationCursor := ""
	seenConversations := make(map[string]struct{}, 60)
	counter.queries.Store(0)
	for pageNumber := 0; pageNumber < 2; pageNumber++ {
		request, parseErr := parseConversationPageRequest(url.Values{
			"limit": {"30"}, "cursor": {conversationCursor},
		}, 1)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		page, pageErr := server.loadConversationPage(ctx, 1, request)
		if pageErr != nil {
			t.Fatal(pageErr)
		}
		if len(page.Items) != 30 || !page.HasMore || page.NextCursor == "" {
			t.Fatalf("conversation page %d items=%d hasMore=%t cursor=%q", pageNumber, len(page.Items), page.HasMore, page.NextCursor)
		}
		for _, item := range page.Items {
			if _, duplicate := seenConversations[item.ID]; duplicate {
				t.Fatalf("duplicate conversation %s", item.ID)
			}
			seenConversations[item.ID] = struct{}{}
			if item.LastMessage == "" || item.UnreadCount != 1 {
				t.Fatalf("conversation summary=%+v", item)
			}
		}
		conversationCursor = page.NextCursor
	}
	if queries := counter.queries.Load(); queries != 2 {
		t.Fatalf("two conversation pages executed %d SQL statements, want 2", queries)
	}

	messageCursor := ""
	seenMessages := make(map[string]struct{}, 200)
	counter.queries.Store(0)
	for pageNumber := 0; pageNumber < 2; pageNumber++ {
		request, parseErr := parseDirectMessagePageRequest(url.Values{
			"limit": {"100"}, "cursor": {messageCursor},
		}, 1, 1)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		page, pageErr := server.loadDirectMessagePage(ctx, 1, "c00000001", 1, request)
		if pageErr != nil {
			t.Fatal(pageErr)
		}
		if len(page.Items) != 100 || !page.HasMore || page.NextCursor == "" {
			t.Fatalf("message page %d items=%d hasMore=%t cursor=%q", pageNumber, len(page.Items), page.HasMore, page.NextCursor)
		}
		for index, item := range page.Items {
			if _, duplicate := seenMessages[item.ID]; duplicate {
				t.Fatalf("duplicate message %s", item.ID)
			}
			seenMessages[item.ID] = struct{}{}
			if index > 0 && page.Items[index-1].internalID >= item.internalID {
				t.Fatalf("message page is not chronological: %d then %d", page.Items[index-1].internalID, item.internalID)
			}
		}
		messageCursor = page.NextCursor
	}
	if queries := counter.queries.Load(); queries != 2 {
		t.Fatalf("two message pages executed %d SQL statements, want 2", queries)
	}

	incrementalRequest, err := parseDirectMessagePageRequest(url.Values{
		"limit": {"25"}, "after": {"m00999800"},
	}, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	incremental, err := server.loadDirectMessagePage(ctx, 1, "c00000001", 1, incrementalRequest)
	if err != nil {
		t.Fatal(err)
	}
	if len(incremental.Items) != 25 || !incremental.HasMore || incremental.Items[0].internalID != 999801 || incremental.Items[24].internalID != 999825 {
		t.Fatalf("incremental page first/last/count/more=%d/%d/%d/%t", incremental.Items[0].internalID, incremental.Items[len(incremental.Items)-1].internalID, len(incremental.Items), incremental.HasMore)
	}
	latestRequest, err := parseDirectMessagePageRequest(url.Values{"limit": {"25"}, "after": {"m01000000"}}, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	counter.queries.Store(0)
	latest, err := server.loadDirectMessagePage(ctx, 1, "c00000001", 1, latestRequest)
	if err != nil || len(latest.Items) != 0 || latest.HasMore {
		t.Fatalf("valid latest anchor page=%+v err=%v", latest, err)
	}
	if queries := counter.queries.Load(); queries != 2 {
		t.Fatalf("empty valid incremental page executed %d SQL statements, want page plus anchor validation", queries)
	}
	otherConversationRequest, err := parseDirectMessagePageRequest(url.Values{"limit": {"25"}, "after": {"m00050000"}}, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	counter.queries.Store(0)
	if _, err = server.loadDirectMessagePage(ctx, 1, "c00000001", 1, otherConversationRequest); !errors.Is(err, errInvalidDirectMessageAfterAnchor) {
		t.Fatalf("cross-conversation anchor error=%v", err)
	}
	if queries := counter.queries.Load(); queries != 2 {
		t.Fatalf("invalid incremental anchor executed %d SQL statements, want page plus anchor validation", queries)
	}

	conversationRequest, err := parseConversationPageRequest(url.Values{"limit": {"30"}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	conversationQuery, conversationArgs := conversationPageSQL(1, conversationRequest)
	assertDirectMessageScalePlan(t, ctx, pool, "conversation page", conversationQuery, conversationArgs,
		[]string{"idx_direct_conversations_low_page", "idx_direct_conversations_high_page", "direct_conversation_unread_counts_pkey"})
	messageRequest, err := parseDirectMessagePageRequest(url.Values{"limit": {"100"}}, 1, 50000)
	if err != nil {
		t.Fatal(err)
	}
	messageQuery, messageArgs := directMessagePageSQL(50000, 1, messageRequest)
	assertDirectMessageScalePlan(t, ctx, pool, "message history", messageQuery, messageArgs,
		[]string{"idx_direct_messages_conversation_id"})
	assertDirectMessageScalePlan(t, ctx, pool, "unread conversation", `select id from direct_messages
		where recipient_id=$1 and conversation_id=$2 and read_at is null order by id limit $3`, []any{int64(1), int64(50000), 100},
		[]string{"idx_direct_messages_unread_conversation"})
	assertDirectMessageScalePlan(t, ctx, pool, "empty incremental anchor validation",
		`select exists(select 1 from direct_messages where public_id=$1 and conversation_id=$2)`,
		[]any{"m01000000", int64(1)}, []string{"idx_direct_messages_public_id"})
}

func assertDirectMessageScalePlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name, query string, args []any, indexes []string) {
	t.Helper()
	rows, err := pool.Query(ctx, "explain (analyze,buffers,format text) "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	lines := make([]string, 0)
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	plan := strings.Join(lines, "\n")
	for _, index := range indexes {
		if !strings.Contains(plan, index) {
			t.Fatalf("%s did not use %s:\n%s", name, index, plan)
		}
	}
	if strings.Contains(plan, "Seq Scan on direct_conversations") || strings.Contains(plan, "Seq Scan on direct_messages") {
		t.Fatalf("%s scanned a scale table:\n%s", name, plan)
	}
	t.Logf("%s plan:\n%s", name, plan)
}
