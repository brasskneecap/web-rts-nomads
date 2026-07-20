import { describe, it, expect, beforeEach } from 'vitest'
import { getPerkAuraRing, initPerkDefs, type PerkDef } from './perkDefs'

// Minimal fixture factory — only the fields getPerkAuraRing cares about
// need real values; everything else is filled with harmless placeholders so
// the fixtures type-check against PerkDef without dragging in catalog JSON.
function makeDef(overrides: Partial<PerkDef> & { id: string }): PerkDef {
  return {
    displayName: overrides.id,
    config: {},
    ...overrides,
  }
}

describe('getPerkAuraRing', () => {
  beforeEach(() => {
    initPerkDefs([])
  })

  it('resolves a migrated perk from auras[].radius, not a legacy config key', () => {
    initPerkDefs([
      makeDef({
        id: 'zealous_march',
        // Legacy key intentionally absent — this perk is fully migrated.
        config: {},
        auras: [
          {
            radius: 192,
            targets: 'allies',
            includeSelf: true,
            statModifiers: [{ stat: 'moveSpeed', op: 'add', value: 0.2 }],
          },
        ],
      }),
    ])

    expect(getPerkAuraRing('zealous_march')).toEqual({ radius: 192, color: undefined })
  })

  it('falls back to the legacy config-key lookup for a non-migrated perk', () => {
    initPerkDefs([
      makeDef({
        id: 'guardian_aura',
        config: { radius: 150 },
      }),
    ])

    expect(getPerkAuraRing('guardian_aura')).toEqual({ radius: 150 })
  })

  it('prefers the aura schema over a legacy config key when a perk carries both', () => {
    // Anti-regression case: once a perk is migrated, the server may leave the
    // legacy config key in place for a transition period (or delete it
    // later) — the aura schema must win either way so the ring never lags
    // behind the gameplay-authoritative radius.
    initPerkDefs([
      makeDef({
        id: 'zealous_march',
        config: { radiusPixels: 999 },
        auras: [
          {
            radius: 192,
            targets: 'allies',
            statModifiers: [],
          },
        ],
      }),
    ])

    expect(getPerkAuraRing('zealous_march')).toEqual({ radius: 192, color: undefined })
  })

  it('returns null when a perk has neither an aura schema nor a legacy config source', () => {
    initPerkDefs([makeDef({ id: 'no_aura_perk', config: { unrelated: 5 } })])

    expect(getPerkAuraRing('no_aura_perk')).toBeNull()
  })

  it('returns null for an unknown perk id', () => {
    initPerkDefs([])

    expect(getPerkAuraRing('does_not_exist')).toBeNull()
  })

  it('picks the largest radius when a perk carries multiple auras', () => {
    initPerkDefs([
      makeDef({
        id: 'multi_aura_perk',
        auras: [
          { radius: 100, targets: 'allies', statModifiers: [] },
          { radius: 250, targets: 'enemies', statModifiers: [] },
          { radius: 180, targets: 'allies', statModifiers: [] },
        ],
      }),
    ])

    expect(getPerkAuraRing('multi_aura_perk')).toEqual({ radius: 250, color: undefined })
  })

  it('falls back to the legacy config path when auras is present but empty', () => {
    initPerkDefs([
      makeDef({
        id: 'guardian_aura',
        auras: [],
        config: { radius: 75 },
      }),
    ])
    // An empty auras array must be treated as "no aura schema" and fall
    // through to the legacy AURA_RADIUS_SOURCES lookup, not short-circuit.
    expect(getPerkAuraRing('guardian_aura')).toEqual({ radius: 75 })
  })

  it('returns the ringColor authored on the winning aura entry', () => {
    initPerkDefs([
      makeDef({
        id: 'zealous_march',
        auras: [{ radius: 192, targets: 'allies', statModifiers: [], ringColor: '#38bdf8' }],
      }),
    ])

    expect(getPerkAuraRing('zealous_march')).toEqual({ radius: 192, color: '#38bdf8' })
  })

  it('takes color from the SAME aura entry whose radius won, never a different one', () => {
    // The smaller aura authors a distinct ringColor; the larger (winning)
    // aura authors none. The result must carry the LARGER aura's radius
    // together with ITS OWN (absent) color — never the smaller aura's color
    // grafted onto the larger aura's radius.
    initPerkDefs([
      makeDef({
        id: 'multi_aura_perk',
        auras: [
          { radius: 100, targets: 'allies', statModifiers: [], ringColor: '#ff0000' },
          { radius: 250, targets: 'enemies', statModifiers: [] },
        ],
      }),
    ])

    expect(getPerkAuraRing('multi_aura_perk')).toEqual({ radius: 250, color: undefined })
  })

  it('legacy config-key perks never carry a color, even when active-gated', () => {
    initPerkDefs([makeDef({ id: 'guardian_aura', config: { radius: 150 } })])

    const ring = getPerkAuraRing('guardian_aura')
    expect(ring).not.toBeNull()
    expect(ring?.color).toBeUndefined()
  })
})
