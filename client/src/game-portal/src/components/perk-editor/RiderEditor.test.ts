import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import type { ActionSchemaBundle } from '@/game/abilities/program/programSchema'
import type { AbilityBuilderCatalogs } from '@/components/ability-builder/useAbilityBuilder'
import type { AbilityRider } from '@/game/perks/perkEditorForm'
import RiderEditor from './RiderEditor.vue'

// The real shared_suffering rider (server/internal/game/catalog/perks/
// siphoner/shared_suffering/shared_suffering.json) — the canonical
// load/round-trip fixture called out in the task brief.
function sharedSufferingRider(): AbilityRider {
  return {
    target: 'siphon_life',
    trigger: 'on_tick',
    actions: [
      {
        id: 'echo_targets',
        type: 'select_targets',
        target: {
          source: 'all_in_scene',
          origin: 'initial_target_position',
          relations: ['enemy'],
          radius: 120,
          excludeCurrentEvent: true,
          aliveState: 'alive',
        },
      },
      {
        id: 'echo_dmg',
        type: 'deal_damage',
        config: { amountRef: 'trigger_damage', amountMult: 0.4, type: 'shadow' },
      },
    ],
  }
}

function makeSchema(): ActionSchemaBundle {
  return {
    actions: [
      {
        type: 'select_targets',
        runnable: true,
        fields: [
          { key: 'target', label: 'Target Selection', control: 'target_query', section: 'Targeting', targetQueryFields: ['source', 'origin', 'relations', 'radius', 'excludeCurrentEvent', 'aliveState'] },
        ],
      },
      {
        type: 'deal_damage',
        runnable: true,
        fields: [
          { key: 'amount', label: 'Amount', control: 'number', section: 'Properties' },
          { key: 'type', label: 'Damage Type', control: 'enum', section: 'Properties' },
          { key: 'amountRef', label: 'Amount From (context)', control: 'text', section: 'Properties' },
          { key: 'amountMult', label: 'Amount ×', control: 'number', section: 'Properties' },
        ],
      },
    ],
    enums: { triggerTypes: ['on_cast_complete', 'on_tick', 'on_zone_enter'], actionTypes: ['select_targets', 'deal_damage', 'wait'] },
  }
}

function emptyCatalogs(): AbilityBuilderCatalogs {
  return { effects: [], projectiles: [], damageTypes: ['shadow', 'fire', 'physical'], categories: [], autoCastSelectors: [], unitTypes: [] }
}

function mountEditor(rider: AbilityRider, schema: ActionSchemaBundle | null = makeSchema()) {
  return mount(RiderEditor, {
    props: {
      modelValue: rider,
      abilityIds: ['siphon_life', 'frost_bolt'],
      schema,
      catalogs: emptyCatalogs(),
    },
  })
}

describe('RiderEditor', () => {
  it('renders the target/trigger fields and one FlowActionCard per rider action', () => {
    const wrapper = mountEditor(sharedSufferingRider())

    expect((wrapper.find('input[aria-label="Target ability"]').element as HTMLInputElement).value).toBe('siphon_life')
    expect((wrapper.find('select[aria-label="Trigger"]').element as HTMLSelectElement).value).toBe('on_tick')

    const cards = wrapper.findAll('[data-test="flow-action-card"]')
    expect(cards).toHaveLength(2)
    expect(cards[0].text()).toContain('Select Targets')
    expect(cards[1].text()).toContain('Deal Damage')
  })

  it('editing the target ability field emits an updated rider', async () => {
    const wrapper = mountEditor(sharedSufferingRider())
    const input = wrapper.find('input[aria-label="Target ability"]')
    await input.setValue('frost_bolt')

    const emitted = wrapper.emitted('update:modelValue')
    expect(emitted).toBeTruthy()
    const last = emitted![emitted!.length - 1][0] as AbilityRider
    expect(last.target).toBe('frost_bolt')
    // actions/trigger untouched by a target-only edit.
    expect(last.trigger).toBe('on_tick')
    expect(last.actions).toHaveLength(2)
  })

  it('editing the trigger field emits an updated rider', async () => {
    const wrapper = mountEditor(sharedSufferingRider())
    await wrapper.find('select[aria-label="Trigger"]').setValue('on_tick')

    const emitted = wrapper.emitted('update:modelValue')
    const last = emitted![emitted!.length - 1][0] as AbilityRider
    expect(last.trigger).toBe('on_tick')
  })

  it('selecting the deal_damage action shows its schema-driven fields (amountRef/amountMult/type), and editing amountMult writes back', async () => {
    const wrapper = mountEditor(sharedSufferingRider())
    const dealDamageCard = wrapper.findAll('[data-test="flow-action-card"]').find((c) => c.text().includes('Deal Damage'))!
    await dealDamageCard.find('.flow-action__body').trigger('click')

    const inspector = wrapper.find('[data-test="rider-inspector"]')
    expect(inspector.exists()).toBe(true)
    expect(inspector.text()).toContain('deal_damage')

    // amountRef: a plain text field showing the authored value.
    const amountRefInput = inspector.findAll('input[type="text"]').find((i) => (i.element as HTMLInputElement).value === 'trigger_damage')
    expect(amountRefInput).toBeTruthy()

    // amountMult: a plain number field — edit it and confirm the rider's
    // actions array round-trips with the new value, everything else intact.
    const amountMultInput = inspector.findAll('input[type="number"]').find((i) => (i.element as HTMLInputElement).value === '0.4')
    expect(amountMultInput).toBeTruthy()
    ;(amountMultInput!.element as HTMLInputElement).value = '0.6'
    await amountMultInput!.trigger('input')
    await amountMultInput!.trigger('change')

    const emitted = wrapper.emitted('update:modelValue')
    const last = emitted![emitted!.length - 1][0] as AbilityRider
    expect(last.actions[1].config).toEqual({ amountRef: 'trigger_damage', amountMult: 0.6, type: 'shadow' })
    // The other action (select_targets) — and its own target query — is
    // untouched by editing a sibling action's config.
    expect(last.actions[0]).toEqual(sharedSufferingRider().actions[0])
    expect(last.target).toBe('siphon_life')
    expect(last.trigger).toBe('on_tick')
  })

  it('adding an action appends it to the rider and removing an action removes it', async () => {
    const wrapper = mountEditor(sharedSufferingRider())

    await wrapper.find('select[aria-label="New action type"]').setValue('wait')
    await wrapper.find('[data-test="rider-add-action"]').trigger('click')

    let last = wrapper.emitted('update:modelValue')!.at(-1)![0] as AbilityRider
    expect(last.actions).toHaveLength(3)
    expect(last.actions[2].type).toBe('wait')

    // Re-mount with the 3-action rider (mirrors how the parent would pass
    // the emitted value back down) and remove the middle (deal_damage) action.
    await wrapper.setProps({ modelValue: last })
    const dealDamageCard = wrapper.findAll('[data-test="flow-action-card"]').find((c) => c.text().includes('Deal Damage'))!
    await dealDamageCard.find('.flow-action__controls button[title="Delete"]').trigger('click')

    last = wrapper.emitted('update:modelValue')!.at(-1)![0] as AbilityRider
    expect(last.actions.map((a) => a.type)).toEqual(['select_targets', 'wait'])
  })

  it('falls back to a curated trigger/action list when schema is null, still including nested-only types like on_beam_tick', () => {
    const wrapper = mountEditor(sharedSufferingRider(), null)
    const options = wrapper.find('select[aria-label="Trigger"]').findAll('option').map((o) => o.element.value)
    expect(options).toContain('on_tick')
  })
})
