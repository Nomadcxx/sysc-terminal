package cell

import (
	"image/color"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-Go/animations"
)

func TestFireFrameFillsGrid(t *testing.T) {
	pal := animations.GetFirePalette("nord")
	fx := animations.NewFireEffect(80, 24, pal)
	for i := 0; i < 5; i++ {
		fx.Update()
	}
	g, err := Parse(fx.Render(), 80, 24)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if g.Cols != 80 || g.Rows != 24 {
		t.Fatalf("grid %dx%d", g.Cols, g.Rows)
	}
	lit, coloured := 0, 0
	for y := 0; y < g.Rows; y++ {
		for x := 0; x < g.Cols; x++ {
			c := g.At(x, y)
			if c.Ch != ' ' && c.Ch != 0 {
				lit++
			}
			if c.Fg != DefaultFg {
				coloured++
			}
		}
	}
	if lit < 100 {
		t.Fatalf("only %d non-space cells in 1920; parser is dropping content", lit)
	}
	if coloured != lit {
		t.Fatalf("%d of %d lit cells carry no 38;2 colour; SGR is being lost", lit-coloured, lit)
	}
	row0lit := 0
	for x := 0; x < g.Cols; x++ {
		if c := g.At(x, 0); c.Ch != ' ' && c.Ch != 0 {
			row0lit++
		}
	}
	if row0lit == 0 {
		t.Fatal("row 0 is all spaces; fire is not bottom-anchored in the first frames")
	}
}

func TestRejectsOSC(t *testing.T) {
	g, err := Parse("A\x1b]52;c;AAAA\x07B", 2, 1)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if g.At(0, 0).Ch != 'A' || g.At(1, 0).Ch != 'B' {
		t.Fatalf("osc shifted cells: %+v %+v", g.At(0, 0), g.At(1, 0))
	}
}

func TestMalformedCSIDoesNotPanic(t *testing.T) {
	g, err := Parse("x\x1b[38;2;1y", 8, 1)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if g.At(0, 0).Ch != 'x' {
		t.Fatalf("col 0 = %q, want 'x'", g.At(0, 0).Ch)
	}
}

func TestWideRuneOccupiesTwoCells(t *testing.T) {
	g, err := Parse("A界B", 4, 1)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if g.At(0, 0).Ch != 'A' || g.At(1, 0).Ch != '界' || g.At(2, 0).Ch != 0 || g.At(3, 0).Ch != 'B' {
		t.Fatalf("unexpected wide-rune cells: %+v %+v %+v %+v", g.At(0, 0), g.At(1, 0), g.At(2, 0), g.At(3, 0))
	}
}

func TestCombiningMarkDoesNotShiftNextCell(t *testing.T) {
	g, err := Parse("A\u0301B", 2, 1)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if g.At(0, 0).Ch != 'A' || g.At(1, 0).Ch != 'B' {
		t.Fatalf("unexpected combining-mark cells: %+v %+v", g.At(0, 0), g.At(1, 0))
	}
}

func TestOutOfRangeSGRIsIgnored(t *testing.T) {
	g, err := Parse("\x1b[38;2;999;0;0mX", 1, 1)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := g.At(0, 0).Fg; got != DefaultFg {
		t.Fatalf("malformed RGB changed foreground to %+v", got)
	}
}

func TestSGRSubsetAppliesRGBAndReset(t *testing.T) {
	g, err := Parse("\x1b[38;2;1;2;3mA\x1b[48;2;4;5;6mB\x1b[31mC\x1b[mD\x1b[38;2;7;8;9mE\x1b[0mF", 6, 1)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := g.At(0, 0).Fg; got != (color.RGBA{R: 1, G: 2, B: 3, A: 255}) {
		t.Fatalf("foreground = %+v, want RGB(1,2,3)", got)
	}
	if got := g.At(1, 0).Bg; got != (color.RGBA{R: 4, G: 5, B: 6, A: 255}) {
		t.Fatalf("background = %+v, want RGB(4,5,6)", got)
	}
	if got := g.At(2, 0).Fg; got != (color.RGBA{R: 1, G: 2, B: 3, A: 255}) {
		t.Fatalf("unsupported SGR changed foreground to %+v", got)
	}
	if got := g.At(3, 0); got.Fg != DefaultFg || got.Bg != DefaultBg {
		t.Fatalf("empty SGR did not reset: %+v", got)
	}
	if got := g.At(5, 0); got.Fg != DefaultFg || got.Bg != DefaultBg {
		t.Fatalf("zero SGR did not reset: %+v", got)
	}
}

func TestRejectsOversizedFrame(t *testing.T) {
	const frameLimit = 16 << 20
	if _, err := Parse(strings.Repeat("x", frameLimit+1), 1, 1); err == nil {
		t.Fatal("oversized rendered frame accepted")
	}
}
