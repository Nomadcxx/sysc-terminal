package wayland

import (
	"fmt"
	"time"

	"github.com/Nomadcxx/sysc-terminal/internal/raster"
	"github.com/Nomadcxx/sysc-terminal/internal/wayland/layershell"
)

// TargetFrameInterval sets a 20 FPS ceiling; cached cells and allocation-free
// colour decoding meet the measured fire CPU gate of 12.5 ms (25% of one core).
const TargetFrameInterval = 50 * time.Millisecond

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
