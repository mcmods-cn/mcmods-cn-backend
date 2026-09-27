package httpapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"mcmods-cn-backend/internal/security"
)

const (
	maxRealtimeConnections           = 2048
	maxRealtimeConnectionsPerUser    = 4
	maxRealtimeConnectionsPerSession = 2
	maxRealtimeConnectionLifetime    = 30 * time.Minute
	realtimeLeaseTTL                 = 90 * time.Second
)

type realtimeEvent struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Data   any    `json:"data,omitempty"`
	Origin string `json:"origin,omitempty"`
}

type realtimeHub struct {
	mu       sync.RWMutex
	users    map[int64]map[chan realtimeEvent]struct{}
	rejected atomic.Uint64
	dropped  atomic.Uint64
}

func newRealtimeHub() *realtimeHub {
	return &realtimeHub{users: make(map[int64]map[chan realtimeEvent]struct{})}
}

func (hub *realtimeHub) subscribe(userID int64) (<-chan realtimeEvent, func()) {
	channel := make(chan realtimeEvent, 32)
	hub.mu.Lock()
	if hub.users[userID] == nil {
		hub.users[userID] = make(map[chan realtimeEvent]struct{})
	}
	hub.users[userID][channel] = struct{}{}
	hub.mu.Unlock()
	return channel, func() {
		hub.mu.Lock()
		delete(hub.users[userID], channel)
		if len(hub.users[userID]) == 0 {
			delete(hub.users, userID)
		}
		hub.mu.Unlock()
	}
}

func (hub *realtimeHub) publish(userID int64, event realtimeEvent) {
	if hub == nil || userID <= 0 {
		return
	}
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	for channel := range hub.users[userID] {
		select {
		case channel <- event:
		default:
			hub.dropped.Add(1)
		}
	}
}

func (hub *realtimeHub) deliveryStats() (rejected, dropped uint64) {
	if hub == nil {
		return 0, 0
	}
	return hub.rejected.Load(), hub.dropped.Load()
}

func (hub *realtimeHub) connectionCount() (int, int) {
	if hub == nil {
		return 0, 0
	}
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	connections := 0
	for _, channels := range hub.users {
		connections += len(channels)
	}
	return connections, len(hub.users)
}

func (s *Server) realtimeEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusNotImplemented, "streaming is unavailable")
		return
	}
	claims := currentClaims(r)
	sessionID := "user-" + strconv.FormatInt(claims.Subject, 10)
	if claims.SessionID != "" {
		sessionID = hex.EncodeToString(security.SessionFingerprint(claims.SessionID))
	}
	lease, allowed := s.cache.AcquireRealtimeLease(r.Context(), claims.Subject, sessionID,
		maxRealtimeConnections, maxRealtimeConnectionsPerUser, maxRealtimeConnectionsPerSession, realtimeLeaseTTL)
	if !allowed {
		s.realtime.rejected.Add(1)
		writeAPIError(w, http.StatusTooManyRequests, "REALTIME_CONNECTION_LIMIT", "realtime connection limit reached", 30, nil)
		return
	}
	defer lease.Release(r.Context())
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	channel, unsubscribe := s.realtime.subscribe(claims.Subject)
	defer unsubscribe()
	if _, err := fmt.Fprint(w, ": connected\n\n"); err != nil {
		return
	}
	flusher.Flush()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	lifetime := time.NewTimer(maxRealtimeConnectionLifetime)
	defer lifetime.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-lifetime.C:
			return
		case <-heartbeat.C:
			if !lease.Refresh(r.Context()) {
				return
			}
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case event := <-channel:
			raw, err := json.Marshal(event.Data)
			if err != nil {
				s.realtime.dropped.Add(1)
				continue
			}
			if _, err = fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", event.ID, event.Type, raw); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) publishRealtimeUser(userID int64, eventType string, data any) {
	event := realtimeEvent{ID: randomHex(12), Type: eventType, Data: data, Origin: s.realtimeOriginID()}
	s.realtime.publish(userID, event)
	if s.queue != nil {
		_ = s.queue.PublishBroadcast(context.Background(), "user."+strconv.FormatInt(userID, 10), event)
	}
}

func (s *Server) realtimeOriginID() string {
	s.realtimeOriginOnce.Do(func() {
		if s.realtimeOrigin == "" {
			s.realtimeOrigin = randomHex(16)
		}
	})
	return s.realtimeOrigin
}

func (s *Server) subscribeRealtimeBroadcast() {
	if s.queue == nil {
		return
	}
	// Register the definition even while realtime or NATS is disabled. The
	// queue client retains it and stages the subscription on a later successful
	// runtime reconfiguration.
	_ = s.queue.SubscribeBroadcast("user.*", func(_ context.Context, subject string, raw []byte) {
		parts := strings.Split(subject, ".")
		if len(parts) == 0 {
			return
		}
		userID, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
		if err != nil {
			return
		}
		var event realtimeEvent
		if json.Unmarshal(raw, &event) == nil {
			if event.Origin != "" && event.Origin == s.realtimeOriginID() {
				return
			}
			s.realtime.publish(userID, event)
		}
	})
}
