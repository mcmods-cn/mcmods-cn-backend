package httpapi

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Tnze/go-mc/nbt"
)

const (
	blueprintSchemaVersion       = "mcmods-blueprint/v1"
	maxBlueprintDecodedNBTBytes  = int64(256 << 20)
	maxBlueprintDimension        = 4096
	maxBlueprintVolume           = 16 << 20
	maxBlueprintNonAirBlockCount = 8 << 20
)

type blueprintDocument struct {
	SchemaVersion string           `json:"schemaVersion"`
	Name          string           `json:"name"`
	SourceFormat  string           `json:"sourceFormat"`
	DataVersion   int              `json:"dataVersion,omitempty"`
	Size          [3]int           `json:"size"`
	Blocks        []blueprintBlock `json:"blocks"`
	BlockEntities []map[string]any `json:"blockEntities,omitempty"`
	Entities      []map[string]any `json:"entities,omitempty"`
	Warnings      []string         `json:"warnings,omitempty"`
}

type blueprintBlock struct {
	Position [3]int              `json:"position"`
	State    blueprintBlockState `json:"state"`
}

type blueprintBlockState struct {
	ID         string            `json:"id"`
	Properties map[string]string `json:"properties,omitempty"`
}

type blueprintMaterial struct {
	State      string
	BlockID    string
	Properties map[string]string
	Count      int64
}

func decodeBlueprint(data []byte, format, name string) (blueprintDocument, error) {
	format = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(format)), ".")
	if format == "json" {
		var document blueprintDocument
		if err := json.Unmarshal(data, &document); err != nil {
			return blueprintDocument{}, err
		}
		if document.SchemaVersion != blueprintSchemaVersion {
			return blueprintDocument{}, fmt.Errorf("unsupported normalized blueprint schema %q", document.SchemaVersion)
		}
		document = finalizeBlueprint(document, name, format)
		return document, validateBlueprintDocument(document)
	}
	root, err := decodeNBTMap(data)
	if err != nil {
		return blueprintDocument{}, err
	}
	var document blueprintDocument
	switch format {
	case "schem":
		document, err = decodeSpongeSchematic(root)
	case "litematic":
		document, err = decodeLitematic(root)
	case "schematic":
		document, err = decodeLegacySchematic(root)
	case "nbt":
		document, err = decodeVanillaStructure(root)
	default:
		return blueprintDocument{}, fmt.Errorf("unsupported blueprint format %q", format)
	}
	if err != nil {
		return blueprintDocument{}, err
	}
	document.SourceFormat = format
	document = finalizeBlueprint(document, name, format)
	return document, validateBlueprintDocument(document)
}

func finalizeBlueprint(document blueprintDocument, name, format string) blueprintDocument {
	document.SchemaVersion = blueprintSchemaVersion
	if strings.TrimSpace(document.Name) == "" {
		document.Name = strings.TrimSpace(name)
	}
	if document.SourceFormat == "" {
		document.SourceFormat = format
	}
	blocks := make([]blueprintBlock, 0, len(document.Blocks))
	for _, block := range document.Blocks {
		if block.State.ID != "" && block.State.ID != "minecraft:air" && block.State.ID != "air" {
			blocks = append(blocks, block)
		}
	}
	document.Blocks = blocks
	return document
}

func decodeNBTMap(data []byte) (map[string]any, error) {
	reader := io.Reader(bytes.NewReader(data))
	var closer io.Closer
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		compressed, err := gzip.NewReader(reader)
		if err != nil {
			return nil, err
		}
		reader, closer = compressed, compressed
	} else if len(data) >= 2 && data[0] == 0x78 {
		compressed, err := zlib.NewReader(reader)
		if err != nil {
			return nil, err
		}
		reader, closer = compressed, compressed
	}
	if closer != nil {
		defer closer.Close()
	}
	limited := &io.LimitedReader{R: reader, N: maxBlueprintDecodedNBTBytes + 1}
	result := map[string]any{}
	if _, err := nbt.NewDecoder(limited).Decode(&result); err != nil {
		return nil, err
	}
	if limited.N <= 0 {
		return nil, errors.New("decoded blueprint NBT exceeds processing limit")
	}
	return result, nil
}

