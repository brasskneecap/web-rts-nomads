// Vitest harness for the Twin Bronze advancement (Soldier node 8) — client-side
// HUD rendering of the 4th perk cell.
//
// Trigger: the 4th cell is rendered whenever `unit.extraPerkSlots?.bronze > 0`
// (the server-authoritative snapshot field), NOT a function of perkIds.length.
// This matches the design intent: the slot exists as a locked icon from the
// moment the owner has the advancement, the same way silver/gold render
// locked before the unit promotes into those tiers.
//
// Scope:
//   - AC #8: Twin Bronze soldier at gold rank (4 perkIds) → 4 cells, all filled.
//   - AC #9: Standard soldier (no extraPerkSlots, empty perkIds) → 3 cells, all locked.
//   - NEW:   Twin Bronze soldier pre-promotion (extraPerkSlots set, no perks)
//             → 4 cells, all locked, with cell 3 = locked bronze.
//   - NEW:   Twin Bronze soldier at bronze rank (extraPerkSlots set, 2 perks)
//             → 4 cells: primary bronze real, silver/gold locked, secondary bronze real.
//
// perkIds layout for Twin Bronze (server grant ordering):
//   [0] = primary bronze
//   [1] = secondary bronze  ← appears 4th in HUD (slot 12 of action grid)
//   [2] = silver (if reached)
//   [3] = gold (if reached)
//
// Valid soldier perk IDs used below are drawn from:
//   - vanguard bronze: retaliation, hold_the_line, reinforced_armor, shield_bash, interlock
//   - vanguard silver: last_stand, punishing_guard, brace, challengers_mark
//   - vanguard gold:   guardian_aura, pain_share, rallying_banner
// These are the ids present in the server catalog JSON files; they must match
// whatever the PERK_DEF_MAP is seeded with in each test.

import { describe, expect, it, beforeEach } from 'vitest'
import { GameState, type Unit, type ActionItem } from './GameState'
import { initPerkDefs, initPerkRanksFromPaths } from '../maps/perkDefs'
import type { PerkDef } from '../maps/perkDefs'

// Minimal perk defs — only the fields getPerkActionItems / buildPerkSlot read.
// Perks carry no innate rank field anymore; the standard 3-cell layout
// instead resolves each perk's cell via PERK_RANK_BY_ID_MAP (built from a
// promotion path's perksByRank — see initPerkRanksFromPaths below), matching
// production's server-driven association model. The Twin Bronze branch
// places by fixed index and doesn't consult rank at all.
const STUB_PERK_DEFS: PerkDef[] = [
  { id: 'retaliation', displayName: 'Retaliation', icon: 'perk-retaliation', config: {} },
  { id: 'hold_the_line', displayName: 'Hold the Line', icon: 'perk-hold-the-line', config: {} },
  { id: 'last_stand', displayName: 'Last Stand', icon: 'perk-last-stand', config: {} },
  { id: 'guardian_aura', displayName: 'Guardian Aura', icon: 'perk-guardian-aura', config: {} },
]

// vanguard bronze holds BOTH bronze perks (Twin Bronze grants two), matching
// the "vanguard bronze: retaliation, hold_the_line, ..." doc note below.
const STUB_PERKS_BY_RANK: Record<string, string[]> = {
  bronze: ['retaliation', 'hold_the_line'],
  silver: ['last_stand'],
  gold: ['guardian_aura'],
}

// Twin Bronze marker — what the server sends on the snapshot for a Soldier
// whose owner has soldier_twin_bronze acquired.
const TWIN_BRONZE_SLOTS: Record<string, number> = { bronze: 1 }

function makeSoldier(overrides: Partial<Unit> = {}): Unit {
  return {
    id: 1,
    unitType: 'soldier',
    name: 'Soldier',
    capabilities: ['attack'] as Unit['capabilities'],
    visible: true,
    x: 0,
    y: 0,
    hp: 100,
    maxHp: 100,
    ownerId: 'p1',
    abilities: [],
    ...overrides,
  }
}

function makeStateWithSoldier(soldier: Unit): GameState {
  const state = new GameState()
  state.localPlayerId = 'p1'
  state.units = [soldier]
  state.selectedUnitIds = new Set([soldier.id])
  state.selectedUnitOrder = [soldier.id]
  return state
}

