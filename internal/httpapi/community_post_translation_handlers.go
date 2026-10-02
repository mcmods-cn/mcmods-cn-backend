package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

func (s *Server) requestCommunityPostTranslation(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	var request struct {
		TargetLocale string `json:"targetLocale"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid translation request")
		return
	}
	targetLocale := normalizeContentLocale(request.TargetLocale)
	var postID, revisionID int64
	var sourceLocale, title, body string
	err := s.db.QueryRow(r.Context(), `select id,published_revision_id,source_locale,title,body_markdown from community_posts
		where public_id=$1 and status='active' and review_status='approved'`, publicID).Scan(&postID, &revisionID, &sourceLocale, &title, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusBadRequest, "translation request is invalid")
		return
	}
	if err != nil {
		logCommunityPostDataFailure("translation_source", publicID, err)
		writeError(w, http.StatusInternalServerError, "failed to load translation source")
		return
	}
	if targetLocale == "" || targetLocale == sourceLocale {
		writeError(w, http.StatusBadRequest, "translation request is invalid")
		return
	}
	var cachedTitle, cachedBody string
	err = s.db.QueryRow(r.Context(), `select title,body_markdown from community_post_translations where post_id=$1 and locale=$2 and source_revision_id=$3`, postID, targetLocale, revisionID).Scan(&cachedTitle, &cachedBody)
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"cached": true, "translation": map[string]string{"title": cachedTitle, "bodyMarkdown": cachedBody}})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		logCommunityPostDataFailure("translation_cache", publicID, err)
		writeError(w, http.StatusInternalServerError, "failed to load cached translation")
		return
	}
	limit := int64(claimsNumericPermissionValue(claims, "user.ai.daily_token_limit"))
	if limit <= 0 {
		writeError(w, http.StatusForbidden, "no daily AI token allowance is available")
		return
	}
	task, err := s.enqueueCommunityPostTranslation(r.Context(), publicID, postID, revisionID, sourceLocale, targetLocale, title, body, claims.Subject, limit)
	if errors.Is(err, errAIQuotaExceeded) {
		writeError(w, http.StatusTooManyRequests, "daily AI token allowance is insufficient")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"taskId": task.TaskUID, "status": task.Status, "countsTowardDailyTokenQuota": true})
}

func (s *Server) enqueueCommunityPostTranslation(ctx context.Context, publicID string, postID, revisionID int64, sourceLocale, targetLocale, title, body string, actorID, tokenLimit int64) (enqueuedContentTranslation, error) {
	cfg := s.aiConfigFromSettings(ctx)
	binding, ok := findAITaskModel(cfg.TaskModels, aiTaskContentTranslation)
	if !ok {
		return enqueuedContentTranslation{}, errors.New("content translation is not configured")
	}
	provider, model, ok := resolveAIModel(cfg, binding.ModelKey)
	if !ok {
		return enqueuedContentTranslation{}, errors.New("content translation model is unavailable")
	}
	payload := map[string]any{"scope": "community_post", "postId": publicID, "postInternalId": postID,
		"sourceRevisionId": revisionID, "sourceLocale": sourceLocale, "targetLocale": targetLocale,
		"quotaBacked": true, "items": []map[string]string{{"key": "title", "text": title}, {"key": "bodyMarkdown", "text": body}}}
	raw, _ := json.Marshal(payload)
	reserved := int64(utf8.RuneCountInString(title+body)*2 + 256)
	concurrencyKey := "community-post:" + publicID + ":" + targetLocale + ":" + strconv.FormatInt(revisionID, 10) + ":actor:" + strconv.FormatInt(actorID, 10)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return enqueuedContentTranslation{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1))`, concurrencyKey); err != nil {
		return enqueuedContentTranslation{}, err
	}
	var existing enqueuedContentTranslation
	err = tx.QueryRow(ctx, `select task_uid,status from ai_tasks where task_type=$1 and concurrency_key=$2
		and status in ('queued','running','retrying') order by created_at desc limit 1`, aiTaskContentTranslation, concurrencyKey).
		Scan(&existing.TaskUID, &existing.Status)
	if err == nil {
		if err = tx.Commit(ctx); err != nil {
			return enqueuedContentTranslation{}, err
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return enqueuedContentTranslation{}, err
	}
	if err = reserveAITaskQuotaTx(ctx, tx, actorID, tokenLimit, reserved); err != nil {
		return enqueuedContentTranslation{}, err
	}
	taskUID := "ai_" + randomHex(16)
	var taskID int64
	err = tx.QueryRow(ctx, `insert into ai_tasks(task_uid,task_type,provider,model,status,priority,concurrency_key,payload,created_by,queued_at,quota_reserved_tokens)
		values($1,$2,$3,$4,'queued',0,$5,$6::jsonb,$7,now(),$8) returning id`, taskUID, aiTaskContentTranslation,
		provider.Code, model.Model, concurrencyKey, string(raw), actorID, reserved).Scan(&taskID)
	if err != nil {
		return enqueuedContentTranslation{}, err
	}
	if err = enqueueAITaskTx(ctx, tx, "ai.community_post_translation.requested", taskID, taskUID, aiTaskContentTranslation, ""); err != nil {
		return enqueuedContentTranslation{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return enqueuedContentTranslation{}, err
	}
	s.writeAITaskLog(ctx, taskID, "info", "task_queued", "Community post translation queued", payload)
	return enqueuedContentTranslation{TaskUID: taskUID, Status: "queued"}, nil
}

func (s *Server) communityPostTranslationResult(w http.ResponseWriter, r *http.Request) {
	taskUID := strings.ToLower(strings.TrimSpace(r.PathValue("taskId")))
	var status, errorText string
	var createdBy int64
	var payloadRaw []byte
	err := s.db.QueryRow(r.Context(), `select status,error,coalesce(created_by,0),payload from ai_tasks where task_uid=$1 and task_type=$2`, taskUID, aiTaskContentTranslation).
		Scan(&status, &errorText, &createdBy, &payloadRaw)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && createdBy != currentClaims(r).Subject) {
		writeError(w, http.StatusNotFound, "translation task not found")
		return
	}
	if err != nil {
		logCommunityPostDataFailure("translation_task", taskUID, err)
		writeError(w, http.StatusInternalServerError, "failed to load translation task")
		return
	}
	response := map[string]any{"taskId": taskUID, "status": status, "error": errorText}
	if status == "completed" {
		payload, payloadErr := decodeCommunityPostTranslationTaskPayload(payloadRaw)
		if payloadErr != nil {
			logCommunityPostDataFailure("translation_payload", taskUID, payloadErr)
			writeError(w, http.StatusInternalServerError, "translation task data is invalid")
			return
		}
		var title, body string
		err = s.db.QueryRow(r.Context(), `select title,body_markdown from community_post_translations where post_id=$1 and locale=$2 and source_revision_id=$3`, payload.PostInternalID, payload.TargetLocale, payload.SourceRevision).Scan(&title, &body)
		if errors.Is(err, pgx.ErrNoRows) {
			err = errors.New("completed community post translation result is missing")
		}
		if err != nil {
			logCommunityPostDataFailure("translation_result", taskUID, err)
			writeError(w, http.StatusInternalServerError, "translation result is unavailable")
			return
		}
		response["translation"] = map[string]string{"title": title, "bodyMarkdown": body}
	}
	writeJSON(w, http.StatusOK, response)
}

type communityPostTranslationTaskPayload struct {
	PostInternalID int64  `json:"postInternalId"`
	TargetLocale   string `json:"targetLocale"`
	SourceRevision int64  `json:"sourceRevisionId"`
}

func decodeCommunityPostTranslationTaskPayload(raw []byte) (communityPostTranslationTaskPayload, error) {
	var payload communityPostTranslationTaskPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return communityPostTranslationTaskPayload{}, fmt.Errorf("decode community post translation payload: %w", err)
	}
	payload.TargetLocale = normalizeContentLocale(payload.TargetLocale)
	if payload.PostInternalID <= 0 || payload.SourceRevision <= 0 || payload.TargetLocale == "" {
		return communityPostTranslationTaskPayload{}, errors.New("community post translation payload is invalid")
	}
	return payload, nil
}

