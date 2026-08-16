package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"time"

	"mcmods-cn-backend/internal/antiabuse"
)

var antiAbuseFormActions = stringSet("comment.create", "comment.reply", "comment.edit", "message.send", "report.create", "upload.create", "review.submit", "community.submit")

func (s *Server) antiAbuseFormToken(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	objectKey := strings.TrimSpace(r.URL.Query().Get("object"))
	if !antiAbuseFormActions[action] || objectKey == "" || len(objectKey) > 160 || !strings.HasPrefix(objectKey, "/api/v1/") {
		writeError(w, http.StatusBadRequest, "invalid anti-abuse form scope")
		return
	}
	claims := currentClaims(r)
	token, err := s.antiAbuse.IssueFormToken(r.Context(), claims.Subject, claims.SessionID, s.requestClientLocation(r).IP, action, objectKey)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to issue form token")
		return
	}
	writeJSON(w, http.StatusOK, token)
}

func (s *Server) adminAntiAbuseConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		settings, err := s.antiAbuse.Settings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load anti-abuse configuration")
			return
		}
		writeJSON(w, http.StatusOK, settings)
		return
	}
	var settings antiabuse.Settings
	if decodeJSON(r, &settings) != nil {
		writeError(w, http.StatusBadRequest, "invalid anti-abuse configuration")
		return
	}
	if err := antiabuse.ValidateSettings(settings); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_anti_abuse_config", err.Error(), 0, nil)
		return
	}
	saved, err := s.antiAbuse.SaveSettings(r.Context(), settings, currentClaims(r).Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save anti-abuse configuration")
		return
	}
	s.writeAppLog(r.Context(), "admin_operation", "warn", "update_anti_abuse_config", "anti_abuse.config", currentClaims(r).Subject, r, http.StatusOK, 0, map[string]any{"emergencyMode": saved.EmergencyMode})
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) adminResetAntiAbuseConfig(w http.ResponseWriter, r *http.Request) {
	saved, err := s.antiAbuse.SaveSettings(r.Context(), antiabuse.DefaultSettings(), currentClaims(r).Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reset anti-abuse configuration")
		return
	}
	s.writeAppLog(r.Context(), "admin_operation", "warn", "reset_anti_abuse_config", "anti_abuse.config", currentClaims(r).Subject, r, http.StatusOK, 0, nil)
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) adminAntiAbuseOverview(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `select window_name,outcome,count(*) from (
		select '1h' window_name,outcome from anti_abuse_events where created_at>now()-interval '1 hour'
		union all select '24h',outcome from anti_abuse_events where created_at>now()-interval '24 hours'
		union all select '7d',outcome from anti_abuse_events where created_at>now()-interval '7 days') values group by window_name,outcome`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load anti-abuse overview")
		return
	}
	counts := map[string]map[string]int64{"1h": {}, "24h": {}, "7d": {}}
	for rows.Next() {
		var windowName, outcome string
		var count int64
		if rows.Scan(&windowName, &outcome, &count) == nil {
			counts[windowName][outcome] = count
		}
	}
	rows.Close()
	var restricted, highRisk int64
	_ = s.db.QueryRow(r.Context(), `select
		(select count(*) from anti_abuse_restrictions where lifted_at is null and starts_at<=now() and (ends_at is null or ends_at>now())),
		(select count(*) from anti_abuse_user_states where trust_level in ('high_risk','restricted'))`).Scan(&restricted, &highRisk)
	var challengePassed, challengeFailed, duplicateBlocked, verifiedCrawlerReads, unknownCrawlerReads int64
	_ = s.db.QueryRow(r.Context(), `select
		count(*) filter(where rule_codes@>array['challenge_passed']::text[]),
		count(*) filter(where rule_codes@>array['challenge_failed']::text[]),
		count(*) filter(where rule_codes&&array['exact_duplicate_user','concurrent_exact_duplicate']::text[]),
		count(*) filter(where crawler_class in ('verified_search_engine','allowed_bot','monitoring_bot')),
		count(*) filter(where crawler_class in ('unknown_crawler','suspicious_bot'))
		from anti_abuse_events where created_at>now()-interval '24 hours'`).Scan(&challengePassed, &challengeFailed, &duplicateBlocked, &verifiedCrawlerReads, &unknownCrawlerReads)
	trend := s.querySimpleRows(r, `select stat_date,action,outcome,crawler_class,event_count from anti_abuse_daily_stats where stat_date>=current_date-30 order by stat_date,action,outcome`)
	writeJSON(w, http.StatusOK, map[string]any{"counts": counts, "activeRestrictions": restricted, "highRiskUsers": highRisk, "trend": trend,
		"challengePassed24h": challengePassed, "challengeFailed24h": challengeFailed, "duplicateBlocked24h": duplicateBlocked,
		"verifiedCrawlerReads24h": verifiedCrawlerReads, "unknownCrawlerReads24h": unknownCrawlerReads})
}

