package wayland

import (
	"image"
	"testing"

	"github.com/Nomadcxx/sysc-terminal/internal/cell"
	"github.com/Nomadcxx/sysc-terminal/internal/raster"
)

func openTestRaster(t *testing.T) *raster.Rasterizer {
	t.Helper()
	path, err := findFont("")
	if err != nil {
		t.Fatalf("no system font: %v", err)
	}
	rz, err := raster.Open(path, 12)
	if err != nil {
		t.Fatalf("open font: %v", err)
	}
	return rz
}

func mustParseGrid(t *testing.T, s string, cols, rows int) *cell.Grid {
	t.Helper()
	g, err := cell.Parse(s, cols, rows)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return g
}

// TestDamageIsRelativeToLastCommit proves the damage follows the frame the
// compositor shows, not the frame a buffer slot happens to hold.
//
// The two shm slots alternate, so the slot a frame is painted into can hold a
// frame that is two commits old. Damaging against that slot would report the
// wrong rectangles and leave changed cells untouched.
func TestDamageIsRelativeToLastCommit(t *testing.T) {
	rz := openTestRaster(t)
	cw, ch := rz.CellSize()
	width, height := 8*cw, 2*ch

	first := mustParseGrid(t, "X", 4, 2)
	second := mustParseGrid(t, "YZ", 4, 2)
	third := mustParseGrid(t, "XZ", 4, 2)

	var d damageTracker
	rects, full := d.plan(rz, first, false, width, height)
	if full {
		t.Fatal("the first frame has no committed frame to compare against")
	}
	if len(rects) != 1 || rects[0] != image.Rect(0, 0, width, height) {
		t.Fatalf("first frame damage = %v, want the whole surface", rects)
	}
	d.commit(first)

	// The second frame goes into the other slot. The compositor shows the
	// first frame, so both cells of the first row changed.
	if _, full := d.plan(rz, second, false, width, height); full {
		t.Fatal("the second frame damaged the whole surface")
	}
	d.commit(second)

	// The third frame returns to the content of the first frame and goes back
	// into the first slot, which still holds the first frame. The compositor
	// shows the second one, so only the first cell changed. A tracker that
	// compared against the slot would report the second cell instead.
	rects, full = d.plan(rz, third, false, width, height)
	if full {
		t.Fatal("the third frame damaged the whole surface")
	}
	changed := image.Rect(0, 0, cw, ch)
	unchanged := image.Rect(cw, 0, 2*cw, ch)
	covered := false
	for _, rect := range rects {
		if rect.Overlaps(unchanged) {
			t.Fatalf("damage %v touches the cell that did not change", rect)
		}
		if rect.Intersect(changed) == changed {
			covered = true
		}
	}
	if !covered {
		t.Fatalf("damage %v does not cover the changed cell %v", rects, changed)
	}
}

// TestDamageFollowsBlurCost checks that a blurred frame is damaged in full:
// a blur spreads every changed pixel by its radius, so a rectangle around the
// change would leave the halo behind.
func TestDamageFollowsBlurCost(t *testing.T) {
	rz := openTestRaster(t)
	cw, ch := rz.CellSize()
	width, height := 8*cw, 2*ch

	var d damageTracker
	grid := mustParseGrid(t, "X", 4, 2)
	d.commit(grid)
	rects, full := d.plan(rz, grid, true, width, height)
	if !full || len(rects) != 0 {
		t.Fatalf("blurred frame damage = %v (full=%t), want the whole surface", rects, full)
	}
}
