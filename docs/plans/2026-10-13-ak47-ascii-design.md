# AK-47 artwork morph

Owner approved braille-particle artwork on 2026-10-13.

Keep `ak-47` as the existing swirl effect. Add `ak-47-rama` with the supplied
RAMA block artwork embedded in sysc-Go, and `ak-47-text` with artwork selected
through the existing file controls. Both use the existing AK-47 renderer:
two rotations over eight seconds, two seconds to form the artwork, two seconds
holding it, and two seconds to reform the rifle. Attribution stays fixed.

Sample occupied artwork cells into a bounded target point set. Preserve file
spacing and character aspect; reject empty, oversized, invalid UTF-8 or control
input. Reuse elapsed-time scheduling, palettes, resize and pixel aspect handling.
Do not add dependencies. The engine imports the shared implementation and
extends its existing footer and file paths. The shell discovers RAMA from the
engine catalog; custom artwork uses the existing library/TUI and engine CLI.

Remove `justice-cross` and `cyberpunk-girl` from released effect catalogs and
CLI/TUI/engine entry points. Retain their development source for later work.

Publish sysc-Go v1.0.7, then pin it in sysc-terminal v0.2.0. Update AUR source
hashes, package metadata and included model attribution for both packages.

## Implementation plan

1. Add `TestAK47ArtworkCycle` in sysc-Go `animations/ak47_test.go`: input bounds,
   two turns, stable artwork hold, exact loop, target changes, resize/aspect and
   registry exclusions. Observe failure, then extend `animations/ak47.go` and
   add `animations/models/RAMA_SYSC.txt`.
2. Extend registry, CLI and TUI factories/file selection. Update focused selector
   checks and remove unfinished effect entry points. Build CLI and TUI.
3. Add engine checks for RAMA, custom-file lifecycle, fixed credit and rejection
   of excluded effects. Observe failure; extend constructor/footer/text paths.
   Qualify a full 14-second loop at desktop size using the existing budget check.
4. Publish the shared library release, resolve the engine dependency without a
   local workspace, then run focused package checks and publish its release.
5. Build both AUR packages from the tagged archives, inspect installed contents,
   generate `.SRCINFO`, and push reviewed package changes. Install the released
   engine on the desktop while preserving wallpaper assignments.

Use named checks, one package per command, with `timeout 90s env GOMAXPROCS=2
go test -count=1 <package> -run <Name>`. No broad or race runs.
