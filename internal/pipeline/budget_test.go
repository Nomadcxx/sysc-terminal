package pipeline

import (
	"fmt"
	"os"
	"runtime"
	"slices"
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
}

func TestWallpaperFrameBudget(t *testing.T) {
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

	e, err := effect.New("fire", "nord", cols, rows, "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	stride, total, err := raster.BufferSize(modeW, modeH, 2)
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	dst := make([]byte, total)
	slotBytes := stride * modeH
	var previous [2]*cell.Grid
	for i := 0; i < rows; i++ {
		if err := e.Tick(); err != nil {
			t.Fatalf("warmup tick %d: %v", i, err)
		}
		g := e.Grid()
		slot := i % len(previous)
		pixels := dst[slot*slotBytes : (slot+1)*slotBytes]
		if err := rz.DrawChanged(g, previous[slot], pixels, modeW, modeH, stride); err != nil {
			t.Fatalf("warmup draw %d: %v", i, err)
		}
		previous[slot] = g
	}

	samples := make([]time.Duration, 0, budgetRuns)
	cpuSamples := make([]time.Duration, 0, budgetRuns)
	effectCPU := make([]time.Duration, 0, budgetRuns)
	rasterCPU := make([]time.Duration, 0, budgetRuns)
	for i := 0; i < budgetRuns; i++ {
		cpuStart, err := threadCPU()
		if err != nil {
			t.Fatalf("read thread CPU: %v", err)
		}
		start := time.Now()
		if err := e.Tick(); err != nil {
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
		if err := rz.DrawChanged(g, previous[slot], pixels, modeW, modeH, stride); err != nil {
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
		"budget font=%s grid=%dx%d cell=%dx%d n=%d target_fps=%.1f wall_min=%s wall_mean=%s wall_p95=%s wall_max=%s wall_cap=%s cpu_min=%s cpu_mean=%s cpu_p95=%s cpu_max=%s cpu_cap=%s cpu_share_p95=%.1f%% effect_cpu_p95=%s raster_cpu_p95=%s",
		opened, cols, rows, cw, ch, len(samples), float64(time.Second)/float64(wayland.TargetFrameInterval), min, mean, p95, max, budget,
		cpuMin, cpuMean, cpuP95, cpuMax, cpuBudget, float64(cpuP95)/float64(wayland.TargetFrameInterval)*100,
		effectOrdered[(len(effectOrdered)*95+99)/100-1], rasterOrdered[(len(rasterOrdered)*95+99)/100-1],
	)
	fmt.Fprintln(os.Stderr, line)
	t.Log(line)
	if mean > budget || p95 > budget || cpuMean >= cpuBudget || cpuP95 >= cpuBudget {
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
