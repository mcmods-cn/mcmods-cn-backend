package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"mcmods-cn-backend/internal/antiabuse"
	"mcmods-cn-backend/internal/security"
)

type antiAbuseContextKey string

const (
	antiAbuseCrawlerContextKey    antiAbuseContextKey = "anti-abuse-crawler"
	antiAbuseModerationContextKey antiAbuseContextKey = "anti-abuse-moderation"
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
	action := antiAbuseAction(r)
	if action == "" || s.antiAbuse == nil || !s.antiAbuse.Enabled() {
		next.ServeHTTP(w, r)
		return
	}
	body, content := inspectAntiAbuseBody(r)
	if body != nil {
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	input := antiabuse.Evaluation{
		UserID: claims.Subject, SessionID: claims.SessionID, IP: s.requestClientLocation(r).IP,
		DeviceID: r.Header.Get("X-Client-ID"), UserAgent: r.UserAgent(), RequestID: r.Header.Get("X-Request-ID"),
		Action: action, RateScope: antiAbuseRateScope(r), RateLimitPercent: antiAbuseRateLimitPercent(claims, action),
		ObjectType: antiAbuseObjectType(r), ObjectKey: antiAbuseObjectKey(r), Content: content,
		FormToken: r.Header.Get("X-Anti-Abuse-Form"), Honeypot: r.Header.Get("X-Anti-Abuse-Trap"),
		ChallengeProof: r.Header.Get("X-Anti-Abuse-Challenge"), IdempotencyKey: r.Header.Get("Idempotency-Key"), Administrator: claimsAllow(claims, "admin.*"),
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
		retry := max(0, int(decision.RetryAfter.Round(time.Second).Seconds()))
		s.antiAbuse.RecordDecision(context.Background(), input, decision, input.CrawlerClass)
		writeAPIError(w, status, decision.Code, decision.Message, retry, map[string]any{"challenge": decision.Challenge})
		return
	}
	if decision.Moderation {
		r = r.WithContext(context.WithValue(r.Context(), antiAbuseModerationContextKey, true))
	}
	recorder := &antiAbuseResponseRecorder{ResponseWriter: w}
	next.ServeHTTP(recorder, r)
	if recorder.status >= 200 && recorder.status < 400 {
		go s.antiAbuse.RecordSuccess(context.Background(), input, decision)
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

func antiAbuseModerationRequired(r *http.Request) bool {
	value, _ := r.Context().Value(antiAbuseModerationContextKey).(bool)
	return value
}

func antiAbuseAction(r *http.Request) string {
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
		if stringBodyField(r, "parentId") != "" {
			return "comment.reply"
		}
		return "comment.create"
	case strings.Contains(requestPath, "/comments/") && strings.HasSuffix(requestPath, "/reports"):
		return "report.create"
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
	collectContentStrings(value, "", &parts)
	return body, strings.Join(parts, "\n")
}

func collectContentStrings(value any, key string, result *[]string) {
	if len(*result) >= 16 {
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		for childKey, child := range typed {
			collectContentStrings(child, strings.ToLower(childKey), result)
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

func stringBodyField(r *http.Request, field string) string {
	if r.Body == nil {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxJSONRequestBodyBytes+1))
	if err != nil {
		return ""
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	var values map[string]any
	if json.Unmarshal(body, &values) != nil {
		return ""
	}
	value, _ := values[field].(string)
	return strings.TrimSpace(value)
}

func antiAbuseObjectType(r *http.Request) string {
	segments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(segments) >= 3 {
		return segments[2]
	}
	return "request"
}

func antiAbuseObjectKey(r *http.Request) string {
	cleaned := path.Clean(r.URL.Path)
	if len(cleaned) > 160 {
		cleaned = cleaned[:160]
	}
	return cleaned
}

func (s *Server) botTraffic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.antiAbuse == nil || !s.antiAbuse.Enabled() {
			next.ServeHTTP(w, r)
			return
		}
		clientIP := s.requestClientLocation(r).IP
		class := s.antiAbuse.ClassifyCrawler(r.Context(), clientIP, r.UserAgent(), r.Header.Get("X-MCMods-Bot-Token"))
		r = r.WithContext(context.WithValue(r.Context(), antiAbuseCrawlerContextKey, class))
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			allowed, retry := s.antiAbuse.ReadLimit(r.Context(), class, clientIP, r.URL.Path)
			s.antiAbuse.RecordCrawler(context.Background(), class, clientIP, r.UserAgent(), r.URL.Path, allowed)
			if !allowed {
				seconds := max(1, int(retry.Round(time.Second).Seconds()))
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
