package raster

import (
	"math"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-terminal/internal/cell"
)

var fontPaths = []string{
	"/usr/share/fonts/TTF/JetBrainsMonoNerdFont-Regular.ttf",
	"/usr/share/fonts/TTF/JetBrainsMono-Regular.ttf",
	"/usr/share/fonts/noto/NotoSansMono-Regular.ttf",
}

func openTestFont(t *testing.T) *Rasterizer {
	t.Helper()
	var tried []string
	for _, p := range fontPaths {
		tried = append(tried, p)
		r, err := Open(p, 12)
		if err == nil {
			return r
		}
	}
	t.Fatalf("no system font; tried %v", tried)
	return nil
}

func TestRejectsOverflowGeometry(t *testing.T) {
	_, _, err := BufferSize(math.MaxInt32, math.MaxInt32, 2)
	if err == nil {
		t.Fatal("overflowing buffer size accepted")
	}
}

func TestDrawsColouredGlyph(t *testing.T) {
	rz := openTestFont(t)
	g, err := cell.Parse("\033[38;2;255;0;0mX\033[0m ", 2, 1)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cw, ch := rz.CellSize()
	if cw < 1 || ch < 1 {
		t.Fatalf("cell %dx%d", cw, ch)
	}
	w, h := 2*cw, ch
	stride, total, err := BufferSize(w, h, 1)
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	dst := make([]byte, total)
	if err := rz.Draw(g, dst, w, h, stride); err != nil {
		t.Fatalf("draw: %v", err)
	}
	covered := 0
	for i := 0; i+3 < len(dst); i += 4 {
		if dst[i] != 0 || dst[i+1] != 0 || dst[i+2] != 0 || dst[i+3] != 0 {
			covered++
		}
	}
	if covered == 0 {
		t.Fatal("no coverage in ARGB buffer; glyph was not rasterised")
	}
}

func TestSecondFrameCostsNoMoreThanFirst(t *testing.T) {
	f := newFixture(t)
	g := f.solidGrid('▒', 100)
	first := f.timeN(func() { f.Draw(g) }, 5)
	second := f.timeN(func() { f.Draw(g) }, 5)
	if second > first*105/100 {
		t.Fatalf("cached frame %v slower than cold %v; glyph cache is not being hit", second, first)
	}
}

type fixture struct {
	t  *testing.T
	rz *Rasterizer
	w  int
	h  int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	rz := openTestFont(t)
	cw, ch := rz.CellSize()
	return &fixture{t: t, rz: rz, w: 10 * cw, h: 10 * ch}
}

func (f *fixture) solidGrid(ch rune, n int) *cell.Grid {
	f.t.Helper()
	cols := 10
	rows := (n + cols - 1) / cols
	s := ""
	for i := 0; i < n; i++ {
		s += "\033[38;2;200;80;40m" + string(ch) + "\033[0m"
		if (i+1)%cols == 0 {
			s += "\n"
		}
	}
	g, err := cell.Parse(s, cols, rows)
	if err != nil {
		f.t.Fatalf("parse: %v", err)
	}
	return g
}

func (f *fixture) Draw(g *cell.Grid) {
	f.t.Helper()
	stride, total, err := BufferSize(f.w, f.h, 1)
	if err != nil {
		f.t.Fatalf("size: %v", err)
	}
	dst := make([]byte, total)
	if err := f.rz.Draw(g, dst, f.w, f.h, stride); err != nil {
		f.t.Fatalf("draw: %v", err)
	}
}

func (f *fixture) timeN(fn func(), n int) time.Duration {
	start := time.Now()
	for i := 0; i < n; i++ {
		fn()
	}
	return time.Since(start)
}

func TestFontMissingFails(t *testing.T) {
	_, err := Open("/no/such/font.ttf", 12)
	if err == nil {
		t.Fatal("missing font accepted")
	}
}
