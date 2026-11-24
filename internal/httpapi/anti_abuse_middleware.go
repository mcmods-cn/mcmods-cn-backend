package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"

	"mcmods-cn-backend/internal/antiabuse"
	"mcmods-cn-backend/internal/security"
)

type antiAbuseContextKey string

const (
	antiAbuseCrawlerContextKey    antiAbuseContextKey = "anti-abuse-crawler"
	antiAbuseModerationContextKey antiAbuseContextKey = "anti-abuse-moderation"
	antiAbuseCommentContextKey    antiAbuseContextKey = "anti-abuse-comment"
)

type antiAbuseResponseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *antiAbuseResponseRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *antiAbuseResponseRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(body)
}

func (s *Server) serveProtectedMutation(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	claims := currentClaims(r)
	if claims.Subject > 0 && claimsExplicitlyAllow(claims, "account.banned") && !bannedMutationAllowed(r) {
		writeAPIError(w, http.StatusForbidden, "account_banned", "当前账户处于封禁状态，只能执行登录、退出和必要的账户安全操作", 0, nil)
		return
	}
	if s.antiAbuse == nil || !s.antiAbuse.Enabled() {
		next.ServeHTTP(w, r)
		return
	}
	var comment *commentSubmission
	var body []byte
	var content, submissionKey string
	if r.Pattern == createCommentRoute {
		comment = &commentSubmission{}
		comment.err = decodeJSON(r, &comment.request)
		r = r.WithContext(context.WithValue(r.Context(), antiAbuseCommentContextKey, comment))
		if comment.err == nil {
			content = truncateRunes(comment.request.Body, 20000)
			if !comment.request.watchOnly() {
				submissionKey = comment.request.key(r.Header.Get("Idempotency-Key"))
			}
		}
		if submissionKey != "" && len(submissionKey) <= 128 && s.db != nil {
			err := s.db.QueryRow(r.Context(), `select exists(select 1 from comments where author_id=$1 and idempotency_key=$2)`,
				claims.Subject, submissionKey).Scan(&comment.replayOnly)
			if err == nil && comment.replayOnly {
				// The handler still checks current target access, scope and the row under its lock.
				next.ServeHTTP(w, r)
				return
			}
		}
	}
	action := antiAbuseAction(r, comment)
	if action == "" {
		next.ServeHTTP(w, r)
		return
	}
	if comment == nil {
		body, content = inspectAntiAbuseBody(r)
		if body != nil {
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
	}
	contentClass := antiabuse.ContentClassBody
	if creatorTeamRoleMetadataRequest(r, body) {
		contentClass = antiabuse.ContentClassCreatorTeamRoleMetadata
	}
	input := antiabuse.Evaluation{
		UserID: claims.Subject, SessionID: claims.SessionID, IP: s.requestClientLocation(r).IP,
		DeviceID: r.Header.Get("X-Client-ID"), UserAgent: r.UserAgent(), RequestID: r.Header.Get("X-Request-ID"),
		Action: action, RateScope: antiAbuseRateScope(r), RateLimitPercent: antiAbuseRateLimitPercent(claims, action),
		ObjectType: antiAbuseObjectType(r), ObjectKey: antiAbuseObjectKey(r), Content: content, ContentClass: contentClass,
		FormToken: r.Header.Get("X-Anti-Abuse-Form"), Honeypot: r.Header.Get("X-Anti-Abuse-Trap"),
		ChallengeProof: r.Header.Get("X-Anti-Abuse-Challenge"), IdempotencyKey: submissionKey, Administrator: claimsAllow(claims, "admin.*"),
		CrawlerClass: crawlerClassFromRequest(r), Now: time.Now(),
	}
	decision, err := s.antiAbuse.Evaluate(r.Context(), input)
	if err != nil {
		// Low-risk service errors fail soft so Redis, DNS, or security-log
		// outages do not turn every authenticated write into a site-wide outage.
		s.writeAppLog(context.Background(), "system", "warn", "anti_abuse_degraded", action, claims.Subject, r, http.StatusServiceUnavailable, 0, map[string]any{"error": err.Error()})
		next.ServeHTTP(w, r)
		return
	}
	if antiAbuseDecisionBlocks(decision.Outcome) {
		status := http.StatusForbidden
		if decision.Outcome == antiabuse.Delay {
			status = http.StatusTooManyRequests
		}
		if decision.Outcome == antiabuse.Challenge {
			status = http.StatusPreconditionRequired
		}
		retry := antiAbuseRetryAfterSeconds(decision.RetryAfter)
		if persistErr := s.antiAbuse.RecordDecision(r.Context(), input, decision, input.CrawlerClass); persistErr != nil {
			s.writeAppLog(context.Background(), "system", "error", "anti_abuse_state_persist_failed", action, claims.Subject, r,
				http.StatusServiceUnavailable, 0, map[string]any{"error": persistErr.Error()})
			writeAPIError(w, http.StatusServiceUnavailable, "anti_abuse_state_unavailable",
				"安全状态暂时无法保存，请稍后重试", 1, nil)
			return
		}
		writeAPIError(w, status, decision.Code, decision.Message, retry, map[string]any{"challenge": decision.Challenge})
		return
	}
	if decision.Moderation {
		r = r.WithContext(context.WithValue(r.Context(), antiAbuseModerationContextKey, true))
	}
	recorder := &antiAbuseResponseRecorder{ResponseWriter: w}
	next.ServeHTTP(recorder, r)
	if recorder.status >= 200 && recorder.status < 400 {
		s.antiAbuse.EnqueueSuccess(input, decision)
	}
}

func bannedMutationAllowed(r *http.Request) bool {
	path := strings.TrimSuffix(r.URL.Path, "/")
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return true
	}
	for _, allowed := range []string{
		"/api/v1/auth/login", "/api/v1/auth/logout", "/api/v1/auth/password/forgot",
		"/api/v1/auth/password/reset", "/api/v1/auth/security", "/api/v1/realtime",
	} {
		if strings.HasPrefix(path, allowed) {
			return true
		}
	}
	return false
}

