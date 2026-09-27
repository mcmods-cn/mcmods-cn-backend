package serverprobe

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

func parseConfigurationDynamicRegistry(data []byte) (string, []string, error) {
	return parseConfigurationDynamicRegistryWithBudget(data, newConfigurationParseBudget())
}

func parseConfigurationDynamicRegistryWithBudget(data []byte, budget *configurationParseBudget) (string, []string, error) {
	reader := bytes.NewReader(data)
	registryName, err := readProtocolString(reader, maxConfigurationPacketBytes)
	if err != nil {
		return "", nil, err
	}
	if err = budget.claim("dynamic registry name", 1, reader.Len(), 1); err != nil {
		return "", nil, err
	}
	count, err := readVarInt(reader)
	if err != nil || budget.claim("dynamic registry entry", count, reader.Len(), 2) != nil {
		return "", nil, fmt.Errorf("invalid dynamic registry entry count %d", count)
	}
	entries := make([]string, 0, min(int(count), 256))
	for index := int32(0); index < count; index++ {
		id, readErr := readProtocolString(reader, maxConfigurationPacketBytes)
		if readErr != nil {
			return "", nil, readErr
		}
		entries = append(entries, id)
		present, readErr := reader.ReadByte()
		if readErr != nil {
			return "", nil, readErr
		}
		if present != 0 {
			if readErr = skipConfigurationNBT(reader); readErr != nil {
				return "", nil, readErr
			}
		}
	}
	if reader.Len() != 0 {
		return "", nil, fmt.Errorf("%d trailing bytes in dynamic registry", reader.Len())
	}
	return registryName, entries, nil
}

func skipConfigurationNBT(reader *bytes.Reader) error {
	tagType, err := reader.ReadByte()
	if err != nil {
		return err
	}
	remainingNodes := maximumConfigurationNBTNodes
	return skipConfigurationNBTPayload(reader, tagType, 0, &remainingNodes)
}

func skipConfigurationNBTPayload(reader *bytes.Reader, tagType byte, depth int, remainingNodes *int) error {
	if depth > 64 {
		return errors.New("NBT depth exceeds 64")
	}
	if remainingNodes == nil || *remainingNodes <= 0 {
		return errors.New("NBT node count exceeds safety limit")
	}
	*remainingNodes--
	switch tagType {
	case 0:
		return nil
	case 1:
		return skipConfigurationBytes(reader, 1)
	case 2:
		return skipConfigurationBytes(reader, 2)
	case 3, 5:
		return skipConfigurationBytes(reader, 4)
	case 4, 6:
		return skipConfigurationBytes(reader, 8)
	case 7:
		length, err := readConfigurationInt32(reader)
		if err != nil {
			return err
		}
		return skipConfigurationArray(reader, length, 1)
	case 8:
		_, err := readConfigurationNBTString(reader)
		return err
	case 9:
		elementType, err := reader.ReadByte()
		if err != nil {
			return err
		}
		length, err := readConfigurationInt32(reader)
		if err != nil {
			return err
		}
		if length < 0 || length > maximumConfigurationNBTNodes || int(length) > *remainingNodes {
			return fmt.Errorf("invalid NBT list length %d", length)
		}
		for index := int32(0); index < length; index++ {
			if err = skipConfigurationNBTPayload(reader, elementType, depth+1, remainingNodes); err != nil {
				return err
			}
		}
		return nil
	case 10:
		for {
			childType, err := reader.ReadByte()
			if err != nil {
				return err
			}
			if childType == 0 {
				return nil
			}
			if _, err = readConfigurationNBTString(reader); err != nil {
				return err
			}
			if err = skipConfigurationNBTPayload(reader, childType, depth+1, remainingNodes); err != nil {
				return err
			}
		}
	case 11:
		length, err := readConfigurationInt32(reader)
		if err != nil {
			return err
		}
		return skipConfigurationArray(reader, length, 4)
	case 12:
		length, err := readConfigurationInt32(reader)
		if err != nil {
			return err
		}
		return skipConfigurationArray(reader, length, 8)
	default:
		return fmt.Errorf("unknown NBT tag type %d", tagType)
	}
}

func readConfigurationNBTString(reader *bytes.Reader) (string, error) {
	var length uint16
	if err := binary.Read(reader, binary.BigEndian, &length); err != nil {
		return "", err
	}
	data := make([]byte, int(length))
	if _, err := io.ReadFull(reader, data); err != nil {
		return "", err
	}
	return string(data), nil
}

func readConfigurationInt32(reader *bytes.Reader) (int32, error) {
	var value int32
	err := binary.Read(reader, binary.BigEndian, &value)
	return value, err
}

func skipConfigurationArray(reader *bytes.Reader, count int32, elementSize int64) error {
	if count < 0 || int64(count) > int64(maxConfigurationPacketBytes)/elementSize {
		return fmt.Errorf("invalid NBT array length %d", count)
	}
	return skipConfigurationBytes(reader, int64(count)*elementSize)
}

func skipConfigurationBytes(reader *bytes.Reader, count int64) error {
	if count < 0 || count > int64(reader.Len()) {
		return io.ErrUnexpectedEOF
	}
	_, err := reader.Seek(count, io.SeekCurrent)
	return err
}
