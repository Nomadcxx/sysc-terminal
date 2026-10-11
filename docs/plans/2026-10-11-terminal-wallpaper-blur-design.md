# Terminal wallpaper blur design

Owner approved adjustable blur and Settings controls on 2026-10-11, with GPU rendering permitted. Blur starts off. A strength of 0..64 output pixels softens wallpaper pixels after the existing sysc-Go effect and glyph rendering; it does not replace effects or control compositor frosting.

Use area averaging and three separable sliding-window box passes on a quarter-size framebuffer. The existing wp_viewport scales the reduced buffer to the logical output size on the compositor. Keep an unblurred canvas and its own dirty-render history; compositor buffers receive only the final blurred image. Cache reduced pixels and scratch storage per geometry. Respect frame callbacks, released buffers and pause. The strength uses output pixels, as sysc-lock does. Blur-enabled playback uses the approved 120 ms cadence fallback, while zero blur retains 50 ms. Run the real frame CPU budget with blur before choosing the implementation; GPU post-processing remains the fallback design if CPU costs cannot fit the approved budget.

Advertise `capability blur 64` through --list. Add --blur 0..64. Older shells ignore the extra catalog line; newer shells omit the flag when strength is zero and reject unsupported nonzero blur before replacing a running wallpaper.

Settings → Terminal Art gains a labeled Blur strength slider with 0 meaning off. Settings stores the default for the next effect selection, matching Default palette. Each effect assignment records its applied strength so reconnect, supervision and rollback preserve presentation. Running effects change when selected again; the UI says this. No settings slider triggers process churn or changes paused state.

Use named tests for input validation, smoothing/edge behavior, canvas history, compatibility, assignment round-trip/reconnect/rollback and the Settings control. Review the diffs and CI before merging. New artwork effects are commissioned in a separate handover.