// Extract only the perk-kind ActionItems from the grid.
function perkItems(actions: ActionItem[]): ActionItem[] {
  return actions.filter((a) => a.kind === 'perk')
}

beforeEach(() => {
  initPerkDefs(STUB_PERK_DEFS)
  initPerkRanksFromPaths([STUB_PERKS_BY_RANK])
})

// ─────────────────────────────────────────────────────────────────────────────
// AC #8 — Twin Bronze at gold rank: 4 cells, all filled
// ─────────────────────────────────────────────────────────────────────────────
describe('Twin Bronze — AC #8: fully promoted unit renders 4 perks', () => {
  // perkIds layout when fully promoted:
  //   [0] = primary bronze   ("retaliation")
  //   [1] = secondary bronze ("hold_the_line") ← slot 12 / cell 4
  //   [2] = silver           ("last_stand")
  //   [3] = gold             ("guardian_aura")
  const FULL_PERK_IDS = ['retaliation', 'hold_the_line', 'last_stand', 'guardian_aura']

  it('returns 4 perk cells when extraPerkSlots.bronze > 0', () => {
    const soldier = makeSoldier({ perkIds: FULL_PERK_IDS, extraPerkSlots: TWIN_BRONZE_SLOTS })
    const state = makeStateWithSoldier(soldier)
    const perks = perkItems(state.getSelectionSummary().actions)
    expect(perks).toHaveLength(4)
  })

  it('cell 0 (bronze) is the primary bronze perk', () => {
    const soldier = makeSoldier({ perkIds: FULL_PERK_IDS, extraPerkSlots: TWIN_BRONZE_SLOTS })
    const perks = perkItems(makeStateWithSoldier(soldier).getSelectionSummary().actions)
    expect(perks[0].id).toBe('perk-retaliation')
    expect(perks[0].perkRank).toBe('bronze')
  })

  it('cell 1 (silver) reads from perkIds[2] after the remap', () => {
    const soldier = makeSoldier({ perkIds: FULL_PERK_IDS, extraPerkSlots: TWIN_BRONZE_SLOTS })
    const perks = perkItems(makeStateWithSoldier(soldier).getSelectionSummary().actions)
    expect(perks[1].id).toBe('perk-last-stand')
    expect(perks[1].perkRank).toBe('silver')
  })

  it('cell 2 (gold) reads from perkIds[3] after the remap', () => {
    const soldier = makeSoldier({ perkIds: FULL_PERK_IDS, extraPerkSlots: TWIN_BRONZE_SLOTS })
    const perks = perkItems(makeStateWithSoldier(soldier).getSelectionSummary().actions)
    expect(perks[2].id).toBe('perk-guardian-aura')
    expect(perks[2].perkRank).toBe('gold')
  })

  it('cell 3 (slot 12) is the secondary bronze perk from perkIds[1]', () => {
    const soldier = makeSoldier({ perkIds: FULL_PERK_IDS, extraPerkSlots: TWIN_BRONZE_SLOTS })
    const perks = perkItems(makeStateWithSoldier(soldier).getSelectionSummary().actions)
    expect(perks[3].id).toBe('perk-hold-the-line')
    expect(perks[3].perkRank).toBe('bronze')
  })

  it('the total action grid length is 12 (8 pad + 4 perks, no trailing empty)', () => {
    const soldier = makeSoldier({ perkIds: FULL_PERK_IDS, extraPerkSlots: TWIN_BRONZE_SLOTS })
    const { actions } = makeStateWithSoldier(soldier).getSelectionSummary()
    expect(actions).toHaveLength(12)
  })
})

