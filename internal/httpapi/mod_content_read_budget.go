package httpapi

import (
	"fmt"
	"net/http"
	"time"
)

func (s *Server) allowModContentRead(w http.ResponseWriter, r *http.Request, scope string, anonymousLimit int) bool {
	if s.antiAbuse == nil || !s.antiAbuse.Enabled() {
		return true
	}
	claims := currentClaims(r)
	identity := s.requestClientLocation(r).IP
	limit := anonymousLimit
	if claims.Subject > 0 {
		identity += fmt.Sprintf(":user:%d", claims.Subject)
		limit *= 2
	}
	allowed, retry := s.antiAbuse.ExpensiveReadLimit(r.Context(), scope, identity, limit, time.Minute)
	if allowed {
		return true
	}
	seconds := max(1, int(retry.Round(time.Second).Seconds()))
	writeAPIError(w, http.StatusTooManyRequests, "MOD_CONTENT_READ_RATE_LIMIT", "mod content read rate exceeded", seconds, nil)
	return false
}
