package httpapi

import (
	"context"
	"time"
)

// PostgreSQL preserves admission across a volatile Redis restart. Redis still
// has to grant its lease; neither dependency's failure enables local admission.
func (s *Server) acquireRealtimeDatabaseLease(ctx context.Context, id string, userID int64, session string) (bool, error) {
	tx, err := s.db.Begin(ctx)
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

func (s *Server) refreshRealtimeDatabaseLease(ctx context.Context, id string) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tag, err := s.db.Exec(ctx, `update realtime_connection_leases set expires_at=clock_timestamp()+$2*interval '1 millisecond' where lease_id=$1 and expires_at>clock_timestamp()`, id, realtimeLeaseTTL.Milliseconds())
	return err == nil && tag.RowsAffected() == 1
}

func (s *Server) releaseRealtimeDatabaseLease(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = s.db.Exec(ctx, `delete from realtime_connection_leases where lease_id=$1`, id)
}
