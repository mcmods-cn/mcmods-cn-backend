package httpapi

import (
	"bytes"
	"image/png"
	"testing"
)

func TestRenderBlueprintCover(t *testing.T) {
	document := blueprintDocument{Blocks: []blueprintBlock{
		{Position: [3]int{0, 0, 0}, State: blueprintBlockState{ID: "minecraft:stone"}},
		{Position: [3]int{1, 0, 0}, State: blueprintBlockState{ID: "minecraft:oak_planks"}},
		{Position: [3]int{0, 1, 0}, State: blueprintBlockState{ID: "minecraft:glass"}},
	}}
	raw, err := renderBlueprintCover(document)
	if err != nil {
		t.Fatalf("render cover: %v", err)
	}
	preview, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode cover: %v", err)
	}
	if preview.Bounds().Dx() != blueprintCoverWidth || preview.Bounds().Dy() != blueprintCoverHeight {
		t.Fatalf("unexpected cover size %v", preview.Bounds())
	}
	background := preview.At(0, 0)
	changed := false
	for y := 0; y < preview.Bounds().Dy() && !changed; y += 10 {
		for x := 0; x < preview.Bounds().Dx(); x += 10 {
			if preview.At(x, y) != background {
				changed = true
				break
			}
		}
	}
	if !changed {
		t.Fatal("cover contains no rendered blocks")
	}
}
