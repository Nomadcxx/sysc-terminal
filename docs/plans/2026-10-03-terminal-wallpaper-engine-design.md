# Terminal wallpaper engine — design

Date: 2026-10-03. Tracker: `sysc-912`. Depends on feasibility audit in
this directory. Status lives in bd, not here.

Owner approved D1–D11 and the numeric budget on 2026-10-04. Desktop live
qualification and installation were separately approved the same day.

## Goal

Paint live sysc-Go terminal effects on Niri wallpaper layer, behind windows,
no desktop input or focus theft, selectable per output via existing
sysc-shell wallpaper service.

Out of scope: terminal emulator, compositor, effect editor, shaders,
asset marketplace, application toolkit, screensaver (sysc-walls / `sysc-911`),
lock screen (`sysc-910`), C/CGO, copying palettes or registries.

## Decisions

### D1 — Dedicated engine binary in this repository

`sysc-terminal` = supervised process, one instance per connector, launched
by sysc-shell same way as gSlapper. Shell does not grow Background
wallpaper surface (wallpaper design already left that out). sysc-walls stays
idle screensaver. gSlapper stays image/video engine.

### D2 — Native cell raster, not a hosted terminal

Consume sysc-Go `Update` / `Render() string` / `SetText` / concrete `Resize`
where present. Decode bounded SGR subset into cell grid. Rasterise with
monospace font onto `wl_shm` buffers. No exec Kitty. No `RenderCells` added
to sysc-Go for v1.

### D3 — Layer-shell contract

Per mapped output:

| Field | Value |
|---|---|
| Layer | Background (`zwlr_layer_shell_v1` layer 0) |
| Namespace | `sysc-terminal` |
| Anchor | top, right, bottom, left |
| Size | 0×0 (compositor configures) |
| Exclusive zone | -1 (stretch under panels; a wallpaper does not reserve or avoid) |
| Keyboard | none |
| Input region | empty `wl_region` (not nil: nil means whole surface takes input) |
| Coverage | output configured size; extends under panels |
| On compositor error or display disconnect | Unmap what exists, report, exit non-zero |

No Bottom (depth clock lives there, namespace
`sysc-wallpaper-depth-clock`). No `slapper` or `sysc-shell:*`.
Keep text/assets upright: ignore output transform for glyph orientation; if
Niri already applies transform to buffer, follow shell existing
output-transform policy, no second one invented. Confirm in first live gate.
gSlapper ignores `wl_output` transform; we same until that gate says otherwise.

Exit row not a nicety. Display dies + process lingers → output has namespace
that paints nothing, shell generation logic has nothing to restart, user left
with no wallpaper, no recovery. Exiting makes it normal supervision event.

Shell coverage: add `sysc-terminal` to explicit case list in
`wallpaperOurNamespace` (`internal/shell/popout_wallpaper.go` on `origin/main`,
line 1468), today:

```go
func wallpaperOurNamespace(namespace string) bool {
	switch namespace {
	case "slapper", "awww-daemon", "swaybg":
		return true
	}
	return strings.HasPrefix(namespace, "sysc-shell")
}
```

`sysc-terminal` goes in `switch`. Must **not** rely on `sysc-shell` prefix:
engine = sibling process, not shell aux surface — and prefix deliberately
narrow so anything else on Background stays foreign. Without new case,
`niri.BackgroundOwners(layers,
wallpaperOurNamespace)` (called at line 1462) treats surface as foreign,
`Snapshot.Covered` makes picker lie.

### D4 — Cell grid from font metrics, device pixels throughout

Earlier revision said "logical output size … yield columns
and rows" *and* "12 px logical, multiplied by `scale120/120`". Those double-count
scale, cannot both be true: `scale120/120` converts logical size to pixel size,
so grid already derived from pixel size is in pixels.

**Decision: shm buffer in device pixels, so is everything derived
from it.** At scale 1.25 on 1536×864 output buffer = 1920×1080; 12 px
logical cell = 15 device px, cell count from mode pixel size, not logical size.
Alternative — logical-size buffer plus
`Surface.SetBufferScale(2)` — gives compositor-scaled bitmap, soft
glyphs, and whole point of terminal effect is crisp block-drawing
characters. So:

- `cols = modePixelWidth / round(advance(M) at pixelSize)`, `rows = modePixelHeight / round(lineHeight at pixelSize)`, both integer divisions.
- `pixelSize = round(12 * scale120 / 120)`, clamped to at least 1.
- Leftover pixels (`modePixelWidth - cols*cellW`, `modePixelHeight - rows*cellH`) letterboxed with theme background colour (first palette entry, or black). Not distributed into last row or column.
- `wl_fractional_scale` bound. Preferred-scale event changing `scale120` → grid recomputed, surface reconfigured. 1.25 laptop first-class target, so scale change must not leave stale grid.

**Crop direction, decided.** Earlier revision said "construct effects at
`max(cols,21)` × `max(rows,24)` then crop/centre", silent
no-op at real 3440×1440 (491×90 well above floor), undefined
elsewhere. Floor exists because fireworks does
`rand.Intn(width-20)` and aquarium fish ranges panic on `n <= 0`; construction-time
guard, not layout intent. So effect always
constructed at `max(cols,21)` × `max(rows,24)`, and:

- If `cols*rows` already at or above floor, decoded grid taken
  from **top-left**, unmodified. Only case on real hardware.
- If output cell rectangle below 21×24, **refuse assignment.**
  No crop, centre, or letterbox of 21×24 construction into smaller
  cell rectangle — those three instructions conflict, 21×24 cells cannot
  sit inside 4×4 grid without cropping. Cropping from either end changes
  picture, correct end depends on effect anchoring.
  *Not* knowable from frame: measured at five updates, fire row 0
  already non-blank (`"       ▒  ▒                         "`), so no
  "blank top rows" to trim. Construction still uses
  `max(cols,21)×max(rows,24)` so fireworks/aquarium no panic; mapping surface
  whose cell grid under floor = hard error, shell reports unavailable, same as
  missing font.
- `width*height*4` must not overflow int32; reject at configure time, not at
  buffer creation.

**Per-glyph alpha cache: required, not optimisation.** At the measured
491×90 frame size there are 44 190 cells; at 8.3 FPS that is 368 250
glyph rasterisations per second if each cell goes through `vector.Rasterizer`
every frame. sysc-shell
measured same trap — per-frame glyph raster was 15% of bar repaint — and
its `internal/render/text.go` keeps `map[rasterKey]Mask` of per-glyph
`*image.Alpha` rasters, bounded at 256 with half-when-full eviction. Monospace
font at 12 px draws from few dozen distinct codepoints, so per-glyph **alpha**
cache turns steady state into 44 190 small bitmap
blits plus tint. Requirements:

- Key on (glyph id, ppem) only. Cache alpha mask; tint with cell `38;2` colour at blit. Fire/matrix/skull emit many distinct RGBs; colour in key plus 256-entry bound evicts every frame, you pay
  uncached 774k rasters/s this section exists to avoid. Backgrounds filled, not rastered.
- Bounded, with eviction — unbounded map = RSS leak, budget has
  RSS cap.
- Cache `*font.Font`, not `*font.Face`: font read-only and
  concurrent-safe, face not.
- Check: draw an identical frame twice and assert every cached glyph key
  retains the same alpha-mask pointer. This proves cache reuse without a
  timing-sensitive unit test; Task 8 measures steady-state draw cost at
  wallpaper size. Without cache, the second draw replaces the masks and the
  check fails.

Font: first existing path among config `font_path`, then an ordered list of
braille-capable monospace faces under `/usr/share/fonts/`,
`/usr/local/share/fonts/` and `/run/current-system/sw/share/fonts` — JetBrains
Mono (Nerd Font first), DejaVu Sans Mono, Noto Sans Mono — covering both the
Arch/Nix `TTF/` and Debian/Ubuntu `truetype/` layouts, then any font file
under those roots. Missing font = hard
start error (honest unavailable), not crash loop. No fontconfig CGO. Missing
glyphs: replacement `.notdef` at width 1 cell. Combining marks: not combined;
documented limit. East-Asian wide runes: if `uniseg`/`runewidth` reports width
2, occupy two cells, skip next column; if font draws them narrow,
accept gap (documented).

