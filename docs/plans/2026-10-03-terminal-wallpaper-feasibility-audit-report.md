# Terminal wallpaper engine — feasibility audit

Date: 2026-10-03. Tracker: `sysc-912`. Commission:
`/home/nomadx/sysc-shell/docs/plans/2026-10-03-terminal-wallpaper-engine-execution-handover.md`.

Report source-backed. Claims marked **measured**, **inspected**, or **inferred**. `sysc-909` research reports and `sysc-911` walls-integration design not on disk at audit time; commissions still open. Handovers read. Primary sources inspected instead.

No prototype renderer or live wallpaper surface mapped. Two experiments to settle remaining risks specified at end; need owner approval before run.

## Pinned sources

| Tree | Revision | Notes |
|---|---|---|
| `/home/nomadx/sysc-Go` | `65f65576` `test/combined-prs` (`v1.0.2-37-g65f6557`) | Dirty: `animations/text_updatable_test.go`, `tui/syscwalls_export_test.go`. Untracked ASCII. `LibraryVersion = "1.1.0"`. **Preserved, not edited.** |
| `/home/nomadx/Documents/sysc-Go` | `1dc1251` `master` (`v1.0.3-7`) | Dirty: `fire.go`, `matrixart.go`, `palettes.go`, `cmd/syscgo/main.go`, `go.mod`. Untracked: `animations/cellstyle.go`, `cellstyle_test.go`, `frame_bench_test.go`. Extra unpublished effects (`plasma.go`, `sonar.go`, `cracktro.go`). Worktrees under `.worktrees/` (feat-text-updatable, fix-render-buffer-reuse, pr49-fix-burn, release-1.0.3). **Preserved, not edited.** |
| sysc-Go tag `v1.0.3` | worktree `c820407` | Registry no `skull`. Version sysc-walls pins. |
| `/home/nomadx/Documents/sysc-screen` (sysc-walls) | `2676c546` (`v1.0.1-38`) | Module `github.com/Nomadcxx/sysc-walls`. `go.mod` requires `sysc-Go v1.0.3`. GPL-3. |
| `/home/nomadx/gSlapper` | `b67ed0e` (`v1.5.1-4`) | GPL-3. C + GStreamer. Live instance running. |
| `/home/nomadx/sysc-wayland` | `9d401ad` (`v0.3.1-16`) | BSD-3. Tags up to `v0.3.2-rc.1`. `client` generated from `protocols/wayland.xml`; no separate shm package. |
| `/home/nomadx/sysc-shell` | `HEAD` `d68decf9` (dirty primary); `origin/main` `4c3b7cba` | Wallpaper stack on origin/main = baseline. origin/main pins **`sysc-wayland v0.3.1`**; dirty primary still v0.2.2. |
| Niri | `26.04 (8ed0da4)` | Live. One output: `DP-1` logical 3440×1440 scale 1 transform Normal. |

Existing worktrees and dirty files above left untouched.

## What the animation library actually emits

### Contract (inspected)

`Animation` in `/home/nomadx/sysc-Go/animations/common.go:24-33`:

- `Update()` — no dt, no frame index
- `Render() string` — full frame, not cell buffer
- `Reset()` — documented on interface; **FireEffect, FireTextEffect, FireworksEffect not implement it**

Dimensions = character columns/rows (`Config.Width` / `Height`). `TextUpdatable.SetText` exists; `TestTextBasedEffectsSatisfyTextUpdatable` **measured** pass on `/home/nomadx/sysc-Go`.

sysc-walls use *different* adapter interface (`internal/animations/animations.go:5-9`): `Update(frame int)`, `Render() string`, `Resize(width, height int)`. No `Reset`. Wrappers ignore frame arg, call sysc-Go `Update()`.

Effects spawn no goroutines (grep of `animations/*.go` found none). Caller owns timing. sysc-walls `cmd/display/main.go:445-517` **inspected**: 50 ms ticker (20 FPS) in dedicated goroutine, `fmt.Print(output)` then `ESC[H` home cursor. That goroutine = display process, not effect.

Randomness = `math/rand`, no seed API. Deterministic tests need process-level seed or best-effort.

### Frame encoding (measured)

Headless probe vs `/home/nomadx/sysc-Go` at 80×24, five `Update`s, nord palette:

| Effect | Bytes | CSI | OSC | DCS | Newlines | Sample |
|---|---|---|---|---|---|---|
| fire | 12849 | 790 | 0 | 0 | 23 | `\033[38;2;R;G;Bm` + `\033[0m` + block elements `░▒▓` |
| matrix | 5695 | 360 | 0 | 0 | 23 | `\033[38;2;…m` + **short reset `\033[m`**, ASCII + Greek/Cyrillic (`Д`, `ξ`, `С`) |
| fire-text | 23319 | 1734 | 0 | 0 | 23 | same 38;2 family as fire |

