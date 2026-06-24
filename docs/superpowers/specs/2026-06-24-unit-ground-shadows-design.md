# Unit Ground Shadows — Design

**Date:** 2026-06-24
**Status:** Approved (pending spec review)

## Problem

Units read as "flat stickers" on the map because nothing visually connects a
sprite to the ground it stands on. There is currently **no ground shadow** under
units — the "shadow/elevation" references for flyers in
[unitDefs.ts:68](../../../client/src/game-portal/src/game/maps/unitDefs.ts#L68)
and [protocol.ts:896](../../../client/src/game-portal/src/game/network/protocol.ts#L896)
are aspirational comments that were never implemented.

## Goal

A soft elliptical "blob" shadow under every unit's feet, grounding the sprite.
On by default for all units with sensible auto-sizing, and tunable (or
disable-able) per unit through that unit's catalog file.

## Non-Goals

- No simulation / gameplay impact. This is purely client-side rendering.
- No new art assets — the shadow is drawn procedurally on the canvas.
- No dynamic light-direction / time-of-day shadows. A single static blob.

## Data Flow

The per-unit shadow config rides through the existing catalog pipeline, exactly
like `bounds` and `attackVisual`:

1. **Catalog JSON** (`server/internal/game/catalog/units/**/<unit>.json`) — a new
   optional `shadow` block alongside `bounds`.
2. **Go `unitDef`** ([server/internal/game/unit_defs.go](../../../server/internal/game/unit_defs.go)) —
   add `Shadow json.RawMessage `json:"shadow,omitempty"``. Opaque pass-through;
   the server never reads it. Without this field the key would be dropped when
   the catalog is unmarshalled and re-marshalled to the client.
3. **Client `UnitDef`** ([unitDefs.ts](../../../client/src/game-portal/src/game/maps/unitDefs.ts)) —
   add optional `shadow?: UnitShadow`.
4. **Renderer** ([CanvasRenderer.ts](../../../client/src/game-portal/src/game/rendering/CanvasRenderer.ts)) —
   the only consumer; draws the blob.

This respects the architecture rule that the server owns simulation and the
client is a view: the shadow never touches the tick loop, state, or determinism.

## Config Shape

All fields optional; every one has a default derived from the unit's existing
`bounds`, so a unit with **no** `shadow` block still gets a proportional shadow.

```jsonc
"shadow": {
  "enabled": true,   // default true; false disables the shadow for this unit
  "radiusX": 18,     // horizontal radius (px). Default: bounds.halfWidth * 0.85
  "radiusY": 7,      // vertical radius (px).   Default: resolvedRadiusX * 0.4
  "opacity": 0.35,   // 0..1 peak alpha at the center. Default: 0.35
  "offsetX": 0,      // px nudge from the feet anchor. Default: 0
  "offsetY": 0       // px nudge from the feet anchor (positive = down). Default: 0
}
```

### Client type

```ts
export type UnitShadow = {
  enabled?: boolean
  radiusX?: number
  radiusY?: number
  opacity?: number
  offsetX?: number
  offsetY?: number
}
```
Added to `UnitDef` as `shadow?: UnitShadow`.

## Resolution Helper (the testable core)

A pure function isolates all defaulting/flyer logic from the canvas so it can be
unit-tested without a rendering context:

```ts
// Returns null when the shadow is disabled.
resolveUnitShadow(
  config: UnitShadow | undefined,
  bounds: UnitBounds,
  flyer: boolean,
): { radiusX: number; radiusY: number; opacity: number; offsetX: number; offsetY: number } | null
```

Rules:
- `enabled === false` → returns `null` (no shadow).
- `radiusX` default = `bounds.halfWidth * 0.85`.
- `radiusY` default = `radiusX * 0.4`.
- `opacity` default = `0.35`, clamped to `[0, 1]`.
- `offsetX` / `offsetY` default = `0`.
- **Flyer adjustment** (applied after the above, only when `flyer === true` and
  the field was not explicitly set in config):
  - `offsetY += FLYER_SHADOW_DROP` (default ~14px) so the shadow falls to the
    ground beneath the floating sprite.
  - `radiusX *= 1.15`, `radiusY *= 1.15` (larger, diffuse).
  - `opacity *= 0.6` (fainter).
  - Explicit per-file values always win over the flyer auto-adjustment.

## Rendering

In `drawUnits`, at the **start** of each unit's per-entry block (before perk
auras, selection rings, and the sprite — so the shadow is the lowest layer):

1. Resolve the feet anchor. Reuse the same feet math the selection ring uses
   (`unit.x + ringOffsetX`, `unit.y + bottomOffset - ringLift + ringOffsetY`),
   then apply the shadow's `offsetX`/`offsetY`.
2. Call `resolveUnitShadow(...)`; skip drawing if it returns `null` or the unit
   has no `spriteSet` (placeholder blobs already read as grounded).
3. Draw a **soft radial-gradient ellipse**:
   - A cached unit-circle radial gradient (`rgba(0,0,0,opacity)` at center →
     `rgba(0,0,0,0)` at edge) is scaled to the ellipse via a `ctx.save()` →
     `translate(cx, cy)` → `scale(radiusX, radiusY)` → fill unit circle →
     `ctx.restore()`. One fill per visible unit; feathered edge for free.
   - `imageSmoothingEnabled` is irrelevant (vector fill).

### Layering note

Drawing the shadow first means selection / hover / inspected rings render on top
of it. That is correct: the ring is a thin stroke at the feet and reads fine
over a faint shadow, and the sprite then covers the shadow's upper half, which
is exactly the "standing on it" look we want.

## Performance

One extra gradient-filled ellipse per visible unit per frame. The gradient
object is built once (module-level or cached on the renderer) and reused via
transform scaling, so there is no per-unit gradient allocation. Negligible
versus the existing per-unit sprite + ring + overhead-UI work.

## Defaults Rollout

No catalog edits are strictly required — the renderer's derived defaults give
every unit a shadow immediately. We will add an explicit `shadow` block only
where a unit needs tuning (e.g. flyers that want a custom drop, or oversized
sprites whose auto-radius looks wrong). The `raider_roc` flyer is the obvious
first candidate for a hand-tuned block during the visual pass.

## Testing

- **Unit test** `resolveUnitShadow` (pure, no canvas): default derivation from
  bounds, opacity clamping, `enabled: false` → null, flyer adjustments, and
  explicit-value-wins-over-flyer-default.
- **Visual verification:** run the client, confirm ground units now sit on a
  soft shadow; confirm a flyer (`raider_roc`) casts an offset, fainter shadow
  below it. Tune live with the existing **F9** catalog-reload hotkey.

## Files Touched

- `server/internal/game/unit_defs.go` — add `Shadow json.RawMessage` field.
- `client/src/game-portal/src/game/maps/unitDefs.ts` — add `UnitShadow` type +
  `shadow?` on `UnitDef`, and the `resolveUnitShadow` helper (or a sibling
  module + test).
- `client/src/game-portal/src/game/rendering/CanvasRenderer.ts` — draw the blob
  in `drawUnits`; cache the radial gradient.
- (Optional, during the visual pass) per-unit `shadow` blocks in catalog JSON,
  starting with the flyer(s).
