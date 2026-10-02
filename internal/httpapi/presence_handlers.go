package httpapi

import (
	"crypto/hmac"
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
	presenceRateWindow              = 5 * time.Minute
	presenceSharedRequestsPerWindow = 60
	presenceLocalRequestsPerWindow  = 12
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
	location := s.requestClientLocation(r)
	claims := currentClaims(r)
	// Client visitor IDs do not participate in identity or admission. Reject
	// exhausted sources before spending the JSON body parsing budget.
	visitorToken, visitorID, sourceID := anonymousPresenceIdentity(s.cfg.AntiAbuse.HMACSecret, location.IP, r.UserAgent(), "")
	if claims.Subject > 0 {
		visitorID = "user:" + strconv.FormatInt(claims.Subject, 10)
		visitorToken = ""
		sourceID = visitorID
	}
	limit := s.cache.ConsumeRateLimitPolicy(r.Context(), "presence:"+sourceID,
		presenceSharedRequestsPerWindow, presenceLocalRequestsPerWindow, presenceRateWindow)
	if !limit.Allowed {
		retryAfter := max(1, int(limit.RetryAfter.Round(time.Second)/time.Second))
		writeAPIError(w, http.StatusTooManyRequests, "PRESENCE_RATE_LIMIT", "presence heartbeat rate exceeded", retryAfter, nil)
		return
	}
	var request presenceRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "presence heartbeat request is invalid")
		return
	}
	if claims.Subject > 0 && claims.SessionID != "" {
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
	digest := sha256.Sum256([]byte(visitorID))
	s.cache.TouchPresence(r.Context(), hex.EncodeToString(digest[:]), time.Now().UTC())
	response := map[string]any{"online": true}
	if visitorToken != "" {
		response["visitorId"] = visitorToken
	}
	writeJSON(w, http.StatusOK, response)
}

func anonymousPresenceIdentity(secret, clientIP, userAgent, _ string) (token, fingerprint, source string) {
	secret = strings.TrimSpace(secret)
	clientIP = strings.TrimSpace(clientIP)
	userAgent = strings.ToLower(strings.Join(strings.Fields(userAgent), " "))
	if len(userAgent) > 256 {
		userAgent = userAgent[:256]
	}
	sign := func(message string) string {
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(message))
		return hex.EncodeToString(mac.Sum(nil))
	}
	token = "p1." + sign("presence:v1\x00"+clientIP+"\x00"+userAgent)
	digest := sha256.Sum256([]byte(token))
	fingerprint = hex.EncodeToString(digest[:])
	source = sign("presence-source:v1\x00" + clientIP)
	return token, fingerprint, source
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
