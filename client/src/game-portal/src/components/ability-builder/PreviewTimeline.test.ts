import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import type { AbilityExecutionTraceEvent } from '@/game/abilities/program/programPreview'
import PreviewTimeline from './PreviewTimeline.vue'

const events: AbilityExecutionTraceEvent[] = [
  { t: 0, type: 'cast_started' },
  { t: 1.4, type: 'damage_applied', path: 'triggers[0].actions[1]', payload: { unit: 3, amount: 140 } },
  { t: 2, type: 'healing_applied', payload: { unit: 4, amount: 30 } },
]

describe('PreviewTimeline', () => {
  it('renders a marker per event', () => {
    const wrapper = mount(PreviewTimeline, { props: { events, duration: 3 } })
    expect(wrapper.findAll('[data-test="preview-timeline-marker"]')).toHaveLength(3)
  })

  it('clicking a marker emits select(index)', async () => {
    const wrapper = mount(PreviewTimeline, { props: { events, duration: 3 } })
    const markers = wrapper.findAll('[data-test="preview-timeline-marker"]')
    await markers[1].trigger('click')
    expect(wrapper.emitted('select')).toEqual([[1]])
  })

  it('renders no markers for an empty trace', () => {
    const wrapper = mount(PreviewTimeline, { props: { events: [], duration: 3 } })
    expect(wrapper.findAll('[data-test="preview-timeline-marker"]')).toHaveLength(0)
  })
})