func validateBlueprintDocument(document blueprintDocument) error {
	if _, err := checkedBlueprintVolume(document.Size); err != nil {
		return err
	}
	if document.DataVersion < 0 || int64(document.DataVersion) > math.MaxInt32 {
		return errors.New("blueprint data version is out of range")
	}
	if len(document.Blocks) > maxBlueprintNonAirBlockCount {
		return errors.New("blueprint contains too many non-air blocks")
	}
	for _, block := range document.Blocks {
		for axis, coordinate := range block.Position {
			if coordinate < 0 || coordinate >= document.Size[axis] {
				return errors.New("blueprint contains a block outside its declared dimensions")
			}
		}
	}
	return nil
}

func checkedBlueprintVolume(size [3]int) (int, error) {
	volume := 1
	for _, dimension := range size {
		if dimension <= 0 || dimension > maxBlueprintDimension {
			return 0, errors.New("blueprint dimensions exceed the processing limit")
		}
		if volume > maxBlueprintVolume/dimension {
			return 0, errors.New("blueprint volume exceeds the processing limit")
		}
		volume *= dimension
	}
	return volume, nil
}

func decodeVanillaStructure(root map[string]any) (blueprintDocument, error) {
	size := intTriplet(root["size"])
	paletteValues := anySlice(root["palette"])
	if len(paletteValues) == 0 {
		palettes := anySlice(root["palettes"])
		if len(palettes) > 0 {
			paletteValues = anySlice(palettes[0])
		}
	}
	palette := make([]blueprintBlockState, 0, len(paletteValues))
	for _, raw := range paletteValues {
		palette = append(palette, blockStateFromMap(anyMap(raw)))
	}
	if len(palette) == 0 {
		return blueprintDocument{}, errors.New("structure palette is empty")
	}
	if _, err := checkedBlueprintVolume(size); err != nil {
		return blueprintDocument{}, err
	}
	document := blueprintDocument{Size: size, DataVersion: intValue(root["DataVersion"])}
	for _, raw := range anySlice(root["blocks"]) {
		block := anyMap(raw)
		stateIndex := intValue(block["state"])
		if stateIndex < 0 || stateIndex >= len(palette) {
			continue
		}
		state := palette[stateIndex]
		if state.ID == "" || state.ID == "minecraft:air" || state.ID == "air" {
			continue
		}
		if len(document.Blocks) >= maxBlueprintNonAirBlockCount {
			return blueprintDocument{}, errors.New("blueprint contains too many non-air blocks")
		}
		document.Blocks = append(document.Blocks, blueprintBlock{Position: intTriplet(block["pos"]), State: state})
		if entity := anyMap(block["nbt"]); len(entity) > 0 {
			document.BlockEntities = append(document.BlockEntities, entity)
		}
	}
	for _, raw := range anySlice(root["entities"]) {
		if entity := anyMap(raw); len(entity) > 0 {
			document.Entities = append(document.Entities, entity)
		}
	}
	return document, nil
}

func decodeSpongeSchematic(root map[string]any) (blueprintDocument, error) {
	if schematic := anyMap(root["Schematic"]); len(schematic) > 0 {
		root = schematic
	}
	size := [3]int{intValue(root["Width"]), intValue(root["Height"]), intValue(root["Length"])}
	blocksRoot := anyMap(root["Blocks"])
	paletteRaw := anyMap(root["Palette"])
	dataRaw := root["BlockData"]
	if len(blocksRoot) > 0 {
		paletteRaw = anyMap(blocksRoot["Palette"])
		dataRaw = blocksRoot["Data"]
	}
	if size[0] <= 0 || size[1] <= 0 || size[2] <= 0 || len(paletteRaw) == 0 {
		return blueprintDocument{}, errors.New("schematic dimensions or palette are missing")
	}
	volume, err := checkedBlueprintVolume(size)
	if err != nil {
		return blueprintDocument{}, err
	}
	maxPalette := 0
	for _, value := range paletteRaw {
		if index := intValue(value); index > maxPalette {
			maxPalette = index
		}
	}
	palette := make([]blueprintBlockState, maxPalette+1)
	for state, value := range paletteRaw {
		palette[intValue(value)] = parseBlockState(state)
	}
	indices, err := decodeVarInts(byteSlice(dataRaw), volume)
	if err != nil {
		return blueprintDocument{}, err
	}
	document := blueprintDocument{Size: size, DataVersion: intValue(root["DataVersion"])}
	for linear, paletteIndex := range indices {
		if paletteIndex < 0 || paletteIndex >= len(palette) {
			continue
		}
		state := palette[paletteIndex]
		if state.ID == "" || state.ID == "minecraft:air" || state.ID == "air" {
			continue
		}
		if len(document.Blocks) >= maxBlueprintNonAirBlockCount {
			return blueprintDocument{}, errors.New("blueprint contains too many non-air blocks")
		}
		x := linear % size[0]
		z := (linear / size[0]) % size[2]
		y := linear / (size[0] * size[2])
		document.Blocks = append(document.Blocks, blueprintBlock{Position: [3]int{x, y, z}, State: state})
	}
	return document, nil
}

