package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"mcmods-cn-backend/internal/activity"
)

const requestActivityContextKey contextKey = "request_activity"

type requestActivity struct {
	mu                   sync.Mutex
	userID               int64
	actionID             int16
	objectTypeID         int16
	objectEntityType     string
	objectInternalID     int64
	objectPublicID       string
	markdownAddedBytes   int
	markdownDeletedBytes int
	skip                 bool
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

func annotateActivity(r *http.Request, actionID, objectTypeID int16, publicID string, markdownAddedBytes int) {
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
}

func annotateActivityID(r *http.Request, actionID, objectTypeID int16, entityType string, internalID int64, markdownAddedBytes int) {
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
	if internalID > 0 && entityType != "" {
		annotation.objectEntityType = entityType
		annotation.objectInternalID = internalID
	}
	if markdownAddedBytes > 0 {
		annotation.markdownAddedBytes = markdownAddedBytes
	}
}

func annotateActivityDelta(r *http.Request, actionID, objectTypeID int16, publicID string, markdownAddedBytes, markdownDeletedBytes int) {
	annotateActivity(r, actionID, objectTypeID, publicID, markdownAddedBytes)
	annotation, _ := r.Context().Value(requestActivityContextKey).(*requestActivity)
	if annotation == nil {
		return
	}
	annotation.mu.Lock()
	if markdownDeletedBytes > 0 {
		annotation.markdownDeletedBytes = markdownDeletedBytes
	}
	annotation.mu.Unlock()
}

func annotateActivityIDDelta(r *http.Request, actionID, objectTypeID int16, entityType string, internalID int64, markdownAddedBytes, markdownDeletedBytes int) {
	annotateActivityID(r, actionID, objectTypeID, entityType, internalID, markdownAddedBytes)
	annotation, _ := r.Context().Value(requestActivityContextKey).(*requestActivity)
	if annotation == nil {
		return
	}
	annotation.mu.Lock()
	if markdownDeletedBytes > 0 {
		annotation.markdownDeletedBytes = markdownDeletedBytes
	}
	annotation.mu.Unlock()
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
	if isHighFrequencyActivityExcluded(r) {
		return
	}
	annotation.mu.Lock()
	event := activity.Event{
		UserID:               annotation.userID,
		ActionID:             annotation.actionID,
		ObjectTypeID:         annotation.objectTypeID,
		ObjectEntityType:     annotation.objectEntityType,
		ObjectInternalID:     annotation.objectInternalID,
		ObjectPublicID:       annotation.objectPublicID,
		MarkdownAddedBytes:   annotation.markdownAddedBytes,
		MarkdownDeletedBytes: annotation.markdownDeletedBytes,
		OccurredAt:           time.Now().UTC(),
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
	if event.ActionID == activity.ActionView {
		identity := event.ObjectPublicID
		if identity == "" && event.ObjectInternalID > 0 {
			identity = event.ObjectEntityType + ":" + strconv.FormatInt(event.ObjectInternalID, 10)
		}
		key := strconv.FormatInt(event.UserID, 10) + ":" + strconv.FormatInt(int64(event.ObjectTypeID), 10) + ":" + identity
		if !s.cache.ClaimThrottle(r.Context(), "activity-view:"+key, 5*time.Minute) {
			return
		}
	}
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

func isHighFrequencyActivityExcluded(r *http.Request) bool {
	path := strings.ToLower(r.URL.Path)
	if strings.Contains(path, "/users/me/drafts") ||
		strings.Contains(path, "/users/me/markdown-playground") ||
		(strings.Contains(path, "/messages/conversations/") && strings.HasSuffix(path, "/presence")) {
		return true
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	for _, marker := range []string{
		"/mod-imports/", "/modpack-imports/", "/content-project-imports/",
		"/export-imports/", "/catalog-imports/", "/translations/",
	} {
		if strings.Contains(path, marker) {
			return true
		}
	}
	for _, suffix := range []string{"/icon", "/cover", "/render", "/content"} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

func inferredActivityObject(path string) int16 {
	path = strings.ToLower(path)
	if objectTypeID := inferredSimpleProjectActivityObject(path); objectTypeID > 0 {
		return objectTypeID
	}
	switch {
	case strings.Contains(path, "/review-locks"), strings.Contains(path, "/change-requests"),
		strings.Contains(path, "/content-revisions"), strings.Contains(path, "/server-reviews"),
		strings.Contains(path, "/mod-content-reviews"), strings.Contains(path, "/creator-claims"),
		strings.Contains(path, "/comment-reports"), strings.Contains(path, "/mod-applications"),
		strings.Contains(path, "/revisions"):
		return activity.ObjectReview
	case strings.Contains(path, "/comments"):
		return activity.ObjectComment
	case strings.Contains(path, "/recipes"), strings.Contains(path, "/recipe-types"):
		return activity.ObjectRecipe
	case strings.Contains(path, "/mod-tags"):
		return activity.ObjectTag
	case strings.Contains(path, "/catalog/resources"):
		return activity.ObjectResource
	case strings.Contains(path, "/blueprints"):
		return activity.ObjectBlueprint
	case strings.Contains(path, "/modpacks"), strings.Contains(path, "/modpack-imports"):
		return activity.ObjectModpack
	case strings.Contains(path, "/servers"):
		return activity.ObjectServer
	case strings.Contains(path, "/plugins"):
		return activity.ObjectPlugin
	case strings.Contains(path, "/maps"):
		return activity.ObjectMap
	case strings.Contains(path, "/resource-packs"):
		return activity.ObjectResourcePack
	case strings.Contains(path, "/shaders"):
		return activity.ObjectShaderPack
	case strings.Contains(path, "/datapacks"):
		return activity.ObjectDatapack
	case strings.Contains(path, "/addons"):
		return activity.ObjectAddon
	case strings.Contains(path, "/news"), strings.Contains(path, "/tutorials"),
		strings.Contains(path, "/issues"), strings.Contains(path, "/discussions"),
		strings.Contains(path, "/community-posts"), strings.Contains(path, "/community/posts"):
		return activity.ObjectCommunityPost
	case strings.Contains(path, "/changelogs"):
		return activity.ObjectChangelog
	case strings.Contains(path, "/ratings"):
		return activity.ObjectRating
	case strings.Contains(path, "/skins"):
		return activity.ObjectSkin
	case strings.Contains(path, "/player-profiles"):
		return activity.ObjectPlayerProfile
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

func inferredSimpleProjectActivityObject(path string) int16 {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	for index, segment := range segments {
		if segment != "content-projects" && segment != "content-project-imports" {
			continue
		}
		if index+1 >= len(segments) {
			return 0
		}
		return activityObjectTypeForEntityType(segments[index+1])
	}
	return 0
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

func activityObjectTypeForEntityType(entityType string) int16 {
	switch strings.ToLower(strings.TrimSpace(entityType)) {
	case "recipe", "recipe_type", "recipe_layout_template":
		return activity.ObjectRecipe
	case "mod":
		return activity.ObjectMod
	case "modpack":
		return activity.ObjectModpack
	case "blueprint", "blueprint_variant":
		return activity.ObjectBlueprint
	case "plugin":
		return activity.ObjectPlugin
	case "map":
		return activity.ObjectMap
	case "resource_pack":
		return activity.ObjectResourcePack
	case "shader_pack":
		return activity.ObjectShaderPack
	case "datapack":
		return activity.ObjectDatapack
	case "addon":
		return activity.ObjectAddon
	case "author", "creator":
		return activity.ObjectAuthor
	case "team":
		return activity.ObjectTeam
	case "user":
		return activity.ObjectUser
	case "comment":
		return activity.ObjectComment
	case "tag":
		return activity.ObjectTag
	case "resource", "document", "structure", "mod_content_version", "mod_content_template", "mod_content_section":
		return activity.ObjectResource
	case "community_post":
		return activity.ObjectCommunityPost
	case "minecraft_server", "server":
		return activity.ObjectServer
	case "oss_file", "project_file":
		return activity.ObjectFile
	case "skin", "skin_asset":
		return activity.ObjectSkin
	case "player_profile":
		return activity.ObjectPlayerProfile
	case "project_changelog", "changelog":
		return activity.ObjectChangelog
	case "rating":
		return activity.ObjectRating
	default:
		return 0
	}
}
