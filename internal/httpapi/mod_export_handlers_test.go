package httpapi

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestNormalizeExportPathRejectsUnsafePaths(t *testing.T) {
	for _, value := range []string{"../manifest.json", "/manifest.json", `C:\\manifest.json`, `folder\\manifest.json`, ""} {
		if _, err := normalizeExportPath(value); err == nil {
			t.Fatalf("expected unsafe path %q to fail", value)
		}
	}
	if normalized, err := normalizeExportPath("data/example/structures/test.nbt"); err != nil || normalized != "data/example/structures/test.nbt" {
		t.Fatalf("unexpected normalized path %q: %v", normalized, err)
	}
}

func TestValidateExportZIPRejectsCaseInsensitiveDuplicates(t *testing.T) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, name := range []string{"manifest.json", "MANIFEST.JSON"} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = entry.Write([]byte("{}"))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = validateExportZIP(reader.File); err == nil {
		t.Fatal("expected duplicate normalized ZIP paths to fail")
	}
}

func TestNormalizeExportNamespaces(t *testing.T) {
	actual := normalizeExportNamespaces([]string{" Create ", "create", "example-mod", "../bad"})
	if len(actual) != 2 || actual[0] != "create" || actual[1] != "example-mod" {
		t.Fatalf("unexpected namespaces: %#v", actual)
	}
}

func TestDecodeExportTranslationValuesSkipsNonStrings(t *testing.T) {
	values, skippedKeys, total, err := decodeExportTranslationValues([]byte(`{"plain":"value","nested":{"text":"value"},"count":3,"empty":null}`))
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 || len(values) != 1 || values["plain"] != "value" {
		t.Fatalf("unexpected decoded translations: total=%d values=%#v", total, values)
	}
	if len(skippedKeys) != 3 {
		t.Fatalf("expected three skipped keys, got %#v", skippedKeys)
	}
}

func TestDecodeExportTranslationValuesSupportsExporterWrapper(t *testing.T) {
	values, skippedKeys, total, err := decodeExportTranslationValues([]byte(`{
		"language":"zh_cn",
		"namespace":"minecraft",
		"translation_count":3,
		"translations":{"block.minecraft.stone":"石头","item.minecraft.apple":"苹果","invalid":{"text":"bad"}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(values) != 2 || values["block.minecraft.stone"] != "石头" || values["item.minecraft.apple"] != "苹果" {
		t.Fatalf("unexpected wrapped translations: total=%d values=%#v", total, values)
	}
	if len(skippedKeys) != 1 || skippedKeys[0] != "invalid" {
		t.Fatalf("unexpected skipped keys: %#v", skippedKeys)
	}
}

func TestShouldRetryModExportStatus(t *testing.T) {
	for _, status := range []string{"failed", "cancelled"} {
		if !shouldRetryModExportStatus(status) {
			t.Fatalf("expected %s to be retryable", status)
		}
	}
	for _, status := range []string{"queued", "validating", "importing", "ready", "partial"} {
		if shouldRetryModExportStatus(status) {
			t.Fatalf("expected %s not to be retryable", status)
		}
	}
}
