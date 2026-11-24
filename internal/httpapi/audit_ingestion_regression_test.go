package httpapi

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"
)

func TestAuditGR009ExplicitInvalidQuantitiesAreRejected(t *testing.T) {
	for _, raw := range []string{`{"count":-7}`, `{"count":0}`, `{"amount":"many"}`, `{"amount":null}`, `{"count":1.5}`, `{"count":2,"amount":3}`, `{"amount":9007199254740992}`} {
		var value map[string]any
		_ = json.Unmarshal([]byte(raw), &value)
		if amount, err := validatedExportRecipeAmount(value, true); err == nil {
			t.Errorf("explicit invalid quantity %s accepted as %v", raw, amount)
		}
	}
	for _, tc := range []struct {
		raw    string
		amount float64
	}{{`{}`, 1}, {`{"type":"item_stack","count":64}`, 64}, {`{"type":"fluid_stack","amount":0.25}`, 0.25}, {`{"count":2,"amount":2}`, 2}} {
		var value map[string]any
		_ = json.Unmarshal([]byte(tc.raw), &value)
		if amount, err := validatedExportRecipeAmount(value, true); err != nil || amount == nil || *amount != tc.amount {
			t.Errorf("valid %s = %v/%v", tc.raw, amount, err)
		}
	}
	if _, err := validatedExportRecipeAmount(map[string]any{"count": math.Inf(1)}, true); err == nil {
		t.Error("infinity accepted")
	}
}
func TestCurrentJEICandidatePreservesObservedQuantity(t *testing.T) {
	for _, raw := range []string{`{"count":-7}`, `{"amount":null}`, `{"amount":"many"}`, `{"count":1.5}`, `{"count":0,"amount":1}`, `{"amount":9007199254740992}`} {
		var value map[string]any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		if _, err := validatedExportRecipeAmount(value, false); err == nil {
			t.Errorf("invalid current quantity accepted: %s", raw)
		}
	}
}

func TestAuditGR009ProbabilitiesMustAgree(t *testing.T) {
	chance, percent := .25, 90.0
	binding := exportJEIBinding{SemanticRole: "output", ChanceAvailable: true, Chance: &chance, ChancePercent: &percent}
	if validateExportRecipeChance(binding) == nil {
		t.Fatal("contradictory probabilities accepted")
	}
	percent = 25
	if err := validateExportRecipeChance(binding); err != nil {
		t.Fatal(err)
	}
}
func TestAuditCONT004CanonicalNumbersRetainRangeAndIntegerRules(t *testing.T) {
	for _, definition := range []map[string]any{{"lightLevel": 16.0}, {"lightLevel": 1.5}, {"durability": 1.5}, {"temperature": -1.0}, {"maxStackSize": 0.0}} {
		if validateCatalogResourceDefinition("minecraft.item", definition) == nil {
			t.Errorf("invalid canonical definition accepted: %#v", definition)
		}
	}
	for _, definition := range []map[string]any{{"lightLevel": 0.0}, {"lightLevel": 15.0}, {"temperature": 0.0}, {"maxStackSize": 64.0}} {
		if err := validateCatalogResourceDefinition("minecraft.item", definition); err != nil {
			t.Errorf("valid boundary rejected: %v", err)
		}
	}
}
func TestAuditGR001PNGMinimizationPreservesPixelsAndTransparency(t *testing.T) {
	original := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	original.SetNRGBA(0, 0, color.NRGBA{R: 39, G: 78, B: 91, A: 50})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, original); err != nil {
		t.Fatal(err)
	}
	raw := encoded.Bytes()
	payload := []byte("Comment\x00machine_path=/private/synthetic;access_token=synthetic-only")
	chunk := make([]byte, len(payload)+12)
	binary.BigEndian.PutUint32(chunk[:4], uint32(len(payload)))
	copy(chunk[4:8], "tEXt")
	copy(chunk[8:], payload)
	binary.BigEndian.PutUint32(chunk[len(chunk)-4:], crc32.ChecksumIEEE(chunk[4:len(chunk)-4]))
	enriched := append(append(append([]byte(nil), raw[:len(raw)-12]...), chunk...), raw[len(raw)-12:]...)
	clean, err := minimizeExportPNG(enriched)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(clean, []byte("synthetic")) || bytes.Contains(clean, []byte("tEXt")) {
		t.Fatal("unrelated PNG text retained")
	}
	decoded, err := png.Decode(bytes.NewReader(clean))
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			if decoded.At(x, y) != original.At(x, y) {
				t.Fatalf("pixel changed at %d/%d", x, y)
			}
		}
	}
	if !bytes.Equal(clean, raw) {
		t.Fatal("image chunks changed while removing text")
	}
}
func TestAuditGR001TypedNestedDisplayAndLocalizedChanceProjection(t *testing.T) {
	field := modContentEntryTypeField{Code: "display", Type: "json"}
	value, _, err := normalizeModContentFieldValue(field, map[string]any{"x": 1.0, "frame": "task", "machine_path": "synthetic-private", "title": map[string]any{"text": "Name", "access_token": "synthetic-token"}, "icon": map[string]any{"item": "example:item", "private": "synthetic"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(value)
	if bytes.Contains(raw, []byte("synthetic")) || !bytes.Contains(raw, []byte("example:item")) || !bytes.Contains(raw, []byte("Name")) {
		t.Fatalf("bad display projection: %s", raw)
	}
	clean := minimizeExportLocaleText(json.RawMessage(`{"zh_cn":"25% 概率","machine_path":"synthetic-private","access_token":"synthetic-token"}`))
	if bytes.Contains(clean, []byte("synthetic")) || !bytes.Contains(clean, []byte("25%")) {
		t.Fatalf("bad chance labels: %s", clean)
	}
	manifest := modExportManifest{Errors: []json.RawMessage{json.RawMessage(`{"stage":"recipes","message":"synthetic-secret","nested":{"access_token":"synthetic-secret"}}`)}}
	minimizeExportManifest(&manifest)
	raw, _ = json.Marshal(manifest.Errors)
	if bytes.Contains(raw, []byte("synthetic")) {
		t.Fatalf("raw diagnostic retained: %s", raw)
	}
}
