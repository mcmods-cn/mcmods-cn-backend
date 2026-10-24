package httpapi

import (
	"net/http"

	"mcmods-cn-backend/internal/activity"
)

// Responses that disclose a prepared capability have no other business rows to
// commit. Persist their existing durable event before disclosing the response;
// view/best-effort events and explicit skips retain their original behavior.
func (s *Server) commitRequestActivity(r *http.Request) error {
	annotation, _ := r.Context().Value(requestActivityContextKey).(*requestActivity)
	event, valid := requestActivityEvent(r, annotation)
	if !valid || !activity.IsDurableAction(event.ActionID) {
		return nil
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if err = enqueueRequestActivityTx(r, tx); err != nil {
		return err
	}
	return tx.Commit(r.Context())
}