No OSC, DCS, clipboard (`OSC 52`), hyperlinks, device-attribute queries in frames. **Unsupported control strings = parser requirement, not current emitter behaviour.** Skull use background SGR `\033[48;2;…m` (`skull.go` inspected). Lipgloss-based effects (matrix, rain, beams, aquarium, …) emit same SGR family; fire/fire-text/skull use raw `fmt.Fprintf` ANSI.

Bounded interpreter already exists as uncommitted test code: `Documents/sysc-Go/animations/cellstyle_test.go` `interpret()` walks `\033…m`, pairs runes with active SGR. `TestCellPainterPaintsSamePictureAsLipgloss` **measured** pass. `cellstyle.go` itself = faster ANSI *emitter* (cached SGR, coalesced runs), not cell-buffer API. Not remove need for wallpaper-side decoder unless sysc-Go grows `RenderCells` method.

**Row and cell shape (measured, fire 80×24, nord, 5 updates).** `Render()` = 11 938 bytes, splits on `\n` into exactly 24 rows — emitter always writes full height, `fire.go:103` emits literal `" "` wherever `heat < 5`. Row 0 stripped of SGR = `"       ▒  ▒                         "`: seven leading spaces, then two shade cells. Last row = `"▓███████████████████"`. Two consequences for tests:

- Check asserting "cell (0,0) not zero rune" **passes for wrong reason** — column 0 of row 0 is `' '` (0x20), not 0; same assertion passes against parser filling whole grid with spaces. Meaningful check counts non-space glyphs across grid, asserts at least one cell carries `38;2` colour.
- Check asserting "top rows blank" also wrong. Row 0 already non-blank at five updates, so fire *not* bottom-anchored in first frames. Anything cropping rows from either end changes picture; crop direction must be stated decision, not implementation detail.

### Cost of Update+Render only (measured)

Same probe, `GOMAXPROCS=2`, nord, five warm-up frames discarded, no compositor, no font raster, no SHM:

| Effect | Grid | Cells | n | mean | p95 | max | Render bytes | B/cell |
|---|---|---|---|---|---|---|---|---|
| fire | 80×24 | 1 920 | 40 | 738 µs | 1.15 ms | 1.39 ms | 22 245 | 11.59 |
| fire | 240×64 | 15 360 | 40 | 4.10 ms | 5.68 ms | 5.88 ms | 133 631 | 8.70 |
| fire | **430×90** | **38 700** | 15 | **6.51 ms** | **8.43 ms** | 8.43 ms | **248 029** | 6.41 |
| matrix | 80×24 | 1 920 | 300 | 727 µs | unmeasured | — | ≈14 KiB | — |

Cost = property of cell count, not effect: matrix at 80×24 costs same as fire at 80×24, so only fire scaled to wallpaper size. Matrix row keeps earlier run's coarser sampling; not comparable to p95 column.

sysc-walls own comment (`frame_bench_test.go`, untracked) calls 240×64 "roughly 1080p kitty". 3440×1440 at 8×16 px cells = 430×90, ~2.5× that cell count.

**This closes earlier inference.** Previous revision said "**Inferred** ANSI generation ~9 ms/frame before raster and buffer upload. Unrun at wallpaper size." Measured at real wallpaper size = **p95 8.43 ms** of 50 ms tick, emitting **248 KB per frame** — roughly 5 MB/s string churn at 20 FPS, fresh `string` per `Render()` call. Bytes-per-cell falls with size (11.59 → 6.41) because SGR runs coalesce, so marginal cost of bigger grid sub-linear in cells; 240×64 number was pessimistic extrapolation, not linear one.

**Still unmeasured = expensive half.** Update+Render 8.43 ms p95. Parse, glyph raster, `wl_shm` upload not in number:

