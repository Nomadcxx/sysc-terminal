# Terminal wallpaper engine Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship Go Niri Background-layer engine playing sysc-Go effects behind windows, supervised by sysc-shell as first-class wallpaper backend.

**Architecture:** One `sysc-terminal` process per output owns layer-shell, bounded SGR cell grid, font raster. sysc-shell persists `kind=effect`, launches binary against owned socket. sysc-Go v1.0.3 only effect implementation.

**Tech Stack:** Go, sysc-wayland **v0.3.1** (generated layer-shell / fractional-scale / viewporter), sysc-Go v1.0.3, go-text/typesetting, Unix-socket IPC.

**Do not start until owner approves design and resource numbers.** Experiment B (live surface) also needs explicit yes.

Work in dedicated worktree of this repo, branch `feat/engine`, based on `origin/main`. Shell work = second worktree of sysc-shell based on `origin/main`. Never implement on dirty primary checkout.

Test command shape (machine dies on race / full-tree tests):

```bash
timeout 90s env GOMAXPROCS=2 go test -count=1 <pkg> -run <Name>
```

Every check in plan must be able to fail. Check passing against broken implementation worse than no check: buys confidence, not information. Where earlier revision had such check, replacement says what broke it.

Commit messages must pass `~/.git-hooks/commit-msg` (broad banned word list). Author stays `Nomadcxx <noovie@gmail.com>`. No trailers. Never `--no-verify`.

Tracking: per `AGENTS.md`, implementation issues live in this repo's bd database, created at Stage 3 start; plan progress not tracked in parallel markdown file. Task 7 also creates one issue in sysc-shell for `KindEffect` change. If run blocked, update `sysc-912` in sysc-shell, no new commission.

---

### Task 0: Module skeleton

**Files:**
- Create: `go.mod`, `go.sum`
- Create: `protocols/*.xml`
- Create: `internal/wayland/{layershell,xdgshell,fractionalscale,viewporter}/generate.go`
- Create: `cmd/sysc-terminal/main.go`
- Create: `internal/cell/doc.go` (package comment only)

**Step 1:** `go mod init github.com/Nomadcxx/sysc-terminal` **with `go 1.26`**, not 1.24. `github.com/Nomadcxx/sysc-wayland v0.3.1` declares `go 1.26`, sysc-shell declares `go 1.26.4`, lower go directive won't resolve require. Require `github.com/Nomadcxx/sysc-Go v1.0.3` and `github.com/Nomadcxx/sysc-wayland v0.3.1`.

**Step 2:** Copy protocol XML from sysc-shell `protocols/`: layer-shell, fractional-scale, viewporter, xdg-shell. **Carry provenance forward.** Each `generate.go` and each copied XML records upstream repo, revision, SHA-256 — layer-shell `wlr-protocols` commit `2b8d43325b7012cc3f9b55c08d26e50e42beac7d` / `87e0b9c837aecd6977f76f3c47d73088b7159871f5d979dc1840f6cadb5e2ed8` (version 5, what Niri 26.04 advertises); fractional-scale and viewporter from wayland-protocols 1.49 / `5941de5d…` and `dcb12279…`. Licence and traceability obligation, not decoration; makes XML copyable not mysterious.

**Step 3:** Add four `generate.go` files matching shell's generate lines, two corrections:

- `-xdg-shell-import` must point at **this repo's** `internal/wayland/xdgshell`, not `github.com/Nomadcxx/sysc-shell/internal/platform/wayland/xdgshell`. Importing shell internal path from sibling module not permitted; design forbids depending on shell.
- Pin scanner at `@v0.3.1`, same tag as module require. Shell pins `@v0.1.1` against v0.3.1 require; works but reads as accident.

Then `go generate ./internal/wayland/...`, confirm four generated binding packages exist.

**Step 4:** `cmd/sysc-terminal/main.go` prints usage including `-I` / `--ipc-socket`, exits 0 on `--help`. No Wayland connect yet. `--list` is Task 2 work, not this one; usage line reserves it.

**Step 5:**

```bash
timeout 90s env GOMAXPROCS=2 go build -o /tmp/sysc-terminal ./cmd/sysc-terminal
/tmp/sysc-terminal --help | grep -F -- '--ipc-socket'
go list -f '{{.Name}}' ./internal/wayland/...
```

