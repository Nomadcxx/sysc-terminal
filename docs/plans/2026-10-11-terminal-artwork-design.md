# Standalone character artwork design

The commission in sysc-1136 and sysc-1137 authorizes three built-in ASCII scenes: great-wave, cyberpunk-girl and wasteland. The approved engine architecture and resource budget remain applicable.

## Representation and ownership

Use one small shared scene implementation in sysc-Go animations, with three explicit constructors and standalone registry metadata. Bake printable ASCII glyphs and colour roles into gzip-compressed cell frames. Do not ship source rasters or GIFs. The installed runtime uses only Go standard library decompression; no GIF player, goroutine, background clock or new module dependency. Extend the existing offline conversion tool only if useful; its still-image ASCII ramp and 1:2 cell aspect are the reference. A small Go baker handles frame disposal and offsets correctly and can reproduce the assets from local originals.

Runtime GIF decoding is unnecessary work and memory. Procedural reconstruction of the supplied portraits would lose their distinctive composition. Baked cell frames preserve all original frames, need no Downloads paths and compress repeated regions efficiently.

Bake at 240 columns and proportional rows using conventional 1:2 character cells. Fit and center within the output, reserving a readable provenance row. Downsampling averages source cells so the small wasteland figure survives; upsampling picks source cells. Maximum output 512 by 256 cells, 160 frames, 256 by 128 source cells, 8 MiB inflated data per scene and 1 MiB compressed per scene. Validate headers, lengths, delays, glyphs and colour roles before allocation/use; render stays below 4 MiB. Each scene loads once lazily and shares immutable frames. No unbounded cache.

## Motion and colour

Update advances a nominal 50 ms step. Frame durations remain 120 ms for cyberpunk-girl, 90 ms for wasteland; delays are accumulated rather than truncated. Great Wave has 24 restrained foam/water glyph variants at 250 ms, a six-second loop with fixed crest, boats and Fuji. Render reads the current frame without advancing it; Reset restores phase zero; Resize preserves phase. Existing pause scheduling does no effect work.

At blur 0, host tick timing approximates source playback. At blur 64, 120 ms host ticks make loops 2.4 times slower. This follows existing frame-based effects and needs no host timing API change. Document and preview the cadence difference. The default 1:2 cell convention matches the existing offline converter. The engine passes measured raster cell width/height with worker resize, retains it on change/reset, and the scene fits with that ratio. This corrects the wallpaper font's actual 7:16 cells without altering the animation clock.

Map baked cool/warm/neutral/secondary roles through existing sysc-Go logo and particle palettes. Luminance is carried by ASCII density; a dark background and ordered brightness levels preserve subject/background contrast across Kanagawa, Dracula and monochrome. Credit row: Hokusai / The Met (public domain), or Valenberg.com (supplied reference). Sources and SHA-256 recorded with the baked assets; original files remain local.

## Integration and qualification

Add library selector entries and constructors. Engine adds only constructor cases and updates its sysc-Go pin after library merge. Shell already consumes standalone catalog entries; prove this with its existing catalog test and a focused scene catalog input. No copied registry or scene UI.

Focused checks cover baked validation, complete loops, repeated rendering, reset, size clamps, palette change, actual ink, engine list/constructor/tick/resize/pause. Use the engine's named one-package check command with CGO_ENABLED=0 and GOMAXPROCS=2. Extend its existing budget check to select an effect/theme, then measure all three at the font-derived wallpaper grid with blur 0 and 64. CPU p95/mean caps: 12.5 ms at 50 ms and 30 ms at 120 ms. Wall cap is the interval; RSS under 150 MiB and soak growth under 10 MiB.

Before merge, show small-grid and wallpaper previews plus complete loops. Review locally, run CI, merge library before engine, install through documented Go procedure and deploy shell current-main only if necessary. Live processes use dedicated owned sockets; capture current assignment and installed identities, observe playback/pause and RSS, and restore the owner's assignments. Fractional scale and unavailable outputs are recorded honestly.
