import type { BuildingRenderDef } from './buildingDefs'

// Procedural fallback render defs for buildings that don't ship a sprite.png.
// These used to live in the server building catalog (each def's `render`
// block), but render layers are pure frontend presentation data, so they
// belong here. Every building that has a sprite is drawn from that sprite and
// never reaches this path — add an entry only when a building needs a drawn
// fill-in before its sprite exists.
//
// Coordinates and layer semantics match BuildingRenderDef / BuildingRenderLayer
// in buildingDefs.ts.
export const BUILDING_FALLBACK_RENDER: Record<string, BuildingRenderDef> = {
  'spawn-point': {
    inset: 0.24,
    layers: [
      { kind: 'rect', x: 0.35, y: 0.08, w: 0.3, h: 0.84, color: '#38bdf8' },
      { kind: 'rect', x: 0.08, y: 0.35, w: 0.84, h: 0.3, color: '#38bdf8' },
      { kind: 'rect', x: 0.41, y: 0.14, w: 0.18, h: 0.72, color: '#e0f2fe' },
      { kind: 'rect', x: 0.14, y: 0.41, w: 0.72, h: 0.18, color: '#e0f2fe' },
    ],
  },
}

// Returns the frontend-defined procedural render for a building type, or
// undefined when the building should be drawn from its sprite (the common case).
export function getBuildingFallbackRender(type: string): BuildingRenderDef | undefined {
  return BUILDING_FALLBACK_RENDER[type]
}
