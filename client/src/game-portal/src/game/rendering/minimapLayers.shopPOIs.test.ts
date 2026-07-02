// Neutral shop POIs on the minimap: any building tile that sells something
// (item-purchase / recipe-purchase capability) is a point of interest that
// should render as a light-yellow house marker above fog of war, like
// neutral-camp dots.
//
// getShopPOIs is the pure filter shared by all three minimap surfaces
// (in-game HUD, lobby preview, map editor). All shop kinds share one bright
// SHOP_POI_COLOR so the markers are easy to spot at minimap scale.

import { describe, expect, it } from 'vitest'
import { SHOP_POI_COLOR, drawMinimapPOIs, getShopPOIs } from './minimapLayers'
import type { MinimapMapInput } from './minimapLayers'
import type { BuildingTile } from '../network/protocol'

function makeTile(over: Partial<BuildingTile> & { buildingType: string }): BuildingTile {
  return {
    id: `${over.buildingType}-${over.x ?? 0}-${over.y ?? 0}`,
    x: 0,
    y: 0,
    width: 3,
    height: 3,
    occupied: true,
    visible: true,
    capabilities: [],
    ...over,
  }
}

describe('getShopPOIs', () => {
  it('keeps buildings that sell items or recipes and drops everything else', () => {
    const buildings: BuildingTile[] = [
      makeTile({ buildingType: 'neutral-shop', x: 10, y: 5, capabilities: ['item-purchase'] }),
      makeTile({ buildingType: 'recipe-shop', x: 20, y: 8, capabilities: ['recipe-purchase'] }),
      makeTile({ buildingType: 'goldmine', x: 4, y: 4, capabilities: ['resource-source'] }),
      makeTile({ buildingType: 'townhall', x: 1, y: 1, capabilities: ['unit-spawner'] }),
      makeTile({ buildingType: 'enemy-spawnpoint', x: 2, y: 9, capabilities: ['enemy-spawner'] }),
    ]

    const pois = getShopPOIs(buildings)

    expect(pois.map((p) => p.buildingType).sort()).toEqual(['neutral-shop', 'recipe-shop'])
  })

  it('carries the footprint through so callers can center the marker', () => {
    const [poi] = getShopPOIs([
      makeTile({
        buildingType: 'recipe-shop',
        x: 21,
        y: 53,
        width: 3,
        height: 3,
        capabilities: ['recipe-purchase'],
      }),
    ])

    expect(poi).toMatchObject({ x: 21, y: 53, width: 3, height: 3 })
  })

  it('skips hidden building tiles', () => {
    const pois = getShopPOIs([
      makeTile({ buildingType: 'neutral-shop', capabilities: ['item-purchase'], visible: false }),
    ])
    expect(pois).toEqual([])
  })

  it('tolerates an absent buildings list', () => {
    expect(getShopPOIs(undefined)).toEqual([])
    expect(getShopPOIs(null)).toEqual([])
  })
})

// jsdom has no real 2D canvas (and never finishes loading the SVG house
// image), so the path-drawn fallback house runs here. Record the calls we
// care about: each shop marker is one fill()ed house path in the shared
// light-yellow POI color.
function makeRecordingCtx() {
  const fills: string[] = []
  const ctx = {
    fillStyle: '',
    strokeStyle: '',
    lineWidth: 0,
    beginPath() {},
    moveTo() {},
    lineTo() {},
    closePath() {},
    arc() {},
    fill() {
      fills.push(String(this.fillStyle))
    },
    stroke() {},
    drawImage() {},
  }
  return { ctx: ctx as unknown as CanvasRenderingContext2D, fills }
}

function makeMap(buildings: MinimapMapInput['buildings']): MinimapMapInput {
  return {
    gridCols: 32,
    gridRows: 32,
    cellSize: 64,
    terrain: [],
    tiles: [],
    defaultTile: undefined,
    obstacles: [],
    buildings,
    neutralSpawns: undefined,
  }
}

describe('drawMinimapPOIs — shop markers', () => {
  const bounds = { x: 0, y: 0, width: 128, height: 128 }

  it('paints one marker per shop derived from the map buildings (lobby/editor path)', () => {
    const { ctx, fills } = makeRecordingCtx()
    const map = makeMap([
      {
        id: 'neutral-shop-10-5',
        buildingType: 'neutral-shop',
        x: 10,
        y: 5,
        width: 3,
        height: 3,
        occupied: true,
        visible: true,
        capabilities: ['item-purchase'],
      },
    ])

    drawMinimapPOIs(ctx, map, bounds, null)

    expect(fills).toEqual([SHOP_POI_COLOR])
  })

  it('prefers an explicit POI list over the (FOW-filtered) map buildings (in-game path)', () => {
    const { ctx, fills } = makeRecordingCtx()
    // Live building list is empty — the shop is unscouted and the server
    // dropped it — but the welcome-time POI list still has it.
    const map = makeMap([])

    drawMinimapPOIs(ctx, map, bounds, null, null, [
      { id: 'recipe-shop-21-3', buildingType: 'recipe-shop', x: 21, y: 3, width: 3, height: 3 },
    ])

    expect(fills).toEqual([SHOP_POI_COLOR])
  })
})