func decodeLitematic(root map[string]any) (blueprintDocument, error) {
	regions := anyMap(root["Regions"])
	if len(regions) == 0 {
		return blueprintDocument{}, errors.New("litematic contains no regions")
	}
	document := blueprintDocument{DataVersion: intValue(root["MinecraftDataVersion"])}
	min := [3]int{math.MaxInt, math.MaxInt, math.MaxInt}
	max := [3]int{math.MinInt, math.MinInt, math.MinInt}
	type regionData struct {
		origin [3]int
		size   [3]int
		step   [3]int
		states []blueprintBlockState
		packed []int64
	}
	parsed := make([]regionData, 0, len(regions))
	for _, raw := range regions {
		region := anyMap(raw)
		origin := coordinateTriplet(region["Position"])
		rawSize := coordinateTriplet(region["Size"])
		size := [3]int{absInt(rawSize[0]), absInt(rawSize[1]), absInt(rawSize[2])}
		step := [3]int{signInt(rawSize[0]), signInt(rawSize[1]), signInt(rawSize[2])}
		if size[0] == 0 || size[1] == 0 || size[2] == 0 {
			continue
		}
		if _, err := checkedBlueprintVolume(size); err != nil {
			return blueprintDocument{}, err
		}
		states := make([]blueprintBlockState, 0)
		for _, state := range anySlice(region["BlockStatePalette"]) {
			states = append(states, blockStateFromMap(anyMap(state)))
		}
		if len(states) == 0 {
			continue
		}
		parsed = append(parsed, regionData{origin: origin, size: size, step: step, states: states, packed: longSlice(region["BlockStates"])})
		for axis := 0; axis < 3; axis++ {
			end := origin[axis] + step[axis]*(size[axis]-1)
			if minInt(origin[axis], end) < min[axis] {
				min[axis] = minInt(origin[axis], end)
			}
			if maxInt(origin[axis], end) > max[axis] {
				max[axis] = maxInt(origin[axis], end)
			}
		}
	}
	if len(parsed) == 0 {
		return blueprintDocument{}, errors.New("litematic regions contain no palette data")
	}
	document.Size = [3]int{max[0] - min[0] + 1, max[1] - min[1] + 1, max[2] - min[2] + 1}
	if _, err := checkedBlueprintVolume(document.Size); err != nil {
		return blueprintDocument{}, err
	}
	for _, region := range parsed {
		bits := maxInt(2, int(math.Ceil(math.Log2(float64(len(region.states))))))
		total, err := checkedBlueprintVolume(region.size)
		if err != nil {
			return blueprintDocument{}, err
		}
		for linear := 0; linear < total; linear++ {
			paletteIndex := packedValue(region.packed, linear, bits)
			if paletteIndex < 0 || paletteIndex >= len(region.states) {
				continue
			}
			state := region.states[paletteIndex]
			if state.ID == "" || state.ID == "minecraft:air" || state.ID == "air" {
				continue
			}
			if len(document.Blocks) >= maxBlueprintNonAirBlockCount {
				return blueprintDocument{}, errors.New("blueprint contains too many non-air blocks")
			}
			x := linear % region.size[0]
			z := (linear / region.size[0]) % region.size[2]
			y := linear / (region.size[0] * region.size[2])
			document.Blocks = append(document.Blocks, blueprintBlock{
				Position: [3]int{region.origin[0] + x*region.step[0] - min[0], region.origin[1] + y*region.step[1] - min[1], region.origin[2] + z*region.step[2] - min[2]},
				State:    state,
			})
		}
	}
	return document, nil
}

