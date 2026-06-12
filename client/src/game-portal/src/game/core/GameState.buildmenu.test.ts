// Regression test for the worker build menu.
//
// Bug: the menu placed buildable buildings in slots 0..N-1 then hard-coded the
// exit button at slot 6, which OVERWROTE the 7th buildable building. Sorted by
// type that 7th building was "townhall", so once the catalog grew to 7
// buildable buildings (chapel was added) the town hall silently dropped out of
// the menu and workers could no longer build it.
//
// Fix: exit goes in the slot immediately after the buildings, so it never
// collides with one.

import { describe, expect, it } from 'vitest'
import { buildMenuActions } from './GameState'
import { initBuildingDefs, BUILDABLE_BUILDING_DEFS } from '../maps/buildingDefs'
import type { BuildingDef } from '../maps/buildingDefs'

function mkDef(type: string, buildable = true): BuildingDef {
  return {
    type,
    buildable,
    width: 1,
    height: 1,
    maxHp: 100,
    buildSeconds: 5,
    resourceCost: { gold: 100 },
    capabilities: [],
    spawnUnitTypes: [],
    metadata: {},
    color: '#fff',
    label: type,
    hotkey: type[0],
  }
}

describe('worker build menu', () => {
  it('includes every buildable building plus exit, with none overwritten (townhall as 7th building)', () => {
    // 7 buildable (sorted by type, townhall sorts last) + 2 world-placed.
    initBuildingDefs([
      mkDef('Tower'),
      mkDef('barracks'),
      mkDef('blacksmith'),
      mkDef('chapel'),
      mkDef('farm'),
      mkDef('marketplace'),
      mkDef('townhall'),
      mkDef('goldmine', false),
      mkDef('spawn-point', false),
    ])
    expect(BUILDABLE_BUILDING_DEFS).toHaveLength(7)

    const actions = buildMenuActions(9) // high tier so nothing is tier-gated

    // Every buildable building must have its own build action.
    for (const def of BUILDABLE_BUILDING_DEFS) {
      expect(
        actions.some((a) => a?.id === `build-${def.type}`),
        `expected build-${def.type} in the menu`,
      ).toBe(true)
    }
    // The regression victim specifically, and it carries its hotkey for the
    // tooltip (def.hotkey "t" -> "T").
    const townhall = actions.find((a) => a?.id === 'build-townhall')
    expect(townhall).toBeTruthy()
    expect(townhall?.hotkey).toBe('T')
    // Every building action exposes its hotkey for the tooltip.
    for (const a of actions.filter((x) => x?.id?.startsWith('build-'))) {
      expect(a.hotkey, `${a.id} hotkey`).toBeTruthy()
    }
    // Exit is present and did not replace any building.
    expect(actions.some((a) => a?.id === 'close-build-menu')).toBe(true)
    expect(actions.filter((a) => a?.id?.startsWith('build-')).length).toBe(7)
  })

  it('handles the small catalog (<=6 buildable) without a gap before exit', () => {
    initBuildingDefs([mkDef('barracks'), mkDef('farm'), mkDef('townhall')])
    const actions = buildMenuActions(9)
    expect(actions.filter((a) => a?.id?.startsWith('build-')).length).toBe(3)
    // Exit sits immediately after the 3 buildings (slot 3), no overwrite.
    expect(actions[3]?.id).toBe('close-build-menu')
    expect(actions.some((a) => a?.id === 'build-townhall')).toBe(true)
  })
})
