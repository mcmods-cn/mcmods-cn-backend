package httpapi

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"strings"
	"testing"
)

func TestSEC030GIFValidationCoversEveryFrameAndAnimationBudget(t *testing.T) {
	valid := encodeSEC030GIF(t, 2, 2, 2, []int{1, 2})
	if _, err := validateRasterImageBytes(valid, "image/gif"); err != nil {
		t.Fatalf("valid two-frame GIF rejected: %v", err)
	}

	truncatedLaterFrame := append([]byte(nil), valid[:len(valid)-2]...)
	if _, err := gif.Decode(bytes.NewReader(truncatedLaterFrame)); err != nil {
		t.Fatalf("fixture no longer demonstrates first-frame-only acceptance: %v", err)
	}
	if _, err := validateRasterImageBytes(truncatedLaterFrame, "image/gif"); err == nil {
		t.Fatal("GIF with a truncated later frame was accepted")
	}

	tooManyFrames := encodeSEC030GIF(t, 1, 1, 121, nil)
	if _, err := validateRasterImageBytes(tooManyFrames, "image/gif"); err == nil {
		t.Fatal("121-frame GIF was accepted")
	}

	tooLong := encodeSEC030GIF(t, 1, 1, 2, []int{2_000, 2_000})
	if _, err := validateRasterImageBytes(tooLong, "image/gif"); err == nil {
		t.Fatal("40-second GIF was accepted")
	}

	tooManyDecodedPixels := encodeSEC030GIF(t, 2_048, 2_048, 17, nil)
	if _, err := validateRasterImageBytes(tooManyDecodedPixels, "image/gif"); err == nil {
		t.Fatal("GIF exceeding the total decoded-pixel budget was accepted")
	}
}

func TestSEC030GIFBudgetIsCheckedBeforeFullDecode(t *testing.T) {
	const edge = 2_048
	data := append([]byte("GIF89a"), 0, 0, 0, 0, 0, 0, 0)
	binary.LittleEndian.PutUint16(data[6:8], edge)
	binary.LittleEndian.PutUint16(data[8:10], edge)
	for range 17 {
		data = append(data, gifImageDescriptor, 0, 0, 0, 0, 0, 0, 0, 0, 0)
		binary.LittleEndian.PutUint16(data[len(data)-5:len(data)-3], edge)
		binary.LittleEndian.PutUint16(data[len(data)-3:len(data)-1], edge)
		data = append(data, 2, 1, 0, 0)
	}
	data = append(data, gifTrailer)

	_, err := decodeGIFAnimationWithBudget(data, maxValidatedGIFFrames, maxValidatedGIFTotalPixels, maxValidatedGIFDuration)
	if err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("expected the descriptor budget to reject before full decoding, got %v", err)
	}
	if _, err = gif.DecodeAll(bytes.NewReader(data)); err == nil {
		t.Fatal("malformed compressed-data fixture unexpectedly decoded")
	}
}

func encodeSEC030GIF(t *testing.T, width, height, frameCount int, delays []int) []byte {
	t.Helper()
	palette := color.Palette{color.Transparent, color.RGBA{R: 0x44, G: 0x88, B: 0xcc, A: 0xff}}
	frame := image.NewPaletted(image.Rect(0, 0, width, height), palette)
	for index := range frame.Pix {
		frame.Pix[index] = 1
	}
	animation := &gif.GIF{
		Image:  make([]*image.Paletted, frameCount),
		Delay:  make([]int, frameCount),
		Config: image.Config{ColorModel: palette, Width: width, Height: height},
	}
	for index := range animation.Image {
		animation.Image[index] = frame
		if index < len(delays) {
			animation.Delay[index] = delays[index]
		}
	}
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, animation); err != nil {
		t.Fatalf("encode GIF fixture: %v", err)
	}
	return encoded.Bytes()
}
