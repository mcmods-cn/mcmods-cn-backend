package httpapi

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/config"
)

func TestSanitizeStickerImageStripsPNGTextMetadata(t *testing.T) {
	t.Parallel()
	imageData := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	imageData.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, imageData); err != nil {
		t.Fatal(err)
	}
	input := insertPNGChunkBeforeIEND(t, encoded.Bytes(), "tEXt", []byte("Author\x00C:\\secret\\artist.txt"))
	if !bytes.Contains(input, []byte("secret")) {
		t.Fatal("PNG metadata fixture is missing its private marker")
	}

	output, width, height, err := sanitizeStickerImage(input, "image/png", normalizedStickerLimits(config.StickerConfig{}))
	if err != nil {
		t.Fatal(err)
	}
	if width != 2 || height != 2 {
		t.Fatalf("dimensions = %dx%d, want 2x2", width, height)
	}
	if bytes.Contains(output, []byte("secret")) || bytes.Contains(output, []byte("Author")) {
		t.Fatal("sanitized PNG retained a private text chunk")
	}
	if _, err = png.Decode(bytes.NewReader(output)); err != nil {
		t.Fatalf("sanitized PNG cannot be decoded: %v", err)
	}
}

func TestSanitizeStickerImageStripsGIFCommentsAndEnforcesLimits(t *testing.T) {
	t.Parallel()
	palette := color.Palette{color.Transparent, color.White}
	frame := image.NewPaletted(image.Rect(0, 0, 2, 2), palette)
	frame.SetColorIndex(0, 0, 1)
	animation := &gif.GIF{
		Image:     []*image.Paletted{frame},
		Delay:     []int{1},
		LoopCount: 0,
		Config:    image.Config{ColorModel: palette, Width: 2, Height: 2},
	}
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, animation); err != nil {
		t.Fatal(err)
	}
	input := insertGIFComment(t, encoded.Bytes(), []byte("private-comment"))
	output, width, height, err := sanitizeStickerImage(input, "image/gif", normalizedStickerLimits(config.StickerConfig{}))
	if err != nil {
		t.Fatal(err)
	}
	if width != 2 || height != 2 || bytes.Contains(output, []byte("private-comment")) {
		t.Fatalf("GIF was not sanitized: dimensions=%dx%d comment=%v", width, height, bytes.Contains(output, []byte("private-comment")))
	}
	if _, err = gif.DecodeAll(bytes.NewReader(output)); err != nil {
		t.Fatalf("sanitized GIF cannot be decoded: %v", err)
	}

	twoFrames := *animation
	twoFrames.Image = []*image.Paletted{frame, frame}
	twoFrames.Delay = []int{1, 1}
	encoded.Reset()
	if err = gif.EncodeAll(&encoded, &twoFrames); err != nil {
		t.Fatal(err)
	}
	limits := normalizedStickerLimits(config.StickerConfig{MaxGIFFrames: 1})
	if _, _, _, err = sanitizeStickerImage(encoded.Bytes(), "image/gif", limits); err == nil {
		t.Fatal("GIF exceeding the configured frame limit was accepted")
	}
}

func TestSanitizeStickerImageRejectsDeclaredTypeMismatchAndTruncation(t *testing.T) {
	t.Parallel()
	imageData := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, imageData); err != nil {
		t.Fatal(err)
	}
	limits := normalizedStickerLimits(config.StickerConfig{})
	if _, _, _, err := sanitizeStickerImage(encoded.Bytes(), "image/gif", limits); err == nil {
		t.Fatal("PNG bytes declared as GIF were accepted")
	}
	if _, _, _, err := sanitizeStickerImage(encoded.Bytes()[:12], "image/png", limits); err == nil {
		t.Fatal("truncated PNG was accepted")
	}
}

func TestOnlySanitizedStickerDerivativesArePublicInlineFiles(t *testing.T) {
	t.Parallel()
	if isPublicInlineOSSFileSource("sticker") || isPublicInlineOSSFileSource("sticker-upload") {
		t.Fatal("original sticker uploads remain publicly readable")
	}
	if !isPublicInlineOSSFileSource("sticker_derived") {
		t.Fatal("sanitized sticker derivatives are not publicly readable")
	}
}

func TestStickerRoutesKeepReadManageAndUploadPermissionsSeparate(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	routes := strings.ReplaceAll(string(source), "\r\n", "\n")
	for _, required := range []string{
		`HandleFunc("GET /api/v1/stickers", s.publicStickerCatalog)`,
		`HandleFunc("GET /api/v1/admin/stickers", s.requirePermission("sticker.manage", s.adminStickerCatalog))`,
		`HandleFunc("POST /api/v1/admin/sticker-packs", s.requirePermission("sticker.manage", s.createStickerPack))`,
		`requirePermission("sticker.manage", s.requirePermission("sticker.upload", s.createSticker))`,
	} {
		if !strings.Contains(routes, required) {
			t.Fatalf("sticker route is missing permission boundary %q", required)
		}
	}
}

func insertPNGChunkBeforeIEND(t *testing.T, source []byte, chunkType string, data []byte) []byte {
	t.Helper()
	if len(source) < 12 || string(source[len(source)-8:len(source)-4]) != "IEND" || len(chunkType) != 4 {
		t.Fatal("invalid PNG fixture")
	}
	chunk := make([]byte, 12+len(data))
	binary.BigEndian.PutUint32(chunk[:4], uint32(len(data)))
	copy(chunk[4:8], chunkType)
	copy(chunk[8:8+len(data)], data)
	binary.BigEndian.PutUint32(chunk[8+len(data):], crc32.ChecksumIEEE(chunk[4:8+len(data)]))
	result := append([]byte{}, source[:len(source)-12]...)
	result = append(result, chunk...)
	return append(result, source[len(source)-12:]...)
}

func insertGIFComment(t *testing.T, source, comment []byte) []byte {
	t.Helper()
	if len(source) < 1 || source[len(source)-1] != 0x3b || len(comment) == 0 || len(comment) > 255 {
		t.Fatal("invalid GIF fixture")
	}
	result := append([]byte{}, source[:len(source)-1]...)
	result = append(result, 0x21, 0xfe, byte(len(comment)))
	result = append(result, comment...)
	return append(result, 0x00, 0x3b)
}
