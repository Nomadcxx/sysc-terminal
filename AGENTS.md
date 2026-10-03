# sysc-terminal

Go wallpaper engine that paints live sysc-Go terminal effects onto a Niri
wlr-layer-shell Background surface. sysc-shell selects and supervises it the
same way it supervises gSlapper. This is not a screensaver, a lock screen, a
terminal emulator, or a compositor.

Tracker for the commission: `sysc-912` in `/home/nomadx/sysc-shell`. After the
owner approves the design, implementation issues live in this repository's bd
database.

## Golden rule

No commit that names a coding tool, and no commit authored or committed by one,
is ever pushed. That includes trailers, generated-by lines, and tool emails.

The machine `commit-msg` hook (`~/.git-hooks/commit-msg`) rejects messages that
match a broad word list (`cursor`, `agent`, `codex`, `llm`, `bot`, …). Screen
the message. `scripts/githooks/pre-push` plus the same global hook refuse a
push whose author, committer, or message matches those patterns. Never
`--no-verify`.

Author and committer must remain `Nomadcxx <noovie@gmail.com>`.

## Product constraints

- Go only. No C, CGO, C++, Rust, Lua, Qt, or GStreamer. gSlapper's C is
  evidence for layer-shell behaviour, not permission to add those languages.
- Niri first. Other compositors need a separate approved design.
- Protocol types stay in this repository's Wayland boundary. One goroutine owns
  the connection and every proxy.
- One authoritative effect implementation: import `github.com/Nomadcxx/sysc-Go`.
  Do not copy registries or palettes.
- Do not host Kitty or any xdg-toplevel as a wallpaper. Window rules do not
  make a layer surface.
- Do not import sysc-walls (GPL-3, screensaver, CGO idle). Do not import
  sysc-shell internals.
- Pin `github.com/Nomadcxx/sysc-wayland` and generate layer-shell here, the
  same shape as sysc-shell. Do not vendor dankgo.
- Do not `pkill` by binary name. Stop only processes this engine or the shell
  started, via the owned socket then the recorded pid.
- Preserve the owner's current wallpaper backend while testing. Restore
  `origin/main` gSlapper assignment after a live experiment.

## Engineering

- Stop at the first working rung: existing code, Go standard library, native
  Linux, pinned dependency, then new code.
- Draw after invalidation. Use frame callbacks and buffer release. Do not tick
  effect `Update`/`Render` on a timer while paused.
- Bound request size and time on the control socket. Validate effect IDs and
  artwork paths. No arbitrary command strings.
- One focused runnable check for non-trivial logic. Named tests, one package:
  `timeout 90s env GOMAXPROCS=2 go test -count=1 <pkg> -run <Name>`
- Never `go test -race`, `go test ./...`, or `go test -count=N` with N > 1.

## Workflow

1. Read `docs/plans/2026-10-03-terminal-wallpaper-engine-design.md` and the
   implementation plan. Stage 3 does not start without owner approval of the
   architecture, scope, and numeric resource budget.
2. Work in a dedicated worktree off `origin/main`.
3. Write the failing check before the code.
4. Keep Wayland dispatch on the owner goroutine.
5. Installed shell changes use sysc-shell `scripts/deploy`. This binary
   installs by the procedure in the plan, not by copying over the owner's
   wallpaper process.

## Tracking

bd in this repository is for engine implementation issues. The commission and
its design/plan documents are attached to `sysc-912` in sysc-shell. Do not
keep a parallel markdown TODO.

## Documents

Designs and plans live in `docs/plans/` of this repository. Copies are also
written under `/home/nomadx/sysc-shell/docs/plans/` (gitignored there) so the
shell register and `sysc-912` keep absolute paths.
