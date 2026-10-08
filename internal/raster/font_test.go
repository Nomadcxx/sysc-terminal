package raster

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
)

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// The bundled fallback exists so a machine with no probed font still renders.
// A corrupt commit of the TTF would parse as an empty face, so assert it loads.
func TestBundledFallbackIsARealFont(t *testing.T) {
	if len(bundledFont) == 0 {
		t.Fatal("bundledFont is empty: the embedded font is missing")
	}
	loader, err := ot.NewLoader(bytes.NewReader(bundledFont))
	if err != nil {
		t.Fatalf("opentype loader on the bundled font: %v", err)
	}
	f, err := font.NewFont(loader)
	if err != nil {
		t.Fatalf("NewFont on the bundled font: %v", err)
	}
	if _, ok := f.NominalGlyph('M'); !ok {
		t.Fatal("the bundled font has no 'M' glyph")
	}
}

// With no font on the probed paths, the bundled copy must still be handed back
// as a real file: a path the caller can open.
func TestFindFontFallsBackToTheBundledCopy(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	got, err := findFontIn(nil)
	if err != nil {
		t.Fatalf("findFontIn with no candidates: %v", err)
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("stat the fallback %q: %v", got, err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("the fallback %q is not a regular file", got)
	}
}

// A font found in the probed roots wins over the bundled copy: the user's
// choice of face beats ours. The roots are checked after the default paths, so
// this only asserts the contract that matters here, not which path won.
func TestFindFontPrefersAProbedSystemFont(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "truetype")
	system := filepath.Join(root, "dejavu", "DejaVuSansMono.ttf")
	writeFile(t, system, 128)
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	got, err := findFontIn([]string{root})
	if err != nil {
		t.Fatalf("findFontIn: %v", err)
	}
	if got == filepath.Join(cache, "sysc-terminal", "fonts", bundledFontFile) {
		t.Fatalf("findFontIn fell back to the bundled copy although %q was probed", system)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("findFontIn = %q, which does not exist: %v", got, err)
	}
}

func TestFindFontHonoursAnExplicitPath(t *testing.T) {
	dir := t.TempDir()
	explicit := filepath.Join(dir, "Mine.ttf")
	writeFile(t, explicit, 64)
	if got, err := FindFont(explicit); err != nil || got != explicit {
		t.Fatalf("FindFont(%q) = %q, %v", explicit, got, err)
	}
	if _, err := FindFont(filepath.Join(dir, "absent.ttf")); err == nil {
		t.Fatal("FindFont accepted a path that does not exist")
	}
}
