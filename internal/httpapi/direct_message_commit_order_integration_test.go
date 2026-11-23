package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestTEST022MessageCursorPreservesConcurrentSendsIntegration(t *testing.T) {
	f := newTEST022Fixture(t, false)
	conversation := f.start(t, 0, f.editor, f.otherEditor)
	path := "/api/v1/messages/conversations/" + conversation
	if _, err := f.db.Exec(f.ctx, `create function test022_pause_first_send() returns trigger language plpgsql as $$
		begin
			if new.body='first concurrent message' then
				perform pg_advisory_xact_lock(540022);
			end if;
			return new;
		end; $$;
		create trigger test022_pause_first_send before insert on direct_messages
			for each row execute function test022_pause_first_send()`); err != nil {
		t.Fatal(err)
	}
	barrier, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer barrier.Rollback(f.ctx)
	var holder int32
	if err = barrier.QueryRow(f.ctx, `select pg_backend_pid() from pg_advisory_xact_lock(540022)`).Scan(&holder); err != nil {
		t.Fatal(err)
	}

	type delivery struct {
		Message directMessageItem `json:"message"`
	}
	anchor := decodeTEST022Data[delivery](t, f.requireAt(t, 0, f.editor, http.MethodPost, path, sendDirectMessageRequest{Body: "initial message"}, http.StatusCreated))
	type sendResult struct {
		status int
		err    error
	}
	send := func(index int, token, body string, result chan<- sendResult) {
		raw, err := json.Marshal(sendDirectMessageRequest{Body: body})
		if err != nil {
			result <- sendResult{err: err}
			return
		}
		request, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.origins[index].URL+path, bytes.NewReader(raw))
		if err != nil {
			result <- sendResult{err: err}
			return
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := f.origins[index].Client().Do(request)
		if err != nil {
			result <- sendResult{err: err}
			return
		}
		defer response.Body.Close()
		result <- sendResult{status: response.StatusCode}
	}
	first, second := make(chan sendResult, 1), make(chan sendResult, 1)
	go send(0, f.editor, "first concurrent message", first)
	waitForBlockedSend := func(blocker int32, finished <-chan sendResult) int32 {
		t.Helper()
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case result := <-finished:
				t.Fatalf("send completed before the older insert committed: %+v", result)
			case <-f.ctx.Done():
				t.Fatal(f.ctx.Err())
			case <-tick.C:
				var waiter int32
				if err := f.db.QueryRow(f.ctx, `select coalesce((
					select pid from pg_stat_activity where datname=current_database() and
						wait_event_type='Lock' and $1=any(pg_blocking_pids(pid)) limit 1
				),0)`, blocker).Scan(&waiter); err != nil {
					t.Fatal(err)
				}
				if waiter != 0 {
					return waiter
				}
			}
		}
	}
	writer := waitForBlockedSend(holder, first)
	go send(1, f.otherEditor, "second concurrent message", second)
	waitForBlockedSend(writer, second)
	if err = barrier.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	for _, result := range []<-chan sendResult{first, second} {
		select {
		case sent := <-result:
			if sent.err != nil || sent.status != http.StatusCreated {
				t.Fatalf("concurrent send = %+v", sent)
			}
		case <-f.ctx.Done():
			t.Fatal(f.ctx.Err())
		}
	}
	type page struct {
		Items []directMessageItem `json:"items"`
	}
	next := decodeTEST022Data[page](t, f.requireAt(t, 0, f.editor, http.MethodGet, path+"?after="+anchor.Message.ID, nil, http.StatusOK))
	if len(next.Items) != 2 || next.Items[0].Body != "first concurrent message" || next.Items[1].Body != "second concurrent message" {
		t.Fatalf("incremental cursor lost order or a committed message: %+v", next.Items)
	}
}
