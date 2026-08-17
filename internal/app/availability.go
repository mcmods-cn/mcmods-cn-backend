package app

import (
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"

	"mcmods-cn-backend/internal/config"
)

const (
	availabilityCORSHeaders  = "Authorization, Content-Type, Idempotency-Key, X-Request-ID"
	availabilityErrorMessage = "backend service is temporarily unavailable"
)

type runtimeHealthIssue struct {
	Component string    `json:"component"`
	Code      string    `json:"code"`
	Message   string    `json:"message"`
	Since     time.Time `json:"since"`
}

type runtimeHealthSnapshot struct {
	Ready   bool                 `json:"ready"`
	Status  string               `json:"status"`
	App     string               `json:"app"`
	Env     string               `json:"env"`
	Issues  []runtimeHealthIssue `json:"issues"`
	Checked time.Time            `json:"checkedAt"`
}

type availabilityHandler struct {
	cfg config.Config

	mu      sync.RWMutex
	handler http.Handler
	issues  map[string]runtimeHealthIssue
}

func newAvailabilityHandler(cfg config.Config) *availabilityHandler {
	handler := &availabilityHandler{cfg: cfg, issues: make(map[string]runtimeHealthIssue)}
	handler.reportIssue("startup", "backend_initializing", "backend dependencies are initializing")
	return handler
}

func (h *availabilityHandler) setHandler(handler http.Handler) {
	h.mu.Lock()
	h.handler = handler
	h.mu.Unlock()
}

func (h *availabilityHandler) reportIssue(component, code, message string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if current, ok := h.issues[component]; ok && current.Code == code && current.Message == message {
		return
	}
	h.issues[component] = runtimeHealthIssue{
		Component: component,
		Code:      code,
		Message:   message,
		Since:     time.Now().UTC(),
	}
}

func (h *availabilityHandler) resolveIssue(component string) {
	h.mu.Lock()
	delete(h.issues, component)
	h.mu.Unlock()
}

func (h *availabilityHandler) snapshot() (runtimeHealthSnapshot, http.Handler) {
	h.mu.RLock()
	issues := make([]runtimeHealthIssue, 0, len(h.issues))
	for _, issue := range h.issues {
		issues = append(issues, issue)
	}
	handler := h.handler
	h.mu.RUnlock()
	sort.Slice(issues, func(left, right int) bool { return issues[left].Component < issues[right].Component })
	ready := handler != nil && len(issues) == 0
	status := "ok"
	if !ready {
		status = "error"
	}
	return runtimeHealthSnapshot{
		Ready: ready, Status: status, App: "mcmods-cn-backend", Env: h.cfg.Env,
		Issues: issues, Checked: time.Now().UTC(),
	}, handler
}

func (h *availabilityHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	snapshot, handler := h.snapshot()
	if r.Method == http.MethodGet && r.URL.Path == "/live" {
		if handler != nil {
			handler.ServeHTTP(w, r)
			return
		}
		h.setAvailabilityResponseHeaders(w, r)
		writeAvailabilityJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"status": "alive", "app": "mcmods-cn-backend"}})
		return
	}
	if r.URL.Path == "/health" || r.URL.Path == "/ready" || !snapshot.Ready {
		h.setAvailabilityResponseHeaders(w, r)
	}
	if r.Method == http.MethodOptions && (r.URL.Path == "/health" || r.URL.Path == "/ready" || !snapshot.Ready) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/health" {
		w.Header().Set("Deprecation", "true")
		w.Header().Set("Link", "</ready>; rel=successor-version")
		if snapshot.Ready && handler != nil {
			handler.ServeHTTP(w, r)
			return
		}
	}
	if r.Method == http.MethodGet && r.URL.Path == "/ready" && !snapshot.Ready {
		writeAvailabilityJSON(w, http.StatusServiceUnavailable, map[string]any{"error": availabilityErrorMessage, "data": snapshot})
		return
	}
	if !snapshot.Ready || handler == nil {
		w.Header().Set("Retry-After", "5")
		writeAvailabilityJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": availabilityErrorMessage,
			"data":  snapshot,
		})
		return
	}
	handler.ServeHTTP(w, r)
}

func (h *availabilityHandler) setAvailabilityResponseHeaders(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Origin")
	if origin := r.Header.Get("Origin"); origin != "" && origin == h.cfg.FrontendOrigin {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
	w.Header().Set("Access-Control-Allow-Headers", availabilityCORSHeaders)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
}

func writeAvailabilityJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
