package raster

import (
	"bytes"
	"image"
	"math/rand"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-terminal/internal/cell"
	"github.com/Nomadcxx/sysc-terminal/internal/effect"
)

// wideRunes are the double width glyphs the fixture uses. A frame has to
// carry more than one of them, so a glyph that changes from one wide rune to
// another exercises the two-cell footprint.
var wideRunes = []rune{'漢', '龍', '語', '藝', '國', '世', '界', '愛', '夢', '電'}

// randomGrid builds a grid with blanks, coloured cells and wide glyphs, so a
// diff between two of them exercises every branch of the damage rules.
func randomGrid(t testing.TB, cols, rows int, seed int64) *cell.Grid {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))
	var b strings.Builder
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			switch rng.Intn(8) {
			case 0:
				b.WriteByte(' ')
			case 1:
				b.WriteString("\033[38;2;255;0;0m")
				b.WriteRune(rune('a' + rng.Intn(26)))
				b.WriteString("\033[0m")
			case 2:
				b.WriteString("\033[48;2;0;0;255m")
				b.WriteRune(rune('0' + rng.Intn(10)))
				b.WriteString("\033[0m")
			case 3:
				b.WriteRune(wideRunes[rng.Intn(len(wideRunes))])
			default:
				b.WriteRune(rune('A' + rng.Intn(26)))
			}
		}
		b.WriteByte('\n')
	}
	g, err := cell.Parse(b.String(), cols, rows)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return g
}

// coverage marks every pixel the damage rectangles reach.
func coverage(rects []image.Rectangle, width, height int) []bool {
	covered := make([]bool, width*height)
	for _, rect := range rects {
		r := rect.Intersect(image.Rect(0, 0, width, height))
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				covered[y*width+x] = true
			}
		}
	}
	return covered
}

// TestDamageRectsCoverChangedCells checks the two properties the surface
// damage rests on: every cell that changed is inside the damage, and every
// damage rectangle covers a cell that changed.
func TestDamageRectsCoverChangedCells(t *testing.T) {
	rz := openTestFont(t)
	cw, ch := rz.CellSize()
	const cols, rows = 40, 12
	width, height := cols*cw+3, rows*ch+2
	for seed := int64(1); seed <= 12; seed++ {
		before := randomGrid(t, cols, rows, seed)
		after := randomGrid(t, cols, rows, seed+1000)
		rects := rz.DamageRects(after, before, nil, width, height)
		covered := coverage(rects, width, height)
		changed := 0
		for y := 0; y < rows; y++ {
			for x := 0; x < cols; x++ {
				if before.At(x, y) == after.At(x, y) {
					continue
				}
				changed++
				for py := y * ch; py < (y+1)*ch && py < height; py++ {
					for px := x * cw; px < (x+1)*cw && px < width; px++ {
						if !covered[py*width+px] {
							t.Fatalf("seed %d: cell %d,%d changed but pixel %d,%d is not damaged", seed, x, y, px, py)
						}
					}
				}
			}
		}
		if changed == 0 {
			t.Fatalf("seed %d produced no change to check", seed)
		}
		for _, rect := range rects {
			if rect.Empty() {
				t.Fatalf("seed %d: empty damage rectangle", seed)
			}
			justified := false
			for y := 0; y < rows && !justified; y++ {
				for x := 0; x < cols; x++ {
					if before.At(x, y) == after.At(x, y) {
						continue
					}
					if rect.Overlaps(image.Rect(x*cw, y*ch, (x+1)*cw, (y+1)*ch)) {
						justified = true
						break
					}
				}
			}
			if !justified {
				t.Fatalf("seed %d: damage rectangle %v covers no changed cell", seed, rect)
			}
		}
	}
}

// TestDamageRectsCoverChangedPixels renders both frames and checks that every
// pixel that differs between them is inside the damage. This is what the
// compositor would show if it repainted only the damaged part.
func TestDamageRectsCoverChangedPixels(t *testing.T) {
	rz := openTestFont(t)
	cw, ch := rz.CellSize()
	const cols, rows = 40, 12
	width, height := cols*cw, rows*ch
	stride := width * 4
	for seed := int64(1); seed <= 12; seed++ {
		before := randomGrid(t, cols, rows, seed)
		after := randomGrid(t, cols, rows, seed+1000)
		rects := rz.DamageRects(after, before, nil, width, height)
		covered := coverage(rects, width, height)
		old := make([]byte, stride*height)
		now := make([]byte, stride*height)
		if err := rz.Draw(before, old, width, height, stride); err != nil {
			t.Fatal(err)
		}
		if err := rz.Draw(after, now, width, height, stride); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < len(old); i += 4 {
			if old[i] == now[i] && old[i+1] == now[i+1] && old[i+2] == now[i+2] && old[i+3] == now[i+3] {
				continue
			}
			p := i / 4
			if !covered[p] {
				t.Fatalf("seed %d: pixel %d,%d changed but is not damaged", seed, p%width, p/width)
			}
		}
	}
}