Expected: help text contains `--ipc-socket`; `go list` prints `layershell`, `xdgshell`, `fractionalscale`, `viewporter`.

**Step 6:** Commit

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

**Step 1: Write failing test**

Previous revision asserted `g.At(0, 0).Ch == 0` fails. Cannot fail: measured against sysc-Go v1.0.3, fire's `Render()` writes all 24 rows and `fire.go:103` emits literal `" "` where `heat < 5`, so cell (0,0) is `' '` (0x20) — same assertion also passes against parser filling whole grid with spaces, i.e. rendering nothing. Actual frame content, measured at 80×24 / nord / 5 updates: row 0 stripped of SGR is `"       ▒  ▒                         "`, last row `"▓███████████████████"`. So check counts ink, and checks ink is coloured:

```go
func TestFireFrameFillsGrid(t *testing.T) {
	pal := animations.GetFirePalette("nord")
	fx := animations.NewFireEffect(80, 24, pal)
	for i := 0; i < 5; i++ {
		fx.Update()
	}
	g, err := Parse(fx.Render(), 80, 24)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if g.Cols != 80 || g.Rows != 24 {
		t.Fatalf("grid %dx%d", g.Cols, g.Rows)
	}
	// Ink, not "non-zero rune": a grid full of spaces must fail this.
	lit, coloured := 0, 0
	for y := 0; y < g.Rows; y++ {
		for x := 0; x < g.Cols; x++ {
			c := g.At(x, y)
			if c.Ch != ' ' && c.Ch != 0 {
				lit++
			}
			if c.Fg != DefaultFg {
				coloured++
			}
		}
	}
	if lit < 100 {
		t.Fatalf("only %d non-space cells in 1920; parser is dropping content", lit)
	}
	if coloured != lit {
		t.Fatalf("%d of %d lit cells carry no 38;2 colour; SGR is being lost", lit-coloured, lit)
	}
	// Fire uses unseeded math/rand. Exact glyph at (7,0) is not stable.
	// Row 0 is not blank at five updates, so anchoring cannot be assumed.
	row0lit := 0
	for x := 0; x < g.Cols; x++ {
		if c := g.At(x, 0); c.Ch != ' ' && c.Ch != 0 {
			row0lit++
		}
	}
	if row0lit == 0 {
		t.Fatal("row 0 is all spaces; fire is not bottom-anchored in the first frames")
	}
}

func TestRejectsOSC(t *testing.T) {
	g, err := Parse("A\x1b]52;c;AAAA\x07B", 2, 1)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if g.At(0, 0).Ch != 'A' || g.At(1, 0).Ch != 'B' {
		t.Fatalf("osc shifted cells: %+v %+v", g.At(0, 0), g.At(1, 0))
	}
}

func TestMalformedCSIDoesNotPanic(t *testing.T) {
	g, err := Parse("x\x1b[38;2;1y", 8, 1)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if g.At(0, 0).Ch != 'x' {
		t.Fatalf("col 0 = %q, want 'x'", g.At(0, 0).Ch)
	}
}
```

Row-0 ink check also documents fire not bottom-anchored in first frames, so cropping from either end changes picture.

**Step 2:** Run `timeout 90s env GOMAXPROCS=2 go test -count=1 ./internal/cell -run 'TestFireFrameFillsGrid|TestRejectsOSC|TestMalformedCSIDoesNotPanic'`
Expected: FAIL, `Parse` undefined.

**Step 3:** Implement `Grid`, `Cell{Ch rune; Fg, Bg color.RGBA}`, `Parse(s string, cols, rows int) (*Grid, error)`. Default fg white, bg transparent/black. Newlines advance row. Clip overflow. Skip OSC/DCS/APC (`\033]` `P` `_` `^`) until BEL or ST. SGR subset per design D6, hand-rolled scanner, no `x/ansi`.

**Step 4:** Re-run three tests. Expected: PASS.

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
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	e.Tick()
	g := e.Grid()
	if g.Cols != 80 || g.Rows != 24 {
		t.Fatalf("grid %dx%d", g.Cols, g.Rows)
	}
	if e.Generation() != 1 {
		t.Fatalf("generation %d, want 1", e.Generation())
	}
}

