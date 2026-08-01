package serverprobe

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"reflect"
	"testing"
)

func TestRawJSONStringUTF16PreservesSurrogates(t *testing.T) {
	units, err := rawJSONStringUTF16(json.RawMessage(`"\ud812A\udfff"`))
	if err != nil {
		t.Fatal(err)
	}
	want := []uint16{0xd812, 'A', 0xdfff}
	if !reflect.DeepEqual(units, want) {
		t.Fatalf("units = %#v, want %#v", units, want)
	}
}

func TestDecodeForgeOptimizedWritesFinalPartialByte(t *testing.T) {
	decoded, err := decodeForgeOptimized([]uint16{2, 0, 0x41})
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{0x41, 0}; !bytes.Equal(decoded, want) {
		t.Fatalf("decoded = %x, want %x", decoded, want)
	}
}

func TestParseForgeModData(t *testing.T) {
	data := new(bytes.Buffer)
	_ = data.WriteByte(0)
	_ = binary.Write(data, binary.BigEndian, uint16(1))
	writeVarInt(data, 2)
	_ = writeProtocolString(data, "example")
	_ = writeProtocolString(data, "1.2.3")
	_ = writeProtocolString(data, "main")
	writeVarInt(data, 7)
	_ = data.WriteByte(1)
	writeVarInt(data, 0)

	mods, truncated, err := parseForgeModData(data.Bytes(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if truncated || len(mods) != 1 || mods[0].ModID != "example" || mods[0].Marker != "1.2.3" {
		t.Fatalf("unexpected Forge result: truncated=%v mods=%#v", truncated, mods)
	}
	if len(mods[0].Channels) != 1 || mods[0].Channels[0].Resource != "example:main" {
		t.Fatalf("unexpected Forge channels: %#v", mods[0].Channels)
	}
}

func TestParseForgeStringChannelVersion(t *testing.T) {
	data := new(bytes.Buffer)
	_ = data.WriteByte(0)
	_ = binary.Write(data, binary.BigEndian, uint16(1))
	writeVarInt(data, 2)
	_ = writeProtocolString(data, "example")
	_ = writeProtocolString(data, "1.2.3")
	_ = writeProtocolString(data, "main")
	_ = writeProtocolString(data, "ALLOWVANILLA")
	_ = data.WriteByte(1)
	writeVarInt(data, 0)

	mods, _, err := parseForgeModData(data.Bytes(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if got := mods[0].Channels[0].Version; got != "ALLOWVANILLA" {
		t.Fatalf("channel version = %q", got)
	}
}
