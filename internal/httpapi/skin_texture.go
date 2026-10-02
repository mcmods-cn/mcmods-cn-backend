package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"log"
	"path"
	"strings"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	maxMinecraftTextureUploadBytes    int64 = 2 << 20
	maxMinecraftTextureDimension            = 1024
	maxMinecraftTexturePixels               = 1024 * 1024
	minecraftTextureProcessingWorkers       = 4
)

var minecraftTextureProcessingSlots = make(chan struct{}, minecraftTextureProcessingWorkers)

func acquireMinecraftTextureProcessing(ctx context.Context) (func(), bool) {
	select {
	case minecraftTextureProcessingSlots <- struct{}{}:
		return func() { <-minecraftTextureProcessingSlots }, true
	case <-ctx.Done():
		return func() {}, false
	default:
		return func() {}, false
	}
}

type sanitizedMinecraftTexture struct {
	Kind      string
	Hash      string
	Data      []byte
	Width     int
	Height    int
	SizeBytes int64
}

type minecraftTextureBlob struct {
	Hash      string
	OSSFileID int64
	ObjectKey string
	Width     int
	Height    int
	SizeBytes int64
}

// pendingTextureUpload identifies a provider object written inside a larger
// business transaction. Until that transaction commits, a rollback must be
// followed by a durable unregistered-object deletion job.
type pendingTextureUpload struct {
	Config    ossConfigPayload
	ObjectKey string
}

type minecraftTextureStore interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func sanitizeMinecraftTexture(reader io.Reader, kind string, maxBytes int64) (sanitizedMinecraftTexture, error) {
	var result sanitizedMinecraftTexture
	kind, err := normalizeMinecraftTextureKind(kind)
	if err != nil {
		return result, err
	}
	if reader == nil {
		return result, fmt.Errorf("texture file is required")
	}
	if maxBytes <= 0 {
		maxBytes = maxMinecraftTextureUploadBytes
	}

	raw, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return result, fmt.Errorf("read texture: %w", err)
	}
	if int64(len(raw)) > maxBytes {
		return result, fmt.Errorf("texture exceeds %d bytes", maxBytes)
	}
	if len(raw) == 0 {
		return result, fmt.Errorf("texture file is empty")
	}

	config, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return result, fmt.Errorf("texture must be a valid PNG: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 ||
		config.Width > maxMinecraftTextureDimension || config.Height > maxMinecraftTextureDimension ||
		config.Width > maxMinecraftTexturePixels/config.Height {
		return result, fmt.Errorf("texture dimensions exceed the image limit")
	}
	targetWidth, targetHeight, err := minecraftTextureTargetDimensions(kind, config.Width, config.Height)
	if err != nil {
		return result, err
	}
	if targetWidth <= 0 || targetHeight <= 0 ||
		targetWidth > maxMinecraftTextureDimension || targetHeight > maxMinecraftTextureDimension ||
		targetWidth > maxMinecraftTexturePixels/targetHeight {
		return result, fmt.Errorf("sanitized texture dimensions exceed the image limit")
	}

	decoded, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return result, fmt.Errorf("decode texture PNG: %w", err)
	}
	if decoded.Bounds().Dx() != config.Width || decoded.Bounds().Dy() != config.Height {
		return result, fmt.Errorf("texture dimensions changed while decoding")
	}

	canonical := image.NewNRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	draw.Draw(canonical, image.Rect(0, 0, config.Width, config.Height), decoded, decoded.Bounds().Min, draw.Src)
	var encoded bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(&encoded, canonical); err != nil {
		return result, fmt.Errorf("encode sanitized texture: %w", err)
	}
	if int64(encoded.Len()) > maxBytes {
		return result, fmt.Errorf("sanitized texture exceeds %d bytes", maxBytes)
	}
	digest := sha256.Sum256(encoded.Bytes())
	result = sanitizedMinecraftTexture{
		Kind:      kind,
		Hash:      hex.EncodeToString(digest[:]),
		Data:      append([]byte(nil), encoded.Bytes()...),
		Width:     targetWidth,
		Height:    targetHeight,
		SizeBytes: int64(encoded.Len()),
	}
	return result, nil
}

func normalizeMinecraftTextureKind(kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "skin":
		return "skin", nil
	case "cape":
		return "cape", nil
	default:
		return "", fmt.Errorf("texture kind must be skin or cape")
	}
}

func minecraftTextureTargetDimensions(kind string, width, height int) (int, int, error) {
	if width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("texture dimensions are invalid")
	}
	sameScale := func(baseWidth, baseHeight int) (int, bool) {
		if width%baseWidth != 0 || height%baseHeight != 0 {
			return 0, false
		}
		widthScale := width / baseWidth
		heightScale := height / baseHeight
		return widthScale, widthScale > 0 && widthScale == heightScale
	}
	switch kind {
	case "skin":
		if _, ok := sameScale(64, 64); ok {
			return width, height, nil
		}
		if _, ok := sameScale(64, 32); ok {
			return width, height, nil
		}
		return 0, 0, fmt.Errorf("skin dimensions must be equally scaled 64x64 or 64x32")
	case "cape":
		if _, ok := sameScale(64, 32); ok {
			return width, height, nil
		}
		if scale, ok := sameScale(22, 17); ok {
			return 64 * scale, 32 * scale, nil
		}
		return 0, 0, fmt.Errorf("cape dimensions must be equally scaled 64x32 or 22x17")
	default:
		return 0, 0, fmt.Errorf("texture kind must be skin or cape")
	}
}

