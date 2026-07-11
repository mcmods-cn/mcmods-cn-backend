package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/security"
)

type submitModRevisionRequest struct {
	Snapshot     createModRequest `json:"snapshot"`
	ChangeReason string           `json:"changeReason"`
}

type reviewModRevisionRequest struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

type modRevisionResponse struct {
	ID           int64            `json:"id"`
	ModID        int64            `json:"modId"`
	Version      int              `json:"version"`
	Status       string           `json:"status"`
	Snapshot     createModRequest `json:"snapshot"`
	ChangeReason string           `json:"changeReason"`
	SubmittedBy  *int64           `json:"submittedBy,omitempty"`
	ReviewedBy   *int64           `json:"reviewedBy,omitempty"`
	ReviewNote   string           `json:"reviewNote"`
	CreatedAt    time.Time        `json:"createdAt"`
	ReviewedAt   *time.Time       `json:"reviewedAt,omitempty"`
}

type modRevisionComparisonResponse struct {
	Before        modRevisionResponse `json:"before"`
	After         modRevisionResponse `json:"after"`
	ChangedFields []string            `json:"changedFields"`
}

func (s *Server) submitModRevision(w http.ResponseWriter, r *http.Request) {
	siteID := normalizeModSiteID(r.PathValue("siteId"))
	claims := currentClaims(r)
	identity, err := s.modIdentity(r.Context(), siteID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "模组不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取模组失败")
		return
	}
	if !canEditMod(claims, identity) {
		writeError(w, http.StatusForbidden, "没有编辑该模组的权限")
		return
	}

	var req submitModRevisionRequest
	if err = decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	if strings.TrimSpace(req.Snapshot.SiteID) == "" {
		req.Snapshot.SiteID = identity.SiteID
	}
	if err = normalizeAndValidateModRequest(&req.Snapshot); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err = ensureModSiteIDAvailable(r.Context(), s.db, req.Snapshot.SiteID, identity.ID); errors.Is(err, errModSiteIDTaken) {
		writeError(w, http.StatusConflict, "模组站内 ID 已被占用")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "检查模组站内 ID 失败")
		return
	}
	req.ChangeReason = strings.TrimSpace(req.ChangeReason)
	if len(req.ChangeReason) > 500 {
		writeError(w, http.StatusBadRequest, "修改说明不能超过 500 个字符")
		return
	}
	snapshot, err := json.Marshal(req.Snapshot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成审核快照失败")
		return
	}
	status := "pending"
	if canSkipProjectReview(claims, identity) {
		status = "approved"
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建模组修订事务失败")
		return
	}
	defer tx.Rollback(r.Context())
	var revisionID int64
	err = tx.QueryRow(
		r.Context(),
		`insert into mod_revisions (mod_id, version, status, snapshot, change_reason, submitted_by, reviewed_at)
		 values ($1, (select coalesce(max(version), 0) + 1 from mod_revisions where mod_id = $1), $2, $3, $4, $5,
		         case when $2 = 'approved' then now() else null end)
		 returning id`,
		identity.ID, status, snapshot, req.ChangeReason, claims.Subject,
	).Scan(&revisionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "提交模组修改失败")
		return
	}
	if status == "approved" {
		if err = applyModSnapshot(r.Context(), tx, identity.ID, revisionID, req.Snapshot); err != nil {
			if errors.Is(err, errModSiteIDTaken) {
				writeError(w, http.StatusConflict, "模组站内 ID 已被占用")
				return
			}
			writeError(w, http.StatusInternalServerError, "应用免审核模组修订失败")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "提交模组修订失败")
		return
	}
	revision, err := s.modRevisionByID(r.Context(), revisionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取审核版本失败")
		return
	}
	writeJSON(w, http.StatusCreated, revision)
}

