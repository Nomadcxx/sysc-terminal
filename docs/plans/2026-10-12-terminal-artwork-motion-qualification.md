# Animated character scenes qualification

Snapshot: 2026-10-12. Tracks sysc-terminal-8 and sysc-1136/1137.

## Candidate

The owner rejected the deployed density-frame conversion and approved block
ASCII for Cyberpunk girl and Wasteland, with authored braille motion for Great
Wave. This candidate replaces the baked loops with procedural character geometry.

Library worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-Go/artwork-motion`,
branch `feat/artwork-motion`, base `23c52e60dd7eb3994f94a38675fd85a18b49236c`.
Engine worktree: `/home/nomadx/sysc-terminal/.worktrees/artwork-motion`, branch
`feat/artwork-motion`, base `8352431ecbc8b9f03adc6b461bff8b4d55fe78da`.
The implementation remains uncommitted. The temporary `artwork-dev.mod` points
at the library worktree; release qualification must repeat on the final remote pin.

Evidence directory:
`/home/nomadx/sysc-shell/docs/plans/assets/2026-10-11-terminal-artwork/motion/`.
`source-sha256.txt` identifies the source files and candidate binary used for
these checks. Preview tooling and development modfiles stay out of release commits.

## Composition and loops

Girl and Wasteland use coloured quadrant block glyphs. Girl fills the output
height with preserved facial proportions, head sway, breathing, moving lips and
cheek light, accumulating nose blood and falling drops, and scanning visor fragments.
Wasteland separates the sun, ruins, figure/reflection and wreckage; clouds, dust,
birds, ripples and a distant vehicle move through the scene.

Wave uses braille geometry: the body and lip deform, foam fingers articulate,
spray follows falling arcs, contours travel and boats bob beneath fixed Fuji.
The braille composer selects one foreground colour per cell. Two-sample minimum
strokes retain thin water contours within that representation.

All scenes account for the measured 7×16 font cells. Landscapes extend their sky,
water and foreground across ultrawide outputs. The vehicle slows and turns at its
endpoints, with a directional dust trail that fades near the turn. Bounded polygon
strokes prevent off-screen coordinates from increasing raster work.

The preview helper uses the engine's cell parser, native raster and installed
JetBrains Mono font. Each scene has complete 20 fps MP4 loops and initial PNGs
at 491×90, 365×90 and 80×24 cells in Kanagawa and Dracula. Names use
`<effect>-<theme>-<cols>x<rows>-loop.mp4`. `preview-metadata.json` records the
18 verified durations, frame counts and pixel dimensions. The loops last 8 seconds
for Wave, 12 for Girl and 16 for Wasteland.

Visual acceptance remains open. The previews provide the review evidence;
changed-frame counts do not establish whether the artwork meets the brief.

## Focused checks

Observed failing checks before the corresponding fixes:

- Physical aspect: Wave stretched shapes at 3.437000 versus 2.322581 scale.
- Vehicle loop: its silhouette jumped 163.65 world units at the boundary.
- Stroke bound: an off-screen line exhausted the two-second test timeout.
- Foreground extension: the ultrawide right bank contained zero detail samples.

The corrected checks pass, along with deterministic Render/reset, character and
sample bounds, blood falling below the mouth, moving crest/distant regions,
complete-period closure, palette/resize behavior and elapsed-time/pause checks.
The aspect check covers both monitor grids and 80×24. Fresh local review found
no material issue in the aspect, vehicle and bounded-stroke corrections.

Commands passed with CGO disabled and GOMAXPROCS=2:

```sh
timeout 90s env CGO_ENABLED=0 GOMAXPROCS=2 go test -count=1 ./animations -run TestArtwork
timeout 90s env CGO_ENABLED=0 GOMAXPROCS=2 go test -count=1 ./tui -run 'TestArtworkSelector|TestAnimationNeedsFile'
timeout 90s env CGO_ENABLED=0 GOMAXPROCS=2 go test -modfile=artwork-dev.mod -count=1 ./internal/effect -run 'TestArtwork|TestListMatchesRegistry'
timeout 90s env CGO_ENABLED=0 GOMAXPROCS=2 go test -modfile=artwork-dev.mod -count=1 ./internal/wayland -run 'TestArtwork|TestEffectWorkerTicksAndHonorsPause|TestEffectWorkerChangeAndResize'
```

`go vet` passed for the touched animations/tui and effect/wayland/pipeline
packages. The candidate engine and preview helper built. Both worktree diffs
passed `git diff --check`.

## Complete-cycle CPU qualification

The existing TestWallpaperFrameBudget measured the complete engine pipeline at
3440×1440 / 491×90 cells, including effect generation, parsing and raster/blur.
Each gate samples a complete scene period after warmup. CPU mean and p95 must
stay below 12.5 ms at blur 0 and 30 ms at blur 64. All six gates passed.

| Scene / theme | Blur | CPU mean | CPU p95 | Cap |
|---|---:|---:|---:|---:|
| Wave / Kanagawa | 0 | 6.362 ms | 7.941 ms | 12.5 ms |
| Wave / Kanagawa | 64 | 18.556 ms | 19.830 ms | 30 ms |
| Girl / Dracula | 0 | 5.994 ms | 7.015 ms | 12.5 ms |
| Girl / Dracula | 64 | 18.695 ms | 20.875 ms | 30 ms |
| Wasteland / Kanagawa | 0 | 7.827 ms | 9.914 ms | 12.5 ms |
| Wasteland / Kanagawa | 64 | 21.059 ms | 24.682 ms | 30 ms |

Raw evidence: `budget-<effect>-<0|64>.log` in the evidence directory. Wasteland
measurements include the final extended wreckage detail.

## Release gates

No revised binary, shell code or wallpaper assignment was installed during this
headless qualification. This snapshot does not qualify live Wayland RSS or the
ten-minute growth cap. Those gates, owner review of the complete loops, final-pin
checks and CI remain required before release.

The existing bd 0.17.7 exporter can replace JSONL with a partial or empty issue
set after deduplication. Recovery restored the primary issue databases and JSONL;
avoid further export/sync until record preservation is secured. The engine's
pre-commit hook calls `bd sync --flush-only`, so commit preparation must account
for that defect without bypassing hooks. Existing tracker entries remain open.
