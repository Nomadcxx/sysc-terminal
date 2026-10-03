package effect

import (
	"hash/fnv"
	"os"
	"path/filepath"
	"testing"
)

func gridSum(e *Effect) uint64 {
	h := fnv.New64a()
	g := e.Grid()
	if g == nil {
		return 0
	}
	for y := 0; y < g.Rows; y++ {
		for x := 0; x < g.Cols; x++ {
			c := g.At(x, y)
			h.Write([]byte{byte(c.Ch >> 8), byte(c.Ch), c.Fg.R, c.Fg.G, c.Fg.B})
		}
	}
	return h.Sum64()
}

func TestFireTextSetTextChangesGrid(t *testing.T) {
	e, err := New("fire-text", "nord", 80, 24, "SYSC")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	for i := 0; i < 8; i++ {
		e.Tick()
	}
	before := gridSum(e)
	if before == 0 {
		t.Fatal("empty grid after ticks")
	}
	if err := e.SetText("ZZZZ"); err != nil {
		t.Fatalf("settext: %v", err)
	}
	for i := 0; i < 8; i++ {
		e.Tick()
	}
	after := gridSum(e)
	if before == after {
		t.Fatal("SetText left the grid unchanged")
	}
}

func TestMissingArtworkFile(t *testing.T) {
	path := filepath.Join("/usr/share", "sysc-terminal-missing-art.txt")
	_, err := NewFromFile("fire-text", "nord", 80, 24, path)
	if err == nil {
		t.Fatal("missing artwork file accepted")
	}
}

func TestArtworkFileNotAllowed(t *testing.T) {
	_, err := NewFromFile("fire-text", "nord", 80, 24, "/tmp/art.txt")
	if err == nil {
		t.Fatal("file outside allowlist accepted")
	}
}

func TestResizeChangesGrid(t *testing.T) {
	e, err := New("fire", "nord", 80, 24, "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	e.Tick()
	if err := e.Resize(120, 32); err != nil {
		t.Fatalf("resize: %v", err)
	}
	e.Tick()
	g := e.Grid()
	if g.Cols != 120 || g.Rows != 32 {
		t.Fatalf("grid %dx%d after resize, want 120x32", g.Cols, g.Rows)
	}
	if e.EffectWidth() != 120 {
		t.Fatalf("width %d", e.EffectWidth())
	}
}

func TestResizeClampsFloor(t *testing.T) {
	e, err := New("fire", "nord", 80, 24, "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := e.Resize(4, 4); err != nil {
		t.Fatalf("resize: %v", err)
	}
	e.Tick()
	if e.EffectWidth() < 21 {
		t.Fatalf("width %d, floor 21", e.EffectWidth())
	}
}

func TestNewFromFileReadsAllowedPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "never")
	// Allowed tree: $HOME/.config. Write a temp file there if HOME is writable.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Join(home, ".config", "sysc-terminal-test")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "art.txt")
	if err := os.WriteFile(path, []byte("HELLO"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path); os.Remove(dir) })
	e, err := NewFromFile("fire-text", "nord", 80, 24, path)
	if err != nil {
		t.Fatalf("new from file: %v", err)
	}
	e.Tick()
	if e.Grid() == nil {
		t.Fatal("no grid")
	}
}
