package raster

import (
	"bytes"
	"image"
	"testing"

	"github.com/Nomadcxx/sysc-terminal/internal/cell"
)

// TestWideGlyphSurvivesColourChangeInIncrementalDraw pins the incremental
// draw of a double-width glyph whose colour changed between two frames.
//
// A wide rune occupies two cells: the rune in the first cell and a
// continuation marker in the second. The marker carries the colour of the
// rune, so a colour change makes the marker look changed as well. The
// incremental path then cleared the marker on its own, which erased the half
// of the glyph that had just been drawn. The grid does not remember the
// erased pixels, so the damage stayed on screen until the glyph or its
// colour changed again (sysc-2017).
//
// The test font carries no CJK glyph, so the test installs a synthetic
// two-cell mask for the wide rune into the glyph cache.
func TestWideGlyphSurvivesColourChangeInIncrementalDraw(t *testing.T) {
	rz := openTestFont(t)
	face, src := rz.faceFor('界')
	gid, _ := face.NominalGlyph('界')
	wideMask := image.NewAlpha(image.Rect(0, 0, 2*rz.cellW, rz.cellH))
	for y := 0; y < rz.cellH; y++ {
		for x := 0; x < 2*rz.cellW; x++ {
			wideMask.Pix[y*wideMask.Stride+x] = 255
		}
	}
	rz.cache[rasterKey{gid: gid, ppem: rz.ppem, src: src}] = wideMask

	cw, ch := rz.CellSize()
	const cols, rows = 2, 1
	width, height := cols*cw, rows*ch
	stride, total, err := BufferSize(width, height, 1)
	if err != nil {
		t.Fatalf("size: %v", err)
	}

	red, err := cell.Parse("\033[38;2;255;0;0m界\033[0m", cols, rows)
	if err != nil {
		t.Fatalf("parse red: %v", err)
	}
	green, err := cell.Parse("\033[38;2;0;255;0m界\033[0m", cols, rows)
	if err != nil {
		t.Fatalf("parse green: %v", err)
	}

	// The incremental path draws into the buffer that already holds the
	// previous frame, so the first frame goes in with a full draw.
	incremental := make([]byte, total)
	if err := rz.Draw(red, incremental, width, height, stride); err != nil {
		t.Fatalf("draw red: %v", err)
	}
	if err := rz.DrawChanged(green, red, incremental, width, height, stride); err != nil {
		t.Fatalf("draw changed: %v", err)
	}
	full := make([]byte, total)
	if err := rz.Draw(green, full, width, height, stride); err != nil {
		t.Fatalf("draw full: %v", err)
	}

	if painted := paintedPixels(incremental, stride, cw, width, height); painted != cw*ch {
		t.Fatalf("the incremental draw left %d of %d pixels of the second cell unpainted", cw*ch-painted, cw*ch)
	}
	if !bytes.Equal(incremental, full) {
		t.Fatalf("the incremental draw differs from the full draw")
	}
}

// paintedPixels counts the pixels of the second cell that carry colour.
func paintedPixels(buf []byte, stride, cellW, width, height int) int {
	painted := 0
	for y := 0; y < height; y++ {
		for x := cellW; x < 2*cellW && x < width; x++ {
			off := y*stride + x*4
			if off+3 >= len(buf) {
				continue
			}
			if buf[off] != 0 || buf[off+1] != 0 || buf[off+2] != 0 {
				painted++
			}
		}
	}
	return painted
}
