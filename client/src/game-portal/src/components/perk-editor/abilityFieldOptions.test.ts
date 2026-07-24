// abilityFieldOptions.test.ts
import { describe, expect, it } from 'vitest'
import { actionsForTarget, fieldsForAction, fieldPreview } from './abilityFieldOptions'

const markerTrap = {
  id: 'marker_trap',
  program: {
    triggers: [{
      id: 'cast', type: 'on_cast_complete',
      actions: [{
        id: 'zone', type: 'create_zone',
        config: {
          name: 'Marker Zone', radius: 115, duration: 12,
          triggers: [{
            id: 'entered', type: 'on_tick',
            actions: [
              { id: 'pick_enemy', type: 'select_targets', target: { radius: 110 } },
              { id: 'mark', type: 'apply_status_duration', config: { name: 'Marked', duration: 4 } },
            ],
          }],
        },
      }],
    }],
  },
}
const defs = { marker_trap: markerTrap }

describe('actionsForTarget', () => {
  it('discovers actions nested inside a zone trigger', () => {
    const ids = actionsForTarget(defs, 'marker_trap').map((a) => a.id)
    expect(ids).toContain('zone')
    expect(ids).toContain('mark')       // two levels down
    expect(ids).toContain('pick_enemy')
  })
  it('returns nothing for a tag: target or unknown ability', () => {
    expect(actionsForTarget(defs, 'tag:trap')).toEqual([])
    expect(actionsForTarget(defs, 'nope')).toEqual([])
  })
})

describe('fieldsForAction', () => {
  it('offers only numeric config keys', () => {
    const fields = fieldsForAction(defs, 'marker_trap', 'mark')
    expect(fields).toContain('duration')
    expect(fields).not.toContain('name')
  })
  it('offers target.radius when the action has a target query radius', () => {
    expect(fieldsForAction(defs, 'marker_trap', 'pick_enemy')).toContain('target.radius')
  })
})

describe('fieldPreview', () => {
  it('reads a multiplier as a signed percent', () => {
    expect(fieldPreview('multiply', 1.35)).toBe('+35%')
    expect(fieldPreview('multiply', 0.8)).toBe('−20%')
  })
  it('reads an add as a signed flat', () => {
    expect(fieldPreview('add', 2)).toBe('+2')
    expect(fieldPreview('add', -1)).toBe('−1')
  })
})