- **Parse** bounded and cheap by construction — subset = UTF-8 runes, newline, two SGR forms, so one pass over 248 KB.
- **Glyph raster = real risk.** 430×90 = 38 700 cells per frame; at 20 FPS = **774 000 glyph rasters per second** if nothing cached. sysc-shell measured same problem: re-rasterising colour glyphs every frame "was 15 percent of a bar repaint", why `internal/render/text.go` caches per-glyph `*image.Alpha` masks in `map[rasterKey]Mask` bounded at 256 with half-when-full eviction. Engine inherits requirement whether or not design says so, because monospace font at D4's 12 px cell uses *small fixed glyph set* — few dozen codepoints — so per-glyph **alpha** cache (keyed on glyph id + ppem, tinted at blit) makes steady state 38 700 blits of small bitmaps, not 38 700 outlines through `vector.Rasterizer`. Colour in cache key thrashes unique `38;2` palettes.
- **Upload** = 3440×1440×4 = 19.3 MB shared memory per frame, double-buffered. Memcpy, but not free, not in 8.43 ms.

Attempt to measure two halves directly (uncached outline + `vector.Rasterizer` per glyph vs cached per-glyph `*image.Alpha` blit, 38 700 glyphs at ppem 15) abandoned without result. Number still open item; plan must make headless raster+parse budget check at 430×90 cheapest available proof, not leave to live soak.

### Size floor (inspected)

sysc-walls `clampEffectSize` (`optimized.go:28-42`) raises width < 21 or height < 24 because fireworks `rand.Intn(width-20)` and aquarium fish ranges panic on `n <= 0`. `TestEffectsDoNotPanicOnSmallTerminals` exists. Wallpaper adapter must apply same construction floor (never 1×1) and refuse to map output whose cell grid under 21×24.

### Registry compatibility

Authoritative consumer pin: **sysc-Go v1.0.3** (walls `go.mod` and `MinSyscGoVersion = "1.0.3"`). Tagged v1.0.3 registry names, all wrapped by walls `CreateOptimizedAnimationWithText`:

**Enumeration helpers exist — do not copy these tables (measured at tag v1.0.3, `animations/registry.go`).** `EffectRegistry` holds exactly 15 ids below; `ThemeRegistry` holds 13 themes with aliases; package exports `GetEffectNames()`, `GetTextBasedEffects()` (so "RequiresText" column queryable, not transcribed), `GetEffectMetadata(name)`, `GetLibraryVersion()`, `GetThemeNames()` (returns names *including* aliases: `catppuccin`/`catppuccin-mocha`, `tokyo-night`/`tokyonight`), `GetThemeMetadata(name)` (resolves aliases, nil for unknown). `ThemeMetadata` = `{Name, Aliases, Description, VersionAdded}` — **no colour fields**. So picker can list every id, mark which take artwork, list every theme, by asking library. One exec, works before any engine instance running, keeps AGENTS.md "do not copy registries or palettes" satisfied by construction. Library does *not* expose theme→colour mapping: `animations/palettes.go` has eight getters (`GetFirePalette`, `GetMatrixPalette`, `GetParticlePalette`, `GetRainPalette`, `GetFireworksPalette`, `GetScreenaverPalette`, `GetBurnPalette`, `GetSkullPalette`, plus `GetDefaultFirePalette`), each `func(themeName string) []string`. Engine binds effect id to one getter — small switch, not copied table. Per-effect `UpdatePalette([]string)` exists on fire, fire-text, fireworks, matrix, rain.

`GetFirePalette("nord")` returns 8 colours, first `#2e3440`; `NewFireEffect(width, height int, palette []string) *FireEffect`. `animations` `go.mod` declares `go 1.24.2`.

| ID | RequiresText | Resize on concrete type | SetText | Walls wrap | Notes |
|---|---|---|---|---|---|
| matrix | no | yes (via wrap) | no | yes | Lipgloss SGR, mixed-script rain |
| matrix-art | yes | wrap | yes | yes | Artwork |
| fire | no | `FireEffect.Resize` | no | yes | Raw 38;2, no `Reset` |
| fire-text | yes | yes | yes | yes | Datetime overlay blocked in walls |
| fireworks | no | wrap | no | yes | Needs width ≥ 21 |
| rain | no | wrap | no | yes | |
| rain-art | yes | wrap | yes | yes | |
| beams | no | wrap | no | yes | Half-block glyphs `▀▄▌▐` |
| beam-text | yes | wrap | yes | yes | |
| ring-text | yes | wrap | yes | yes | |
| blackhole | yes | wrap | yes | yes | |
| aquarium | no | wrap | no | yes | Needs height ≥ 24 |
| pour | yes | yes | yes | yes | |
| print | yes | yes | yes | yes | |
| decrypt | yes | wrap | yes | yes | |

On local HEADs, **not** on tag v1.0.3, **not** wrapped by walls:

