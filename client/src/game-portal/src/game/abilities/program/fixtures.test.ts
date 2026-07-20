import { describe, it, expect } from 'vitest'
import { parseProgram, serializeProgram } from './abilityProgram'

// The `program` sub-object of the canonical design-doc v2 Greater Heal
// fixture. Kept byte-identical to the Go fixture (greaterHealV2JSON) so both
// sides lock the same JSON shape to their types.
const greaterHealProgram = {
  entry: { type: 'unit', relations: ['self', 'ally'], range: 'match_attack_range' },
  triggers: [
    {
      id: 't_cast',
      type: 'on_cast_complete',
      actions: [
        {
          id: 'a_select',
          type: 'select_targets',
          target: {
            source: 'all_in_scene',
            origin: 'caster',
            relations: ['self', 'ally'],
            radius: -1,
            ordering: 'lowest_health_percentage',
            maxCount: 3,
            includeInitialTarget: true,
          },
          outputs: { targets: 'healTargets' },
        },
        {
          id: 'a_heal',
          type: 'restore_health',
          input: { targets: { key: 'healTargets' } },
          config: { amount: 15, school: 'holy' },
        },
        {
          id: 'a_vfx',
          type: 'play_presentation',
          input: { attach: { key: 'healTargets' } },
          config: { asset: 'healing_glow', oncePerTarget: true },
        },
      ],
    },
  ],
}

// The `program` sub-object of the canonical design-doc v2 Meteor fixture.
// Kept byte-identical to the Go fixture (meteorV2JSON).
const meteorProgram = {
  entry: { type: 'ground_point', relations: ['enemy'], range: 400 },
  triggers: [
    {
      id: 't_cast',
      type: 'on_cast_complete',
      actions: [
        {
          id: 'a_meteor',
          type: 'play_presentation',
          config: {
            asset: 'meteor',
            position: { key: 'castPoint' },
            scale: 3,
            renderLayer: 'in_front_of_units',
            presentationId: 'p_meteor',
          },
        },
      ],
    },
  ],
  presentations: [
    {
      id: 'p_meteor',
      asset: 'meteor',
      position: { key: 'castPoint' },
      scale: 3,
      renderLayer: 'in_front_of_units',
      triggers: [
        {
          id: 't_cross',
          type: 'on_animation_marker',
          timing: { marker: 'cross_unit_plane' },
          actions: [
            { id: 'a_layer', type: 'change_render_layer', config: { layer: 'behind_units' } },
          ],
        },
        {
          id: 't_impact',
          type: 'on_animation_marker',
          timing: { marker: 'impact' },
          actions: [
            {
              id: 'a_sel',
              type: 'select_targets',
              target: {
                source: 'all_in_scene',
                origin: 'impact_position',
                radius: 230,
                relations: ['enemy'],
              },
              outputs: { targets: 'hitEnemies' },
            },
            {
              id: 'a_dmg',
              type: 'deal_damage',
              input: { targets: { key: 'hitEnemies' } },
              config: { amount: 140, type: 'fire' },
            },
            {
              id: 'a_zone',
              type: 'create_zone',
              config: {
                name: 'Burning Crater',
                position: { key: 'impactPosition' },
                anchor: 'ground',
                radius: 120,
                duration: 4,
                tickInterval: 0.5,
                owner: { key: 'caster' },
                presentation: 'burning_crater',
                triggers: [
                  {
                    id: 't_burn',
                    type: 'on_tick',
                    timing: { tickInterval: 0.5 },
                    actions: [
                      {
                        id: 'a_bsel',
                        type: 'select_targets',
                        target: {
                          source: 'all_in_scene',
                          origin: 'zone_center',
                          radius: 120,
                          relations: ['enemy'],
                        },
                        outputs: { targets: 'burnHits' },
                      },
                      {
                        id: 'a_bdmg',
                        type: 'deal_damage',
                        input: { targets: { key: 'burnHits' } },
                        config: { amount: 12, type: 'fire' },
                      },
                    ],
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

describe('v2 fixtures', () => {
  it('parses greater_heal program', () => {
    const p = parseProgram(greaterHealProgram)
    expect(p.triggers[0].type).toBe('on_cast_complete')
    expect(p.triggers[0].actions).toHaveLength(3)
  })
  it('parses meteor program and preserves nested zone config', () => {
    const p = parseProgram(meteorProgram)
    expect(p.presentations?.[0].triggers?.length).toBe(2)
    // nested create_zone config survives verbatim inside the impact trigger
    const impact = p.presentations![0].triggers!.find((t) => t.timing?.marker === 'impact')!
    const zoneAction = impact.actions.find((a) => a.type === 'create_zone')!
    expect(JSON.stringify(zoneAction.config)).toContain('Burning Crater')
    // round-trip preserves it
    expect(JSON.stringify(serializeProgram(p))).toContain('burning_crater')
  })
})
