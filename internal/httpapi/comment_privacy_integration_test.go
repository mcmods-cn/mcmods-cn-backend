package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

func TestCommentIDRoutesRespectPrivateTargetIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	s := &Server{db: pool}
	unique := fmt.Sprintf("comment_private_%d", time.Now().UnixNano())
	var ownerID, viewerID, targetID int64
	var rootID, childID int64
	for index, dest := range []*int64{&ownerID, &viewerID} {
		if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash) values($1,$2,'test') returning id`,
			fmt.Sprintf("%s_%d", unique, index), fmt.Sprintf("%s_%d@example.test", unique, index)).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, cleanup := range []struct {
			sql  string
			args []any
		}{
			{`delete from comments where id=any($1::bigint[])`, []any{[]int64{rootID, childID}}},
			{`delete from comment_floor_counters where target_type='player_profile' and target_id=$1`, []any{targetID}},
			{`delete from player_profiles where id=$1`, []any{targetID}},
			{`delete from users where id=any($1::bigint[])`, []any{[]int64{ownerID, viewerID}}},
		} {
			if _, err := pool.Exec(context.Background(), cleanup.sql, cleanup.args...); err != nil {
				t.Error(err)
			}
		}
	})
	if err = pool.QueryRow(ctx, `insert into player_profiles(user_id,uuid,name,visibility)
		values($1,gen_random_uuid(),$2,'public') returning id`, ownerID, "Test_"+randomHex(4)).Scan(&targetID); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	target := commentTargetInfo{Type: "player_profile", InternalID: targetID}
	root, err := insertCommentTree(ctx, tx, target, ownerID, createCommentRequest{Body: "root", Status: "published", IdempotencyKey: unique + "root"})
	if err != nil {
		t.Fatal(err)
	}
	rootID = root.ID
	child, err := insertCommentTree(ctx, tx, target, ownerID, createCommentRequest{Body: "private reply content", ParentID: root.PublicID, Status: "published", IdempotencyKey: unique + "child"})
	if err != nil {
		t.Fatal(err)
	}
	childID = child.ID
	if _, err = tx.Exec(ctx, `insert into comment_watches(user_id,comment_id) values($1,$2)`, viewerID, rootID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `update player_profiles set visibility='private' where id=$1`, targetID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	viewer := security.Claims{Subject: viewerID, PermissionRules: []security.PermissionRule{
		{Code: "comment.watch", Allow: true, Priority: 100}, {Code: "comment.react", Allow: true, Priority: 100},
	}}
	request := func(method string, claims security.Claims) *http.Request {
		r := httptest.NewRequest(method, "/api/v1/comments/"+root.PublicID+"?reaction=heart", strings.NewReader(`{"reaction":"heart"}`))
		r.SetPathValue("commentId", root.PublicID)
		return r.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	}
	for _, test := range []struct {
		name    string
		method  string
		claims  security.Claims
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"anonymous-replies", http.MethodGet, security.Claims{}, s.commentReplies},
		{"other-user-replies", http.MethodGet, viewer, s.commentReplies},
		{"other-user-reaction", http.MethodPut, viewer, s.commentReaction},
		{"other-user-watch", http.MethodPut, viewer, s.commentWatch},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			test.handler(response, request(test.method, test.claims))
			if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "private reply content") {
				t.Fatalf("private target leaked/accepted operation: status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	response := httptest.NewRecorder()
	s.myCommentWatches(response, request(http.MethodGet, viewer))
	var watched struct {
		Data struct{ Items []json.RawMessage } `json:"data"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &watched); err != nil || response.Code != http.StatusOK || watched.Data.Items == nil || len(watched.Data.Items) != 0 {
		t.Fatalf("old watch exposed a newly private target: status=%d body=%s err=%v", response.Code, response.Body.String(), err)
	}
	response = httptest.NewRecorder()
	s.commentReplies(response, request(http.MethodGet, security.Claims{Subject: ownerID}))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "private reply content") {
		t.Fatalf("owner lost access: status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err = pool.Exec(ctx, `update player_profiles set visibility='public' where id=$1`, targetID); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	s.commentReplies(response, request(http.MethodGet, viewer))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "private reply content") {
		t.Fatalf("public replies stopped working: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestConcurrentCommentWatchesRespectUserLimitIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	unique := fmt.Sprintf("watch_limit_%d", time.Now().UnixNano())
	var userID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash) values($1,$2,'test') returning id`, unique, unique+"@example.test").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, statement := range []string{`delete from comments where author_id=$1`, `delete from users where id=$1`} {
			if _, err := pool.Exec(context.Background(), statement, userID); err != nil {
				t.Error(err)
			}
		}
	})
	rows, err := pool.Query(ctx, `insert into comments(target_type,target_id,author_id,body)
		select 'player_profile',$1,$1,'synthetic quota fixture' from generate_series(1,2007) returning id`, userID)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into comment_watches(user_id,comment_id)
		select $1,unnest($2::bigint[])`, userID, ids[:1999]); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: pool}
	start := make(chan struct{})
	results := make(chan error, 8)
	var workers sync.WaitGroup
	for _, id := range ids[1999:] {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, err := s.ensureCommentWatch(ctx, userID, id)
			results <- err
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if err.Error() != "watch limit exceeded" {
			t.Fatal(err)
		}
	}
	var active int
	if err = pool.QueryRow(ctx, `select count(*) from comment_watches where user_id=$1 and status='active'`, userID).Scan(&active); err != nil || succeeded != 1 || active != 2000 {
		t.Fatalf("watch quota exceeded: success=%d active=%d err=%v", succeeded, active, err)
	}
	// Repeating an existing watch at the limit remains idempotently successful.
	if state, err := s.ensureCommentWatch(ctx, userID, ids[0]); err != nil || !state.Active {
		t.Fatalf("existing watch lost at quota limit: state=%+v err=%v", state, err)
	}
}
