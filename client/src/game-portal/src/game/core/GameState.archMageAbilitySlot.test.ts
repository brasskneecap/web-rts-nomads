// Client HUD rendering for the Arch Mage ability-slot system
// (arch-mage-spell-system): a learned ability-slot ability renders in its
// rank's bottom-row cell as a CASTABLE ability (not a display-only perk), the
// arcane_missiles passive is hidden from the castable ability row, and a rank
// with no learned slot falls back to a locked perk placeholder.

import { describe, expect, it, beforeEach } from 'vitest'
import { GameState, type Unit, type ActionItem } from './GameState'
import { initPerkDefs, initPerkRanksFromPaths } from '../maps/perkDefs'
import type { AbilitySnapshot } from '../network/protocol'

beforeEach(() => {
  initPerkDefs([])
  initPerkRanksFromPaths([])
})

function makeArchMage(abilities: AbilitySnapshot[]): Unit {
  return {
    id: 1,
    unitType: 'adept',
    name: 'Arch Mage',
    capabilities: ['attack'] as Unit['capabilities'],
    visible: true,
    x: 0,
    y: 0,
    hp: 100,
    maxHp: 100,
    ownerId: 'p1',
    abilities,
  } as Unit
}

function stateWith(unit: Unit): GameState {
  const s = new GameState()
  s.localPlayerId = 'p1'
  s.units = [unit]
  s.selectedUnitIds = new Set([unit.id])
  s.selectedUnitOrder = [unit.id]
  return s
}

const PASSIVE: AbilitySnapshot = {
  id: 'arcane_missiles',
  displayName: 'Arcane Missiles',
  // The server now provides the tooltip prose (generated from the ability's
  // config or an author override); the client appends the live charge readout.
  description: 'Passive. Spending mana builds Arcane Charge; at 30 charge it fires 3 missiles.',
  passive: true,
  chargeCurrent: 12,
  chargeRequired: 30,
}
const BRONZE_SLOT: AbilitySnapshot = {
  id: 'fireball',
  displayName: 'Fireball',
  abilitySlotRank: 'bronze',
  supportsAutoCast: true,
  autoCast: true,
}

describe('Arch Mage ability slots', () => {
  it('renders the bronze slot spell in a castable bottom-row cell (kind ability + perkRank bronze)', () => {
    const actions = stateWith(makeArchMage([PASSIVE, BRONZE_SLOT])).getSelectionSummary().actions
    const cell = actions.find((a: ActionItem) => a.id === 'cast-ability-fireball')
    expect(cell).toBeTruthy()
    expect(cell!.kind).toBe('ability') // castable, not display-only perk
    expect(cell!.perkRank).toBe('bronze') // sits in the bronze perk cell
    expect(cell!.disabled).toBeFalsy()
    expect(cell!.supportsAutoCast).toBe(true)
  })

  it('renders the passive as a non-castable info cell with a live charge badge', () => {
    const actions = stateWith(makeArchMage([PASSIVE, BRONZE_SLOT])).getSelectionSummary().actions
    // It is NOT a castable ability cell...
    expect(actions.find((a: ActionItem) => a.id === 'cast-ability-arcane_missiles')).toBeUndefined()
    // ...it is a disabled passive cell that shows the accumulated charge.
    const passive = actions.find((a: ActionItem) => a.id === 'passive-arcane_missiles')
    expect(passive).toBeTruthy()
    expect(passive!.disabled).toBe(true)
    expect(passive!.chargeText).toBe('12/30')
    // The tooltip carries the server description prose plus the live charge line.
    expect(passive!.tooltipBody).toContain('Arcane Charge')
    expect(passive!.tooltipBody).toContain('Charge: 12/30')
  })

  it('falls back to a locked perk cell for a rank with no learned slot', () => {
    const actions = stateWith(makeArchMage([PASSIVE, BRONZE_SLOT])).getSelectionSummary().actions
    const perkCells = actions.filter((a: ActionItem) => a.kind === 'perk')
    // silver + gold still render as (locked) perk cells; bronze became an ability cell.
    expect(perkCells.length).toBe(2)
  })
})

describe('Arch Mage gold perk cell', () => {
  const GOLD_PERK = {
    id: 'arcane_feedback',
    displayName: 'Arcane Feedback',
    icon: 'perk-arcane-feedback',
    config: {},
  }

  it('renders the granted gold perk in the GOLD cell even though it is the unit\'s only perk (perkIds[0])', () => {
    // The Arch Mage learns a spell at bronze (spell slot) and has an EMPTY
    // silver spell pool AND empty silver/bronze perk pools, so its only perk
    // is granted at gold rank — the server appends it at perkIds[0], not
    // perkIds[2]. The gold perk cell must find it via PERK_RANK_BY_ID_MAP
    // (built from the path's perksByRank), not by slot index or spell-slot
    // presence (which would treat the silver "gap" as a perk-granting rank
    // and wrongly attribute the gold perk to the silver cell).
    initPerkDefs([GOLD_PERK])
    initPerkRanksFromPaths([{ gold: ['arcane_feedback'] }])
    const unit = makeArchMage([PASSIVE, BRONZE_SLOT])
    unit.perkIds = ['arcane_feedback']

    const actions = stateWith(unit).getSelectionSummary().actions
    const goldCell = actions.find((a: ActionItem) => a.kind === 'perk' && a.perkRank === 'gold')
    expect(goldCell).toBeTruthy()
    expect(goldCell!.id).toBe('perk-arcane-feedback') // the perk icon, NOT the 'lock' placeholder
    expect(goldCell!.tooltipTitle).toContain('Arcane Feedback')
  })
})