// TestDamageRectsCoverWideGlyphChange checks the two-cell footprint of a wide
// glyph. The glyph paints over the cell next to it, so that cell changes its
// pixels even when its own content did not change.
//
// The cases are checked on the geometry of the damage rather than on the
// rendered pixels, because the font the test uses may not carry the wide
// glyphs at all.
func TestDamageRectsCoverWideGlyphChange(t *testing.T) {
	rz := openTestFont(t)
	cw, ch := rz.CellSize()
	const cols, rows = 8, 2
	width, height := cols*cw, rows*ch
	for _, tc := range []struct {
		name          string
		before, after string
	}{
		{name: "narrow glyph gives way to a wide one", before: "a", after: "漢"},
		{name: "wide glyph gives way to a narrow one", before: "漢", after: "a"},
		{name: "wide glyph changes to another wide glyph", before: "龍", after: "漢"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, err := cell.Parse(tc.before, cols, rows)
			if err != nil {
				t.Fatal(err)
			}
			after, err := cell.Parse(tc.after, cols, rows)
			if err != nil {
				t.Fatal(err)
			}
			rects := rz.DamageRects(after, before, nil, width, height)
			covered := coverage(rects, width, height)
			for x := 0; x < 2; x++ {
				for y := 0; y < ch; y++ {
					for px := x * cw; px < (x+1)*cw; px++ {
						if !covered[y*width+px] {
							t.Fatalf("cell %d is not damaged by the wide glyph", x)
						}
					}
				}
			}
		})
	}
}

// TestDamageRectsFullWhenShapeChanges checks that a frame the compositor
// cannot compare against is damaged in full.
func TestDamageRectsFullWhenShapeChanges(t *testing.T) {
	rz := openTestFont(t)
	cw, ch := rz.CellSize()
	g := randomGrid(t, 8, 4, 7)
	rects := rz.DamageRects(g, nil, nil, 8*cw, 4*ch)
	if len(rects) != 1 || rects[0] != image.Rect(0, 0, 8*cw, 4*ch) {
		t.Fatalf("first frame damage = %v, want one full rectangle", rects)
	}
	resized := randomGrid(t, 9, 4, 8)
	rects = rz.DamageRects(resized, g, nil, 9*cw, 4*ch)
	if len(rects) != 1 || rects[0] != image.Rect(0, 0, 9*cw, 4*ch) {
		t.Fatalf("resized frame damage = %v, want one full rectangle", rects)
	}
}

// TestDamageRectsCoverFooterStrip checks that the credit band is damaged on
// every frame: the footer clears and repaints it whatever the effect does.
func TestDamageRectsCoverFooterStrip(t *testing.T) {
	rz := openTestFont(t)
	cw, ch := rz.CellSize()
	const cols, rows = 40, 12
	width, height := cols*cw, rows*ch
	before := randomGrid(t, cols, rows, 5)
	after := randomGrid(t, cols, rows, 5)
	before.SmallFooter = true
	after.SmallFooter = true
	rects := rz.DamageRects(after, before, nil, width, height)
	for _, rect := range rects {
		if rect.Min.X == 0 && rect.Max.X == width && rect.Max.Y == height {
			return
		}
	}
	t.Fatalf("the credit band is not damaged; rectangles %v", rects)
}

// TestDamageAreaStaysSmall checks the point of the change: the compositor
// receives a small part of the surface instead of all of it. The effects
// below change between 0.2% and 3% of their cells per frame, so the damage
// has to shrink by at least 80% against the full-surface damage it replaces.
//
// The rectangle count has to stay under the bound the Wayland layer applies
// (wayland.maxDamageRects), otherwise the frame falls back to the
// whole-surface damage and the effect above is lost again.
func TestDamageAreaStaysSmall(t *testing.T) {
	rz := openTestFont(t)
	cw, ch := rz.CellSize()
	const width, height = 3440, 1440
	const maxRects = 1024
	cols, rows := width/cw, height/ch
	for _, id := range []string{"matrix", "rain", "aquarium", "fireworks", "beams"} {
		t.Run(id, func(t *testing.T) {
			e, err := effect.New(id, "nord", cols, rows, "")
			if err != nil {
				t.Fatalf("effect %s: %v", id, err)
			}
			e.SetCellAspect(cw, ch)
			var previous *cell.Grid
			fractions := make([]float64, 0, 30)
			rectCount := 0
			for i := 0; i < 30; i++ {
				if err := e.TickElapsed(50 * time.Millisecond); err != nil {
					t.Fatal(err)
				}
				current := e.Grid()
				if current == nil {
					t.Fatal("tick produced no grid")
				}
				if previous != nil {
					rects := rz.DamageRects(current, previous, nil, width, height)
					area := 0
					for _, rect := range rects {
						area += rect.Dx() * rect.Dy()
					}
					rectCount = len(rects)
					if rectCount > maxRects {
						t.Fatalf("%s asks for %d rectangles, want at most %d", id, rectCount, maxRects)
					}
					fractions = append(fractions, float64(area)/float64(width*height))
				}
				previous = current
			}
			slices.Sort(fractions)
			median := fractions[len(fractions)/2]
			t.Logf("%s damages %.2f%% of the surface per frame in %d rectangles", id, median*100, rectCount)
			if median > 0.2 {
				t.Fatalf("%s damages %.2f%% of the surface, want at most 20%%", id, median*100)
			}
		})
	}
}

