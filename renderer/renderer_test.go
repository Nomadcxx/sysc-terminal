package renderer

import (
	"bytes"
	"github.com/Nomadcxx/sysc-terminal/internal/cell"
	"github.com/Nomadcxx/sysc-terminal/internal/effect"
	"strings"
	"testing"
)

func newRenderer(t *testing.T) *Renderer {
	t.Helper()
	r, err := New(Config{Effect: "rain", Palette: "nord", Width: 320, Height: 240, PixelSize: 12})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestRendererMatchesInternalPipeline(t *testing.T) {
	r := newRenderer(t)
	for range 3 {
		if err := r.Step(); err != nil {
			t.Fatal(err)
		}
		a, b := make([]byte, 320*240*4), make([]byte, 320*240*4)
		if _, err := r.Draw(a, 320*4, nil); err != nil {
			t.Fatal(err)
		}
		if err := r.raster.Draw(r.effect.Grid(), b, 320, 240, 320*4); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Fatal("wrapper differs from same effect/grid raster")
		}
	}
}
func TestRendererResizeInvalidatesPreviousFrame(t *testing.T) {
	r := newRenderer(t)
	_ = r.Step()
	a := make([]byte, 320*240*4)
	old, err := r.Draw(a, 320*4, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Resize(640, 480); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Draw(make([]byte, 640*480*4), 640*4, old); err == nil {
		t.Fatal("accepted frame before resize step")
	}
	_ = r.Step()
	b, c := make([]byte, 640*480*4), make([]byte, 640*480*4)
	_, err = r.Draw(b, 640*4, old)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Draw(c, 640*4, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, c) {
		t.Fatal("stale history skipped resized pixels")
	}
}
func TestRendererRejectsBufferShape(t *testing.T) {
	r := newRenderer(t)
	_ = r.Step()
	for _, stride := range []int{0, -1, 1279, 1280} {
		if _, err := r.Draw(make([]byte, 100), stride, nil); err == nil {
			t.Fatal("accepted invalid buffer")
		}
	}
	if err := r.Resize(1<<30, 1<<30); err == nil {
		t.Fatal("overflow accepted")
	}
}
func TestRendererBGRAOpaque(t *testing.T) {
	r := newRenderer(t)
	grid, err := cell.Parse("\x1b[38;2;255;0;1mX", 21, 24)
	if err != nil {
		t.Fatal(err)
	}
	r.effect, _ = effect.New("rain", "nord", 21, 24, "")
	dst := make([]byte, 320*240*4)
	if err = r.raster.Draw(grid, dst, 320, 240, 1280); err != nil {
		t.Fatal(err)
	}
	red := false
	for i := 0; i < len(dst); i += 4 {
		if dst[i+3] != 255 {
			t.Fatal("nonopaque")
		}
		if dst[i+2] > dst[i] {
			red = true
		}
	}
	if !red {
		t.Fatal("glyph not BGRA")
	}
}
func TestRendererPauseDoesNoWork(t *testing.T) {
	r := newRenderer(t)
	_ = r.Step()
	n := r.effect.RenderCount()
	r.SetPaused(true)
	for range 5 {
		if err := r.Step(); err != nil {
			t.Fatal(err)
		}
	}
	if r.effect.RenderCount() != n {
		t.Fatal("paused rendered")
	}
}
func TestRendererCatalogRejectsTextAndUnknown(t *testing.T) {
	for _, id := range []string{"fire-text", "matrix-art", "unknown"} {
		if err := Validate(id, "nord"); err == nil {
			t.Fatal("accepted", id)
		}
	}
	if err := Validate("rain", "unknown"); err == nil {
		t.Fatal("accepted unknown palette")
	}
	for _, id := range Effects() {
		if err := Validate(id, "nord"); err != nil {
			t.Fatal(id, err)
		}
	}
}

func TestRendererTextEffectsFitAHeader(t *testing.T) {
	if len(TextEffects()) == 0 {
		t.Fatal("text catalogue is empty")
	}
	for _, name := range TextEffects() {
		t.Run(name, func(t *testing.T) {
			r, err := New(Config{Effect: name, Palette: "eldritch", Text: "SYSC\nLOCKED", Width: 480, Height: 96, PixelSize: 10})
			if err != nil {
				t.Fatal(err)
			}
			if r.effect.EffectHeight() >= effect.MinimumRows {
				t.Fatal("header is forced into wallpaper geometry")
			}
			dst := make([]byte, 480*96*4)
			seen := false
			for range 80 {
				if err = r.Step(); err != nil {
					t.Fatal(err)
				}
				if _, err = r.Draw(dst, 480*4, nil); err != nil {
					t.Fatal(err)
				}
				for i := 0; i < len(dst); i += 4 {
					if dst[i] != 0 || dst[i+1] != 0 || dst[i+2] != 0 {
						seen = true
					}
				}
			}
			if !seen {
				t.Fatal("header frames are blank")
			}
		})
	}
}

func TestRendererRejectsUnsafeArtwork(t *testing.T) {
	for _, text := range []string{"bad\x1b[31m", "bad\x00", string([]byte{255}), strings.Repeat("x", (64<<10)+1)} {
		if _, err := New(Config{Effect: "print", Palette: "nord", Text: text, Width: 480, Height: 96}); err == nil {
			t.Fatal("unsafe artwork accepted")
		}
	}
}

func TestRendererRejectsUndersizedTextGeometry(t *testing.T) {
	if _, err := New(Config{Effect: "pour", Palette: "nord", Text: "SYSC", Width: 320, Height: 1}); err == nil {
		t.Fatal("undersized header accepted")
	}
}

func TestRendererShellPaletteCatalog(t *testing.T) {
	for _, name := range []string{"rose-pine", "kanagawa", "noctalia", "eldritch-abyss", "void", "red", "cyan", "coral", "pink"} {
		t.Run(name, func(t *testing.T) {
			if err := Validate("matrix", name); err != nil {
				t.Fatal(err)
			}
			r, err := New(Config{Effect: "matrix", Palette: name, Width: 320, Height: 240, PixelSize: 12})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Step(); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Draw(make([]byte, 320*240*4), 320*4, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}