func decodeLegacySchematic(root map[string]any) (blueprintDocument, error) {
	size := [3]int{intValue(root["Width"]), intValue(root["Height"]), intValue(root["Length"])}
	blocks := byteSlice(root["Blocks"])
	metadata := byteSlice(root["Data"])
	if size[0] <= 0 || size[1] <= 0 || size[2] <= 0 || len(blocks) == 0 {
		return blueprintDocument{}, errors.New("legacy schematic dimensions or block data are missing")
	}
	volume, err := checkedBlueprintVolume(size)
	if err != nil {
		return blueprintDocument{}, err
	}
	document := blueprintDocument{Size: size}
	limit := minInt(len(blocks), volume)
	for linear := 0; linear < limit; linear++ {
		id := int(blocks[linear])
		if id == 0 {
			continue
		}
		if len(document.Blocks) >= maxBlueprintNonAirBlockCount {
			return blueprintDocument{}, errors.New("blueprint contains too many non-air blocks")
		}
		data := 0
		if linear < len(metadata) {
			data = int(metadata[linear])
		}
		x := linear % size[0]
		z := (linear / size[0]) % size[2]
		y := linear / (size[0] * size[2])
		document.Blocks = append(document.Blocks, blueprintBlock{
			Position: [3]int{x, y, z},
			State:    blueprintBlockState{ID: fmt.Sprintf("legacy:%d", id), Properties: map[string]string{"data": strconv.Itoa(data)}},
		})
	}
	document.Warnings = append(document.Warnings, "Legacy numeric block IDs cannot always be mapped to modern resource locations.")
	return document, nil
}

func encodeBlueprint(document blueprintDocument, format string) ([]byte, string, error) {
	if err := validateBlueprintDocument(document); err != nil {
		return nil, "", err
	}
	format = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(format)), ".")
	switch format {
	case "json":
		raw, err := json.Marshal(document)
		return raw, "application/json", err
	case "nbt":
		return encodeVanillaStructure(document)
	case "schem":
		return encodeSpongeSchematic(document)
	case "litematic":
		return encodeLitematic(document)
	default:
		return nil, "", fmt.Errorf("unsupported target format %q", format)
	}
}

func encodeLitematic(document blueprintDocument) ([]byte, string, error) {
	volume, err := checkedBlueprintVolume(document.Size)
	if err != nil {
		return nil, "", err
	}
	palette := []blueprintBlockState{{ID: "minecraft:air"}}
	lookup := map[string]int{"minecraft:air": 0}
	indices := make([]int, volume)
	for _, block := range document.Blocks {
		x, y, z := block.Position[0], block.Position[1], block.Position[2]
		if x < 0 || y < 0 || z < 0 || x >= document.Size[0] || y >= document.Size[1] || z >= document.Size[2] {
			continue
		}
		key := formatBlockState(block.State)
		paletteIndex, exists := lookup[key]
		if !exists {
			paletteIndex = len(palette)
			lookup[key] = paletteIndex
			palette = append(palette, block.State)
		}
		indices[x+z*document.Size[0]+y*document.Size[0]*document.Size[2]] = paletteIndex
	}
	paletteNBT := make([]map[string]any, 0, len(palette))
	for _, state := range palette {
		entry := map[string]any{"Name": state.ID}
		if len(state.Properties) > 0 {
			entry["Properties"] = state.Properties
		}
		paletteNBT = append(paletteNBT, entry)
	}
	bits := maxInt(2, int(math.Ceil(math.Log2(float64(len(palette))))))
	packed := packLitematicValues(indices, bits)
	regionSize := map[string]any{"x": blueprintInt32(document.Size[0]), "y": blueprintInt32(document.Size[1]), "z": blueprintInt32(document.Size[2])}
	region := map[string]any{
		"BlockStatePalette": paletteNBT,
		"BlockStates":       packed,
		"Entities":          []map[string]any{},
		"PendingBlockTicks": []map[string]any{},
		"PendingFluidTicks": []map[string]any{},
		"Position":          map[string]any{"x": int32(0), "y": int32(0), "z": int32(0)},
		"Size":              regionSize,
		"TileEntities":      []map[string]any{},
	}
	now := time.Now().UnixMilli()
	name := strings.TrimSpace(document.Name)
	if name == "" {
		name = "Mcmods blueprint"
	}
	root := map[string]any{
		"Version":              int32(6),
		"SubVersion":           int32(1),
		"MinecraftDataVersion": blueprintInt32(document.DataVersion),
		"Metadata": map[string]any{
			"Author":        "mcmods.cn",
			"Description":   "Converted by mcmods.cn",
			"EnclosingSize": regionSize,
			"Name":          name,
			"RegionCount":   int32(1),
			"TimeCreated":   now,
			"TimeModified":  now,
			"TotalBlocks":   blueprintInt32(len(document.Blocks)),
			"TotalVolume":   blueprintInt32(volume),
		},
		"Regions": map[string]any{"Main": region},
	}
	return encodeGzipNBT(root, "Litematic")
}

