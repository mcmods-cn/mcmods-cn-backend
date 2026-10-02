package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image/gif"
	"image/png"
	"io"
	"log"
	"net/http"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"mcmods-cn-backend/internal/config"
)

const (
	maxStickerBytes         = int64(4 << 20)
	maxStickerEdge          = 1024
	maxStickerPixels        = int64(4_194_304)
	maxStickerGIFFrames     = 120
	maxStickerGIFTotalPixel = int64(64_000_000)
	defaultStickerPacks     = 64
	defaultStickersPerPack  = 128
	defaultStickerCatalog   = 1024
	hardStickerPacks        = 256
	hardStickersPerPack     = 512
	hardStickerCatalog      = 4096
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
	if value.MaxPacks <= 0 {
		value.MaxPacks = defaultStickerPacks
	} else if value.MaxPacks > hardStickerPacks {
		value.MaxPacks = hardStickerPacks
	}
	if value.MaxStickersPerPack <= 0 {
		value.MaxStickersPerPack = defaultStickersPerPack
	} else if value.MaxStickersPerPack > hardStickersPerPack {
		value.MaxStickersPerPack = hardStickersPerPack
	}
	if value.MaxCatalogItems <= 0 {
		value.MaxCatalogItems = defaultStickerCatalog
	} else if value.MaxCatalogItems > hardStickerCatalog {
		value.MaxCatalogItems = hardStickerCatalog
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
	FileID        int64
	SourceFileID  int64
	ContentType   string
	Size          int64
	SHA256        string
	Width         int
	Height        int
	SanitizedData []byte
}

type stickerReferenceQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

var errStickerImageNoLongerActive = errors.New("sticker image is no longer active")

func parseStickerToken(token string) (string, string, bool) {
	value, ok := strings.CutPrefix(token, "[sticker:")
	if !ok || !strings.HasSuffix(value, "]") {
		return "", "", false
	}
	value = strings.TrimSuffix(value, "]")
	packCode, code, ok := strings.Cut(value, ":")
	if !ok || strings.Contains(code, ":") || !validStickerCode(packCode) || !validStickerCode(code) {
		return "", "", false
	}
	return packCode, code, true
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
	limits := normalizedStickerLimits(s.cfg.Sticker)
	locale := normalizeContentLocale(r.URL.Query().Get("locale"))
	if _, ok := supportedEditableContentLocales[locale]; !ok {
		locale = "zh-CN"
	}
	rows, err := s.db.Query(r.Context(), `select p.code,p.sort_order,
		coalesce(pt.name,zh.name,en.name,p.code),s.code,s.sort_order,
		coalesce(st.name,stzh.name,sten.name,s.code),f.public_id,s.mime_type,s.width,s.height
		from sticker_packs p
		join stickers s on s.pack_id=p.id and s.status='active'
		join oss_files f on f.id=s.image_file_id and f.status='active' and f.scan_status='trusted_generated' and f.source='sticker_derived'
		left join sticker_pack_translations pt on pt.pack_id=p.id and pt.locale=$1
		left join sticker_pack_translations zh on zh.pack_id=p.id and zh.locale='zh-CN'
		left join sticker_pack_translations en on en.pack_id=p.id and en.locale='en-US'
		left join sticker_translations st on st.sticker_id=s.id and st.locale=$1
		left join sticker_translations stzh on stzh.sticker_id=s.id and stzh.locale='zh-CN'
		left join sticker_translations sten on sten.sticker_id=s.id and sten.locale='en-US'
		where p.status='active' order by p.sort_order,p.id,s.sort_order,s.id limit $2`, locale, limits.MaxCatalogItems+1)
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
	rowCount := 0
	for rows.Next() {
		rowCount++
		if rowCount > limits.MaxCatalogItems {
			writeError(w, http.StatusInternalServerError, "sticker catalog exceeds the configured item budget")
			return
		}
		var packCode, packName, code, name, fileID, mimeType string
		var packSort, stickerSort, width, height int
		if err = rows.Scan(&packCode, &packSort, &packName, &code, &stickerSort, &name, &fileID, &mimeType, &width, &height); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read sticker catalog")
			return
		}
		index, ok := packIndex[packCode]
		if !ok {
			if len(packs) >= limits.MaxPacks {
				writeError(w, http.StatusInternalServerError, "sticker catalog exceeds the configured pack budget")
				return
			}
			index = len(packs)
			packIndex[packCode] = index
			packs = append(packs, pack{Code: packCode, Name: packName, Stickers: []item{}})
		}
		if len(packs[index].Stickers) >= limits.MaxStickersPerPack {
			writeError(w, http.StatusInternalServerError, "sticker pack exceeds the configured item budget")
			return
		}
		packs[index].Stickers = append(packs[index].Stickers, item{Code: code, Name: name, ImageURL: "/api/v1/oss/files/" + fileID + "/content", MimeType: mimeType, Width: width, Height: height})
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read sticker catalog")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"locale": locale, "packs": packs})
}

func (s *Server) adminStickerCatalog(w http.ResponseWriter, r *http.Request) {
	limits := normalizedStickerLimits(s.cfg.Sticker)
	rows, err := s.db.Query(r.Context(), `select p.code,p.status,p.sort_order,
		coalesce((select jsonb_object_agg(locale,name) from sticker_pack_translations where pack_id=p.id),'{}'::jsonb),
		s.code,s.status,s.sort_order,f.public_id,s.mime_type,s.width,s.height,s.file_size,s.checksum,
		coalesce((select jsonb_object_agg(locale,name) from sticker_translations where sticker_id=s.id),'{}'::jsonb)
		from sticker_packs p left join stickers s on s.pack_id=p.id left join oss_files f on f.id=s.image_file_id
		order by p.sort_order,p.id,s.sort_order,s.id limit $1`, limits.MaxCatalogItems+limits.MaxPacks+1)
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
	rowCount := 0
	for rows.Next() {
		rowCount++
		if rowCount > limits.MaxCatalogItems+limits.MaxPacks {
			writeError(w, http.StatusInternalServerError, "sticker administration catalog exceeds the configured budget")
			return
		}
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
			if err = json.Unmarshal(packTranslations, &names); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to decode sticker pack translations")
				return
			}
			index = len(packs)
			if index >= limits.MaxPacks {
				writeError(w, http.StatusInternalServerError, "sticker administration catalog exceeds the configured pack budget")
				return
			}
			indexes[packCode] = index
			packs = append(packs, adminPack{Code: packCode, Status: packStatus, SortOrder: packSort, Translations: names, Stickers: []adminSticker{}})
		}
		if code != nil {
			var names stickerTranslationMap
			if err = json.Unmarshal(translations, &names); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to decode sticker translations")
				return
			}
			if len(packs[index].Stickers) >= limits.MaxStickersPerPack {
				writeError(w, http.StatusInternalServerError, "sticker pack exceeds the configured item budget")
				return
			}
			packs[index].Stickers = append(packs[index].Stickers, adminSticker{Code: *code, Status: stickerStringValue(status), SortOrder: stickerIntValue(sortOrder), ImageFileID: stickerStringValue(filePublicID), MimeType: stickerStringValue(mimeType), Width: stickerIntValue(width), Height: stickerIntValue(height), FileSize: stickerInt64Value(fileSize), Checksum: stickerStringValue(checksum), Translations: names})
		}
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read sticker administration data")
		return
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
	limits := normalizedStickerLimits(s.cfg.Sticker)
	if err = lockStickerCatalogBudgetTx(r.Context(), tx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock sticker catalog budget")
		return
	}
	var packBudgetAvailable bool
	if err = tx.QueryRow(r.Context(), `select count(*)<$1 from sticker_packs`, limits.MaxPacks).Scan(&packBudgetAvailable); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect sticker pack budget")
		return
	}
	if !packBudgetAvailable {
		writeAPIError(w, http.StatusConflict, "STICKER_CATALOG_LIMIT", "the configured sticker pack limit has been reached", 0, nil)
		return
	}
	var packID int64
	err = tx.QueryRow(r.Context(), `insert into sticker_packs(code,status,sort_order,created_by) values($1,$2,$3,$4) returning id`, request.Code, status, request.SortOrder, currentClaims(r).Subject).Scan(&packID)
	if err != nil {
		writeStickerPackCreateDatabaseError(w, err)
		return
	}
	for locale, name := range names {
		if _, err = tx.Exec(r.Context(), `insert into sticker_pack_translations(pack_id,locale,name) values($1,$2,$3)`, packID, locale, name); err != nil {
			writeError(w, 500, "failed to save sticker pack translations")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
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
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to save sticker pack")
		return
	}
	writeJSON(w, 200, map[string]any{"code": code})
}