func (s *Server) loadMinecraftTextureUpload(ctx context.Context, file trustedRasterOSSFile, kind string) (sanitizedMinecraftTexture, *aliyunoss.Client, ossConfigPayload, error) {
	var texture sanitizedMinecraftTexture
	var emptyConfig ossConfigPayload
	if file.ID <= 0 || file.UploaderID <= 0 || file.ObjectKey == "" || file.Status != "active" ||
		!trustedOSSScanStatus(file.ScanStatus) || !safeRasterContentType(file.ContentType) {
		return texture, nil, emptyConfig, fmt.Errorf("texture file was not found")
	}
	if file.SizeBytes <= 0 || file.SizeBytes > maxMinecraftTextureUploadBytes {
		return texture, nil, emptyConfig, fmt.Errorf("texture file size is invalid")
	}
	client, cfg, err := s.ossClient(ctx)
	if err != nil {
		return texture, nil, emptyConfig, err
	}
	object, err := client.GetObject(ctx, &aliyunoss.GetObjectRequest{
		Bucket: aliyunoss.Ptr(cfg.Bucket),
		Key:    aliyunoss.Ptr(file.ObjectKey),
	})
	if err != nil {
		return texture, nil, emptyConfig, fmt.Errorf("read private texture object: %w", err)
	}
	defer object.Body.Close()
	texture, err = sanitizeMinecraftTexture(object.Body, kind, maxMinecraftTextureUploadBytes)
	if err != nil {
		return texture, nil, emptyConfig, err
	}
	return texture, client, cfg, nil
}

func persistSanitizedMinecraftTextureTx(ctx context.Context, tx pgx.Tx, client *aliyunoss.Client, cfg ossConfigPayload, texture sanitizedMinecraftTexture) (minecraftTextureBlob, *pendingTextureUpload, error) {
	return persistSanitizedMinecraftTextureWithStore(ctx, tx, client, cfg, texture)
}

func persistSanitizedMinecraftTextureWithStore(ctx context.Context, store minecraftTextureStore, client *aliyunoss.Client, cfg ossConfigPayload, texture sanitizedMinecraftTexture) (minecraftTextureBlob, *pendingTextureUpload, error) {
	var result minecraftTextureBlob
	kind, err := normalizeMinecraftTextureKind(texture.Kind)
	if err != nil {
		return result, nil, err
	}
	if texture.Width <= 0 || texture.Height <= 0 || texture.SizeBytes != int64(len(texture.Data)) || len(texture.Data) == 0 {
		return result, nil, fmt.Errorf("sanitized texture is incomplete")
	}
	if texture.Width > maxMinecraftTextureDimension || texture.Height > maxMinecraftTextureDimension ||
		texture.Width > maxMinecraftTexturePixels/texture.Height {
		return result, nil, fmt.Errorf("sanitized texture dimensions exceed the image limit")
	}
	targetWidth, targetHeight, err := minecraftTextureTargetDimensions(kind, texture.Width, texture.Height)
	if err != nil {
		return result, nil, err
	}
	if targetWidth != texture.Width || targetHeight != texture.Height {
		return result, nil, fmt.Errorf("sanitized texture dimensions are not canonical")
	}
	digest := sha256.Sum256(texture.Data)
	computedHash := hex.EncodeToString(digest[:])
	if !strings.EqualFold(texture.Hash, computedHash) {
		return result, nil, fmt.Errorf("sanitized texture hash is invalid")
	}

	if err = lockMinecraftTextureLifecycleStore(ctx, store, computedHash); err != nil {
		return result, nil, fmt.Errorf("lock texture blob lifecycle: %w", err)
	}
	existing, fileStatus, err := minecraftTextureBlobByHashStore(ctx, store, computedHash)
	hasExisting := err == nil
	if hasExisting && fileStatus == "active" {
		return existing, nil, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		if err != nil {
			return result, nil, fmt.Errorf("load texture blob: %w", err)
		}
		if fileStatus != "deleted" {
			return result, nil, fmt.Errorf("texture blob file has invalid status %q", fileStatus)
		}
	}
	if client == nil {
		return result, nil, fmt.Errorf("texture object storage client is unavailable")
	}
	textureCategory := ossSharedSkinTextureCategory()
	lifecycleID, err := newMinecraftUUID()
	if err != nil {
		return result, nil, err
	}
	objectKey := path.Join(ossObjectPrefix(cfg.Prefix, textureCategory), computedHash+"-"+strings.ReplaceAll(lifecycleID, "-", "")+".png")
	_, err = client.PutObject(ctx, &aliyunoss.PutObjectRequest{
		Bucket:        aliyunoss.Ptr(cfg.Bucket),
		Key:           aliyunoss.Ptr(objectKey),
		ContentType:   aliyunoss.Ptr("image/png"),
		ContentLength: aliyunoss.Ptr(int64(len(texture.Data))),
		Body:          bytes.NewReader(texture.Data),
		Metadata:      map[string]string{"sha256": computedHash, "texture-kind": kind},
	})
	if err != nil {
		return result, nil, fmt.Errorf("store sanitized texture: %w", err)
	}
	pendingUpload := &pendingTextureUpload{Config: cfg, ObjectKey: objectKey}

	var fileID int64
	err = store.QueryRow(ctx, `insert into oss_files(
		bucket,endpoint,region,object_key,category,source,original_name,source_original_name,
		content_type,size_bytes,source_size_bytes,sha256,status,scan_status)
		values($1,$2,$3,$4,$5,'minecraft_texture_derived',$6,$6,'image/png',$7,0,$8,'active','trusted_generated')
		returning id`, cfg.Bucket, cfg.Endpoint, cfg.Region, objectKey, textureCategory, computedHash+".png",
		len(texture.Data), computedHash).Scan(&fileID)
	if err != nil {
		return result, pendingUpload, fmt.Errorf("record sanitized texture: %w", err)
	}
	if hasExisting {
		_, err = store.Exec(ctx, `update skin_texture_blobs set
			oss_file_id=$2,object_key=$3,width=$4,height=$5,size_bytes=$6
			where hash=$1`, computedHash, fileID, objectKey, texture.Width, texture.Height, len(texture.Data))
	} else {
		_, err = store.Exec(ctx, `insert into skin_texture_blobs(hash,oss_file_id,object_key,width,height,size_bytes)
			values($1,$2,$3,$4,$5,$6)`, computedHash, fileID, objectKey, texture.Width, texture.Height, len(texture.Data))
	}
	if err != nil {
		return result, pendingUpload, fmt.Errorf("record texture blob: %w", err)
	}
	result = minecraftTextureBlob{Hash: computedHash, OSSFileID: fileID, ObjectKey: objectKey,
		Width: texture.Width, Height: texture.Height, SizeBytes: int64(len(texture.Data))}
	return result, pendingUpload, nil
}

