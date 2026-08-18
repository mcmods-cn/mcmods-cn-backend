package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/gif"
	_ "image/png"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/config"
)

const (
	maxStickerBytes         = int64(4 << 20)
	maxStickerEdge          = 1024
	maxStickerPixels        = int64(4_194_304)
	maxStickerGIFFrames     = 120
	maxStickerGIFTotalPixel = int64(64_000_000)
)

func normalizedStickerLimits(value config.StickerConfig) config.StickerConfig {
	if value.MaxBytes <= 0 {
		value.MaxBytes = maxStickerBytes
	}
	if value.MaxEdge <= 0 {
		value.MaxEdge = maxStickerEdge
	}
	if value.MaxPixels <= 0 {
		value.MaxPixels = maxStickerPixels
	}
	if value.MaxGIFFrames <= 0 {
		value.MaxGIFFrames = maxStickerGIFFrames
	}
	if value.MaxGIFDecodedPixels <= 0 {
		value.MaxGIFDecodedPixels = maxStickerGIFTotalPixel
	}
	if value.MaxGIFDuration <= 0 {
		value.MaxGIFDuration = 30 * time.Second
	}
	return value
}

type stickerTranslationMap map[string]string

type stickerPackMutation struct {
	Code         string                `json:"code"`
	Status       string                `json:"status"`
	SortOrder    int                   `json:"sortOrder"`
	Translations stickerTranslationMap `json:"translations"`
}

type stickerMutation struct {
	Code         string                `json:"code"`
	ImageFileID  string                `json:"imageFileId"`
	Status       string                `json:"status"`
	SortOrder    int                   `json:"sortOrder"`
	Translations stickerTranslationMap `json:"translations"`
}

type stickerImageMeta struct {
	FileID      int64
	PublicID    string
	ObjectKey   string
	ContentType string
	Size        int64
	SHA256      string
	Width       int
	Height      int
}

func editableStickerLocales() []string {
	locales := make([]string, 0, len(supportedEditableContentLocales))
	for locale := range supportedEditableContentLocales {
		locales = append(locales, locale)
	}
	sort.Strings(locales)
	return locales
}

func normalizeStickerTranslations(input stickerTranslationMap) (stickerTranslationMap, error) {
	result := make(stickerTranslationMap, len(supportedEditableContentLocales))
	for _, locale := range editableStickerLocales() {
		name := strings.TrimSpace(input[locale])
		if name == "" || len([]rune(name)) > 80 {
			return nil, errors.New("all enabled language names are required and must not exceed 80 characters")
		}
		result[locale] = name
	}
	if len(input) != len(result) {
		for locale := range input {
			if _, ok := result[normalizeContentLocale(locale)]; !ok {
				return nil, errors.New("translation contains an unsupported locale")
			}
		}
	}
	return result, nil
}

func validStickerCode(value string) bool {
	if len(value) < 1 || len(value) > 48 {
		return false
	}
	for index, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || index > 0 && (char == '_' || char == '-') {
			continue
		}
		return false
	}
	return true
}

func normalizeStickerStatus(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "active", true
	}
	return value, value == "active" || value == "disabled"
}

