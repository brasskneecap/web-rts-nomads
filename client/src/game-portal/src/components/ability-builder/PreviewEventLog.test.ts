import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import type { AbilityExecutionTraceEvent } from '@/game/abilities/program/programPreview'
import PreviewEventLog from './PreviewEventLog.vue'

const events: AbilityExecutionTraceEvent[] = [
  { t: 0, type: 'trigger_fired', path: 'triggers[0]', payload: { type: 'on_cast_complete' } },
  {
    t: 1.4,
    type: 'damage_applied',
    path: 'triggers[0].actions[1]',
    payload: { unit: 3, amount: 140, type: 'fire' },
  },
  { t: 1.4, type: 'action_skipped', path: 'triggers[0].actions[2]', payload: { type: 'play_sound' } },
  { t: 2.0, type: 'healing_applied', payload: { unit: 4, amount: 30 } },
]

describe('PreviewEventLog', () => {
  it('renders rows in trace order', () => {
    const wrapper = mount(PreviewEventLog, { props: { events } })
    const rows = wrapper.findAll('[data-test="preview-log-row"]')
    expect(rows).toHaveLength(4)
    expect(rows[0].text()).toContain('Trigger Fired')
    expect(rows[1].text()).toContain('Damage Applied')
    expect(rows[1].text()).toContain('140')
    expect(rows[3].text()).toContain('Healing Applied')
  })

  it('filter tabs narrow by type', async () => {
    const wrapper = mount(PreviewEventLog, { props: { events } })
    await wrapper.find('[data-tab="damage"]').trigger('click')
    const rows = wrapper.findAll('[data-test="preview-log-row"]')
    expect(rows).toHaveLength(1)
    expect(rows[0].text()).toContain('Damage Applied')
  })

  it('clicking a row with a path emits selectNode(path) and select(index)', async () => {
    const wrapper = mount(PreviewEventLog, { props: { events } })
    const rows = wrapper.findAll('[data-test="preview-log-row"]')
    await rows[1].find('button').trigger('click')
    expect(wrapper.emitted('selectNode')).toEqual([['triggers[0].actions[1]']])
    expect(wrapper.emitted('select')).toEqual([[1]])
  })

  it('a row with no path does not emit selectNode when clicked', async () => {
    const wrapper = mount(PreviewEventLog, { props: { events } })
    const rows = wrapper.findAll('[data-test="preview-log-row"]')
    // healing_applied (index 3) has no `path` in this fixture.
    await rows[3].find('button').trigger('click')
    expect(wrapper.emitted('selectNode')).toBeUndefined()
  })

  it('action_skipped row shows a deferred style/tag', () => {
    const wrapper = mount(PreviewEventLog, { props: { events } })
    const rows = wrapper.findAll('[data-test="preview-log-row"]')
    expect(rows[2].classes()).toContain('pv-log__row--skipped')
    expect(rows[2].find('[data-test="preview-log-deferred"]').exists()).toBe(true)
  })
})