func minecraftTextureBlobByHashStore(ctx context.Context, store minecraftTextureStore, hash string) (minecraftTextureBlob, string, error) {
	var result minecraftTextureBlob
	var fileStatus string
	err := store.QueryRow(ctx, `select blob.hash,blob.oss_file_id,blob.object_key,blob.width,blob.height,blob.size_bytes,file.status
		from skin_texture_blobs blob join oss_files file on file.id=blob.oss_file_id
		where blob.hash=$1 for update of blob,file`, hash).Scan(&result.Hash, &result.OSSFileID, &result.ObjectKey,
		&result.Width, &result.Height, &result.SizeBytes, &fileStatus)
	return result, fileStatus, err
}

func (s *Server) compensateUncommittedMinecraftTextureUpload(upload *pendingTextureUpload) {
	if s == nil || upload == nil || strings.TrimSpace(upload.ObjectKey) == "" {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.enqueueUnregisteredOSSObjectDeletion(cleanupCtx, upload.Config, upload.ObjectKey, "minecraft-texture-transaction-aborted"); err != nil {
		log.Printf("queue uncommitted Minecraft texture cleanup for %s: %v", upload.ObjectKey, err)
	}
}

func (s *Server) tombstoneUnreferencedMinecraftTextureBlobTx(ctx context.Context, tx pgx.Tx, hash, reason string) error {
	if err := lockMinecraftTextureLifecycleStore(ctx, tx, hash); err != nil {
		return err
	}
	var fileID, activeReferences int64
	err := tx.QueryRow(ctx, `select blob.oss_file_id,blob.active_reference_count
		from skin_texture_blobs blob where blob.hash=$1 for update`, hash).Scan(&fileID, &activeReferences)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil || activeReferences > 0 {
		return err
	}
	var actualReferences int64
	if err = tx.QueryRow(ctx, `select count(*) from skin_assets
		where blob_hash=$1 and status='active'`, hash).Scan(&actualReferences); err != nil {
		return err
	}
	if actualReferences > 0 {
		_, err = tx.Exec(ctx, `update skin_texture_blobs set active_reference_count=$2 where hash=$1`, hash, actualReferences)
		return err
	}
	return s.tombstoneOSSFileTx(ctx, tx, fileID, reason)
}

func lockMinecraftTextureLifecycleStore(ctx context.Context, store minecraftTextureStore, hash string) error {
	if _, err := store.Exec(ctx, `select pg_advisory_xact_lock_shared(
		hashtext('minecraft-texture-reference'),hashtext('calibration'))`); err != nil {
		return err
	}
	_, err := store.Exec(ctx, `select pg_advisory_xact_lock(
		hashtext('minecraft-texture-blob'),hashtext($1))`, hash)
	return err
}

func newMinecraftUUID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate Minecraft UUID: %w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}