func (s *Server) publicStickerCatalog(w http.ResponseWriter, r *http.Request) {
	locale := normalizeContentLocale(r.URL.Query().Get("locale"))
	if _, ok := supportedEditableContentLocales[locale]; !ok {
		locale = "zh-CN"
	}
	rows, err := s.db.Query(r.Context(), `select p.code,p.sort_order,
		coalesce(pt.name,zh.name,en.name,p.code),s.code,s.sort_order,
		coalesce(st.name,stzh.name,sten.name,s.code),f.public_id,s.mime_type,s.width,s.height
		from sticker_packs p
		join stickers s on s.pack_id=p.id and s.status='active'
		join oss_files f on f.id=s.image_file_id and f.status='active' and f.scan_status in ('clean','trusted_generated')
		left join sticker_pack_translations pt on pt.pack_id=p.id and pt.locale=$1
		left join sticker_pack_translations zh on zh.pack_id=p.id and zh.locale='zh-CN'
		left join sticker_pack_translations en on en.pack_id=p.id and en.locale='en-US'
		left join sticker_translations st on st.sticker_id=s.id and st.locale=$1
		left join sticker_translations stzh on stzh.sticker_id=s.id and stzh.locale='zh-CN'
		left join sticker_translations sten on sten.sticker_id=s.id and sten.locale='en-US'
		where p.status='active' order by p.sort_order,p.id,s.sort_order,s.id`, locale)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load sticker catalog")
		return
	}
	defer rows.Close()
	type item struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		ImageURL string `json:"imageURL"`
		MimeType string `json:"mimeType"`
		Width    int    `json:"width"`
		Height   int    `json:"height"`
	}
	type pack struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		Stickers []item `json:"stickers"`
	}
	packs := make([]pack, 0)
	packIndex := map[string]int{}
	for rows.Next() {
		var packCode, packName, code, name, fileID, mimeType string
		var packSort, stickerSort, width, height int
		if err = rows.Scan(&packCode, &packSort, &packName, &code, &stickerSort, &name, &fileID, &mimeType, &width, &height); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read sticker catalog")
			return
		}
		index, ok := packIndex[packCode]
		if !ok {
			index = len(packs)
			packIndex[packCode] = index
			packs = append(packs, pack{Code: packCode, Name: packName, Stickers: []item{}})
		}
		packs[index].Stickers = append(packs[index].Stickers, item{Code: code, Name: name, ImageURL: "/api/v1/oss/files/" + fileID + "/content", MimeType: mimeType, Width: width, Height: height})
	}
	var version int64
	_ = s.db.QueryRow(r.Context(), `select version from sticker_catalog_state where singleton`).Scan(&version)
	writeJSON(w, http.StatusOK, map[string]any{"version": version, "locale": locale, "packs": packs})
}

func (s *Server) adminStickerCatalog(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `select p.code,p.status,p.sort_order,
		coalesce((select jsonb_object_agg(locale,name) from sticker_pack_translations where pack_id=p.id),'{}'::jsonb),
		s.code,s.status,s.sort_order,f.public_id,s.mime_type,s.width,s.height,s.file_size,s.checksum,
		coalesce((select jsonb_object_agg(locale,name) from sticker_translations where sticker_id=s.id),'{}'::jsonb)
		from sticker_packs p left join stickers s on s.pack_id=p.id left join oss_files f on f.id=s.image_file_id
		order by p.sort_order,p.id,s.sort_order,s.id`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load sticker administration data")
		return
	}
	defer rows.Close()
	type adminSticker struct {
		Code         string                `json:"code"`
		Status       string                `json:"status"`
		ImageFileID  string                `json:"imageFileId"`
		MimeType     string                `json:"mimeType"`
		Checksum     string                `json:"checksum"`
		SortOrder    int                   `json:"sortOrder"`
		Width        int                   `json:"width"`
		Height       int                   `json:"height"`
		FileSize     int64                 `json:"fileSize"`
		Translations stickerTranslationMap `json:"translations"`
	}
	type adminPack struct {
		Code         string                `json:"code"`
		Status       string                `json:"status"`
		SortOrder    int                   `json:"sortOrder"`
		Translations stickerTranslationMap `json:"translations"`
		Stickers     []adminSticker        `json:"stickers"`
	}
	packs := make([]adminPack, 0)
	indexes := map[string]int{}
	for rows.Next() {
		var packCode, packStatus string
		var packSort int
		var packTranslations []byte
		var code, status, filePublicID, mimeType, checksum *string
		var sortOrder, width, height *int
		var fileSize *int64
		var translations []byte
		if err = rows.Scan(&packCode, &packStatus, &packSort, &packTranslations, &code, &status, &sortOrder, &filePublicID, &mimeType, &width, &height, &fileSize, &checksum, &translations); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read sticker administration data")
			return
		}
		index, ok := indexes[packCode]
		if !ok {
			var names stickerTranslationMap
			_ = json.Unmarshal(packTranslations, &names)
			index = len(packs)
			indexes[packCode] = index
			packs = append(packs, adminPack{Code: packCode, Status: packStatus, SortOrder: packSort, Translations: names, Stickers: []adminSticker{}})
		}
		if code != nil {
			var names stickerTranslationMap
			_ = json.Unmarshal(translations, &names)
			packs[index].Stickers = append(packs[index].Stickers, adminSticker{Code: *code, Status: stickerStringValue(status), SortOrder: stickerIntValue(sortOrder), ImageFileID: stickerStringValue(filePublicID), MimeType: stickerStringValue(mimeType), Width: stickerIntValue(width), Height: stickerIntValue(height), FileSize: stickerInt64Value(fileSize), Checksum: stickerStringValue(checksum), Translations: names})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"locales": editableStickerLocales(), "packs": packs})
}

func stickerStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func stickerIntValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
func stickerInt64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func (s *Server) createStickerPack(w http.ResponseWriter, r *http.Request) {
	var request stickerPackMutation
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid sticker pack request")
		return
	}
	request.Code = strings.ToLower(strings.TrimSpace(request.Code))
	status, ok := normalizeStickerStatus(request.Status)
	names, err := normalizeStickerTranslations(request.Translations)
	if !validStickerCode(request.Code) || !ok || err != nil {
		writeAPIError(w, http.StatusBadRequest, "STICKER_PACK_INVALID", "sticker pack fields are invalid", 0, nil)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to start sticker pack update")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var packID int64
	err = tx.QueryRow(r.Context(), `insert into sticker_packs(code,status,sort_order,created_by) values($1,$2,$3,$4) returning id`, request.Code, status, request.SortOrder, currentClaims(r).Subject).Scan(&packID)
	if err != nil {
		writeAPIError(w, http.StatusConflict, "STICKER_PACK_CODE_EXISTS", "sticker pack code already exists", 0, nil)
		return
	}
	for locale, name := range names {
		if _, err = tx.Exec(r.Context(), `insert into sticker_pack_translations(pack_id,locale,name) values($1,$2,$3)`, packID, locale, name); err != nil {
			writeError(w, 500, "failed to save sticker pack translations")
			return
		}
	}
	if err = bumpStickerCatalogVersion(r.Context(), tx); err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, 500, "failed to save sticker pack")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"code": request.Code})
}

func (s *Server) updateStickerPack(w http.ResponseWriter, r *http.Request) {
	code := strings.ToLower(strings.TrimSpace(r.PathValue("code")))
	var request stickerPackMutation
	if decodeJSON(r, &request) != nil {
		writeError(w, 400, "invalid sticker pack request")
		return
	}
	status, ok := normalizeStickerStatus(request.Status)
	names, err := normalizeStickerTranslations(request.Translations)
	if !validStickerCode(code) || !ok || err != nil {
		writeAPIError(w, 400, "STICKER_PACK_INVALID", "sticker pack fields are invalid", 0, nil)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to start sticker pack update")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var packID int64
	if err = tx.QueryRow(r.Context(), `update sticker_packs set status=$2,sort_order=$3,updated_at=now() where code=$1 returning id`, code, status, request.SortOrder).Scan(&packID); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "sticker pack does not exist")
		return
	} else if err != nil {
		writeError(w, 500, "failed to update sticker pack")
		return
	}
	for locale, name := range names {
		if _, err = tx.Exec(r.Context(), `insert into sticker_pack_translations(pack_id,locale,name) values($1,$2,$3) on conflict(pack_id,locale) do update set name=excluded.name`, packID, locale, name); err != nil {
			writeError(w, 500, "failed to update sticker translations")
			return
		}
	}
	if err = bumpStickerCatalogVersion(r.Context(), tx); err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, 500, "failed to save sticker pack")
		return
	}
	writeJSON(w, 200, map[string]any{"code": code})
}

