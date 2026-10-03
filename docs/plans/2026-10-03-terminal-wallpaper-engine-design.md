# Terminal wallpaper engine — design

Date: 2026-10-03. Tracker: `sysc-912`. Depends on the feasibility audit in
this directory. Status lives in bd, not here.

This design is **not approved** until the owner accepts the architecture,
scope, and numeric resource budget. Implementation is Stage 3 of the
commission and waits for that yes.

## Goal

Paint live sysc-Go terminal effects on Niri's wallpaper layer, behind windows,
with no desktop input or focus theft, selectable per output through the
existing sysc-shell wallpaper service.

Out of scope: a terminal emulator, a compositor, an effect editor, shaders,
asset marketplace, application toolkit, screensaver (sysc-walls / `sysc-911`),
lock screen (`sysc-910`), C/CGO, copying palettes or registries.

## Decisions

### D1 — Dedicated engine binary in this repository

`sysc-terminal` is a supervised process, one instance per connector, launched
by sysc-shell the same way gSlapper is. Shell does not grow a Background
wallpaper surface (wallpaper design already left that out). sysc-walls stays
the idle screensaver. gSlapper stays the image/video engine.

### D2 — Native cell raster, not a hosted terminal

Consume sysc-Go `Update` / `Render() string` / `SetText` / concrete `Resize`
where present. Decode a bounded SGR subset into a cell grid. Rasterise with a
monospace font onto `wl_shm` buffers. Do not exec Kitty. Do not add
`RenderCells` to sysc-Go for v1.

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
| Input region | empty `wl_region` |
| Coverage | the output's configured size; no exclusive reservation |

Do not use Bottom (depth clock lives there, namespace
`sysc-wallpaper-depth-clock`). Do not use `slapper` or `sysc-shell:*`.
Keep text/assets upright: ignore output transform for glyph orientation; if
Niri already applies transform to the buffer, follow shell's existing
output-transform policy rather than inventing a second one. Confirm in the
first live gate.

### D4 — Cell grid from font metrics

Logical output size and the measured advance of `M` plus line height at the
configured pixel size yield columns and rows. Default size: 12 px logical,
multiplied by `scale120/120`. Extra pixels after `cols*cellW` × `rows*cellH`
are letterboxed with the theme background colour (first palette entry, or
black). Construct sysc-Go effects at `max(cols,21)` × `max(rows,24)` then
crop/centre the decoded grid into the real cell rectangle so fireworks and
aquarium never see a panicking size. Reject `width*height` that overflows
int32 buffer bytes (`*4` for argb8888).

Font: first existing path among config `font_path`, then
`/usr/share/fonts/TTF/JetBrainsMonoNerdFont-Regular.ttf`,
`/usr/share/fonts/TTF/JetBrainsMono-Regular.ttf`,
`/usr/share/fonts/noto/NotoSansMono-Regular.ttf`. Missing font is a hard
start error (honest unavailable), not a crash loop. No fontconfig CGO. Missing
glyphs: replacement `.notdef` at width 1 cell. Combining marks: not combined;
documented limit. East-Asian wide runes: if `uniseg`/`runewidth` reports width
2, occupy two cells and skip the next column; if the font draws them narrow,
accept the gap (documented).

### D5 — One owner goroutine per process, one effect instance

The Wayland dispatch goroutine owns proxies, buffers, frame callbacks, and
the pause flag. Effect `Update`/`Render` run on that goroutine or on a single
worker that submits an immutable cell grid through a capacity-1 channel
(latest wins). Effects must not start goroutines. Tick interval: 50 ms (20
FPS) while playing. A tick that finds the previous raster still in flight
drops the new tick. Paused: stop the ticker, do not `Surface.Frame`, do not
call `Update`/`Render`. Last buffer stays attached. Reset: if the concrete
type has `Reset()`, call it; otherwise reconstruct the effect.

### D6 — Bounded ANSI subset

Accept only:

- UTF-8 runes
- `\n` as row advance
- CSI SGR: `0` / empty (`\033[m` / `\033[0m`), `38;2;r;g;b`, `48;2;r;g;b`
- Ignore (do not execute): other SGR parameters, cursor movement, erase,
  OSC, DCS, APC, PM, SOS, CSI with unknown finals, BEL, hyperlinks, clipboard

Artwork and `Render()` output never reach a shell. Truncated or malformed CSI
is skipped until the next rune that is not part of an escape. No query
responses are written to the socket or to Wayland.

Reuse `charmbracelet/x/ansi` for sequence splitting if it stays smaller than
a 40-line state machine. The `interpret()` test helper in uncommitted
sysc-Go `cellstyle_test.go` is the semantic model (rune + active SGR).

### D7 — Control protocol (gSlapper-shaped, not gSlapper)

Unix socket path chosen by the parent (shell):
`$XDG_RUNTIME_DIR/sysc-shell/terminal-<sanitized-connector>.sock`.
Line-oriented, max 4 KiB per line, 2 s write/read deadline. Peer: same uid.
Verbs:

| Request | Ack |
|---|---|
| `query` | `STATUS: playing\|paused effect <id> theme <theme>` |
| `pause` | `OK` (idempotent) |
| `resume` | `OK` (idempotent) |
| `reset` | `OK` |
| `change effect <id> theme <theme> [file <abs>]` | `OK` or `ERR …` |
| `stop` / `quit` | `OK` then exit after unmapping |

Unknown verbs: `ERR unknown`. Effect ids and themes must match the **pinned**
sysc-Go registry (v1.0.3 names). File must be absolute UTF-8, no newlines, and
pass the same allowlist walls uses (`$HOME/.config`, `$HOME/.local/share`,
`/usr/share`, `/usr/local/share`) plus `$HOME` only if we decide it — v1
keeps the walls allowlist, not the whole home directory. Shell never sends a
shell command string.

