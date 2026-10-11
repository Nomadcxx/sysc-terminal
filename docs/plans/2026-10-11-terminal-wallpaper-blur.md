# Terminal wallpaper blur implementation plan

**Goal:** Add bounded blur to wallpaper frames and a persistent default in Settings → Terminal Art.
**Architecture:** Quarter-size CPU post-processing after the authoritative renderer, with separate unblurred dirty history and catalog capability negotiation. GPU is permitted if measured CPU cost fails the approved resource budget.
**Tech stack:** Go standard library, wl_shm and existing settings/assignment plumbing.

1. Engine: failing named raster/CLI tests; add cached blur, --blur validation, --list capability and owner-buffer integration. Preserve zero-blur path. Extend the existing opt-in hardware budget check to include blur. Run named checks and the real CPU gate.
2. Shell: failing named config, catalog/assignment and Settings tests; add blur default/range, persistent assignment/job/command field, launch/rollback propagation and capability guard. Add existing settings slider and next-selection explanation. Check old-engine compatibility and reconnect.
3. Review both diffs locally, run required focused gates, create concise PRs and push. Merge only with a clean review and successful relevant checks. Follow-up artifact: artwork-effect execution handover for Great Wave, cyberpunk girl and wasteland, with source hashes and authoritative registry seams.

Engine worktree: /home/nomadx/sysc-terminal/.worktrees/catalog-refresh-20261011 (feat/wallpaper-blur).
Tracking: sysc-terminal-6, sysc-1135. The initial commission remains sysc-912. Use bd for state.