func (s *Server) createSticker(w http.ResponseWriter, r *http.Request) { s.mutateSticker(w, r, false) }
func (s *Server) updateSticker(w http.ResponseWriter, r *http.Request) { s.mutateSticker(w, r, true) }

func (s *Server) mutateSticker(w http.ResponseWriter, r *http.Request, update bool) {
	packCode := strings.ToLower(strings.TrimSpace(r.PathValue("packCode")))
	code := strings.ToLower(strings.TrimSpace(r.PathValue("code")))
	var request stickerMutation
	if decodeJSON(r, &request) != nil {
		writeError(w, 400, "invalid sticker request")
		return
	}
	if !update {
		code = strings.ToLower(strings.TrimSpace(request.Code))
	}
	status, ok := normalizeStickerStatus(request.Status)
	names, err := normalizeStickerTranslations(request.Translations)
	if !validStickerCode(packCode) || !validStickerCode(code) || !ok || err != nil {
		writeAPIError(w, 400, "STICKER_INVALID", "sticker fields are invalid", 0, nil)
		return
	}
	var image stickerImageMeta
	if strings.TrimSpace(request.ImageFileID) != "" {
		if !claimsAllow(currentClaims(r), "sticker.upload") {
			writeError(w, http.StatusForbidden, "sticker upload permission is required to replace an image")
			return
		}
		image, err = s.validateStickerOSSFile(r.Context(), request.ImageFileID, currentClaims(r).Subject)
		if err != nil {
			writeAPIError(w, 422, "STICKER_IMAGE_INVALID", err.Error(), 0, nil)
			return
		}
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to start sticker update")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var packID, stickerID, previousImageFileID int64
	if err = tx.QueryRow(r.Context(), `select id from sticker_packs where code=$1`, packCode).Scan(&packID); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "sticker pack does not exist")
		return
	} else if err != nil {
		writeError(w, 500, "failed to load sticker pack")
		return
	}
	if update {
		err = tx.QueryRow(r.Context(), `select id,image_file_id from stickers where pack_id=$1 and code=$2 for update`, packID, code).Scan(&stickerID, &previousImageFileID)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, 404, "sticker does not exist")
			return
		}
		if err != nil {
			writeError(w, 500, "failed to lock sticker")
			return
		}
		if image.FileID > 0 {
			_, err = tx.Exec(r.Context(), `update stickers set image_file_id=$2,mime_type=$3,width=$4,height=$5,file_size=$6,checksum=$7,status=$8,sort_order=$9,updated_at=now() where id=$1`, stickerID, image.FileID, image.ContentType, image.Width, image.Height, image.Size, image.SHA256, status, request.SortOrder)
		} else {
			_, err = tx.Exec(r.Context(), `update stickers set status=$2,sort_order=$3,updated_at=now() where id=$1`, stickerID, status, request.SortOrder)
		}
	} else {
		if image.FileID == 0 {
			writeAPIError(w, 400, "STICKER_IMAGE_REQUIRED", "a validated PNG or GIF is required", 0, nil)
			return
		}
		err = tx.QueryRow(r.Context(), `insert into stickers(pack_id,code,image_file_id,mime_type,width,height,file_size,checksum,status,sort_order,created_by) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) returning id`, packID, code, image.FileID, image.ContentType, image.Width, image.Height, image.Size, image.SHA256, status, request.SortOrder, currentClaims(r).Subject).Scan(&stickerID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "sticker does not exist")
		return
	} else if err != nil {
		writeAPIError(w, 409, "STICKER_CODE_EXISTS", "sticker code already exists in this pack", 0, nil)
		return
	}
	for locale, name := range names {
		if _, err = tx.Exec(r.Context(), `insert into sticker_translations(sticker_id,locale,name) values($1,$2,$3) on conflict(sticker_id,locale) do update set name=excluded.name`, stickerID, locale, name); err != nil {
			writeError(w, 500, "failed to save sticker translations")
			return
		}
	}
	if image.FileID > 0 {
		if _, err = tx.Exec(r.Context(), `update oss_files set source='sticker',scan_status='clean' where id=$1`, image.FileID); err != nil {
			writeError(w, 500, "failed to activate sticker image")
			return
		}
		if update && previousImageFileID > 0 && previousImageFileID != image.FileID {
			if err = s.tombstoneOSSFileTx(r.Context(), tx, previousImageFileID, "sticker_image_replaced"); err != nil {
				writeError(w, 500, "failed to queue replaced sticker image deletion")
				return
			}
		}
	}
	if err = bumpStickerCatalogVersion(r.Context(), tx); err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, 500, "failed to save sticker")
		return
	}
	writeJSON(w, map[bool]int{true: 200, false: 201}[update], map[string]any{"packCode": packCode, "code": code})
}

