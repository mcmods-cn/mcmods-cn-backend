package httpapi

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
)

func TestUnreadReconciliationQueryIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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
		create temporary table users(id bigint primary key,public_id text not null,status text not null);
		create temporary table notifications(id bigint primary key,recipient_id bigint,updated_at timestamptz not null);
		create index idx_notifications_recipient_id on notifications(recipient_id,id);
		create index idx_notifications_broadcast_id on notifications(id) where recipient_id is null;
		create temporary table notification_receipts(notification_id bigint not null,user_id bigint not null,read_at timestamptz,primary key(notification_id,user_id));
		create temporary table notification_read_watermarks(user_id bigint primary key,max_notification_id bigint not null,read_at timestamptz not null);
		create temporary table notification_broadcast_state(singleton boolean primary key,live_count bigint not null,max_notification_id bigint not null);
		create temporary table direct_messages(id bigint primary key,recipient_id bigint not null,read_at timestamptz);
		create index idx_direct_messages_recipient_read on direct_messages(recipient_id,read_at);
		insert into users values(1,'u00000001','active');
		insert into notifications values(1,null,clock_timestamp()),(2,1,clock_timestamp());
		insert into notification_broadcast_state values(true,1,1);
		insert into direct_messages values(1,1,null)
	`); err != nil {
		t.Fatal(err)
	}
	redisServer := miniredis.RunT(t)
	cache := querycache.New(config.RedisConfig{
		Enabled: true, Addr: redisServer.Addr(), Prefix: "mcmods", Namespace: "unread-integration",
		PoolSize: 2, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second,
		UnreadCounterEnabled: true, UnreadTTL: time.Minute,
	})
	defer cache.Close()
	var userID int64
	if err = pool.QueryRow(ctx, `select id from users where status='active' order by id limit 1`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err = cache.LoadUnread(ctx, userID, func(context.Context) (querycache.UnreadSummary, error) {
		return querycache.UnreadSummary{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	candidates := cache.UnreadReconciliationCandidates(5)
	if _, _, err = reconcileUnreadUsers(ctx, pool, cache, candidates); err != nil {
		t.Fatal(err)
	}
	value, err := cache.LoadUnread(ctx, userID, func(context.Context) (querycache.UnreadSummary, error) {
		return querycache.UnreadSummary{}, nil
	})
	if err != nil || value.Notifications != 2 || value.Messages != 1 {
		t.Fatalf("reconciled unread value=%+v err=%v", value, err)
	}
}
