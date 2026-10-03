# Terminal wallpaper engine — feasibility audit

Date: 2026-10-03. Tracker: `sysc-912`. Commission:
`/home/nomadx/sysc-shell/docs/plans/2026-10-03-terminal-wallpaper-engine-execution-handover.md`.

This report is source-backed. Claims are marked **measured**, **inspected**,
or **inferred**. `sysc-909` research reports and the `sysc-911` walls-integration
design were not on disk at audit time; those commissions are still open. Their
handovers were read. Primary sources were inspected instead.

No prototype renderer or live wallpaper surface was mapped. Two concrete
experiments that would settle remaining risks are specified at the end and
need owner approval before they run.

## Pinned sources

| Tree | Revision | Notes |
|---|---|---|
| `/home/nomadx/sysc-Go` | `65f65576` `test/combined-prs` (`v1.0.2-37-g65f6557`) | Dirty: `animations/text_updatable_test.go`, `tui/syscwalls_export_test.go`. Untracked ASCII. `LibraryVersion = "1.1.0"`. **Preserved, not edited.** |
| `/home/nomadx/Documents/sysc-Go` | `1dc1251` `master` (`v1.0.3-7`) | Dirty: `fire.go`, `matrixart.go`, `palettes.go`, `cmd/syscgo/main.go`, `go.mod`. Untracked: `animations/cellstyle.go`, `cellstyle_test.go`, `frame_bench_test.go`. Extra unpublished effects (`plasma.go`, `sonar.go`, `cracktro.go`). Worktrees under `.worktrees/` (feat-text-updatable, fix-render-buffer-reuse, pr49-fix-burn, release-1.0.3). **Preserved, not edited.** |
| sysc-Go tag `v1.0.3` | worktree `c820407` | Registry has no `skull`. This is the version sysc-walls pins. |
| `/home/nomadx/Documents/sysc-screen` (sysc-walls) | `2676c546` (`v1.0.1-38`) | Module `github.com/Nomadcxx/sysc-walls`. `go.mod` requires `sysc-Go v1.0.3`. GPL-3. |
| `/home/nomadx/gSlapper` | `b67ed0e` (`v1.5.1-4`) | GPL-3. C + GStreamer. Live instance running. |
| `/home/nomadx/sysc-wayland` | `9d401ad` (`v0.3.1-16`) | BSD-3. Shell still pins **v0.2.2**. |
| `/home/nomadx/sysc-shell` | `HEAD` `d68decf9` (dirty primary); `origin/main` `4c3b7cba` | Wallpaper stack on origin/main is the baseline. Pin `sysc-wayland v0.2.2`. |
| Niri | `26.04 (8ed0da4)` | Live. One output: `DP-1` logical 3440×1440 scale 1 transform Normal. |

Existing worktrees and dirty files listed above were left untouched.

## What the animation library actually emits

### Contract (inspected)

`Animation` in `/home/nomadx/sysc-Go/animations/common.go:24-33`:

- `Update()` — no dt, no frame index
- `Render() string` — a full frame, not a cell buffer
- `Reset()` — documented on the interface; **FireEffect and FireworksEffect do not implement it** (no `Reset` method in `fire.go` / `fireworks.go` in either checkout)

Dimensions are character columns/rows (`Config.Width` / `Height`). `TextUpdatable.SetText` exists; `TestTextBasedEffectsSatisfyTextUpdatable` **measured** pass on `/home/nomadx/sysc-Go`.

sysc-walls uses a *different* adapter interface (`internal/animations/animations.go:5-9`): `Update(frame int)`, `Render() string`, `Resize(width, height int)`. No `Reset`. Wrappers ignore the frame argument and call sysc-Go `Update()`.

Effects do not spawn goroutines (grep of `animations/*.go` found none). Timing is owned by the caller. sysc-walls `cmd/display/main.go:445-517` **inspected**: a 50 ms ticker (20 FPS) in a dedicated goroutine, `fmt.Print(output)` then `ESC[H` to home the cursor. That goroutine is the display process, not the effect.

Randomness is `math/rand` without a seed API. Deterministic tests need a process-level seed or they are best-effort.

### Frame encoding (measured)

Headless probe against `/home/nomadx/sysc-Go` at 80×24, five `Update`s, nord palette:

