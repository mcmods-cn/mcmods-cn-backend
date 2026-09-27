package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *Server) modExportTags(w http.ResponseWriter, r *http.Request) {
	revisionID := r.PathValue("revisionId")
	if !s.canReadModExportRevision(w, r, revisionID) {
		return
	}
	registry := strings.TrimSpace(r.URL.Query().Get("registry"))
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(registry) > 160 || len(query) > 128 {
		writeError(w, http.StatusBadRequest, "tag filters are too long")
		return
	}
	limit := boundedLimit(r.URL.Query().Get("limit"), 80, 200)
	scope := "tags\x00" + registry + "\x00" + strings.ToLower(query)
	cursor, err := decodeModExportPageCursor(r.URL.Query().Get("cursor"), revisionID, scope)
	if err != nil || cursor.Ordinal != 0 {
		writeError(w, http.StatusBadRequest, "invalid tag cursor")
		return
	}
	rows, hasMore, err := loadModExportTagPage(r.Context(), s.db, revisionID, registry, query, cursor.First, cursor.Second, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read tags")
		return
	}
	items := make([]map[string]any, 0, limit)
	for _, row := range rows {
		parts := strings.SplitN(row.ID, ":", 2)
		namespace, objectPath := "", row.ID
		if len(parts) == 2 {
			namespace, objectPath = parts[0], parts[1]
		}
		items = append(items, map[string]any{
			"publicId": row.PublicID, "id": row.ID, "registry": row.Registry, "namespace": namespace, "path": objectPath,
			"translationKey": "", "iconPath": "", "previewPath": "", "names": map[string]string{},
			"data": map[string]any{"memberCount": row.MemberCount},
		})
	}
	nextCursor := ""
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		nextCursor = encodeModExportPageCursor(modExportPageCursor{RevisionID: revisionID, Scope: scope, First: last.Registry, Second: last.ID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": limit, "hasMore": hasMore, "nextCursor": nextCursor})
}

func (s *Server) modExportTagDetail(w http.ResponseWriter, r *http.Request) {
	revisionID := r.PathValue("revisionId")
	if !s.canReadModExportRevision(w, r, revisionID) {
		return
	}
	registry := strings.TrimSpace(r.URL.Query().Get("registry"))
	tagID := strings.TrimSpace(r.URL.Query().Get("tagId"))
	publicIDQuery := strings.TrimSpace(r.URL.Query().Get("publicId"))
	if registry == "" || (tagID == "" && publicIDQuery == "") || len(registry) > 160 || len(tagID) > 512 {
		writeError(w, http.StatusBadRequest, "registry and tagId are required")
		return
	}
	var memberCount int
	var publicID string
	var tagInternalID int64
	if err := s.db.QueryRow(r.Context(), `select tag.entity_id,entity.public_id,tag.canonical_id,snapshot.member_count
		from tag_import_snapshots snapshot join catalog_tags tag on tag.entity_id=snapshot.tag_id
		join catalog_entities entity on entity.id=tag.entity_id
		where snapshot.revision_id=$1 and tag.registry=$2
		and (($3<>'' and entity.public_id=$3) or ($3='' and tag.canonical_id=$4))`,
		revisionID, registry, publicIDQuery, tagID).Scan(&tagInternalID, &publicID, &tagID, &memberCount); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "tag not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to read tag")
		}
		return
	}
	locale := normalizeExportContentLocale(r.URL.Query().Get("locale"))
	limit := boundedLimit(r.URL.Query().Get("limit"), 80, 200)
	scope := "tag-members\x00" + strconv.FormatInt(tagInternalID, 10)
	cursor, err := decodeModExportPageCursor(r.URL.Query().Get("cursor"), revisionID, scope)
	if err != nil || cursor.Second != "" || cursor.Ordinal != 0 {
		writeError(w, http.StatusBadRequest, "invalid tag member cursor")
		return
	}
	rows, hasMore, err := loadModExportTagMemberPage(r.Context(), s.db, revisionID, tagInternalID, cursor.First, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read tag members")
		return
	}
	members := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		members = append(members, map[string]any{"publicId": row.PublicID, "id": row.ID,
			"registry": row.Registry, "translationKey": row.TranslationKey, "names": row.Names, "iconPath": row.IconPath})
	}
	if err = s.decorateExportTranslationNames(r.Context(), revisionID, locale, members); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve tag member translations")
		return
	}
	nextCursor := ""
	if hasMore && len(rows) > 0 {
		nextCursor = encodeModExportPageCursor(modExportPageCursor{RevisionID: revisionID, Scope: scope, First: rows[len(rows)-1].ID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"publicId": publicID, "id": tagID,
		"registry": registry, "memberCount": memberCount, "members": members, "limit": limit, "hasMore": hasMore, "nextCursor": nextCursor})
}