func (worker *AIWorker) persistCommunityPostTranslation(ctx context.Context, taskID int64, rawPayload []byte, result map[string]any) error {
	payload, err := decodeCommunityPostTranslationTaskPayload(rawPayload)
	if err != nil {
		return err
	}
	translated, err := strictTranslationItemsToMap(result, stringSet("title", "bodyMarkdown"))
	if err != nil {
		return err
	}
	if strings.TrimSpace(translated["title"]) == "" || strings.TrimSpace(translated["bodyMarkdown"]) == "" {
		return errors.New("community post translation result is incomplete")
	}
	var currentRevision int64
	if err = worker.db.QueryRow(ctx, `select published_revision_id from community_posts where id=$1 and review_status='approved'`, payload.PostInternalID).Scan(&currentRevision); err != nil {
		return fmt.Errorf("load community post translation source revision: %w", err)
	}
	if currentRevision != payload.SourceRevision {
		return errors.New("community post changed while translation was running")
	}
	_, err = worker.db.Exec(ctx, `insert into community_post_translations(post_id,locale,title,body_markdown,source_revision_id,ai_task_id)
		values($1,$2,$3,$4,$5,$6) on conflict(post_id,locale) do update set title=excluded.title,
		body_markdown=excluded.body_markdown,source_revision_id=excluded.source_revision_id,ai_task_id=excluded.ai_task_id,updated_at=now()`,
		payload.PostInternalID, normalizeContentLocale(payload.TargetLocale), translated["title"], translated["bodyMarkdown"], payload.SourceRevision, taskID)
	return err
}
