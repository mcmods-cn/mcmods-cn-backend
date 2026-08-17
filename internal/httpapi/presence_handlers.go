package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mcmods-cn-backend/internal/security"
)

const (
	publicPresenceWindow            = 5 * time.Minute
	defaultPresenceSnapshotInterval = 10 * time.Minute
)

type publicOnlineStatus string

const (
	publicOnlineStatusOnline  publicOnlineStatus = "online"
	publicOnlineStatusOffline publicOnlineStatus = "offline"
	publicOnlineStatusHidden  publicOnlineStatus = "hidden"
)

type presenceRequest struct {
	VisitorID string `json:"visitorId"`
}

func (s *Server) touchSitePresence(w http.ResponseWriter, r *http.Request) {
	var request presenceRequest
	_ = decodeJSON(r, &request)
	visitorID := strings.TrimSpace(request.VisitorID)
	if len(visitorID) < 16 || len(visitorID) > 128 {
		location := s.requestClientLocation(r)
		visitorID = location.IP + "\x00" + r.UserAgent()
	}
	claims := currentClaims(r)
	if claims.Subject > 0 {
		visitorID = "user:" + strconv.FormatInt(claims.Subject, 10)
		if claims.SessionID != "" {
			sessionHash := hex.EncodeToString(security.SessionFingerprint(claims.SessionID))
			s.cache.TouchUserPresence(r.Context(), claims.Subject, sessionHash, time.Now().UTC(), s.cache.Config().PresenceTTL)
			// PostgreSQL only receives a low-frequency activity snapshot. Redis
			// outages therefore cannot turn every heartbeat into a database write.
			snapshotInterval := s.cache.Config().PresenceSnapshotInterval
			if snapshotInterval <= 0 {
				snapshotInterval = defaultPresenceSnapshotInterval
			}
			if s.cache.ClaimThrottle(r.Context(), "presence-snapshot:"+strconv.FormatInt(claims.Subject, 10)+":"+sessionHash, snapshotInterval) {
				_, _ = s.db.Exec(r.Context(), `insert into user_presence_sessions(session_hash,user_id,last_active_at,updated_at)
					values($1,$2,now(),now()) on conflict(session_hash) do update set
					last_active_at=now(),updated_at=now()`, security.SessionFingerprint(claims.SessionID), claims.Subject)
			}
		}
	}
	digest := sha256.Sum256([]byte(visitorID))
	s.cache.TouchPresence(r.Context(), hex.EncodeToString(digest[:]), time.Now().UTC())
	writeJSON(w, http.StatusOK, map[string]bool{"online": true})
}

func mapPublicOnlineStatus(show bool, lastActiveAt *time.Time, now time.Time) publicOnlineStatus {
	return mapPublicOnlineVisibility(show, lastActiveAt != nil && !lastActiveAt.Before(now.Add(-publicPresenceWindow)))
}

func mapPublicOnlineVisibility(show bool, active bool) publicOnlineStatus {
	if !show {
		return publicOnlineStatusHidden
	}
	if active {
		return publicOnlineStatusOnline
	}
	return publicOnlineStatusOffline
}
