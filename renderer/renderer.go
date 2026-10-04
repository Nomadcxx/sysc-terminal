// Package renderer paints the existing terminal effects into caller-owned BGRA.
// A Renderer belongs to one goroutine. It owns no transport or scheduling.
package renderer

import (
	"fmt"
	"github.com/Nomadcxx/sysc-Go/animations"
	"github.com/Nomadcxx/sysc-terminal/internal/cell"
	"github.com/Nomadcxx/sysc-terminal/internal/effect"
	"github.com/Nomadcxx/sysc-terminal/internal/raster"
	"slices"
)

type Config struct {
	Effect, Palette, Font    string
	Width, Height, PixelSize int
}

// Frame is immutable history for a specific caller-owned pixel buffer.
// Passing nil forces a full draw; discard history when the buffer is modified.
type Frame struct {
	owner    *Renderer
	geometry uint64
	grid     *cell.Grid
}
type Renderer struct {
	effect        *effect.Effect
	raster        *raster.Rasterizer
	width, height int
	geometry      uint64
	needsStep     bool
}

func Effects() []string {
	text := animations.GetTextBasedEffects()
	out := []string{}
	for _, id := range animations.GetEffectNames() {
		if !slices.Contains(text, id) {
			out = append(out, id)
		}
	}
	return out
}
func Palettes() []string { return slices.Clone(animations.GetThemeNames()) }
func Validate(id, palette string) error {
	if !slices.Contains(Effects(), id) {
		return fmt.Errorf("unknown or artwork-dependent effect %q", id)
	}
	if animations.GetThemeMetadata(palette) == nil {
		return fmt.Errorf("unknown palette %q", palette)
	}
	return nil
}
func New(cfg Config) (*Renderer, error) {
	if err := Validate(cfg.Effect, cfg.Palette); err != nil {
		return nil, err
	}
	if cfg.PixelSize == 0 {
		cfg.PixelSize = 12
	}
	if cfg.PixelSize < 1 || cfg.PixelSize > 256 {
		return nil, fmt.Errorf("font pixel size outside 1..256")
	}
	if _, _, err := raster.BufferSize(cfg.Width, cfg.Height, 1); err != nil {
		return nil, err
	}
	font, err := raster.FindFont(cfg.Font)
	if err != nil {
		return nil, err
	}
	rz, err := raster.Open(font, cfg.PixelSize)
	if err != nil {
		return nil, err
	}
	cw, ch := rz.CellSize()
	cols, rows, err := geometry(cfg.Width, cfg.Height, cw, ch)
	if err != nil {
		return nil, err
	}
	fx, err := effect.New(cfg.Effect, cfg.Palette, cols, rows, "")
	if err != nil {
		return nil, err
	}
	return &Renderer{effect: fx, raster: rz, width: cfg.Width, height: cfg.Height, geometry: 1, needsStep: true}, nil
}
func geometry(w, h, cw, ch int) (int, int, error) {
	_, total, err := raster.BufferSize(w, h, 1)
	if err != nil {
		return 0, 0, err
	}
	// ponytail: 256 MiB pixels and 1M cells bound each renderer; consumers own aggregate budgets.
	if total > 256<<20 || cw <= 0 || ch <= 0 {
		return 0, 0, fmt.Errorf("renderer geometry exceeds budget")
	}
	cols, rows := max(effect.MinimumCols, w/cw), max(effect.MinimumRows, h/ch)
	if cols > cell.MaxGridCells/rows {
		return 0, 0, fmt.Errorf("renderer grid exceeds budget")
	}
	return cols, rows, nil
}
func (r *Renderer) Step() error {
	before := r.effect.Generation()
	if err := r.effect.Tick(); err != nil {
		return err
	}
	if r.effect.Generation() != before {
		r.needsStep = false
	}
	return nil
}
func (r *Renderer) SetPaused(paused bool) { r.effect.SetPaused(paused) }
func (r *Renderer) Resize(width, height int) error {
	if width == r.width && height == r.height {
		return nil
	}
	cw, ch := r.raster.CellSize()
	cols, rows, err := geometry(width, height, cw, ch)
	if err != nil {
		return err
	}
	if err = r.effect.Resize(cols, rows); err != nil {
		return err
	}
	r.width, r.height = width, height
	r.geometry++
	r.needsStep = true
	return nil
}
func (r *Renderer) Draw(dst []byte, stride int, previous *Frame) (*Frame, error) {
	if r.needsStep || r.effect.Grid() == nil {
		return nil, fmt.Errorf("renderer must step after construction/resize")
	}
	var old *cell.Grid
	if previous != nil && previous.owner == r && previous.geometry == r.geometry {
		old = previous.grid
	}
	grid := r.effect.Grid()
	if err := r.raster.DrawChanged(grid, old, dst, r.width, r.height, stride); err != nil {
		return nil, err
	}
	return &Frame{owner: r, geometry: r.geometry, grid: grid}, nil
}
