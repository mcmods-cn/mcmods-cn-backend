package httpapi

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestValidateModResourceImageSpec(t *testing.T) {
	png32 := testTransparentPNG(t, 32)
	if err := validateModResourceImageSpec(png32, "image/png", "mod_resource:create:minecraft.item:icon_32"); err != nil {
		t.Fatalf("valid 32px icon rejected: %v", err)
	}
	if err := validateModResourceImageSpec(png32, "image/png", "mod_resource:create:minecraft.item:icon_128"); err == nil {
		t.Fatal("32px icon accepted as 128px variant")
	}
	if err := validateModResourceImageSpec(png32, "image/jpeg", "mod_resource:create:minecraft.item:icon_32"); err == nil {
		t.Fatal("non-PNG resource icon accepted")
	}
	if err := validateModResourceImageSpec(png32, "image/png", "comment"); err != nil {
		t.Fatalf("unrelated upload source was constrained: %v", err)
	}
}

func TestValidateModResourceRenderImageSpec(t *testing.T) {
	wide := testTransparentPNGSize(t, 640, 320)
	if err := validateModResourceImageSpec(wide, "image/png", "mod_resource:create:minecraft.item:render"); err != nil {
		t.Fatalf("non-square rendered image rejected: %v", err)
	}
	tall := testTransparentPNGSize(t, 320, 1024)
	if err := validateModResourceImageSpec(tall, "image/png", "mod_resource:create:minecraft.item:render"); err != nil {
		t.Fatalf("1024px rendered image rejected: %v", err)
	}
	oversized := testTransparentPNGSize(t, 1025, 8)
	if err := validateModResourceImageSpec(oversized, "image/png", "mod_resource:create:minecraft.item:render"); err == nil {
		t.Fatal("oversized rendered image accepted without OSS resizing")
	}
	if _, err := validateModResourcePNGConfig(oversized, "image/png"); err != nil {
		t.Fatalf("oversized source image was rejected before OSS resizing: %v", err)
	}
	if err := validateModResourceImageSpec(wide, "image/jpeg", "mod_resource:create:minecraft.item:render"); err == nil {
		t.Fatal("non-PNG rendered image accepted")
	}
}

func TestModResourceRenderUploadSource(t *testing.T) {
	for _, source := range []string{
		"mod_resource:create:minecraft.item:render",
		"mod_resource:create:minecraft.item:render_256",
	} {
		if !isModResourceRenderUploadSource(source) {
			t.Fatalf("render source %q was not recognized", source)
		}
	}
	if isModResourceRenderUploadSource("comment:render") {
		t.Fatal("unrelated render source was recognized")
	}
	if got, want := persistedModResourceRenderObjectKey("mcmods/users/1/image.png"), "mcmods/users/1/image.render-1024.png"; got != want {
		t.Fatalf("processed render object key = %q, want %q", got, want)
	}
}

func TestValidateModContentTemplateDefinition(t *testing.T) {
	valid := map[string]any{
		"resourceKinds": []any{"mod.skill"},
		"entryTypes": []any{map[string]any{
			"code": "skill",
			"groups": []any{map[string]any{
				"code": "skill",
				"fields": []any{map[string]any{
					"code": "cd", "type": "number", "paths": []any{[]any{"skill", "cd"}},
				}},
			}},
		}},
	}
	if err := validateModContentTemplateDefinition(valid); err != nil {
		t.Fatalf("valid extensible entry schema rejected: %v", err)
	}
	invalid := map[string]any{
		"entryTypes": []any{map[string]any{
			"code": "skill",
			"groups": []any{map[string]any{
				"code": "skill",
				"fields": []any{map[string]any{
					"code": "cd", "type": "script", "paths": []any{[]any{"skill", "cd"}},
				}},
			}},
		}},
	}
	if err := validateModContentTemplateDefinition(invalid); err == nil {
		t.Fatal("unknown custom field type accepted")
	}
}

func testTransparentPNG(t *testing.T, size int) []byte {
	return testTransparentPNGSize(t, size, size)
}

func testTransparentPNGSize(t *testing.T, width, height int) []byte {
	t.Helper()
	value := image.NewNRGBA(image.Rect(0, 0, width, height))
	value.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 128})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, value); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
