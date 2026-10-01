package httpapi

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image/gif"
	"time"

	"mcmods-cn-backend/internal/config"
)

func validateStickerGIF(data []byte, limits config.StickerConfig) error {
	if err := preflightStickerGIF(data, limits); err != nil {
		return err
	}
	animation, decodeErr := gif.DecodeAll(bytes.NewReader(data))
	if decodeErr != nil || len(animation.Image) == 0 || len(animation.Image) > limits.MaxGIFFrames {
		return errors.New("GIF frame count is invalid")
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
		return errors.New("GIF animation exceeds the decoded size or duration limit")
	}
	return nil
}

// preflightStickerGIF walks the bounded encoded blocks without allocating pixel
// buffers or decompressing LZW. DecodeAll remains the content validator, but is
// called only after every frame's dimensions and cumulative limits are checked.
func preflightStickerGIF(data []byte, limits config.StickerConfig) error {
	if int64(len(data)) > limits.MaxBytes || len(data) < 13 || string(data[:6]) != "GIF87a" && string(data[:6]) != "GIF89a" {
		return errors.New("GIF header or size is invalid")
	}
	width := int(binary.LittleEndian.Uint16(data[6:8]))
	height := int(binary.LittleEndian.Uint16(data[8:10]))
	if width == 0 || height == 0 || width > limits.MaxEdge || height > limits.MaxEdge || int64(width)*int64(height) > limits.MaxPixels {
		return errors.New("sticker dimensions exceed the safety limit")
	}
	cursor := 13
	read := func(size int) ([]byte, error) {
		if size > len(data)-cursor {
			return nil, errors.New("GIF block is truncated")
		}
		part := data[cursor : cursor+size]
		cursor += size
		return part, nil
	}
	colorTable := func(flags byte) error {
		if flags&0x80 == 0 {
			return nil
		}
		_, err := read(3 << (uint(flags&7) + 1))
		return err
	}
	blocks := func() error {
		for {
			size, err := read(1)
			if err != nil {
				return err
			}
			if size[0] == 0 {
				return nil
			}
			if _, err = read(int(size[0])); err != nil {
				return err
			}
		}
	}
	if err := colorTable(data[10]); err != nil {
		return err
	}
	frames, totalPixels, delay := 0, int64(0), uint16(0)
	duration := time.Duration(0)
	for {
		marker, err := read(1)
		if err != nil {
			return err
		}
		switch marker[0] {
		case 0x3b: // Trailer: DecodeAll also ignores bytes after this marker.
			if frames == 0 {
				return errors.New("GIF frame count is invalid")
			}
			return nil
		case 0x21: // Extension, including frame delay and non-image data.
			label, err := read(1)
			if err != nil {
				return err
			}
			switch label[0] {
			case 0xf9:
				control, err := read(6)
				if err != nil {
					return err
				}
				if control[0] != 4 || control[5] != 0 {
					return errors.New("GIF graphic control block is invalid")
				}
				delay = binary.LittleEndian.Uint16(control[2:4])
				continue
			case 0x01:
				header, err := read(13)
				if err != nil {
					return err
				}
				if header[0] != 12 {
					return errors.New("GIF text extension is invalid")
				}
			case 0xff:
				size, err := read(1)
				if err != nil {
					return err
				}
				// Go's decoder accepts Adobe's 10-byte header as well as 11.
				if size[0] != 10 && size[0] != 11 {
					return errors.New("GIF application extension is invalid")
				}
				if _, err = read(int(size[0])); err != nil {
					return err
				}
			case 0xfe:
			default:
				return errors.New("GIF extension is unsupported")
			}
			if err = blocks(); err != nil {
				return err
			}
		case 0x2c: // Image descriptor followed by palette and LZW sub-blocks.
			descriptor, err := read(9)
			if err != nil {
				return err
			}
			left := int(binary.LittleEndian.Uint16(descriptor[0:2]))
			top := int(binary.LittleEndian.Uint16(descriptor[2:4]))
			frameWidth := int(binary.LittleEndian.Uint16(descriptor[4:6]))
			frameHeight := int(binary.LittleEndian.Uint16(descriptor[6:8]))
			if frameWidth == 0 || frameHeight == 0 || left+frameWidth > width || top+frameHeight > height {
				return errors.New("GIF frame dimensions are invalid")
			}
			frames++
			pixels := int64(frameWidth) * int64(frameHeight)
			if frames > limits.MaxGIFFrames || pixels > limits.MaxGIFDecodedPixels-totalPixels {
				return errors.New("GIF frame count or decoded size exceeds the safety limit")
			}
			totalPixels += pixels
			frameDuration := time.Duration(delay) * 10 * time.Millisecond
			if frameDuration > limits.MaxGIFDuration-duration {
				return errors.New("GIF animation exceeds the duration limit")
			}
			duration += frameDuration
			delay = 0
			if err = colorTable(descriptor[8]); err != nil {
				return err
			}
			codeSize, err := read(1)
			if err != nil {
				return err
			}
			if codeSize[0] < 2 || codeSize[0] > 8 {
				return errors.New("GIF LZW code size is invalid")
			}
			if err = blocks(); err != nil {
				return err
			}
		default:
			return errors.New("GIF block marker is invalid")
		}
	}
}
