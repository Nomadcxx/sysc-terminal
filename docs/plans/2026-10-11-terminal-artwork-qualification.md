# Terminal artwork qualification

## Delivered code

sysc-Go PR [#138](https://github.com/Nomadcxx/sysc-Go/pull/138) merged as `23c52e60dd7eb3994f94a38675fd85a18b49236c`. Engine PR [#22](https://github.com/Nomadcxx/sysc-terminal/pull/22) pins `v1.0.6-0.20261011112227-23c52e60dd7e`, without a replace directive. Shell already admits standalone registry entries; its existing catalog checks passed, so this commission requires no shell source change.

The library embeds printable ASCII cells with bounded glyph/colour roles and delays. Great Wave uses the Met public-domain Hokusai scan (object 45434); the supplied GIF adaptations retain all 12/160 frames, offsets, transparency, disposal and Valenberg.com credit. The source hashes and reproduction command live in the library's `animations/artwork/README.md`. Raw references remain local.

The engine passes measured character-cell aspect through resize, changes and reset. Render reads the current frame; Update advances nominal 50 ms. Blur's 120 ms cadence stretches playback by 2.4×. Pause freezes frames and stops effect work. A stable foreground for spaces avoids redrawing invisible cells during visor flashes; the initial full-cycle girl gate failed at 13.731 ms before this correction.

## Local and CI checks

All commands used CGO_ENABLED=0 and GOMAXPROCS=2. Named tests used `timeout 90s`, `-count=1`, one package and a focused `-run` expression.

- Library: animations/TestArtwork; baker/TestArtworkGIFBounds and TestArtworkGIFDisposal; TUI/TestArtworkSelector and TestAnimationNeedsFile; CLI/TestArtworkCLI. Vet of these touched packages passed.
- Engine: effect/TestArtworkConstructTickResizePause, TestArtworkBlankCellsKeepStyle and TestList; wayland/TestArtworkWorkerRetainsCellAspect and TestWorker. Touched effect/wayland/pipeline vet and engine build passed against the final upstream pin.
- Shell: wallpaper/TestParseList and TestTerminalBlurCapability passed.
- Fresh local review found and resolved the baker's DecodeAll allocation bound: a GIF block preflight rejects more than 160 frames before decoding. Final review found no remaining code blockers.
- Library PR CI passed in run `38135205557`; engine PR CI passed in run `38135505871`. CI runs the repository's broader suite; local engine checks stayed within its named-package restrictions.

## CPU budget

Final-pin measurements ran sequentially at 3440×1440 with JetBrains Mono Nerd Font, 12 px, 7×16 cells and a 491×90 grid, Kanagawa. Each sample covers a complete source cycle: 120/29/288 updates. CPU mean/p95 and wall gates passed in all six runs. CPU p95 caps are 12.5 ms at blur 0 and 30 ms at blur 64.

| Scene | CPU p95, blur 0 | CPU p95, blur 64 |
|---|---:|---:|
| great-wave | 4.98ms | 16.904ms |
| cyberpunk-girl | 6.975ms | 19.871ms |
| wasteland | 5.954ms | 16.889ms |

Logs and images live under `/home/nomadx/sysc-shell/docs/plans/assets/2026-10-11-terminal-artwork/`.

## Appearance and live hardware

Previews include first frames at 80×24 and 491×90 cells plus complete loops in Kanagawa and Dracula (`previews/`). Live captures cover all three scenes at blur 0 and 64 (`live-warm/`). Unblurred captures retain the wave crest, portrait/visor, and wasteland sun/figure/angular foreground. Small-grid detail is limited by ASCII resolution. Maximum blur obscures the ASCII detail and dims the scene; it is unsuitable when recognizability is the priority.

Niri reports DP-1 at 3440×1440/120 Hz and DP-3 at 2560×1440/165 Hz, both scale 1. Test surfaces map on Background with keyboard interactivity None. Tests pause captured shell-owned engines, use dedicated sockets, stop only recorded test processes, then resume owners. They preserve assignments and configuration.

Cold soak (600 s from ready): girl blur 0 on DP-1 reached 78508 KiB RSS (+8580 KiB); wasteland blur 64 on DP-3 reached 81052 KiB (+27316 KiB). Both pass the 150 MiB absolute cap. **Wasteland fails the 10 MiB cold-start growth cap.** During the 60–600 s interval it grew 5264 KiB, suggesting a plateau, without proving the cause. Both paused engines measured zero CPU ticks over 30 s and resumed; the script cleaned test processes and resumed both owners.

A separate diagnostic process with Go GC tracing keeps live heap at 33 MiB after collection, with a 66–67 MiB heap goal; its resident memory increases while the heap fills between collections (`memory-diagnostic/gctrace.log`). This supports runtime heap expansion as a cause of startup growth, without proving an allocation profile.

A separate full ten-minute interval after 60 s warmup allows at least one complete 34.56 s wasteland loop before its baseline. All three pass the steady-state absolute/growth caps:

| Scene/cadence | Warm baseline RSS, KiB | At +600 s, KiB | Growth, KiB |
|---|---:|---:|---:|
| great-wave, blur 0 | 82692 | 82700 | 8 |
| cyberpunk-girl, blur 0 | 80700 | 80892 | 192 |
| wasteland, blur 64 | 82740 | 82772 | 32 |

The measured average active CPU was about 4.4%, 9.4% and 12.2% of one core, respectively. This run used the reviewed runtime source through a temporary local-library modfile; final-pin named checks/build and six CPU gates independently passed. The merged library's runtime artwork source matches the local source. The GC diagnostic used the final upstream-pin binary.

The warmed run also measured zero CPU ticks for each of the three paused engines over 30 s, resumed them, stopped every test process and resumed both owners. Final socket queries report sysc-logo/material playing on DP-1 and DP-3. Saved assignments and the shell config hash match the captured snapshots. Another session deployed shell revision `520a855591c9aa6f71f1fd9cbff452d839b099f5` at 11:25:58 UTC during the soak; this commission did not deploy or roll it back. The restored owner sockets still report the captured effect and theme.

**Release approval:** On 2026-10-11 the owner reviewed these measurements and requested merge, rebuild from origin/main and deployment. Shipment proceeds with the recorded cold-start growth exception. The warmed absolute/growth gates pass; the original cold-start failure remains visible for future memory work.

## Qualification limits

The implementation bounds artwork output to 512×256 cells. Wider grids receive parser padding, so the scene centers within the capped artwork area. The actual 491×90 and 365×90 grids are unaffected. Laptop/4K hardware and fractional-scale observations require separate evidence. Wallpaper pixel blur does not establish compositor frosting; sysc-terminal-1 retains that investigation.

## Deployment

Engine PR #22 merged with local commit `32a6e204d0cd57531520f151e75d377828efd683`; author and committer are Nomadcxx <noovie@gmail.com>. Release `d4f7ba687210fdbe7fd872a884b2de129b87297e` reached origin/main. Named effect/worker checks and the build passed on the merge; main CI passed in run `38139374504`.

Installed with `CGO_ENABLED=0 GOMAXPROCS=2 GOBIN=/home/nomadx/.local/bin go install github.com/Nomadcxx/sysc-terminal/cmd/sysc-terminal@main`. Go build metadata identifies engine `v0.1.1-0.20261011122800-d4f7ba687210`, library `v1.0.6-0.20261011112227-23c52e60dd7e`, and CGO disabled. Installed SHA-256: `2980f24784660e65cf1758e8db227b0ad0d4006a1e00bdb45bfa604fe5a281ae`.

The live picker refreshed to 16 standalone effects, including all three scenes (`assets/2026-10-11-terminal-artwork/installed-picker.png` in the local shell plan directory). During deployment the owner selected Great Wave, cyberpunk girl and wasteland/material on both outputs. Owned process executable metadata confirms the new release. The owner's latest selection remains active. Shell already supports catalog refresh, so this commission required no shell source change or deployment.

Tracking closes sysc-terminal-8 and shell sysc-1136/sysc-1137 after approval and deployment. Fractional-scale/resize blur observations and compositor frosting remain separate work.
