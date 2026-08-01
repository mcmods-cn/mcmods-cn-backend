package serverprobe

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

const maxForgeStatusPayload = 4 << 20

type forgeChannel struct {
	Resource string
	Version  string
	Required bool
}

type forgeModMarker struct {
	ModID    string
	Marker   string
	Channels []forgeChannel
}

type forgeStatusSummary struct {
	Detected  bool
	Truncated bool
	Mods      []forgeModMarker
}

// summarizeForgeStatus supports both legacy modinfo and Forge's compact
// forgeData.d representation. The compact decoder is adapted from the parser
// supplied with the server feature specification.
func summarizeForgeStatus(payload []byte) (forgeStatusSummary, error) {
	var status struct {
		ForgeData *struct {
			FMLNetworkVersion int             `json:"fmlNetworkVersion"`
			D                 json.RawMessage `json:"d"`
			Mods              []struct {
				ModID     string `json:"modId"`
				ModMarker string `json:"modmarker"`
			} `json:"mods"`
			Truncated bool `json:"truncated"`
		} `json:"forgeData"`
		ModInfo *struct {
			ModList []struct {
				ModID   string `json:"modid"`
				Version string `json:"version"`
			} `json:"modList"`
		} `json:"modinfo"`
	}
	if err := json.Unmarshal(payload, &status); err != nil {
		return forgeStatusSummary{}, err
	}
	if status.ForgeData != nil {
		result := forgeStatusSummary{Detected: true, Truncated: status.ForgeData.Truncated}
		if len(status.ForgeData.D) != 0 && string(status.ForgeData.D) != "null" {
			units, err := rawJSONStringUTF16(status.ForgeData.D)
			if err != nil {
				return forgeStatusSummary{}, err
			}
			data, err := decodeForgeOptimized(units)
			if err != nil {
				return forgeStatusSummary{}, err
			}
			mods, truncated, err := parseForgeModData(data, status.ForgeData.FMLNetworkVersion)
			if err != nil {
				return forgeStatusSummary{}, err
			}
			result.Mods, result.Truncated = mods, truncated
			return result, nil
		}
		for _, mod := range status.ForgeData.Mods {
			result.Mods = append(result.Mods, forgeModMarker{ModID: mod.ModID, Marker: mod.ModMarker})
		}
		return result, nil
	}
	if status.ModInfo != nil {
		result := forgeStatusSummary{Detected: true}
		for _, mod := range status.ModInfo.ModList {
			result.Mods = append(result.Mods, forgeModMarker{ModID: mod.ModID, Marker: mod.Version})
		}
		return result, nil
	}
	return forgeStatusSummary{}, nil
}

func rawJSONStringUTF16(raw json.RawMessage) ([]uint16, error) {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return nil, errors.New("value is not a JSON string")
	}
	units := make([]uint16, 0, len(raw))
	for index := 1; index < len(raw)-1; {
		if raw[index] == '\\' {
			index++
			if index >= len(raw)-1 {
				return nil, io.ErrUnexpectedEOF
			}
			if raw[index] == 'u' {
				if index+4 >= len(raw) {
					return nil, io.ErrUnexpectedEOF
				}
				value, err := strconv.ParseUint(string(raw[index+1:index+5]), 16, 16)
				if err != nil {
					return nil, err
				}
				units = append(units, uint16(value))
				index += 5
				continue
			}
			escapes := map[byte]uint16{
				'"': '"', '\\': '\\', '/': '/', 'b': '\b', 'f': '\f',
				'n': '\n', 'r': '\r', 't': '\t',
			}
			value, ok := escapes[raw[index]]
			if !ok {
				return nil, fmt.Errorf("unsupported JSON escape %q", raw[index])
			}
			units = append(units, value)
			index++
			continue
		}
		value, size := utf8.DecodeRune(raw[index : len(raw)-1])
		if value == utf8.RuneError && size == 1 {
			return nil, errors.New("invalid UTF-8 in JSON string")
		}
		if value <= 0xffff {
			units = append(units, uint16(value))
		} else {
			units = append(units, utf16.Encode([]rune{value})...)
		}
		index += size
	}
	return units, nil
}