func (s *Server) modRevisionHistory(w http.ResponseWriter, r *http.Request) {
	siteID := normalizeModSiteID(r.PathValue("siteId"))
	claims := currentClaims(r)
	identity, err := s.modIdentity(r.Context(), siteID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "模组不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取模组失败")
		return
	}
	canSeePending := canEditMod(claims, identity) || hasPermission(claims.Permissions, "project.review")
	rows, err := s.db.Query(
		r.Context(),
		`select id, mod_id, version, status, snapshot, change_reason, submitted_by, reviewed_by, review_note, created_at, reviewed_at
		 from mod_revisions where mod_id = $1 and ($2 or status = 'approved') order by version desc`,
		identity.ID, canSeePending,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取历史版本失败")
		return
	}
	defer rows.Close()
	items := make([]modRevisionResponse, 0)
	for rows.Next() {
		item, scanErr := scanModRevision(rows)
		if scanErr != nil {
			writeError(w, http.StatusInternalServerError, "解析历史版本失败")
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "读取历史版本失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) compareModRevisions(w http.ResponseWriter, r *http.Request) {
	siteID := normalizeModSiteID(r.PathValue("siteId"))
	claims := currentClaims(r)
	identity, err := s.modIdentity(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusNotFound, "模组不存在")
		return
	}
	beforeID, beforeErr := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	afterID, afterErr := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if beforeErr != nil || afterErr != nil {
		writeError(w, http.StatusBadRequest, "历史版本参数无效")
		return
	}
	before, err := s.modRevisionByID(r.Context(), beforeID)
	if err != nil || before.ModID != identity.ID {
		writeError(w, http.StatusNotFound, "基准版本不存在")
		return
	}
	after, err := s.modRevisionByID(r.Context(), afterID)
	if err != nil || after.ModID != identity.ID {
		writeError(w, http.StatusNotFound, "目标版本不存在")
		return
	}
	canSeePending := canEditMod(claims, identity) || hasPermission(claims.Permissions, "project.review")
	if !canSeePending && (before.Status != "approved" || after.Status != "approved") {
		writeError(w, http.StatusForbidden, "没有查看待审核版本的权限")
		return
	}
	writeJSON(w, http.StatusOK, modRevisionComparisonResponse{Before: before, After: after, ChangedFields: changedSnapshotFields(before.Snapshot, after.Snapshot)})
}

func (s *Server) reviewModRevision(w http.ResponseWriter, r *http.Request) {
	revisionID, err := strconv.ParseInt(r.PathValue("revisionId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "审核版本无效")
		return
	}
	var req reviewModRevisionRequest
	if err = decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	req.Status = strings.TrimSpace(req.Status)
	req.Note = strings.TrimSpace(req.Note)
	if req.Status != "approved" && req.Status != "rejected" {
		writeError(w, http.StatusBadRequest, "审核结果无效")
		return
	}
	revision, err := s.modRevisionByID(r.Context(), revisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "审核版本不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取审核版本失败")
		return
	}
	if revision.Status != "pending" {
		writeError(w, http.StatusConflict, "该版本已经审核")
		return
	}

	claims := currentClaims(r)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "开始审核失败")
		return
	}
	defer tx.Rollback(r.Context())
	if req.Status == "approved" {
		if err = applyModSnapshot(r.Context(), tx, revision.ModID, revision.ID, revision.Snapshot); err != nil {
			if errors.Is(err, errModSiteIDTaken) {
				writeError(w, http.StatusConflict, "模组站内 ID 已被占用")
				return
			}
			writeError(w, http.StatusInternalServerError, "应用模组版本失败")
			return
		}
	}
	if _, err = tx.Exec(
		r.Context(),
		`update mod_revisions set status = $2, reviewed_by = $3, review_note = $4, reviewed_at = now() where id = $1`,
		revisionID, req.Status, claims.Subject, req.Note,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "保存审核结果失败")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "提交审核结果失败")
		return
	}
	updated, _ := s.modRevisionByID(r.Context(), revisionID)
	writeJSON(w, http.StatusOK, updated)
}

type modIdentityRecord struct {
	ID       int64
	UniqueID string
	SiteID   string
	OwnerID  *int64
}

func (s *Server) modIdentity(ctx context.Context, siteID string) (modIdentityRecord, error) {
	var identity modIdentityRecord
	siteID = normalizeModSiteID(siteID)
	err := s.db.QueryRow(
		ctx,
		`select id, project_code, slug, created_by from mods where slug = $1`,
		siteID,
	).Scan(&identity.ID, &identity.UniqueID, &identity.SiteID, &identity.OwnerID)
	return identity, err
}

func canEditMod(claims security.Claims, identity modIdentityRecord) bool {
	if identity.OwnerID != nil && claims.Subject != 0 && *identity.OwnerID == claims.Subject {
		return true
	}
	return hasPermission(claims.Permissions, "project.edit") ||
		hasPermission(claims.Permissions, "project.editor."+identity.UniqueID) ||
		hasPermission(claims.Permissions, "project.owner."+identity.UniqueID)
}

func canSkipProjectReview(claims security.Claims, identity modIdentityRecord) bool {
	return hasPermission(claims.Permissions, "project.no-review."+identity.UniqueID)
}

func (s *Server) modRevisionByID(ctx context.Context, id int64) (modRevisionResponse, error) {
	return scanModRevision(s.db.QueryRow(ctx, `select id, mod_id, version, status, snapshot, change_reason, submitted_by, reviewed_by, review_note, created_at, reviewed_at from mod_revisions where id = $1`, id))
}