### D5 — One owner goroutine per process, one effect instance, one clock

Wayland dispatch goroutine owns proxies, buffers, frame callbacks, and
pause flag. Effect `Update`/`Render` run on single worker that submits
immutable cell grid back over capacity-1 channel, latest wins. Effects
must not start goroutines.

**One clock, not two.** Earlier revision specified both "a 50 ms ticker
while playing" and "a frame callback rasterizes if a tick is due",
two sources of truth for same decision, can disagree. Frame
callback = only clock:

- Compositor `wl_surface.frame` callback only thing that
  requests frame. The owner advances at most once per 120 ms (8.3 FPS),
  leaving 30 ms of CPU per frame within the 25% one-core budget.
  60 Hz and 144 Hz displays use the same limit. Display that stops
  sending callbacks stops engine, which is what 1 Hz link or
  occluded-and-idle surface should do anyway.
- If previous tick still in flight when callback arrives, tick dropped. Same rule as capacity-1 channel latest-wins, not
  a second one: never more than one frame pending, in either place.
- Paused: no `Surface.Frame` request, no `Update`/`Render` call. Last buffer stays attached. Resume must request **one** `Surface.Frame` to
  restart callback chain; without it first live pause/resume hangs
  on frozen buffer until something else damages surface. "Paused CPU
  is idle and `Update`/`Render` count is zero" = hard budget gate, so
  enforced at this level and re-asserted through effect wrapper in
  plan checks.

Reset: if concrete type has `Reset()`, call it; else reconstruct effect.
`FireEffect`, `FireTextEffect`, `FireworksEffect` have no `Reset`.

**Exit on display loss.** `Display.SetErrorHandler` and connection read error
both return error owner cannot recover from. Owner
unmaps what it has, writes one line to log, exits non-zero so
shell generation logic sees process gone and restarts it. Must not
loop retrying: wallpaper process alive but owning no surface looks
identical to working one from shell view, user gets
black output with "running" pill.

### D6 — Bounded ANSI subset

Accept only:

- UTF-8 runes
- `\n` as row advance
- CSI SGR: `0` / empty (`\033[m` / `\033[0m`), `38;2;r;g;b`, `48;2;r;g;b`
- Ignore (do not execute): other SGR parameters, cursor movement, erase,
  OSC, DCS, APC, PM, SOS, CSI with unknown finals, BEL, hyperlinks, clipboard

Artwork and `Render()` output never reach shell. Truncated or malformed CSI
skipped until next rune not part of escape. No query
responses written to socket or Wayland.

**Hand-rolled scanner, decided.** Earlier revision left this open as
"reuse `charmbracelet/x/ansi` if it stays smaller than a 40-line state
machine", not a decision — condition to be re-litigated during
implementation. Choose hand-rolled scanner:

- Accepted grammar = four things: UTF-8 rune, `\n`, SGR parameter
  list restricted to `0`, empty, `38;2;r;g;b`, `48;2;r;g;b`, and "skip until
  next non-escape rune" for everything else. General CSI parser carries
  state, tables, dependency understanding sequences this engine must
  *ignore*, a class of bug rather than feature.
- `x/ansi` already in build via sysc-Go, so cost not the
  dependency — it's that second parser implementation must be kept in
  step with subset whose whole point is small and auditable.
- Semantic model = `interpret()` helper in uncommitted sysc-Go
  `cellstyle_test.go`: walk `\033…m`, pair each rune with active SGR. That
  helper = reference for what "correct" means, ~40 lines.

Rejected alternative not banned forever. If subset ever grows to
need real CSI, cursor, or erase handling — meaning library
started emitting layout, not just colour — revisit.

### D7 — Control protocol (gSlapper-shaped, not gSlapper)

Unix socket path chosen by parent (shell):
`$XDG_RUNTIME_DIR/sysc-shell/terminal-<sanitized-connector>.sock`.
Line-oriented, max 4 KiB per line, 2 s write/read deadline. Socket mode 0600.
Peer: same uid via `SO_PEERCRED` (gSlapper master does not check credentials;
we do, socket lives in user runtime dir and shell
already does this for gSlapper).
Verbs:

| Request | Ack |
|---|---|
| `query` | `STATUS: playing\|paused effect <id> theme <theme>` |
| `pause` | `OK` (idempotent) |
| `resume` | `OK` (idempotent) |
| `reset` | `OK` |
| `change effect <id> theme <theme> [file <abs>]` | `OK` or `ERR …` |
| `stop` / `quit` | `OK` then exit after unmapping |

Unknown verbs: `ERR unknown`. Effect ids and themes must match **pinned**
sysc-Go registry (v1.0.3 names). File must be absolute UTF-8, no newlines, and
pass same allowlist walls uses (`$HOME/.config`, `$HOME/.local/share`,
`/usr/share`, `/usr/local/share`) plus `$HOME` only if we decide it — v1
keeps walls allowlist, not whole home directory. Shell never sends shell
command string.

**Engine also answers stateless `--list` flag**, one exec, no socket, no
running instance:

```
sysc-terminal --list
  effect <id> <requires-text 0|1>
  theme  <name> <aliases-comma-separated>
  version <libraryVersion>
```

Exists because D9 needs picker to list ids and themes, and AGENTS.md
forbids copying registries or palettes. sysc-Go v1.0.3 already exports
`GetEffectNames()`, `GetTextBasedEffects()`, `GetThemeNames()`, and
`GetThemeMetadata(name)` (resolves aliases, returns nil for unknown),
so engine prints what library reports, not transcribed table.
Works before any instance running, which socket query cannot. Library
does *not* expose theme→colour mapping; that is per-effect
palette-getter switch inside `internal/effect`, never printed.

`--list` also becomes probe (see D8), so registry question and
availability question = one exec instead of two.

### D8 — Assignment kind `effect`

Extend wallpaper assignments; do not stuff effect into `path` as fake
image.

```text
connector -> {
    kind: "image" | "video" | "effect",
    path: abs media path, empty when kind=effect,
    effect: registry id, empty when not effect,
    theme: palette name, empty when not effect,
    artwork: abs allowlisted path, optional,
    preview_path: still, unused for effects,
    desired_playback: "playing" | "paused"
}
```

`EngineFor(KindEffect)` returns `"sysc-terminal"` if binary probes
successfully, else `""` and picker shows unavailable banner; image/video
keep working. Two shell-side details to get right, both verified
against `origin/main`:

- `EngineFor` = method on `Capabilities`, not free function on `Kind`, and
  current body is
  `if c.GSlapper { return EngineGSlapper }; if kind == KindVideo { return "" }; return c.Static()`.
  Effect branch must be **inserted before `if c.GSlapper` early
  return**, or gSlapper swallows `KindEffect` on any machine with gSlapper
  installed. File calls existing body "the one statement of that
  policy", so effect case belongs there, nowhere else.
- Needs new `Capabilities` field for "terminal engine installed",
  alongside `c.GSlapper`. Probing = `exec.LookPath("sysc-terminal")` plus
  successful `sysc-terminal --list`, not `--help | grep -F -- '--ipc-socket'`:
  sentinel line cheaper, no dependence on flag spelling surviving
  rename, same output feeds picker list.

`Apply` for `KindEffect` must not `os.Stat` media path (today image/video
path requires real file — `engine.go:317-322`). Route through existing
`Engine` seam (`Apply` / `Restore` / `SetPaused` / `Capabilities`): separate
`terminalEngine` selected by kind, not effect branch inside
`gslapperEngine`, so image/video behaviour untouched. `SetPaused` today is
video-only; KindEffect must pause.

**Preview: blank tile in v1, decided.** `preview_path` = still, none exists
for effect. Rather than render one headlessly for every effect in
picker, v1 shows blank tile, effect first frame appears
the moment it applied on output. Alternative — `sysc-terminal --preview
<id> <out.png>`, one headless frame — small addition if owner finds
blank tile unacceptable, upgrade path, not v1
requirement. Do not leave undecided: picker code needs to know whether
to render tile or skip it.

One selected backend per output: applying effect stops owned gSlapper
or fallback on that connector first (existing generation/ready/retire path).
Applying image/video stops owned terminal instance same way.
Never kill user-launched `sysc-terminal` or gSlapper whose argv lacks our
socket path.