### D8 — Assignment kind `effect`

Extend wallpaper assignments; do not stuff an effect into `path` as a fake
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

`EngineFor(KindEffect)` returns `"sysc-terminal"` if the binary probes
successfully (`sysc-terminal --help` contains `--ipc-socket` or `-I`), else
`""` and the picker shows an unavailable banner; image/video keep working.
One selected backend per output: applying an effect stops the owned gSlapper
or fallback on that connector first (existing generation/ready/retire path).
Applying an image/video stops the owned terminal instance the same way.
Never kill a user-launched `sysc-terminal` or gSlapper whose argv lacks our
socket path.

Reconnect: persist the effect assignment; replay on the connector. Startup
reconcile unchanged. Restore on an effect output relaunches the engine with
the persisted effect, it does not go through D16 still-fallback. Restore on
a media output is unchanged.

### D9 — Shell chrome

Reuse the wallpaper panel and service. Add an Effects filter/source next to
Images/Videos, listing registry ids (and a theme combo, artwork file picker
for RequiresText). Engine pill row gains `sysc-terminal` when the binary is
installed. Settings/Control Centre: do **not** add a second effect selector.
Screensaver effect selection belongs to `sysc-911` and sysc-walls' own config.
If both products show the same registry names, they write different stores
(wallpaper assignments vs walls `daemon.conf`).

Absent binary: honest banner, existing wallpaper still usable.

### D10 — Pause sources (v1)

| Source | Action |
|---|---|
| Panel Pause / IPC `pause` | Stop tick, keep last frame |
| Output disconnect | Stop process, keep assignment |
| `OpPause` from shell | Same as IPC pause |
| Session suspend | Shell sends pause before `loginctl suspend` if it already has a hook; if not, the process simply dies with the session — do not invent logind listeners inside the engine for v1 |
| Lock / screensaver / DPMS | Not v1. `sysc-910` / `sysc-911` / Niri output power become extra `OpPause` sources later. No occlusion detector |

### D11 — Pins

| Module | Pin | Reason |
|---|---|---|
| github.com/Nomadcxx/sysc-Go | v1.0.3 | Walls-compatible registry. skull/sonar/cracktro wait on a later tag |
| github.com/Nomadcxx/sysc-wayland | v0.2.2 | Matches shell. v0.3.1 is an optional upgrade with a measured proxy-ID fix |
| layer-shell XML | same commit as shell (`2b8d4332…`) | Niri 26.04 advertises v5 |

Handwritten C/CGO requires a new owner approval. Not part of this design.

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
     │  tick 20 FPS ──► sysc-Go effect.Update/Render
     │         │
     │         ▼
     │  bounded SGR ──► cell grid ──► font raster ──► attach buffer
     └───────────────── unix socket (query/pause/change/stop)
```

Protocol types live in `internal/wayland` (generated bindings + owner loop).
Effect construction lives in `internal/effect` (thin wrap of sysc-Go, size
floor, palette from sysc-Go not a copied table). Cells in `internal/cell`.
Raster in `internal/raster`. IPC in `internal/ipc`. `cmd/sysc-terminal` is
flags + wiring.

## Resource budget (proposed)

Owner must accept or rewrite these numbers before Stage 3.

Effect: `fire`. Font: default 12 px. Target: 20 FPS. Soak: 10 minutes.

| Gate | Desktop 3440×1440 / 1 | Laptop 1536×864 / 1.25 |
|---|---|---|
| CPU p95 while playing | < 25% of one core | same |
| RSS | < 150 MiB | same |
| RSS delta over soak | < 10 MiB | same |
| Paused | 0 Update/Render; CPU ≈ idle | same |
| Dropped ticks | skip, never queue > 1 | same |

ANSI-only fire at 240×64 was 3.7 ms/frame **measured**. Wallpaper cell count
is larger; raster is unmeasured. If Experiment B or the first live gate
misses this budget, the fix is skip-rate / smaller font, not a new renderer
architecture.

Two-output hardware: separate unrun gate, not a v1 blocker on this machine.

## Rollout and rollback

- Engine installs to `~/.local/bin/sysc-terminal` (or `/usr/local/bin`) by the
  procedure in the plan. Shell `scripts/deploy` is unchanged until the
  KindEffect PR.
- Probe failure ⇒ engine pill absent, no assignment of kind effect can apply.
- Failed apply of an effect leaves the previous image/video/effect running.
- To roll back an engine change: restore the previous sysc-terminal binary or
  remove it; shell falls back to gSlapper/static. Assignments with
  `kind=effect` remain in JSON but apply as unavailable until the binary
  returns — do not coerce them to a fake image.
- Live tests must restore the owner's gSlapper still on DP-1
  (`Sun-Setting-Horizon.png` at audit time) via the wallpaper service, not
  `pkill`.

## Compatibility table

v1 ships the v1.0.3 registry (15 effects). See the audit for the full matrix.
First vertical slice: `fire` (no text). Second: `fire-text` or `matrix-art`
(artwork + SetText). Then the rest of the 15. skull and later wait on a
sysc-Go tag.

## Open items for the owner

1. Accept D1–D11 or name the change.
2. Accept or rewrite the resource numbers.
3. Approve or defer Experiment B (live namespace probe).
4. Confirm exclusive zone 0 rather than gSlapper's live -1.
5. Confirm v1.0.3 pin (no skull) versus waiting on a sysc-Go release.