| ID | Where | Gate |
|---|---|---|
| skull, skull-text | `/home/nomadx/sysc-Go` `LibraryVersion 1.1.0`; Documents master lists skull as VersionAdded 1.0.3 but tag worktree disagrees | Need published sysc-Go tag |
| sonar, cracktro, plasma | Documents/sysc-Go only | Unpublished; out of v1 |
| burn | source + TextUpdatable test, not in v1.0.3 registry | Out of v1 until registered and tagged |
| ticker | TUI spinner, not wallpaper effect | Ignore |

Artwork: walls load UTF-8 text from allowlisted absolute path (`isSafePath`: `$HOME/.config`, `$HOME/.local/share`, `/usr/share`, `/usr/local/share`) or bundled `SYSC.txt`. Empty text becomes `"SYSC-WALLS"`.

Unicode/font: matrix rain includes non-Latin letters. East-Asian wide runes not systematically tested. Fallback must be documented (missing glyph → replacement, width 1) not silently shift columns. Combining characters: unrun.

## Wallpaper layer evidence

### gSlapper on this machine (measured)

Live `niri msg -j layers` on `DP-1`:

| namespace | layer | keyboard_interactivity |
|---|---|---|
| `slapper` | Background | None |
| `sysc-shell:bar` | Top | None |
| `sysc-shell-toast` | Overlay | None |
| `sysc-shell-osd` | Overlay | None |

gSlapper process: `gslapper -I /run/user/1000/sysc-shell/gslapper-DP-1.sock --no-save-state -o fill no-audio loop -r 30 DP-1 <still png>`. RSS **100212 KiB**, CPU **0.1%**, elapsed ~23 h. Still image, not video, not effect. Video/effect CPU unrun.

Creation path **inspected** (`gSlapper/src/main.c:3294-3313`): empty `wl_region` as input region, `zwlr_layer_shell_v1_get_layer_surface(..., BACKGROUND, "slapper")`, anchors all four, size 0×0, **exclusive zone -1**. No `set_keyboard_interactivity` call found; Niri reports None. GPL-3 C/GStreamer. Evidence only.

### Depth clock policy (inspected, do not collide)

`internal/shell/depthclock.go`: namespace `sysc-wallpaper-depth-clock`, **Bottom** (not Background), exclusive -1, keyboard none, empty input rects, 560×176 card. Sits above Background wallpaper. Terminal engine on Background compatible if it not use Bottom and not take keyboard.

### sysc-wayland (inspected, verified against v0.2.2 and v0.3.1)

Packages in module: `client`, `cursorshape`, `textinput`, `cmd/sysc-wayland-scanner`, `protocols/`. **`wl_shm` not separate package** — `Shm`, `ShmPool`, `Buffer`, `Compositor`, `Region`, `Output`, `Surface`, `Callback` all generated into `client` from `protocols/wayland.xml`. Earlier phrasing ("core protocol, `wl_shm`") described one package twice. **No** generated layer-shell package ships in module; shell generates one:

```
internal/platform/wayland/layershell/generate.go
//go:generate … sysc-wayland-scanner@v0.1.1 … wlr-layer-shell-unstable-v1.xml
```

Comment pins protocol commit `2b8d43325b7012cc3f9b55c08d26e50e42beac7d` (version 5, what Niri 26.04 advertises), SHA-256 `87e0b9c837aecd6977f76f3c47d73088b7159871f5d979dc1840f6cadb5e2ed8`. New engine must generate same way inside *this* repository, not import `sysc-shell/internal/...`, so scanner `-xdg-shell-import` must point at this repo's own `internal/wayland/xdgshell`.

**Method-set check (measured).** Whole surface engine needs identical at v0.2.2 and v0.3.1: `Display.GetRegistry/Sync/Roundtrip/SetErrorHandler`, `Registry.Bind`, `Compositor.CreateSurface/CreateRegion`, `Shm.CreatePool(fd, size)`, `ShmPool.CreateBuffer(offset, w, h, stride, format)`, `Surface.Frame/Attach/Commit/DamageBuffer/SetInputRegion/SetOpaqueRegion/SetBufferScale/SetBufferTransform/Offset/SetPreferredBufferScaleHandler`, `Buffer.SetReleaseHandler`, `Region.Add/Subtract/Destroy`, `Output.SetMode/Scale/Name/Geometry/DoneHandler`, `Callback.SetDoneHandler`. Pin choice not capability question.

**Pin: v0.3.1, not v0.2.2 (corrected).** Row previously read "Shell still pins v0.2.2". Read off dirty primary checkout. sysc-shell `origin/main` `go.mod` requires `v0.3.1`; origin/main = stated baseline for design and shell worktree. Only behavioural difference between tags = one hunk in `client/context.go` `RegisterWithID`: v0.2.2 panics whenever ID already present, v0.3.1 panics only when existing object not zombie, because server reuses IDs after processing destroy. Exactly the "proxy ID reuse" case previously called "optional" — not optional against live Niri. `sessionlock` and `idle` also only at v0.3.x; neither needed for v1.