func (s *Server) createSticker(w http.ResponseWriter, r *http.Request) { s.mutateSticker(w, r, false) }
func (s *Server) updateSticker(w http.ResponseWriter, r *http.Request) { s.mutateSticker(w, r, true) }

func isTemporaryStickerUploadSource(source string) bool {
	source = strings.TrimSpace(source)
	return source == "sticker-upload" || strings.HasPrefix(source, "sticker-upload:") && len(source) > len("sticker-upload:")
}

func (s *Server) discardStickerUpload(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("fileId")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "sticker upload file is invalid")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start sticker upload cleanup")
		return
	}
	defer tx.Rollback(r.Context())
	var fileID int64
	var status, source string
	err = tx.QueryRow(r.Context(), `select id,status,source from oss_files
		where public_id=$1 and uploader_id=$2 for update`, publicID, currentClaims(r).Subject).Scan(&fileID, &status, &source)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "sticker upload file does not exist")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load sticker upload file")
		return
	}
	if !isTemporaryStickerUploadSource(source) {
		writeError(w, http.StatusNotFound, "sticker upload file does not exist")
		return
	}
	if status == "active" {
		if err = s.tombstoneUnreferencedStickerFileTx(r.Context(), tx, fileID, "sticker_upload_discarded"); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to discard sticker upload file")
			return
		}
		if err = tx.QueryRow(r.Context(), `select status from oss_files where id=$1`, fileID).Scan(&status); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to verify sticker upload cleanup")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to discard sticker upload file")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"discarded": status == "deleted"})
}

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
		image, err = s.persistSanitizedStickerImage(r.Context(), image, currentClaims(r).Subject, packCode, code)
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to persist sanitized sticker image")
			return
		}
	}
	imageBound := false
	if image.FileID > 0 {
		defer func() {
			if imageBound {
				return
			}
			cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 15*time.Second)
			defer cancel()
			if cleanupErr := s.cleanupUnboundStickerImage(cleanupContext, image.FileID); cleanupErr != nil {
				log.Printf("cleanup unbound sticker derivative %d: %v", image.FileID, cleanupErr)
			}
		}()
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to start sticker update")
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	limits := normalizedStickerLimits(s.cfg.Sticker)
	if !update {
		if err = lockStickerCatalogBudgetTx(r.Context(), tx); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to lock sticker catalog budget")
			return
		}
	}
	var packID, stickerID, previousImageFileID int64
	if err = tx.QueryRow(r.Context(), `select id from sticker_packs where code=$1 for update`, packCode).Scan(&packID); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "sticker pack does not exist")
		return
	} else if err != nil {
		writeError(w, 500, "failed to load sticker pack")
		return
	}
	if !update {
		var packBudgetAvailable, catalogBudgetAvailable bool
		if err = tx.QueryRow(r.Context(), `select (select count(*) from stickers where pack_id=$1)<$2,
			(select count(*) from stickers)<$3`, packID, limits.MaxStickersPerPack, limits.MaxCatalogItems).Scan(&packBudgetAvailable, &catalogBudgetAvailable); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to inspect sticker catalog budget")
			return
		}
		if !packBudgetAvailable || !catalogBudgetAvailable {
			writeAPIError(w, http.StatusConflict, "STICKER_CATALOG_LIMIT", "the configured sticker catalog limit has been reached", 0, nil)
			return
		}
	}
	if update {
		err = tx.QueryRow(r.Context(), `select id,image_file_id from stickers where pack_id=$1 and code=$2`, packID, code).Scan(&stickerID, &previousImageFileID)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, 404, "sticker does not exist")
			return
		}
		if err != nil {
			writeError(w, 500, "failed to lock sticker")
			return
		}
	}
	if err = lockStickerImageFilesTx(r.Context(), tx, image.FileID, previousImageFileID, image.FileID, image.SourceFileID); err != nil {
		if errors.Is(err, errStickerImageNoLongerActive) {
			writeAPIError(w, http.StatusConflict, "STICKER_IMAGE_INVALID", "the uploaded image is no longer active", 0, nil)
		} else {
			writeError(w, http.StatusInternalServerError, "failed to lock sticker image")
		}
		return
	}
	if update {
		var lockedStickerID, lockedImageFileID int64
		err = tx.QueryRow(r.Context(), `select id,image_file_id from stickers where pack_id=$1 and code=$2 for update`, packID, code).Scan(&lockedStickerID, &lockedImageFileID)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, 404, "sticker does not exist")
			return
		}
		if err != nil || lockedStickerID != stickerID || lockedImageFileID != previousImageFileID {
			writeError(w, 409, "sticker changed while its image was being locked")
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
		writeStickerMutationDatabaseError(w, err)
		return
	}
	for locale, name := range names {
		if _, err = tx.Exec(r.Context(), `insert into sticker_translations(sticker_id,locale,name) values($1,$2,$3) on conflict(sticker_id,locale) do update set name=excluded.name`, stickerID, locale, name); err != nil {
			writeError(w, 500, "failed to save sticker translations")
			return
		}
	}
	if image.FileID > 0 {
		if err = s.tombstoneUnreferencedStickerFileTx(r.Context(), tx, image.SourceFileID, "sticker_source_consumed"); err != nil {
			writeError(w, 500, "failed to retire the private sticker upload")
			return
		}
		if update && previousImageFileID > 0 && previousImageFileID != image.FileID {
			if err = s.tombstoneUnreferencedStickerFileTx(r.Context(), tx, previousImageFileID, "sticker_image_replaced"); err != nil {
				writeError(w, 500, "failed to queue replaced sticker image deletion")
				return
			}
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to save sticker")
		return
	}
	imageBound = image.FileID > 0
	writeJSON(w, map[bool]int{true: 200, false: 201}[update], map[string]any{"packCode": packCode, "code": code})
}

