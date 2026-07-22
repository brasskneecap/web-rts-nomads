import { describe, expect, it } from 'vitest'
import { expandTileGroups, groupTiles } from './terrainTileGroups'

describe('expandTileGroups', () => {
  it('migrates a legacy grouped payload (sheet/sx/sy) to tileset/col/row', () => {
    const expanded = expandTileGroups([
      { sheet: 'grass-grass-8x8', sx: 96, sy: 0, locations: [[1, 2]] } as never,
    ])
    expect(expanded).toEqual([
      { x: 1, y: 2, tileset: 'grass-grass-8x8', col: 3, row: 0 },
    ])
  })

  it('expands a new grouped payload (tileset/col/row) unchanged', () => {
    const expanded = expandTileGroups([
      { tileset: 't', col: 2, row: 1, locations: [[0, 0]] },
    ])
    expect(expanded).toEqual([{ x: 0, y: 0, tileset: 't', col: 2, row: 1 }])
  })
})

describe('groupTiles', () => {
  it('round-trips through expandTileGroups to the same tile', () => {
    const tiles = [{ x: 0, y: 0, tileset: 't', col: 2, row: 1 }]
    const grouped = groupTiles(tiles)
    const expanded = expandTileGroups(grouped)
    expect(expanded).toEqual(tiles)
  })
})
