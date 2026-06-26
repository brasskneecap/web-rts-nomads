# In-Match Music Cycling — Design

**Date:** 2026-06-26
**Status:** Approved

## Goal

Play background music during a match by cycling through a folder of `.mp3`
files. Adding a new track should require only dropping a file into a folder —
no code change.

## Directory

New folder: `client/src/game-portal/src/assets/audio/music/match/`.

- Contains a `README.md` so the (initially empty) directory is tracked by git
  and documents the "drop an mp3 here" workflow.
- Any `*.mp3` placed here is auto-discovered at build time and joins the
  rotation. The existing menu loop (`Iron Warchant.mp3`) stays in the parent
  `music/` folder and is unaffected.

## Discovery (no hardcoded filenames)

A new composable `useMatchMusic.ts` collects track URLs with Vite's
`import.meta.glob('@/assets/audio/music/match/*.mp3', { eager: true })`. Folder
keys are sorted for a stable base order before shuffling.

## Playback engine

Mirrors the structure of the existing `useMenuAudio.ts` for consistency:

- A single reused `HTMLAudioElement`, `loop = false` (tracks are advanced
  manually so the playlist can rotate).
- **Shuffle, no immediate repeat:** maintain a shuffled queue of all tracks.
  On each track's `ended` event, advance to the next. When the queue empties,
  reshuffle; if the first track of the new shuffle equals the track that just
  finished, swap it with the second so nothing plays back-to-back (only
  meaningful with 2+ tracks).
- Volume is driven by the existing `effectiveMusicVolume` from
  `useAudioSettings`, with the same live-update `watch` as menu audio.
- Same autoplay-gesture fallback (`pointerdown` / `keydown`) as menu audio,
  since a match may begin without a fresh user gesture.
- **Empty folder → no-op** (silent match), no errors.
- `Math.random` is acceptable here: this is client-side presentation, not
  server simulation, so the determinism rule does not apply.

## Wiring into the match lifecycle

In `App.vue`, "in a match" is already detected via `route.meta.silenceMusic`
(exposed as the `shouldPlayMusic` computed). Today entering a match stops menu
music and plays nothing. We invert that single hook point:

- Match start (`shouldPlayMusic` → false): `stopMenuMusic()` + `startMatchMusic()`.
- Match end (`shouldPlayMusic` → true): `stopMatchMusic()` + `startMenuMusic()`.

No new lifecycle plumbing — the existing `watch(shouldPlayMusic)` and
`onSplashDismiss` are the only edits.

## Out of scope (YAGNI)

No crossfades, no per-map playlists, no in-match next/prev controls, no
settings-panel track picker. The existing music volume slider already governs
match music through `effectiveMusicVolume`.
