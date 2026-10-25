package wayland

import (
	"image"

	"github.com/Nomadcxx/sysc-terminal/internal/cell"
	"github.com/Nomadcxx/sysc-terminal/internal/raster"
)

// damageTracker decides which part of a frame the compositor has to repaint.
//
// Surface damage is relative to what the compositor already shows, so the
// tracker remembers the grid of the last committed frame. The buffer slot
// being painted holds an older frame than that, and the difference between
// the two is what keeps the damage narrower than the redraw.
type damageTracker struct {
	committed *cell.Grid
	rects     []image.Rectangle
}

// reset forgets the committed frame. The next frame damages the whole
// surface, because the compositor holds a frame of a different shape.
func (d *damageTracker) reset() {
	d.committed = nil
}

// plan returns the buffer-pixel rectangles that changed since the last commit
// and whether the whole surface has to be damaged instead.
//
// blurred marks a frame that went through the blur pass: a blur spreads every
// changed pixel by its radius, so such a frame is damaged in full.
func (d *damageTracker) plan(r *raster.Rasterizer, g *cell.Grid, blurred bool, width, height int) ([]image.Rectangle, bool) {
	if blurred || r == nil || g == nil {
		return nil, true
	}
	d.rects = r.DamageRects(g, d.committed, d.rects[:0], width, height)
	return d.rects, false
}

// commit records g as the frame the compositor now shows.
func (d *damageTracker) commit(g *cell.Grid) {
	d.committed = g
}