func antiAbuseDecisionBlocks(outcome antiabuse.Outcome) bool {
	switch outcome {
	case antiabuse.Challenge, antiabuse.Delay, antiabuse.TempBlock, antiabuse.Deny, antiabuse.AccountReview:
		return true
	default:
		return false
	}
}

// Retry-After is an integer number of seconds, but the limiter's remaining
// window can be fractional. Rounding down invites a retry before admission is
// possible; subtracting before division also avoids duration-addition overflow.
func antiAbuseRetryAfterSeconds(remaining time.Duration) int {
	if remaining <= 0 {
		return 0
	}
	return int((remaining-1)/time.Second) + 1
}

func antiAbuseModerationRequired(r *http.Request) bool {
	value, _ := r.Context().Value(antiAbuseModerationContextKey).(bool)
	return value
}

func antiAbuseAction(r *http.Request, comment *commentSubmission) string {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return ""
	}
	requestPath := r.URL.Path
	for _, excluded := range []string{"/api/v1/anti-abuse/", "/api/v1/admin/", "/api/v1/users/me/drafts", "/content-metrics/", "/presence", "/review-locks/", "/auth/logout"} {
		if strings.Contains(requestPath, excluded) {
			return ""
		}
	}
	switch {
	case strings.Contains(requestPath, "/comment-targets/") && strings.HasSuffix(requestPath, "/comments"):
		if comment != nil && comment.err == nil && strings.TrimSpace(comment.request.ParentID) != "" {
			return "comment.reply"
		}
		return "comment.create"
	case strings.Contains(requestPath, "/comments/") && r.Method == http.MethodPatch:
		return "comment.edit"
	case strings.HasPrefix(requestPath, "/api/v1/messages/") && r.Method == http.MethodPost:
		return "message.send"
	case strings.Contains(requestPath, "/ratings/") || strings.Contains(requestPath, "/reaction"):
		return "reaction.write"
	case strings.Contains(requestPath, "/favorites"):
		return "favorite.write"
	case strings.HasSuffix(requestPath, "/follow"):
		return "follow.write"
	case strings.Contains(requestPath, "/reports"):
		return "report.create"
	case strings.Contains(requestPath, "/uploads") || strings.Contains(requestPath, "/files") || strings.Contains(requestPath, "/oss/"):
		return "upload.create"
	case strings.HasPrefix(requestPath, "/api/v1/community/posts"):
		return "community.submit"
	case strings.HasPrefix(requestPath, "/api/v1/mods") || strings.HasPrefix(requestPath, "/api/v1/modpacks") ||
		strings.HasPrefix(requestPath, "/api/v1/content-projects") || strings.HasPrefix(requestPath, "/api/v1/servers") ||
		strings.HasPrefix(requestPath, "/api/v1/blueprints") || strings.HasPrefix(requestPath, "/api/v1/skins") ||
		strings.HasPrefix(requestPath, "/api/v1/creators") || strings.HasPrefix(requestPath, "/api/v1/catalog-editor") ||
		strings.HasPrefix(requestPath, "/api/v1/mod-content") || strings.Contains(requestPath, "/change-requests") || strings.Contains(requestPath, "/submit"):
		return "review.submit"
	default:
		return "write.generic"
	}
}

