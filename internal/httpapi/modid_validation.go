package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"mcmods-cn-backend/internal/activity"
)

type modIDCandidate struct {
	ID    string  `json:"id"`
	Count int     `json:"count"`
	Ratio float64 `json:"ratio"`
}

type modIDAnalysis struct {
	Configured []string         `json:"configuredModids"`
	Detected   []modIDCandidate `json:"detectedModids"`
	Primary    string           `json:"primaryDetectedModid"`
	Required   bool             `json:"confirmationRequired"`
}

var excludedImportNamespaces = map[string]struct{}{
	"minecraft": {}, "forge": {}, "neoforge": {}, "fabric": {}, "fabric-api": {}, "c": {},
}

func analyzeImportMODIDs(configured []string, counts map[string]int) modIDAnalysis {
	normalizedConfigured := normalizeImportMODIDs(configured)
	normalizedCounts := make(map[string]int, len(counts))
	total := 0
	for raw, count := range counts {
		id := normalizeImportMODID(raw)
		if id == "" || count <= 0 {
			continue
		}
		if _, excluded := excludedImportNamespaces[id]; excluded {
			continue
		}
		normalizedCounts[id] += count
		total += count
	}
	candidates := make([]modIDCandidate, 0, len(normalizedCounts))
	for id, count := range normalizedCounts {
		candidates = append(candidates, modIDCandidate{ID: id, Count: count, Ratio: float64(count) / float64(max(1, total))})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Count == candidates[j].Count {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].Count > candidates[j].Count
	})
	primary := ""
	if len(candidates) > 0 {
		primary = candidates[0].ID
	}
	configuredSet := make(map[string]struct{}, len(normalizedConfigured))
	for _, id := range normalizedConfigured {
		configuredSet[id] = struct{}{}
	}
	required := false
	if primary != "" {
		_, primaryMatches := configuredSet[primary]
		required = len(normalizedConfigured) == 0 || !primaryMatches
		majorCandidates := 0
		for _, candidate := range candidates {
			if candidate.Ratio >= 0.20 {
				majorCandidates++
			}
		}
		required = required || majorCandidates > 1
	}
	return modIDAnalysis{Configured: normalizedConfigured, Detected: candidates, Primary: primary, Required: required}
}

func normalizeImportMODIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = normalizeImportMODID(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func normalizeImportMODID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if !embeddedIconResourceIDPattern.MatchString(value + ":placeholder") {
		return ""
	}
	return value
}

func importNamespaceCounts(values []string) map[string]int {
	result := make(map[string]int)
	for _, value := range values {
		if namespace := normalizeImportMODID(value); namespace != "" {
			result[namespace]++
		}
	}
	return result
}

func embeddedImportNamespaceCounts(entries []embeddedIconCatalogEntry) map[string]int {
	result := make(map[string]int)
	for _, entry := range entries {
		namespace, _ := resourceParts(entry.RegisterName)
		if namespace = normalizeImportMODID(namespace); namespace != "" {
			result[namespace]++
		}
	}
	return result
}

