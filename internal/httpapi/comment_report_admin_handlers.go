package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type adminCommentReport struct {
	ID             string     `json:"id"`
	CommentID      string     `json:"commentId"`
	CommentBody    string     `json:"commentBody"`
	CommentStatus  string     `json:"commentStatus"`
	CommentURL     string     `json:"commentUrl"`
	AuthorID       string     `json:"authorId"`
	AuthorName     string     `json:"authorName"`
	ReporterID     string     `json:"reporterId"`
	ReporterName   string     `json:"reporterName"`
	Reason         string     `json:"reason"`
	Detail         string     `json:"detail"`
	Status         string     `json:"status"`
	ResolutionNote string     `json:"resolutionNote"`
	CreatedAt      time.Time  `json:"createdAt"`
	ResolvedAt     *time.Time `json:"resolvedAt,omitempty"`
}

type reviewCommentReportRequest struct {
	Action string `json:"action"`
	Note   string `json:"note"`
}

func (s *Server) adminCommentReports(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "resolved" && status != "dismissed" {
		writeError(w, http.StatusBadRequest, "举报状态不正确")
		return
	}
	limit := boundedLimit(r.URL.Query().Get("limit"), 50, 100)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	rows, err := s.db.Query(r.Context(), `select report.public_id,comment.public_id,comment.body,comment.status,
		author.public_id,author.username,reporter.public_id,reporter.username,
		report.reason,report.detail,report.status,report.resolution_note,report.created_at,report.resolved_at
		from comment_reports report
		join comments comment on comment.id=report.comment_id
		join users author on author.id=comment.author_id
		join users reporter on reporter.id=report.reporter_id
		where report.status=$1 order by report.created_at,report.id limit $2 offset $3`, status, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取评论举报列表失败")
		return
	}
	defer rows.Close()
	items := make([]adminCommentReport, 0)
	for rows.Next() {
		var item adminCommentReport
		if err = rows.Scan(&item.ID, &item.CommentID, &item.CommentBody, &item.CommentStatus,
			&item.AuthorID, &item.AuthorName, &item.ReporterID, &item.ReporterName,
			&item.Reason, &item.Detail, &item.Status, &item.ResolutionNote, &item.CreatedAt, &item.ResolvedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "解析评论举报列表失败")
			return
		}
		item.CommentURL = "/comments/" + item.CommentID
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "读取评论举报列表失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": limit, "offset": offset})
}

func (s *Server) reviewCommentReport(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "举报编号不正确")
		return
	}
	var request reviewCommentReportRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	request.Action = strings.TrimSpace(request.Action)
	request.Note = strings.TrimSpace(request.Note)
	if request.Action != "hide" && request.Action != "dismiss" {
		writeError(w, http.StatusBadRequest, "举报处理操作不正确")
		return
	}
	if utf8.RuneCountInString(request.Note) > 2000 {
		writeError(w, http.StatusBadRequest, "处理说明不能超过 2000 个字符")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建举报审核事务失败")
		return
	}
	defer tx.Rollback(r.Context())
	var reportID, commentID int64
	var commentPublicID string
	err = tx.QueryRow(r.Context(), `select report.id,report.comment_id,comment.public_id
		from comment_reports report join comments comment on comment.id=report.comment_id
		where report.public_id=$1 and report.status='pending' for update`, publicID).
		Scan(&reportID, &commentID, &commentPublicID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "举报不存在或已经处理")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取评论举报失败")
		return
	}
	claims := currentClaims(r)
	resultStatus := "dismissed"
	notificationTitle := "评论举报未采纳"
	if request.Action == "hide" {
		resultStatus = "resolved"
		notificationTitle = "评论举报已处理"
		if _, err = tx.Exec(r.Context(), `update comments set status='hidden',updated_at=now()
			where id=$1 and status not in ('deleted','spam')`, commentID); err != nil {
			writeError(w, http.StatusInternalServerError, "隐藏违规评论失败")
			return
		}
	}
	rows, err := tx.Query(r.Context(), `update comment_reports set status=$2,reviewer_id=$3,resolution_note=$4,
		resolved_at=now(),updated_at=now()
		where comment_id=$1 and status='pending' and ($2='resolved' or id=$5)
		returning reporter_id`, commentID, resultStatus, claims.Subject, request.Note, reportID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存举报处理结果失败")
		return
	}
	reporterIDs := make([]int64, 0)
	for rows.Next() {
		var reporterID int64
		if err = rows.Scan(&reporterID); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "读取举报用户失败")
			return
		}
		reporterIDs = append(reporterIDs, reporterID)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		writeError(w, http.StatusInternalServerError, "读取举报用户失败")
		return
	}
	rows.Close()
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "提交举报处理结果失败")
		return
	}
	for _, reporterID := range reporterIDs {
		s.enqueueOrCreateDirectNotification(r.Context(), reporterID, claims.Subject, "review", notificationTitle,
			request.Note, map[string]any{"commentId": commentPublicID, "url": "/comments/" + commentPublicID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": resultStatus})
}
