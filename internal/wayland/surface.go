package wayland

import (
	"errors"
	"fmt"
	"time"

	"github.com/Nomadcxx/sysc-terminal/internal/raster"
	"github.com/Nomadcxx/sysc-terminal/internal/wayland/layershell"
)

// MaxFPS is the frame-rate ceiling; cached cells and allocation-free colour
// decoding meet the measured fire CPU gate of 12.5 ms (25% of one core) at 20
// FPS. Rates above it are rejected rather than clamped silently at start-up,
// and clamped at runtime so an out-of-range socket request cannot raise cost.
const MaxFPS = 20

// MinFPS is the slowest rate the control socket accepts. 1 FPS is the floor
// for a background animation nobody is watching.
const MinFPS = 1

// TargetFrameInterval sets a 20 FPS ceiling; cached cells and allocation-free
// colour decoding meet the measured fire CPU gate of 12.5 ms (25% of one core).
const TargetFrameInterval = time.Second / MaxFPS

// ErrFPSRange is the single answer for any rejected rate, from the socket, the
// control op or --fps.
var ErrFPSRange = errors.New("fps must be 1..20")

type Spec struct {
	Namespace        string
	Layer            layershell.ZwlrLayerShellV1Layer
	ExclusiveZone    int32
	Keyboard         layershell.ZwlrLayerSurfaceV1KeyboardInteractivity
	Anchor           uint32
	Width, Height    int32
	EmptyInputRegion bool
}

// Exclusive zone -1 is the wallpaper contract: the compositor extends the
// surface to every anchored edge instead of insetting it around a panel, so
// the effect paints under the bar and frosted blur has pixels to sample.
var LayerSpec = Spec{
	Namespace:        "sysc-terminal",
	Layer:            layershell.ZwlrLayerShellV1LayerBackground,
	ExclusiveZone:    -1,
	Keyboard:         layershell.ZwlrLayerSurfaceV1KeyboardInteractivityNone,
	Anchor:           uint32(layershell.ZwlrLayerSurfaceV1AnchorTop | layershell.ZwlrLayerSurfaceV1AnchorRight | layershell.ZwlrLayerSurfaceV1AnchorBottom | layershell.ZwlrLayerSurfaceV1AnchorLeft),
	EmptyInputRegion: true,
}

func AcceptConfigure(width, height int32) error {
	_, _, err := raster.BufferSize(int(width), int(height), 2)
	return err
}

func HandleDisplayLost(cause error) error {
	if cause == nil {
		cause = fmt.Errorf("wayland: display lost")
	}
	return fmt.Errorf("wayland: unmap and exit: %w", cause)
}

func ShouldRequestFrame(paused bool) bool { return !paused }

type frameCallbackAction uint8

const (
	keepFrameCallback frameCallbackAction = iota
	requestFrameCallback
	cancelFrameCallback
)

func frameCallbackActionFor(paused, pending bool) frameCallbackAction {
	if paused && pending {
		return cancelFrameCallback
	}
	if !paused && !pending {
		return requestFrameCallback
	}
	return keepFrameCallback
}

func ShouldRetryPaint(released, pending, ready bool) bool {
	return released && pending && ready
}

// ShouldAdvance reports whether a tick is due. A zero last time is far enough in
// the past to be due, which is what resume relies on.
func ShouldAdvance(last, now time.Time, interval time.Duration) bool {
	return now.Sub(last) >= interval
}

// frameRate normalises a requested rate into MinFPS..MaxFPS, where zero (and
// anything out of range) means MaxFPS. Config and the control op both go
// through it, so a clamped rate is the only rate the owner can ever hold.
func frameRate(fps int) int {
	if fps < MinFPS || fps > MaxFPS {
		return MaxFPS
	}
	return fps
}

// TickInterval is the smallest gate that lets a tick through: the slower of
// the requested frame rate and the approved blur cadence. Blur costs 8.3 FPS
// on its own, so a caller asking for more than the blur path affords still gets
// the blur budget, and asking for less never makes the effect run faster.
func TickInterval(fps, blur int) time.Duration {
	interval := time.Second / time.Duration(frameRate(fps))
	if blur > 0 && FrameInterval(blur) > interval {
		return FrameInterval(blur)
	}
	return interval
}

// FrameInterval uses the approved 8.3 FPS fallback when post-processing blur is
// enabled. The normal renderer keeps its measured 20 FPS ceiling.
func FrameInterval(blur int) time.Duration {
	if blur > 0 {
		return 120 * time.Millisecond
	}
	return TargetFrameInterval
}