func packLitematicValues(values []int, bits int) []int64 {
	if len(values) == 0 || bits <= 0 || bits > 63 {
		return []int64{}
	}
	result := make([]int64, (len(values)*bits+63)/64)
	mask := uint64((uint64(1) << bits) - 1)
	for index, raw := range values {
		if raw < 0 {
			continue
		}
		value := uint64(raw) & mask // #nosec G115 -- non-negative palette index is deliberately packed as unsigned bits.
		bitIndex := index * bits
		word := bitIndex / 64
		offset := bitIndex % 64
		result[word] = int64(uint64(result[word]) | value<<offset) // #nosec G115 -- NBT long arrays carry packed unsigned bit patterns in signed int64 words.
		if offset+bits > 64 {
			result[word+1] = int64(uint64(result[word+1]) | value>>(64-offset)) // #nosec G115 -- intentional packed-bit reinterpretation.
		}
	}
	return result
}

func encodeVanillaStructure(document blueprintDocument) ([]byte, string, error) {
	palette, indices := buildPalette(document)
	paletteNBT := make([]map[string]any, 0, len(palette))
	for _, state := range palette {
		entry := map[string]any{"Name": state.ID}
		if len(state.Properties) > 0 {
			entry["Properties"] = state.Properties
		}
		paletteNBT = append(paletteNBT, entry)
	}
	blocks := make([]map[string]any, 0, len(document.Blocks))
	for index, block := range document.Blocks {
		blocks = append(blocks, map[string]any{"pos": []int32{blueprintInt32(block.Position[0]), blueprintInt32(block.Position[1]), blueprintInt32(block.Position[2])}, "state": blueprintInt32(indices[index])})
	}
	root := map[string]any{
		"DataVersion": blueprintInt32(document.DataVersion),
		"size":        []int32{blueprintInt32(document.Size[0]), blueprintInt32(document.Size[1]), blueprintInt32(document.Size[2])},
		"palette":     paletteNBT,
		"blocks":      blocks,
		"entities":    []map[string]any{},
	}
	return encodeGzipNBT(root, "")
}

func encodeSpongeSchematic(document blueprintDocument) ([]byte, string, error) {
	if document.Size[0] > math.MaxInt16 || document.Size[1] > math.MaxInt16 || document.Size[2] > math.MaxInt16 {
		return nil, "", errors.New("schematic dimensions exceed the format limit")
	}
	palette := []blueprintBlockState{{ID: "minecraft:air"}}
	indices := make([]int, len(document.Blocks))
	lookup := map[string]int{"minecraft:air": 0}
	for index, block := range document.Blocks {
		key := formatBlockState(block.State)
		paletteIndex, exists := lookup[key]
		if !exists {
			paletteIndex = len(palette)
			lookup[key] = paletteIndex
			palette = append(palette, block.State)
		}
		indices[index] = paletteIndex
	}
	paletteNBT := make(map[string]any, len(palette))
	for index, state := range palette {
		paletteNBT[formatBlockState(state)] = int32(index)
	}
	data := make([]byte, document.Size[0]*document.Size[1]*document.Size[2])
	encoded := make([]byte, 0, len(data))
	indexByPosition := make(map[[3]int]int, len(document.Blocks))
	for index, block := range document.Blocks {
		indexByPosition[block.Position] = indices[index]
	}
	for y := 0; y < document.Size[1]; y++ {
		for z := 0; z < document.Size[2]; z++ {
			for x := 0; x < document.Size[0]; x++ {
				encoded = appendVarInt(encoded, indexByPosition[[3]int{x, y, z}])
			}
		}
	}
	root := map[string]any{
		"Version":     int32(3),
		"DataVersion": blueprintInt32(document.DataVersion),
		"Width":       blueprintInt16(document.Size[0]),
		"Height":      blueprintInt16(document.Size[1]),
		"Length":      blueprintInt16(document.Size[2]),
		"Blocks": map[string]any{
			"Palette": paletteNBT,
			"Data":    encoded,
		},
	}
	return encodeGzipNBT(root, "Schematic")
}