func (s *Server) pauseCatalogImportForMODIDConfirmation(ctx context.Context, jobID, runToken string, modID int64, sourceHash string, counts map[string]int) (bool, error) {
	rows, err := s.db.Query(ctx, `select identifier from mod_identifiers where mod_id=$1 order by is_primary desc,display_order,id`, modID)
	if err != nil {
		return false, err
	}
	configured := make([]string, 0)
	for rows.Next() {
		var value string
		if err = rows.Scan(&value); err != nil {
			rows.Close()
			return false, err
		}
		configured = append(configured, value)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return false, err
	}
	analysis := analyzeImportMODIDs(configured, counts)
	encoded, _ := json.Marshal(struct {
		SourceHash string        `json:"sourceHash"`
		Analysis   modIDAnalysis `json:"analysis"`
	}{SourceHash: sourceHash, Analysis: analysis})
	digest := sha256.Sum256(encoded)
	analysisHash := hex.EncodeToString(digest[:])

	var confirmedHash string
	var confirmed bool
	if err = s.db.QueryRow(ctx, `select modid_analysis_hash,modid_confirmed_at is not null
		from catalog_import_jobs where id=$1 and run_token=$2 and mod_id=$3`, jobID, runToken, modID).Scan(&confirmedHash, &confirmed); err != nil {
		return false, err
	}
	detectedJSON, _ := json.Marshal(analysis.Detected)
	if !analysis.Required || (confirmed && confirmedHash == analysisHash) {
		_, err = s.db.Exec(ctx, `update catalog_import_jobs set detected_modids=$3::jsonb,configured_modids=$4,
			primary_detected_modid=$5,modid_analysis_hash=$6,modid_confirmation_required=false,updated_at=now()
			where id=$1 and run_token=$2 and mod_id=$7`, jobID, runToken, string(detectedJSON), analysis.Configured, analysis.Primary, analysisHash, modID)
		return false, err
	}
	tag, err := s.db.Exec(ctx, `update catalog_import_jobs set status='confirmation_required',progress=18,current_stage='modid_confirmation',
		detected_modids=$3::jsonb,configured_modids=$4,primary_detected_modid=$5,modid_analysis_hash=$6,
		modid_confirmation_required=true,modid_confirmed_at=null,modid_confirmed_by=null,run_token='',heartbeat_at=now(),updated_at=now()
		where id=$1 and run_token=$2 and mod_id=$7`, jobID, runToken, string(detectedJSON), analysis.Configured, analysis.Primary, analysisHash, modID)
	return tag.RowsAffected() == 1, err
}

type confirmMODIDMismatchRequest struct {
	Confirm      bool   `json:"confirmModidMismatch"`
	AnalysisHash string `json:"analysisHash"`
}

func (s *Server) confirmModExportMODIDMismatch(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.requireModEditor(w, r)
	if !ok {
		return
	}
	var request confirmMODIDMismatchRequest
	if decodeJSON(r, &request) != nil || !request.Confirm || len(request.AnalysisHash) != 64 {
		writeAPIError(w, http.StatusConflict, "MODID_CONFIRMATION_REQUIRED", "请确认识别到的 MODID 与当前模组配置不一致", 0, nil)
		return
	}
	jobID := r.PathValue("jobId")
	actorID := currentClaims(r).Subject
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法确认导入任务")
		return
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(r.Context(), `update catalog_import_jobs set status='queued',progress=0,current_stage='confirmed',
		modid_confirmed_at=now(),modid_confirmed_by=$4,updated_at=now()
		where id=$1 and mod_id=$2 and status='confirmation_required' and modid_confirmation_required
		 and modid_analysis_hash=$3 and created_by=$4
		 and updated_at>=now()-interval '24 hours'`, jobID, identity.ID, strings.ToLower(request.AnalysisHash), actorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法确认导入任务")
		return
	}
	if tag.RowsAffected() != 1 {
		writeAPIError(w, http.StatusConflict, "MODID_CONFIRMATION_EXPIRED", "导入分析已经变化或确认已被使用，请重新检查", 0, nil)
		return
	}
	if err = enqueueModExportAttemptTx(r.Context(), tx, jobID, "mod.catalog_import.confirmed"); err != nil {
		writeError(w, http.StatusInternalServerError, "无法重新加入导入队列")
		return
	}
	if _, err = tx.Exec(r.Context(), `insert into user_activity_events(user_id,action_id,object_type_id,object_route_id,occurred_at)
		select $1,$2,$3,route.id,now() from public_routes route where route.entity_type='mod' and route.internal_id=$4`, actorID, activity.ActionEdit, activity.ObjectMod, identity.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "无法记录 MODID 确认操作")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "无法提交 MODID 确认")
		return
	}
	s.writeAppLog(context.Background(), "user_interaction", "warn", "mod_import.modid_mismatch_confirmed", jobID, actorID, r, http.StatusAccepted, 0, map[string]any{"modId": identity.ID, "analysisHash": request.AnalysisHash})
	job, err := s.modExportJobByID(r.Context(), jobID, identity.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法读取已确认的导入任务")
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}
