package effect

import (
	"strings"
	"testing"
	"time"
)

func TestANSILoveLifecycle(t *testing.T) {
	if !strings.Contains(List(), "effect ansi-love 0\n") {
		t.Fatal("ANSI Love missing from registry list")
	}
	e, err := New("ansi-love", "nord", 80, 24, "")
	if err != nil {
		t.Fatal(err)
	}
	e.SetCellAspect(7, 16)
	if err := e.TickElapsed(0); err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		g := e.Grid()
		if !g.SmallFooter {
			t.Fatal("ANSI Love credit must use small footer")
		}
		var b strings.Builder
		for x := 0; x < g.Cols; x++ {
			b.WriteRune(g.At(x, g.Rows-1).Ch)
		}
		if !strings.HasSuffix(b.String(), "Drawn by Warpus, December 2019") {
			t.Fatalf("credit %q", b.String())
		}
	}
	check()
	e.SetPaused(true)
	before := e.Generation()
	if err := e.TickElapsed(30 * time.Second); err != nil {
		t.Fatal(err)
	}
	if e.Generation() != before {
		t.Fatal("paused effect advanced")
	}
	e.SetPaused(false)
	if err := e.Resize(120, 36); err != nil {
		t.Fatal(err)
	}
	if err := e.Redraw(); err != nil {
		t.Fatal(err)
	}
	check()
	if err := e.Reset(); err != nil {
		t.Fatal(err)
	}
	if err := e.TickElapsed(time.Second); err != nil {
		t.Fatal(err)
	}
	check()
}

func TestANSILoveDefaultsToSextant(t *testing.T) {
	e, err := New("ansi-love", "nord", 80, 24, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Tick(); err != nil {
		t.Fatal(err)
	}
	for y := 0; y < e.Grid().Rows; y++ {
		for x := 0; x < e.Grid().Cols; x++ {
			if c := e.Grid().At(x, y); c.Ch >= 0x1FB00 && c.Ch <= 0x1FB3B {
				return
			}
		}
	}
	t.Fatal("default ANSI Love grid contains no Sextant glyphs")
}

func TestANSILoveBackendOptionSelectsQuad(t *testing.T) {
	e, err := NewWithOptions("ansi-love", "nord", 80, 24, "", Options{ANSILoveBackend: "quad"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Tick(); err != nil {
		t.Fatal(err)
	}
	for y := 0; y < e.Grid().Rows; y++ {
		for x := 0; x < e.Grid().Cols; x++ {
			if c := e.Grid().At(x, y); c.Ch >= 0x2580 && c.Ch <= 0x259F {
				return
			}
		}
	}
	t.Fatal("Quad option rendered no quadrant block glyphs")
}

func TestANSILoveBackendOptionRejectsUnknown(t *testing.T) {
	if _, err := NewWithOptions("ansi-love", "nord", 80, 24, "", Options{ANSILoveBackend: "kitty"}); err == nil {
		t.Fatal("unknown ANSI Love backend accepted")
	}
}
