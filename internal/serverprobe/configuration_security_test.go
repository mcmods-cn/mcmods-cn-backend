package serverprobe

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestConfigurationRegistryCountCannotPreallocateBeyondPayload(t *testing.T) {
	for name, parse := range map[string]func([]byte) error{
		"frozen":  func(data []byte) error { _, _, err := parseConfigurationFrozenRegistry(data); return err },
		"dynamic": func(data []byte) error { _, _, err := parseConfigurationDynamicRegistry(data); return err },
	} {
		t.Run(name, func(t *testing.T) {
			data := new(bytes.Buffer)
			_ = writeProtocolString(data, "minecraft:item")
			writeVarInt(data, 1_000_000)
			result := testing.Benchmark(func(b *testing.B) {
				for index := 0; index < b.N; index++ {
					if err := parse(data.Bytes()); err == nil {
						b.Fatal("impossible registry count was accepted")
					}
				}
			})
			if result.AllocedBytesPerOp() > 1<<20 {
				t.Fatalf("malformed count allocated %d bytes/op", result.AllocedBytesPerOp())
			}
		})
	}
}

func TestConfigurationProbeConcurrencyIsProcessWide(t *testing.T) {
	releases := make([]func(), 0, maximumConcurrentConfigurationProbes)
	for index := 0; index < maximumConcurrentConfigurationProbes; index++ {
		release, err := acquireConfigurationProbeSlot(context.Background())
		if err != nil {
			t.Fatalf("acquire slot %d: %v", index, err)
		}
		releases = append(releases, release)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := acquireConfigurationProbeSlot(ctx); !errors.Is(err, errConfigurationProbeCapacity) {
		t.Fatalf("probe beyond the process-wide limit error = %v", err)
	}
	for _, release := range releases {
		release()
	}
	release, err := acquireConfigurationProbeSlot(context.Background())
	if err != nil {
		t.Fatalf("released probe capacity was not reusable: %v", err)
	}
	release()
}

func TestConfigurationRegistryBudgetIsSharedAcrossPackets(t *testing.T) {
	budget := &configurationParseBudget{remaining: 3}
	frozen := new(bytes.Buffer)
	_ = writeProtocolString(frozen, "minecraft:item")
	writeVarInt(frozen, 2)
	for index := 0; index < 2; index++ {
		writeVarInt(frozen, int32(index))
		_ = writeProtocolString(frozen, fmt.Sprintf("example:item_%d", index))
	}
	writeVarInt(frozen, 0)
	if _, _, err := parseConfigurationFrozenRegistryWithBudget(frozen.Bytes(), budget); err != nil {
		t.Fatalf("first packet should fit the shared budget: %v", err)
	}
	dynamic := new(bytes.Buffer)
	_ = writeProtocolString(dynamic, "minecraft:biome")
	writeVarInt(dynamic, 1)
	_ = writeProtocolString(dynamic, "example:moon")
	_ = dynamic.WriteByte(0)
	if _, _, err := parseConfigurationDynamicRegistryWithBudget(dynamic.Bytes(), budget); err == nil {
		t.Fatal("second packet exceeded the shared probe budget")
	}
}

func TestConfigurationNBTNodeBudgetRejectsZeroByteLists(t *testing.T) {
	data := new(bytes.Buffer)
	_ = writeProtocolString(data, "minecraft:dimension_type")
	writeVarInt(data, 1)
	_ = writeProtocolString(data, "example:moon")
	_ = data.WriteByte(1)
	_ = data.WriteByte(9)
	_ = data.WriteByte(0)
	_ = binary.Write(data, binary.BigEndian, int32(maximumConfigurationNBTNodes+1))
	if _, _, err := parseConfigurationDynamicRegistry(data.Bytes()); err == nil {
		t.Fatal("zero-byte NBT list exceeded the node budget")
	}
}

func TestFabricRegistryAndEvidenceBudgetsAreSmallEnoughForOneProbe(t *testing.T) {
	if maxFabricRegistrySyncBytes > 8<<20 {
		t.Fatalf("Fabric buffer budget = %d", maxFabricRegistrySyncBytes)
	}
	if maximumConfigurationIdentifiers > 16_384 || maximumConfigurationNamespaces > 4_096 {
		t.Fatalf("registry budgets are too large: identifiers=%d namespaces=%d",
			maximumConfigurationIdentifiers, maximumConfigurationNamespaces)
	}
}
