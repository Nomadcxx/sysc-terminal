package raster

import "image"

// Block elements (U+2580..U+259F) and light box lines are drawn as exact
// coverage of the cell, the way kitty draws them, rather than from font
// glyphs: font block glyphs rarely fill the integer cell, so block art shows
// a grid of seams between rows and columns.

// Light box-drawing arms.
const (
	armL = 1 << iota
	armR
	armU
	armD
)

var lightBox = map[rune]uint8{
	'─': armL | armR, '│': armU | armD,
	'┌': armR | armD, '┐': armL | armD, '└': armU | armR, '┘': armU | armL,
	'├': armU | armD | armR, '┤': armU | armD | armL,
	'┬': armL | armR | armD, '┴': armL | armR | armU, '┼': armL | armR | armU | armD,
	'╴': armL, '╵': armU, '╶': armR, '╷': armD,
}

// Quadrants U+2596..U+259F as upper-left, upper-right, lower-left,
// lower-right bits.
var quadrants = [...]uint8{4, 8, 1, 1 | 4 | 8, 1 | 8, 1 | 2 | 4, 1 | 2 | 8, 2, 2 | 4, 2 | 4 | 8}

// blockMask is the cell-sized coverage for a block element or light box
// line, or nil for runes the font draws. The set is fixed, so the per-size
// cache is bounded without eviction.
func (r *Rasterizer) blockMask(ch rune) *image.Alpha {
	if m, ok := r.blocks[ch]; ok {
		return m
	}
	w, h := r.cellW, r.cellH
	m := image.NewAlpha(image.Rect(0, 0, w, h))
	fill := func(rect image.Rectangle, a uint8) {
		rect = rect.Intersect(m.Rect)
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			for x := rect.Min.X; x < rect.Max.X; x++ {
				m.Pix[y*m.Stride+x] = a
			}
		}
	}
	eighths := func(x0, y0, x1, y1 int) image.Rectangle {
		return image.Rect(w*x0/8, h*y0/8, w*x1/8, h*y1/8)
	}
	switch {
	case ch == '▀':
		fill(eighths(0, 0, 8, 4), 255)
	case ch >= '▁' && ch <= '█':
		fill(eighths(0, 8-int(ch-'▀'), 8, 8), 255)
	case ch >= '▉' && ch <= '▏':
		fill(eighths(0, 0, int('▐'-ch), 8), 255)
	case ch == '▐':
		fill(eighths(4, 0, 8, 8), 255)
	case ch >= '░' && ch <= '▓':
		fill(m.Rect, uint8(64*int(ch-'▐')-1))
	case ch == '▔':
		fill(eighths(0, 0, 8, 1), 255)
	case ch == '▕':
		fill(eighths(7, 0, 8, 8), 255)
	case ch >= '▖' && ch <= '▟':
		q := quadrants[ch-'▖']
		for bit, sub := range [4]image.Rectangle{eighths(0, 0, 4, 4), eighths(4, 0, 8, 4), eighths(0, 4, 4, 8), eighths(4, 4, 8, 8)} {
			if q&(1<<bit) != 0 {
				fill(sub, 255)
			}
		}
	default:
		arms, ok := lightBox[ch]
		if !ok {
			return nil
		}
		t := max(1, h/16)
		cx, cy := (w-t)/2, (h-t)/2
		for bit, arm := range [4]image.Rectangle{
			image.Rect(0, cy, cx+t, cy+t), image.Rect(cx, cy, w, cy+t),
			image.Rect(cx, 0, cx+t, cy+t), image.Rect(cx, cy, cx+t, h),
		} {
			if arms&(1<<bit) != 0 {
				fill(arm, 255)
			}
		}
	}
	if r.blocks == nil {
		r.blocks = make(map[rune]*image.Alpha)
	}
	r.blocks[ch] = m
	return m
}
