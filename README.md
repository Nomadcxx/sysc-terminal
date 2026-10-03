# sysc-terminal

Animated terminal-effect wallpapers for Niri.

The engine maps a real wlr-layer-shell **Background** surface per output,
renders [sysc-Go](https://github.com/Nomadcxx/sysc-Go) effects into that
surface, and leaves desktop input alone. [sysc-shell](https://github.com/Nomadcxx/sysc-shell)
selects it as a wallpaper backend next to gSlapper.

This is not [sysc-walls](https://github.com/Nomadcxx/sysc-walls). sysc-walls is
an idle screensaver that opens fullscreen Kitty. This engine stays behind
windows.

## Status

Planning. Implementation waits on owner approval of:

- `docs/plans/2026-10-03-terminal-wallpaper-feasibility-audit-report.md`
- `docs/plans/2026-10-03-terminal-wallpaper-engine-design.md`
- `docs/plans/2026-10-03-terminal-wallpaper-engine.md`

Commission: `sysc-912`.

## Licence

MIT. Effects come from sysc-Go (MIT). Wayland transport from sysc-wayland
(BSD-3). This repository does not link gSlapper or sysc-walls.
