package effect

import (
	"fmt"
	"strings"

	"github.com/Nomadcxx/sysc-Go/animations"
	"github.com/Nomadcxx/sysc-terminal/internal/cell"
)

const (
	minCols = 21
	minRows = 24
)

type ticker interface {
	Update()
	Render() string
}

type Effect struct {
	id, theme string
	w, h      int
	paused    bool
	gen       int
	grid      *cell.Grid
	fx        ticker
}

func New(id, theme string, cols, rows int, text string) (*Effect, error) {
	if animations.GetEffectMetadata(id) == nil {
		return nil, fmt.Errorf("unknown effect %q", id)
	}
	if animations.GetThemeMetadata(theme) == nil {
		return nil, fmt.Errorf("unknown theme %q", theme)
	}
	if cols < minCols {
		cols = minCols
	}
	if rows < minRows {
		rows = minRows
	}
	fx, err := construct(id, theme, cols, rows, text)
	if err != nil {
		return nil, err
	}
	return &Effect{id: id, theme: theme, w: cols, h: rows, fx: fx}, nil
}

func (e *Effect) Tick() {
	if e == nil || e.paused || e.fx == nil {
		return
	}
	e.fx.Update()
	g, err := cell.Parse(e.fx.Render(), e.w, e.h)
	if err != nil {
		return
	}
	e.grid = g
	e.gen++
}

func (e *Effect) Grid() *cell.Grid { return e.grid }
func (e *Effect) Generation() int  { return e.gen }
func (e *Effect) EffectWidth() int { return e.w }
func (e *Effect) SetPaused(p bool) { e.paused = p }

func List() string {
	var b strings.Builder
	text := map[string]bool{}
	for _, id := range animations.GetTextBasedEffects() {
		text[id] = true
	}
	for _, id := range animations.GetEffectNames() {
		flag := 0
		if text[id] {
			flag = 1
		}
		fmt.Fprintf(&b, "effect %s %d\n", id, flag)
	}
	seen := map[string]bool{}
	for _, name := range animations.GetThemeNames() {
		meta := animations.GetThemeMetadata(name)
		if meta == nil || seen[meta.Name] {
			continue
		}
		seen[meta.Name] = true
		fmt.Fprintf(&b, "theme  %s %s\n", meta.Name, strings.Join(meta.Aliases, ","))
	}
	fmt.Fprintf(&b, "version %s\n", animations.GetLibraryVersion())
	return b.String()
}

func construct(id, theme string, w, h int, text string) (ticker, error) {
	switch id {
	case "fire":
		return animations.NewFireEffect(w, h, animations.GetFirePalette(theme)), nil
	case "fire-text":
		return animations.NewFireTextEffect(w, h, animations.GetFirePalette(theme), text), nil
	case "fireworks":
		return animations.NewFireworksEffect(w, h, animations.GetFireworksPalette(theme)), nil
	case "matrix":
		return animations.NewMatrixEffect(w, h, animations.GetMatrixPalette(theme)), nil
	case "matrix-art":
		return animations.NewMatrixArtEffect(w, h, animations.GetMatrixPalette(theme), text), nil
	case "rain":
		return animations.NewRainEffect(w, h, animations.GetRainPalette(theme)), nil
	case "rain-art":
		return animations.NewRainArtEffect(w, h, animations.GetRainPalette(theme), text), nil
	case "beams":
		pal := animations.GetParticlePalette(theme)
		return animations.NewBeamsEffect(animations.BeamsConfig{Width: w, Height: h, BeamGradientStops: pal, FinalGradientStops: pal}), nil
	case "beam-text":
		pal := animations.GetParticlePalette(theme)
		return animations.NewBeamTextEffect(animations.BeamTextConfig{Width: w, Height: h, Text: text, BeamGradientStops: pal, FinalGradientStops: pal}), nil
	case "ring-text":
		pal := animations.GetParticlePalette(theme)
		return animations.NewRingTextEffect(animations.RingTextConfig{Width: w, Height: h, Text: text, RingColors: pal, FinalGradientStops: pal}), nil
	case "blackhole":
		pal := animations.GetParticlePalette(theme)
		return animations.NewBlackholeEffect(animations.BlackholeConfig{Width: w, Height: h, Text: text, StarColors: pal, FinalGradientStops: pal}), nil
	case "aquarium":
		pal := animations.GetScreensaverPalette(theme)
		c := last(pal)
		return animations.NewAquariumEffect(animations.AquariumConfig{
			Width: w, Height: h,
			FishColors: pal, WaterColors: pal, SeaweedColors: pal,
			BubbleColor: c, DiverColor: c, BoatColor: c, MermaidColor: c, AnchorColor: c,
		}), nil
	case "pour":
		pal := animations.GetParticlePalette(theme)
		return animations.NewPourEffect(animations.PourConfig{Width: w, Height: h, Text: text, FinalGradientStops: pal}), nil
	case "print":
		pal := animations.GetParticlePalette(theme)
		return animations.NewPrintEffect(animations.PrintConfig{Width: w, Height: h, Text: text, GradientStops: pal}), nil
	case "decrypt":
		pal := animations.GetParticlePalette(theme)
		return animations.NewDecryptEffect(animations.DecryptConfig{Width: w, Height: h, Text: text, Palette: pal, CiphertextColors: pal, FinalGradientStops: pal}), nil
	default:
		return nil, fmt.Errorf("unconstructed effect %q", id)
	}
}

func last(p []string) string {
	if len(p) == 0 {
		return "#ffffff"
	}
	return p[len(p)-1]
}