func (s *Server) adminAntiAbuseEvents(w http.ResponseWriter, r *http.Request) {
	limit := boundedLimit(r.URL.Query().Get("limit"), 100, 500)
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	outcome := strings.TrimSpace(r.URL.Query().Get("outcome"))
	sensitive := r.URL.Query().Get("sensitive") == "true" && claimsAllow(currentClaims(r), "security.anti-abuse.sensitive")
	rows, err := s.db.Query(r.Context(), `select event.public_id,coalesce(account.public_id,''),coalesce(account.username,''),event.action,event.object_type,event.object_key,
		event.outcome,event.risk_score,event.rule_codes,event.ip_hash,event.subnet_hash,event.device_hash,event.crawler_class,event.content_hash,event.similarity,
		event.disposition,event.review_note,event.created_at
		from anti_abuse_events event left join users account on account.id=event.user_id
		where ($1='' or event.action=$1) and ($2='' or event.outcome=$2) order by event.created_at desc,event.id desc limit $3`, action, outcome, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load anti-abuse events")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, userID, username, actionValue, objectType, objectKey, outcomeValue, ipHash, subnetHash, deviceHash, crawlerClass, contentHash, disposition, reviewNote string
		var score, similarity int
		var rules []string
		var createdAt time.Time
		if rows.Scan(&id, &userID, &username, &actionValue, &objectType, &objectKey, &outcomeValue, &score, &rules, &ipHash, &subnetHash, &deviceHash, &crawlerClass, &contentHash, &similarity, &disposition, &reviewNote, &createdAt) != nil {
			continue
		}
		if !sensitive {
			ipHash, subnetHash, deviceHash = redactHash(ipHash), redactHash(subnetHash), redactHash(deviceHash)
		}
		items = append(items, map[string]any{"id": id, "userId": userID, "username": username, "action": actionValue, "objectType": objectType,
			"objectKey": objectKey, "outcome": outcomeValue, "riskScore": score, "rules": rules, "ipHash": ipHash, "subnetHash": subnetHash,
			"deviceHash": deviceHash, "crawlerClass": crawlerClass, "contentHash": redactHash(contentHash), "similarity": similarity,
			"disposition": disposition, "reviewNote": reviewNote, "createdAt": createdAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) adminReviewAntiAbuseEvent(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Disposition string `json:"disposition"`
		Note        string `json:"note"`
	}
	if decodeJSON(r, &request) != nil || !stringSet("acknowledged", "false_positive", "confirmed_malicious")[request.Disposition] || len(request.Note) > 2000 {
		writeError(w, http.StatusBadRequest, "invalid event disposition")
		return
	}
	tag, err := s.db.Exec(r.Context(), `update anti_abuse_events set disposition=$2,review_note=$3,reviewed_by=$4,reviewed_at=now() where public_id=$1`,
		strings.ToLower(r.PathValue("id")), request.Disposition, strings.TrimSpace(request.Note), currentClaims(r).Subject)
	if err != nil || tag.RowsAffected() != 1 {
		writeError(w, http.StatusNotFound, "anti-abuse event was not found")
		return
	}
	s.writeAppLog(r.Context(), "admin_operation", "warn", "review_anti_abuse_event", r.PathValue("id"), currentClaims(r).Subject, r, http.StatusOK, 0, request)
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

func (s *Server) adminUpdateAntiAbuseUserState(w http.ResponseWriter, r *http.Request) {
	var request struct {
		TrustLevel      string `json:"trustLevel"`
		RiskScore       int    `json:"riskScore"`
		ManuallyTrusted bool   `json:"manuallyTrusted"`
	}
	if decodeJSON(r, &request) != nil || !stringSet("new", "normal", "trusted", "high_risk", "restricted")[request.TrustLevel] || request.RiskScore < 0 || request.RiskScore > 1000 {
		writeError(w, http.StatusBadRequest, "invalid user risk state")
		return
	}
	var userID int64
	if s.db.QueryRow(r.Context(), `select id from users where public_id=$1`, strings.ToLower(r.PathValue("id"))).Scan(&userID) != nil {
		writeError(w, http.StatusNotFound, "user was not found")
		return
	}
	_, err := s.db.Exec(r.Context(), `insert into anti_abuse_user_states(user_id,trust_level,risk_score,manually_trusted,updated_at)
		values($1,$2,$3,$4,now()) on conflict(user_id) do update set trust_level=excluded.trust_level,risk_score=excluded.risk_score,
		manually_trusted=excluded.manually_trusted,updated_at=now()`, userID, request.TrustLevel, request.RiskScore, request.ManuallyTrusted)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update user risk state")
		return
	}
	s.cache.InvalidatePrefix(r.Context(), "anti-abuse:account:")
	s.writeAppLog(r.Context(), "admin_operation", "warn", "update_anti_abuse_user_state", r.PathValue("id"), currentClaims(r).Subject, r, http.StatusOK, 0, request)
	writeJSON(w, http.StatusOK, map[string]bool{"updated": true})
}

type antiAbuseRestrictionRequest struct {
	UserID          string   `json:"userId"`
	Actions         []string `json:"actions"`
	Mode            string   `json:"mode"`
	DurationMinutes int      `json:"durationMinutes"`
	Reason          string   `json:"reason"`
	AppealAllowed   bool     `json:"appealAllowed"`
}

func (s *Server) adminAntiAbuseRestrictions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{"items": s.querySimpleRows(r, `select restriction.public_id,account.public_id user_id,account.username,restriction.actions,restriction.mode,restriction.source,
			restriction.rule_code,restriction.risk_score,restriction.reason,restriction.automatic,restriction.appeal_allowed,restriction.starts_at,restriction.ends_at,
			restriction.lifted_at,restriction.lift_reason from anti_abuse_restrictions restriction left join users account on account.id=restriction.user_id
			order by restriction.created_at desc limit 500`)})
		return
	}
	var request antiAbuseRestrictionRequest
	if decodeJSON(r, &request) != nil || !antiAbuseRestrictionModeAllowed(request.Mode) || len(request.Actions) > 20 || len(strings.TrimSpace(request.Reason)) < 3 {
		writeError(w, http.StatusBadRequest, "invalid anti-abuse restriction")
		return
	}
	var userID int64
	if s.db.QueryRow(r.Context(), `select id from users where public_id=$1`, strings.ToLower(strings.TrimSpace(request.UserID))).Scan(&userID) != nil {
		writeError(w, http.StatusNotFound, "user was not found")
		return
	}
	for _, action := range request.Actions {
		if !antiAbuseActionAllowed(action) {
			writeError(w, http.StatusBadRequest, "invalid restriction action")
			return
		}
	}
	if len(request.Actions) == 0 {
		switch request.Mode {
		case "no_comment":
			request.Actions = []string{"comment.create", "comment.reply", "comment.edit"}
		case "no_review":
			request.Actions = []string{"review.submit", "community.submit"}
		case "no_upload":
			request.Actions = []string{"upload.create"}
		case "challenge", "moderation", "cooldown":
			writeError(w, http.StatusBadRequest, "at least one restriction action is required")
			return
		}
	}
	var endsAt any
	if request.Mode != "permanent_ban" {
		minutes := min(max(request.DurationMinutes, 1), 43200)
		endsAt = time.Now().Add(time.Duration(minutes) * time.Minute)
	}
	var publicID string
	err := s.db.QueryRow(r.Context(), `insert into anti_abuse_restrictions(user_id,actions,mode,source,reason,automatic,appeal_allowed,ends_at,created_by)
		values($1,$2,$3,'administrator',$4,false,$5,$6,$7) returning public_id`, userID, compactAntiAbuseActions(request.Actions), request.Mode,
		strings.TrimSpace(request.Reason), request.AppealAllowed, endsAt, currentClaims(r).Subject).Scan(&publicID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create restriction")
		return
	}
	s.cache.InvalidatePrefix(r.Context(), "anti-abuse:account:")
	s.writeAppLog(r.Context(), "admin_operation", "warn", "create_anti_abuse_restriction", publicID, currentClaims(r).Subject, r, http.StatusCreated, 0, request)
	writeJSON(w, http.StatusCreated, map[string]string{"id": publicID})
}

