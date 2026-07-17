package httpapi

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"mcmods-cn-backend/internal/activity"
)

const requestActivityContextKey contextKey = "request_activity"

type requestActivity struct {
	mu                 sync.Mutex
	userID             int64
	actionID           int16
	objectTypeID       int16
	objectPublicID     string
	markdownAddedBytes int
	metadata           map[string]any
	skip               bool
}

func markActivityUser(r *http.Request, userID int64) {
	annotation, _ := r.Context().Value(requestActivityContextKey).(*requestActivity)
	if annotation == nil {
		return
	}
	annotation.mu.Lock()
	annotation.userID = userID
	annotation.mu.Unlock()
}

func annotateActivity(r *http.Request, actionID, objectTypeID int16, publicID string, markdownAddedBytes int, metadata map[string]any) {
	annotation, _ := r.Context().Value(requestActivityContextKey).(*requestActivity)
	if annotation == nil {
		return
	}
	annotation.mu.Lock()
	defer annotation.mu.Unlock()
	if actionID > 0 {
		annotation.actionID = actionID
	}
	if objectTypeID > 0 {
		annotation.objectTypeID = objectTypeID
	}
	if publicID != "" {
		annotation.objectPublicID = publicID
	}
	if markdownAddedBytes > 0 {
		annotation.markdownAddedBytes = markdownAddedBytes
	}
	if len(metadata) > 0 {
		if annotation.metadata == nil {
			annotation.metadata = make(map[string]any, len(metadata))
		}
		for key, value := range metadata {
			annotation.metadata[key] = value
		}
	}
}

func skipRequestActivity(r *http.Request) {
	annotation, _ := r.Context().Value(requestActivityContextKey).(*requestActivity)
	if annotation == nil {
		return
	}
	annotation.mu.Lock()
	annotation.skip = true
	annotation.mu.Unlock()
}

func (s *Server) recordRequestActivity(r *http.Request, annotation *requestActivity) {
	if s.activity == nil || annotation == nil {
		return
	}
	annotation.mu.Lock()
	event := activity.Event{
		UserID:             annotation.userID,
		ActionID:           annotation.actionID,
		ObjectTypeID:       annotation.objectTypeID,
		ObjectPublicID:     annotation.objectPublicID,
		MarkdownAddedBytes: annotation.markdownAddedBytes,
		Metadata:           annotation.metadata,
		OccurredAt:         time.Now().UTC(),
	}
	skip := annotation.skip
	annotation.mu.Unlock()
	if skip || event.UserID <= 0 {
		return
	}
	if event.ActionID == 0 {
		event.ActionID = inferredActivityAction(r)
	}
	if event.ObjectTypeID == 0 {
		event.ObjectTypeID = inferredActivityObject(r.URL.Path)
	}
	if event.ObjectPublicID == "" {
		event.ObjectPublicID = inferredPublicID(r.URL.Path)
	}
	if event.ActionID == 0 || event.ObjectTypeID == 0 {
		return
	}
	if event.Metadata == nil {
		event.Metadata = map[string]any{}
	}
	event.Metadata["method"] = r.Method
	event.Metadata["path"] = r.URL.Path
	s.activity.Record(event)
}

func inferredActivityAction(r *http.Request) int16 {
	path := strings.ToLower(r.URL.Path)
	switch {
	case strings.Contains(path, "/download"):
		return activity.ActionDownload
	case strings.Contains(path, "/uploads"):
		return activity.ActionUpload
	case strings.Contains(path, "/claim"):
		return activity.ActionClaim
	case strings.Contains(path, "/transfer"):
		return activity.ActionTransfer
	case strings.Contains(path, "/checkin"):
		return activity.ActionCheckIn
	case strings.Contains(path, "/purchase"):
		return activity.ActionPurchase
	case strings.Contains(path, "/use"):
		return activity.ActionUse
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		return activity.ActionView
	case http.MethodPost:
		return activity.ActionCreate
	case http.MethodPut, http.MethodPatch:
		return activity.ActionEdit
	case http.MethodDelete:
		return activity.ActionDelete
	default:
		return 0
	}
}

func inferredActivityObject(path string) int16 {
	path = strings.ToLower(path)
	switch {
	case strings.Contains(path, "/comments"):
		return activity.ObjectComment
	case strings.Contains(path, "/recipes"), strings.Contains(path, "/recipe-types"):
		return activity.ObjectRecipe
	case strings.Contains(path, "/mod-tags"):
		return activity.ObjectTag
	case strings.Contains(path, "/blueprints"):
		return activity.ObjectBlueprint
	case strings.Contains(path, "/authors"):
		return activity.ObjectAuthor
	case strings.Contains(path, "/teams"):
		return activity.ObjectTeam
	case strings.Contains(path, "/creators"):
		return activity.ObjectAuthor
	case strings.Contains(path, "/mods"), strings.Contains(path, "/mod-imports"):
		return activity.ObjectMod
	case strings.Contains(path, "/shop"):
		return activity.ObjectShopItem
	case strings.Contains(path, "/tasks"), strings.Contains(path, "/levels"):
		return activity.ObjectTask
	case strings.Contains(path, "/economy"), strings.Contains(path, "/currencies"):
		return activity.ObjectEconomy
	case strings.Contains(path, "/files"), strings.Contains(path, "/oss"):
		return activity.ObjectFile
	case strings.Contains(path, "/users"), strings.Contains(path, "/profile"):
		return activity.ObjectUser
	default:
		return 0
	}
}

func inferredPublicID(path string) string {
	for _, segment := range strings.Split(path, "/") {
		if len(segment) != 9 {
			continue
		}
		valid := true
		for _, character := range segment {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
				valid = false
				break
			}
		}
		if valid {
			return segment
		}
	}
	return ""
}