// ─────────────────────────────────────────────────────────────────────────────
// USER CORRECTION — the 4th cell exists from the moment the owner has the
// advancement. Pre-promotion (no perks granted yet) still renders 4 cells,
// all locked.
// ─────────────────────────────────────────────────────────────────────────────
describe('Twin Bronze — 4th cell is a slot, not a conditional on perk count', () => {
  it('pre-promotion (perkIds empty, extraPerkSlots set) renders 4 locked cells', () => {
    const soldier = makeSoldier({ perkIds: [], extraPerkSlots: TWIN_BRONZE_SLOTS })
    const perks = perkItems(makeStateWithSoldier(soldier).getSelectionSummary().actions)
    expect(perks).toHaveLength(4)
    // [0] bronze locked, [1] silver locked, [2] gold locked, [3] bronze (secondary) locked
    expect(perks.map((p) => p.id)).toEqual(['lock', 'lock', 'lock', 'lock'])
    expect(perks.map((p) => p.perkRank)).toEqual(['bronze', 'silver', 'gold', 'bronze'])
  })

  it('bronze rank-up (2 perks, extraPerkSlots set) renders cells [primary, locked-silver, locked-gold, secondary]', () => {
    const soldier = makeSoldier({
      perkIds: ['retaliation', 'hold_the_line'],
      extraPerkSlots: TWIN_BRONZE_SLOTS,
    })
    const perks = perkItems(makeStateWithSoldier(soldier).getSelectionSummary().actions)
    expect(perks).toHaveLength(4)
    expect(perks[0].id).toBe('perk-retaliation')  // primary bronze
    expect(perks[1].id).toBe('lock')              // silver — not yet reached
    expect(perks[1].perkRank).toBe('silver')
    expect(perks[2].id).toBe('lock')              // gold  — not yet reached
    expect(perks[2].perkRank).toBe('gold')
    expect(perks[3].id).toBe('perk-hold-the-line') // secondary bronze
    expect(perks[3].perkRank).toBe('bronze')
  })

  it('silver rank-up (3 perks, extraPerkSlots set) renders cells [primary, silver, locked-gold, secondary]', () => {
    const soldier = makeSoldier({
      perkIds: ['retaliation', 'hold_the_line', 'last_stand'],
      extraPerkSlots: TWIN_BRONZE_SLOTS,
    })
    const perks = perkItems(makeStateWithSoldier(soldier).getSelectionSummary().actions)
    expect(perks).toHaveLength(4)
    expect(perks[0].id).toBe('perk-retaliation')  // primary bronze
    expect(perks[1].id).toBe('perk-last-stand')   // silver  — perkIds[2]
    expect(perks[2].id).toBe('lock')              // gold — not yet reached
    expect(perks[2].perkRank).toBe('gold')
    expect(perks[3].id).toBe('perk-hold-the-line') // secondary bronze — perkIds[1]
  })
})

// ─────────────────────────────────────────────────────────────────────────────
// AC #9 — Standard soldier (no Twin Bronze advancement on owner)
// ─────────────────────────────────────────────────────────────────────────────
describe('Twin Bronze — AC #9: standard soldier renders 3 perks regardless of perkIds length', () => {
  it('unpromoted standard soldier (extraPerkSlots absent, empty perkIds) → 3 cells', () => {
    const soldier = makeSoldier({ perkIds: [] })
    const perks = perkItems(makeStateWithSoldier(soldier).getSelectionSummary().actions)
    expect(perks).toHaveLength(3)
  })

  it('absent perkIds entirely → 3 cells, all locked', () => {
    const soldier = makeSoldier({ perkIds: undefined })
    const perks = perkItems(makeStateWithSoldier(soldier).getSelectionSummary().actions)
    expect(perks).toHaveLength(3)
    expect(perks.map((p) => p.id)).toEqual(['lock', 'lock', 'lock'])
    expect(perks.map((p) => p.perkRank)).toEqual(['bronze', 'silver', 'gold'])
  })

  it('fully promoted standard soldier (3 perks, no extraPerkSlots) → 3 cells, no remap', () => {
    const soldier = makeSoldier({ perkIds: ['retaliation', 'last_stand', 'guardian_aura'] })
    const perks = perkItems(makeStateWithSoldier(soldier).getSelectionSummary().actions)
    expect(perks).toHaveLength(3)
    expect(perks[0].id).toBe('perk-retaliation')
    expect(perks[0].perkRank).toBe('bronze')
    expect(perks[1].id).toBe('perk-last-stand')
    expect(perks[1].perkRank).toBe('silver')
    expect(perks[2].id).toBe('perk-guardian-aura')
    expect(perks[2].perkRank).toBe('gold')
  })

  it('standard soldier grid length is 12 (8 pad + 3 perks + 1 trailing empty)', () => {
    const soldier = makeSoldier({ perkIds: [] })
    const { actions } = makeStateWithSoldier(soldier).getSelectionSummary()
    expect(actions).toHaveLength(12)
  })
})