func writeStickerMutationDatabaseError(w http.ResponseWriter, err error) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		if pgErr.ConstraintName == "uq_stickers_image_file" {
			writeAPIError(w, http.StatusConflict, "STICKER_IMAGE_IN_USE", "the uploaded image already belongs to another sticker", 0, nil)
			return
		}
		writeAPIError(w, http.StatusConflict, "STICKER_CODE_EXISTS", "sticker code already exists in this pack", 0, nil)
		return
	}
	writeError(w, http.StatusInternalServerError, "failed to save sticker")
}

func writeStickerPackCreateDatabaseError(w http.ResponseWriter, err error) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "sticker_packs_code_key" {
		writeAPIError(w, http.StatusConflict, "STICKER_PACK_CODE_EXISTS", "sticker pack code already exists", 0, nil)
		return
	}
	writeError(w, http.StatusInternalServerError, "failed to save sticker pack")
}

func lockStickerImageFilesTx(ctx context.Context, tx pgx.Tx, requiredActiveFileID int64, fileIDs ...int64) error {
	unique := make(map[int64]struct{}, len(fileIDs))
	ordered := make([]int64, 0, len(fileIDs))
	for _, fileID := range fileIDs {
		if fileID <= 0 {
			continue
		}
		if _, exists := unique[fileID]; exists {
			continue
		}
		unique[fileID] = struct{}{}
		ordered = append(ordered, fileID)
	}
	sort.Slice(ordered, func(left, right int) bool { return ordered[left] < ordered[right] })
	for _, fileID := range ordered {
		var fileStatus string
		if err := tx.QueryRow(ctx, `select status from oss_files where id=$1 for update`, fileID).Scan(&fileStatus); err != nil {
			return err
		}
		if fileID == requiredActiveFileID && fileStatus != "active" {
			return errStickerImageNoLongerActive
		}
	}
	return nil
}

