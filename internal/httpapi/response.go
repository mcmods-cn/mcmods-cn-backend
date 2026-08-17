package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

const maxJSONRequestBodyBytes = int64(8 << 20)

const backendResponseHeader = "X-MCMods-API-Response"

type apiResponse struct {
	Data       any    `json:"data,omitempty"`
	Error      string `json:"error,omitempty"`
	Code       string `json:"code,omitempty"`
	RetryAfter int    `json:"retryAfter,omitempty"`
	Details    any    `json:"details,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	markBackendResponse(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiResponse{Data: data})
}

func writeJSONBytes(w http.ResponseWriter, status int, payload []byte) {
	markBackendResponse(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(payload) // #nosec G705 -- callers pass json.Marshal output or bytes from the JSON-only query cache.
}

func writeError(w http.ResponseWriter, status int, message string) {
	markBackendResponse(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiResponse{Error: message})
}

func writeAPIError(w http.ResponseWriter, status int, code, message string, retryAfter int, details any) {
	markBackendResponse(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if retryAfter > 0 {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiResponse{Error: message, Code: code, RetryAfter: retryAfter, Details: details})
}

func markBackendResponse(w http.ResponseWriter) {
	w.Header().Set(backendResponseHeader, "1")
}

func decodeJSON(r *http.Request, target any) error {
	limited := &io.LimitedReader{R: r.Body, N: maxJSONRequestBodyBytes + 1}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if limited.N <= 0 {
		return fmt.Errorf("JSON request exceeds %d bytes", maxJSONRequestBodyBytes)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain one JSON value")
		}
		return err
	}
	if limited.N <= 0 {
		return fmt.Errorf("JSON request exceeds %d bytes", maxJSONRequestBodyBytes)
	}
	return nil
}
