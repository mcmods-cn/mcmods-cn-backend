package httpapi

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// realtimeLeaseCoordinator owns the durable admission lease for one stream.
// The normal server always uses PostgreSQL. The private dependency permits a
// middleware unit test to supply its own coordinator without a runtime setting
// that could weaken cross-instance admission.
type realtimeLeaseCoordinator interface {
	Acquire(context.Context, string, int64, string) (bool, error)
	Refresh(context.Context, string) bool
	Release(string)
}

type postgresRealtimeLeaseCoordinator struct {
	db *pgxpool.Pool
}

func (s *Server) realtimeLeaseCoordinator() realtimeLeaseCoordinator {
	if s.realtimeAdmission != nil {
		return s.realtimeAdmission
	}
	return postgresRealtimeLeaseCoordinator{db: s.db}
}

// PostgreSQL preserves admission across a volatile Redis restart. The cache
// policy grants the first lease (shared, or optional single-instance local);
// neither mode bypasses this durable cross-instance quota.
func (store postgresRealtimeLeaseCoordinator) Acquire(ctx context.Context, id string, userID int64, session string) (bool, error) {
	if store.db == nil {
		return false, errors.New("realtime admission database is unavailable")
	}
	tx, err := store.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('mcmods-realtime-admission'))`); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `delete from realtime_connection_leases where expires_at<=clock_timestamp()`); err != nil {
		return false, err
	}
	var total, user, perSession int
	if err = tx.QueryRow(ctx, `select count(*),count(*) filter(where user_id=$1),count(*) filter(where session_hash=$2) from realtime_connection_leases`, userID, session).Scan(&total, &user, &perSession); err != nil {
		return false, err
	}
	if total >= maxRealtimeConnections || user >= maxRealtimeConnectionsPerUser || perSession >= maxRealtimeConnectionsPerSession {
		return false, nil
	}
	if _, err = tx.Exec(ctx, `insert into realtime_connection_leases(lease_id,user_id,session_hash,expires_at) values($1,$2,$3,clock_timestamp()+$4*interval '1 millisecond')`, id, userID, session, realtimeLeaseTTL.Milliseconds()); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (store postgresRealtimeLeaseCoordinator) Refresh(ctx context.Context, id string) bool {
	if store.db == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tag, err := store.db.Exec(ctx, `update realtime_connection_leases set expires_at=clock_timestamp()+$2*interval '1 millisecond' where lease_id=$1 and expires_at>clock_timestamp()`, id, realtimeLeaseTTL.Milliseconds())
	return err == nil && tag.RowsAffected() == 1
}

func (store postgresRealtimeLeaseCoordinator) Release(id string) {
	if store.db == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = store.db.Exec(ctx, `delete from realtime_connection_leases where lease_id=$1`, id)
}
