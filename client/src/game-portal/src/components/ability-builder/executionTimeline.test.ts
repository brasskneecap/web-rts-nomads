import { describe, expect, it } from 'vitest'
import type { AbilityProgram } from '@/game/abilities/program/abilityProgram'
import type { AbilityExecutionTraceEvent } from '@/game/abilities/program/programPreview'
import { buildExecutionTimeline } from './executionTimeline'

// Program shape + trace mirror a real Fireball preview run (captured from the
// server harness): On Cast Complete → Launch Projectile → [nested] On Projectile
// Impact → Select Targets, Deal Damage. Trace paths use the id grammar.
function fireballProgram(): AbilityProgram {
  return {
    entry: { type: 'unit', range: 400 },
    triggers: [
      {
        id: 'cast',
        type: 'on_cast_complete',
        actions: [
          {
            id: 'proj',
            type: 'launch_projectile',
            config: {
              triggers: [
                {
                  // Real Fireball: the trigger id is "impact" but the trace roots
                  // its context at the TYPE ("on_projectile_impact.actions[…]"),
                  // exercising the tolerant leaf-action-id matching.
                  id: 'impact',
                  type: 'on_projectile_impact',
                  actions: [
                    { id: 'sel', type: 'select_targets' },
                    { id: 'dmg', type: 'deal_damage', config: { amount: 90, type: 'fire' } },
                  ],
                },
              ],
            },
          },
        ],
      },
    ],
  }
}

function fireballTrace(): AbilityExecutionTraceEvent[] {
  const e = (t: number, type: string, path: string, payload?: Record<string, unknown>) => ({ t, type, path, payload })
  return [
    e(0.65, 'trigger_fired', 'cast', { type: 'on_cast_complete' }),
    e(0.65, 'action_started', 'cast.actions[proj]', { type: 'launch_projectile' }),
    e(0.65, 'projectile_launched', 'cast.actions[proj]', { travelMode: 'to_target' }),
    e(0.65, 'action_completed', 'cast.actions[proj]'),
    e(0.9, 'action_started', 'on_projectile_impact.actions[sel]', { type: 'select_targets' }),
    e(0.9, 'targets_selected', 'on_projectile_impact.actions[sel]', { count: 1 }),
    e(0.9, 'action_completed', 'on_projectile_impact.actions[sel]'),
    e(0.9, 'action_started', 'on_projectile_impact.actions[dmg]', { type: 'deal_damage' }),
    e(0.9, 'damage_applied', 'on_projectile_impact.actions[dmg]', { amount: 90, type: 'fire', unit: 2 }),
    e(0.9, 'action_completed', 'on_projectile_impact.actions[dmg]'),
  ]
}