func antiAbuseRateScope(r *http.Request) string {
	requestPath := strings.TrimSuffix(r.URL.Path, "/")
	if strings.HasPrefix(requestPath, "/api/v1/mods/") && strings.Contains(requestPath, "/content-versions") {
		switch r.Method {
		case http.MethodPost:
			return "mod_content_version_create"
		case http.MethodPut:
			return "mod_content_version_update"
		case http.MethodDelete:
			return "mod_content_version_delete"
		}
	}
	return ""
}

func antiAbuseRateLimitPercent(claims security.Claims, action string) int {
	global := claimsNumericPermissionValue(claims, "security.anti-abuse.rate_multiplier")
	scoped := claimsNumericPermissionValue(claims, "security.anti-abuse.rate_multiplier."+antiAbusePermissionAction(action))
	value := max(global, scoped)
	if value <= 0 {
		return antiabuse.DefaultRateLimitPercent
	}
	return antiabuse.NormalizeRateLimitPercent(int(value))
}

func antiAbusePermissionAction(action string) string {
	return strings.Map(func(character rune) rune {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_' || character == '-' {
			return character
		}
		return '_'
	}, strings.ToLower(strings.TrimSpace(action)))
}

func inspectAntiAbuseBody(r *http.Request) ([]byte, string) {
	if r.Body == nil || !strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "json") {
		return nil, ""
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxJSONRequestBodyBytes+1))
	if err != nil {
		return body, ""
	}
	var value any
	if json.Unmarshal(body, &value) != nil {
		return body, ""
	}
	parts := make([]string, 0, 8)
	if reference := creatorMutationAvatarReference(r, body); reference != "" {
		collectCreatorMutationContent(value, reference, &parts)
	} else {
		collectContentStrings(value, "", &parts)
	}
	return body, strings.Join(parts, "\n")
}

