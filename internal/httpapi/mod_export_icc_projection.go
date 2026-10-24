package httpapi

import (
	"bytes"
	"compress/zlib"
	"crypto/md5" // ICC profile IDs are defined by the ICC format as MD5.
	"encoding/binary"
	"errors"
	"io"
	"sort"
)

var errExportICCRegistration = errors.New("unregistered_export_asset: purpose=PNG_ICC_profile; register this color-profile codec before importing")

// ICC names, descriptions, device/creator labels and private tags are metadata,
// not color transforms. Rebuild their container while retaining registered
// transform payload bytes, pixel bytes and required color interpretation.
func projectExportICCChunk(payload []byte) ([]byte, error) {
	separator := bytes.IndexByte(payload, 0)
	if separator < 1 || separator > 79 || separator+2 > len(payload) || payload[separator+1] != 0 {
		return nil, errExportICCRegistration
	}
	reader, err := zlib.NewReader(bytes.NewReader(payload[separator+2:]))
	if err != nil {
		return nil, errExportICCRegistration
	}
	profile, err := io.ReadAll(io.LimitReader(reader, (4<<20)+1))
	closeErr := reader.Close()
	if err != nil || closeErr != nil || len(profile) > 4<<20 {
		return nil, errExportICCRegistration
	}
	profile, err = projectExportICCProfile(profile)
	if err != nil {
		return nil, err
	}
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err = writer.Write(profile); err != nil {
		return nil, err
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	return append([]byte("Catalog color profile\x00\x00"), compressed.Bytes()...), nil
}

func projectExportICCProfile(profile []byte) ([]byte, error) {
	if len(profile) < 132 || int(binary.BigEndian.Uint32(profile[:4])) != len(profile) || string(profile[36:40]) != "acsp" || (profile[8] != 2 && profile[8] != 4) {
		return nil, errExportICCRegistration
	}
	if color := string(profile[16:20]); color != "RGB " && color != "GRAY" {
		return nil, errExportICCRegistration
	}
	if pcs := string(profile[20:24]); pcs != "XYZ " && pcs != "Lab " {
		return nil, errExportICCRegistration
	}
	switch string(profile[12:16]) {
	case "scnr", "mntr", "prtr", "spac":
	default:
		return nil, errExportICCRegistration
	}
	if binary.BigEndian.Uint32(profile[64:68]) > 3 {
		return nil, errExportICCRegistration
	}
	count := int(binary.BigEndian.Uint32(profile[128:132]))
	if count > 1024 || count > (len(profile)-132)/12 {
		return nil, errExportICCRegistration
	}
	tableEnd := 132 + count*12
	tags := map[string][]byte{}
	seen := map[string]bool{}
	for i := 0; i < count; i++ {
		entry := 132 + i*12
		tag := string(profile[entry : entry+4])
		offset := uint64(binary.BigEndian.Uint32(profile[entry+4 : entry+8]))
		size := uint64(binary.BigEndian.Uint32(profile[entry+8 : entry+12]))
		if seen[tag] || offset < uint64(tableEnd) || size < 8 || offset+size > uint64(len(profile)) {
			return nil, errExportICCRegistration
		}
		seen[tag] = true
		data := profile[offset : offset+size]
		switch tag {
		case "desc", "cprt", "dmnd", "dmdd", "vued", "pseq", "psid", "meta":
			// Descriptive/source-chain metadata stays in the private source ZIP.
			continue
		}
		if !registeredExportICCColorTag(tag, data) {
			return nil, errExportICCRegistration
		}
		tags[tag] = append([]byte(nil), data...)
		// All registered ICC tag types reserve these bytes; they carry no
		// transform meaning and must not preserve arbitrary data.
		clear(tags[tag][4:8])
	}
	// Required profile labels use fixed nonidentifying values. This keeps the
	// ICC container valid without publishing descriptions/copyright free text.
	tags["desc"] = exportICCLabel("Catalog color profile", profile[8])
	tags["cprt"] = exportICCLabel("Metadata retained in private source", profile[8])
	if profile[8] == 2 {
		tags["cprt"] = append([]byte("text\x00\x00\x00\x00Metadata retained in private source"), 0)
	}
	result := append([]byte(nil), profile[:128]...)
	clear(result[10:12])
	clear(result[4:8])
	clear(result[24:36])
	binary.BigEndian.PutUint16(result[24:26], 2000)
	binary.BigEndian.PutUint16(result[26:28], 1)
	binary.BigEndian.PutUint16(result[28:30], 1)
	clear(result[40:44])
	clear(result[48:56])
	clear(result[80:128])
	// Reserved flag/attribute bits are not color data.
	flags := binary.BigEndian.Uint32(result[44:48])
	binary.BigEndian.PutUint32(result[44:48], flags&3)
	attributes := binary.BigEndian.Uint64(result[56:64])
	binary.BigEndian.PutUint64(result[56:64], attributes&15)
	keys := make([]string, 0, len(tags))
	for tag := range tags {
		keys = append(keys, tag)
	}
	sort.Strings(keys)
	result = binary.BigEndian.AppendUint32(result, uint32(len(keys)))
	result = append(result, make([]byte, 12*len(keys))...)
	for i, tag := range keys {
		for len(result)%4 != 0 {
			result = append(result, 0)
		}
		offset := len(result)
		result = append(result, tags[tag]...)
		entry := 132 + i*12
		copy(result[entry:entry+4], tag)
		binary.BigEndian.PutUint32(result[entry+4:entry+8], uint32(offset))
		binary.BigEndian.PutUint32(result[entry+8:entry+12], uint32(len(tags[tag])))
	}
	for len(result)%4 != 0 {
		result = append(result, 0)
	}
	binary.BigEndian.PutUint32(result[:4], uint32(len(result)))
	// ICC ID calculation clears flags, intent and profile ID as required by
	// ICC.1; this digest is not used as an upload/content integrity algorithm.
	idInput := append([]byte(nil), result...)
	clear(idInput[44:48])
	clear(idInput[64:68])
	clear(idInput[84:100])
	id := md5.Sum(idInput)
	copy(result[84:100], id[:])
	return result, nil
}

func registeredExportICCColorTag(tag string, data []byte) bool {
	kind, size := string(data[:4]), len(data)
	switch tag {
	case "rXYZ", "gXYZ", "bXYZ", "wtpt", "bkpt", "lumi":
		return kind == "XYZ " && size == 20
	case "rTRC", "gTRC", "bTRC", "kTRC":
		return exportICCCurveLength(data) == size
	case "chad":
		return kind == "sf32" && size == 44
	case "chrm":
		return kind == "chrm" && size >= 12 && size == 12+8*int(binary.BigEndian.Uint16(data[8:10]))
	case "A2B0", "A2B1", "A2B2", "B2A0", "B2A1", "B2A2", "D2B0", "D2B1", "D2B2", "D2B3", "B2D0", "B2D1", "B2D2", "B2D3", "gamt", "pre0", "pre1", "pre2":
		return exportICCLUTLengthValid(data)
	case "vcgt":
		if kind != "vcgt" || size < 18 {
			return false
		}
		gammaType := binary.BigEndian.Uint32(data[8:12])
		if gammaType == 1 {
			return size == 48
		}
		return gammaType == 0 && size == 18+int(binary.BigEndian.Uint16(data[12:14]))*int(binary.BigEndian.Uint16(data[14:16]))*int(binary.BigEndian.Uint16(data[16:18]))
	case "meas":
		return kind == "meas" && size == 36
	case "view":
		return kind == "view" && size == 36
	case "tech":
		return kind == "sig " && size == 12
	}
	return false
}

func exportICCCurveLength(data []byte) int {
	if len(data) < 12 {
		return -1
	}
	if !bytes.Equal(data[4:8], []byte{0, 0, 0, 0}) {
		return -1
	}
	switch string(data[:4]) {
	case "curv":
		n := uint64(binary.BigEndian.Uint32(data[8:12]))
		if n > uint64((len(data)-12)/2) {
			return -1
		}
		return 12 + int(n)*2
	case "para":
		arity := []int{1, 3, 4, 5, 7}
		function := int(binary.BigEndian.Uint16(data[8:10]))
		if function >= len(arity) || data[10] != 0 || data[11] != 0 {
			return -1
		}
		return 12 + 4*arity[function]
	}
	return -1
}

func exportICCLUTLengthValid(data []byte) bool {
	if len(data) < 12 {
		return false
	}
	kind := string(data[:4])
	inputs, outputs := int(data[8]), int(data[9])
	if (inputs != 1 && inputs != 3) || (outputs != 1 && outputs != 3) {
		return false
	}
	if kind == "mft1" || kind == "mft2" {
		if len(data) < 48 {
			return false
		}
		grid := int(data[10])
		if grid < 2 || data[11] != 0 {
			return false
		}
		nodes := 1
		for i := 0; i < inputs; i++ {
			nodes *= grid
		}
		if kind == "mft1" {
			return len(data) == 48+256*inputs+nodes*outputs+256*outputs
		}
		if len(data) < 52 {
			return false
		}
		return len(data) == 52+2*(int(binary.BigEndian.Uint16(data[48:50]))*inputs+nodes*outputs+int(binary.BigEndian.Uint16(data[50:52]))*outputs)
	}
	if (kind != "mAB " && kind != "mBA ") || len(data) < 32 || data[10] != 0 || data[11] != 0 {
		return false
	}
	used := make([]bool, len(data))
	for i := 0; i < 32; i++ {
		used[i] = true
	}
	for section := 0; section < 5; section++ {
		offset := int(binary.BigEndian.Uint32(data[12+4*section : 16+4*section]))
		if offset == 0 {
			continue
		}
		if offset < 32 || offset >= len(data) || offset%4 != 0 {
			return false
		}
		end := offset
		switch section {
		case 1:
			end += 48 // Matrix coefficients and offsets.
		case 3:
			if len(data)-offset < 20 {
				return false
			}
			precision := int(data[offset+16])
			if precision != 1 && precision != 2 {
				return false
			}
			if data[offset+17] != 0 || data[offset+18] != 0 || data[offset+19] != 0 {
				return false
			}
			nodes := 1
			for i := 0; i < inputs; i++ {
				grid := int(data[offset+i])
				if grid < 2 {
					return false
				}
				nodes *= grid
			}
			for i := inputs; i < 16; i++ {
				if data[offset+i] != 0 {
					return false
				}
			}
			end += 20 + nodes*outputs*precision
		default:
			curves := outputs
			if section == 4 {
				curves = inputs
			}
			if kind == "mBA " {
				curves = inputs
				if section == 4 {
					curves = outputs
				}
			}
			for i := 0; i < curves; i++ {
				if end >= len(data) {
					return false
				}
				size := exportICCCurveLength(data[end:])
				if size < 0 || size > len(data)-end {
					return false
				}
				end += size
				if i+1 < curves {
					aligned := (end + 3) &^ 3
					if aligned > len(data) {
						return false
					}
					for _, padding := range data[end:aligned] {
						if padding != 0 {
							return false
						}
					}
					end = aligned
				}
			}
		}
		if end > len(data) {
			return false
		}
		for i := offset; i < end; i++ {
			if used[i] {
				return false
			}
			used[i] = true
		}
	}
	for i, ok := range used {
		if !ok && data[i] != 0 {
			return false
		}
	}
	return true
}

func exportICCLabel(text string, version byte) []byte {
	if version == 2 {
		data := append([]byte("desc\x00\x00\x00\x00"), make([]byte, 4)...)
		binary.BigEndian.PutUint32(data[8:12], uint32(len(text)+1))
		data = append(data, []byte(text)...)
		data = append(data, 0)
		return append(data, make([]byte, 78)...)
	}
	// multiLocalizedUnicodeType: one en-US record, UTF-16BE label.
	data := []byte("mluc\x00\x00\x00\x00")
	data = binary.BigEndian.AppendUint32(data, 1)
	data = binary.BigEndian.AppendUint32(data, 12)
	data = append(data, []byte("enUS")...)
	data = binary.BigEndian.AppendUint32(data, uint32(2*len(text)))
	data = binary.BigEndian.AppendUint32(data, 28)
	for _, b := range []byte(text) {
		data = append(data, 0, b)
	}
	return data
}
