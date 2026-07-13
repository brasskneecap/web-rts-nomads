# pack conformance fixture

This directory holds a synthetic PixelLab-shaped export plus its golden
output from the REAL CLI packer (`scripts/pack-unit-sprites.mjs`). The
`spritePacking.test.ts` conformance suite decodes `metadata.json` + the raw
frame PNGs, runs them through the pure TS core (`planSpriteSheets`), and
diffs the result (manifest JSON + rasterized sheet pixels) against
`sprites.json` + `packed/*.png` here. It proves the TS core reproduces the
CLI's sheet math byte-for-byte, not just "close enough."

Contents:
- `metadata.json` — synthetic PixelLab export metadata (2 rotations, one
  2-frame animation across 2 directions).
- `rotations/*.png`, `animations/Walking-test/*/*.png` — the raw 4x4 source
  frames, each a distinct solid color so a wrong row/column placement in the
  packed sheet would produce visibly different pixels.
- `packed/rotations.png`, `packed/walking.png` — the CLI's packed sheets.
- `sprites.json` — the CLI's manifest output (includes `packedAt`, which the
  conformance test strips before comparing, and `key: "__packtest__"`, which
  the conformance test also strips — see note below).

## Note on the `key` field

The CLI's manifest sets `key: path.basename(unitDir)` — derived from the
containing folder's name, not from `metadata.json` itself (real PixelLab
exports never carry a `key` field). `planSpriteSheets(meta, frameDims)` is a
pure function with no directory argument, so it cannot and does not
reproduce this field; `SpriteManifestJSON` omits `key` entirely. A future
task (the drop-zone UI) knows the dropped folder's name and is expected to
attach `key` itself when assembling the manifest to upload. The conformance
test strips `key` (and `packedAt`) from the golden before comparing.

## Regenerating this fixture

Only regenerate when `scripts/pack-unit-sprites.mjs` itself changes in a way
that affects sheet layout or manifest shape. From `client/src/game-portal`:

1. Create a throwaway synthetic export under
   `src/assets/units/human/__packtest__/`:
   - `metadata.json` with `character.size = {width:4, height:4}`,
     `frames.rotations = {north: "rotations/north.png", south:
     "rotations/south.png"}`, and `frames.animations = {"Walking-test": {
     north: ["animations/Walking-test/north/frame_000.png",
     "animations/Walking-test/north/frame_001.png"], south: [
     "animations/Walking-test/south/frame_000.png",
     "animations/Walking-test/south/frame_001.png"] }}`.
   - The 6 raw 4x4 PNGs it references, each a distinct solid color. Write a
     throwaway Node script using `pngjs` (already a devDependency) that
     writes each PNG's pixel buffer directly, place it under `scripts/` so
     Node's module resolution finds `pngjs`, run it with `node`, then
     **delete it**.
2. Run `npm run pack:sprites` from `client/src/game-portal`. It writes
   `packed/rotations.png`, `packed/walking.png`, and `sprites.json` under
   `__packtest__/`.
3. Copy into this directory (`src/game/units/__fixtures__/pack/`):
   `metadata.json`, the raw frame PNGs (preserving their relative paths —
   `rotations/north.png`, `animations/Walking-test/north/frame_000.png`,
   etc.), the `packed/` sheets, and `sprites.json`.
4. **Revert the asset-tree pollution completely:**
   `rm -rf src/assets/units/human/__packtest__` (and delete the throwaway
   script from `scripts/` if you copied it there instead of a temp dir).
   Then verify `git status --short src/assets/units/` is EMPTY. The fixture
   must live ONLY under `__fixtures__/`.
5. Re-run `npm run test -- spritePacking` — if the manifest test fails, the
   TS core has diverged from the CLI; fix the core, not this fixture.
