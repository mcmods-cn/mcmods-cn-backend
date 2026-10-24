package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"mcmods-cn-backend/internal/activity"
)

func TestAuditFixCMP003FavoriteSummaryIsReadViewWithoutCreateReward(t *testing.T) {
	f := newTEST013Fixture(t)
	if _, err := f.db.Exec(f.ctx, `update task_definitions set status='disabled'; insert into task_definitions(code,name,condition,rewards,status) values('owned_summary_view','Owned summary read','{"action":"create","objectType":"user","metric":"count","target":1}'::jsonb,'{"experience":25,"currencies":{"diamond":3}}'::jsonb,'active')`); err != nil {
		t.Fatal(err)
	}
	monitor := auditFixActivityMonitor(t, f)
	original := f.origin.Config.Handler
	finished := make(chan struct{}, 2)
	route := "/api/v1/users/me/favorites/summary"
	f.origin.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		original.ServeHTTP(w, r)
		if r.URL.Path == route {
			finished <- struct{}{}
		}
	})
	for range 2 {
		f.require(t, f.editor, http.MethodPost, route, favoriteMembershipSummaryRequest{EntityType: "mod", EntityPublicIDs: []string{f.modCode}}, 200)
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			t.Fatal("summary request activity did not finish")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := monitor.Close(ctx); err != nil {
		t.Fatal(err)
	}
	var creates, views, rewards, xp, coins, ledgers int64
	if err := f.db.QueryRow(f.ctx, `select (select count(*) from user_activity_events where user_id=$1 and action_id=$2 and object_type_id=$3),(select count(*) from user_activity_events where user_id=$1 and action_id=$4 and object_type_id=$3),(select count(*) from user_task_progress where user_id=$1 and rewarded_at is not null),coalesce((select experience from user_experience where user_id=$1),0),coalesce((select b.balance from user_currency_balances b join currencies c on c.id=b.currency_id where b.user_id=$1 and c.code='diamond'),0),(select count(*) from currency_transactions where user_id=$1)`, f.userIDs[f.editor], activity.ActionCreate, activity.ObjectUser, activity.ActionView).Scan(&creates, &views, &rewards, &xp, &coins, &ledgers); err != nil {
		t.Fatal(err)
	}
	if creates+rewards+xp+coins+ledgers != 0 || views != 1 {
		t.Fatalf("read-only POST creates/views/reward/xp/coins/ledgers=%d/%d/%d/%d/%d/%d want0/1/0/0/0/0", creates, views, rewards, xp, coins, ledgers)
	}
}
