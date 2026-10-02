package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestBlueprintProcessingLimitsRejectTheAuditScale(t *testing.T) {
	if maxBlueprintSourceBytes > 32<<20 {
		t.Errorf("source limit = %d, want at most 32 MiB", maxBlueprintSourceBytes)
	}
	if maxBlueprintDecodedNBTBytes > 32<<20 {
		t.Errorf("decoded NBT limit = %d, want at most 32 MiB", maxBlueprintDecodedNBTBytes)
	}
	if maxBlueprintVolume > 2<<20 {
		t.Errorf("volume limit = %d, want at most 2 Mi blocks", maxBlueprintVolume)
	}
	if maxBlueprintNonAirBlockCount > 250_000 {
		t.Errorf("non-air block limit = %d, want at most 250000", maxBlueprintNonAirBlockCount)
	}
	document := blueprintDocument{SchemaVersion: blueprintSchemaVersion, Size: [3]int{250_001, 1, 1}, Blocks: make([]blueprintBlock, 250_001)}
	for index := range document.Blocks {
		document.Blocks[index] = blueprintBlock{Position: [3]int{index, 0, 0}, State: blueprintBlockState{ID: "minecraft:stone"}}
	}
	if err := validateBlueprintDocument(document); err == nil {
		t.Fatal("250001 retained blocks unexpectedly passed the processing budget")
	}
}

func TestNormalizedBlueprintJSONRoundTripsAndEnforcesItsWriterBudget(t *testing.T) {
	document := blueprintDocument{
		SchemaVersion: blueprintSchemaVersion,
		Name:          "Budgeted blueprint",
		SourceFormat:  "schem",
		DataVersion:   3955,
		Size:          [3]int{2, 1, 1},
		Blocks: []blueprintBlock{{Position: [3]int{0, 0, 0}, State: blueprintBlockState{
			ID: "minecraft:oak_log", Properties: map[string]string{"axis": "x"},
		}}},
		BlockEntities: []map[string]any{{"id": "minecraft:chest"}},
		Entities:      []map[string]any{{"id": "minecraft:item"}},
		Warnings:      []string{"example"},
	}
	payload, err := encodeNormalizedBlueprintJSON(document)
	if err != nil {
		t.Fatal(err)
	}
	var decoded blueprintDocument
	if err = json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != document.SchemaVersion || decoded.Name != document.Name || decoded.DataVersion != document.DataVersion ||
		len(decoded.Blocks) != 1 || len(decoded.BlockEntities) != 1 || len(decoded.Entities) != 1 || len(decoded.Warnings) != 1 {
		t.Fatalf("normalized round trip lost fields: %#v", decoded)
	}
	writer := &blueprintJSONBuffer{limit: 16}
	if _, err = writer.Write(make([]byte, 17)); !errors.Is(err, errBlueprintNormalizedSizeLimit) {
		t.Fatalf("oversized normalized output error = %v", err)
	}
}

func TestBlueprintProcessingSemaphoreHasAProcessWideHardLimit(t *testing.T) {
	if maxBlueprintProcessWorkers != 2 {
		t.Fatalf("processing slots = %d, want 2", maxBlueprintProcessWorkers)
	}
	if !blueprintProcessingSlots.TryAcquire(maxBlueprintProcessWorkers) {
		t.Fatal("could not reserve the configured blueprint processing budget")
	}
	defer blueprintProcessingSlots.Release(maxBlueprintProcessWorkers)
	if blueprintProcessingSlots.TryAcquire(1) {
		blueprintProcessingSlots.Release(1)
		t.Fatal("processing budget admitted work beyond its hard limit")
	}
}

func TestBlueprintMaterialsHaveACardinalityBudget(t *testing.T) {
	document := blueprintDocument{Size: [3]int{8193, 1, 1}, Blocks: make([]blueprintBlock, 8193)}
	for index := range document.Blocks {
		document.Blocks[index] = blueprintBlock{
			Position: [3]int{index, 0, 0},
			State:    blueprintBlockState{ID: fmt.Sprintf("example:block_%05d", index)},
		}
	}
	if _, err := blueprintMaterials(document); err == nil {
		t.Fatal("8193 distinct materials unexpectedly passed the processing budget")
	}
}

func TestBlueprintWorkerHasSharedAdmissionAndBulkMaterialPersistence(t *testing.T) {
	worker, err := os.ReadFile("blueprint_worker.go")
	if err != nil {
		t.Fatal(err)
	}
	workerSource := string(worker)
	for _, required := range []string{
		"blueprintProcessingSlots.Acquire",
		"blueprintProcessingSlots.Release",
		"replaceBlueprintMaterialsTx",
		"tx.CopyFrom",
	} {
		if !strings.Contains(workerSource, required) {
			t.Errorf("blueprint worker is missing %q", required)
		}
	}
	start := strings.Index(workerSource, "func replaceBlueprintMaterialsTx(")
	end := strings.Index(workerSource[start+1:], "\nfunc ")
	if start < 0 || end < 0 {
		t.Fatal("bulk material persistence boundary was not found")
	}
	materialBoundary := workerSource[start : start+1+end]
	if strings.Contains(materialBoundary, "for _, material := range materials") || strings.Count(materialBoundary, "tx.Exec") != 1 {
		t.Fatal("material persistence still performs per-material Exec calls")
	}

	handlers, err := os.ReadFile("blueprint_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"maxBlueprintActiveJobsPerUser", "reserveBlueprintUserJobBudgetTx", "errBlueprintUserJobLimit"} {
		if !strings.Contains(string(handlers), required) {
			t.Errorf("blueprint job admission is missing %q", required)
		}
	}
}
