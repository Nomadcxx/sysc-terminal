# Lock-screen parity: engine delta

Not a locker design. Locker contracts live in sysc-shell
`docs/plans/2026-10-03-sysc-lock-design.md` (`sysc-910`). This file is only
what that design must know about **sysc-terminal**, plus UX facts from
current DMS/Noctalia that the lock design is wrong or silent about.

## Engine vs lock surface

`sysc-terminal` is a Background-layer client. `ext-session-lock-v1` blanks
normal clients. A playing effect is gone the moment Niri sends `locked`.

sysc-lock v1 already pauses wallpaper on spawn and paints a **static**
PNG/JPEG of the current wallpaper file. That path assumes an image path.
KindEffect has none.

For an effect assignment, the lock background is one of:

1. Last rasterised ARGB frame, copied out before pause (still of fire/rain).
2. Theme solid (lock design fallback). Honest, dull.
3. Pre-lock screencopy of the whole desktop (Noctalia; deferred in sysc-lock).

Recommend (1) when the engine is the assigned wallpaper, else (2). Do not
keep the engine mapped through lock. Do not composite live frames onto the
lock surface in v1.

IPC: shell already pauses on locker spawn. On unlock, D5 applies: resume
must request **one** `wl_surface.frame` or the callback chain stays dead.

## Corrections to the sysc-lock parity table

Checked against DMS `quickshell/Modules/Lock/` (Lock.qml,
LockScreenContent.qml) and Noctalia `src/shell/lockscreen/` plus
https://docs.noctalia.dev/noctalia/configuration/shell/.

| Lock-design claim | Current evidence | Action |
| --- | --- | --- |
| DMS Caps Lock = no | LockScreenContent shows “Caps Lock is on” | Keep sysc v1 Caps Lock; table row is stale |
| Reduced motion “—” for DMS | Scrolling wallpaper + theme motion on lock | Lock v1 still ships no motion (good). If transitions land later, honour reduced motion |
| Lock bg = per-monitor wallpaper file | True for image/video. False for KindEffect | Need last-frame still or solid |
| — | DMS #2131 / #2950: IME/`xdg_popup` on lock surface is a niri protocol kill | No text-input/IME on the lock surface. Hidden field if an input method is ever required |
| — | DMS shares one password buffer across outputs | Same for sysc-lock |
| — | WCAG 2.2 AA: paste and password-managers allowed | Do not swallow paste on the field |
| — | Empty Enter ignored unless Noctalia `allow_empty_password` | Same default; avoids `pam_faillock` from wake keys |
| — | Inactive / unlist outputs: DMS solid colour, Noctalia black | Match lock design’s unlisted-output policy; do not leave a live wallpaper showing |

Deferred in sysc-lock and still deferred: fingerprint, virtual keyboard,
screencopy bg, MPRIS/weather, lock notifications, DMS power-off-monitors
fade. Do not pull them into `sysc-terminal`.

## UI floor (when sysc-910 paints)

Compact lock is the parity floor, not Noctalia Regular (media + weather +
session + ~810 px panel).

- Clock + date, user identity, masked password, error beside the field
  (4 s auto-clear), Caps Lock, layout chip, focus on the field.
- Scrim or tint so clock/field contrast holds on a busy terminal still
  (Noctalia `tint_intensity`; DMS card surface on the field). Measure the
  composed buffer.
- Touch targets ≥ 44 px on the laptop output.
- Tab stays inside lock chrome. Colour is not the only error cue.

## Out of this repository

No lock protocol, PAM, or lock UI in `sysc-terminal`. Engine v1: pause on
the shell’s locker spawn, freeze last buffer, resume with one frame
callback. Snapshot of that buffer for KindEffect lock backgrounds is a
sysc-lock/sysc-shell job when `sysc-910` implements it.