Theme seed: v1 does not write `theme.source`/`theme.seed` from effect.
Leave previous seed. Depth-clock masks stay image-path-based; effect
assignment installs no mask.

Reconnect: persist effect assignment; replay on connector. Startup
reconcile unchanged. Restore on effect output relaunches engine with
persisted effect, does not go through D16 still-fallback. Restore on
media output unchanged.

### D9 — Shell chrome

Reuse wallpaper panel and service. Add Effects filter/source next to
Images/Videos, listing registry ids (plus theme combo, artwork file picker
for RequiresText). **Id list and theme list come from `sysc-terminal
--list` (D7), not from table in shell.** Engine pill row gains
`sysc-terminal` when binary installed. Settings/Control Centre: do
**not** add second effect selector. Screensaver effect selection belongs to
`sysc-911` and sysc-walls own config. If both products show same registry
names, they write different stores (wallpaper assignments vs walls
`daemon.conf`).

Absent binary: honest banner, existing wallpaper still usable.

### D10 — Pause sources (v1)

| Source | Action |
|---|---|
| Panel Pause / IPC `pause` | Stop tick, keep last frame |
| Output disconnect | Stop process, keep assignment |
| `OpPause` from shell | Same as IPC pause |
| Session suspend | Shell sends pause before `loginctl suspend` if it already has hook; if not, process simply dies with session — do not invent logind listeners inside engine for v1 |
| Lock / screensaver / DPMS | Not v1. `sysc-910` / `sysc-911` / Niri output power become extra `OpPause` sources later. No occlusion detector |

### D11 — Pins

| Module | Pin | Reason |
|---|---|---|
| github.com/Nomadcxx/sysc-Go | v1.0.3 | Walls-compatible registry. skull/sonar/cracktro wait on later tag |
| github.com/Nomadcxx/sysc-wayland | **v0.3.1** | Version sysc-shell `origin/main` requires. Earlier revision said v0.2.2 because "matches shell", read off shell dirty primary checkout; `origin/main` `go.mod` says v0.3.1, origin/main = baseline for both this design and shell worktree |
| layer-shell XML | same commit as shell (`2b8d43325b7012cc3f9b55c08d26e50e42beac7d`) | Niri 26.04 advertises v5 |
| go directive | `go 1.26` | sysc-wayland v0.3.1 declares `go 1.26`; sysc-shell declares `go 1.26.4`. Lower go directive will not resolve require |
| sysc-wayland-scanner tag | same as require | Shell pins scanner at `@v0.1.1` while requiring v0.3.1, works but reads as accident |

v0.2.2 versus v0.3.1 = one hunk in `client/context.go` `RegisterWithID`: v0.2.2
panics whenever ID already registered, v0.3.1 panics only when
existing object not zombie, because server reuses IDs after
processing destroy. Audit previously filed that as "optional
upgrade"; against live Niri that reuses IDs it is the reason to take v0.3.1,
not reason to stay behind. Everything else engine needs is byte-identical
between two tags, module ships no layer-shell package, so
generating locally required either way.

Handwritten C/CGO requires new owner approval. Not part of this design.

## Architecture

```text
  wallpaper panel / IPC
            │
            ▼
  sysc-shell internal/wallpaper.Service
            │  argv + owned socket, one process per connector
            ▼
       sysc-terminal
            │
     ┌──────┴──────┐
     │ Wayland owner goroutine
     │  layer-shell Background, shm, frame cb, pause flag
     │         │
     │         ▼
     │  frame callback, ≥120ms since last tick
     │         │
     │         ▼
     │  sysc-Go effect.Update/Render  ──► bounded SGR ──► cell grid
     │                                              │
     │                                              ▼
     │                              glyph alpha cache ──► attach buffer
     └───────────────── unix socket (query/pause/change/stop)
```

Protocol types live in `internal/wayland` (generated bindings + owner loop).
Effect construction lives in `internal/effect` (thin wrap of sysc-Go, size
floor, palette from sysc-Go not copied table). Cells in `internal/cell`.
Raster in `internal/raster`. IPC in `internal/ipc`. `cmd/sysc-terminal` =
flags + wiring.

## Resource budget (approved)

