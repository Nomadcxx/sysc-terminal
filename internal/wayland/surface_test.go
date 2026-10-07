package wayland

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-terminal/internal/cell"
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
	if s.ExclusiveZone != -1 {
		t.Fatalf("exclusive zone %d, want -1 so the surface stretches under panels", s.ExclusiveZone)
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

func TestPausedDoesNotRequestFrame(t *testing.T) {
	if ShouldRequestFrame(true) {
		t.Fatal("paused owner requested Surface.Frame; CPU cannot idle")
	}
}

func TestResumeRequestsOneFrame(t *testing.T) {
	if !ShouldRequestFrame(false) {
		t.Fatal("resume dropped the frame callback chain")
	}
}

func TestPauseCancelsPendingFrameCallback(t *testing.T) {
	if got := frameCallbackActionFor(true, true); got != cancelFrameCallback {
		t.Fatalf("pause with a callback pending = %v, want cancel", got)
	}
	if got := frameCallbackActionFor(false, false); got != requestFrameCallback {
		t.Fatalf("resume without a callback pending = %v, want request", got)
	}
}

func TestTargetFrameIntervalProvidesApprovedCPUMargin(t *testing.T) {
	const want = 50 * time.Millisecond
	if TargetFrameInterval != want {
		t.Fatalf("target frame interval = %s, want %s for the measured CPU budget", TargetFrameInterval, want)
	}
}

func TestAdvanceThrottledBeforeTargetFrameInterval(t *testing.T) {
	last := time.Unix(1, 0)
	if ShouldAdvance(last, last.Add(49*time.Millisecond)) {
		t.Fatal("advanced a tick before 50ms")
	}
}

func TestAdvanceAtTargetFrameInterval(t *testing.T) {
	last := time.Unix(1, 0)
	if !ShouldAdvance(last, last.Add(50*time.Millisecond)) {
		t.Fatal("did not advance at the 50ms frame interval")
	}
}

func TestBufferReleaseRetriesPendingPaint(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		released, pending, ready bool
		want                     bool
	}{
		{name: "no release", pending: true, ready: true},
		{name: "no pending frame", released: true, ready: true},
		{name: "not configured", released: true, pending: true},
		{name: "released slot and pending frame", released: true, pending: true, ready: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldRetryPaint(tc.released, tc.pending, tc.ready); got != tc.want {
				t.Fatalf("ShouldRetryPaint(%t, %t, %t) = %t, want %t", tc.released, tc.pending, tc.ready, got, tc.want)
			}
		})
	}
}

func TestResumeAdvancesFromZeroLast(t *testing.T) {
	if !ShouldAdvance(time.Time{}, time.Unix(1, 0)) {
		t.Fatal("resume with no prior tick did not advance")
	}
}

func TestFrameIntervalStartsWhenTickSubmitted(t *testing.T) {
	now := time.Unix(100, 0)
	w := &effectWorker{requests: make(chan workerRequest, 16), frames: make(chan workerResult, 1), acks: make(chan workerAck, 16), stop: make(chan struct{})}
	o := &Owner{worker: w}
	o.onFrame(now)
	if !o.tickPending || o.lastTick != now {
		t.Fatalf("tick pending=%t last=%v, want submission time %v", o.tickPending, o.lastTick, now)
	}
	w.frames <- workerResult{grid: &cell.Grid{Cols: 1, Rows: 1}}
	if err := o.drainWorker(); err != nil {
		t.Fatal(err)
	}
	if o.lastTick != now {
		t.Fatal("frame completion reset the submission clock")
	}
	o.onFrame(now.Add(TargetFrameInterval))
	if len(w.requests) != 1 {
		t.Fatal("queued another tick while one is pending")
	}
}
