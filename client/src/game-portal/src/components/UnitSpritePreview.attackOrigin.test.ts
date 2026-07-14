import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UnitSpritePreview from './UnitSpritePreview.vue'
import {
  clearRuntimeSpriteSets,
  registerRuntimeSpriteSet,
  type UnitSpriteSet,
} from '@/game/rendering/unitSprites'

// The component starts a requestAnimationFrame loop on setup; happy-dom may not
// provide rAF, and we don't need the canvas to run for the emit path.
beforeEach(() => {
  vi.stubGlobal('requestAnimationFrame', () => 1)
  vi.stubGlobal('cancelAnimationFrame', () => {})
  const fake: UnitSpriteSet = {
    key: 'testunit',
    size: { width: 64, height: 64 },
    rotations: {},
    animations: new Map(),
    beamOrigin: { x: 0, y: 0 },
  }
  registerRuntimeSpriteSet(fake)
})

afterEach(() => {
  clearRuntimeSpriteSets()
  vi.restoreAllMocks()
})

describe('UnitSpritePreview attack-origin authoring', () => {
  it('emits update:attackOrigin when the X field is edited', async () => {
    const wrapper = mount(UnitSpritePreview, { props: { unitKey: 'testunit' } })
    await flushPromises()

    // Open the Attack Origin section.
    const toggle = wrapper.findAll('button').find((b) => b.text().includes('Attack Origin'))
    expect(toggle, 'Attack Origin toggle should render for a unit with art').toBeTruthy()
    await toggle!.trigger('click')

    // The X numeric input is the first number input inside the origin body.
    const numberInputs = wrapper.findAll('.sprite-preview__origin-inputs input[type="number"]')
    expect(numberInputs.length, 'origin X/Y inputs should render').toBeGreaterThanOrEqual(2)
    const xInput = numberInputs[0]

    await xInput.setValue('25')

    const emits = wrapper.emitted('update:attackOrigin')
    expect(emits, 'editing X must emit update:attackOrigin').toBeTruthy()
    const last = emits![emits!.length - 1][0] as any
    expect(last?.byFacing?.south?.x).toBe(25)
  })
})

import { defineComponent, ref } from 'vue'

describe('parent v-model:attack-origin binding (mirrors the panel)', () => {
  it('round-trips: editing the child updates the parent ref AND the child reflects it', async () => {
    const Parent = defineComponent({
      components: { UnitSpritePreview },
      setup() {
        // Mirror the panel: a ref holding the form object, attackOrigin a member.
        const form = ref<{ type: string; attackOrigin?: any }>({ type: 'testunit', attackOrigin: undefined })
        return { form }
      },
      template: `<UnitSpritePreview :unit-key="form.type" v-model:attack-origin="form.attackOrigin" />`,
    })

    const wrapper = mount(Parent)
    await flushPromises()

    const toggle = wrapper.findAll('button').find((b) => b.text().includes('Attack Origin'))
    await toggle!.trigger('click')
    const xInput = wrapper.findAll('.sprite-preview__origin-inputs input[type="number"]')[0]
    await xInput.setValue('25')
    await flushPromises()

    // Did the PARENT ref actually receive the update?
    expect((wrapper.vm as any).form.attackOrigin?.byFacing?.south?.x).toBe(25)
    // And does the child now display it (proving the prop round-tripped back)?
    expect((xInput.element as HTMLInputElement).value).toBe('25')
  })
})

import Fixture from './__attackOriginBindingFixture.vue'

describe('SFC <script setup> v-model:attack-origin binding (faithful panel repro)', () => {
  it('round-trips through a real SFC ref-member v-model', async () => {
    const wrapper = mount(Fixture)
    await flushPromises()

    const toggle = wrapper.findAll('button').find((b) => b.text().includes('Attack Origin'))
    await toggle!.trigger('click')
    const xInput = wrapper.findAll('.sprite-preview__origin-inputs input[type="number"]')[0]
    await xInput.setValue('25')
    await flushPromises()

    // If SFC compilation of the ref-member v-model is wrong, this stays undefined.
    expect((wrapper.vm as any).form.attackOrigin?.byFacing?.south?.x).toBe(25)
    expect((xInput.element as HTMLInputElement).value).toBe('25')
  })
})
