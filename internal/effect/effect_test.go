package effect

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-Go/animations"
	"github.com/Nomadcxx/sysc-terminal/internal/cell"
)

func TestFireConstructAndTick(t *testing.T) {
	e, err := New("fire", "nord", 80, 24, "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	e.Tick()
	g := e.Grid()
	if g.Cols != 80 || g.Rows != 24 {
		t.Fatalf("grid %dx%d", g.Cols, g.Rows)
	}
	if e.Generation() != 1 {
		t.Fatalf("generation %d, want 1", e.Generation())
	}
}

func TestUnknownEffectRejected(t *testing.T) {
	if _, err := New("not-an-effect", "nord", 80, 24, ""); err == nil {
		t.Fatal("unknown effect accepted")
	}
}

func TestUnknownThemeRejected(t *testing.T) {
	if _, err := New("fire", "not-a-theme", 80, 24, ""); err == nil {
		t.Fatal("unknown theme accepted")
	}
}

func TestTinySizeClamped(t *testing.T) {
	e, err := New("fireworks", "nord", 4, 4, "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	e.Tick()
	if w := e.EffectWidth(); w < 21 {
		t.Fatalf("constructed at width %d, floor is 21", w)
	}
}

func TestPauseDoesNotAdvance(t *testing.T) {
	e, err := New("fire", "nord", 80, 24, "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	e.Tick()
	before := e.Generation()
	e.SetPaused(true)
	for i := 0; i < 5; i++ {
		e.Tick()
	}
	if got := e.Generation(); got != before {
		t.Fatalf("generation %d -> %d while paused", before, got)
	}
}

func TestPausedTickDoesNotRender(t *testing.T) {
	e, err := New("fire", "nord", 80, 24, "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	e.Tick()
	n := e.RenderCount()
	if n == 0 {
		t.Fatal("tick did not count a render")
	}
	e.SetPaused(true)
	e.Tick()
	if got := e.RenderCount(); got != n {
		t.Fatalf("paused tick rendered %d -> %d", n, got)
	}
}

func TestTextEffectRequiresArtwork(t *testing.T) {
	if _, err := New("fire-text", "nord", 80, 24, ""); err == nil {
		t.Fatal("text effect started without required artwork")
	}
}

func TestResetRestartsEffectAndClearsGrid(t *testing.T) {
	e, err := New("fire", "nord", 80, 24, "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	e.Tick()
	if e.Grid() == nil {
		t.Fatal("tick produced no grid")
	}
	if err := e.Reset(); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if e.Grid() != nil {
		t.Fatal("reset retained a stale frame")
	}
	e.Tick()
	if e.Grid() == nil {
		t.Fatal("tick after reset produced no grid")
	}
}

type oversizedTicker struct{}

func (oversizedTicker) Update()        {}
func (oversizedTicker) Render() string { return strings.Repeat("x", cell.MaxFrameBytes+1) }

func TestTickReturnsFrameParseError(t *testing.T) {
	e, err := New("fire", "nord", 80, 24, "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	e.fx = oversizedTicker{}
	if err := e.Tick(); err == nil {
		t.Fatal("tick accepted oversized frame")
	}
	if e.Generation() != 0 {
		t.Fatalf("generation advanced after parse failure: %d", e.Generation())
	}
}

func TestListMatchesRegistry(t *testing.T) {
	listed := map[string]bool{}
	sc := bufio.NewScanner(strings.NewReader(List()))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) >= 2 && f[0] == "effect" {
			listed[f[1]] = true
		}
	}
	for _, id := range animations.GetEffectNames() {
		if !listed[id] {
			t.Fatalf("--list missing registry id %q", id)
		}
		if _, err := New(id, "nord", 80, 24, "hi"); err != nil {
			t.Fatalf("New(%q) rejected listed id: %v", id, err)
		}
	}
	for id := range listed {
		if animations.GetEffectMetadata(id) == nil {
			t.Fatalf("--list printed unknown id %q", id)
		}
	}
}

func TestNewFromFileBoundsArtworkRead(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	config := filepath.Join(home, ".config")
	if err := os.Mkdir(config, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(config, "art.txt")
	if err := os.WriteFile(path, make([]byte, 64<<10+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFromFile("fire-text", "nord", 80, 24, path); err == nil {
		t.Fatal("oversized artwork was accepted")
	}
}

func TestLogoSpinConstructAndTick(t *testing.T) {
	for _, id := range []string{"sysc-logo", "cross-logo", "justice-cross", "logo-morph"} {
		e, err := New(id, "nord", 80, 24, "")
		if err != nil {
			t.Fatalf("%s: new: %v", id, err)
		}
		e.Tick()
		e.Tick()
		if e.Generation() != 2 {
			t.Fatalf("%s: generation %d, want 2", id, e.Generation())
		}
		grid := e.Grid()
		if grid == nil || grid.Cols != 80 || grid.Rows != 24 {
			t.Fatalf("%s: grid %+v", id, grid)
		}
		// A frame that ticks cleanly but paints nothing would still pass the
		// assertions above, so require actual braille dots to land in the grid.
		dots := 0
		for y := 0; y < grid.Rows; y++ {
			for x := 0; x < grid.Cols; x++ {
				if ch := grid.At(x, y).Ch; ch >= 0x2800 && ch <= 0x28FF {
					dots++
				}
			}
		}
		if dots == 0 {
			t.Errorf("%s: frame carried no braille dots", id)
		}
	}
}