func (s *Server) adminLiftAntiAbuseRestriction(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Reason string `json:"reason"`
	}
	if decodeJSON(r, &request) != nil || len(strings.TrimSpace(request.Reason)) < 3 {
		writeError(w, http.StatusBadRequest, "a lift reason is required")
		return
	}
	tag, err := s.db.Exec(r.Context(), `update anti_abuse_restrictions set lifted_at=now(),lifted_by=$2,lift_reason=$3 where public_id=$1 and lifted_at is null`,
		strings.ToLower(r.PathValue("id")), currentClaims(r).Subject, strings.TrimSpace(request.Reason))
	if err != nil || tag.RowsAffected() != 1 {
		writeError(w, http.StatusNotFound, "active restriction was not found")
		return
	}
	s.cache.InvalidatePrefix(r.Context(), "anti-abuse:account:")
	s.writeAppLog(r.Context(), "admin_operation", "warn", "lift_anti_abuse_restriction", r.PathValue("id"), currentClaims(r).Subject, r, http.StatusOK, 0, request)
	writeJSON(w, http.StatusOK, map[string]bool{"lifted": true})
}

type antiAbuseBotRuleRequest struct {
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Matcher  string `json:"matcher"`
	Token    string `json:"token"`
	ReadOnly bool   `json:"readOnly"`
}