func bumpStickerCatalogVersion(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `update sticker_catalog_state set version=version+1,updated_at=now() where singleton`)
	return err
}

func (s *Server) deleteUnusedStickerPack(w http.ResponseWriter, r *http.Request) {
	code := strings.ToLower(strings.TrimSpace(r.PathValue("code")))
	if !validStickerCode(code) {
		writeError(w, http.StatusBadRequest, "invalid sticker pack code")
		return
	}
	var used bool
	if err := s.db.QueryRow(r.Context(), `select exists(select 1 from stickers sticker
		join sticker_packs pack on pack.id=sticker.pack_id where pack.code=$1)`, code).Scan(&used); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect sticker pack")
		return
	}
	if used {
		writeAPIError(w, http.StatusConflict, "STICKER_PACK_NOT_EMPTY", "disable or remove unused stickers before deleting the pack", 0, nil)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete sticker pack")
		return
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(r.Context(), `delete from sticker_packs where code=$1`, code)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete sticker pack")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "sticker pack does not exist")
		return
	}
	if err = bumpStickerCatalogVersion(r.Context(), tx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update sticker catalog version")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete sticker pack")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteUnusedSticker(w http.ResponseWriter, r *http.Request) {
	packCode := strings.ToLower(strings.TrimSpace(r.PathValue("packCode")))
	code := strings.ToLower(strings.TrimSpace(r.PathValue("code")))
	if !validStickerCode(packCode) || !validStickerCode(code) {
		writeError(w, http.StatusBadRequest, "invalid sticker code")
		return
	}
	token := "[sticker:" + packCode + ":" + code + "]"
	used, err := s.stickerMarkdownIsUsed(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect sticker usage")
		return
	}
	if used {
		writeAPIError(w, http.StatusConflict, "STICKER_IN_USE", "the sticker is referenced by published or historical content; disable it instead", 0, nil)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete sticker")
		return
	}
	defer tx.Rollback(r.Context())
	var fileID int64
	err = tx.QueryRow(r.Context(), `delete from stickers using sticker_packs pack
		where stickers.pack_id=pack.id and pack.code=$1 and stickers.code=$2 returning stickers.image_file_id`, packCode, code).Scan(&fileID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "sticker does not exist")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete sticker")
		return
	}
	if err = s.tombstoneOSSFileTx(r.Context(), tx, fileID, "unused_sticker_deleted"); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to queue sticker image deletion")
		return
	}
	if err = bumpStickerCatalogVersion(r.Context(), tx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update sticker catalog version")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete sticker")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) stickerMarkdownIsUsed(ctx context.Context, token string) (bool, error) {
	var used bool
	err := s.db.QueryRow(ctx, `select
		exists(select 1 from comments where position($1 in body)>0) or
		exists(select 1 from mods where position($1 in body_markdown)>0) or
		exists(select 1 from modpacks where position($1 in body_markdown)>0) or
		exists(select 1 from simple_projects where position($1 in body_markdown)>0) or
		exists(select 1 from community_posts where position($1 in body_markdown)>0) or
		exists(select 1 from minecraft_servers where position($1 in body_markdown)>0) or
		exists(select 1 from blueprints where position($1 in description_markdown)>0) or
		exists(select 1 from project_changelog_localizations where position($1 in body_markdown)>0) or
		exists(select 1 from mod_resource_version_detail_localizations where position($1 in content_markdown)>0)`, token).Scan(&used)
	return used, err
}

