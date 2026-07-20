import { describe, it, expect, beforeEach } from 'vitest'
import { getPerkAuraRadius, initPerkDefs, type PerkDef } from './perkDefs'

// Minimal fixture factory — only the fields getPerkAuraRadius cares about
// need real values; everything else is filled with harmless placeholders so
// the fixtures type-check against PerkDef without dragging in catalog JSON.
function makeDef(overrides: Partial<PerkDef> & { id: string }): PerkDef {
  return {
    displayName: overrides.id,
    config: {},
    ...overrides,
  }
}

describe('getPerkAuraRadius', () => {
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

    expect(getPerkAuraRadius('zealous_march')).toBe(192)
  })

  it('falls back to the legacy config-key lookup for a non-migrated perk', () => {
    initPerkDefs([
      makeDef({
        id: 'guardian_aura',
        config: { radius: 150 },
      }),
    ])

    expect(getPerkAuraRadius('guardian_aura')).toBe(150)
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

    expect(getPerkAuraRadius('zealous_march')).toBe(192)
  })

  it('returns null when a perk has neither an aura schema nor a legacy config source', () => {
    initPerkDefs([makeDef({ id: 'no_aura_perk', config: { unrelated: 5 } })])

    expect(getPerkAuraRadius('no_aura_perk')).toBeNull()
  })

  it('returns null for an unknown perk id', () => {
    initPerkDefs([])

    expect(getPerkAuraRadius('does_not_exist')).toBeNull()
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

    expect(getPerkAuraRadius('multi_aura_perk')).toBe(250)
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
    expect(getPerkAuraRadius('guardian_aura')).toBe(75)
  })
})