describe('buildExecutionTimeline', () => {
  it('produces one lane per program node, in flow order with depth', () => {
    const { lanes } = buildExecutionTimeline(fireballProgram(), fireballTrace(), 6)
    expect(lanes.map((l) => l.label)).toEqual([
      'On Cast Complete',
      'Launch Projectile',
      'On Projectile Impact',
      'Select Targets',
      'Deal Damage',
    ])
    expect(lanes.map((l) => l.depth)).toEqual([0, 1, 2, 3, 3])
    expect(lanes.map((l) => l.kind)).toEqual(['trigger', 'action', 'trigger', 'action', 'action'])
  })

  it('gives launch_projectile a travel bar from launch to impact', () => {
    const { lanes } = buildExecutionTimeline(fireballProgram(), fireballTrace(), 6)
    const proj = lanes.find((l) => l.nodeType === 'launch_projectile')!
    expect(proj.startT).toBeCloseTo(0.65)
    expect(proj.endT).toBeCloseTo(0.9) // spans to the nested impact's events
    expect(proj.endT! - proj.startT!).toBeGreaterThan(0) // ⇒ renders a bar
  })

  it('infers a marker for the impact trigger even though it emits no trigger_fired', () => {
    const { lanes } = buildExecutionTimeline(fireballProgram(), fireballTrace(), 6)
    const impact = lanes.find((l) => l.nodeType === 'on_projectile_impact')!
    expect(impact.fired).toBe(true)
    expect(impact.startT).toBeCloseTo(0.9)
    expect(impact.markers).toEqual([0.9])
  })

  it('categorizes instantaneous actions as markers with the right category', () => {
    const { lanes } = buildExecutionTimeline(fireballProgram(), fireballTrace(), 6)
    const dmg = lanes.find((l) => l.nodeType === 'deal_damage')!
    expect(dmg.category).toBe('damage')
    expect(dmg.endT).toBeCloseTo(dmg.startT!) // marker, not a bar
    expect(dmg.markers).toEqual([0.9])
    const sel = lanes.find((l) => l.nodeType === 'select_targets')!
    expect(sel.category).toBe('targets')
  })

  it('fits the axis to content, not the (much longer) run duration', () => {
    const { axisDuration } = buildExecutionTimeline(fireballProgram(), fireballTrace(), 6)
    expect(axisDuration).toBeGreaterThan(0.9)
    expect(axisDuration).toBeLessThan(2) // fit to ~0.9s of activity, not 6s
  })

  it('gives create_zone a duration bar and repeating ticks their own markers', () => {
    const program: AbilityProgram = {
      entry: { type: 'ground_point', range: 500 },
      triggers: [
        {
          id: 'imp',
          type: 'on_cast_complete',
          actions: [
            {
              id: 'cz',
              type: 'create_zone',
              config: {
                triggers: [
                  {
                    id: 'tick',
                    type: 'on_zone_tick',
                    actions: [{ id: 'zd', type: 'deal_damage', config: { amount: 12 } }],
                  },
                ],
              },
            },
          ],
        },
      ],
    }
    const trace: AbilityExecutionTraceEvent[] = [
      { t: 1.0, type: 'zone_created', path: 'imp.actions[cz]', payload: { duration: 4, name: 'Burning' } },
      { t: 1.5, type: 'damage_applied', path: 'tick.actions[zd]', payload: { amount: 12 } },
      { t: 2.0, type: 'damage_applied', path: 'tick.actions[zd]', payload: { amount: 12 } },
      { t: 2.5, type: 'damage_applied', path: 'tick.actions[zd]', payload: { amount: 12 } },
    ]
    const { lanes } = buildExecutionTimeline(program, trace, 6)
    const cz = lanes.find((l) => l.nodeType === 'create_zone')!
    expect(cz.category).toBe('zone')
    expect(cz.startT).toBeCloseTo(1.0)
    expect(cz.endT).toBeCloseTo(5.0) // created + 4s duration
    const zd = lanes.find((l) => l.nodeType === 'deal_damage')!
    expect(zd.markers).toEqual([1.5, 2.0, 2.5]) // one diamond per tick
  })

  it('walks presentation (marker) triggers so a meteor-style zone extends the axis', () => {
    // Meteor: the impact flow lives in a PRESENTATION trigger fired by an
    // animation marker; its trace paths root at marker[impact]. The zone lasts
    // 4s and ticks every 0.5s — all of which must extend the axis past the cast.
    const program: AbilityProgram = {
      entry: { type: 'ground_point', range: 500 },
      triggers: [
        {
          id: 'cast',
          type: 'on_cast_complete',
          actions: [{ id: 'meteor', type: 'play_presentation', config: { presentation: 'p_meteor' } }],
        },
      ],
      presentations: [
        {
          id: 'p_meteor',
          asset: 'meteor',
          position: { key: 'cast_point' },
          triggers: [
            {
              id: 'impact',
              type: 'on_animation_marker',
              timing: { marker: 'impact' },
              actions: [
                { id: 'sel', type: 'select_targets' },
                { id: 'dmg', type: 'deal_damage', config: { amount: 140 } },
                {
                  id: 'zone',
                  type: 'create_zone',
                  config: {
                    triggers: [
                      {
                        id: 'burn',
                        type: 'on_zone_tick',
                        actions: [{ id: 'bdmg', type: 'deal_damage', config: { amount: 12 } }],
                      },
                    ],
                  },
                },
              ],
            },
          ],
        },
      ],
    }
    const e = (t: number, type: string, path: string, payload?: Record<string, unknown>) => ({ t, type, path, payload })
    const trace: AbilityExecutionTraceEvent[] = [
      e(0.8, 'trigger_fired', 'cast', { type: 'on_cast_complete' }),
      e(0.8, 'presentation_played', 'cast.actions[meteor]', { asset: 'meteor' }),
      e(1.4, 'targets_selected', 'marker[impact].actions[sel]', { count: 2 }),
      e(1.4, 'damage_applied', 'marker[impact].actions[dmg]', { amount: 140 }),
      e(1.4, 'zone_created', 'marker[impact].actions[zone]', { duration: 4, name: 'Burning Crater' }),
      e(1.45, 'damage_applied', 'burn.actions[bdmg]', { amount: 12 }),
      e(1.9, 'damage_applied', 'burn.actions[bdmg]', { amount: 12 }),
      e(2.4, 'damage_applied', 'burn.actions[bdmg]', { amount: 12 }),
      e(3.9, 'damage_applied', 'burn.actions[bdmg]', { amount: 12 }),
    ]
    const { lanes, axisDuration } = buildExecutionTimeline(program, trace, 6)

    // The presentation's impact subtree now has lanes.
    const byType = (t: string) => lanes.find((l) => l.nodeType === t)
    expect(byType('on_animation_marker')?.fired).toBe(true)
    expect(byType('create_zone')).toBeTruthy()

    // The zone bar spans its full 4s duration...
    const zone = byType('create_zone')!
    expect(zone.startT).toBeCloseTo(1.4)
    expect(zone.endT).toBeCloseTo(5.4)
    // ...and the axis covers it (was collapsing to ~1s before presentations were walked).
    expect(axisDuration).toBeGreaterThanOrEqual(5.4)

    // The burn-tick damage lane carries a marker per tick.
    const burnDmg = lanes.filter((l) => l.nodeType === 'deal_damage').find((l) => l.markers.length > 1)
    expect(burnDmg?.markers).toEqual([1.45, 1.9, 2.4, 3.9])
  })

  it('returns no lanes for a null program', () => {
    expect(buildExecutionTimeline(null, [], 3).lanes).toEqual([])
  })
})
