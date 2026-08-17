package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type realtimeEvent struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

type realtimeHub struct {
	mu    sync.RWMutex
	users map[int64]map[chan realtimeEvent]struct{}
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
		}
	}
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
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	channel, unsubscribe := s.realtime.subscribe(claims.Subject)
	defer unsubscribe()
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		case event := <-channel:
			raw, _ := json.Marshal(event.Data)
			fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", event.ID, event.Type, raw)
			flusher.Flush()
		}
	}
}

func (s *Server) publishRealtimeUser(userID int64, eventType string, data any) {
	event := realtimeEvent{ID: randomHex(12), Type: eventType, Data: data}
	s.realtime.publish(userID, event)
	if s.queue != nil {
		_ = s.queue.PublishBroadcast(context.Background(), "user."+strconv.FormatInt(userID, 10), event)
	}
}

func (s *Server) subscribeRealtimeBroadcast() {
	if s.queue == nil || !s.cfg.NATS.Realtime {
		return
	}
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
			s.realtime.publish(userID, event)
		}
	})
}
