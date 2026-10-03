package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
	"github.com/jackc/pgx/v5"
	xwebp "golang.org/x/image/webp"
)

func (s *Server) ossFiles(w http.ResponseWriter, r *http.Request) {
	limit := boundedLimit(r.URL.Query().Get("limit"), 100, 500)
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	prefix := strings.TrimSpace(r.URL.Query().Get("prefix"))
	args := []any{}
	where := []string{"1 = 1"}
	if category != "" {
		args = append(args, category)
		where = append(where, fmt.Sprintf("category = $%d", len(args)))
	}
	if prefix != "" {
		args = append(args, prefix+"%")
		where = append(where, fmt.Sprintf("object_key like $%d", len(args)))
	}
	args = append(args, limit)
	rows, err := s.db.Query(
		r.Context(),
		`select public_id, bucket, endpoint, region, object_key, category, source, original_name, source_original_name, content_type, size_bytes, source_size_bytes, sha256, status, scan_status, created_at, updated_at
		 from oss_files
		 where `+strings.Join(where, " and ")+`
		 order by created_at desc
		 limit $`+strconv.Itoa(len(args)),
		args...,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取 OSS 文件目录失败")
		return
	}
	defer rows.Close()

	files := make([]map[string]any, 0)
	for rows.Next() {
		var size, sourceSize int64
		var id, bucket, endpoint, region, objectKey, category, source, originalName, sourceOriginalName, contentType, sha, status, scanStatus string
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &bucket, &endpoint, &region, &objectKey, &category, &source, &originalName, &sourceOriginalName, &contentType, &size, &sourceSize, &sha, &status, &scanStatus, &createdAt, &updatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "解析 OSS 文件目录失败")
			return
		}
		files = append(files, ossFileRecord(id, bucket, endpoint, region, objectKey, category, source, originalName, sourceOriginalName, contentType, size, sourceSize, sha, status, scanStatus, createdAt, updatedAt))
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "读取 OSS 文件目录失败")
		return
	}
	writeJSON(w, http.StatusOK, files)
}

func (s *Server) updateOSSFileScanStatus(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "无效的 OSS 文件 ID")
		return
	}
	var request struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "无效的扫描状态")
		return
	}
	request.Status = strings.ToLower(strings.TrimSpace(request.Status))
	request.Note = strings.TrimSpace(request.Note)
	if request.Status != "pending" && request.Status != "clean" && request.Status != "rejected" {
		writeError(w, http.StatusBadRequest, "扫描状态必须是 pending、clean 或 rejected")
		return
	}
	if len(request.Note) > 1000 {
		writeError(w, http.StatusBadRequest, "扫描备注过长")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法更新扫描状态")
		return
	}
	defer tx.Rollback(r.Context())
	var internalID int64
	var previousStatus, previousFileStatus string
	err = tx.QueryRow(r.Context(), `select id,scan_status,status from oss_files where public_id=$1 for update`, publicID).
		Scan(&internalID, &previousStatus, &previousFileStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "OSS 文件不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法读取 OSS 文件")
		return
	}
	if previousFileStatus == "deleted" {
		writeError(w, http.StatusConflict, "deleted OSS files cannot be rescanned")
		return
	}
	fileStatus := "quarantined"
	if request.Status == "clean" {
		fileStatus = "active"
	} else if request.Status == "rejected" {
		fileStatus = "quarantined"
	}
	if _, err = tx.Exec(r.Context(), `update oss_files
		set scan_status=$2,status=$3,updated_at=now() where id=$1`, internalID, request.Status, fileStatus); err != nil {
		writeError(w, http.StatusInternalServerError, "无法更新扫描状态")
		return
	}
	if err = synchronizeProjectFilesForOSSScanTx(r.Context(), tx, internalID, currentClaims(r).Subject, request.Status); err != nil {
		writeError(w, http.StatusInternalServerError, "无法同步项目文件发布状态")
		return
	}
	if request.Status == "rejected" {
		if _, err = tx.Exec(r.Context(), `update mirrored_project_files set status='failed'
			where oss_file_id=$1 and status in ('scanning','ready')`, internalID); err != nil {
			writeError(w, http.StatusInternalServerError, "无法同步自动镜像扫描状态")
			return
		}
	} else if request.Status == "pending" {
		if _, err = tx.Exec(r.Context(), `update mirrored_project_files set status='scanning'
			where oss_file_id=$1 and status='ready'`, internalID); err != nil {
			writeError(w, http.StatusInternalServerError, "无法同步自动镜像重新扫描状态")
			return
		}
	}
	if _, err = tx.Exec(r.Context(), `update report_evidence set
		scan_status=$2,
		status=case when $2='rejected' then 'pending_delete' else status end,
		cleanup_after=case when $2='rejected' then now() else cleanup_after end,
		last_error=case when $2='rejected' then $3 else '' end
		where object_key=(select object_key from oss_files where id=$1) and status<>'deleted'`,
		internalID, request.Status, request.Note); err != nil {
		writeError(w, http.StatusInternalServerError, "无法同步举报附件扫描状态")
		return
	}
	metadata, _ := json.Marshal(map[string]any{
		"previousStatus":     previousStatus,
		"previousFileStatus": previousFileStatus,
		"status":             request.Status,
		"fileStatus":         fileStatus,
		"note":               request.Note,
	})
	if _, err = tx.Exec(r.Context(), `insert into audit_events(
		aggregate_type,aggregate_key,actor_id,action,ip,user_agent,metadata)
		values('oss_file',$1,$2,'scan_status_update',$3,$4,$5::jsonb)`,
		publicID, currentClaims(r).Subject, s.requestClientLocation(r).IP, r.UserAgent(), metadata); err != nil {
		writeError(w, http.StatusInternalServerError, "无法记录扫描状态变更")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "无法提交扫描状态变更")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": publicID, "scanStatus": request.Status, "status": fileStatus})
}

func (s *Server) resolveActiveOSSFileInternalIDForUploader(ctx context.Context, publicID string, uploaderID int64) (int64, error) {
	var internalID int64
	err := s.db.QueryRow(ctx, `select id from oss_files
		where public_id=$1 and uploader_id=$2 and status='active'`, publicID, uploaderID).Scan(&internalID)
	return internalID, err
}

func requiresSynchronousCatalogImageValidation(source string) bool {
	source = strings.ToLower(strings.TrimSpace(source))
	return strings.HasPrefix(source, "mod_resource:") ||
		strings.HasPrefix(source, "recipe_gui:") ||
		strings.HasPrefix(source, "catalog_resource:") ||
		strings.HasPrefix(source, "catalog_recipe:") ||
		strings.HasPrefix(source, "blueprint_cover:")
}

const maximumSynchronousRasterBytes = int64(16 << 20)

