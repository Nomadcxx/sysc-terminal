# Terminal wallpaper engine Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship a Go Niri Background-layer engine that plays sysc-Go effects behind windows, supervised by sysc-shell as a first-class wallpaper backend.

**Architecture:** `sysc-terminal` process per output owns layer-shell, a bounded SGR cell grid, and font raster. sysc-shell persists `kind=effect` and launches the binary against an owned socket. sysc-Go v1.0.3 is the only effect implementation.

**Tech Stack:** Go, sysc-wayland v0.2.2 (generated layer-shell / fractional-scale / viewporter), sysc-Go v1.0.3, go-text/typesetting, Unix-socket IPC.

**Do not start this plan until the owner approves the design and the resource numbers.** Experiment B (live surface) also needs an explicit yes.

Work from a dedicated worktree of this repository, branch `feat/engine`, based on `origin/main`. Shell work is a second worktree of sysc-shell based on `origin/main`. Never implement on a dirty primary checkout.

Test command shape (machine dies on race / full-tree tests):

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 <pkg> -run <Name>
```

Commit messages must pass `~/.git-hooks/commit-msg` (broad banned word list). Author remains `Nomadcxx <noovie@gmail.com>`. No trailers. Never `--no-verify`.

---

### Task 0: Module skeleton

**Files:**
- Create: `go.mod`, `go.sum`
- Create: `cmd/sysc-terminal/main.go`
- Create: `internal/cell/doc.go` (package comment only)

**Step 1:** `go mod init github.com/Nomadcxx/sysc-terminal` with Go 1.24. Require `github.com/Nomadcxx/sysc-Go v1.0.3` and `github.com/Nomadcxx/sysc-wayland v0.2.2`. Copy protocol XML from sysc-shell `protocols/` (layer-shell, fractional-scale, viewporter, xdg-shell as the layer-shell import). Add `internal/wayland/{layershell,xdgshell,fractionalscale,viewporter}/generate.go` matching shell's generate lines, then `go generate ./internal/wayland/...`.

**Step 2:** `cmd/sysc-terminal/main.go` prints usage including `-I` / `--ipc-socket` and exits 0 on `--help`. No Wayland connect yet.

**Step 3:**

```bash
timeout 90s env GOMAXPROCS=2 go build -o /tmp/sysc-terminal ./cmd/sysc-terminal
/tmp/sysc-terminal --help | grep -F -- '--ipc-socket'
```

Expected: help text contains `--ipc-socket`.

**Step 4:** Commit

```bash
git add go.mod go.sum cmd/sysc-terminal/main.go internal/wayland protocols internal/cell/doc.go
git commit -m "$(cat <<'EOF'
build: add module, protocol generate, and help command

EOF
)"
```

---

### Task 1: Bounded SGR cell grid

**Files:**
- Create: `internal/cell/grid.go`
- Create: `internal/cell/parse.go`
- Test: `internal/cell/parse_test.go`

**Step 1: Write the failing test**

```go
func TestFireFrameFillsGrid(t *testing.T) {
    pal := animations.GetFirePalette("nord")
    fx := animations.NewFireEffect(80, 24, pal)
    for i := 0; i < 5; i++ {
        fx.Update()
    }
    g, err := Parse(fx.Render(), 80, 24)
    if err != nil {
        t.fatal...
    }
    if g.Cols != 80 || g.Rows != 24 {
        t.Fatalf("grid %dx%d", g.Cols, g.Rows)
    }
    if g.At(0, 0).Ch == 0 {
        t.Fatal("empty cell rune")
    }
}

func TestRejectsOSC(t *testing.T) {
    g, err := Parse("A\x1b]52;c;AAAA\x07B", 2, 1)
    if err != nil {
        t.fatal...
    }
    if g.At(0, 0).Ch != 'A' || g.At(1, 0).Ch != 'B' {
        t.Fatalf("osc shifted cells: %+v %+v", g.At(0, 0), g.At(1, 0))
    }
}

func TestMalformedCSIDoesNotPanic(t *testing.T) {
    _, err := Parse("x\x1b[38;2;1y", 8, 1)
    if err != nil {
        t.fatal...
    }
}
```

**Step 2:** Run `timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/cell -run 'TestFireFrameFillsGrid|TestRejectsOSC|TestMalformedCSIDoesNotPanic'`
Expected: FAIL, `Parse` undefined.

**Step 3:** Implement `Grid`, `Cell{Ch rune; Fg, Bg color.RGBA}`, `Parse(s string, cols, rows int) (*Grid, error)`. Default fg white, bg transparent/black. Newlines advance row. Clip overflow. Skip OSC/DCS/APC (`\033]` `P` `_` `^`) until BEL or ST. SGR subset per design D6.

**Step 4:** Re-run the three tests. Expected: PASS.

**Step 5:** Commit `feat: parse effect frames into a bounded cell grid`

---

### Task 2: Effect wrapper and fire without compositor

**Files:**
- Create: `internal/effect/effect.go`
- Test: `internal/effect/effect_test.go`

**Step 1: Failing test**

```go
func TestFireConstructAndTick(t *testing.T) {
    e, err := New("fire", "nord", 80, 24, "")
    if err != nil { t.fatal }
    e.Tick()
    g := e.Grid()
    if g.Cols != 80 || g.Rows != 24 { t.fatal }
}