| Effect | Bytes | CSI | OSC | DCS | Newlines | Sample |
|---|---|---|---|---|---|---|
| fire | 12849 | 790 | 0 | 0 | 23 | `\033[38;2;R;G;Bm` + `\033[0m` + block elements `░▒▓` |
| matrix | 5695 | 360 | 0 | 0 | 23 | `\033[38;2;…m` + **short reset `\033[m`**, ASCII + Greek/Cyrillic (`Д`, `ξ`, `С`) |
| fire-text | 23319 | 1734 | 0 | 0 | 23 | same 38;2 family as fire |

No OSC, DCS, clipboard (`OSC 52`), hyperlinks, or device-attribute queries in those frames. **Unsupported control strings are a parser requirement, not a current emitter behaviour.** Skull uses background SGR `\033[48;2;…m` (`skull.go` inspected). Lipgloss-based effects (matrix, rain, beams, aquarium, …) emit the same SGR family; fire/fire-text/skull use raw `fmt.Fprintf` ANSI.

A bounded interpreter already exists as uncommitted test code: `Documents/sysc-Go/animations/cellstyle_test.go` `interpret()` walks `\033…m` and pairs runes with active SGR. `TestCellPainterPaintsSamePictureAsLipgloss` **measured** pass. `cellstyle.go` itself is a faster ANSI *emitter* (cached SGR, coalesced runs), not a cell-buffer API. It does not remove the need for a wallpaper-side decoder unless sysc-Go grows a `RenderCells` method.

### Cost of Update+Render only (measured)

Same probe, `GOMAXPROCS=2`, no compositor, no font raster, no SHM:

| Effect | Grid | n | per frame | avg Render bytes |
|---|---|---|---|---|
| fire | 80×24 | 300 | 610 µs | 22 KiB |
| fire | 240×64 | 80 | 3.69 ms | 117 KiB |
| matrix | 80×24 | 300 | 727 µs | 14 KiB |

sysc-walls' own comment (`frame_bench_test.go`, untracked) calls 240×64 “roughly 1080p kitty”. A 3440×1440 wallpaper at 8×16 px cells is about 430×90, ~2.5× that cell count. **Inferred** ANSI generation ~9 ms/frame before raster and buffer upload. Unrun at wallpaper size. Raster + `wl_shm` + attach is unrun.

### Size floor (inspected)

sysc-walls `clampEffectSize` (`optimized.go:28-42`) raises width < 21 or height < 24 because fireworks `rand.Intn(width-20)` and aquarium fish ranges panic on `n <= 0`. `TestEffectsDoNotPanicOnSmallTerminals` exists. A wallpaper adapter must apply the same floor (or letterbox) rather than constructing effects at 1×1.

### Registry compatibility

Authoritative consumer pin: **sysc-Go v1.0.3** (walls `go.mod` and `MinSyscGoVersion = "1.0.3"`). Tagged v1.0.3 registry names, all wrapped by walls `CreateOptimizedAnimationWithText`:

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

Present on local HEADs, **not** on tag v1.0.3, **not** wrapped by walls:

| ID | Where | Gate |
|---|---|---|
| skull, skull-text | `/home/nomadx/sysc-Go` `LibraryVersion 1.1.0`; Documents master lists skull as VersionAdded 1.0.3 but the tag worktree disagrees | Needs a published sysc-Go tag |
| sonar, cracktro, plasma | Documents/sysc-Go only | Unpublished; out of v1 |
| burn | source + TextUpdatable test, not in v1.0.3 registry | Out of v1 until registered and tagged |
| ticker | TUI spinner, not a wallpaper effect | Ignore |

Artwork: walls loads UTF-8 text from an allowlisted absolute path (`isSafePath`: `$HOME/.config`, `$HOME/.local/share`, `/usr/share`, `/usr/local/share`) or bundled `SYSC.txt`. Empty text becomes `"SYSC-WALLS"`.

Unicode/font: matrix rain includes non-Latin letters. East-Asian wide runes are not systematically tested. Fallback must be documented (missing glyph → replacement, width 1) rather than silently shifting columns. Combining characters: unrun.

## Wallpaper layer evidence

### gSlapper on this machine (measured)