func readOSSUploadedRaster(ctx context.Context, client *aliyunoss.Client, cfg ossConfigPayload, objectKey string, expectedSize int64, expectedSHA256 string) ([]byte, error) {
	if expectedSize <= 0 || expectedSize > maximumSynchronousRasterBytes {
		return nil, errors.New("resource image exceeds the upload limit")
	}
	result, err := client.GetObject(ctx, &aliyunoss.GetObjectRequest{
		Bucket: aliyunoss.Ptr(cfg.Bucket),
		Key:    aliyunoss.Ptr(objectKey),
	})
	if err != nil {
		return nil, fmt.Errorf("read uploaded image: %w", err)
	}
	defer result.Body.Close()
	data, err := io.ReadAll(io.LimitReader(result.Body, maximumSynchronousRasterBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read uploaded image body: %w", err)
	}
	if int64(len(data)) != expectedSize || int64(len(data)) > maximumSynchronousRasterBytes {
		return nil, errors.New("uploaded image size mismatch")
	}
	if expectedSHA256 != "" {
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != expectedSHA256 {
			return nil, errors.New("uploaded image hash mismatch")
		}
	}
	return data, nil
}

func validateOSSUploadedRaster(ctx context.Context, client *aliyunoss.Client, cfg ossConfigPayload, objectKey, declaredContentType string, expectedSize int64, expectedSHA256, source string) error {
	data, err := readOSSUploadedRaster(ctx, client, cfg, objectKey, expectedSize, expectedSHA256)
	if err != nil {
		return err
	}
	if _, err = validateRasterImageBytes(data, declaredContentType); err != nil {
		return err
	}
	return validateModResourceImageSpec(data, declaredContentType, source)
}

const maxModResourceRenderEdge = 1024

func validateModResourceImageSpec(data []byte, declaredContentType, source string) error {
	source = strings.ToLower(strings.TrimSpace(source))
	expectedWidth, expectedHeight := 0, 0
	renderImage := false
	switch {
	case strings.HasSuffix(source, ":icon_32"):
		expectedWidth, expectedHeight = 32, 32
	case strings.HasSuffix(source, ":icon_128"):
		expectedWidth, expectedHeight = 128, 128
	case isModResourceRenderUploadSource(source):
		renderImage = true
	default:
		return nil
	}
	config, err := validateModResourcePNGConfig(data, declaredContentType)
	if err != nil {
		return err
	}
	if renderImage {
		if max(config.Width, config.Height) > maxModResourceRenderEdge {
			return fmt.Errorf("mod resource rendered image longest edge must not exceed %dpx", maxModResourceRenderEdge)
		}
		return nil
	}
	if config.Width != expectedWidth || config.Height != expectedHeight {
		return fmt.Errorf("mod resource image must be %dx%d PNG", expectedWidth, expectedHeight)
	}
	return nil
}

func validateModResourcePNGConfig(data []byte, declaredContentType string) (image.Config, error) {
	if normalizeRasterContentType(declaredContentType) != "image/png" {
		return image.Config{}, errors.New("mod resource images must use PNG")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "png" || config.Width <= 0 || config.Height <= 0 {
		return image.Config{}, errors.New("mod resource image must be a valid PNG")
	}
	// PNG color types 4 and 6 have an alpha channel. Indexed PNG may carry
	// transparency through a validated tRNS chunk.
	if !pngSupportsTransparency(data) {
		return image.Config{}, errors.New("mod resource PNG must support a transparent background")
	}
	return config, nil
}

func pngSupportsTransparency(data []byte) bool {
	if len(data) < 33 || !bytes.Equal(data[:8], []byte("\x89PNG\r\n\x1a\n")) {
		return false
	}
	colorType := data[25]
	transparent := colorType == 4 || colorType == 6
	seenImageData, paletteSize := false, 0
	for offset := 8; offset < len(data); {
		if len(data)-offset < 12 {
			return false
		}
		length := uint64(binary.BigEndian.Uint32(data[offset : offset+4]))
		end := uint64(offset) + 12 + length
		if end > uint64(len(data)) {
			return false
		}
		kind := string(data[offset+4 : offset+8])
		payload := data[offset+8 : int(end)-4]
		if crc32.ChecksumIEEE(data[offset+4:int(end)-4]) != binary.BigEndian.Uint32(data[int(end)-4:int(end)]) {
			return false
		}
		switch kind {
		case "PLTE":
			paletteSize = len(payload) / 3
		case "IDAT":
			seenImageData = true
		case "tRNS":
			if seenImageData {
				return false
			}
			switch colorType {
			case 0:
				transparent = len(payload) == 2
			case 2:
				transparent = len(payload) == 6
			case 3:
				transparent = len(payload) > 0 && len(payload) <= paletteSize
			default:
				return false
			}
			if !transparent {
				return false
			}
		case "IEND":
			return transparent && seenImageData && len(payload) == 0 && int(end) == len(data)
		}
		offset = int(end)
	}
	return false
}

func webPDimensions(data []byte) (int, int, bool) {
	if len(data) < 20 || !bytes.Equal(data[:4], []byte("RIFF")) || !bytes.Equal(data[8:12], []byte("WEBP")) {
		return 0, 0, false
	}
	// RIFF size is the number of bytes after the first eight bytes. Requiring
	// an exact match rejects both truncated files and data hidden after the
	// declared container.
	if uint64(binary.LittleEndian.Uint32(data[4:8]))+8 != uint64(len(data)) {
		return 0, 0, false
	}
	width, height := 0, 0
	canvasWidth, canvasHeight := 0, 0
	foundCanvasHeader := false
	foundImageData := false
	for offset := 12; offset < len(data); {
		if len(data)-offset < 8 {
			return 0, 0, false
		}
		chunkType := string(data[offset : offset+4])
		chunkSize := uint64(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		payloadStart := uint64(offset + 8)
		payloadEnd := payloadStart + chunkSize
		paddedEnd := payloadEnd + chunkSize%2
		if payloadEnd < payloadStart || paddedEnd > uint64(len(data)) {
			return 0, 0, false
		}
		if chunkSize%2 != 0 && data[int(payloadEnd)] != 0 {
			return 0, 0, false
		}
		payload := data[int(payloadStart):int(payloadEnd)]
		switch chunkType {
		case "VP8X":
			// VP8X only describes the extended canvas. It is not image data
			// by itself and must be followed by a complete VP8/VP8L payload.
			if foundCanvasHeader || foundImageData || len(payload) != 10 {
				return 0, 0, false
			}
			canvasWidth = 1 + int(payload[4]) + int(payload[5])<<8 + int(payload[6])<<16
			canvasHeight = 1 + int(payload[7]) + int(payload[8])<<8 + int(payload[9])<<16
			foundCanvasHeader = true
		case "VP8L":
			if foundImageData || len(payload) < 10 || payload[0] != 0x2f {
				return 0, 0, false
			}
			width = 1 + ((int(payload[1]) | int(payload[2])<<8) & 0x3fff)
			height = 1 + ((int(payload[2])>>6 | int(payload[3])<<2 | int(payload[4])<<10) & 0x3fff)
			foundImageData = true
		case "VP8 ":
			if foundImageData || len(payload) < 11 || !bytes.Equal(payload[3:6], []byte{0x9d, 0x01, 0x2a}) {
				return 0, 0, false
			}
			frameTag := uint32(payload[0]) | uint32(payload[1])<<8 | uint32(payload[2])<<16
			firstPartitionSize := int(frameTag >> 5)
			if frameTag&1 != 0 || (frameTag>>1)&7 > 3 || (frameTag>>4)&1 == 0 ||
				firstPartitionSize <= 0 || 10+firstPartitionSize > len(payload) {
				return 0, 0, false
			}
			width = (int(payload[6]) | int(payload[7])<<8) & 0x3fff
			height = (int(payload[8]) | int(payload[9])<<8) & 0x3fff
			foundImageData = true
		case "ANIM", "ANMF":
			// Animated WebP needs frame-by-frame compressed-stream validation
			// that the standard library does not provide. Reject it instead of
			// treating a syntactically complete RIFF as a decoded image.
			return 0, 0, false
		}
		offset = int(paddedEnd)
	}
	if !foundImageData || width <= 0 || height <= 0 {
		return 0, 0, false
	}
	if foundCanvasHeader {
		if canvasWidth <= 0 || canvasHeight <= 0 || canvasWidth != width || canvasHeight != height {
			return 0, 0, false
		}
		width, height = canvasWidth, canvasHeight
	}
	return width, height, true
}

const (
	maxValidatedRasterPixels   = int64(16_777_216)
	maxValidatedGIFFrames      = 120
	maxValidatedGIFTotalPixels = int64(64_000_000)
	maxValidatedGIFDuration    = 30 * time.Second
	gifGraphicControlExtension = 0xf9
	gifExtensionIntroducer     = 0x21
	gifImageDescriptor         = 0x2c
	gifTrailer                 = 0x3b
)

type gifAnimationFacts struct {
	width, height  int
	frames         int
	totalPixels    int64
	durationIn10MS int64
}

func decodeGIFAnimationWithBudget(data []byte, maxFrames int, maxTotalPixels int64, maxDuration time.Duration) (*gif.GIF, error) {
	facts, err := inspectGIFAnimationBudget(data, maxFrames, maxTotalPixels, maxDuration)
	if err != nil {
		return nil, err
	}
	animation, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil || len(animation.Image) != facts.frames || len(animation.Delay) != facts.frames ||
		animation.Config.Width != facts.width || animation.Config.Height != facts.height {
		return nil, errors.New("GIF animation is invalid or truncated")
	}
	var decodedPixels, decodedDuration int64
	for index, frame := range animation.Image {
		bounds := frame.Bounds()
		if bounds.Dx() <= 0 || bounds.Dy() <= 0 || bounds.Min.X < 0 || bounds.Min.Y < 0 ||
			bounds.Max.X > facts.width || bounds.Max.Y > facts.height {
			return nil, errors.New("GIF frame bounds are invalid")
		}
		pixels := int64(bounds.Dx()) * int64(bounds.Dy())
		if pixels <= 0 || decodedPixels > maxTotalPixels-pixels {
			return nil, errors.New("GIF decoded pixel budget exceeded")
		}
		decodedPixels += pixels
		if animation.Delay[index] < 0 || decodedDuration > int64(maxDuration/(10*time.Millisecond))-int64(animation.Delay[index]) {
			return nil, errors.New("GIF animation duration exceeded")
		}
		decodedDuration += int64(animation.Delay[index])
	}
	if decodedPixels != facts.totalPixels || decodedDuration > facts.durationIn10MS {
		return nil, errors.New("GIF animation metadata is inconsistent")
	}
	return animation, nil
}

func inspectGIFAnimationBudget(data []byte, maxFrames int, maxTotalPixels int64, maxDuration time.Duration) (gifAnimationFacts, error) {
	var facts gifAnimationFacts
	if maxFrames <= 0 || maxTotalPixels <= 0 || maxDuration <= 0 || len(data) < 13 ||
		(!bytes.Equal(data[:6], []byte("GIF87a")) && !bytes.Equal(data[:6], []byte("GIF89a"))) {
		return facts, errors.New("GIF header or safety budget is invalid")
	}
	facts.width = int(binary.LittleEndian.Uint16(data[6:8]))
	facts.height = int(binary.LittleEndian.Uint16(data[8:10]))
	if facts.width <= 0 || facts.height <= 0 {
		return facts, errors.New("GIF logical screen is invalid")
	}
	offset := 13
	if data[10]&0x80 != 0 {
		colorTableBytes := 3 * (1 << ((data[10] & 0x07) + 1))
		if colorTableBytes > len(data)-offset {
			return facts, errors.New("GIF global color table is truncated")
		}
		offset += colorTableBytes
	}
	maxDurationIn10MS := int64(maxDuration / (10 * time.Millisecond))
	for offset < len(data) {
		blockType := data[offset]
		offset++
		switch blockType {
		case gifTrailer:
			if offset != len(data) || facts.frames == 0 {
				return facts, errors.New("GIF trailer or frame count is invalid")
			}
			return facts, nil
		case gifExtensionIntroducer:
			if offset >= len(data) {
				return facts, errors.New("GIF extension is truncated")
			}
			label := data[offset]
			offset++
			if label == gifGraphicControlExtension {
				if len(data)-offset < 6 || data[offset] != 4 || data[offset+5] != 0 {
					return facts, errors.New("GIF graphic control extension is invalid")
				}
				delay := int64(binary.LittleEndian.Uint16(data[offset+2 : offset+4]))
				if delay > maxDurationIn10MS-facts.durationIn10MS {
					return facts, errors.New("GIF animation duration exceeded")
				}
				facts.durationIn10MS += delay
				offset += 6
				continue
			}
			var skipErr error
			offset, skipErr = skipGIFDataSubBlocks(data, offset)
			if skipErr != nil {
				return facts, skipErr
			}
		case gifImageDescriptor:
			if len(data)-offset < 9 {
				return facts, errors.New("GIF image descriptor is truncated")
			}
			left := int(binary.LittleEndian.Uint16(data[offset : offset+2]))
			top := int(binary.LittleEndian.Uint16(data[offset+2 : offset+4]))
			width := int(binary.LittleEndian.Uint16(data[offset+4 : offset+6]))
			height := int(binary.LittleEndian.Uint16(data[offset+6 : offset+8]))
			packed := data[offset+8]
			offset += 9
			if width <= 0 || height <= 0 || left > facts.width-width || top > facts.height-height {
				return facts, errors.New("GIF frame dimensions are invalid")
			}
			pixels := int64(width) * int64(height)
			if facts.frames >= maxFrames || pixels <= 0 || facts.totalPixels > maxTotalPixels-pixels {
				return facts, errors.New("GIF frame or decoded pixel budget exceeded")
			}
			facts.frames++
			facts.totalPixels += pixels
			if packed&0x80 != 0 {
				colorTableBytes := 3 * (1 << ((packed & 0x07) + 1))
				if colorTableBytes > len(data)-offset {
					return facts, errors.New("GIF local color table is truncated")
				}
				offset += colorTableBytes
			}
			if offset >= len(data) || data[offset] < 2 || data[offset] > 8 {
				return facts, errors.New("GIF LZW code size is invalid")
			}
			offset++
			var skipErr error
			offset, skipErr = skipGIFDataSubBlocks(data, offset)
			if skipErr != nil {
				return facts, skipErr
			}
		default:
			return facts, errors.New("GIF contains an unknown block")
		}
	}
	return facts, errors.New("GIF is missing its trailer")
}

func skipGIFDataSubBlocks(data []byte, offset int) (int, error) {
	for {
		if offset >= len(data) {
			return offset, errors.New("GIF data sub-block is truncated")
		}
		size := int(data[offset])
		offset++
		if size == 0 {
			return offset, nil
		}
		if size > len(data)-offset {
			return offset, errors.New("GIF data sub-block exceeds the file")
		}
		offset += size
	}
}

func validateRasterImageBytes(data []byte, declaredContentType string) (string, error) {
	if len(data) == 0 {
		return "", errors.New("empty raster image")
	}
	declared := normalizeRasterContentType(declaredContentType)
	detected := normalizeRasterContentType(http.DetectContentType(data))
	if !supportedRasterContentType(declared) {
		return "", errors.New("unsupported resource image content type")
	}
	if detected != declared {
		return "", fmt.Errorf("resource image type mismatch: declared %s, detected %s", declared, detected)
	}
	if declared == "image/webp" {
		width, height, ok := webPDimensions(data)
		if !ok || int64(width)*int64(height) > maxValidatedRasterPixels {
			return "", errors.New("invalid or oversized WebP image")
		}
		config, err := xwebp.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width != width || config.Height != height {
			return "", errors.New("invalid WebP image bitstream")
		}
		if _, err = xwebp.Decode(bytes.NewReader(data)); err != nil {
			return "", errors.New("invalid or truncated WebP image")
		}
		return declared, nil
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || normalizeRasterContentType("image/"+format) != declared || config.Width <= 0 || config.Height <= 0 {
		return "", errors.New("invalid raster image")
	}
	if int64(config.Width)*int64(config.Height) > maxValidatedRasterPixels {
		return "", errors.New("resource image dimensions are too large")
	}
	switch declared {
	case "image/png":
		_, err = png.Decode(bytes.NewReader(data))
	case "image/jpeg":
		_, err = jpeg.Decode(bytes.NewReader(data))
	case "image/gif":
		_, err = decodeGIFAnimationWithBudget(data, maxValidatedGIFFrames, maxValidatedGIFTotalPixels, maxValidatedGIFDuration)
	}
	if err != nil {
		return "", errors.New("invalid or truncated raster image")
	}
	return declared, nil
}

func normalizeRasterContentType(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
	switch value {
	case "image/apng":
		return "image/png"
	case "image/jpg":
		return "image/jpeg"
	default:
		return value
	}
}

func supportedRasterContentType(value string) bool {
	switch normalizeRasterContentType(value) {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

type ossFileHashLookup struct {
	UploaderID    *int64
	Category      string
	Source        string
	ScanStatuses  []string
	RequireRaster bool
	// RequireTrusted prevents an unscanned upload from becoming a public
	// catalog image merely because its bytes match a later request.
	RequireTrusted bool
}

func (s *Server) findExistingOSSFileByHashExact(ctx context.Context, sha256 string, sizeBytes int64, lookup ossFileHashLookup) (map[string]any, bool, error) {
	if sha256 == "" || sizeBytes <= 0 || len(lookup.ScanStatuses) == 0 {
		return nil, false, nil
	}
	var size, sourceSize int64
	var id, bucket, endpoint, region, objectKey, category, source, originalName, sourceOriginalName, contentType, sha, status, scanStatus string
	var createdAt, updatedAt time.Time
	query := `select public_id, bucket, endpoint, region, object_key, category, source, original_name, source_original_name, content_type, size_bytes, source_size_bytes, sha256, status, scan_status, created_at, updated_at
		from oss_files
		where sha256 = $1 and coalesce(nullif(source_size_bytes, 0), size_bytes) = $2 and status = 'active'
		  and scan_status = any($3::text[])`
	args := []any{sha256, sizeBytes, lookup.ScanStatuses}
	if lookup.UploaderID != nil {
		args = append(args, *lookup.UploaderID)
		query += fmt.Sprintf(` and uploader_id = $%d`, len(args))
	}
	if lookup.Category != "" {
		args = append(args, lookup.Category)
		query += fmt.Sprintf(` and category = $%d`, len(args))
	}
	if lookup.Source != "" {
		args = append(args, lookup.Source)
		query += fmt.Sprintf(` and source = $%d`, len(args))
	}
	if lookup.RequireTrusted {
		query += ` and scan_status in ('clean','trusted_generated')`
	}
	if lookup.RequireRaster {
		query += ` and lower(split_part(content_type,';',1)) in ('image/png','image/jpeg','image/jpg','image/gif','image/webp')`
	}
	query += ` order by created_at asc limit 1`
	err := s.db.QueryRow(ctx, query, args...).Scan(&id, &bucket, &endpoint, &region, &objectKey, &category, &source, &originalName, &sourceOriginalName, &contentType, &size, &sourceSize, &sha, &status, &scanStatus, &createdAt, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("find existing OSS file by hash: %w", err)
	}
	return ossFileRecord(id, bucket, endpoint, region, objectKey, category, source, originalName, sourceOriginalName, contentType, size, sourceSize, sha, status, scanStatus, createdAt, updatedAt), true, nil
}

func ossFileRecord(id string, bucket string, endpoint string, region string, objectKey string, category string, source string, originalName string, sourceOriginalName string, contentType string, size int64, sourceSize int64, sha string, status string, scanStatus string, createdAt time.Time, updatedAt time.Time) map[string]any {
	if sourceOriginalName == "" {
		sourceOriginalName = originalName
	}
	if sourceSize <= 0 {
		sourceSize = size
	}
	sourceExtension := strings.ToLower(filepath.Ext(sourceOriginalName))
	converted := (contentType == "image/webp" && (sourceExtension == ".jpg" || sourceExtension == ".jpeg" || sourceExtension == ".png")) ||
		sourceSize != size || strings.HasSuffix(objectKey, ".render-1024.png")
	return map[string]any{
		"id":                 id,
		"bucket":             bucket,
		"endpoint":           endpoint,
		"region":             region,
		"objectKey":          objectKey,
		"category":           category,
		"source":             source,
		"originalName":       originalName,
		"sourceOriginalName": sourceOriginalName,
		"contentType":        contentType,
		"sizeBytes":          size,
		"sourceSizeBytes":    sourceSize,
		"converted":          converted,
		"sha256":             sha,
		"status":             status,
		"scanStatus":         scanStatus,
		"createdAt":          createdAt,
		"updatedAt":          updatedAt,
	}
}

func (s *Server) enforceUserFileUploadLimits(r *http.Request, sizeBytes int64, deferStoredSizeCheck bool) error {
	limits, err := ossUserQuotaLimitsForRequest(r)
	if err != nil {
		return err
	}
	if err = limits.validateSingle(sizeBytes); err != nil {
		return err
	}
	total, daily, err := s.loadOSSUserQuotaAdmissionSnapshots(r.Context(), currentClaims(r).Subject)
	if err != nil {
		return err
	}
	if !quotaAllows(daily.activeSource, daily.reservedSource, sizeBytes, limits.daily) {
		return newOSSUserQuotaError(fmt.Sprintf("超过每日上传额度：%s", formatLimitBytes(limits.daily)))
	}
	storedBytes := sizeBytes
	if deferStoredSizeCheck {
		storedBytes = 0
	}
	if !quotaAllows(total.activeStored, total.reservedStored, storedBytes, limits.total) {
		return newOSSUserQuotaError(fmt.Sprintf("超过用户文件总容量：%s", formatLimitBytes(limits.total)))
	}
	return nil
}

func (s *Server) enforceUserStoredFileLimit(r *http.Request, sizeBytes int64) error {
	limits, err := ossUserQuotaLimitsForRequest(r)
	if err != nil {
		return err
	}
	total, _, err := s.loadOSSUserQuotaAdmissionSnapshots(r.Context(), currentClaims(r).Subject)
	if err != nil {
		return err
	}
	if !quotaAllows(total.activeStored, total.reservedStored, sizeBytes, limits.total) {
		return newOSSUserQuotaError(fmt.Sprintf("超过用户文件总容量：%s", formatLimitBytes(limits.total)))
	}
	return nil
}

func (s *Server) presignOSSFile(w http.ResponseWriter, r *http.Request) {
	var req ossPresignRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	s.presignOSSFileWithRequest(w, r, req)
}

func (s *Server) presignOSSFileWithRequest(w http.ResponseWriter, r *http.Request, req ossPresignRequest) bool {
	req.ObjectKey = strings.TrimSpace(req.ObjectKey)
	if req.ObjectKey == "" {
		writeError(w, http.StatusBadRequest, "missing OSS ObjectKey")
		return false
	}
	payload, err := s.prepareOSSFileDownload(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to generate OSS download URL")
		return false
	}
	s.recordOSSDownloadStat(r.Context(), req.ObjectKey)
	writeJSON(w, http.StatusOK, payload)
	return true
}

// Preparing a URL must not write a response or counters. Business handlers can
// commit their authoritative download facts before disclosing the signed URL.
func (s *Server) prepareOSSFileDownload(ctx context.Context, req ossPresignRequest) (map[string]any, error) {
	cfg := s.ossConfigFromSettings(ctx)
	return s.prepareOSSFileDownloadWithConfig(ctx, cfg, req, s.originalNameForOSSObject(ctx, req.ObjectKey))
}

func (s *Server) prepareOSSFileDownloadWithConfig(ctx context.Context, cfg ossConfigPayload, req ossPresignRequest, originalName string) (map[string]any, error) {
	if req.ExpiresMinutes <= 0 {
		req.ExpiresMinutes = cfg.DownloadURLTTLMinutes
	}
	if req.ExpiresMinutes > maxOSSDownloadURLTTLMinutes {
		req.ExpiresMinutes = maxOSSDownloadURLTTLMinutes
	}
	expires := time.Duration(req.ExpiresMinutes) * time.Minute
	contentDisposition := downloadContentDisposition(originalName)
	access, err := s.resolveOSSObjectAccessWithConfig(ctx, cfg, req.ObjectKey, ossObjectAccessOptions{
		Expires:            expires,
		ContentDisposition: contentDisposition,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"url":             access.URL,
		"expiresAt":       access.ExpiresAt,
		"downloadUrlMode": access.Mode,
		"filename":        originalName,
	}, nil
}

func (s *Server) ossUploadLogs(w http.ResponseWriter, r *http.Request) {
	items, err := s.querySimpleRows(r, `select id, file_id, uploader_id, object_key, original_name, size_bytes, ip, user_agent, result, message, created_at from oss_upload_logs order by created_at desc limit $1`, boundedLimit(r.URL.Query().Get("limit"), 100, 500))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read OSS upload logs")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) ossScanLogs(w http.ResponseWriter, r *http.Request) {
	items, err := s.querySimpleRows(r, `select id, file_id, object_key, engine, result, message, payload, created_at from oss_scan_logs order by created_at desc limit $1`, boundedLimit(r.URL.Query().Get("limit"), 100, 500))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read OSS scan logs")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) ossDownloadStats(w http.ResponseWriter, r *http.Request) {
	items, err := s.querySimpleRows(r, `select object_key, downloads, total_bytes, last_download_at from oss_download_stats order by downloads desc, last_download_at desc nulls last limit $1`, boundedLimit(r.URL.Query().Get("limit"), 100, 500))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read OSS download statistics")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) ossClient(ctx context.Context) (*aliyunoss.Client, ossConfigPayload, error) {
	cfg := s.ossConfigFromSettings(ctx)
	if !cfg.Enabled {
		return nil, cfg, fmt.Errorf("OSS 尚未启用: %w", errOSSConfigurationUnavailable)
	}
	if cfg.Region == "" || cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
		return nil, cfg, fmt.Errorf("OSS 配置不完整: %w", errOSSConfigurationUnavailable)
	}
	if requiresSecurityToken(cfg.AccessKeyID) && cfg.SecurityToken == "" {
		return nil, cfg, fmt.Errorf("OSS STS 临时凭证缺少 SecurityToken: %w", errOSSConfigurationUnavailable)
	}
	return newOSSClient(cfg, cfg.Endpoint, cfg.UseCName), cfg, nil
}

func (s *Server) ossDownloadClient(ctx context.Context, cfg ossConfigPayload) (*aliyunoss.Client, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("OSS 尚未启用: %w", errOSSConfigurationUnavailable)
	}
	if cfg.Region == "" || cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
		return nil, fmt.Errorf("OSS 配置不完整: %w", errOSSConfigurationUnavailable)
	}
	if requiresSecurityToken(cfg.AccessKeyID) && cfg.SecurityToken == "" {
		return nil, fmt.Errorf("OSS STS 临时凭证缺少 SecurityToken: %w", errOSSConfigurationUnavailable)
	}
	endpoint := cfg.Endpoint
	return newOSSClient(cfg, endpoint, isCustomOSSEndpoint(endpoint)), nil
}

func newOSSClient(cfg ossConfigPayload, endpoint string, useCName bool) *aliyunoss.Client {
	ossCfg := aliyunoss.LoadDefaultConfig().
		WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.AccessKeySecret, cfg.SecurityToken)).
		WithRegion(cfg.Region).
		WithEndpoint(endpoint).
		WithUseCName(useCName || isCustomOSSEndpoint(endpoint))
	return aliyunoss.NewClient(ossCfg)
}

func redactOSSConfig(payload ossConfigPayload) map[string]any {
	return map[string]any{
		"enabled":                 payload.Enabled,
		"region":                  payload.Region,
		"endpoint":                payload.Endpoint,
		"publicEndpoint":          payload.PublicEndpoint,
		"bucket":                  payload.Bucket,
		"accessKeyId":             payload.AccessKeyID,
		"hasAccessKeySecret":      strings.TrimSpace(payload.AccessKeySecret) != "",
		"hasSecurityToken":        strings.TrimSpace(payload.SecurityToken) != "",
		"useCName":                payload.UseCName || isCustomOSSEndpoint(payload.Endpoint),
		"prefix":                  payload.Prefix,
		"downloadUrlTtlMinutes":   payload.DownloadURLTTLMinutes,
		"downloadUrlMode":         payload.DownloadURLMode,
		"allowedExtensions":       payload.AllowedExtensions,
		"bucketAccessPolicy":      "private-read-write",
		"temporaryDownloadPolicy": payload.DownloadURLMode,
	}
}

func normalizeOSSConfig(payload ossConfigPayload) ossConfigPayload {
	payload.Region = strings.TrimSpace(payload.Region)
	payload.Endpoint = normalizeOSSEndpoint(payload.Endpoint)
	payload.PublicEndpoint = normalizeOSSEndpoint(payload.PublicEndpoint)
	payload.Bucket = strings.TrimSpace(payload.Bucket)
	payload.AccessKeyID = strings.TrimSpace(payload.AccessKeyID)
	payload.AccessKeySecret = strings.TrimSpace(payload.AccessKeySecret)
	payload.SecurityToken = strings.TrimSpace(payload.SecurityToken)
	payload.Prefix = normalizeObjectPrefix(payload.Prefix)
	if payload.Prefix == "" {
		payload.Prefix = "mcmods"
	}
	if payload.Region != "" && payload.Endpoint == "" {
		payload.Endpoint = defaultOSSEndpoint(payload.Region)
	}
	if payload.PublicEndpoint == "" && isCustomOSSEndpoint(payload.Endpoint) {
		payload.PublicEndpoint = payload.Endpoint
		payload.Endpoint = defaultOSSEndpoint(payload.Region)
		payload.UseCName = false
	}
	if payload.PublicEndpoint == "" {
		payload.PublicEndpoint = "https://oss.mcmods.cn"
	}
	if !isCustomOSSEndpoint(payload.Endpoint) {
		payload.UseCName = false
	}
	if payload.DownloadURLTTLMinutes <= 0 {
		payload.DownloadURLTTLMinutes = 10
	}
	if payload.DownloadURLTTLMinutes > maxOSSDownloadURLTTLMinutes {
		payload.DownloadURLTTLMinutes = maxOSSDownloadURLTTLMinutes
	}
	payload.DownloadURLMode = normalizeOSSDownloadMode(payload.DownloadURLMode)
	return payload
}

func (payload ossConfigPayload) displayEndpoint() string {
	if payload.PublicEndpoint != "" {
		return payload.PublicEndpoint
	}
	return payload.Endpoint
}

func defaultOSSConfig() ossConfigPayload {
	return ossConfigPayload{
		Prefix:                "mcmods",
		PublicEndpoint:        "https://oss.mcmods.cn",
		DownloadURLTTLMinutes: 10,
		DownloadURLMode:       ossDownloadModePresigned,
		AllowedExtensions:     append([]string(nil), defaultOSSAllowedExtensions...),
	}
}

func normalizeOSSDownloadMode(string) string {
	// Legacy esa_private_origin settings are deliberately migrated at read time.
	// A stable edge URL cannot enforce the expiry claimed by this API.
	return ossDownloadModePresigned
}

func (s *Server) originalNameForOSSObject(ctx context.Context, objectKey string) string {
	var originalName string
	err := s.db.QueryRow(ctx, `select original_name from oss_files where object_key = $1`, objectKey).Scan(&originalName)
	if err != nil || strings.TrimSpace(originalName) == "" {
		return path.Base(objectKey)
	}
	return originalName
}

func downloadContentDisposition(filename string) string {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		filename = "download"
	}
	asciiFallback := strings.Map(func(r rune) rune {
		if r < 0x20 || r == '"' || r == '\\' || r == ';' {
			return '-'
		}
		if r > 0x7e {
			return '-'
		}
		return r
	}, filename)
	asciiFallback = strings.TrimSpace(asciiFallback)
	if asciiFallback == "" {
		asciiFallback = "download"
	}
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, asciiFallback, url.PathEscape(filename))
}

