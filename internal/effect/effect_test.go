package effect

import (
	"bufio"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-Go/animations"
	"github.com/Nomadcxx/sysc-terminal/internal/cell"
)

func TestArtworkElapsed(t *testing.T) {
	for _, id := range []string{"great-wave", "wasteland"} {
		fast, err := New(id, "kanagawa", 80, 24, "")
		if err != nil {
			t.Fatal(err)
		}
		slow, err := New(id, "kanagawa", 80, 24, "")
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 120; i++ {
			if err = fast.TickElapsed(50 * time.Millisecond); err != nil {
				t.Fatal(err)
			}
		}
		for i := 0; i < 50; i++ {
			if err = slow.TickElapsed(120 * time.Millisecond); err != nil {
				t.Fatal(err)
			}
		}
		if !reflect.DeepEqual(fast.Grid(), slow.Grid()) {
			t.Fatalf("%s changes speed with blur", id)
		}
		before, count := fast.Grid(), fast.RenderCount()
		fast.SetPaused(true)
		if err = fast.TickElapsed(time.Hour); err != nil {
			t.Fatal(err)
		}
		if fast.Grid() != before || fast.RenderCount() != count {
			t.Fatal("paused effect performed work")
		}
		fast.SetPaused(false)
		fast.TickElapsed(50 * time.Millisecond)
		slow.TickElapsed(50 * time.Millisecond)
		if !reflect.DeepEqual(fast.Grid(), slow.Grid()) {
			t.Fatal("paused time leaked into motion")
		}
	}
}

func TestArtworkConstructTickResizePause(t *testing.T) {
	for _, id := range []string{"great-wave", "wasteland"} {
		t.Run(id, func(t *testing.T) {
			if !strings.Contains(List(), "effect "+id+" 0\n") {
				t.Fatal("standalone catalog entry missing")
			}
			e, err := New(id, "kanagawa", 80, 24, "")
			if err != nil {
				t.Fatal(err)
			}
			if err = e.Tick(); err != nil {
				t.Fatal(err)
			}
			first := e.Grid()
			ink := 0
			for y := 0; y < 24; y++ {
				for x := 0; x < 80; x++ {
					if first.At(x, y).Ch != ' ' {
						ink++
					}
				}
			}
			if ink < 200 {
				t.Fatalf("only %d ink cells", ink)
			}
			for i := 0; i < 8; i++ {
				if err = e.Tick(); err != nil {
					t.Fatal(err)
				}
			}
			if reflect.DeepEqual(e.Grid(), first) {
				t.Fatal("animation does not move")
			}
			e.SetPaused(true)
			gen, renders := e.Generation(), e.RenderCount()
			frozen := e.Grid()
			for i := 0; i < 5; i++ {
				_ = e.Tick()
			}
			if e.Generation() != gen || e.RenderCount() != renders || e.Grid() != frozen {
				t.Fatal("pause did effect work")
			}
			e.SetPaused(false)
			e.SetCellAspect(7, 16)
			if err = e.Resize(491, 90); err != nil {
				t.Fatal(err)
			}
			if err = e.Tick(); err != nil {
				t.Fatal(err)
			}
			if e.Grid().Cols != 491 || e.Grid().Rows != 90 {
				t.Fatal("wrong resize geometry")
			}
			if err = e.Reset(); err != nil {
				t.Fatal(err)
			}
			if err = e.Tick(); err != nil {
				t.Fatal(err)
			}
			reset := e.Grid()
			if err = e.Reset(); err != nil {
				t.Fatal(err)
			}
			if err = e.Tick(); err != nil || !reflect.DeepEqual(reset, e.Grid()) {
				t.Fatal("reset changed aspect or phase")
			}
		})
	}
}