// TestDamageKeepsCompositorCanvasCurrent plays the part of the compositor: it
// keeps the surface contents and replaces only the damaged part of them with
// the new buffer. After every frame the surface has to show the frame that
// was drawn, with no stale region left over.
//
// The two buffers alternate, so a frame is measured against the frame the
// compositor last showed and not against the history of the buffer it lands
// in. A damage computed from the wrong frame leaves the old pixels on screen.
func TestDamageKeepsCompositorCanvasCurrent(t *testing.T) {
	rz := openTestFont(t)
	cw, ch := rz.CellSize()
	const cols, rows = 40, 12
	width, height := cols*cw, rows*ch
	stride := width * 4
	const frames = 12
	for _, smallFooter := range []bool{false, true} {
		name := "cells"
		if smallFooter {
			name = "credit"
		}
		t.Run(name, func(t *testing.T) {
			grids := make([]*cell.Grid, 0, frames)
			for seed := int64(1); seed <= frames; seed++ {
				g := randomGrid(t, cols, rows, seed)
				g.SmallFooter = smallFooter
				grids = append(grids, g)
			}
			canvas := make([]byte, stride*height)
			var buffers [2][]byte
			for i := range buffers {
				buffers[i] = make([]byte, stride*height)
			}
			var history [2]*cell.Grid
			var committed *cell.Grid
			for i, g := range grids {
				slot := i % 2
				if err := rz.DrawChanged(g, history[slot], buffers[slot], width, height, stride); err != nil {
					t.Fatal(err)
				}
				history[slot] = g
				rects := rz.DamageRects(g, committed, nil, width, height)
				for _, rect := range rects {
					r := rect.Intersect(image.Rect(0, 0, width, height))
					for y := r.Min.Y; y < r.Max.Y; y++ {
						copy(canvas[y*stride+r.Min.X*4:], buffers[slot][y*stride+r.Min.X*4:y*stride+r.Max.X*4])
					}
				}
				committed = g
				want := make([]byte, stride*height)
				if err := rz.Draw(g, want, width, height, stride); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(canvas, want) {
					p := 0
					for p < len(canvas) && canvas[p] == want[p] {
						p++
					}
					t.Fatalf("frame %d left the surface stale at pixel %d,%d", i, (p/4)%width, (p/4)/width)
				}
			}
		})
	}
}

// BenchmarkDamageRects measures the cost of the damage computation at the
// size of the reference output, for the effects that change the most cells.
func BenchmarkDamageRects(b *testing.B) {
	for _, id := range []string{"fire", "matrix"} {
		b.Run(id, func(b *testing.B) {
			rz := openTestFont(b)
			cw, ch := rz.CellSize()
			const width, height = 3440, 1440
			cols, rows := width/cw, height/ch
			e, err := effect.New(id, "nord", cols, rows, "")
			if err != nil {
				b.Fatalf("effect %s: %v", id, err)
			}
			e.SetCellAspect(cw, ch)
			var previous *cell.Grid
			for i := 0; i < 30; i++ {
				if err := e.TickElapsed(50 * time.Millisecond); err != nil {
					b.Fatal(err)
				}
				previous = e.Grid()
			}
			if err := e.TickElapsed(50 * time.Millisecond); err != nil {
				b.Fatal(err)
			}
			current := e.Grid()
			rects := make([]image.Rectangle, 0, 8192)
			// Warm the scratch buffers so the measurement is the steady state.
			rects = rz.DamageRects(current, previous, rects[:0], width, height)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rects = rz.DamageRects(current, previous, rects[:0], width, height)
			}
			b.StopTimer()
			b.ReportMetric(float64(len(rects)), "rects")
		})
	}
}
