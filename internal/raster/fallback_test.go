package raster

import (
	"os"
	"testing"

	"github.com/Nomadcxx/sysc-terminal/internal/cell"
)

// fallbackHostFace returns an installed face that covers runes the bundled
// JetBrains Mono Regular lacks (U+25CD), or skips the test.
func fallbackHostFace(t *testing.T) string {
	t.Helper()
	for _, p := range []string{
		"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
		"/usr/share/fonts/TTF/DejaVuSansMono.ttf",
	} {
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p
		}
	}
	t.Skip("no installed fallback face for the coverage runes")
	return ""
}

func TestFallbackDrawsRunesThePrimaryFaceLacks(t *testing.T) {
	primary, err := materialiseBundledFont()
	if err != nil {
		t.Skipf("bundled font unavailable: %v", err)
	}
	fallback := fallbackHostFace(t)

	rz, err := OpenWithFallbacks([]string{primary, fallback}, 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(rz.faces) < 2 {
		t.Fatal("expected the fallback face to load")
	}
	// Cell metrics must come from the primary face alone: adding a fallback
	// must never reflow the grid.
	only, err := OpenWithFallbacks([]string{primary}, 16)
	if err != nil {
		t.Fatal(err)
	}
	w1, h1 := rz.CellSize()
	w2, h2 := only.CellSize()
	if w1 != w2 || h1 != h2 {
		t.Fatalf("cell size changed with a fallback: %d,%d vs %d,%d", w1, h1, w2, h2)
	}

	const missing = '◍' // U+25CD, absent from the bundled face, present in DejaVu
	if _, src := rz.faceFor('M'); src != 0 {
		t.Fatal("'M' should resolve on the primary face")
	}
	if _, src := rz.faceFor(missing); src != 1 {
		t.Fatalf("missing rune resolved to face %d, want the fallback", src)
	}

	g, err := cell.Parse(string(missing), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	w, h := rz.CellSize()
	stride, total, _ := BufferSize(w, h, 1)
	buf := make([]byte, total)
	if err := rz.DrawChanged(g, nil, buf, w, h, stride); err != nil {
		t.Fatal(err)
	}
	inked := false
	for i := 3; i < len(buf); i += 4 { // alpha byte
		if buf[i] != 0 {
			inked = true
			break
		}
	}
	if !inked {
		t.Fatal("fallback draw produced no ink")
	}
	found := false
	for _, r := range rz.AffectedRunes() {
		if r == "U+25CD" {
			found = true
		}
	}
	if !found {
		t.Fatalf("affected record missing the fallback rune: %v", rz.AffectedRunes())
	}

	// A rune no face carries keeps the old tofu path without panicking.
	if _, src := rz.faceFor('͸'); src != 0 { // U+0378, unassigned
		t.Fatalf("unassigned rune claimed a fallback: %d", src)
	}
}
