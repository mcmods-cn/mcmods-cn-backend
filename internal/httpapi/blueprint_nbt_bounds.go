package httpapi

import (
	"encoding/binary"
	"errors"
)

const (
	maxBlueprintNBTDepth = 128
	// Bound map/list objects as well as byte arrays. The format-dependent object
	// limit may be reached before the separate block-count or byte-size limit.
	maxBlueprintNBTValues = 2 << 20
)

type blueprintNBTScanner struct {
	data   []byte
	offset int
	values int
}

func validateBlueprintNBT(data []byte) error {
	s := blueprintNBTScanner{data: data}
	tag, err := s.byte()
	if err != nil || tag != 10 {
		return errors.New("blueprint NBT root must be a compound")
	}
	if err = s.string(); err != nil {
		return err
	}
	if err = s.payload(tag, 0); err != nil {
		return err
	}
	if s.offset != len(data) {
		return errors.New("blueprint NBT contains trailing data")
	}
	return nil
}

func (s *blueprintNBTScanner) skip(size int) error {
	if size < 0 || size > len(s.data)-s.offset {
		return errors.New("blueprint NBT payload is truncated")
	}
	s.offset += size
	return nil
}

func (s *blueprintNBTScanner) byte() (byte, error) {
	if s.offset == len(s.data) {
		return 0, errors.New("blueprint NBT payload is truncated")
	}
	value := s.data[s.offset]
	s.offset++
	return value, nil
}

func (s *blueprintNBTScanner) string() error {
	if err := s.skip(2); err != nil {
		return err
	}
	size := int(binary.BigEndian.Uint16(s.data[s.offset-2 : s.offset]))
	return s.skip(size)
}

func (s *blueprintNBTScanner) length() (int, error) {
	if err := s.skip(4); err != nil {
		return 0, err
	}
	size := int32(binary.BigEndian.Uint32(s.data[s.offset-4 : s.offset]))
	if size < 0 {
		return 0, errors.New("blueprint NBT length is negative")
	}
	return int(size), nil
}

func (s *blueprintNBTScanner) payload(tag byte, depth int) error {
	s.values++
	if depth > maxBlueprintNBTDepth || s.values > maxBlueprintNBTValues {
		return errors.New("blueprint NBT object complexity exceeds processing limit")
	}
	switch tag {
	case 1:
		return s.skip(1)
	case 2:
		return s.skip(2)
	case 3, 5:
		return s.skip(4)
	case 4, 6:
		return s.skip(8)
	case 7, 11, 12:
		count, err := s.length()
		if err != nil {
			return err
		}
		width := 1
		if tag == 11 {
			width = 4
		} else if tag == 12 {
			width = 8
		}
		if count > (len(s.data)-s.offset)/width {
			return errors.New("blueprint NBT array length exceeds its payload")
		}
		return s.skip(count * width)
	case 8:
		return s.string()
	case 9:
		element, err := s.byte()
		if err != nil {
			return err
		}
		count, err := s.length()
		if err != nil {
			return err
		}
		if element > 12 || (element == 0 && count != 0) || count > maxBlueprintNBTValues-s.values {
			return errors.New("blueprint NBT list is invalid or exceeds processing limit")
		}
		for range count {
			if err := s.payload(element, depth+1); err != nil {
				return err
			}
		}
		return nil
	case 10:
		for {
			child, err := s.byte()
			if err != nil {
				return err
			}
			if child == 0 {
				return nil
			}
			if err = s.string(); err != nil {
				return err
			}
			if err = s.payload(child, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("blueprint NBT contains an invalid tag")
	}
}
