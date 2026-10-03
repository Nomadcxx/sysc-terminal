package wayland

import (
	"errors"
	"math"
	"testing"

	"github.com/Nomadcxx/sysc-terminal/internal/wayland/layershell"
)

func TestLayerSpecMatchesD3(t *testing.T) {
	s := LayerSpec
	if s.Namespace != "sysc-terminal" {
		t.Fatalf("namespace %q", s.Namespace)
	}
	if s.Layer != layershell.ZwlrLayerShellV1LayerBackground {
		t.Fatalf("layer %d, want Background", s.Layer)
	}
	if s.ExclusiveZone != 0 {
		t.Fatalf("exclusive zone %d, want 0", s.ExclusiveZone)
	}
	if s.Keyboard != layershell.ZwlrLayerSurfaceV1KeyboardInteractivityNone {
		t.Fatalf("keyboard %d, want none", s.Keyboard)
	}
	want := uint32(layershell.ZwlrLayerSurfaceV1AnchorTop |
		layershell.ZwlrLayerSurfaceV1AnchorRight |
		layershell.ZwlrLayerSurfaceV1AnchorBottom |
		layershell.ZwlrLayerSurfaceV1AnchorLeft)
	if s.Anchor != want {
		t.Fatalf("anchor %d, want all four edges %d", s.Anchor, want)
	}
	if s.Width != 0 || s.Height != 0 {
		t.Fatalf("size %dx%d, want 0x0", s.Width, s.Height)
	}
	if !s.EmptyInputRegion {
		t.Fatal("input region must be empty, not nil")
	}
}

func TestConfigureRejectsOverflow(t *testing.T) {
	if err := AcceptConfigure(math.MaxInt32, math.MaxInt32); err == nil {
		t.Fatal("overflowing configure accepted")
	}
}

func TestDisplayLossReturnsError(t *testing.T) {
	err := HandleDisplayLost(errors.New("display gone"))
	if err == nil {
		t.Fatal("display loss returned nil; process would look healthy with a black output")
	}
}