func encodeGzipNBT(root map[string]any, rootName string) ([]byte, string, error) {
	var output bytes.Buffer
	compressed := gzip.NewWriter(&output)
	if err := nbt.NewEncoder(compressed).Encode(root, rootName); err != nil {
		return nil, "", errors.Join(err, compressed.Close())
	}
	if err := compressed.Close(); err != nil {
		return nil, "", err
	}
	return output.Bytes(), "application/octet-stream", nil
}

func blueprintMaterials(document blueprintDocument) []blueprintMaterial {
	counts := make(map[string]*blueprintMaterial)
	for _, block := range document.Blocks {
		state := formatBlockState(block.State)
		material := counts[state]
		if material == nil {
			material = &blueprintMaterial{State: state, BlockID: block.State.ID, Properties: block.State.Properties}
			counts[state] = material
		}
		material.Count++
	}
	result := make([]blueprintMaterial, 0, len(counts))
	for _, material := range counts {
		result = append(result, *material)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Count == result[right].Count {
			return result[left].State < result[right].State
		}
		return result[left].Count > result[right].Count
	})
	return result
}

func buildPalette(document blueprintDocument) ([]blueprintBlockState, []int) {
	palette := make([]blueprintBlockState, 0)
	indices := make([]int, len(document.Blocks))
	lookup := map[string]int{}
	for index, block := range document.Blocks {
		key := formatBlockState(block.State)
		paletteIndex, exists := lookup[key]
		if !exists {
			paletteIndex = len(palette)
			lookup[key] = paletteIndex
			palette = append(palette, block.State)
		}
		indices[index] = paletteIndex
	}
	if len(palette) == 0 {
		palette = append(palette, blueprintBlockState{ID: "minecraft:air"})
	}
	return palette, indices
}

func blockStateFromMap(value map[string]any) blueprintBlockState {
	state := blueprintBlockState{ID: stringValue(value["Name"])}
	if state.ID == "" {
		state.ID = stringValue(value["name"])
	}
	state.Properties = stringMap(value["Properties"])
	if len(state.Properties) == 0 {
		state.Properties = stringMap(value["properties"])
	}
	if state.ID == "" {
		state.ID = "minecraft:air"
	}
	return state
}

func parseBlockState(value string) blueprintBlockState {
	value = strings.TrimSpace(value)
	state := blueprintBlockState{ID: value}
	open := strings.IndexByte(value, '[')
	if open < 0 || !strings.HasSuffix(value, "]") {
		return state
	}
	state.ID = value[:open]
	state.Properties = map[string]string{}
	for _, pair := range strings.Split(value[open+1:len(value)-1], ",") {
		key, entry, ok := strings.Cut(pair, "=")
		if ok {
			state.Properties[strings.TrimSpace(key)] = strings.TrimSpace(entry)
		}
	}
	return state
}

func formatBlockState(state blueprintBlockState) string {
	if len(state.Properties) == 0 {
		return state.ID
	}
	keys := make([]string, 0, len(state.Properties))
	for key := range state.Properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, key+"="+state.Properties[key])
	}
	return state.ID + "[" + strings.Join(values, ",") + "]"
}

func decodeVarInts(data []byte, expected int) ([]int, error) {
	result := make([]int, 0, expected)
	for offset := 0; offset < len(data) && len(result) < expected; {
		value, shift := 0, 0
		for {
			if offset >= len(data) || shift > 35 {
				return nil, errors.New("invalid schematic varint data")
			}
			current := data[offset]
			offset++
			value |= int(current&0x7f) << shift
			if current&0x80 == 0 {
				break
			}
			shift += 7
		}
		result = append(result, value)
	}
	if len(result) < expected {
		return nil, fmt.Errorf("schematic block data contains %d entries; expected %d", len(result), expected)
	}
	return result, nil
}

func appendVarInt(destination []byte, value int) []byte {
	for {
		current := byte(value & 0x7f)
		value >>= 7
		if value != 0 {
			current |= 0x80
		}
		destination = append(destination, current)
		if value == 0 {
			return destination
		}
	}
}