func (s *Server) insertOSSUploadLog(ctx context.Context, fileID *int64, uploaderID int64, objectKey string, originalName string, sizeBytes int64, ip string, userAgent string, result string, message string) {
	_, err := s.db.Exec(
		ctx,
		`insert into oss_upload_logs (file_id, uploader_id, object_key, original_name, size_bytes, ip, user_agent, result, message)
		 values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		fileID,
		uploaderID,
		objectKey,
		originalName,
		sizeBytes,
		ip,
		userAgent,
		result,
		message,
	)
	if err != nil {
		s.observeOSSWriteFailure("upload_log", objectKey, err)
	}
}

func (s *Server) resolveUserOSSUploadCategory(r *http.Request, category, source string) (string, error) {
	claims := currentClaims(r)
	normalizedSource := strings.ToLower(strings.TrimSpace(source))
	if siteID, contentID, ok := parseModTextUploadSource(normalizedSource); ok {
		identity, err := s.modIdentity(r.Context(), siteID)
		if err != nil || !canEditMod(claims, identity) {
			return "", errors.New("没有权限向该模组资料正文目录上传文件")
		}
		return ossProjectTextCategory("mod", identity.UniqueID, contentID, "content"), nil
	}
	if strings.HasPrefix(normalizedSource, "mod_text:") {
		return "", errors.New("模组资料正文上传目标不完整")
	}
	if strings.HasPrefix(normalizedSource, "mod_resource:") {
		parts := strings.SplitN(strings.TrimPrefix(normalizedSource, "mod_resource:"), ":", 4)
		if len(parts) < 3 {
			return "", errors.New("模组资料图片上传目标不完整")
		}
		siteID := strings.TrimSpace(parts[0])
		resourceKind := normalizeObjectSegment(parts[1])
		assetKind := normalizeObjectSegment(parts[2])
		if resourceKind == "" {
			resourceKind = "resource"
		}
		if assetKind != "icon" && assetKind != "render" {
			return "", errors.New("模组资料图片类型不正确")
		}
		identity, err := s.modIdentity(r.Context(), siteID)
		if err != nil || !canEditMod(claims, identity) {
			return "", errors.New("没有权限向该模组资料目录上传文件")
		}
		return ossProjectCategory("mod", identity.UniqueID, "icons", resourceKind, "original"), nil
	}
	if strings.HasPrefix(normalizedSource, "recipe_gui:") {
		parts := strings.Split(strings.TrimPrefix(normalizedSource, "recipe_gui:"), ":")
		recipeTypePublicID := normalizeObjectSegment(parts[0])
		templatePublicID := "staging"
		if len(parts) > 1 && normalizeObjectSegment(parts[1]) != "" {
			templatePublicID = normalizeObjectSegment(parts[1])
		}
		var exists bool
		if recipeTypePublicID == "" || !claimsAllow(claims, "content.write") ||
			s.db.QueryRow(r.Context(), `select exists(select 1 from catalog_entities where public_id=$1 and entity_type='recipe_type' and status='active')`, recipeTypePublicID).Scan(&exists) != nil || !exists {
			return "", errors.New("没有权限向该配方模板目录上传文件")
		}
		return ossProjectCategory("catalog", "_shared", "recipe-gui", recipeTypePublicID, templatePublicID), nil
	}
	if strings.HasPrefix(normalizedSource, "creator_avatar:") {
		parts := strings.SplitN(strings.TrimPrefix(normalizedSource, "creator_avatar:"), ":", 2)
		if len(parts) != 2 {
			return "", errors.New("作者头像上传目标不完整")
		}
		kind, publicID := normalizeObjectSegment(parts[0]), normalizeObjectSegment(parts[1])
		var storedKind string
		if s.db.QueryRow(r.Context(), `select kind from creators where public_id=$1 and review_status='approved'`, publicID).
			Scan(&storedKind) != nil {
			return "", errors.New("作者或团队不存在")
		}
		canEdit := claimsAllow(claims, "admin.*") || claimsAllow(claims, "creator.edit") ||
			claimsAllow(claims, "creator.edit."+publicID)
		if !canEdit || kind != normalizeObjectSegment(storedKind) {
			return "", errors.New("没有权限向该作者或团队目录上传文件")
		}
		return ossProjectCategory(kind, publicID, "icons", "avatar", "original"), nil
	}
	for _, candidate := range []struct {
		prefix      string
		destination string
		projectType string
	}{
		{prefix: "mod_gallery:", destination: "gallery", projectType: "mod"},
		{prefix: "modpack_gallery:", destination: "gallery", projectType: "modpack"},
		{prefix: "iconexport:", destination: "iconexporter", projectType: "mod"},
	} {
		if !strings.HasPrefix(normalizedSource, candidate.prefix) {
			continue
		}
		remainder := strings.TrimSpace(strings.TrimPrefix(normalizedSource, candidate.prefix))
		siteID := strings.TrimSpace(strings.SplitN(remainder, ":", 2)[0])
		if siteID == "" || siteID == "draft" {
			return ossUserCategory(claims.Subject, normalizeOSSUserFileScope(category, source)), nil
		}
		if candidate.projectType == "modpack" {
			pack, err := s.modpackBySiteID(r.Context(), normalizeModSiteID(siteID), claims.Subject, true)
			if err != nil {
				return ossUserCategory(claims.Subject, normalizeOSSUserFileScope(category, source)), nil
			}
			if !canEditModpack(claims, pack) {
				return "", errors.New("没有权限向该整合包目录上传文件")
			}
			return ossProjectTextCategory("modpack", pack.PublicID, pack.PublicID, "gallery"), nil
		}
		identity, err := s.modIdentity(r.Context(), siteID)
		if err != nil && candidate.destination == "gallery" {
			return ossUserCategory(claims.Subject, normalizeOSSUserFileScope(category, source)), nil
		}
		if err != nil || !canEditMod(claims, identity) {
			return "", errors.New("没有权限向该模组目录上传文件")
		}
		if candidate.destination == "gallery" {
			return ossProjectTextCategory("mod", identity.UniqueID, identity.UniqueID, "gallery"), nil
		}
		return ossModImportCategory(identity.UniqueID, candidate.destination, "catalog"), nil
	}
	// Tickets expose the canonical user category and the frontend returns it
	// unchanged on complete/abort. Unwrap only this authenticated user's exact
	// single-scope prefix; project source permissions above remain authoritative.
	userCategoryPrefix := path.Join(ossUserDirectory, strconv.FormatInt(claims.Subject, 10), "files") + "/"
	if scope, ok := strings.CutPrefix(strings.TrimSpace(category), userCategoryPrefix); ok {
		if scope == "" || scope != normalizeObjectSegment(scope) {
			return "", errors.New("用户文件目录不正确")
		}
		category = scope
	}
	return ossUserCategory(claims.Subject, normalizeOSSUserFileScope(category, source)), nil
}

func parseModTextUploadSource(source string) (siteID string, contentID string, ok bool) {
	source = strings.ToLower(strings.TrimSpace(source))
	if !strings.HasPrefix(source, "mod_text:") {
		return "", "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(source, "mod_text:"), ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	siteID = strings.TrimSpace(parts[0])
	contentID = normalizeObjectSegment(parts[1])
	if siteID == "" || contentID == "" {
		return "", "", false
	}
	return siteID, contentID, true
}

func ossProjectDownloadScope(projectType, projectID string) string {
	return ossProjectDownloadScopePrefix + normalizeProjectFileType(projectType) + ":" + normalizeProjectObjectSegment(projectID)
}

func parseOSSProjectDownloadScope(scope string) (string, string, bool) {
	if !strings.HasPrefix(scope, ossProjectDownloadScopePrefix) {
		return "", "", false
	}
	value := strings.TrimPrefix(scope, ossProjectDownloadScopePrefix)
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	projectType := normalizeProjectFileType(parts[0])
	projectID := normalizeProjectObjectSegment(parts[1])
	return projectType, projectID, projectType != "" && validCatalogPublicID(projectID)
}

type persistedWebPObject struct {
	ObjectKey    string
	OriginalName string
	SizeBytes    int64
}

type persistedModResourceRenderObject struct {
	ObjectKey string
	SizeBytes int64
}

func (s *Server) persistOversizedModResourceRender(
	ctx context.Context,
	client *aliyunoss.Client,
	cfg ossConfigPayload,
	sourceObjectKey string,
	contentType string,
	sourceSize int64,
	sourceSHA256 string,
) (persistedModResourceRenderObject, bool, error) {
	data, err := readOSSUploadedRaster(ctx, client, cfg, sourceObjectKey, sourceSize, sourceSHA256)
	if err != nil {
		return persistedModResourceRenderObject{}, false, err
	}
	config, err := validateModResourcePNGConfig(data, contentType)
	if err != nil {
		return persistedModResourceRenderObject{}, false, err
	}
	if max(config.Width, config.Height) <= maxModResourceRenderEdge {
		return persistedModResourceRenderObject{}, false, nil
	}

	destinationObjectKey := persistedModResourceRenderObjectKey(sourceObjectKey)
	encodedDestination := base64.RawURLEncoding.EncodeToString([]byte(destinationObjectKey))
	result, err := client.ProcessObject(ctx, &aliyunoss.ProcessObjectRequest{
		Bucket: aliyunoss.Ptr(cfg.Bucket),
		Key:    aliyunoss.Ptr(sourceObjectKey),
		Process: aliyunoss.Ptr(
			fmt.Sprintf("image/resize,l_%d,limit_1|image/format,png|sys/saveas,o_%s", maxModResourceRenderEdge, encodedDestination),
		),
	})
	if err != nil {
		return persistedModResourceRenderObject{}, false, err
	}
	if result.ProcessStatus != "" && !strings.EqualFold(result.ProcessStatus, "OK") {
		s.deleteOSSObjectIfUnregistered(ctx, cfg, destinationObjectKey, "mod-resource-render-process-failed")
		return persistedModResourceRenderObject{}, false, fmt.Errorf("OSS image process status: %s", result.ProcessStatus)
	}
	head, err := client.HeadObject(ctx, &aliyunoss.HeadObjectRequest{
		Bucket: aliyunoss.Ptr(cfg.Bucket),
		Key:    aliyunoss.Ptr(destinationObjectKey),
	})
	if err != nil {
		s.deleteOSSObjectIfUnregistered(ctx, cfg, destinationObjectKey, "mod-resource-render-head-failed")
		return persistedModResourceRenderObject{}, false, err
	}
	return persistedModResourceRenderObject{ObjectKey: destinationObjectKey, SizeBytes: head.ContentLength}, true, nil
}

func shouldPersistMarkdownImageAsWebP(originalName string, contentType string, source string, category string) bool {
	extension := strings.ToLower(filepath.Ext(originalName))
	if extension != ".jpg" && extension != ".jpeg" && extension != ".png" {
		return false
	}
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if contentType != "image/jpeg" && contentType != "image/jpg" && contentType != "image/png" {
		return false
	}
	source = normalizeObjectSegment(source)
	category = strings.ToLower(strings.TrimSpace(category))
	if source == "playground" || source == "comment" || source == "markdown" || source == "project_intro" || source == "projectintro" {
		return true
	}
	return strings.Contains(category, "/files/text/") ||
		strings.HasSuffix(category, "/files/playground") ||
		strings.HasSuffix(category, "/files/comments") ||
		category == "users/playground" ||
		category == "users/comments" ||
		strings.HasSuffix(category, "/description")
}

func (s *Server) persistImageAsWebP(ctx context.Context, client *aliyunoss.Client, cfg ossConfigPayload, sourceObjectKey string, sourceOriginalName string) (persistedWebPObject, error) {
	destinationObjectKey := persistedWebPObjectKey(sourceObjectKey)
	encodedDestination := base64.RawURLEncoding.EncodeToString([]byte(destinationObjectKey))
	result, err := client.ProcessObject(ctx, &aliyunoss.ProcessObjectRequest{
		Bucket:  aliyunoss.Ptr(cfg.Bucket),
		Key:     aliyunoss.Ptr(sourceObjectKey),
		Process: aliyunoss.Ptr("image/format,webp|sys/saveas,o_" + encodedDestination),
	})
	if err != nil {
		return persistedWebPObject{}, err
	}
	if result.ProcessStatus != "" && !strings.EqualFold(result.ProcessStatus, "OK") {
		s.deleteOSSObjectIfUnregistered(ctx, cfg, destinationObjectKey, "markdown-webp-process-failed")
		return persistedWebPObject{}, fmt.Errorf("OSS image process status: %s", result.ProcessStatus)
	}
	head, err := client.HeadObject(ctx, &aliyunoss.HeadObjectRequest{
		Bucket: aliyunoss.Ptr(cfg.Bucket),
		Key:    aliyunoss.Ptr(destinationObjectKey),
	})
	if err != nil {
		s.deleteOSSObjectIfUnregistered(ctx, cfg, destinationObjectKey, "markdown-webp-head-failed")
		return persistedWebPObject{}, err
	}
	baseName := strings.TrimSuffix(path.Base(sourceOriginalName), filepath.Ext(sourceOriginalName))
	if baseName == "" {
		baseName = "image"
	}
	return persistedWebPObject{
		ObjectKey:    destinationObjectKey,
		OriginalName: baseName + ".webp",
		SizeBytes:    head.ContentLength,
	}, nil
}

func randomObjectName() string {
	random := make([]byte, 16)
	_, _ = rand.Read(random)
	hexValue := hex.EncodeToString(random)
	return hexValue[0:8] + "-" + hexValue[8:12] + "-" + hexValue[12:16] + "-" + hexValue[16:20] + "-" + hexValue[20:32]
}

func normalizeObjectPrefix(value string) string {
	parts := strings.Split(strings.Trim(value, "/ "), "/")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		if segment := normalizeObjectSegment(part); segment != "" {
			cleaned = append(cleaned, segment)
		}
	}
	return strings.Join(cleaned, "/")
}

func isAllowedObjectKey(objectKey string, configuredPrefix string) bool {
	objectKey = strings.TrimSpace(objectKey)
	if objectKey == "" || strings.HasPrefix(objectKey, "/") || strings.Contains(objectKey, "..") {
		return false
	}
	prefix := normalizeObjectPrefix(configuredPrefix)
	if prefix == "" {
		return true
	}
	return objectKey == prefix || strings.HasPrefix(objectKey, prefix+"/")
}

func normalizeSHA256(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 64 {
		return ""
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return ""
		}
	}
	return value
}

func metadataValue(metadata map[string]string, key string) string {
	for currentKey, value := range metadata {
		if strings.EqualFold(currentKey, key) {
			return value
		}
	}
	return ""
}

func normalizeOSSEndpoint(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
		return "https://" + value
	}
	return value
}

func defaultOSSEndpoint(region string) string {
	region = strings.TrimSpace(region)
	if region == "" {
		return ""
	}
	return "https://oss-" + region + ".aliyuncs.com"
}

func isCustomOSSEndpoint(endpoint string) bool {
	endpoint = strings.ToLower(endpoint)
	return endpoint != "" && !strings.Contains(endpoint, ".aliyuncs.com") && !strings.Contains(endpoint, ".aliyun.com")
}

func requiresSecurityToken(accessKeyID string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(accessKeyID)), "STS.")
}

func normalizeObjectSegment(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ToLower(value)
	var builder strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func normalizeProjectObjectSegment(value string) string {
	value = normalizeObjectSegment(value)
	if value == "" {
		return "unassigned"
	}
	return value
}

const maxPermissionBytes int64 = 1<<63 - 1

func permissionMiBToBytes(value int32) int64 {
	if value <= 0 {
		return 0
	}
	if value == maxPermissionValue {
		return maxPermissionBytes
	}
	return int64(value) * 1024 * 1024
}

func formatLimitBytes(value int64) string {
	if value == maxPermissionBytes {
		return "unlimited"
	}
	if value%(1024*1024) == 0 {
		return fmt.Sprintf("%d MiB", value/(1024*1024))
	}
	return fmt.Sprintf("%d bytes", value)
}

func allowedUploadExtension(ext string, allowedExtensions []string) bool {
	ext = normalizeExtension(ext)
	if ext == "" {
		return false
	}
	allowed := make(map[string]struct{}, len(allowedExtensions))
	for _, item := range normalizeAllowedExtensions(allowedExtensions) {
		allowed[item] = struct{}{}
	}
	_, ok := allowed[ext]
	return ok
}

func normalizeAllowedExtensions(values []string) []string {
	if len(values) == 0 {
		values = defaultOSSAllowedExtensions
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		ext := normalizeExtension(value)
		if ext == "" {
			continue
		}
		if _, ok := seen[ext]; ok {
			continue
		}
		seen[ext] = struct{}{}
		result = append(result, ext)
	}
	if len(result) == 0 {
		return append([]string(nil), defaultOSSAllowedExtensions...)
	}
	return result
}

func normalizeExtension(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, ".") {
		value = "." + value
	}
	if len(value) < 2 || strings.ContainsAny(value, `/\:*?"<>|`) {
		return ""
	}
	for _, r := range value[1:] {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '+') {
			return ""
		}
	}
	return value
}
