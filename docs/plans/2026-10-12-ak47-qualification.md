# AK-47 local qualification

2026-10-12. Implementation tracker: sysc-terminal-9.

The local sysc-Go branch `feat/ak47` contains commit `bb5a2e6`. The engine
branch of the same name adds its constructor, forwards measured cell aspect
through the existing optional-method pattern, and measures a complete cycle
in the existing frame-budget check.

## Evidence

The source FBX contains 17,397 triangles after triangulation. The offline Go
converter applies part transforms and emits 24,000 points with face normals.
Its 288,000-byte result has SHA-256
`d077b5c743c65e6af73960fee14502b1a50a83edc3e4ed1fe51ff5c0e13ebcc1`.
The binary and both READMEs preserve Lokeig's CC BY-NC 4.0 attribution.

Focused checks passed for source conversion, a known sampled triangle,
rotation direction, two full turns before morph, exact loop reconstruction,
render purity, elapsed-time equivalence, negative elapsed time, resize, reset,
bounded dimensions, TUI selection and host pause behaviour. Existing logo and
artwork lifecycle checks passed. CLI and engine candidate builds passed.
Read-only code review found a missing CLI help entry; the implementation adds it.

Full-loop headless pipeline, 3440x1440, 491x90 cells, JetBrains Mono Nerd Font
at 12 px, CGO disabled, GOMAXPROCS=2:

| Theme / blur | Frames | CPU mean | CPU p95 | CPU cap |
|---|---:|---:|---:|---:|
| Kanagawa / 0 | 240 | 6.233 ms | 9.879 ms | 12.5 ms |
| Dracula / 64 | 100 | 19.368 ms | 23.333 ms | 30 ms |

The headless maximum-blur soak used the same raster path, two output buffers
and a 120 ms interval. After 60 seconds of warmup, RSS was 131,972 KiB. After
600 further seconds, RSS was 131,032 KiB (127.96 MiB), a change of -940 KiB.
The observed samples remained below 150 MiB. A further 30-second paused run
made zero effect renders. This measures the headless pipeline; it does not
claim a live Wayland/compositor soak.

The local HTML player contains complete 12-second loops at 491x90/Kanagawa,
365x90/Dracula and 80x24/Kanagawa. It includes pause, a frame slider and credits.
The contact sheet and desktop PNG use the actual engine rasterizer. The HTML
uses browser font rendering, so dot coverage differs from the native raster.

## Local review and build

Engine worktree: `/home/nomadx/sysc-terminal/.worktrees/ak47`.
Library worktree: `/home/nomadx/.config/superpowers/worktrees/sysc-Go/ak47`.
Both retain the existing primary checkouts and their unrelated changes.

The ignored engine `go.work` selects both worktrees. The engine module pin
still names the published library. A standalone checkout needs the new library
published and pinned before it can build without this workspace. No local
`replace` directive was added to `go.mod`.

From the engine worktree:

```sh
timeout 90s env CGO_ENABLED=0 GOMAXPROCS=2 go build -o .tmp-go/sysc-terminal-ak47 ./cmd/sysc-terminal
.tmp-go/sysc-terminal-ak47 --list
```

Preview: `.tmp-go/ak47preview/preview.html`.
Native desktop still: `.tmp-go/ak47preview/desktop.png`.
The local preview generator is `.tmp-go/ak47preview/main.go`.

A terminal-only preview is available at `/tmp/syscgo-ak47`:

```sh
/tmp/syscgo-ak47 -effect ak-47 -theme kanagawa -duration 12
```

No installation, wallpaper assignment, compositor surface, remote branch or
pull request changed during this work. Live Wayland qualification remains a
release step. Publish the library first, then resolve the engine module pin to
that revision and repeat the focused integration check without `go.work`.

## Settings integration

Inspected sysc-shell's locally available `origin/main` at `564a7ad8` on
2026-10-13. Its settings and Terminal Art panels refresh the installed engine's
catalog on opening (`internal/shell/panelhost.go`). The effect cards use that
catalog directly (`internal/shell/popout_terminalart.go`).

The candidate's `--list` reports `effect ak-47 0`, which the shell's
`wallpaper.ParseList` accepts as an effect requiring no external artwork.
This shell revision needs no additional registry entry. After installing the
published engine, reopening Settings or Terminal Art should expose `ak-47`.
This is a source trace and candidate catalog check; installed integration has
not been exercised.