func lockStickerCatalogBudgetTx(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('sticker-catalog-budget'))`)
	return err
}

func (s *Server) deleteUnusedStickerPack(w http.ResponseWriter, r *http.Request) {
	code := strings.ToLower(strings.TrimSpace(r.PathValue("code")))
	if !validStickerCode(code) {
		writeError(w, http.StatusBadRequest, "invalid sticker pack code")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete sticker pack")
		return
	}
	defer tx.Rollback(r.Context())
	var packID int64
	if err = tx.QueryRow(r.Context(), `select id from sticker_packs where code=$1 for update`, code).Scan(&packID); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "sticker pack does not exist")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock sticker pack")
		return
	}
	var used bool
	if err = tx.QueryRow(r.Context(), `select exists(select 1 from stickers where pack_id=$1)`, packID).Scan(&used); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect sticker pack")
		return
	}
	if used {
		writeAPIError(w, http.StatusConflict, "STICKER_PACK_NOT_EMPTY", "disable or remove unused stickers before deleting the pack", 0, nil)
		return
	}
	tag, err := tx.Exec(r.Context(), `delete from sticker_packs where id=$1`, packID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete sticker pack")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "sticker pack does not exist")
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
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete sticker")
		return
	}
	defer tx.Rollback(r.Context())
	var packID int64
	if err = tx.QueryRow(r.Context(), `select id from sticker_packs where code=$1 for update`, packCode).Scan(&packID); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "sticker does not exist")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock sticker pack")
		return
	}
	var stickerID, fileID int64
	err = tx.QueryRow(r.Context(), `select id,image_file_id from stickers where pack_id=$1 and code=$2 for update`, packID, code).Scan(&stickerID, &fileID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "sticker does not exist")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete sticker")
		return
	}
	if err = lockStickerImageFilesTx(r.Context(), tx, 0, fileID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock sticker image")
		return
	}
	if err = lockStickerReferenceTx(r.Context(), tx, packCode, code); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock sticker usage")
		return
	}
	used, err := stickerMarkdownIsUsed(r.Context(), tx, "[sticker:"+packCode+":"+code+"]")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect sticker usage")
		return
	}
	if used {
		writeAPIError(w, http.StatusConflict, "STICKER_IN_USE", "the sticker is referenced by published or historical content; disable it instead", 0, nil)
		return
	}
	if _, err = tx.Exec(r.Context(), `delete from stickers where id=$1`, stickerID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete sticker")
		return
	}
	if err = s.tombstoneUnreferencedStickerFileTx(r.Context(), tx, fileID, "unused_sticker_deleted"); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to queue sticker image deletion")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete sticker")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func stickerMarkdownIsUsed(ctx context.Context, queryer stickerReferenceQueryer, token string) (bool, error) {
	packCode, code, ok := parseStickerToken(token)
	if !ok {
		return false, errors.New("invalid sticker token")
	}
	return stickerMarkdownIsUsedWithQueryer(ctx, queryer, packCode, code)
}

func stickerMarkdownIsUsedWithQueryer(ctx context.Context, queryer stickerReferenceQueryer, packCode, code string) (bool, error) {
	var used bool
	err := queryer.QueryRow(ctx, `select exists(select 1 from sticker_content_references where pack_code=$1 and sticker_code=$2)`, packCode, code).Scan(&used)
	return used, err
}

func lockStickerReferenceTx(ctx context.Context, tx pgx.Tx, packCode, code string) error {
	_, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('sticker-reference'),hashtext($1||':'||$2))`, packCode, code)
	return err
}