`go.mod` at v0.2.2 and v0.3.1 declares `go 1.26`. sysc-shell declares `go 1.26.4`. Module here declaring less than 1.26 will not resolve these requires.

Fractional-scale and viewporter also generated in-shell, not shipped by sysc-wayland. Needed for laptop scale 1.25.

Dispatch: one goroutine owns connection (`sysc-wayland/AGENTS.md`).

### Hosting a terminal on the wallpaper layer (inspected, rejected)

sysc-walls launches `kitty --start-as=fullscreen --class sysc-walls-screensaver <display> --effect …` (`config.go:696-746`). Compositor helpers add *window rules*, not layer-shell (`internal/compositor/niri.go` = `niri msg` focus/outputs only). Display binary writes ANSI to stdout. That = xdg-toplevel. Ordinary Niri window rules cannot turn it into Background surface. Fullscreen Kitty takes window, input, focus. **Fails product requirement.**

### Extending gSlapper (inspected, rejected)

No terminal cell path. GPL-3 C + GStreamer. Shell already owns gSlapper as image/video engine (`wallpaper/engine.go`, `gslapper.go`). Effects there would mix languages, licences, process identities, still no live terminal cells without second renderer.

### Extending sysc-walls (inspected, rejected as the wallpaper engine)

Screensaver: idle daemon + systemd user unit + Kitty. GPL-3. CGO Wayland idle (`pkg/idle`). Palette tables duplicated in `optimized.go:getThemePalette` instead of calling sysc-Go. Pin lag vs local HEAD. `sysc-911` already commissions Control Centre/Settings access to this screensaver, states it **not** wallpaper-layer backend. Adding background mode here forks process ownership, puts second effect selector next to 911.

## Shell wallpaper ownership (must not duplicate)

From `2026-09-03-wallpaper-design.md` and current `internal/wallpaper`:

- **D1** panel + `internal/wallpaper` owns library, assignments, engines. Relays off Wayland owner.
- **D12–D18** gSlapper-first, shell-owned sockets, one process per connector, never `*`, never `pkill` by name, never kill foreign instances.
- **D19** assignments JSON: `connector -> {kind: image|video, path, preview_path, desired_playback}`. Kind only `KindImage` / `KindVideo` (`media.go:17-22`). `checkPath` rejects empty/newline/non-UTF-8 paths (`persist.go:59-69`). Effect encoded as fake filename fails kind detection and theme-seed write-back.
- `EngineFor(kind)` returns gSlapper for any kind if installed (`service.go:69-77`). Third kind needs explicit engine name, not reused image path.
- `Apply` `os.Stat`s media path before launch (`engine.go:317-322`). Fake filename fails there even if persist loosened.
- Background coverage treats only `slapper` / `awww-daemon` / `swaybg` / `sysc-shell*` as ours (`popout_wallpaper.go` `wallpaperOurNamespace`). New namespace foreign until listed.
- Pause/Resume exist (`OpPause`/`OpResume`), **video-only** today (`service.go` comments; images no pipeline).
- Engine pills in picker = readout of installed backends (`popout_wallpaper.go:716-732`), not second settings page.
- Restore (`D16`) stops gSlapper, applies still via awww/swaybg. Exception for media, not effects.

Idle: no `IdleService` in current shell source (grep empty). Session suspend = `loginctl suspend` (`popout_session.go`). Screensaver idle = sysc-walls daemon (`sysc-911`). Lock = `sysc-910` (not implemented). Engine must pause on signals shell already observes: explicit pause, output disconnect, session actions once wired — not new occlusion detector.

## Approach comparison

| Approach | Behaviour fit | Architectural fit | Licence / language | Verdict |
|---|---|---|---|---|
| Kitty (or any terminal) hosted as wallpaper | No. xdg-toplevel; steals focus; walls already does this as screensaver | Fights wallpaper.Service and 911 | Kitty = user binary we exec, not library | Reject |
| Native Go cell renderer on real Background surface in **dedicated engine binary** | Yes, if ANSI subset + font raster work | Matches “second consumer of sysc-Go” and “shell does not own Background wallpaper surface” (wallpaper design out-of-scope). User assigned `sysc-terminal` | MIT engine + MIT sysc-Go + BSD sysc-wayland | **Recommend** |
| Native renderer inside sysc-shell | Works technically | Violates wallpaper D1 out-of-scope, Wayland-owner budget, “one repo until second consumer” — second consumer = this engine | Same | Reject |
| Background mode in sysc-walls | Still needs same renderer | Mixes idle/systemd/Kitty with wallpaper; GPL-3 + CGO; duplicate selector vs 911 | GPL-3 infects engine if imported | Reject |
| gSlapper extension | No cell path | Shell already owns for image/video | GPL-3 C | Reject |
| Upstream `RenderCells` on every sysc-Go effect | Cleaner long-term | Cross-repo release gate before wallpaper ships | MIT | Optional later; not required v1 |

