package serverprobe

import (
	"bytes"
	"reflect"
	"strconv"
	"testing"
)

func TestConfigurationProtocolForProxyStatusUses1211(t *testing.T) {
	protocol, fallback := configurationProtocolForStatus(772, "Velocity 1.7.2-1.21.8")
	if protocol != minecraft1211Protocol || !fallback {
		t.Fatalf("protocol = %d, fallback = %v", protocol, fallback)
	}

	protocol, fallback = configurationProtocolForStatus(772, "1.21.8")
	if protocol != 772 || fallback {
		t.Fatalf("regular server protocol = %d, fallback = %v", protocol, fallback)
	}
	if version := minecraftVersionForConfigurationProtocol(minecraft1211Protocol); version != "1.21.1" {
		t.Fatalf("version = %q", version)
	}
}

func TestParseConfigurationNetworkChannels(t *testing.T) {
	data := new(bytes.Buffer)
	writeVarInt(data, 1)
	writeVarInt(data, 4)
	writeVarInt(data, 2)
	for _, id := range []string{"example:main", "other:sync"} {
		_ = writeProtocolString(data, id)
		_ = writeProtocolString(data, id)
		_ = writeProtocolString(data, "1")
	}
	ids, err := parseConfigurationNetworkChannels(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "example:main" || ids[1] != "other:sync" {
		t.Fatalf("unexpected channel IDs: %#v", ids)
	}
}

func TestParseConfigurationFrozenRegistry(t *testing.T) {
	data := new(bytes.Buffer)
	_ = writeProtocolString(data, "minecraft:item")
	writeVarInt(data, 2)
	writeVarInt(data, 1)
	_ = writeProtocolString(data, "minecraft:stone")
	writeVarInt(data, 2)
	_ = writeProtocolString(data, "example:copper_ingot")
	writeVarInt(data, 0)

	name, entries, err := parseConfigurationFrozenRegistry(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if name != "minecraft:item" || len(entries) != 2 || entries[1] != "example:copper_ingot" {
		t.Fatalf("unexpected registry: %q %#v", name, entries)
	}
}

func TestDiscoverableNamespacesExcludeLoaderInfrastructure(t *testing.T) {
	for _, namespace := range []string{"minecraft", "forge", "neoforge", "fabricloader", "c"} {
		if isDiscoverableModNamespace(namespace) {
			t.Fatalf("infrastructure namespace %q was treated as a mod", namespace)
		}
	}
	if !isDiscoverableModNamespace("example_mod") {
		t.Fatal("valid mod namespace was rejected")
	}
}

func TestNulSeparatedChannelsRoundTrip(t *testing.T) {
	encoded := encodeNulSeparatedChannels([]string{
		"example:main",
		fabricDirectRegistryChannel,
		"example:main",
	})
	if got, want := string(encoded), "example:main\x00"+fabricDirectRegistryChannel; got != want {
		t.Fatalf("encoded channels = %q, want %q", got, want)
	}
}

func TestParseFabricRegistrySync(t *testing.T) {
	data := new(bytes.Buffer)
	writeVarInt(data, 1)
	_ = writeProtocolString(data, "")
	writeVarInt(data, 1)
	_ = writeProtocolString(data, "item")
	writeVarInt(data, 2)

	_ = writeProtocolString(data, "")
	writeVarInt(data, 1)
	writeVarInt(data, 1)
	writeVarInt(data, 2)
	_ = writeProtocolString(data, "stone")
	_ = writeProtocolString(data, "dirt")

	_ = writeProtocolString(data, "examplemod")
	writeVarInt(data, 1)
	writeVarInt(data, 30)
	writeVarInt(data, 2)
	_ = writeProtocolString(data, "copper_ingot")
	_ = writeProtocolString(data, "tin_ingot")

	registries, entries, err := parseFabricRegistrySync(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(registries, []fabricRegistryGroup{{namespace: "minecraft", paths: []string{"item"}}}) {
		t.Fatalf("registries = %#v", registries)
	}
	wantEntries := []fabricRegistryGroup{
		{namespace: "minecraft", paths: []string{"stone", "dirt"}},
		{namespace: "examplemod", paths: []string{"copper_ingot", "tin_ingot"}},
	}
	if !reflect.DeepEqual(entries, wantEntries) {
		t.Fatalf("entries = %#v", entries)
	}
	evidence := map[string]int{}
	for _, groups := range [][]fabricRegistryGroup{registries, entries} {
		if err := recordFabricRegistryNamespaces(evidence, groups); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(evidence, map[string]int{"minecraft": 3, "examplemod": 2}) {
		t.Fatalf("namespace evidence = %#v", evidence)
	}
}

func TestParseConfigurationDynamicRegistry(t *testing.T) {
	data := new(bytes.Buffer)
	_ = writeProtocolString(data, "minecraft:dimension_type")
	writeVarInt(data, 2)
	_ = writeProtocolString(data, "minecraft:overworld")
	_ = data.WriteByte(0)
	_ = writeProtocolString(data, "examplemod:moon")
	_ = data.WriteByte(0)

	registry, entries, err := parseConfigurationDynamicRegistry(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if registry != "minecraft:dimension_type" ||
		!reflect.DeepEqual(entries, []string{"minecraft:overworld", "examplemod:moon"}) {
		t.Fatalf("registry = %q, entries = %#v", registry, entries)
	}
}

func BenchmarkParseFabricRegistrySync(b *testing.B) {
	var data bytes.Buffer
	writeVarInt(&data, 1)
	_ = writeProtocolString(&data, "minecraft")
	writeVarInt(&data, 1)
	_ = writeProtocolString(&data, "item")
	writeVarInt(&data, 1)
	_ = writeProtocolString(&data, "example_items")
	writeVarInt(&data, 1)
	writeVarInt(&data, 1)
	writeVarInt(&data, 64)
	for index := range 64 {
		_ = writeProtocolString(&data, "tool_"+strconv.Itoa(index))
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := parseFabricRegistrySync(data.Bytes()); err != nil {
			b.Fatal(err)
		}
	}
}