Live `niri msg -j layers` on `DP-1`:

| namespace | layer | keyboard_interactivity |
|---|---|---|
| `slapper` | Background | None |
| `sysc-shell:bar` | Top | None |
| `sysc-shell-toast` | Overlay | None |
| `sysc-shell-osd` | Overlay | None |

gSlapper process: `gslapper -I /run/user/1000/sysc-shell/gslapper-DP-1.sock --no-save-state -o fill no-audio loop -r 30 DP-1 <still png>`. RSS **100212 KiB**, CPU **0.1%**, elapsed ~23 h. This is a still image, not video and not an effect. Video/effect CPU is unrun.

Creation path **inspected** (`gSlapper/src/main.c:3294-3313`): empty `wl_region` as input region, `zwlr_layer_shell_v1_get_layer_surface(..., BACKGROUND, "slapper")`, anchors all four, size 0×0, **exclusive zone -1**. No `set_keyboard_interactivity` call found; Niri reports None. GPL-3 C/GStreamer. Evidence only.

### Depth clock policy (inspected, do not collide)

`internal/shell/depthclock.go`: namespace `sysc-wallpaper-depth-clock`, **Bottom** (not Background), exclusive -1, keyboard none, empty input rects, 560×176 card. Lives above a Background wallpaper. A terminal engine on Background is compatible if it does not use Bottom and does not take keyboard.

### sysc-wayland (inspected)

Public module: connection, core protocol, `wl_shm` (`Shm.CreatePool`, `ShmPool.CreateBuffer`), `Surface.Frame`, `Surface.SetInputRegion`, `Surface.DamageBuffer`. **No** generated layer-shell package in the module. Shell generates one:

```
internal/platform/wayland/layershell/generate.go
//go:generate … sysc-wayland-scanner@v0.1.1 … wlr-layer-shell-unstable-v1.xml
```

Comment pins protocol commit `2b8d4332…` (version 5, what Niri 26.04 advertises). A new engine must generate the same way inside *this* repository, not import `sysc-shell/internal/...`.

Fractional-scale and viewporter are also generated in-shell, not shipped by sysc-wayland. Needed for laptop scale 1.25.

Dispatch: one goroutine owns the connection (`sysc-wayland/AGENTS.md`).

### Hosting a terminal on the wallpaper layer (inspected, rejected)

sysc-walls launches `kitty --start-as=fullscreen --class sysc-walls-screensaver <display> --effect …` (`config.go:696-746`). Compositor helpers add *window rules*, not layer-shell (`internal/compositor/niri.go` is `niri msg` focus/outputs only). The display binary writes ANSI to stdout. That is an xdg-toplevel. Ordinary Niri window rules cannot turn it into a Background surface. Fullscreen Kitty takes a window, input, and focus. **Does not meet the product requirement.**

### Extending gSlapper (inspected, rejected)

No terminal cell path. GPL-3 C + GStreamer. Shell already owns gSlapper as the image/video engine (`wallpaper/engine.go`, `gslapper.go`). Putting effects there would mix languages, licences, and process identities, and still would not produce live terminal cells without a second renderer.

### Extending sysc-walls (inspected, rejected as the wallpaper engine)

Screensaver: idle daemon + systemd user unit + Kitty. GPL-3. CGO Wayland idle (`pkg/idle`). Palette tables duplicated in `optimized.go:getThemePalette` instead of calling sysc-Go. Pin lag vs local HEAD. `sysc-911` already commissions Control Centre/Settings access to this screensaver and states it is **not** a wallpaper-layer backend. Adding a background mode here would fork process ownership and put a second effect selector next to 911.

## Shell wallpaper ownership (must not duplicate)

From `2026-09-03-wallpaper-design.md` and current `internal/wallpaper`:

- **D1** panel + `internal/wallpaper` owns library, assignments, engines. Relays off the Wayland owner.
- **D12–D18** gSlapper-first, shell-owned sockets, one process per connector, never `*`, never `pkill` by name, never kill foreign instances.
- **D19** assignments JSON: `connector -> {kind: image|video, path, preview_path, desired_playback}`. Kind is only `KindImage` / `KindVideo` (`media.go:17-22`). `checkPath` rejects empty/newline/non-UTF-8 paths (`persist.go:59-69`). An effect encoded as a fake filename would fail kind detection and theme-seed write-back.
- `EngineFor(kind)` returns gSlapper for any kind if installed (`service.go:69-77`). A third kind needs an explicit engine name, not a reused image path.
- Pause/Resume exist (`OpPause`/`OpResume`) and are **video-only** today (`service.go` comments; images have no pipeline).
- Engine pills in the picker are a readout of installed backends (`popout_wallpaper.go:716-732`), not a second settings page.
- Restore (`D16`) stops gSlapper and applies a still via awww/swaybg. That exception is for media, not for effects.

Idle: no `IdleService` in current shell source (grep empty). Session suspend is `loginctl suspend` (`popout_session.go`). Screensaver idle is sysc-walls' daemon (`sysc-911`). Lock is `sysc-910` (not implemented). The engine must pause on signals the shell can already observe: explicit pause, output disconnect, session actions once wired — not a new occlusion detector.

## Approach comparison

| Approach | Behaviour fit | Architectural fit | Licence / language | Verdict |
|---|---|---|---|---|
| Kitty (or any terminal) hosted as wallpaper | No. xdg-toplevel; steals focus; walls already does this as a screensaver | Would fight wallpaper.Service and 911 | Kitty is a user binary we would exec, not a library | Reject |
| Native Go cell renderer on a real Background surface in a **dedicated engine binary** | Yes, if ANSI subset + font raster work | Matches “second consumer of sysc-Go” and “shell does not own a Background wallpaper surface” (wallpaper design out-of-scope). User assigned `sysc-terminal` | MIT engine + MIT sysc-Go + BSD sysc-wayland | **Recommend** |
| Native renderer inside sysc-shell | Would work technically | Violates wallpaper D1 out-of-scope, Wayland-owner budget, and “one repo until a second consumer” — the second consumer is this engine | Same | Reject |
| Background mode in sysc-walls | Would still need the same renderer | Mixes idle/systemd/Kitty with wallpaper; GPL-3 + CGO; duplicate selector vs 911 | GPL-3 infects the engine if imported | Reject |
| gSlapper extension | No cell path | Shell already owns it for image/video | GPL-3 C | Reject |
| Upstream `RenderCells` on every sysc-Go effect | Cleaner long-term | Cross-repo release gate before wallpaper can ship | MIT | Optional later; not required for v1 |

**Recommendation:** a small Go process in this repository, one instance per output, layer-shell Background, empty input region, exclusive zone **0** (commission; gSlapper uses -1 and that is live-proven, but 0 is the specified contract), keyboard none, namespace `sysc-terminal`. Consume sysc-Go `Render()` through a bounded SGR interpreter (reuse the `interpret()` approach already proven in cellstyle tests). Rasterise with a monospace font via `go-text/typesetting` (already used by shell; do not import shell). sysc-shell grows `KindEffect` and supervises the binary like gSlapper.

A small sysc-Go `RenderCells` API is *not* required to start. Assess it if the ANSI subset becomes a tax; do not rewrite 17 `Render` methods first.

## Licensing and dependencies

| Module | Licence | Use |
|---|---|---|
| sysc-Go | MIT | Import `animations` at **v1.0.3** for v1 registry. Newer effects wait on a tag |
| sysc-wayland | BSD-3 | Pin **v0.2.2** to match shell until a measured need for v0.3.1 (proxy ID reuse). Generate layer-shell / fractional-scale / viewporter locally |
| sysc-walls | GPL-3 | Do not import. Reuse *ideas* (size floor, artwork allowlist, 20 FPS) |
| gSlapper | GPL-3 | Do not import. Reuse *behaviour* (layer, IPC verbs, one process per connector) |
| charmbracelet lipgloss / x/ansi | MIT | Already pulled by sysc-Go. Parser may use `x/ansi` for CSI decoding |
| go-text/typesetting, golang.org/x/image | existing in shell | Font raster; add as direct requires here |

No handwritten C/CGO. No new test framework.

## Resource budget — proposed, not approved

Commission: choose numeric pass/fail **with the owner** before implementation. These are proposals.

Same effect (`fire`), same cell metrics, target **20 FPS** (walls ticker), compare:

