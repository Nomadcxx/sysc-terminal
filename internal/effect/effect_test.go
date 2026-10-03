package effect

import (
	"bufio"
	"strings"
	"testing"

	"github.com/Nomadcxx/sysc-Go/animations"
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
