# sysc-terminal

A Wayland wallpaper engine that paints live [sysc-Go](https://github.com/Nomadcxx/sysc-Go)
terminal effects onto Niri’s background layer.

[sysc-shell](https://github.com/Nomadcxx/sysc-shell) selects it as a wallpaper
backend next to gSlapper. One process per output. Desktop input stays with
the compositor. Effects stay behind windows.

## What this is

- A dedicated Go binary (`sysc-terminal`) that maps a real
  `wlr-layer-shell` **Background** surface.
- A bounded ANSI cell grid plus font raster into `wl_shm`.
- The same supervision model as gSlapper: owned Unix socket, `query` /
  `pause` / `resume` / `change` / `stop`.

## What this is not

- Not [sysc-walls](https://github.com/Nomadcxx/sysc-walls) (Kitty screensaver).
- Not a terminal emulator, compositor, or lock screen.
- Not gSlapper and not GPL. Effects come from sysc-Go (MIT). Transport from
  [sysc-wayland](https://github.com/Nomadcxx/sysc-wayland) (BSD-3).

A live Background surface is invisible once `ext-session-lock-v1` blanks
normal clients. KindEffect lock backgrounds are a sysc-lock problem; see
`docs/plans/2026-10-03-lockscreen-parity-audit.md`.

## Status

Stage 3 started. Design and plan:

- `docs/plans/2026-10-03-terminal-wallpaper-feasibility-audit-report.md`
- `docs/plans/2026-10-03-terminal-wallpaper-engine-design.md`
- `docs/plans/2026-10-03-terminal-wallpaper-engine.md`

Commission: `sysc-912` in sysc-shell. Engine work lives in this repo.

## Pins

- Go 1.26
- `github.com/Nomadcxx/sysc-Go v1.0.3`
- `github.com/Nomadcxx/sysc-wayland v0.3.1`

Niri first. No CGO.

## Build

Once the module exists:

```bash
go generate ./internal/wayland/...
timeout 90s env GOMAXPROCS=2 go build -o /tmp/sysc-terminal ./cmd/sysc-terminal
```

Tests are named, one package:

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 <pkg> -run <Name>
```

Never `go test -race` or `go test ./...`.

## Licence

MIT.