func TestArtworkBlankCellsKeepStyle(t *testing.T) {
	e, err := New("great-wave", "kanagawa", 491, 90, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Tick(); err != nil {
		t.Fatal(err)
	}
	fg := e.Grid().At(0, 0).Fg
	for i := 0; i < 30; i++ {
		if err = e.Tick(); err != nil {
			t.Fatal(err)
		}
		g := e.Grid()
		for y := 0; y < g.Rows; y++ {
			for x := 0; x < g.Cols; x++ {
				c := g.At(x, y)
				if c.Ch == ' ' && c.Fg != fg {
					t.Fatalf("tick %d blank (%d,%d) changed invisible foreground", i, x, y)
				}
			}
		}
	}
}

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

func TestRedrawDoesNotAdvance(t *testing.T) {
	e, err := New("fire", "nord", 80, 24, "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := e.Tick(); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if err := e.Resize(100, 30); err != nil {
		t.Fatalf("resize: %v", err)
	}
	renders := e.RenderCount()
	if err := e.Redraw(); err != nil {
		t.Fatalf("redraw: %v", err)
	}
	g := e.Grid()
	if g == nil || g.Cols != 100 || g.Rows != 30 {
		t.Fatalf("redraw grid = %+v, want 100x30", g)
	}
	if got := e.RenderCount(); got != renders {
		t.Fatalf("redraw changed the render count %d -> %d", renders, got)
	}
	first := e.Grid()
	if err := e.Redraw(); err != nil {
		t.Fatalf("second redraw: %v", err)
	}
	if e.Grid() == first {
		t.Fatal("second redraw reused the first grid")
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
		if !effectAvailable(id) {
			if listed[id] {
				t.Fatalf("--list printed unavailable registry id %q", id)
			}
			continue
		}
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
	for _, id := range []string{"sysc-logo", "cross-logo", "logo-morph"} {
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

func TestAK47Lifecycle(t *testing.T) {
	if !strings.Contains(List(), "effect ak-47 0\n") {
		t.Fatal("missing standalone AK-47")
	}
	a, err := New("ak-47", "kanagawa", 80, 24, "")
	if err != nil {
		t.Fatal(err)
	}
	a.SetCellAspect(7, 16)
	if err = a.TickElapsed(0); err != nil {
		t.Fatal(err)
	}
	first := a.Grid()
	if err = a.TickElapsed(9 * time.Second); err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(first, a.Grid()) {
		t.Fatal("did not morph")
	}
	a.SetPaused(true)
	frozen, count := a.Grid(), a.RenderCount()
	if err = a.TickElapsed(time.Hour); err != nil {
		t.Fatal(err)
	}
	if a.Grid() != frozen || a.RenderCount() != count {
		t.Fatal("pause performed work")
	}
	a.SetPaused(false)
	if err = a.TickElapsed(3 * time.Second); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, a.Grid()) {
		t.Fatal("cycle did not reconstruct")
	}
	if err = a.Resize(365, 90); err != nil {
		t.Fatal(err)
	}
	if err = a.Tick(); err != nil {
		t.Fatal(err)
	}
	if err = a.Reset(); err != nil {
		t.Fatal(err)
	}
	if err = a.Resize(80, 24); err != nil {
		t.Fatal(err)
	}
	if err = a.TickElapsed(0); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, a.Grid()) {
		t.Fatal("reset changed geometry or cell aspect")
	}
}

func TestAK47FixedCredit(t *testing.T) {
	a, err := New("ak-47", "kanagawa", 80, 24, "")
	if err != nil {
		t.Fatal(err)
	}
	var footer string
	for _, elapsed := range []time.Duration{0, time.Second, 7 * time.Second, time.Second, time.Second, time.Second, time.Second} {
		if err = a.TickElapsed(elapsed); err != nil {
			t.Fatal(err)
		}
		g := a.Grid()
		if !g.SmallFooter {
			t.Fatal("AK-47 credit not marked for small fixed rendering")
		}
		var line strings.Builder
		for x := 0; x < g.Cols; x++ {
			line.WriteRune(g.At(x, g.Rows-1).Ch)
		}
		if footer == "" {
			footer = line.String()
		} else if line.String() != footer {
			t.Fatal("credit text/position changed during cycle")
		}
	}
	if !strings.HasSuffix(footer, "AK-47 / Lokeig / CC BY-NC 4.0") {
		t.Fatal("upstream credit lost")
	}
	other, err := New("sysc-logo", "kanagawa", 80, 24, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = other.Tick(); err != nil {
		t.Fatal(err)
	}
	if other.Grid().SmallFooter {
		t.Fatal("unrelated effect gained footer styling")
	}
}

func TestL2A1SARFixedCredit(t *testing.T) {
	if !strings.Contains(List(), "effect l2a1-sar 0\n") {
		t.Fatal("missing standalone L2A1 SAR")
	}
	e, err := New("l2a1-sar", "kanagawa", 80, 24, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = e.TickElapsed(0); err != nil {
		t.Fatal(err)
	}
	g := e.Grid()
	if !g.SmallFooter {
		t.Fatal("L2A1 SAR credit not marked for small fixed rendering")
	}
	var footer strings.Builder
	for x := 0; x < g.Cols; x++ {
		footer.WriteRune(g.At(x, g.Rows-1).Ch)
	}
	if !strings.HasSuffix(footer.String(), "L2A1 SAR / notcplkerry / CC BY 4.0") {
		t.Fatalf("upstream credit lost: %q", footer.String())
	}
}

func TestAK47Artwork(t *testing.T) {
	for _, id := range []string{"ak-47-text"} {
		e, err := New(id, "kanagawa", 80, 24, "XX  XX\n XXXX")
		if err != nil {
			t.Fatal(err)
		}
		if err = e.TickElapsed(10 * time.Second); err != nil {
			t.Fatal(err)
		}
		if !e.Grid().SmallFooter {
			t.Fatalf("%s lost fixed credit", id)
		}
		held := e.Grid()
		e.TickElapsed(time.Second)
		if !reflect.DeepEqual(held, e.Grid()) {
			t.Fatal("artwork hold moves")
		}
		e.SetPaused(true)
		e.TickElapsed(time.Hour)
		if !reflect.DeepEqual(held, e.Grid()) {
			t.Fatal("pause changed frame")
		}
		if id == "ak-47-text" {
			if err = e.SetText("\x1b[31mX"); err == nil {
				t.Fatal("accepted controls")
			}
			if err = e.SetText("XXXXX\nX   X\nXXXXX"); err != nil {
				t.Fatal(err)
			}
			e.SetPaused(false)
			e.TickElapsed(10 * time.Second)
			if reflect.DeepEqual(held, e.Grid()) {
				t.Fatal("text replacement ignored")
			}
		}
	}
	if _, err := New("ak-47-text", "kanagawa", 80, 24, ""); err == nil {
		t.Fatal("missing file accepted")
	}
	for _, id := range []string{"justice-cross", "cyberpunk-girl"} {
		if strings.Contains(List(), "effect "+id+" ") {
			t.Fatalf("unfinished %s listed", id)
		}
		if _, err := New(id, "kanagawa", 80, 24, ""); err == nil {
			t.Fatalf("unfinished %s accepted", id)
		}
	}
}