func TestUnknownEffectRejected(t *testing.T) {
	if _, err := New("not-an-effect", "nord", 80, 24, ""); err == nil {
		t.Fatal("unknown effect accepted")
	}
}

func TestUnknownThemeRejected(t *testing.T) {
	if _, err := New("fire", "not-a-theme", 80, 24, ""); err == nil {
		t.Fatal("unknown theme accepted")
	}
}

func TestTinySizeClamped(t *testing.T) {
	e, err := New("fireworks", "nord", 4, 4, "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	e.Tick() // must not panic: fireworks does rand.Intn(width-20)
	if w := e.EffectWidth(); w < 21 {
		t.Fatalf("constructed at width %d, floor is 21", w)
	}
}

func TestPauseDoesNotAdvance(t *testing.T) {
	e, err := New("fire", "nord", 80, 24, "")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	e.Tick()
	before := e.Generation()
	e.SetPaused(true)
	for i := 0; i < 5; i++ {
		e.Tick()
	}
	if got := e.Generation(); got != before {
		t.Fatalf("generation %d -> %d while paused", before, got)
	}
}
```

Previous revision compared single cell before/after pausing, flagged itself possibly flaky. Worse than flaky: sysc-Go uses `math/rand` with no seed API, cell can legitimately land on same value, so check can pass on wrapper that keeps ticking. `Generation()` increments only on tick that actually ran `Update`+`Render`+`Parse`, so paused wrapper that ticks caught immediately, deterministically.

**Step 2:** Run `-run 'TestFireConstructAndTick|TestUnknownEffectRejected|TestUnknownThemeRejected|TestTinySizeClamped|TestPauseDoesNotAdvance'` Expected: FAIL.

**Step 3:** `New` switches on v1.0.3 registry names, uses sysc-Go constructors, `GetFirePalette` / existing palette helpers from sysc-Go (do not copy walls' `getThemePalette` table). Each id binds to exactly one palette getter — binding is switch, not table. Clamp to 21×24 before construct. `Tick` calls Update+Render+Parse unless paused.

**Step 4:** Add `--list` (design D7), printing ids from `animations.GetEffectNames()`, requires-text flag from `animations.GetTextBasedEffects()`, themes from `animations.GetThemeNames()`, and `animations.GetLibraryVersion()`. Add check that every id `--list` prints is accepted by `New` and every id in v1.0.3 registry appears — only place two sides can drift, so checked not assumed. Note `GetThemeNames()` includes aliases (`catppuccin` *and* `catppuccin-mocha`), `GetThemeMetadata` resolves them; engine accepts either spelling, does not print them twice.

**Step 5:** Tests PASS.

**Step 6:** Commit `feat: wrap sysc-Go fire with pause, size floor, and list`

---

### Task 3: Raster a grid to ARGB bytes (no Wayland)

**Files:**
- Create: `internal/raster/raster.go`
- Test: `internal/raster/raster_test.go`

**Step 1:** Test 2×1 grid of `'X'` with known fg writes non-zero coverage into ARGB buffer of known stride, and `stride = width*4` / `total = stride*height*slots` rejects geometry whose byte size overflows int32. Overflow guard copied in shape from shell's `internal/platform/wayland/shm.go`, which already rejects `stride > math.MaxInt32 || total > math.MaxInt32`.

**Step 2:** Cache test, point of this task:

```go
func TestSecondFrameCostsNoMoreThanFirst(t *testing.T) {
	f := newFixture(t)          // real font, 8x16 cell
	g := f.solidGrid('▒', 100) // 100 distinct-ish cells
	first := f.timeN(func() { f.Draw(g) }, 5)
	second := f.timeN(func() { f.Draw(g) }, 5)
	if second > first*105/100 {
		t.Fatalf("cached frame %v slower than cold %v; glyph cache is not being hit", second, first)
	}
}
```

At 430×90 = 38 700 cells per frame at 20 FPS = **774 000 glyph rasterisations per second** with no cache. sysc-shell hit identical trap — per-frame glyph raster was 15% of bar repaint — and its `internal/render/text.go` keeps per-glyph `*image.Alpha` masks in `map[rasterKey]Mask` bounded at 256 with half-when-full eviction. Monospace font at D4's 12 px cell draws from few dozen codepoints, so per-(glyph, ppem) alpha cache, tinted at blit, turns steady state into blits. Design D4 makes this requirement; do not put colour in cache key. This is the check. Implementation requirements: bounded map with eviction (budget caps RSS), cache `*font.Font` not `*font.Face`, because font read-only and concurrent-safe, face not.

API notes for this typesetting version, obvious spellings do not compile: `font.Face.NominalGlyph(rune) (GID, bool)` (not `GlyphIndex`), `GlyphDataOutline(GID) (GlyphOutline, bool)`, `HorizontalAdvance(GID) float32`, `SetPpem(x, y uint16)`, `FontHExtents() (FontExtents, bool)` with **no `Upem` field** (`(*font.Font).Upem()` is method), no `LineMetric` constant named `LHMetric`. `shaping.Glyph.GlyphID`, `shaping.Bounds.Ascent/Descent/Gap`. `vector.Rasterizer.Draw` takes `*image.Uniform`.

**Step 3:** Load real system font in test. All three design paths exist on this machine (`JetBrainsMonoNerdFont-Regular.ttf` 2 571 596 B, `JetBrainsMono-Regular.ttf` 273 900 B, `NotoSansMono-Regular.ttf` 596 428 B), so read one directly. Previous revision said "skip if missing with `t.Skip`" — check that silently never runs, worse than no check. If font genuinely not found, fail with paths tried.

Draw at integer cell boxes. Background fill first.

**Step 4:** PASS.

**Step 5:** Commit `feat: raster cell grids into argb buffers with a glyph cache`

---

### Task 4: One Niri Background surface

**Files:**
- Create: `internal/wayland/owner.go` (dispatch loop, shm, layer surface)
- Create: `internal/wayland/surface.go`
- Test: `internal/wayland/surface_test.go` (protocol planning tests without compositor: exclusive zone 0, keyboard none, empty input, namespace)
- Modify: `cmd/sysc-terminal/main.go`

**Step 1:** Table check that layer-spec constants equal D3 contract — namespace `sysc-terminal`, layer Background (0), exclusive zone 0, keyboard none, all four anchors, size 0×0 — plus check grid geometry rejects int32-overflowing byte size.

Previous revision said "fake host records `SetInputRegion` empty, `SetExclusiveZone(0)`". That requires interface design never specifies, and single fake is speculative abstraction whose only purpose is making one check look thorough. Real protocol work happens at live gate (Experiment B / Task 9), where Niri itself is oracle and reports layer, namespace, keyboard interactivity. So: constants checked headlessly, behaviour checked live. If owner loop happens to take interface anyway, record calls there — do not build seam to host fake.

**Step 2:** FAIL.

**Step 3:** Bindings already generated in Task 0. Owner goroutine: registry bind compositor, shm, output, layer-shell, optional fractional-scale. Per chosen connector name, create surface per D3. Configure → allocate shm (double-buffer, wait for release) per shell's proven `internal/platform/wayland/shm.go` pattern: `unix.MemfdCreate(..., unix.MFD_CLOEXEC)`, `Ftruncate`, `Mmap(... MAP_SHARED)`, `shm.CreatePool`, one `CreateBuffer` per slot, whole generation retired as unit. Frame callback → if playing and ≥50 ms since last tick, raster and attach; else skip. Paused: do not request `Surface.Frame` at all. SIGTERM unmaps and exits 0.

**One clock.** No ticker. Frame callback only thing requesting frame; 50 ms floor lives in tick gate. Two clocks disagree, fast display then over-renders. If tick still in flight when callback arrives, drop it — same rule as capacity-1 channel, not second one.

**Step 4:** On compositor error or display disconnect, unmap, log one line, exit non-zero. Add check that error path returns non-nil error rather than looping, so shell's generation logic sees process gone and restarts it. Process surviving its display owns nothing and looks healthy from shell side: black wallpaper, "running" pill, no recovery.

**Step 5:** Unit checks PASS. **Do not connect to live compositor until Experiment B approved.**

**Step 6:** Commit `feat: map a click-through background layer surface`

**Experiment B (owner-gated):** run binary with `--effect fire --output DP-1` against spare socket **only after approval**, with cleanup in audit. Record `niri msg -j layers`. If owner declined B, skip live connect and continue; live gate is Task 9.

---

### Task 5: IPC

**Files:**
- Create: `internal/ipc/server.go`
- Test: `internal/ipc/server_test.go`

**Step 1:** Tests: `query` before ready fails until server sets ready; `pause`/`resume` idempotent; `change effect rain theme dracula` updates; `change effect nope` returns ERR; line > 4KiB closes; `file` with newline rejected.

**Step 2:** FAIL.

**Step 3:** Listen on `-I`. Same-uid only (`SO_PEERCRED`). Implement D7 verbs. `stop` cancels owner.

**Step 4:** PASS.

**Step 5:** Commit `feat: add the local control socket`

---

### Task 6: Text effect, palette, artwork, resize

**Files:**
- Modify: `internal/effect/effect.go`
- Test: `internal/effect/text_test.go`

**Step 1:** `fire-text` with artwork `"SYSC"` then `SetText("ZZZZ")` changes checksum of grid after some ticks. Missing artwork file is ERR, not panic. Configure resize from 80×24 to 120×32 calls concrete Resize or reconstructs.

**Step 2:** FAIL.

**Step 3:** Minimal implementation. Path allowlist copied as function, not by importing walls.

**Step 4:** PASS.

**Step 5:** Commit `feat: support text effects, themes, artwork, and resize`

Then add table tests for remaining v1.0.3 ids constructing + one Tick. One commit `test: cover the v1.0.3 effect registry`.

---

### Task 7: Shell KindEffect (sysc-shell worktree)

**Files (sysc-shell):**
- Modify: `internal/wallpaper/media.go` (add `KindEffect`)
- Modify: `internal/wallpaper/persist.go`, `assign.go`, `service.go`, `engine.go`
- Create: `internal/wallpaper/terminal.go` (launch argv, probe via `sysc-terminal --list`)
- Modify: `internal/shell/popout_wallpaper.go` (Effects source, pill, `wallpaperOurNamespace`)
- Tests beside each

**Step 1:** Persist round-trip of effect assignment without media path. `EngineFor(KindEffect)` is `sysc-terminal` iff probe passes. Apply does not `os.Stat` empty path. `SetPaused` works for KindEffect. `wallpaperOurNamespace` includes `sysc-terminal`. Apply stops owned gSlapper on that connector (existing retire path). Fake process in engine tests.

**Step 2:** FAIL on persist of unknown kind `effect`.

**Step 3:** Implement D8/D9, two shell-side details verified against `origin/main`:

- `EngineFor` body today is `if c.GSlapper { return EngineGSlapper }; if
  kind == KindVideo { return "" }; return c.Static()`. Effect branch goes **before `c.GSlapper` early return**, or gSlapper claims `KindEffect` on any machine having it installed. File calls current body "the one statement of that policy", so effect case lives there and nowhere else. Needs new `Capabilities` field for "terminal engine installed".
- Probe with `exec.LookPath("sysc-terminal")` plus successful `sysc-terminal --list`, not `--help | grep -F -- '--ipc-socket'`. Sentinel line cheaper, survives flag rename, and same output already feeds picker's effect/theme list (D7/D9), so registry never transcribed into shell.
- `wallpaperOurNamespace` (`internal/shell/popout_wallpaper.go:1468`) gets `sysc-terminal` in explicit `switch`. Must not ride `strings.HasPrefix(namespace, "sysc-shell")` branch, deliberately narrow so anything else on Background stays foreign.

Launch:

```text
sysc-terminal -I <socket> --output <connector> --effect <id> --theme <theme> [--file <path>]
```

Ready = `query` succeeds, same as gSlapper. Stop = `stop` then wait for socket vanish, then pid recorded in runtime only if our socket in argv.

**Step 4:** Package tests PASS. Do not deploy.

**Step 5:** Commit on shell branch `feat: persist and supervise effect wallpaper assignments`

Cross-repo gate: shell PR waits on sysc-terminal binary answering `query`. Record as bd issue in sysc-shell depending on tagged sysc-terminal release.

---

### Task 8: Pause proof and the real headless budget gate

**Files:**
- Modify: engine pause path
- Test: `internal/effect/effect_test.go` already has pause
- Create: `internal/pipeline/budget_test.go`

**Step 1:** Assert paused `Tick` does not call `Render` (wrapper counter), and owner requests no `Surface.Frame` while paused. Design's gate is "0 Update/Render, CPU ≈ idle"; asserting only in effect wrapper leaves owner loop free to keep ticking paused effect. Check both levels.

**Step 2 — budget gate, at real size.** Previous revision's optional check ticked 50 frames at 80×24, failed if mean > 20 ms, then said "live numbers are the real gate". Both halves wrong:

- 80×24 is 1 920 cells vs 38 700 at wallpaper size, 20× less work; and
- measured `Update`+`Render` p95 at 430×90 is **8.43 ms**, so 20 ms budget at 80×24 passes with roughly 20× headroom however broken real path is. Exercises no raster at all.

So gate is headless, at real cell count, through whole pipeline:

```go
func TestWallpaperFrameBudget(t *testing.T) {
	// Grid from D4 against the chosen font at pixelSize 12 on 3440×1440,
	// not a hardcoded 8×16 cell. 430×90 was the audit's 8×16 measurement
	// of Update+Render only; the live grid is advance(M)×lineHeight.
	// Update -> Render -> parse -> raster, no compositor, real system font.
	// Fail on mean and on p95 against the owner's accepted numbers.
	// Report every number even when passing, so the gate leaves a record.
}
```

`Update`+`Render` p95 8.43 ms is known floor; owner's remaining budget is difference. Run it, write actual numbers into handover — first measurement of raster half, which audit could not obtain and which is single largest unknown in design. If it misses, fixes in order: skip-rate, then smaller font, then dirty-rect upload. Not new renderer architecture, not longer soak.

**Step 3–4:** TDD as usual. Check must print measurements whether passes or fails; gate nobody can inspect afterward is not evidence.

**Step 5:** Commit `fix: keep paused effects from doing frame work` and `test: gate the frame budget at wallpaper cell count`

---

### Task 9: Live qualification (desktop)

Requires owner approval to touch live wallpaper.

Record:

1. `niri msg -j layers` before
2. Apply fire on DP-1 through panel (or argv if shell not merged)
3. Layers shows `sysc-terminal` Background keyboard None
4. Click desktop; niri keybind still works
5. Pause: CPU drops; `query` says paused; CPU sample over 30 s while paused at idle with `Update`/`Render` count unchanged
6. Change to `rain`; visual change (capture two frames)
7. Stop/restore still wallpaper via panel
8. Layers shows `slapper` again (or awww)
9. `ps` RSS/CPU vs design budget
10. **10-minute soak**: leave playing, sample RSS at 0/1/5/10 min, record delta against < 10 MiB cap. Budget names this gate; previous plan revision had no task for it, so would have shipped unmeasured.
11. **Clean shutdown**: IPC `stop` exits cleanly. Not compositor-loss. Display-loss (`Display.SetErrorHandler` / read-error → non-zero exit) is unit check on error callback, or live compositor-kill owner separately allows. Do not label `stop` as display-loss.
12. Restore `Sun-Setting-Horizon.png` if that is still the owner still

Laptop gate separate bd issue (`sysc-terminal` laptop live). Two-output separate unrun gate.

Write `docs/plans/2026-10-03-terminal-wallpaper-engine-completion-handover.md` with hashes, commands, captures, Task 8 budget numbers, soak RSS series, restore steps. Do not claim main/release/installed unless true.

---

### Checkpoints

| After | Review |
|---|---|
| Task 2 | Headless fire grid looks right in dumped PPM optional; `--list` matches registry |
| Task 3 | Glyph cache actually hit (second frame ≈ first) |
| Task 4 | Experiment B / layer spec constants; disconnect exits non-zero |
| Task 6 | Registry table |
| Task 7 | Shell persist + supervise tests; `EngineFor` branch order |
| Task 8 | **Headless 430×90 budget numbers recorded — first raster measurement** |
| Task 9 | Live budget + soak RSS series + restore |

Upstream gates: sysc-Go > v1.0.3 for skull; shell KindEffect merge; `sysc-911` must not add conflicting wallpaper-effect store. sysc-wayland **not** gate: v0.3.1 pinned because shell `origin/main` pins it and because v0.2.2's `RegisterWithID` panics when server reuses proxy ID.

If blocked: update `sysc-912` with branch, worktree, open gate. Do not write completion snapshot for incomplete work.