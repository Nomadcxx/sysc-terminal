package wayland

import (
	"fmt"

	"github.com/Nomadcxx/sysc-terminal/internal/raster"
	"github.com/Nomadcxx/sysc-terminal/internal/wayland/layershell"
)

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
