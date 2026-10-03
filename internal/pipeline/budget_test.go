package pipeline

import (
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-terminal/internal/effect"
	"github.com/Nomadcxx/sysc-terminal/internal/raster"
)

const (
	modeW      = 3440
	modeH      = 1440
	pixelSize  = 12
	budget     = 50 * time.Millisecond
	budgetRuns = 20
)

var fontPaths = []string{
	"/usr/share/fonts/TTF/JetBrainsMonoNerdFont-Regular.ttf",
	"/usr/share/fonts/TTF/JetBrainsMono-Regular.ttf",
	"/usr/share/fonts/noto/NotoSansMono-Regular.ttf",
}

func TestWallpaperFrameBudget(t *testing.T) {
	var rz *raster.Rasterizer
	var opened string
	for _, p := range fontPaths {
		r, err := raster.Open(p, pixelSize)
		if err == nil {
			rz = r
			opened = p
			break
		}
	}
	if rz == nil {
		t.Fatalf("no system font; tried %v", fontPaths)
	}

	cw, ch := rz.CellSize()
	if cw < 1 || ch < 1 {
		t.Fatalf("cell %dx%d", cw, ch)
	}
	cols, rows := modeW/cw, modeH/ch
	if cols < 21 || rows < 24 {
		t.Fatalf("D4 grid %dx%d below construction floor 21x24 (cell %dx%d)", cols, rows, cw, ch)
	}

	e, err := effect.New("fire", "nord", cols, rows, "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	stride, total, err := raster.BufferSize(modeW, modeH, 1)
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	dst := make([]byte, total)

	samples := make([]time.Duration, 0, budgetRuns)
	for i := 0; i < budgetRuns; i++ {
		start := time.Now()
		e.Tick()
		g := e.Grid()
		if g == nil {
			t.Fatal("tick produced no grid")
		}
		if err := rz.Draw(g, dst, modeW, modeH, stride); err != nil {
			t.Fatalf("draw: %v", err)
		}
		samples = append(samples, time.Since(start))
	}

	ordered := slices.Clone(samples)
	slices.Sort(ordered)
	var sum time.Duration
	for _, d := range samples {
		sum += d
	}
	mean := sum / time.Duration(len(samples))
	p95 := ordered[(len(ordered)-1)*95/100]
	min, max := ordered[0], ordered[len(ordered)-1]
	line := fmt.Sprintf(
		"budget font=%s grid=%dx%d cell=%dx%d n=%d min=%s mean=%s p95=%s max=%s cap=%s",
		opened, cols, rows, cw, ch, len(samples), min, mean, p95, max, budget,
	)
	fmt.Fprintln(os.Stderr, line)
	t.Log(line)
	if mean > budget || p95 > budget {
		t.Fatalf("%s", line)
	}
}
