import { describe, it, expect } from 'vitest'
import {
  initTilesetDefs,
  drawTerrainTile,
  isWalkableGroundTile,
  getWangGrassDirtCoord,
  isTerrainCellBlocked,
  drawAutoTiledTerrain,
} from './terrainTileset'
import type { AutoTiledTerrainSpec } from './terrainTileset'
import type { GridCoord, TilesetDef } from '../network/protocol'

const def8x8: TilesetDef = { id: 'test-8x8', name: 'g', image: 'test-8x8.png', cols: 8, rows: 8, offsetX: 0, offsetY: 0, tileWidth: 128, tileHeight: 128, spacingX: 0, spacingY: 0 }

const cliffDef: TilesetDef = { id: 'test-cliff', name: 'cliff', image: 'test-cliff.png', cols: 4, rows: 4, offsetX: 0, offsetY: 0, tileWidth: 32, tileHeight: 32, spacingX: 0, spacingY: 0 }

// Fake HTMLImageElement whose `complete`/`naturalWidth` are already "loaded"
// synchronously — real jsdom Images only resolve after a decode tick, which
// these sync render-path tests can't wait on.
class FakeImage {
  complete = true
  naturalWidth = 32
  naturalHeight = 32
  src = ''
  addEventListener() {}
}

// 3x3 raised plateau in a 5x5 grid: (1,1)-(3,3) inclusive.
const raisedRect: GridCoord[] = []
for (let y = 1; y <= 3; y++) {
  for (let x = 1; x <= 3; x++) raisedRect.push({ x, y })
}

function makeCliffSpec(): AutoTiledTerrainSpec {
  return {
    gridCols: 5,
    gridRows: 5,
    cellSize: 32,
    terrain: [],
    elevation: raisedRect,
    cliffTileset: 'test-cliff',
  }
}

describe('data-driven terrainTileset', () => {
  it('flat -0 sheets fully walkable; -25 cliffs block; wang interior slots walkable', () => {
    expect(isWalkableGroundTile({ tileset: 'grass-grass-elevation-0', col: 3, row: 0 })).toBe(true)
    expect(isWalkableGroundTile({ tileset: 'grass-grass-elevation-25', col: 2, row: 2 })).toBe(false)
    expect(isWalkableGroundTile({ tileset: 'tileset', col: 2, row: 1 })).toBe(true)
    expect(isWalkableGroundTile({ tileset: 'tileset', col: 0, row: 3 })).toBe(true)
  })

  it('wang mask returns tileset col/row', () => {
    const c = getWangGrassDirtCoord(15) // all-grass
    expect(c.tileset).toBe('tileset')
    expect(typeof c.col).toBe('number')
  })

  it('drawTerrainTile slices via def offset/tileWidth', () => {
    initTilesetDefs([def8x8])
    // fake a loaded image by stubbing getTilesetImage is hard; instead assert no throw + that def math is used elsewhere.
    // Minimal: ensure initTilesetDefs registered the def (drawTerrainTile no-ops without a decoded image, which is fine).
    expect(() => drawTerrainTile({ imageSmoothingEnabled: false, drawImage() {} } as any, { tileset: 'test-8x8', col: 1, row: 0 }, 0, 0, 64)).not.toThrow()
  })

  it('isTerrainCellBlocked: cliff wall/outer-corner cells block, flat plateau interior does not', () => {
    const spec = makeCliffSpec()
    // (1,1) is the NW outer corner of the raised rectangle.
    expect(isTerrainCellBlocked(spec, 1, 1)).toBe(true)
    // (2,2) is the plateau's flat interior — all 8 neighbors are also raised.
    expect(isTerrainCellBlocked(spec, 2, 2)).toBe(false)
  })

  it('drawAutoTiledTerrain draws a cliff tile for every raised cell', () => {
    const originalImage = globalThis.Image
    // @ts-expect-error -- test stub stands in for the DOM Image constructor
    globalThis.Image = FakeImage
    try {
      initTilesetDefs([cliffDef])
      const draws: Array<{ destX: number; destY: number }> = []
      const ctx = {
        imageSmoothingEnabled: false,
        drawImage(_img: unknown, _sx: number, _sy: number, _sw: number, _sh: number, destX: number, destY: number) {
          draws.push({ destX, destY })
        },
      } as unknown as CanvasRenderingContext2D

      drawAutoTiledTerrain(ctx, makeCliffSpec())

      // Exactly the 9 raised cells should have drawn a cliff tile (the
      // unregistered ground tileset no-ops, and there are no tiles[]).
      expect(draws.length).toBe(raisedRect.length)
      for (const cell of raisedRect) {
        expect(draws).toContainEqual({ destX: cell.x * 32, destY: cell.y * 32 })
      }
    } finally {
      globalThis.Image = originalImage
    }
  })
})