| Machine | Geometry | Proposed pass |
|---|---|---|
| Desktop (this machine) | 3440×1440 / scale 1 | CPU p95 < 25% of one core while mapped; RSS < 150 MiB; no RSS growth over a 10 min soak; paused CPU ≈ idle and **zero** `Update`/`Render` |
| Laptop | 1536×864 logical / scale 1.25 | Same numeric caps |

Dropped frames: skip stale work if a tick is still rendering when the next tick is due; do not queue more than one pending frame. Recovery: last good buffer stays mapped; on engine crash the shell restores the previous assignment (image/video/effect), never a foreign gSlapper.

gSlapper still baseline **measured**: 100 MiB RSS, 0.1% CPU. sysc-walls display Kitty baseline **unrun** (would steal the live session). Laptop **unrun**. Two-output **unrun** (this compositor has one output; AGENTS.md already records that).

## Remaining risks and smallest proofs

| Risk | Status | Smallest proof |
|---|---|---|
| Bounded SGR decode of real `Render()` into a stable grid | Interpreter exists in tests; not wired to a grid | **Experiment A** (headless, no compositor) |
| Niri maps a *new* Background namespace without stealing input, and gSlapper can be restored | `slapper` is live-proven; `sysc-terminal` is not | **Experiment B** (live, needs approval) |
| Font cell metrics + fractional scale 1.25 | Inferred from shell text renderer | After A+B, one laptop check |
| Artwork path / wide glyphs / malformed CSI | Unrun | Table tests in Stage 3 Task 1 |
| Animated CPU at 430×90 | Inferred from 240×64 ANSI timing | Soak after a real surface exists |

### Experiment A — headless cell adapter (no compositor)

Need: confirm `Render()` plus a bounded parser yields a dense `cols×rows` grid for fire (no text) and matrix (Unicode), and that OSC/DCS/APC are ignored.

Commands (from a worktree of this repo, after Stage 3 Task 1 exists):

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/cell -run 'TestFireGridDimensions|TestMatrixHasNoOSC|TestMalformedCSIIsIgnored'
```

Expected: PASS. Fire 80×24 → 80 columns, 24 rows. Matrix frames contain no OSC. A truncated CSI does not panic or shift later cells.

Cleanup: none (pure test).

Until Task 1 exists, this experiment is specified, not run. Running `TestCellPainter*` (**measured** pass) is the closest existing check and does not map a surface.

### Experiment B — one Niri Background surface (needs owner approval)

Need: prove a namespace that is not `slapper` can sit on Background with empty input region, exclusive zone 0, keyboard none, without eating clicks or replacing the owner's wallpaper permanently.

Preconditions: owner agrees; current gSlapper assignment is recorded; duration ≤ 60 s.

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

Expected observations: `sysc-terminal` appears on Background; bar and clicks still work; after stop, previous wallpaper is back; `gslapper-DP-1.sock` still owned by the original pid or is cleanly relaunched by the shell — not killed by name.

Cleanup: destroy the test surface; if the still wallpaper is gone, apply it through the wallpaper panel (not by copying binaries).

**Do not run Experiment B until the owner approves this recipe.**

## Ownership proposal (for the design, not yet chosen)

Subject to owner approval of this audit:

- **This repository** owns the engine binary, cell adapter, font raster, layer-shell mapping, and its Unix-socket control protocol.
- **sysc-shell** owns assignment persistence (`KindEffect`), process supervision (owned sockets, generation, restore), panel/Settings chrome, and pause triggers it can observe.
- **sysc-Go** remains the only effect implementation. v1 pins v1.0.3. Newer effects are a release gate on sysc-Go.
- **sysc-walls / sysc-911** keep screensaver controls. No second effect picker that writes a different store.
- **sysc-910** lock integration is a later pause source, not a v1 blocker.

## What this audit did not do

- Did not map a live `sysc-terminal` surface.
- Did not run sysc-walls display (would fullscreen Kitty over the session).
- Did not measure laptop, two-output, video gSlapper, or a 10-minute soak.
- Did not read missing `sysc-909` reports.
- Did not edit preserved dirty trees.

Next concrete action: owner reviews this audit, the design, and the proposed budgets. Stage 3 does not start without that approval. Experiment B does not start without a separate yes.
