package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const serverReviewCursorVersion = 1

type serverReviewPageRequest struct {
	Status string
	Limit  int
	Scope  string
	Cursor *serverReviewPageCursor
}

type serverReviewPageCursor struct {
	Version   int       `json:"v"`
	Scope     string    `json:"s"`
	CreatedAt time.Time `json:"createdAt"`
	ID        int64     `json:"id"`
}

func parseServerReviewPageRequest(values url.Values) (serverReviewPageRequest, error) {
	status := strings.ToLower(strings.TrimSpace(values.Get("status")))
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "approved" && status != "rejected" {
		return serverReviewPageRequest{}, errors.New("审核状态不正确")
	}
	limit := 50
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > 100 {
			return serverReviewPageRequest{}, errors.New("服务器审核分页大小不正确")
		}
		limit = parsed
	}
	scope := serverReviewPageScope(status, limit)
	cursor, err := decodeServerReviewPageCursor(values.Get("cursor"), scope)
	if err != nil {
		return serverReviewPageRequest{}, err
	}
	return serverReviewPageRequest{Status: status, Limit: limit, Scope: scope, Cursor: cursor}, nil
}

func serverReviewPageScope(status string, limit int) string {
	material, _ := json.Marshal(struct {
		Version int    `json:"version"`
		Status  string `json:"status"`
		Limit   int    `json:"limit"`
	}{serverReviewCursorVersion, status, limit})
	digest := sha256.Sum256(material)
	return hex.EncodeToString(digest[:16])
}

func encodeServerReviewPageCursor(cursor serverReviewPageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeServerReviewPageCursor(raw, scope string) (*serverReviewPageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 2048 {
		return nil, errors.New("服务器审核游标不正确")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("服务器审核游标不正确")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor serverReviewPageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("服务器审核游标不正确")
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) || cursor.Version != serverReviewCursorVersion ||
		cursor.Scope != scope || cursor.CreatedAt.IsZero() || cursor.ID <= 0 {
		return nil, errors.New("服务器审核游标不正确")
	}
	cursor.CreatedAt = cursor.CreatedAt.UTC()
	return &cursor, nil
}

const minecraftServerReviewPageQuery = `with review_page as materialized (
	select server.public_id,server.name,server.address,server.short_description,
		server.minecraft_versions,server.dedicated_client,server.languages,server.primary_tag,
		server.has_whitelist,server.online_mode,server.modded,server.loader,server.review_status,
		server.review_note,submitter.public_id as submitter_public_id,submitter.username as submitter_username,
		server.created_at,server.reviewed_at,server.id as internal_id
	from minecraft_servers server join users submitter on submitter.id=server.submitted_by
	where server.review_status=$1 %s
	order by server.created_at,server.id limit $%d
), proof_counts as (
	select proof.server_id,count(*) as proof_file_count
	from minecraft_server_proof_files proof
	join oss_files file on file.id=proof.oss_file_id
	where proof.server_id=any(array(select page.internal_id from review_page page))
		and ` + safeReviewAttachmentPredicate + ` group by proof.server_id
), link_counts as (
	select link.server_id,count(*) as link_count
	from minecraft_server_links link
	where link.server_id=any(array(select page.internal_id from review_page page))
	group by link.server_id
), mod_counts as (
	select server_mod.server_id,count(*) as mod_count
	from minecraft_server_mods server_mod
	where server_mod.server_id=any(array(select page.internal_id from review_page page))
	group by server_mod.server_id
)
select page.public_id,page.name,page.address,page.short_description,page.minecraft_versions,
	page.dedicated_client,page.languages,page.primary_tag,page.has_whitelist,page.online_mode,
	page.modded,page.loader,page.review_status,page.review_note,page.submitter_public_id,
	page.submitter_username,page.created_at,page.reviewed_at,page.internal_id,
	coalesce(proof_counts.proof_file_count,0),coalesce(link_counts.link_count,0),coalesce(mod_counts.mod_count,0)
from review_page page
left join proof_counts on proof_counts.server_id=page.internal_id
left join link_counts on link_counts.server_id=page.internal_id
left join mod_counts on mod_counts.server_id=page.internal_id
order by page.created_at,page.internal_id`

func serverReviewPageSQL(request serverReviewPageRequest) (string, []any) {
	arguments := []any{request.Status}
	cursorPredicate := ""
	if request.Cursor != nil {
		cursorPredicate = `and (server.created_at,server.id)>($2,$3)`
		arguments = append(arguments, request.Cursor.CreatedAt, request.Cursor.ID)
	}
	arguments = append(arguments, request.Limit+1)
	return fmt.Sprintf(minecraftServerReviewPageQuery, cursorPredicate, len(arguments)), arguments
}