func (s *Server) tombstoneUnreferencedStickerFileTx(ctx context.Context, tx pgx.Tx, fileID int64, reason string) error {
	var referenced bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from stickers where image_file_id=$1)`, fileID).Scan(&referenced); err != nil {
		return err
	}
	if referenced {
		return nil
	}
	return s.tombstoneOSSFileTx(ctx, tx, fileID, reason)
}

func (s *Server) persistSanitizedStickerImage(ctx context.Context, image stickerImageMeta, uploaderID int64, packCode, code string) (stickerImageMeta, error) {
	if image.SourceFileID <= 0 || len(image.SanitizedData) == 0 {
		return image, errors.New("sanitized sticker image is empty")
	}
	extension := ".png"
	if image.ContentType == "image/gif" {
		extension = ".gif"
	}
	cfg := s.ossConfigFromSettings(ctx)
	objectKey := buildOSSObjectKeyForFile(cfg.Prefix, path.Join("stickers", "derived", packCode, code), code+extension)
	fileID, err := s.writeGeneratedOSSObject(ctx, objectKey, code+extension, image.ContentType, image.SanitizedData, uploaderID, "sticker_derived")
	if err != nil {
		return image, err
	}
	image.FileID = fileID
	image.SanitizedData = nil
	return image, nil
}

func (s *Server) cleanupUnboundStickerImage(ctx context.Context, fileID int64) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockStickerImageFilesTx(ctx, tx, 0, fileID); err != nil {
		return err
	}
	if err = s.tombstoneUnreferencedStickerFileTx(ctx, tx, fileID, "unbound_sticker_derivative"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func sanitizeStickerImage(data []byte, declaredContentType string, limits config.StickerConfig) ([]byte, int, int, error) {
	limits = normalizedStickerLimits(limits)
	if len(data) == 0 || int64(len(data)) > limits.MaxBytes {
		return nil, 0, 0, errors.New("sticker file exceeds the configured size limit")
	}
	declaredContentType = normalizeRasterContentType(declaredContentType)
	if detected := normalizeRasterContentType(http.DetectContentType(data)); detected != declaredContentType ||
		(declaredContentType != "image/png" && declaredContentType != "image/gif") {
		return nil, 0, 0, errors.New("sticker MIME and magic bytes do not match")
	}
	var output bytes.Buffer
	var width, height int
	if declaredContentType == "image/png" {
		decoded, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, 0, 0, errors.New("PNG image is invalid or truncated")
		}
		bounds := decoded.Bounds()
		width, height = bounds.Dx(), bounds.Dy()
		if width <= 0 || height <= 0 || width > limits.MaxEdge || height > limits.MaxEdge || int64(width)*int64(height) > limits.MaxPixels {
			return nil, 0, 0, errors.New("sticker dimensions exceed the safety limit")
		}
		if err = png.Encode(&output, decoded); err != nil {
			return nil, 0, 0, errors.New("failed to encode sanitized PNG")
		}
	} else {
		animation, err := decodeGIFAnimationWithBudget(data, limits.MaxGIFFrames, limits.MaxGIFDecodedPixels, limits.MaxGIFDuration)
		if err != nil {
			return nil, 0, 0, errors.New("GIF frame count is invalid")
		}
		width, height = animation.Config.Width, animation.Config.Height
		if width <= 0 || height <= 0 || width > limits.MaxEdge || height > limits.MaxEdge || int64(width)*int64(height) > limits.MaxPixels {
			return nil, 0, 0, errors.New("sticker dimensions exceed the safety limit")
		}
		totalPixels, duration := int64(0), 0
		for index, frame := range animation.Image {
			bounds := frame.Bounds()
			if bounds.Dx() <= 0 || bounds.Dy() <= 0 || bounds.Dx() > limits.MaxEdge || bounds.Dy() > limits.MaxEdge {
				return nil, 0, 0, errors.New("GIF frame dimensions are invalid")
			}
			totalPixels += int64(bounds.Dx()) * int64(bounds.Dy())
			if index < len(animation.Delay) {
				duration += animation.Delay[index]
			}
		}
		if totalPixels > limits.MaxGIFDecodedPixels || time.Duration(duration)*10*time.Millisecond > limits.MaxGIFDuration {
			return nil, 0, 0, errors.New("GIF animation exceeds the decoded size or duration limit")
		}
		if err = gif.EncodeAll(&output, animation); err != nil {
			return nil, 0, 0, errors.New("failed to encode sanitized GIF")
		}
	}
	if int64(output.Len()) > limits.MaxBytes {
		return nil, 0, 0, errors.New("sanitized sticker exceeds the configured size limit")
	}
	return output.Bytes(), width, height, nil
}

func (s *Server) validateStickerOSSFile(ctx context.Context, publicID string, uploaderID int64) (stickerImageMeta, error) {
	limits := normalizedStickerLimits(s.cfg.Sticker)
	var result stickerImageMeta
	var originalName, sourceObjectKey, sourceContentType, sourceSHA256, source string
	var sourceSize int64
	err := s.db.QueryRow(ctx, `select id,object_key,content_type,size_bytes,sha256,original_name,source from oss_files
		where public_id=$1 and uploader_id=$2 and status='active'`, strings.TrimSpace(publicID), uploaderID).
		Scan(&result.SourceFileID, &sourceObjectKey, &sourceContentType, &sourceSize, &sourceSHA256, &originalName, &source)
	if err != nil {
		return result, errors.New("uploaded sticker file does not exist")
	}
	if !isTemporaryStickerUploadSource(source) {
		return result, errors.New("uploaded sticker file does not exist")
	}
	ext := strings.ToLower(filepath.Ext(originalName))
	if ext != ".png" && ext != ".gif" {
		return result, errors.New("stickers only support PNG or GIF")
	}
	if sourceSize <= 0 || sourceSize > limits.MaxBytes {
		return result, errors.New("sticker file exceeds the configured size limit")
	}
	client, cfg, err := s.ossClient(ctx)
	if err != nil {
		return result, errors.New("sticker storage is unavailable")
	}
	object, err := client.GetObject(ctx, &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(sourceObjectKey)})
	if err != nil {
		return result, errors.New("failed to read uploaded sticker")
	}
	defer object.Body.Close()
	data, err := io.ReadAll(io.LimitReader(object.Body, limits.MaxBytes+1))
	if err != nil || int64(len(data)) != sourceSize {
		return result, errors.New("sticker file size does not match upload")
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != sourceSHA256 {
		return result, errors.New("sticker file hash does not match upload")
	}
	declared := normalizeRasterContentType(sourceContentType)
	if ext == ".png" && declared != "image/png" || ext == ".gif" && declared != "image/gif" {
		return result, errors.New("sticker extension and MIME do not match")
	}
	result.SanitizedData, result.Width, result.Height, err = sanitizeStickerImage(data, declared, limits)
	if err != nil {
		return result, err
	}
	result.ContentType = declared
	result.Size = int64(len(result.SanitizedData))
	sanitizedDigest := sha256.Sum256(result.SanitizedData)
	result.SHA256 = hex.EncodeToString(sanitizedDigest[:])
	return result, nil
}