// A role title labels an existing relationship; restoring the same author and
// role after removal does not publish the title as new text. Recognize only
// this server route and its current DTO. The normal Content, action, risk,
// quota, challenge, authentication and handler validation paths stay intact.
func creatorTeamRoleMetadataRequest(r *http.Request, body []byte) bool {
	const prefix = "/api/v1/creators/"
	if r.Method != http.MethodPut || !strings.HasPrefix(r.URL.Path, prefix) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, prefix), "/")
	if len(parts) != 2 || parts[1] != "members" || !validCatalogPublicID(strings.ToLower(parts[0])) {
		return false
	}
	// Keep an explicit wire shape: a future narrative field must not silently
	// inherit the relationship-metadata classification.
	var input struct {
		Members []struct {
			CreatorID string `json:"creatorId"`
			RoleID    string `json:"roleId"`
			Title     string `json:"title"`
		} `json:"members"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || input.Members == nil || decoder.Decode(new(any)) != io.EOF {
		return false
	}
	members := make([]creatorMemberPayload, len(input.Members))
	for index, member := range input.Members {
		members[index] = creatorMemberPayload{CreatorID: member.CreatorID, RoleID: member.RoleID, Title: member.Title}
	}
	_, err := normalizeCreatorTeamMembers(members)
	return err == nil
}

// Only this concrete DTO's top-level public avatar identity distinguishes a
// real creator image edit from duplicate text. Unknown/nested fields and other
// endpoints cannot use the same spelling as an anti-abuse nonce.
func creatorMutationAvatarReference(r *http.Request, body []byte) string {
	creatorWrite := r.Method == http.MethodPost && r.URL.Path == "/api/v1/creators"
	if r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/v1/creators/") {
		publicID := strings.TrimPrefix(r.URL.Path, "/api/v1/creators/")
		creatorWrite = validCatalogPublicID(strings.ToLower(publicID))
	}
	if !creatorWrite {
		return ""
	}
	var request struct {
		Kind         string  `json:"kind"`
		AvatarFileID *string `json:"avatarFileId"`
	}
	if json.Unmarshal(body, &request) != nil || request.AvatarFileID == nil {
		return ""
	}
	kind := strings.ToLower(strings.TrimSpace(request.Kind))
	reference := strings.ToLower(strings.TrimSpace(*request.AvatarFileID))
	if (kind != "author" && kind != "team") || !validCatalogPublicID(reference) {
		return ""
	}
	return reference
}
func collectCreatorMutationContent(value any, reference string, result *[]string) {
	object, ok := value.(map[string]any)
	if !ok {
		collectContentStrings(value, "", result)
		return
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	included := false
	for _, key := range keys {
		if strings.EqualFold(key, "avatarFileId") {
			if !included && len(*result) < 16 {
				*result = append(*result, "creator-avatar:"+reference)
				included = true
			}
			continue
		}
		collectContentStrings(object[key], strings.ToLower(key), result)
	}
}

func collectContentStrings(value any, key string, result *[]string) {
	if len(*result) >= 16 {
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		// JSON object order carries no meaning. A stable traversal also keeps
		// both fingerprinting and the bounded field selection independent of
		// Go's randomized map iteration. Arrays retain their original order.
		keys := make([]string, 0, len(typed))
		for childKey := range typed {
			keys = append(keys, childKey)
		}
		slices.Sort(keys)
		for _, childKey := range keys {
			collectContentStrings(typed[childKey], strings.ToLower(childKey), result)
		}
	case []any:
		for _, child := range typed {
			collectContentStrings(child, key, result)
		}
	case string:
		for _, allowed := range []string{"body", "bodymarkdown", "content", "message", "title", "summary", "description", "detail", "reason", "name"} {
			if key == allowed && typed != "" {
				*result = append(*result, truncateRunes(typed, 20000))
				return
			}
		}
	}
}

func antiAbuseObjectType(r *http.Request) string {
	segments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(segments) >= 3 {
		return segments[2]
	}
	return "request"
}

func antiAbuseObjectKey(r *http.Request) string {
	return antiabuse.ObjectKey(path.Clean(r.URL.Path))
}

func (s *Server) botTraffic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.antiAbuse == nil || !s.antiAbuse.Enabled() {
			next.ServeHTTP(w, r)
			return
		}
		clientIP := s.requestClientLocation(r).IP
		class, classifyErr := s.antiAbuse.ClassifyCrawler(r.Context(), clientIP, r.UserAgent(), r.Header.Get("X-MCMods-Bot-Token"))
		if classifyErr != nil {
			s.writeAppLog(context.Background(), "system", "warn", "anti_abuse_bot_rules_degraded", r.URL.Path, 0, r,
				http.StatusServiceUnavailable, 0, map[string]any{"error": classifyErr.Error(), "fallbackClass": class})
		}
		r = r.WithContext(context.WithValue(r.Context(), antiAbuseCrawlerContextKey, class))
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			allowed, retry := s.antiAbuse.ReadLimit(r.Context(), class, clientIP, r.URL.Path)
			if recordErr := s.antiAbuse.RecordCrawler(r.Context(), class, clientIP, r.UserAgent(), r.URL.Path, allowed); recordErr != nil {
				s.writeAppLog(context.Background(), "system", "warn", "anti_abuse_crawler_event_failed", r.URL.Path, 0, r,
					http.StatusServiceUnavailable, 0, map[string]any{"error": recordErr.Error(), "crawlerClass": class})
			}
			if !allowed {
				seconds := max(1, antiAbuseRetryAfterSeconds(retry))
				writeAPIError(w, http.StatusTooManyRequests, "crawler_rate_limited", "读取请求过于频繁，请稍后重试", seconds, nil)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func crawlerClassFromRequest(r *http.Request) antiabuse.CrawlerClass {
	value, _ := r.Context().Value(antiAbuseCrawlerContextKey).(antiabuse.CrawlerClass)
	return value
}
