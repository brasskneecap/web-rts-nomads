# Arch Mage (Apprentice promotion path) — art placeholder

This directory is reserved for the Arch Mage promotion variant's sprites. It is
intentionally empty of art for now.

Until a PixelLab export is dropped in here, Arch Mage apprentices render with
the base `apprentice` sprite (the runtime loader in `unitSprites.ts` falls back
from `path` → `unitType`), so the path is fully playable without art.

## Adding the art

1. Drop the PixelLab export into this folder so it looks like the existing
   variants (see `../../../soldier/paths/berserker/` for the shape):
   - `metadata.json`
   - `states/.../rotations/*.png` and `states/.../animations/.../*.png`
     (or the legacy flat `rotations/` + `animations/` layout)
   - optional `portrait.png` for HUD/training surfaces
2. From `client/src/game-portal/`, run `npm run pack:sprites`. The packer
   auto-discovers `<unit>/paths/<path>/` — no config to edit — and emits
   `packed/*.png` + `sprites.json` next to this file.
3. If the new sprites are a different pixel size than the base Apprentice,
   add a `"bounds"` override to
   `server/internal/game/catalog/units/human/apprentice/paths/arch_mage/arch_mage.json`
   so the selection ring / hit-test rect match the new art.

The keyed sprite id is the directory name (`arch_mage`), which must match the
`"path"` field in the server catalog and the `unitPathArchMage` constant.
