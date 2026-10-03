# Terminal wallpaper engine — design

Date: 2026-10-03. Tracker: `sysc-912`. Depends on feasibility audit in
this directory. Status lives in bd, not here.

Design **not approved** until owner accepts architecture, scope, numeric
resource budget. Implementation = Stage 3 of commission, waits for that yes.

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
| Exclusive zone | 0 |
| Keyboard | none |
| Input region | empty `wl_region` (not nil: nil means whole surface takes input) |
| Coverage | output configured size; no exclusive reservation |
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
no-op at real 3440×1440 (430×90 well above floor), undefined
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

**Per-glyph alpha cache: required, not optimisation.** At 430×90 frame
is 38 700 cells, at 20 FPS = 774 000 glyph rasterisations per
second if each cell goes through `vector.Rasterizer` every frame. sysc-shell
measured same trap — per-frame glyph raster was 15% of bar repaint — and
its `internal/render/text.go` keeps `map[rasterKey]Mask` of per-glyph
`*image.Alpha` rasters, bounded at 256 with half-when-full eviction. Monospace
font at 12 px draws from few dozen distinct codepoints, so per-glyph **alpha**
cache turns steady state into 38 700 small bitmap
blits plus tint. Requirements:

- Key on (glyph id, ppem) only. Cache alpha mask; tint with cell `38;2` colour at blit. Fire/matrix/skull emit many distinct RGBs; colour in key plus 256-entry bound evicts every frame, you pay
  uncached 774k rasters/s this section exists to avoid. Backgrounds filled, not rastered.
- Bounded, with eviction — unbounded map = RSS leak, budget has
  RSS cap.
- Cache `*font.Font`, not `*font.Face`: font read-only and
  concurrent-safe, face not.
- Check: second identical frame costs no more than first
  after warm-up. Without cache ratio is 38 700 rasterisations per frame,
  test fails immediately.

Font: first existing path among config `font_path`, then
`/usr/share/fonts/TTF/JetBrainsMonoNerdFont-Regular.ttf`,
`/usr/share/fonts/TTF/JetBrainsMono-Regular.ttf`,
`/usr/share/fonts/noto/NotoSansMono-Regular.ttf`. Missing font = hard
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
  requests frame. 60 Hz display asks 60 times/second, owner advances effect only if at least 50 ms passed since last
  tick. 144 Hz display throttled same way. Display that stops
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
     │  frame callback, ≥50ms since last tick
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

## Resource budget (proposed)

Owner must accept or rewrite these numbers before Stage 3.

Effect: `fire`. Font: default 12 px logical (15 device px at scale 1.25).
Target: 20 FPS. Soak: 10 minutes.

| Gate | Desktop 3440×1440 / 1 | Laptop 1536×864 / 1.25 |
|---|---|---|
| CPU p95 while playing | < 25% of one core | same |
| RSS | < 150 MiB | same |
| RSS delta over soak | < 10 MiB | same |
| Paused | 0 Update/Render; CPU ≈ idle | same |
| Dropped ticks | skip, never queue > 1 | same |

### Where the budget goes — measured versus unmeasured

| Component | Status | At 430×90 | Share of 50 ms |
|---|---|---|---|
| `Update` + `Render` | **measured** | p95 **8.43 ms**, 248 029 B/frame | ~17% |
| Bounded parse of those bytes | unmeasured, by construction one pass | — | small |
| Glyph raster, cached (D4) | unmeasured | few dozen distinct glyphs at 12 px, tinted blits | unknown |
| Glyph raster, uncached | unmeasured, and 774 000 rasters/s | not acceptable | would likely blow the gate |
| `wl_shm` upload + attach | unmeasured | 19.3 MB/frame, double-buffered | unknown |

Earlier revision cited "ANSI-only fire at 240×64 was 3.7 ms/frame
measured" and left wallpaper size as inference. Now measured at
wallpaper size: **p95 8.43 ms** at 430×90, 17% of tick,
leaves room. Unmeasured half = raster, budget not defensible
until measured.

**Therefore first budget gate headless, at real cell count, no
compositor.** Check at 80×24 is 20× smaller than real grid and exercises
no raster, passes no matter how broken actual path is. Gate
ticks N frames of `Update` → `Render` → parse → raster at 430×90, fails on
mean or p95. Cheapest honest proof of largest unknown, must not be deferred to live soak. If it misses, fixes in order: skip-rate, then smaller font, then cell-level dirty-rect upload — not new renderer architecture.

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

1. Accept D1–D11 or name change.
2. Accept or rewrite resource numbers. `Update`+`Render` half
   measured (p95 8.43 ms at 430×90); raster half not, plan puts
   headless 430×90 check ahead of live soak to measure it.
3. Approve or defer Experiment B (live namespace probe).
4. Confirm exclusive zone 0 rather than gSlapper live -1.
5. Confirm v1.0.3 pin (no skull) versus waiting on sysc-Go release.
6. Confirm blank preview tile in D8, or ask for `--preview` now.
7. Degenerate outputs (cell grid under 21×24) refuse assignment. Decided. Remaining owner items: exclusive zone, skull pin, Experiment B,
   blank preview.