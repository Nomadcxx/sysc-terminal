package raster

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-terminal/internal/cell"
	"github.com/go-text/typesetting/font"
)

var fontPaths = []string{
	"/usr/share/fonts/TTF/JetBrainsMonoNerdFont-Regular.ttf",
	"/usr/share/fonts/TTF/JetBrainsMono-Regular.ttf",
	"/usr/share/fonts/noto/NotoSansMono-Regular.ttf",
	// Ubuntu runners and minimal installs only ship DejaVu.
	"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
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

func TestRejectsMachineIntOverflowGeometry(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	if _, _, err := BufferSize(maxInt, 1, 1); err == nil {
		t.Fatal("machine-int overflowing buffer size accepted")
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
		if dst[i+2] != 0 && dst[i+3] == 255 {
			covered++
		}
	}
	if covered == 0 {
		t.Fatal("no coverage in ARGB buffer; glyph was not rasterised")
	}
}

func TestBlankCellIsOpaqueBlack(t *testing.T) {
	rz := openTestFont(t)
	g, err := cell.Parse(" ", 1, 1)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cw, ch := rz.CellSize()
	stride, total, err := BufferSize(cw, ch, 1)
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	dst := make([]byte, total)
	if err := rz.Draw(g, dst, cw, ch, stride); err != nil {
		t.Fatalf("draw: %v", err)
	}
	for i := 0; i+3 < len(dst); i += 4 {
		if dst[i] != 0 || dst[i+1] != 0 || dst[i+2] != 0 || dst[i+3] != 255 {
			t.Fatalf("pixel at byte %d = %v, want opaque black", i, dst[i:i+4])
		}
	}
}

func TestWideGlyphBlitsAcrossItsSecondCell(t *testing.T) {
	rz := openTestFont(t)
	face := font.NewFace(rz.font)
	face.SetPpem(rz.ppem, rz.ppem)
	gid, ok := face.NominalGlyph('界')
	if !ok {
		gid = 0
	}
	g, err := cell.Parse("界A", 3, 1)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	wideMask := image.NewAlpha(image.Rect(0, 0, 2*rz.cellW, rz.cellH))
	for y := 0; y < rz.cellH; y++ {
		for x := rz.cellW; x < 2*rz.cellW; x++ {
			wideMask.Pix[y*wideMask.Stride+x] = 255
		}
	}
	rz.cache[rasterKey{gid: gid, ppem: rz.ppem}] = wideMask
	width, height := 3*rz.cellW, rz.cellH
	stride, total, err := BufferSize(width, height, 1)
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	dst := make([]byte, total)
	if err := rz.Draw(g, dst, width, height, stride); err != nil {
		t.Fatalf("draw: %v", err)
	}
	covered := 0
	for y := 0; y < height; y++ {
		for x := rz.cellW; x < 2*rz.cellW; x++ {
			off := y*stride + x*4
			if dst[off] != 0 || dst[off+1] != 0 || dst[off+2] != 0 {
				covered++
			}
		}
	}
	if covered == 0 {
		t.Fatal("wide glyph did not paint any pixels in its second cell")
	}
}

func TestDrawChangesMatchesFullDrawAcrossFrames(t *testing.T) {
	rz := openTestFont(t)
	cw, ch := rz.CellSize()
	const cols, rows = 4, 2
	width, height := cols*cw, rows*ch
	stride, total, err := BufferSize(width, height, 1)
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	frames := []string{
		"\033[38;2;255;0;0mA界\033[0m  \n\033[48;2;10;20;30m X \033[0m",
		"\033[38;2;0;255;0mABCD\033[0m\n\033[48;2;40;50;60m Y \033[0m",
		" \033[38;2;0;0;255m界Z\033[0m \n    ",
		"\033[48;2;1;2;3m    \033[0m\n\033[38;2;0;0;255m界Q\033[0m ",
	}
	dirty := make([]byte, total)
	var previous *cell.Grid
	for i, frame := range frames {
		current, err := cell.Parse(frame, cols, rows)
		if err != nil {
			t.Fatalf("parse frame %d: %v", i, err)
		}
		if err := rz.DrawChanged(current, previous, dirty, width, height, stride); err != nil {
			t.Fatalf("draw changed frame %d: %v", i, err)
		}
		full := make([]byte, total)
		if err := rz.Draw(current, full, width, height, stride); err != nil {
			t.Fatalf("draw full frame %d: %v", i, err)
		}
		if !bytes.Equal(dirty, full) {
			t.Fatalf("changed draw differs from full draw at frame %d", i)
		}
		previous = current
	}
}

func TestMixMatchesExactAlpha(t *testing.T) {
	for dst := 0; dst < 256; dst++ {
		for src := 0; src < 256; src++ {
			for cov := 0; cov < 256; cov++ {
				want := byte((dst*(255-cov) + src*cov) / 255)
				if got := mix(byte(dst), byte(src), byte(cov)); got != want {
					t.Fatalf("mix(%d, %d, %d) = %d, want %d", dst, src, cov, got, want)
				}
			}
		}
	}
}

func TestSetPixelSizeUpdatesCellMetrics(t *testing.T) {
	rz := openTestFont(t)
	baseW, baseH := rz.CellSize()
	if err := rz.SetPixelSize(24); err != nil {
		t.Fatalf("set pixel size: %v", err)
	}
	largeW, largeH := rz.CellSize()
	if largeW <= baseW || largeH <= baseH {
		t.Fatalf("24px cell %dx%d did not grow beyond 12px cell %dx%d", largeW, largeH, baseW, baseH)
	}
	if err := rz.SetPixelSize(12); err != nil {
		t.Fatalf("restore pixel size: %v", err)
	}
	if gotW, gotH := rz.CellSize(); gotW != baseW || gotH != baseH {
		t.Fatalf("restored cell %dx%d, want %dx%d", gotW, gotH, baseW, baseH)
	}
}

func TestSecondFrameReusesGlyphMasks(t *testing.T) {
	f := newFixture(t)
	// A font-drawn glyph; block elements bypass this cache for geometric masks.
	g := f.solidGrid('@', 100)
	f.Draw(g)
	cached := make(map[rasterKey]*image.Alpha, len(f.rz.cache))
	for key, mask := range f.rz.cache {
		cached[key] = mask
	}
	if len(cached) == 0 {
		t.Fatal("first frame did not populate the glyph cache")
	}

	f.Draw(g)
	if len(f.rz.cache) != len(cached) {
		t.Fatalf("cache size after identical frame = %d, want %d", len(f.rz.cache), len(cached))
	}
	for key, mask := range cached {
		if f.rz.cache[key] != mask {
			t.Fatalf("identical frame replaced cached mask for glyph %d", key.gid)
		}
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

func TestFontMissingFails(t *testing.T) {
	_, err := Open("/no/such/font.ttf", 12)
	if err == nil {
		t.Fatal("missing font accepted")
	}
}

func TestCellTileCacheMatchesRasterAndStaysBounded(t *testing.T) {
	rz := openTestFont(t)
	cw, ch := rz.CellSize()
	for _, frame := range []string{"\033[38;2;127;31;240mABC ", "\033[48;2;20;70;90mX Y ", "    "} {
		g, err := cell.Parse(frame, 4, 1)
		if err != nil {
			t.Fatal(err)
		}
		stride, total, _ := BufferSize(4*cw-2, ch-1, 1)
		got, want := make([]byte, total), make([]byte, total)
		rz.fillBlack(want, stride, 4*cw-2, ch-1)
		for x := 0; x < 4; x++ {
			rz.drawCell(g, x, 0, g.At(x, 0), want, 4*cw-2, ch-1, stride)
		}
		if err := rz.Draw(g, got, 4*cw-2, ch-1, stride); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("cached raster differs for %q", frame)
		}
	}
	if len(rz.tiles) == 0 {
		t.Fatal("no coloured cell tiles cached")
	}
	g, _ := cell.Parse("X", 1, 1)
	for i := 0; i < 600; i++ {
		c := g.At(0, 0)
		c.Fg = color.RGBA{R: byte(i), G: byte(i >> 8), A: 255}
		rz.tile(c)
	}
	if len(rz.tiles) > rasterCacheMax {
		t.Fatalf("tile cache grew to %d", len(rz.tiles))
	}
	uncached := cell.Cell{Ch: 'X', Fg: color.RGBA{R: 90, G: 100, B: 210, A: 255}}
	if rz.tile(uncached) != nil {
		t.Fatal("full cache allocated a tile for a new colour")
	}
	if allocs := testing.AllocsPerRun(100, func() { rz.tile(uncached) }); allocs != 0 {
		t.Fatalf("full cache allocated %.0f objects for new colours", allocs)
	}

	if err := rz.SetPixelSize(15); err != nil {
		t.Fatal(err)
	}
	if len(rz.tiles) != 0 {
		t.Fatal("font resize retained stale tiles")
	}
}

func TestRasterizeWideGlyphIncludesSecondCell(t *testing.T) {
	path := "/usr/share/fonts/droid/DroidSansFallbackFull.ttf"
	if _, err := os.Stat(path); err != nil {
		t.Skipf("CJK test font unavailable: %v", err)
	}
	rz, err := Open(path, 12)
	if err != nil {
		t.Fatalf("open CJK font: %v", err)
	}
	face := font.NewFace(rz.font)
	face.SetPpem(rz.ppem, rz.ppem)
	gid, ok := face.NominalGlyph('界')
	if !ok {
		t.Fatal("CJK font has no glyph for 界")
	}
	mask := rz.raster(face, gid)
	if mask.Bounds().Max.X <= rz.cellW {
		t.Fatalf("wide glyph ink ends at x=%d, cell width=%d", mask.Bounds().Max.X, rz.cellW)
	}
	for y := mask.Bounds().Min.Y; y < mask.Bounds().Max.Y; y++ {
		for x := rz.cellW; x < mask.Bounds().Max.X; x++ {
			if mask.AlphaAt(x, y).A != 0 {
				return
			}
		}
	}
	t.Fatal("wide glyph has no ink in its second cell")
}

func TestOpenRejectsFontOverMemoryLimit(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "oversized-font-*.ttf")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate((32 << 20) + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = Open(f.Name(), 12)
	if err == nil || !strings.Contains(err.Error(), "exceeds 32 MiB") {
		t.Fatalf("oversized font error = %v", err)
	}
}

// Block art tiles only if block elements fill their cell exactly, the way
// kitty draws them; font glyphs leave seams between rows and columns.
func TestBlockElementsTileWithoutSeams(t *testing.T) {
	rz := openTestFont(t)
	cw, ch := rz.CellSize()
	draw := func(art string, cols, rows int) []byte {
		t.Helper()
		g, err := cell.Parse(art, cols, rows)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		w, h := cols*cw, rows*ch
		stride, total, err := BufferSize(w, h, 1)
		if err != nil {
			t.Fatalf("size: %v", err)
		}
		dst := make([]byte, total)
		if err := rz.Draw(g, dst, w, h, stride); err != nil {
			t.Fatalf("draw: %v", err)
		}
		return dst
	}
	red := "\033[38;2;255;0;0m"
	full := draw(red+"██\n"+red+"██", 2, 2)
	for i := 0; i < len(full); i += 4 {
		if full[i+2] != 255 {
			t.Fatalf("full block leaves a seam at pixel %d", i/4)
		}
	}
	// The upper half block covers exactly the top half of its cell.
	half := draw(red+"▀", 1, 1)
	for y := 0; y < ch; y++ {
		want := byte(0)
		if y < ch/2 {
			want = 255
		}
		for x := 0; x < cw; x++ {
			if got := half[(y*cw+x)*4+2]; got != want {
				t.Fatalf("upper half block row %d col %d = %d, want %d", y, x, got, want)
			}
		}
	}
	// A horizontal light line spans the cell edge to edge so lines join.
	line := draw(red+"──", 2, 1)
	for x := 0; x < 2*cw; x++ {
		lit := false
		for y := 0; y < ch; y++ {
			lit = lit || line[(y*2*cw+x)*4+2] == 255
		}
		if !lit {
			t.Fatalf("horizontal line breaks at column %d", x)
		}
	}
}
