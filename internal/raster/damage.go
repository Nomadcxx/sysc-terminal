package raster

import (
	"image"

	"github.com/Nomadcxx/sysc-terminal/internal/cell"
)

// damageSpan is a run of cells in cell units, half open: [start, end).
type damageSpan struct {
	start, end int
}

// DamageRects appends the buffer-pixel rectangles whose contents differ
// between g and previous to dst and returns the extended slice.
//
// The rectangles are in buffer (device pixel) coordinates, which is what
// wl_surface.damage_buffer takes. previous must be the grid of the frame the
// compositor already shows, not the grid of the buffer being painted:
// DrawChanged's previous argument describes the buffer's own history, while
// surface damage is relative to the compositor's contents.
//
// dst belongs to the caller and is reused between frames, so a steady-state
// frame allocates nothing.
func (r *Rasterizer) DamageRects(g, previous *cell.Grid, dst []image.Rectangle, width, height int) []image.Rectangle {
	dst = dst[:0]
	if r == nil || g == nil || width <= 0 || height <= 0 {
		return dst
	}
	cellW, cellH := r.cellW, r.cellH
	if cellW <= 0 || cellH <= 0 {
		return dst
	}
	if previous == nil || previous.Cols != g.Cols || previous.Rows != g.Rows || previous.SmallFooter != g.SmallFooter {
		// A resize, a scale change or a footer change invalidates the whole
		// buffer: the compositor holds a frame of a different shape, so every
		// pixel has to be repainted.
		return append(dst, image.Rect(0, 0, width, height))
	}
	if g == previous {
		return dst
	}

	// The cell loop stops at the first cell that does not fit, so the damage
	// never claims pixels the renderer left untouched.
	drawCols := g.Cols
	if fit := (width + cellW - 1) / cellW; drawCols > fit {
		drawCols = fit
	}
	drawRows := g.Rows
	if fit := (height + cellH - 1) / cellH; drawRows > fit {
		drawRows = fit
	}
	if g.SmallFooter {
		// The last row is painted by drawSmallFooter, not by the cell loop.
		drawRows--
	}

	// A run in a row is one rectangle. Runs that repeat in the row below grow
	// the same rectangle instead of adding one, so a block of cells that
	// changes together costs one request rather than one per row.
	prevSpans, curSpans := r.spansA[:0], r.spansB[:0]
	prevIdx, curIdx := r.idxA[:0], r.idxB[:0]
	for y := 0; y < drawRows; y++ {
		curSpans = r.rowSpans(g, previous, y, drawCols, curSpans[:0])
		curIdx = curIdx[:0]
		i, j := 0, 0
		for j < len(curSpans) {
			cs := curSpans[j]
			matched := false
			for i < len(prevSpans) {
				ps := prevSpans[i]
				if ps.start > cs.start {
					break
				}
				if ps == cs {
					matched = true
					break
				}
				i++
			}
			if matched {
				idx := prevIdx[i]
				// The run below repeats the run above, so the rectangle grows
				// by one row instead of a new one being added.
				rect := dst[idx]
				rect.Max.Y += cellH
				dst[idx] = rect
				curIdx = append(curIdx, idx)
				i++
			} else {
				dst = append(dst, image.Rect(cs.start*cellW, y*cellH, cs.end*cellW, (y+1)*cellH))
				curIdx = append(curIdx, len(dst)-1)
			}
			j++
		}
		prevSpans, curSpans = curSpans, prevSpans
		prevIdx, curIdx = curIdx, prevIdx
	}
	// The scratch buffers go back to the rasterizer so the next frame reuses
	// them instead of allocating.
	r.spansA, r.spansB = prevSpans, curSpans
	r.idxA, r.idxB = prevIdx, curIdx

	// The footer credit is cleared and painted on every frame, so its band is
	// always damaged even when no cell changed.
	if g.SmallFooter {
		dst = append(dst, r.footerStrip(g, width, height))
	}

	// The character grid can overhang the right or bottom edge of the buffer.
	out := dst[:0]
	for _, rect := range dst {
		rect = rect.Intersect(image.Rect(0, 0, width, height))
		if rect.Empty() {
			continue
		}
		out = append(out, rect)
	}
	return out
}

// rowSpans returns the runs of cells in row y whose pixels differ between g
// and previous, in cell units. previous must have the same shape as g.
func (r *Rasterizer) rowSpans(g, previous *cell.Grid, y, cols int, dst []damageSpan) []damageSpan {
	dst = dst[:0]
	grow := g.Row(y)
	prow := previous.Row(y)
	// Cutting the rows to the drawable width lets the compiler drop the
	// bounds check from the scan below.
	if len(grow) > cols {
		grow = grow[:cols]
	}
	if len(prow) > cols {
		prow = prow[:cols]
	}
	for x := 0; x < cols; x++ {
		// A cell is dirty when its content changed or when the glyph it holds
		// changed width. The width decides the narrow tile path against the
		// two-cell clear a wide glyph needs, and it also decides whether the
		// cell to the right is covered by this glyph.
		changed := grow[x] != prow[x]
		wideNow := false
		wideBefore := false
		if x+1 < cols {
			wideNow = grow[x+1].Ch == 0
			wideBefore = prow[x+1].Ch == 0
		}
		if !changed && wideNow == wideBefore {
			continue
		}
		end := x + 1
		if wideNow || wideBefore {
			// A wide glyph clears and paints two cells.
			end = x + 2
		}
		if n := len(dst); n > 0 && dst[n-1].end >= x {
			// Overlaps or touches the previous run: one rectangle covers both.
			if dst[n-1].end < end {
				dst[n-1].end = end
			}
			continue
		}
		dst = append(dst, damageSpan{start: x, end: end})
	}
	return dst
}

// footerStrip returns the full-width band that drawSmallFooter clears on
// every frame.
func (r *Rasterizer) footerStrip(g *cell.Grid, width, height int) image.Rectangle {
	f, err := r.footerRasterizer()
	if err != nil || f == nil {
		return image.Rect(0, 0, width, height)
	}
	row := g.Rows - 1
	padding := max(1, (r.cellH-f.cellH)/2)
	y := height - padding - f.cellH
	top := max(0, min(row*r.cellH, y))
	return image.Rect(0, top, width, height)
}
