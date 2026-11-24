package httpapi

import (
	"bytes"
	"image/color"
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

func TestBlueprintCoverIncludesBothEndsOfALongBuild(t *testing.T) {
	for _, end := range [][3]int{{4095, 0, 0}, {0, 4095, 0}, {0, 0, 4095}} {
		document := blueprintDocument{Blocks: []blueprintBlock{
			{Position: [3]int{}, State: blueprintBlockState{ID: "minecraft:stone"}},
			{Position: end, State: blueprintBlockState{ID: "minecraft:oak_planks"}},
		}}
		raw, err := renderBlueprintCover(document)
		if err != nil {
			t.Fatal(err)
		}
		preview, err := png.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		found := map[color.RGBA]bool{}
		for _, block := range document.Blocks {
			found[shadeCoverColor(coverColor(block.State.ID), 1.18)] = false
		}
		for y := 0; y < preview.Bounds().Dy(); y++ {
			for x := 0; x < preview.Bounds().Dx(); x++ {
				pixel := color.RGBAModel.Convert(preview.At(x, y)).(color.RGBA)
				if _, expected := found[pixel]; expected {
					found[pixel] = true
				}
			}
		}
		for pixel, visible := range found {
			if !visible {
				t.Errorf("end=%v missing block with top color %v", end, pixel)
			}
		}
	}
}