func packedValue(values []int64, index, bits int) int {
	if bits <= 0 || bits > 63 || len(values) == 0 || index < 0 {
		return 0
	}
	bitIndex := index * bits
	word := bitIndex / 64
	offset := bitIndex % 64
	if word >= len(values) {
		return 0
	}
	mask := uint64((uint64(1) << bits) - 1)
	value := uint64(values[word]) >> offset // #nosec G115 -- NBT signed longs are intentionally interpreted as packed unsigned bits.
	if offset+bits > 64 && word+1 < len(values) {
		value |= uint64(values[word+1]) << (64 - offset) // #nosec G115 -- intentional packed-bit reinterpretation.
	}
	value &= mask
	if value > uint64(math.MaxInt) {
		return 0
	}
	return int(value)
}

func anyMap(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return map[string]any{}
}

func anySlice(value any) []any {
	if result, ok := value.([]any); ok {
		return result
	}
	return nil
}

func byteSlice(value any) []byte {
	switch typed := value.(type) {
	case []byte:
		return typed
	case []int8:
		result := make([]byte, len(typed))
		for index, entry := range typed {
			result[index] = byte(entry) // #nosec G115 -- NBT byte arrays use signed int8 storage for raw octets.
		}
		return result
	case []any:
		result := make([]byte, len(typed))
		for index, entry := range typed {
			number := intValue(entry)
			if number < 0 || number > math.MaxUint8 {
				return nil
			}
			result[index] = byte(number)
		}
		return result
	default:
		return nil
	}
}

func longSlice(value any) []int64 {
	switch typed := value.(type) {
	case []int64:
		return typed
	case []any:
		result := make([]int64, len(typed))
		for index, entry := range typed {
			result[index] = int64(intValue(entry))
		}
		return result
	default:
		return nil
	}
}

func intTriplet(value any) [3]int {
	values := intSlice(value)
	result := [3]int{}
	for index := 0; index < len(values) && index < 3; index++ {
		result[index] = values[index]
	}
	return result
}

func coordinateTriplet(value any) [3]int {
	if object := anyMap(value); len(object) > 0 {
		return [3]int{intValue(object["x"]), intValue(object["y"]), intValue(object["z"])}
	}
	return intTriplet(value)
}

func intSlice(value any) []int {
	switch typed := value.(type) {
	case []int32:
		result := make([]int, len(typed))
		for index, entry := range typed {
			result[index] = int(entry)
		}
		return result
	case []int64:
		result := make([]int, len(typed))
		for index, entry := range typed {
			result[index] = int(entry)
		}
		return result
	case []any:
		result := make([]int, len(typed))
		for index, entry := range typed {
			result[index] = intValue(entry)
		}
		return result
	default:
		return nil
	}
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		if typed > int64(math.MaxInt) || typed < int64(math.MinInt) {
			return 0
		}
		return int(typed)
	case uint8:
		return int(typed)
	case uint16:
		return int(typed)
	case uint32:
		return int(typed)
	case uint64:
		if typed > uint64(math.MaxInt) {
			return 0
		}
		return int(typed)
	case float32:
		if typed > float32(math.MaxInt) || typed < float32(math.MinInt) {
			return 0
		}
		return int(typed)
	case float64:
		if typed > float64(math.MaxInt) || typed < float64(math.MinInt) {
			return 0
		}
		return int(typed)
	default:
		return 0
	}
}

func blueprintInt32(value int) int32 {
	return int32(value) // #nosec G115 -- validateBlueprintDocument bounds every caller to int32-safe values.
}

func blueprintInt16(value int) int16 {
	return int16(value) // #nosec G115 -- encodeSpongeSchematic validates dimensions against MaxInt16.
}

func stringValue(value any) string {
	if result, ok := value.(string); ok {
		return result
	}
	return ""
}

func stringMap(value any) map[string]string {
	object := anyMap(value)
	if len(object) == 0 {
		return nil
	}
	result := make(map[string]string, len(object))
	for key, entry := range object {
		result[key] = fmt.Sprint(entry)
	}
	return result
}

func absInt(value int) int {
	if value == math.MinInt {
		return math.MaxInt
	}
	if value < 0 {
		return -value
	}
	return value
}

func signInt(value int) int {
	if value < 0 {
		return -1
	}
	return 1
}
func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