func (s *Server) adminAntiAbuseBotRules(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{"items": s.querySimpleRows(r, `select public_id,kind,label,matcher,read_only,enabled,expires_at,created_at,updated_at from anti_abuse_bot_rules order by updated_at desc`)})
		return
	}
	var request antiAbuseBotRuleRequest
	if decodeJSON(r, &request) != nil || !antiAbuseBotRuleKindAllowed(request.Kind) || len(strings.TrimSpace(request.Label)) < 2 || len(request.Matcher) > 255 {
		writeError(w, http.StatusBadRequest, "invalid bot rule")
		return
	}
	if (request.Kind == "allowed_bot" || request.Kind == "monitoring_bot") && len(request.Token) < 16 {
		writeError(w, http.StatusBadRequest, "allowed bot tokens must contain at least 16 characters")
		return
	}
	if (request.Kind == "ip_allow" || request.Kind == "ip_block") && !validIPMatcher(request.Matcher) {
		writeError(w, http.StatusBadRequest, "invalid bot IP matcher")
		return
	}
	var publicID string
	err := s.db.QueryRow(r.Context(), `insert into anti_abuse_bot_rules(kind,label,matcher,secret_hash,read_only,created_by)
		values($1,$2,$3,$4,true,$5) returning public_id`, request.Kind, strings.TrimSpace(request.Label), strings.TrimSpace(request.Matcher),
		hashSecret(request.Token), currentClaims(r).Subject).Scan(&publicID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create bot rule")
		return
	}
	s.cache.InvalidatePrefix(r.Context(), "anti-abuse:bot-rules")
	s.writeAppLog(r.Context(), "admin_operation", "warn", "create_anti_abuse_bot_rule", publicID, currentClaims(r).Subject, r, http.StatusCreated, 0, map[string]string{"kind": request.Kind, "label": request.Label})
	writeJSON(w, http.StatusCreated, map[string]string{"id": publicID})
}

func (s *Server) adminDeleteAntiAbuseBotRule(w http.ResponseWriter, r *http.Request) {
	tag, err := s.db.Exec(r.Context(), `delete from anti_abuse_bot_rules where public_id=$1`, strings.ToLower(r.PathValue("id")))
	if err != nil || tag.RowsAffected() != 1 {
		writeError(w, http.StatusNotFound, "bot rule was not found")
		return
	}
	s.cache.InvalidatePrefix(r.Context(), "anti-abuse:bot-rules")
	s.writeAppLog(r.Context(), "admin_operation", "warn", "delete_anti_abuse_bot_rule", r.PathValue("id"), currentClaims(r).Subject, r, http.StatusOK, 0, map[string]bool{"deleted": true})
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func antiAbuseRestrictionModeAllowed(value string) bool {
	return stringSet("cooldown", "challenge", "moderation", "no_comment", "no_review", "no_upload", "read_only", "temporary_ban", "permanent_ban")[value]
}
func compactAntiAbuseActions(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
func antiAbuseActionAllowed(value string) bool {
	return antiAbuseFormActions[value] || value == "reaction.write" || value == "favorite.write" || value == "follow.write" || value == "write.generic" || value == "*"
}
func antiAbuseBotRuleKindAllowed(value string) bool {
	return stringSet("allowed_bot", "monitoring_bot", "blocked_bot", "ip_allow", "ip_block")[value]
}
func validIPMatcher(value string) bool {
	if net.ParseIP(strings.TrimSpace(value)) != nil {
		return true
	}
	_, _, err := net.ParseCIDR(strings.TrimSpace(value))
	return err == nil
}
func hashSecret(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func redactHash(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12] + "…"
}