func (s *Server) validateStickerOSSFile(ctx context.Context, publicID string, uploaderID int64) (stickerImageMeta, error) {
	limits := normalizedStickerLimits(s.cfg.Sticker)
	var result stickerImageMeta
	var originalName string
	err := s.db.QueryRow(ctx, `select id,public_id,object_key,content_type,size_bytes,sha256,original_name from oss_files where public_id=$1 and uploader_id=$2 and status='active'`, strings.TrimSpace(publicID), uploaderID).Scan(&result.FileID, &result.PublicID, &result.ObjectKey, &result.ContentType, &result.Size, &result.SHA256, &originalName)
	if err != nil {
		return result, errors.New("uploaded sticker file does not exist")
	}
	ext := strings.ToLower(filepath.Ext(originalName))
	if ext != ".png" && ext != ".gif" {
		return result, errors.New("stickers only support PNG or GIF")
	}
	if result.Size <= 0 || result.Size > limits.MaxBytes {
		return result, errors.New("sticker file exceeds the configured size limit")
	}
	client, cfg, err := s.ossClient(ctx)
	if err != nil {
		return result, errors.New("sticker storage is unavailable")
	}
	object, err := client.GetObject(ctx, &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(result.ObjectKey)})
	if err != nil {
		return result, errors.New("failed to read uploaded sticker")
	}
	defer object.Body.Close()
	data, err := io.ReadAll(io.LimitReader(object.Body, limits.MaxBytes+1))
	if err != nil || int64(len(data)) != result.Size {
		return result, errors.New("sticker file size does not match upload")
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != result.SHA256 {
		return result, errors.New("sticker file hash does not match upload")
	}
	detected := normalizeRasterContentType(http.DetectContentType(data))
	declared := normalizeRasterContentType(result.ContentType)
	if detected != declared || (declared != "image/png" && declared != "image/gif") {
		return result, errors.New("sticker extension, MIME and magic bytes do not match")
	}
	decoded, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || normalizeRasterContentType("image/"+format) != declared {
		return result, errors.New("sticker image cannot be decoded")
	}
	if decoded.Width <= 0 || decoded.Height <= 0 || decoded.Width > limits.MaxEdge || decoded.Height > limits.MaxEdge || int64(decoded.Width)*int64(decoded.Height) > limits.MaxPixels {
		return result, errors.New("sticker dimensions exceed the safety limit")
	}
	if declared == "image/gif" {
		animation, decodeErr := gif.DecodeAll(bytes.NewReader(data))
		if decodeErr != nil || len(animation.Image) == 0 || len(animation.Image) > limits.MaxGIFFrames {
			return result, errors.New("GIF frame count is invalid")
		}
		totalPixels, duration := int64(0), 0
		for index, frame := range animation.Image {
			bounds := frame.Bounds()
			totalPixels += int64(bounds.Dx()) * int64(bounds.Dy())
			if index < len(animation.Delay) {
				duration += animation.Delay[index]
			}
		}
		if totalPixels > limits.MaxGIFDecodedPixels || time.Duration(duration)*10*time.Millisecond > limits.MaxGIFDuration {
			return result, errors.New("GIF animation exceeds the decoded size or duration limit")
		}
	} else if _, _, err = image.Decode(bytes.NewReader(data)); err != nil {
		return result, errors.New("PNG image is invalid or truncated")
	}
	result.Width, result.Height = decoded.Width, decoded.Height
	return result, nil
}