**Recommendation:** small Go process in this repository, one instance per output, layer-shell Background, empty input region, exclusive zone **-1**, keyboard none, namespace `sysc-terminal`. Consume sysc-Go `Render()` through bounded SGR interpreter (reuse `interpret()` approach proven in cellstyle tests). Rasterise with monospace font via `go-text/typesetting` (already used by shell; do not import shell). sysc-shell grows `KindEffect`, supervises binary like gSlapper.

**Superseded (2026-10-07):** this report commissioned exclusive zone **0** ("specified contract", against gSlapper's live-proven **-1**). Zone 0 makes the compositor inset the Background surface around the shell bar's positive exclusive zone, so the effect stops at the bar edge and frosted blur samples an empty strip. The wallpaper contract is **-1**: stretch under panels. See issue #6.

Small sysc-Go `RenderCells` API *not* required to start. Assess if ANSI subset becomes tax; do not rewrite 17 `Render` methods first.

## Licensing and dependencies

| Module | Licence | Use |
|---|---|---|
| sysc-Go | MIT | Import `animations` at **v1.0.3** for v1 registry. Newer effects wait on tag |
| sysc-wayland | BSD-3 | Pin **v0.3.1** — version sysc-shell `origin/main` requires. Generate layer-shell / fractional-scale / viewporter locally |
| sysc-walls | GPL-3 | Do not import. Reuse *ideas* (size floor, artwork allowlist, 20 FPS) |
| gSlapper | GPL-3 | Do not import. Reuse *behaviour* (layer, IPC verbs, one process per connector) |
| charmbracelet lipgloss / x/ansi | MIT | Already pulled by sysc-Go. See parser decision in design: bounded subset ~40 lines; `x/ansi` = second parser to keep in step |
| go-text/typesetting, golang.org/x/image | existing in shell | Font raster; add as direct requires here. Versions in shell: `go-text/typesetting v0.3.5-0.20260729084153-ddb7ff96ad4d`, `golang.org/x/image v0.44.0`, `rivo/uniseg v0.4.7` |

No handwritten C/CGO. No new test framework.

### Reuse that already exists in shell (inspected on `origin/main`)

Two primitives design would otherwise write from scratch; both readable and copyable in shape without importing shell:

- `internal/platform/wayland/shm.go` — double-buffered shm generation. `slotCount = 2`; `stride = int64(width) * 4`; `total = stride * height * slotCount`; rejects `width <= 0 || height <= 0` and `stride > math.MaxInt32 || total > math.MaxInt32` (exactly int32-overflow guard design asks for); `unix.MemfdCreate(..., unix.MFD_CLOEXEC)` + `Ftruncate` + `Mmap(fd, 0, total, PROT_READ|PROT_WRITE, MAP_SHARED)` + `shm.CreatePool(fd, int32(total))`; one `pool.CreateBuffer` per slot. Comment notes pool can be destroyed while buffers still live, so whole generation retires as unit.
- `internal/render/text.go` — glyph raster and cache. `ParseFace(data []byte)` uses `ot.NewLoader` + `font.NewFont` with `sync.Map` keyed by `sha256.Sum256(data)`, because `*font.Font` read-only and concurrent-safe but `*font.Face` not — cache font, not face. `Mask{Alpha *image.Alpha; Color …; Baseline int; Advance w}`, `TextRenderer.raster map[rasterKey]Mask`, `rasterCacheMax = 256`, half-when-full eviction, and measured note that per-frame glyph rasterisation "was 15 percent of a bar repaint".

Useful API facts for that typesetting version, since obvious spellings wrong: `font.Face` has `NominalGlyph(rune) (GID, bool)` (not `GlyphIndex`), `GlyphDataOutline(GID) (GlyphOutline, bool)`, `HorizontalAdvance(GID) float32`, `SetPpem(x, y uint16)`; **no `LineMetric` value named `LHMetric`** (use `CapHeight`/`XHeight`/`Ascent`-based math from `FontHExtents`, which has no `Upem` field — `Upem()` is method on `*font.Font`); `shaping.Glyph` carries `GlyphID`, not `Glyph`; `shaping.Bounds` carries `Ascent`/`Descent`/`Gap`, not `Min`/`Max`; `language` has `NewLanguage`/`ParseScript`/`LookupScript`, no `MustParseScript`; `vector.Rasterizer.Draw` wants `*image.Uniform`, not value.

sysc-wayland scanner (`cmd/sysc-wayland-scanner`, present in every tag from v0.1.1) imports `golang.org/x/crypto/blake2b`, `golang.org/x/tools/imports`, `mvdan.cc/gofumpt/format`, `github.com/iancoleman/strcase`, and has HTTP fetch path. Shell's own generate lines pin it `@v0.1.1` while requiring module at v0.2.2/v0.3.1; generating at same tag as require tidier, costs nothing. Provenance not decoration — each generated file and each copied `protocols/*.xml` carries upstream repo, revision, SHA-256 (layer-shell `2b8d4332…` / `87e0b9c8…`; fractional-scale and viewporter from wayland-protocols 1.49 / `5941de5d…` and `dcb12279…`).

Fonts: all three paths design lists exist on this machine — `JetBrainsMonoNerdFont-Regular.ttf` (2 571 596 B), `JetBrainsMono-Regular.ttf` (273 900 B), `noto/NotoSansMono-Regular.ttf` (596 428 B). Raster check may load system font directly; `t.Skip` guard = unnecessary risk of check that never runs.

## Resource budget — proposed, not approved

Commission: choose numeric pass/fail **with owner** before implementation. These = proposals.

Same effect (`fire`), same cell metrics, target **20 FPS** (walls ticker), compare:

| Machine | Geometry | Proposed pass |
|---|---|---|
| Desktop (this machine) | 3440×1440 / scale 1 | CPU p95 < 25% of one core while mapped; RSS < 150 MiB; no RSS growth over 10 min soak; paused CPU ≈ idle and **zero** `Update`/`Render` |
| Laptop | 1536×864 logical / scale 1.25 | Same numeric caps |

Dropped frames: skip stale work if tick still rendering when next tick due; queue no more than one pending frame. Recovery: last good buffer stays mapped; on engine crash shell restores previous assignment (image/video/effect), never foreign gSlapper.

gSlapper still baseline **measured**: 100 MiB RSS, 0.1% CPU. sysc-walls display Kitty baseline **unrun** (would steal live session). Laptop **unrun**. Two-output **unrun** (compositor has one output; AGENTS.md already records).

### What is known and what is still a guess inside that budget

Proposal above = whole-process CPU share, mixes three costs. Measured and unmeasured, at 430×90 / 20 FPS:

| Component | Status | Number | Share of the 50 ms tick |
|---|---|---|---|
| `Update` + `Render` | **measured** | p95 8.43 ms, 248 029 B/frame | 17% |
| Bounded parse of those bytes | by construction, unmeasured | one pass over 248 KB | small |
| Glyph raster, no cache | **unmeasured — top risk** | 38 700 glyphs/frame = 774 000 rasters/s | unknown, potentially over budget |
| Glyph raster, per-glyph alpha cache | **unmeasured**, but shell caches for exactly this reason | few dozen distinct glyphs at 8×16 | cheap blits |
| `wl_shm` upload + attach | unmeasured | 19.3 MB/frame, double-buffered | unknown |

Two consequences owner should decide, not inherit:

1. **Glyph cache = requirement, not optimisation.** 774 000 outline rasterisations per second = only line in table that can plausibly blow budget alone. Belongs in design as decision with check, not implementation note.
2. **Budget check at 80×24 proves nothing.** 80×24 = 1 920 cells vs 38 700, cost 20× smaller; "mean under 20 ms at 80×24" gate passes no matter how broken real path, exercises no raster. Cheapest honest proof = headless check at 430×90 including parse and raster, no compositor.

## Remaining risks and smallest proofs

| Risk | Status | Smallest proof |
|---|---|---|
| Bounded SGR decode of real `Render()` into stable grid | Interpreter exists in tests; not wired to grid | **Experiment A** (headless, no compositor) |
| **Parse + glyph raster at 430×90 fits tick** | **Not measured. Update+Render p95 8.43 ms; raster 38 700 glyphs/frame, unmeasured** | **Headless 430×90 budget check including parse and raster, no compositor. Not live soak** |
| **Per-glyph alpha cache exists and is hit** | No cache mentioned anywhere in design | Check that second identical frame costs no more than first after warm-up |
| Niri maps *new* Background namespace without stealing input, gSlapper restorable | `slapper` live-proven; `sysc-terminal` not | **Experiment B** (live, needs approval) |
| Engine exits when compositor goes away | Not specified; hung owner leaves no wallpaper, no restart trigger | Check on display error path; live `niri msg quit`-style observation not needed — unit check on error callback returning exit enough |
| Font cell metrics + fractional scale 1.25 | Inferred from shell text renderer | After A+B, one laptop check |
| Artwork path / wide glyphs / malformed CSI | Unrun | Table tests in Stage 3 Task 1 |
| Effect/theme list available to picker without copying registries | **Resolved: sysc-Go v1.0.3 exports enumeration helpers** | `sysc-terminal --list`, one exec, checked by shell-side picker test |
| Crop direction when grid smaller than effect floor | Unspecified; `Render()` row 0 non-blank at 5 updates so either end changes picture | Design decision, then table check on crop helper |

### Experiment A — headless cell adapter (no compositor)

Need: confirm `Render()` plus bounded parser yields dense `cols×rows` grid for fire (no text) and matrix (Unicode), and OSC/DCS/APC ignored.

Commands (from worktree of this repo, after Stage 3 Task 1 exists):

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/cell -run 'TestFireGridDimensions|TestMatrixHasNoOSC|TestMalformedCSIIsIgnored'
```

Expected: PASS. Fire 80×24 → 80 columns, 24 rows. Matrix frames contain no OSC. Truncated CSI not panic or shift later cells.

Cleanup: none (pure test).

Until Task 1 exists, experiment specified, not run. Running `TestCellPainter*` (**measured** pass) = closest existing check; maps no surface.

### Experiment B — one Niri Background surface (needs owner approval)

Need: prove namespace not `slapper` can sit on Background with empty input region, exclusive zone 0, keyboard none, without eating clicks or permanently replacing owner's wallpaper.

Preconditions: owner agrees; current gSlapper assignment recorded; duration ≤ 60 s.

Commands:

```bash
export NIRI_SOCKET=$(ls /run/user/1000/niri.wayland-*.sock | head -1)
export WAYLAND_DISPLAY=wayland-1
export XDG_RUNTIME_DIR=/run/user/1000
# record: niri msg -j layers ; copy assignments.json
# run the approved throwaway mapper (solid colour OR one static glyph raster — not a full engine)
niri msg -j layers   # expect namespace sysc-terminal, layer Background, keyboard None
# click the desktop; type a niri keybind
# stop mapper by pid recorded at launch — never pkill -f sysc-terminal
niri msg -j layers   # slapper (or previous backend) restored
```

Expected observations: `sysc-terminal` appears on Background; bar and clicks still work; after stop, previous wallpaper back; `gslapper-DP-1.sock` still owned by original pid or cleanly relaunched by shell — not killed by name.

Cleanup: destroy test surface; if still wallpaper gone, apply through wallpaper panel (not by copying binaries).

**Do not run Experiment B until owner approves this recipe.**

## Ownership proposal (for the design, not yet chosen)

Subject to owner approval of audit:

- **This repository** owns engine binary, cell adapter, font raster, layer-shell mapping, own Unix-socket control protocol.
- **sysc-shell** owns assignment persistence (`KindEffect`), process supervision (owned sockets, generation, restore), panel/Settings chrome, pause triggers it can observe.
- **sysc-Go** remains only effect implementation. v1 pins v1.0.3. Newer effects = release gate on sysc-Go.
- **sysc-walls / sysc-911** keep screensaver controls. No second effect picker writing different store.
- **sysc-910** lock integration = later pause source, not v1 blocker.

## What this audit did not do

- Not map live `sysc-terminal` surface.
- Not run sysc-walls display (would fullscreen Kitty over session).
- Not measure parse, glyph raster, shm upload. Raster benchmark attempt abandoned without result; number listed as top open risk, not guessed.
- Not measure laptop, two-output, video gSlapper, 10-minute soak.
- Not read missing `sysc-909` reports.
- Not edit preserved dirty trees.

Two claims in earlier revisions wrong, corrected above rather than left in place: sysc-wayland pin (read off dirty checkout instead of `origin/main`) and `wl_shm` framing (described as separate package when generated into `client`). Shell-source line numbers in ownership section from `origin/main`, because primary checkout's Wayland sources actually live in `pr-85-flush-text` worktree.

Next concrete action: owner reviews audit, design, proposed budgets. Stage 3 not start without approval. Experiment B not start without separate yes.