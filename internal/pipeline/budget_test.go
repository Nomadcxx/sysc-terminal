package pipeline

import (
	"fmt"
	"os"
	"runtime"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/Nomadcxx/sysc-terminal/internal/cell"
	"github.com/Nomadcxx/sysc-terminal/internal/effect"
	"github.com/Nomadcxx/sysc-terminal/internal/raster"
	"github.com/Nomadcxx/sysc-terminal/internal/wayland"
	"golang.org/x/sys/unix"
)

const (
	modeW      = 3440
	modeH      = 1440
	pixelSize  = 12
	budget     = wayland.TargetFrameInterval
	cpuBudget  = wayland.TargetFrameInterval / 4
	budgetRuns = 20
)

var fontPaths = []string{
	"/usr/share/fonts/TTF/JetBrainsMonoNerdFont-Regular.ttf",
	"/usr/share/fonts/TTF/JetBrainsMono-Regular.ttf",
	"/usr/share/fonts/noto/NotoSansMono-Regular.ttf",
	// Ubuntu runners and minimal installs ship only DejaVu.
	"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
}

// TestWallpaperFrameBudget is a hardware benchmark, not a correctness test: it
// holds a wall-clock and CPU budget for one frame at the mode's size. A shared
// CI runner cannot meet that budget, so it runs only when asked for it:
//
//	SYSC_FRAME_BUDGET=1 go test ./internal/pipeline/ -run FrameBudget
func TestWallpaperFrameBudget(t *testing.T) {
	if os.Getenv("SYSC_FRAME_BUDGET") == "" {
		t.Skip("frame budget is hardware dependent; set SYSC_FRAME_BUDGET=1 to run it")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
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

	id, theme := os.Getenv("SYSC_BUDGET_EFFECT"), os.Getenv("SYSC_BUDGET_THEME")
	if id == "" {
		id = "fire"
	}
	if theme == "" {
		theme = "nord"
	}
	e, err := effect.New(id, theme, cols, rows, "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	e.SetCellAspect(cw, ch)
	stride, total, err := raster.BufferSize(modeW, modeH, 2)
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	dst := make([]byte, total)
	interval := wayland.TargetFrameInterval
	var blur *raster.Blur
	var canvas []byte
	var canvasGrid *cell.Grid
	if os.Getenv("SYSC_BLUR_BUDGET") != "" {
		radius, err := strconv.Atoi(os.Getenv("SYSC_BLUR_BUDGET"))
		if err != nil {
			t.Fatal(err)
		}
		interval = wayland.FrameInterval(radius)
		blur, err = raster.NewBlur(modeW, modeH, radius)
		if err != nil {
			t.Fatal(err)
		}
		canvas = make([]byte, stride*modeH)
	}
	budget, cpuBudget := interval, interval/4
	draw := func(g *cell.Grid, old *cell.Grid, pixels []byte) error {
		if blur == nil {
			return rz.DrawChanged(g, old, pixels, modeW, modeH, stride)
		}
		if err := rz.DrawChanged(g, canvasGrid, canvas, modeW, modeH, stride); err != nil {
			return err
		}
		canvasGrid = g
		return blur.Draw(pixels, canvas, stride)
	}
	slotBytes := stride * modeH
	var previous [2]*cell.Grid
	for i := 0; i < rows; i++ {
		if err := e.TickElapsed(interval); err != nil {
			t.Fatalf("warmup tick %d: %v", i, err)
		}
		g := e.Grid()
		slot := i % len(previous)
		pixels := dst[slot*slotBytes : (slot+1)*slotBytes]
		if err := draw(g, previous[slot], pixels); err != nil {
			t.Fatalf("warmup draw %d: %v", i, err)
		}
		previous[slot] = g
	}

	// Sample a complete scene loop so intermittent visor/cloud frames cannot
	// hide behind the fire baseline's short steady-state window.
	runs := budgetRuns
	cpuCap := cpuBudget
	switch id {
	case "napoleon-bust", "higgs-field":
		runs = int((20*time.Second + interval - 1) / interval)
		cpuCap = budget
	case "f-14-tomcat", "f-15e-strike-eagle", "f-22-raptor", "fa-18e-super-hornet":
		runs = int((12*time.Second + interval - 1) / interval)
		cpuCap = budget
	case "great-wave":
		runs = int((8*time.Second + interval - 1) / interval)
	case "ak-47-rama":
		runs = int((14*time.Second + interval - 1) / interval)
	case "ak-47":
		runs = int((12*time.Second + interval - 1) / interval)
	case "wasteland":
		runs = int((16*time.Second + interval - 1) / interval)
	}
	// Particle scenes may use the frame's CPU time. Keep CPU reporting for
	// software/GPU comparisons while enforcing the frame deadline for both.
	samples := make([]time.Duration, 0, runs)
	cpuSamples := make([]time.Duration, 0, runs)
	effectCPU := make([]time.Duration, 0, runs)
	rasterCPU := make([]time.Duration, 0, runs)
	for i := 0; i < runs; i++ {
		cpuStart, err := threadCPU()
		if err != nil {
			t.Fatalf("read thread CPU: %v", err)
		}
		start := time.Now()
		if err := e.TickElapsed(interval); err != nil {
			t.Fatalf("tick: %v", err)
		}
		cpuAfterEffect, err := threadCPU()
		if err != nil {
			t.Fatalf("read effect CPU: %v", err)
		}
		g := e.Grid()
		if g == nil {
			t.Fatal("tick produced no grid")
		}
		slot := (rows + i) % len(previous)
		pixels := dst[slot*slotBytes : (slot+1)*slotBytes]
		if err := draw(g, previous[slot], pixels); err != nil {
			t.Fatalf("draw: %v", err)
		}
		previous[slot] = g
		samples = append(samples, time.Since(start))
		cpuEnd, err := threadCPU()
		if err != nil {
			t.Fatalf("read thread CPU: %v", err)
		}
		cpuSamples = append(cpuSamples, cpuEnd-cpuStart)
		effectCPU = append(effectCPU, cpuAfterEffect-cpuStart)
		rasterCPU = append(rasterCPU, cpuEnd-cpuAfterEffect)
	}

	ordered := slices.Clone(samples)
	slices.Sort(ordered)
	cpuOrdered := slices.Clone(cpuSamples)
	slices.Sort(cpuOrdered)
	effectOrdered := slices.Clone(effectCPU)
	slices.Sort(effectOrdered)
	rasterOrdered := slices.Clone(rasterCPU)
	slices.Sort(rasterOrdered)
	var sum time.Duration
	var cpuSum time.Duration
	for _, d := range samples {
		sum += d
	}
	for _, d := range cpuSamples {
		cpuSum += d
	}
	mean := sum / time.Duration(len(samples))
	cpuMean := cpuSum / time.Duration(len(cpuSamples))
	p95 := ordered[(len(ordered)*95+99)/100-1]
	cpuP95 := cpuOrdered[(len(cpuOrdered)*95+99)/100-1]
	min, max := ordered[0], ordered[len(ordered)-1]
	cpuMin, cpuMax := cpuOrdered[0], cpuOrdered[len(cpuOrdered)-1]
	line := fmt.Sprintf(
		"budget effect=%s theme=%s font=%s grid=%dx%d cell=%dx%d n=%d target_fps=%.1f wall_min=%s wall_mean=%s wall_p95=%s wall_max=%s wall_cap=%s cpu_min=%s cpu_mean=%s cpu_p95=%s cpu_max=%s cpu_cap=%s cpu_share_p95=%.1f%% effect_cpu_p95=%s raster_cpu_p95=%s",
		id, theme, opened, cols, rows, cw, ch, len(samples), float64(time.Second)/float64(interval), min, mean, p95, max, budget,
		cpuMin, cpuMean, cpuP95, cpuMax, cpuCap, float64(cpuP95)/float64(interval)*100,
		effectOrdered[(len(effectOrdered)*95+99)/100-1], rasterOrdered[(len(rasterOrdered)*95+99)/100-1],
	)
	fmt.Fprintln(os.Stderr, line)
	t.Log(line)
	if mean > budget || p95 > budget || cpuMean >= cpuCap || cpuP95 >= cpuCap {
		t.Fatalf("%s", line)
	}
}

func threadCPU() (time.Duration, error) {
	var usage unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_THREAD, &usage); err != nil {
		return 0, err
	}
	return time.Duration(usage.Utime.Sec+usage.Stime.Sec)*time.Second +
		time.Duration(usage.Utime.Usec+usage.Stime.Usec)*time.Microsecond, nil
}
