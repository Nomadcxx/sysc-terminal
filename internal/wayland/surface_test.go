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
	if ShouldAdvance(last, last.Add(49*time.Millisecond), TargetFrameInterval) {
		t.Fatal("advanced a tick before 50ms")
	}
}

func TestAdvanceAtTargetFrameInterval(t *testing.T) {
	last := time.Unix(1, 0)
	if !ShouldAdvance(last, last.Add(50*time.Millisecond), TargetFrameInterval) {
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
	if !ShouldAdvance(time.Time{}, time.Unix(1, 0), TargetFrameInterval) {
		t.Fatal("resume with no prior tick did not advance")
	}
}

func TestFrameIntervalStartsWhenTickSubmitted(t *testing.T) {
	now := time.Unix(100, 0)
	w := &effectWorker{requests: make(chan workerRequest, 16), frames: make(chan workerResult, 1), acks: make(chan workerAck, 16), stop: make(chan struct{})}
	o := &Owner{worker: w, fps: MaxFPS}
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

func TestBlurUsesApprovedFrameCadence(t *testing.T) {
	if FrameInterval(0) != 50*time.Millisecond || FrameInterval(64) != 120*time.Millisecond {
		t.Fatal("blur lost the approved CPU margin")
	}
}

func TestTickIntervalClampsToTheApprovedRange(t *testing.T) {
	for _, tc := range []struct {
		fps  int
		want time.Duration
	}{
		{0, 50 * time.Millisecond},
		{25, 50 * time.Millisecond},
		{20, 50 * time.Millisecond},
		{10, 100 * time.Millisecond},
		{1, time.Second},
	} {
		if got := TickInterval(tc.fps, 0); got != tc.want {
			t.Fatalf("TickInterval(%d, 0) = %s, want %s", tc.fps, got, tc.want)
		}
	}
	// Blur's own 8.3 FPS budget is the floor: asking for 20 with blur on still
	// ticks at 120ms.
	if got := TickInterval(20, 16); got != 120*time.Millisecond {
		t.Fatalf("TickInterval(20, 16) = %s, want the blur cadence 120ms", got)
	}
}

func TestShouldAdvanceUsesTheRequestedRate(t *testing.T) {
	last := time.Unix(1, 0)
	if ShouldAdvance(last, last.Add(60*time.Millisecond), TickInterval(10, 0)) {
		t.Fatal("advanced at 60ms under a 10 FPS cap")
	}
	if !ShouldAdvance(last, last.Add(100*time.Millisecond), TickInterval(10, 0)) {
		t.Fatal("did not advance at 100ms under a 10 FPS cap")
	}
}

func TestControlFPSChangesOnlyTheTickGate(t *testing.T) {
	w := &effectWorker{requests: make(chan workerRequest, 16), frames: make(chan workerResult, 1), acks: make(chan workerAck, 16), stop: make(chan struct{})}
	o := &Owner{worker: w, fps: MaxFPS, effectID: "fire", theme: "nord"}
	reply := o.control(ControlRequest{Op: ControlFPS, FPS: 10})
	if reply.Err != nil || reply.FPS != 10 || o.fps != 10 {
		t.Fatalf("fps 10 = %+v (owner %d), want it set to 10", reply, o.fps)
	}
	if len(w.requests) != 0 {
		t.Fatal("fps touched the worker queue")
	}
	// A new rate does not reset the effect, the theme or the playback state.
	if o.effectID != "fire" || o.theme != "nord" || o.paused {
		t.Fatalf("fps reset the effect: id=%q theme=%q paused=%t", o.effectID, o.theme, o.paused)
	}
	// FPS 0 asks for the current rate rather than changing it.
	if reply := o.control(ControlRequest{Op: ControlFPS}); reply.FPS != 10 || o.fps != 10 {
		t.Fatalf("bare ControlFPS = %+v (owner %d), want the current 10", reply, o.fps)
	}
	for _, bad := range []int{-3, 21} {
		reply := o.control(ControlRequest{Op: ControlFPS, FPS: bad})
		if !errors.Is(reply.Err, ErrFPSRange) || o.fps != 10 {
			t.Fatalf("fps %d = %+v (owner %d), want ErrFPSRange with the rate unchanged", bad, reply, o.fps)
		}
	}
	// Status carries the rate, so a shell can read it back after a restart-free
	// change without a second socket verb.
	if reply := o.control(ControlRequest{Op: ControlStatus}); reply.FPS != 10 {
		t.Fatalf("ControlStatus FPS = %d, want 10", reply.FPS)
	}
}

func TestControlFPSWhilePausedDoesNotTick(t *testing.T) {
	w := &effectWorker{requests: make(chan workerRequest, 16), frames: make(chan workerResult, 1), acks: make(chan workerAck, 16), stop: make(chan struct{})}
	o := &Owner{worker: w, fps: MaxFPS, paused: true, lastTick: time.Time{}}
	if reply := o.control(ControlRequest{Op: ControlFPS, FPS: 4}); reply.Err != nil {
		t.Fatal(reply.Err)
	}
	o.onFrame(time.Unix(500, 0))
	if o.tickPending || len(w.requests) != 0 {
		t.Fatalf("paused fps submitted %d worker requests (pending=%t)", len(w.requests), o.tickPending)
	}
}