func TestUnknownEffectRejected(t *testing.T) {
    _, err := New("not-an-effect", "nord", 80, 24, "")
    if err == nil { t.fatal }
}

func TestTinySizeClamped(t *testing.T) {
    e, err := New("fireworks", "nord", 4, 4, "")
    if err != nil { t.fatal }
    e.Tick() // must not panic
}

func TestPauseDoesNotAdvance(t *testing.T) {
    e, _ := New("fire", "nord", 80, 24, "")
    e.Tick()
    a := e.Grid().At(40, 20)
    e.SetPaused(true)
    e.Tick()
    b := e.Grid().At(40, 20)
    if a != b { t.Fatal("paused tick advanced") }
}
```

(`At` comparison may be flaky on fire; compare a hash of the whole grid or a generation counter instead of one cell.)

Prefer: `e.generation` increments only when not paused.

**Step 2:** Run `-run 'TestFireConstructAndTick|TestUnknownEffectRejected|TestTinySizeClamped|TestPauseDoesNotAdvance'` Expected: FAIL.

**Step 3:** `New` switches on v1.0.3 registry names, uses sysc-Go constructors, `GetFirePalette` / existing palette helpers from sysc-Go (do not copy walls' `getThemePalette` table). Clamp to 21×24 before construct. `Tick` calls Update+Render+Parse unless paused.

**Step 4:** Tests PASS.

**Step 5:** Commit `feat: wrap sysc-Go fire with pause and size floor`

---

### Task 3: Raster a grid to ARGB bytes (no Wayland)

**Files:**
- Create: `internal/raster/raster.go`
- Test: `internal/raster/raster_test.go`

**Step 1:** Test that an 2×1 grid of `'X'` with known fg writes non-zero coverage into an ARGB buffer of known stride, and that `cols*cellW` / `rows*cellH` matching the buffer does not overflow.

**Step 2:** FAIL.

**Step 3:** Load a test font from `testdata/` (a small subset or the system path if present; skip if missing with `t.Skip`, but add a committed `testdata` TTF only if licence allows — prefer reading a system font in the test and Skip if absent). Draw at integer cell boxes. Background fill first.

**Step 4:** PASS.

**Step 5:** Commit `feat: raster cell grids into argb buffers`

---

### Task 4: One Niri Background surface

**Files:**
- Create: `internal/wayland/owner.go` (dispatch loop, shm, layer surface)
- Create: `internal/wayland/surface.go`
- Test: `internal/wayland/surface_test.go` (protocol planning tests without a compositor: exclusive zone 0, keyboard none, empty input, namespace)
- Modify: `cmd/sysc-terminal/main.go`

**Step 1:** Table test that the layer spec constants match D3 (namespace `sysc-terminal`, exclusive 0, keyboard none). A fake host records `SetInputRegion` empty, `SetExclusiveZone(0)`.

**Step 2:** FAIL.

**Step 3:** Generate bindings already in Task 0. Owner goroutine: registry bind compositor, shm, output, layer-shell, optional fractional-scale. Per chosen connector name, create surface as D3. Configure → allocate shm (double-buffer, wait for release). Frame callback → if playing and a tick is due, raster and attach; else skip. SIGTERM unmaps and exits 0.

**Step 4:** Unit tests PASS. **Do not connect to the live compositor until Experiment B is approved.**

**Step 5:** Commit `feat: map a click-through background layer surface`

**Experiment B (owner-gated):** run the binary with `--effect fire --output DP-1` against a spare socket **only after approval**, with the cleanup in the audit. Record `niri msg -j layers`. If the owner declined B, skip live connect and continue; the live gate is Task 9.

---

### Task 5: IPC

**Files:**
- Create: `internal/ipc/server.go`
- Test: `internal/ipc/server_test.go`

**Step 1:** Tests: `query` before ready fails until the server sets ready; `pause`/`resume` idempotent; `change effect rain theme dracula` updates; `change effect nope` returns ERR; line > 4KiB closes; `file` with newline rejected.

**Step 2:** FAIL.

**Step 3:** Listen on `-I`. Same-uid only (`SO_PEERCRED`). Implement D7 verbs. `stop` cancels the owner.

**Step 4:** PASS.

**Step 5:** Commit `feat: add the local control socket`

---

### Task 6: Text effect, palette, artwork, resize

**Files:**
- Modify: `internal/effect/effect.go`
- Test: `internal/effect/text_test.go`

**Step 1:** `fire-text` with artwork `"SYSC"` then `SetText("ZZZZ")` changes a checksum of the grid after some ticks. Missing artwork file is ERR, not a panic. Configure resize from 80×24 to 120×32 calls concrete Resize or reconstructs.

**Step 2:** FAIL.

**Step 3:** Minimal implementation. Path allowlist copied as a function, not by importing walls.

**Step 4:** PASS.

**Step 5:** Commit `feat: support text effects, themes, artwork, and resize`

Then add table tests for the remaining v1.0.3 ids constructing + one Tick. One commit `test: cover the v1.0.3 effect registry`.

---

### Task 7: Shell KindEffect (sysc-shell worktree)

**Files (sysc-shell):**
- Modify: `internal/wallpaper/media.go` (add `KindEffect`)
- Modify: `internal/wallpaper/persist.go`, `assign.go`, `service.go`, `engine.go`
- Create: `internal/wallpaper/terminal.go` (launch argv, probe `--help` for `-I`)
- Modify: `internal/shell/popout_wallpaper.go` (Effects source, pill, `wallpaperOurNamespace`)
- Tests beside each

**Step 1:** Persist round-trip of an effect assignment without a media path. `EngineFor(KindEffect)` is `sysc-terminal` iff probe passes. Apply does not `os.Stat` an empty path. `SetPaused` works for KindEffect. `wallpaperOurNamespace` includes `sysc-terminal`. Apply stops owned gSlapper on that connector (existing retire path). Fake process in engine tests.

**Step 2:** FAIL on persist of unknown kind `effect`.

**Step 3:** Implement D8/D9. Launch:

```text
sysc-terminal -I <socket> --output <connector> --effect <id> --theme <theme> [--file <path>]
```

Ready = `query` succeeds, same as gSlapper. Stop = `stop` then wait for socket vanish, then pid recorded in runtime only if our socket is in argv.

**Step 4:** Package tests PASS. Do not deploy.

**Step 5:** Commit on the shell branch `feat: persist and supervise effect wallpaper assignments`

Cross-repo gate: shell PR waits on a sysc-terminal binary that answers `query`. Record as a bd issue in sysc-shell depending on a tagged sysc-terminal release.

---

### Task 8: Pause, budgets, soak helpers

**Files:**
- Modify: engine pause path
- Test: `internal/effect/effect_test.go` already has pause
- Create: `internal/raster/budget_test.go` optional microbench behind `-run TestFireFrameBudget` that ticks 50 frames at 80×24 and fails if mean > 20 ms (loose; live numbers are the real gate)

**Step 1:** Assert paused Tick does not call Render (wrapper counter).

**Step 2–4:** TDD as usual.

**Step 5:** Commit `fix: keep paused effects from doing frame work`

---

### Task 9: Live qualification (desktop)

Requires owner approval to touch the live wallpaper.

Record:

1. `niri msg -j layers` before
2. Apply fire on DP-1 through the panel (or argv if shell is not merged)
3. Layers shows `sysc-terminal` Background keyboard None
4. Click desktop; niri keybind still works
5. Pause: CPU drops; `query` says paused
6. Change to `rain`; visual change (capture two frames)
7. Stop/restore still wallpaper via the panel
8. Layers shows `slapper` again (or awww)
9. `ps` RSS/CPU vs the design budget
10. Restore `Sun-Setting-Horizon.png` if that is still the owner still

Laptop gate is a separate bd issue (`sysc-terminal` laptop live). Two-output is a separate unrun gate.

Write `docs/plans/2026-10-03-terminal-wallpaper-engine-completion-handover.md` with hashes, commands, captures, restore steps. Do not claim main/release/installed unless that is true.

---

### Checkpoints

| After | Review |
|---|---|
| Task 2 | Headless fire grid looks right in a dumped PPM optional |
| Task 4 | Experiment B / layer spec tests |
| Task 6 | Registry table |
| Task 7 | Shell persist + supervise tests |
| Task 9 | Live budget + restore |

Upstream gates: sysc-Go > v1.0.3 for skull; sysc-wayland v0.3.1 optional; sysc-shell KindEffect merge; sysc-911 must not add a conflicting wallpaper-effect store.

If blocked: update `sysc-912` with branch, worktree, and the open gate. Do not write a completion snapshot for incomplete work.