func scanModRevision(row scanner) (modRevisionResponse, error) {
	var result modRevisionResponse
	var snapshot []byte
	err := row.Scan(&result.ID, &result.ModID, &result.Version, &result.Status, &snapshot, &result.ChangeReason, &result.SubmittedBy, &result.ReviewedBy, &result.ReviewNote, &result.CreatedAt, &result.ReviewedAt)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(snapshot, &result.Snapshot)
	return result, err
}

func applyModSnapshot(ctx context.Context, tx pgx.Tx, modID int64, revisionID int64, snapshot createModRequest) error {
	if snapshot.SiteID == "" {
		if err := tx.QueryRow(ctx, `select slug from mods where id = $1`, modID).Scan(&snapshot.SiteID); err != nil {
			return err
		}
	}
	if err := ensureModSiteIDAvailable(ctx, tx, snapshot.SiteID, modID); err != nil {
		return err
	}
	if len(snapshot.Compatibilities) == 0 && len(snapshot.SupportedLoaders) > 0 {
		for _, loader := range snapshot.SupportedLoaders {
			snapshot.Compatibilities = append(snapshot.Compatibilities, modLoaderCompatibilityPayload{Loader: loader, Versions: append([]string(nil), snapshot.SupportedVersions...)})
		}
	}
	snapshot.SupportedLoaders, snapshot.SupportedVersions = compatibilitySummary(snapshot.Compatibilities)
	_, err := tx.Exec(
		ctx,
		`update mods set primary_name=$2, secondary_name=$3, abbreviation=$4, summary=$5, mod_id=$6, environment=$7,
		 primary_category=$8, official_status=$9, source_status=$10, license=$11, curseforge_project_id=$12,
		 modrinth_project_id=$13, icon_url=$14, body_markdown=$15, search_keywords=$16, submission_method=$17,
		 review_status='approved', current_revision_id=$18, supported_versions=$19, supported_loaders=$20,
		 slug=$21, published_at=coalesce(published_at, now()), updated_at=now()
		 where id=$1`,
		modID, snapshot.PrimaryName, snapshot.SecondaryName, snapshot.Abbreviation, snapshot.Summary, snapshot.ModID,
		snapshot.Environment, snapshot.PrimaryCategory, snapshot.OfficialStatus, snapshot.SourceStatus, snapshot.License,
		snapshot.CurseForgeProjectID, snapshot.ModrinthProjectID, snapshot.IconURL, snapshot.BodyMarkdown,
		snapshot.SearchKeywords, snapshot.SubmissionMethod, revisionID, snapshot.SupportedVersions, snapshot.SupportedLoaders, snapshot.SiteID,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return errModSiteIDTaken
		}
		return err
	}
	for _, statement := range []string{
		`delete from mod_links where mod_id=$1`,
		`delete from mod_tags where mod_id=$1`,
		`delete from mod_authors where mod_id=$1`,
		`delete from mod_loader_compatibilities where mod_id=$1`,
		`delete from mod_relationship_groups where mod_id=$1`,
	} {
		if _, err = tx.Exec(ctx, statement, modID); err != nil {
			return err
		}
	}
	for index, link := range snapshot.Links {
		if _, err = tx.Exec(ctx, `insert into mod_links (mod_id, link_type, url, display_order) values ($1,$2,$3,$4)`, modID, link.Type, link.URL, index); err != nil {
			return err
		}
	}
	for _, tag := range snapshot.Tags {
		if _, err = tx.Exec(ctx, `insert into mod_tags (mod_id, tag) values ($1,$2)`, modID, tag); err != nil {
			return err
		}
	}
	for index, author := range snapshot.Authors {
		if _, err = tx.Exec(ctx, `insert into mod_authors (mod_id, name, role, display_order) values ($1,$2,$3,$4)`, modID, author.Name, author.Role, index); err != nil {
			return err
		}
	}
	if err = insertModCompatibilities(ctx, tx, modID, snapshot.Compatibilities); err != nil {
		return err
	}
	return insertModRelationshipGroups(ctx, tx, modID, snapshot.RelationshipGroups)
}

func changedSnapshotFields(before createModRequest, after createModRequest) []string {
	beforeValue := reflect.ValueOf(before)
	afterValue := reflect.ValueOf(after)
	typeInfo := beforeValue.Type()
	changed := make([]string, 0)
	for index := 0; index < beforeValue.NumField(); index++ {
		if reflect.DeepEqual(beforeValue.Field(index).Interface(), afterValue.Field(index).Interface()) {
			continue
		}
		name := strings.Split(typeInfo.Field(index).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			changed = append(changed, name)
		}
	}
	sort.Strings(changed)
	return changed
}