Owner approved D1–D11 and the numeric budget on 2026-10-04. The 20 FPS target
missed the headless CPU gate. At 12.5 FPS, a CGO-disabled run later measured
CPU p95 24.441 ms (30.6% of the 80 ms interval), so the approved first
fallback, lower frame rate, was applied. Other numeric budget limits are
unchanged.

Effect: `fire`. Font: default 12 px logical (15 device px at scale 1.25).
Target: at most 8.3 FPS. Soak: 10 minutes.

| Gate | Desktop 3440×1440 / 1 | Laptop 1536×864 / 1.25 |
|---|---|---|
| CPU p95 while playing | < 25% of one core | same |
| RSS | < 150 MiB | same |
| RSS delta over soak | < 10 MiB | same |
| Paused | 0 Update/Render; CPU ≈ idle | same |
| Dropped ticks | skip, never queue > 1 | same |

### Where the budget goes — measured versus unmeasured

| Component | Status | At 491×90 | Share of 120 ms |
|---|---|---|---|
| `Update` + `Render` + bounded parse | **measured** | CPU p95 **11.782 ms** | — |
| Whole effect + parse + cached raster | **measured headlessly** | CPU p95 **23.884 ms**, wall p95 **30.369 ms** | **19.9% CPU** |
| Raster portion | **measured headlessly** | CPU p95 **7.910 ms** | — |
| Glyph raster, uncached | not used; cache check confirms reuse | 368 250 rasters/s at 8.3 FPS | not acceptable |
| `wl_shm` attach and compositor response | unmeasured; live gate required | 19.8 MB/frame, double-buffered | unknown |

The latest CGO-disabled desktop budget check uses the configured 12 px
JetBrains Mono Nerd font. Its measured cell size is 7×16 device pixels,
yielding 491×90 cells at 3440×1440. At the 120 ms target interval, CPU p95
was 23.884 ms (19.9% of one core) and wall p95 was 30.369 ms. The CPU cap is
30 ms per frame. Earlier 80 ms runs passed, but one measured 24.441 ms CPU
p95 (30.6% of one core), which did not leave dependable headroom. The 20 FPS
target also failed its original cap. This gate does not measure Wayland
attach/response or live RSS soak.

**Therefore first budget gate headless, at real cell count, no
compositor.** Check at 80×24 is 20× smaller than real grid and exercises
no raster, passes no matter how broken actual path is. Gate
ticks N frames of `Update` → `Render` → parse → raster at the font-derived
491×90 grid, fails if wall time reaches 120 ms or CPU mean/p95 reaches 30 ms
(25% of the target interval). The approved skip-rate fallback is now in use;
if this gate misses again, remaining fixes are a smaller font, then cell-level
dirty-rect upload — not a new renderer architecture.

Two-output hardware: separate unrun gate, not v1 blocker on this machine.

## Rollout and rollback

- Engine installs to `~/.local/bin/sysc-terminal` (or `/usr/local/bin`) by
  procedure in plan. Shell `scripts/deploy` unchanged until
  KindEffect PR.
- Probe failure ⇒ engine pill absent, no assignment of kind effect can apply.
- Failed apply of effect leaves previous image/video/effect running.
- Roll back engine change: restore previous sysc-terminal binary or
  remove it; shell falls back to gSlapper/static. Assignments with
  `kind=effect` remain in JSON but apply as unavailable until binary
  returns — do not coerce to fake image.
- Live tests must restore owner gSlapper still on DP-1
  (`Sun-Setting-Horizon.png` at audit time) via wallpaper service, not
  `pkill`.

## Compatibility table

v1 ships v1.0.3 registry (15 effects). See audit for full matrix.
First vertical slice: `fire` (no text). Second: `fire-text` or `matrix-art`
(artwork + SetText). Then rest of 15. skull and later wait on sysc-Go tag.

## Open items for the owner

The owner approved desktop Experiment B, Task 9 live qualification, and
installation on 2026-10-04. Laptop and two-output qualification remain
separate gates. Exclusive zone -1 (stretch under panels, so frosted bars
sample effect pixels), the v1.0.3 pin, blank effect preview, and
refusal below 21×24 cells are settled design decisions.
