package wayland

import (
	"fmt"
	"time"

	"github.com/Nomadcxx/sysc-terminal/internal/raster"
	"github.com/Nomadcxx/sysc-terminal/internal/wayland/layershell"
)

// TargetFrameInterval sets an 8.3 FPS ceiling; 30 ms of CPU per frame then
// equals 25% of one core.
const TargetFrameInterval = 120 * time.Millisecond

type Spec struct {
	Namespace        string
	Layer            layershell.ZwlrLayerShellV1Layer
	ExclusiveZone    int32
	Keyboard         layershell.ZwlrLayerSurfaceV1KeyboardInteractivity
	Anchor           uint32
	Width, Height    int32
	EmptyInputRegion bool
}

var LayerSpec = Spec{
	Namespace:        "sysc-terminal",
	Layer:            layershell.ZwlrLayerShellV1LayerBackground,
	ExclusiveZone:    0,
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

func ShouldAdvance(last, now time.Time) bool {
	return now.Sub(last) >= TargetFrameInterval
}
