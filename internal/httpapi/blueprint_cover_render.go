package httpapi

import (
	"bytes"
	"hash/fnv"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"sort"
)

const (
	blueprintCoverWidth  = 1210
	blueprintCoverHeight = 750
)

type coverBlock struct {
	position [3]int
	blockID  string
}

// renderBlueprintCover creates a stable lightweight preview without requiring
// browser-side WebGL. The interactive viewer still uses the imported models.
func renderBlueprintCover(document blueprintDocument) ([]byte, error) {
	canvas := image.NewRGBA(image.Rect(0, 0, blueprintCoverWidth, blueprintCoverHeight))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.RGBA{R: 11, G: 22, B: 19, A: 255}}, image.Point{}, draw.Src)
	if len(document.Blocks) == 0 {
		return encodeBlueprintCover(canvas)
	}

	occupied := make(map[[3]int]struct{}, len(document.Blocks))
	blocks := make([]coverBlock, 0, len(document.Blocks))
	maxInteger := int(^uint(0) >> 1)
	minX, minY, minZ := maxInteger, maxInteger, maxInteger
	maxX, maxY, maxZ := -maxInteger-1, -maxInteger-1, -maxInteger-1
	for _, block := range document.Blocks {
		position := block.Position
		occupied[position] = struct{}{}
		blocks = append(blocks, coverBlock{position: position, blockID: block.State.ID})
		minX, minY, minZ = min(minX, position[0]), min(minY, position[1]), min(minZ, position[2])
		maxX, maxY, maxZ = max(maxX, position[0]), max(maxY, position[1]), max(maxZ, position[2])
	}

	visible := blocks[:0]
	for _, block := range blocks {
		x, y, z := block.position[0], block.position[1], block.position[2]
		_, topCovered := occupied[[3]int{x, y + 1, z}]
		_, eastCovered := occupied[[3]int{x + 1, y, z}]
		_, southCovered := occupied[[3]int{x, y, z + 1}]
		if !topCovered || !eastCovered || !southCovered {
			visible = append(visible, block)
		}
	}
	sort.SliceStable(visible, func(i, j int) bool {
		left, right := visible[i].position, visible[j].position
		if left[0]+left[2] != right[0]+right[2] {
			return left[0]+left[2] < right[0]+right[2]
		}
		if left[1] != right[1] {
			return left[1] < right[1]
		}
		return left[0] < right[0]
	})

	extentX, extentY, extentZ := maxX-minX+1, maxY-minY+1, maxZ-minZ+1
	fitWidth := float64(blueprintCoverWidth-120) / float64(max(1, extentX+extentZ))
	fitHeight := float64(blueprintCoverHeight-100) / float64(max(1, extentX+extentZ+2*extentY)) * 2
	tileWidth := int(math.Floor(math.Min(36, math.Min(fitWidth*2, fitHeight*2))))
	tileWidth = max(2, tileWidth-tileWidth%2)
	halfWidth := max(1, tileWidth/2)
	tileHeight := max(2, tileWidth/2)
	halfHeight := max(1, tileHeight/2)
	projectedWidth := (extentX + extentZ) * halfWidth
	originX := (blueprintCoverWidth-projectedWidth)/2 + extentZ*halfWidth
	originY := max(36, (blueprintCoverHeight-((extentX+extentZ)*halfHeight+extentY*tileHeight))/2+extentY*tileHeight)

	for _, block := range visible {
		x := block.position[0] - minX
		y := block.position[1] - minY
		z := block.position[2] - minZ
		centerX := originX + (x-z)*halfWidth
		centerY := originY + (x+z)*halfHeight - y*tileHeight
		base := coverColor(block.blockID)
		_, topCovered := occupied[[3]int{block.position[0], block.position[1] + 1, block.position[2]}]
		_, eastCovered := occupied[[3]int{block.position[0] + 1, block.position[1], block.position[2]}]
		_, southCovered := occupied[[3]int{block.position[0], block.position[1], block.position[2] + 1}]
		if !southCovered {
			fillCoverPolygon(canvas, []image.Point{{centerX - halfWidth, centerY}, {centerX, centerY + halfHeight}, {centerX, centerY + halfHeight + tileHeight}, {centerX - halfWidth, centerY + tileHeight}}, shadeCoverColor(base, 0.76))
		}
		if !eastCovered {
			fillCoverPolygon(canvas, []image.Point{{centerX + halfWidth, centerY}, {centerX, centerY + halfHeight}, {centerX, centerY + halfHeight + tileHeight}, {centerX + halfWidth, centerY + tileHeight}}, shadeCoverColor(base, 0.58))
		}
		if !topCovered {
			fillCoverPolygon(canvas, []image.Point{{centerX, centerY - halfHeight}, {centerX + halfWidth, centerY}, {centerX, centerY + halfHeight}, {centerX - halfWidth, centerY}}, shadeCoverColor(base, 1.18))
		}
	}
	return encodeBlueprintCover(canvas)
}

func coverColor(blockID string) color.RGBA {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(blockID))
	value := hash.Sum32()
	return color.RGBA{R: uint8(72 + value%128), G: uint8(82 + (value>>8)%122), B: uint8(78 + (value>>16)%126), A: 255}
}

func shadeCoverColor(base color.RGBA, factor float64) color.RGBA {
	channel := func(value uint8) uint8 { return uint8(math.Min(255, math.Round(float64(value)*factor))) }
	return color.RGBA{R: channel(base.R), G: channel(base.G), B: channel(base.B), A: 255}
}

func fillCoverPolygon(canvas *image.RGBA, points []image.Point, fill color.RGBA) {
	minY, maxY := points[0].Y, points[0].Y
	for _, point := range points[1:] {
		minY, maxY = min(minY, point.Y), max(maxY, point.Y)
	}
	for y := minY; y <= maxY; y++ {
		intersections := make([]int, 0, len(points))
		for index, start := range points {
			end := points[(index+1)%len(points)]
			if start.Y == end.Y || y < min(start.Y, end.Y) || y >= max(start.Y, end.Y) {
				continue
			}
			x := start.X + (y-start.Y)*(end.X-start.X)/(end.Y-start.Y)
			intersections = append(intersections, x)
		}
		sort.Ints(intersections)
		for index := 0; index+1 < len(intersections); index += 2 {
			for x := intersections[index]; x <= intersections[index+1]; x++ {
				if image.Pt(x, y).In(canvas.Bounds()) {
					canvas.SetRGBA(x, y, fill)
				}
			}
		}
	}
}

func encodeBlueprintCover(canvas image.Image) ([]byte, error) {
	var output bytes.Buffer
	if err := png.Encode(&output, canvas); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
