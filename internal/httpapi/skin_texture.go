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
	"path"
	"strings"

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

func (s *Server) persistMinecraftTextureBlob(ctx context.Context, ownerID, fileID int64, kind string) (minecraftTextureBlob, error) {
	texture, _, _, err := s.loadMinecraftTextureUpload(ctx, ownerID, fileID, kind)
	if err != nil {
		return minecraftTextureBlob{}, err
	}
	return s.persistSanitizedMinecraftTexture(ctx, ownerID, texture)
}

func (s *Server) loadMinecraftTextureUpload(ctx context.Context, ownerID, fileID int64, kind string) (sanitizedMinecraftTexture, *aliyunoss.Client, ossConfigPayload, error) {
	var texture sanitizedMinecraftTexture
	var emptyConfig ossConfigPayload
	if ownerID <= 0 || fileID <= 0 {
		return texture, nil, emptyConfig, fmt.Errorf("texture file was not found")
	}
	var objectKey string
	var sizeBytes int64
	err := s.db.QueryRow(ctx, `select object_key,size_bytes from oss_files
		where id=$1 and uploader_id=$2 and status='active'`, fileID, ownerID).Scan(&objectKey, &sizeBytes)
	if err != nil {
		return texture, nil, emptyConfig, fmt.Errorf("texture file was not found: %w", err)
	}
	if sizeBytes <= 0 || sizeBytes > maxMinecraftTextureUploadBytes {
		return texture, nil, emptyConfig, fmt.Errorf("texture file size is invalid")
	}
	client, cfg, err := s.ossClient(ctx)
	if err != nil {
		return texture, nil, emptyConfig, err
	}
	object, err := client.GetObject(ctx, &aliyunoss.GetObjectRequest{
		Bucket: aliyunoss.Ptr(cfg.Bucket),
		Key:    aliyunoss.Ptr(objectKey),
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

func (s *Server) persistSanitizedMinecraftTexture(ctx context.Context, ownerID int64, texture sanitizedMinecraftTexture) (minecraftTextureBlob, error) {
	client, cfg, err := s.ossClient(ctx)
	if err != nil {
		return minecraftTextureBlob{}, err
	}
	return persistSanitizedMinecraftTextureWithStore(ctx, s.db, client, cfg, ownerID, texture)
}

func persistSanitizedMinecraftTextureTx(ctx context.Context, tx pgx.Tx, client *aliyunoss.Client, cfg ossConfigPayload, ownerID int64, texture sanitizedMinecraftTexture) (minecraftTextureBlob, error) {
	return persistSanitizedMinecraftTextureWithStore(ctx, tx, client, cfg, ownerID, texture)
}

func persistSanitizedMinecraftTextureWithStore(ctx context.Context, store minecraftTextureStore, client *aliyunoss.Client, cfg ossConfigPayload, ownerID int64, texture sanitizedMinecraftTexture) (minecraftTextureBlob, error) {
	var result minecraftTextureBlob
	if ownerID <= 0 {
		return result, fmt.Errorf("texture owner is required")
	}
	kind, err := normalizeMinecraftTextureKind(texture.Kind)
	if err != nil {
		return result, err
	}
	if texture.Width <= 0 || texture.Height <= 0 || texture.SizeBytes != int64(len(texture.Data)) || len(texture.Data) == 0 {
		return result, fmt.Errorf("sanitized texture is incomplete")
	}
	if texture.Width > maxMinecraftTextureDimension || texture.Height > maxMinecraftTextureDimension ||
		texture.Width > maxMinecraftTexturePixels/texture.Height {
		return result, fmt.Errorf("sanitized texture dimensions exceed the image limit")
	}
	targetWidth, targetHeight, err := minecraftTextureTargetDimensions(kind, texture.Width, texture.Height)
	if err != nil {
		return result, err
	}
	if targetWidth != texture.Width || targetHeight != texture.Height {
		return result, fmt.Errorf("sanitized texture dimensions are not canonical")
	}
	digest := sha256.Sum256(texture.Data)
	computedHash := hex.EncodeToString(digest[:])
	if !strings.EqualFold(texture.Hash, computedHash) {
		return result, fmt.Errorf("sanitized texture hash is invalid")
	}

	existing, err := minecraftTextureBlobByHashStore(ctx, store, computedHash)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, fmt.Errorf("load texture blob: %w", err)
	}
	if client == nil {
		return result, fmt.Errorf("texture object storage client is unavailable")
	}
	textureCategory := ossSharedSkinTextureCategory()
	objectKey := path.Join(ossObjectPrefix(cfg.Prefix, textureCategory), computedHash+".png")
	_, err = client.PutObject(ctx, &aliyunoss.PutObjectRequest{
		Bucket:        aliyunoss.Ptr(cfg.Bucket),
		Key:           aliyunoss.Ptr(objectKey),
		ContentType:   aliyunoss.Ptr("image/png"),
		ContentLength: aliyunoss.Ptr(int64(len(texture.Data))),
		Body:          bytes.NewReader(texture.Data),
		Metadata:      map[string]string{"sha256": computedHash, "texture-kind": kind},
	})
	if err != nil {
		return result, fmt.Errorf("store sanitized texture: %w", err)
	}

	var fileID int64
	err = store.QueryRow(ctx, `insert into oss_files(
		bucket,endpoint,region,object_key,category,source,original_name,source_original_name,
		content_type,size_bytes,source_size_bytes,sha256,uploader_id,status,scan_status)
		values($1,$2,$3,$4,$5,'minecraft_texture',$6,$6,'image/png',$7,$7,$8,$9,'active','trusted_generated')
		on conflict(object_key) do update set status='active',updated_at=now()
		returning id`, cfg.Bucket, cfg.displayEndpoint(), cfg.Region, objectKey, textureCategory, computedHash+".png",
		len(texture.Data), computedHash, ownerID).Scan(&fileID)
	if err != nil {
		return result, fmt.Errorf("record sanitized texture: %w", err)
	}
	_, err = store.Exec(ctx, `insert into skin_texture_blobs(hash,oss_file_id,object_key,width,height,size_bytes)
		values($1,$2,$3,$4,$5,$6) on conflict(hash) do nothing`, computedHash, fileID, objectKey,
		texture.Width, texture.Height, len(texture.Data))
	if err != nil {
		return result, fmt.Errorf("record texture blob: %w", err)
	}
	existing, err = minecraftTextureBlobByHashStore(ctx, store, computedHash)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, fmt.Errorf("load recorded texture blob: %w", err)
	}
	return result, fmt.Errorf("texture blob was not recorded")
}

func minecraftTextureBlobByHashStore(ctx context.Context, store minecraftTextureStore, hash string) (minecraftTextureBlob, error) {
	var result minecraftTextureBlob
	err := store.QueryRow(ctx, `select hash,oss_file_id,object_key,width,height,size_bytes
		from skin_texture_blobs where hash=$1`, hash).Scan(&result.Hash, &result.OSSFileID, &result.ObjectKey,
		&result.Width, &result.Height, &result.SizeBytes)
	return result, err
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