func decodeForgeOptimized(units []uint16) ([]byte, error) {
	if len(units) < 2 {
		return nil, errors.New("optimized Forge data is too short")
	}
	size := int(units[0]&0x7fff) | int(units[1]&0x7fff)<<15
	if size < 0 || size > maxForgeStatusPayload {
		return nil, fmt.Errorf("invalid decoded size %d", size)
	}
	result := make([]byte, 0, size)
	var bitBuffer uint32
	var bitCount uint
	for _, unit := range units[2:] {
		bitBuffer |= uint32(unit&0x7fff) << bitCount
		bitCount += 15
		for bitCount >= 8 && len(result) < size {
			result = append(result, byte(bitBuffer))
			bitBuffer >>= 8
			bitCount -= 8
		}
	}
	for len(result) < size {
		result = append(result, byte(bitBuffer))
		bitBuffer >>= 8
	}
	return result, nil
}

func parseForgeModData(data []byte, fmlNetworkVersion int) ([]forgeModMarker, bool, error) {
	reader := bytes.NewReader(data)
	truncated, err := reader.ReadByte()
	if err != nil {
		return nil, false, err
	}
	var modCount uint16
	if err = binary.Read(reader, binary.BigEndian, &modCount); err != nil {
		return nil, false, err
	}
	mods := make([]forgeModMarker, 0, modCount)
	for modIndex := 0; modIndex < int(modCount); modIndex++ {
		flags, readErr := readVarInt(reader)
		if readErr != nil {
			return nil, false, readErr
		}
		channelCount := int(uint32(flags) >> 1)
		if channelCount > 65535 {
			return nil, false, errors.New("invalid Forge channel count")
		}
		modID, readErr := readProtocolString(reader, maxForgeStatusPayload)
		if readErr != nil {
			return nil, false, readErr
		}
		marker := "IGNORESERVERONLY"
		if flags&1 == 0 {
			marker, readErr = readProtocolString(reader, maxForgeStatusPayload)
			if readErr != nil {
				return nil, false, readErr
			}
		}
		mod := forgeModMarker{ModID: modID, Marker: marker}
		for channelIndex := 0; channelIndex < channelCount; channelIndex++ {
			path, channelErr := readProtocolString(reader, maxForgeStatusPayload)
			if channelErr != nil {
				return nil, false, channelErr
			}
			version, channelErr := readForgeChannelVersion(reader, fmlNetworkVersion)
			if channelErr != nil {
				return nil, false, channelErr
			}
			required, channelErr := reader.ReadByte()
			if channelErr != nil {
				return nil, false, channelErr
			}
			mod.Channels = append(mod.Channels, forgeChannel{
				Resource: modID + ":" + path, Version: version, Required: required != 0,
			})
		}
		mods = append(mods, mod)
	}
	nonModCount, err := readVarInt(reader)
	if err != nil || nonModCount < 0 || nonModCount > 65535 {
		return nil, false, errors.New("invalid non-Mod channel count")
	}
	for index := int32(0); index < nonModCount; index++ {
		if _, err = readProtocolString(reader, maxForgeStatusPayload); err != nil {
			return nil, false, err
		}
		if _, err = readForgeChannelVersion(reader, fmlNetworkVersion); err != nil {
			return nil, false, err
		}
		if _, err = reader.ReadByte(); err != nil {
			return nil, false, err
		}
	}
	if reader.Len() != 0 {
		return nil, false, fmt.Errorf("%d trailing Forge bytes", reader.Len())
	}
	return mods, truncated != 0, nil
}

func readForgeChannelVersion(reader *bytes.Reader, fmlNetworkVersion int) (string, error) {
	if fmlNetworkVersion == 3 {
		return readProtocolString(reader, maxForgeStatusPayload)
	}
	version, err := readVarInt(reader)
	return strconv.FormatInt(int64(version), 10), err
}
