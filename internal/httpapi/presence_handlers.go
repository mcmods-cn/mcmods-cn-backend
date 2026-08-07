package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"
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
	}
	digest := sha256.Sum256([]byte(visitorID))
	s.cache.TouchPresence(r.Context(), hex.EncodeToString(digest[:]), time.Now().UTC())
	writeJSON(w, http.StatusOK, map[string]bool{"online": true})
}
