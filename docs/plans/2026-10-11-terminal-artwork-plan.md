# Standalone character artwork implementation plan

**Goal:** Deliver recognizable animated ASCII Great Wave, cyberpunk girl and wasteland in the library, engine and shell catalog.

**Architecture:** Bounded baked cell frames in sysc-Go; existing host clock, parser and raster; authoritative catalog consumed by shell.

**Tech stack:** Go standard library; existing sysc-Go palettes and engine renderer; local source assets.

## Library

Add a failing named check in animations/artwork_test.go for standalone metadata and constructor/frame lifecycle, then implement animations/artwork.go and animations/artwork/*.cells.gz. Add tools/artwork-bake/main.go using image/gif compositing with offsets/transparency/disposal, JPEG input for the public-domain Great Wave, and the existing ASCII ramp. Retain all GIF frames and delays. Add provenance in animations/artwork/README.md, without raw references. Check invalid compressed input and geometry. Run timeout 90s env CGO_ENABLED=0 GOMAXPROCS=2 go test -count=1 ./animations -run TestArtwork. Extend tui/model.go, tui/animfactory.go and tui/animfactory_test.go; check selector constructs the three effects with editor-file selection disabled. Run the named TUI checks. Build and vet touched packages.

## Engine and catalog

Write TestArtworkConstructTickResizePause in internal/effect/effect_test.go before adding constructors to internal/effect/effect.go. During development use a temporary modfile replace pointing to the isolated library worktree; remove it from the final pin. Extend existing internal/pipeline/budget_test.go with bounded effect/theme environment selection. Run named effect/list checks, then sequential budget measurements at 3440 by 1440 for every scene with blur 0 and 64. Add a catalog check in sysc-shell internal/wallpaper/terminal_test.go only if current tests do not prove admission. Do not add production shell code if the existing parser works.

## Visual review and release

Generate first-frame images at 80 by 24 and wallpaper grid, plus complete loop animations for all three in two palettes. Inspect focal subjects and visible credit; fix bake before release if unreadable. Record source inspection, resource numbers and links to artifacts in a completion report. Manual local diff review, formatting, vet/build and CI; push screened commits under Nomadcxx identity; merge sysc-Go first. Resolve the upstream hash to a Go pseudo-version, rerun named integration and budget checks on the final engine pin, then merge engine and any needed shell check. Install terminal from current main using CGO-disabled Go install. Shell installation uses scripts/deploy from a clean current-main worktree.

Capture live output inventory, process ownership, assignment/config and deployment stamps. Use only test-owned sockets/pids; record screenshots, active CPU/RSS, pause response and soak. Restore captured owner wallpaper/config with current-main shell backend; confirm assignments and process identities. Record unavailable hardware separately in bd. Close commission issues only when their acceptance conditions are satisfied.
