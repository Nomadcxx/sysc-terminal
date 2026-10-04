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

## Install

Install Go 1.26 or newer, then:

```bash
GOBIN="$HOME/.local/bin" CGO_ENABLED=0 go install github.com/Nomadcxx/sysc-terminal/cmd/sysc-terminal@main
sysc-terminal --list
```

Keep `~/.local/bin` on the shell service's PATH. Restart sysc-shell after
installing so it can probe the effect catalog. In PR #96, open **Terminal Art**
from the Control Centre or Settings, choose an effect and palette, then apply
it to an output. Settings also stores the default palette for new selections.
The shell stops its gSlapper process when switching to terminal effects.

For a reproducible install, replace `main` with a published commit hash.

## Run

Niri must provide `WAYLAND_DISPLAY` and `XDG_RUNTIME_DIR`:

```bash
sysc-terminal --output eDP-1 --effect fire --theme nord \
  --ipc-socket "$XDG_RUNTIME_DIR/sysc-terminal.sock"
```

Use the connector name reported by `niri msg outputs`. Start one process per
output and give each a distinct socket. Stop any existing wallpaper on that
output first. `--list` reports supported effect IDs, themes and text effects;
text effects accept artwork through `--file`.

The control socket accepts one newline-terminated command per connection:
`query`, `pause`, `resume`, `change effect <id> theme <theme> [file <path>]`,
`reset`, `stop`.
The default ceiling is 20 FPS with a CPU budget of 25% of one core. Paused
instances retain their last frame without ticking the effect.

## Build and checks

The module pins sysc-Go at `v1.0.4-0.20261004042739-c80d48f1f38e` and
sysc-wayland at `v0.3.1`. Niri first; no CGO.

```bash
timeout 90s env CGO_ENABLED=0 GOMAXPROCS=2 go build -o /tmp/sysc-terminal ./cmd/sysc-terminal
```

Run named tests in one package:

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 <pkg> -run <Name>
```

Do not run race checks or full-tree tests on the laptop.

## Licence

MIT.
