# Sound effects

Drop sound-effect files (`.mp3`, `.wav`, or `.ogg`) into this folder. They are
discovered automatically at build time and referenced by **bare filename** from
config — there is no manual registry to update.

## How a sound gets played

A building plays a sound for a given *situation* via its config file at
`assets/buildings/<building>/sounds.json`, which maps an event name to a sound
in this folder. An entry can be either a bare filename:

```json
{
  "select": "barracks_select.mp3"
}
```

…or an object that also sets a per-sound volume:

```json
{
  "select": { "file": "barracks_select.mp3", "volume": 0.8 }
}
```

`volume` is a `0..1` multiplier applied **on top of** the SFX slider — `1`
(the default) plays at full SFX volume, `0.5` at half. Out-of-range values are
clamped. Both forms can be mixed across entries and buildings.

When that building is clicked, the engine looks up the `select` entry, finds
the file here, and plays it at the configured volume. A building with no
`sounds.json`, or no entry for the event, simply makes no sound.

The wiring lives in [`useSfx.ts`](../../../composables/useSfx.ts):

- `playBuildingSelectSound(type)` plays the building's `select` sound on a
  single interruptible channel. It stops automatically when the building is
  deselected (clicked away, destroyed, or another selection is made) and is
  replaced when another building is selected.
- `playSfx(filename)` plays any file here as a fire-and-forget one-shot.
- `playBuildingSound(type, event)` plays a building's configured sound for a
  one-shot event (future: `attack`, `build`, ...).

Volume follows the SFX slider in Options.

## Adding more situations later

The same `sounds.json` shape extends to other events — e.g. `attack`, `build`,
`destroy`, `train` — and `playBuildingSound` / `playSfx` already support them.
Just add the key to the building's `sounds.json` and call
`playBuildingSound(type, '<event>')` from the relevant code path.
