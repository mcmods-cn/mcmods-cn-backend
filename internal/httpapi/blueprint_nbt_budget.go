package httpapi

import (
	"encoding/binary"
	"errors"
	"io"
)

const (
	maxBlueprintNBTDepth      = 64
	maxBlueprintNBTValues     = 2 << 20
	maxBlueprintNBTAllocation = 128 << 20
)

// blueprintNBTBudget walks the wire format without allocating payload objects.
// Its allocation counter is a conservative processing budget, not a measurement
// of the Go heap or an assertion about the decoder's exact allocation sizes.
type blueprintNBTBudget struct {
	data       []byte
	offset     int
	values     int64
	allocation int64
}

func validateBlueprintNBT(data []byte) error {
	budget := blueprintNBTBudget{data: data}
	tag, err := budget.byte()
	if err != nil {
		return err
	}
	if tag != 10 {
		return errors.New("blueprint NBT root must be a compound")
	}
	if err := budget.string(); err != nil {
		return err
	}
	if err := budget.payload(tag, 0); err != nil {
		return err
	}
	if budget.offset != len(data) {
		return errors.New("blueprint NBT contains trailing data")
	}
	return nil
}

func (b *blueprintNBTBudget) take(length int64) error {
	if length < 0 || length > int64(len(b.data)-b.offset) {
		return io.ErrUnexpectedEOF
	}
	b.offset += int(length)
	return nil
}

func (b *blueprintNBTBudget) byte() (byte, error) {
	if b.offset == len(b.data) {
		return 0, io.ErrUnexpectedEOF
	}
	value := b.data[b.offset]
	b.offset++
	return value, nil
}

func (b *blueprintNBTBudget) reserve(bytes int64) error {
	if bytes < 0 || bytes > maxBlueprintNBTAllocation-b.allocation {
		return errors.New("blueprint NBT exceeds its decoded allocation budget")
	}
	b.allocation += bytes
	return nil
}

func (b *blueprintNBTBudget) string() error {
	if len(b.data)-b.offset < 2 {
		return io.ErrUnexpectedEOF
	}
	length := int64(binary.BigEndian.Uint16(b.data[b.offset:]))
	b.offset += 2
	if err := b.reserve(length); err != nil {
		return err
	}
	return b.take(length)
}

func (b *blueprintNBTBudget) length() (int64, error) {
	if len(b.data)-b.offset < 4 {
		return 0, io.ErrUnexpectedEOF
	}
	length := int64(int32(binary.BigEndian.Uint32(b.data[b.offset:])))
	b.offset += 4
	if length < 0 {
		return 0, errors.New("blueprint NBT collection has a negative length")
	}
	return length, nil
}

func (b *blueprintNBTBudget) payload(tag byte, depth int) error {
	if depth > maxBlueprintNBTDepth || b.values >= maxBlueprintNBTValues {
		return errors.New("blueprint NBT exceeds its nesting or value budget")
	}
	b.values++
	switch tag {
	case 1, 2, 3, 4, 5, 6:
		width := [...]int64{0, 1, 2, 4, 8, 4, 8}[tag]
		if err := b.reserve(8); err != nil {
			return err
		}
		return b.take(width)
	case 7, 11, 12:
		length, err := b.length()
		if err != nil {
			return err
		}
		width := int64(1)
		if tag == 11 {
			width = 4
		} else if tag == 12 {
			width = 8
		}
		if err := b.reserve(length * width); err != nil {
			return err
		}
		return b.take(length * width)
	case 8:
		return b.string()
	case 9:
		element, err := b.byte()
		if err != nil {
			return err
		}
		length, err := b.length()
		if err != nil {
			return err
		}
		if element > 12 || (element == 0 && length != 0) || length > maxBlueprintNBTValues-b.values {
			return errors.New("blueprint NBT list has an invalid type or excessive length")
		}
		if err := b.reserve(length * 16); err != nil {
			return err
		}
		for index := int64(0); index < length; index++ {
			if err := b.payload(element, depth+1); err != nil {
				return err
			}
		}
		return nil
	case 10:
		if err := b.reserve(96); err != nil {
			return err
		}
		for {
			child, err := b.byte()
			if err != nil {
				return err
			}
			if child == 0 {
				return nil
			}
			if err := b.reserve(64); err != nil {
				return err
			}
			if err := b.string(); err != nil {
				return err
			}
			if err := b.payload(child, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("blueprint NBT contains an unknown tag type")
	}
}
