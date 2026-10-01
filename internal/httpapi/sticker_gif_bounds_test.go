package httpapi

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
)

func stickerGIFTestData(t *testing.T, count, edge, delay int) []byte {
	t.Helper()
	animation := &gif.GIF{LoopCount: 0}
	palette := color.Palette{color.Black, color.White}
	for range count {
		animation.Image = append(animation.Image, image.NewPaletted(image.Rect(0, 0, edge, edge), palette))
		animation.Delay = append(animation.Delay, delay)
	}
	var output bytes.Buffer
	if err := gif.EncodeAll(&output, animation); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestStickerGIFOverLimitDoesNotAllocateAllFrames(t *testing.T) {
	data := stickerGIFTestData(t, 64, 64, 1)
	if int64(len(data)) > maxStickerBytes {
		t.Fatal("fixture must fit the actual 4 MiB sticker-upload limit")
	}
	for _, test := range []struct {
		name   string
		limits config.StickerConfig
	}{
		{"frame count", config.StickerConfig{MaxGIFFrames: 2}},
		{"decoded pixels", config.StickerConfig{MaxGIFDecodedPixels: 8192}},
	} {
		t.Run(test.name, func(t *testing.T) {
			limits := normalizedStickerLimits(test.limits)
			if err := validateStickerGIF(data, limits); err == nil {
				t.Fatal("accepted an over-limit GIF")
			}
			result := testing.Benchmark(func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					_ = validateStickerGIF(data, limits)
				}
			})
			// These small, compressed frames decode to 256 KiB. A rejected
			// animation must not allocate all of those frame buffers.
			if result.AllocedBytesPerOp() > 64<<10 {
				t.Fatalf("over-limit GIF allocated %d bytes before rejection", result.AllocedBytesPerOp())
			}
			t.Logf("rejected GIF allocation: %d bytes per validation", result.AllocedBytesPerOp())
		})
	}
}

func TestStickerGIFEnforcesCumulativePixelsAndDuration(t *testing.T) {
	cases := []struct {
		name               string
		count, edge, delay int
		limits             config.StickerConfig
	}{
		{"decoded pixels", 3, 32, 1, config.StickerConfig{MaxGIFDecodedPixels: 2048}},
		{"duration", 2, 2, 60, config.StickerConfig{MaxGIFDuration: time.Second}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if err := validateStickerGIF(stickerGIFTestData(t, test.count, test.edge, test.delay), normalizedStickerLimits(test.limits)); err == nil {
				t.Fatal("accepted an over-limit GIF")
			}
		})
	}
}

func TestStickerGIFKeepsValidAnimationAndRejectsTruncation(t *testing.T) {
	limits := normalizedStickerLimits(config.StickerConfig{})
	data := stickerGIFTestData(t, 2, 8, 5)
	if err := validateStickerGIF(data, limits); err != nil {
		t.Fatal(err)
	}
	for _, end := range []int{0, 5, 12, len(data) - 1, len(data) - 4} {
		if err := validateStickerGIF(data[:end], limits); err == nil {
			t.Fatalf("accepted GIF truncated at %d", end)
		}
	}
}

func TestStickerGIFRejectsInvalidDescriptorsAndCompressedBlocks(t *testing.T) {
	limits := normalizedStickerLimits(config.StickerConfig{})
	valid := stickerGIFTestData(t, 2, 8, 5)
	imageOffset := bytes.IndexByte(valid, 0x2c)
	controlOffset := bytes.Index(valid, []byte{0x21, 0xf9, 4})
	if imageOffset < 0 || controlOffset < 0 {
		t.Fatal("fixture is missing expected GIF structures")
	}
	for _, test := range []struct {
		name   string
		change func([]byte) []byte
	}{
		{"logical dimensions", func(data []byte) []byte { binary.LittleEndian.PutUint16(data[6:8], 65535); return data }},
		{"frame outside canvas", func(data []byte) []byte {
			binary.LittleEndian.PutUint16(data[imageOffset+1:imageOffset+3], 65535)
			return data
		}},
		{"invalid control size", func(data []byte) []byte { data[controlOffset+2] = 3; return data }},
		{"unterminated compressed data", func(data []byte) []byte { return data[:len(data)-4] }},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := test.change(append([]byte(nil), valid...))
			if err := preflightStickerGIF(data, limits); err == nil {
				t.Fatal("accepted malformed GIF structure")
			}
		})
	}
}
