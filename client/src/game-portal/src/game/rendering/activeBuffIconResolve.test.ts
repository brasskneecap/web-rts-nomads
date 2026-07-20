// activeBuffIconResolve.test.ts — resolveActiveBuffIconPath (CanvasRenderer.ts),
// the pure lookup drawUnitActiveBuffs delegates to. Factored out (rather than
// asserting via a full CanvasRenderer + fake 2D context, see
// CanvasRenderer.clock.test.ts) because there's nothing canvas-shaped about
// this logic — it's a two-map lookup, and testing it directly is both
// simpler and immune to fake-context drift.
import { afterEach, describe, expect, it } from 'vitest'
import { resolveActiveBuffIconPath } from './CanvasRenderer'
import { ACTION_ICON_MAP } from '../maps/actionIconDefs'
import { PERK_DEF_MAP, type PerkDef } from '../maps/perkDefs'

function makePerkDef(overrides: Partial<PerkDef> & { id: string }): PerkDef {
  return { displayName: overrides.id, config: {}, ...overrides }
}

afterEach(() => {
  // Both maps are module-level `let`s populated by a startup loader — reset
  // to empty between tests so one test's fixtures never leak into another.
  PERK_DEF_MAP.clear()
  ACTION_ICON_MAP.clear()
})

describe('resolveActiveBuffIconPath', () => {
  it('resolves a known PERK id via PERK_DEF_MAP -> ACTION_ICON_MAP indirection (existing perk-sourced buffs)', () => {
    PERK_DEF_MAP.set('bloodlust', makePerkDef({ id: 'bloodlust', icon: 'perk-bloodlust' }))
    ACTION_ICON_MAP.set('perk-bloodlust', 'M0 0 L1 1')

    expect(resolveActiveBuffIconPath('bloodlust')).toBe('M0 0 L1 1')
  })

  it('falls back to a DIRECT ACTION_ICON_MAP lookup when the id is not a known perk id (authored apply_status iconKind:"buff")', () => {
    // No PERK_DEF_MAP entry for this id at all — an authored status with no
    // perk backing it, exactly what apply_status(iconKind:"buff") emits.
    ACTION_ICON_MAP.set('buff-warded', 'M2 2 L3 3')

    expect(resolveActiveBuffIconPath('buff-warded')).toBe('M2 2 L3 3')
  })

  it('prefers the perk indirection over the direct id when a perk id happens to collide with an icon id', () => {
    PERK_DEF_MAP.set('buff-warded', makePerkDef({ id: 'buff-warded', icon: 'perk-other' }))
    ACTION_ICON_MAP.set('perk-other', 'M4 4 L5 5')
    ACTION_ICON_MAP.set('buff-warded', 'M9 9 L9 9') // would resolve too, but shouldn't be picked

    expect(resolveActiveBuffIconPath('buff-warded')).toBe('M4 4 L5 5')
  })

  it('returns undefined when neither lookup resolves', () => {
    expect(resolveActiveBuffIconPath('unknown-id')).toBeUndefined()
  })
})
