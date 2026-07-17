import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import ConvertDialog from './ConvertDialog.vue'
import { AbilityBuilderKey } from './AbilityBuilderContext'

function makeBuilderStub(overrides: {
  busy?: boolean
  runnable?: boolean
  isLegacy?: boolean
  warnings?: string[]
  saveError?: string
  convertImpl?: () => Promise<void>
} = {}) {
  const isLegacy = ref(overrides.isLegacy ?? true)
  const runnable = ref(overrides.runnable ?? true)
  const warnings = ref<string[]>(overrides.warnings ?? [])
  const saveError = ref(overrides.saveError ?? '')
  const busy = ref(overrides.busy ?? false)

  const convert = vi.fn(async () => {
    if (overrides.convertImpl) {
      await overrides.convertImpl()
      return
    }
    isLegacy.value = false
  })

  return { busy, runnable, isLegacy, warnings, saveError, convert }
}

function mountDialog(builder: ReturnType<typeof makeBuilderStub>, open = true) {
  return mount(ConvertDialog, {
    props: { open },
    global: { provide: { [AbilityBuilderKey as unknown as string]: builder } },
  })
}

describe('ConvertDialog', () => {
  it('renders nothing when closed', () => {
    const builder = makeBuilderStub()
    const wrapper = mountDialog(builder, false)
    expect(wrapper.find('[data-test="convert-dialog-overlay"]').exists()).toBe(false)
  })

  it('confirming calls builder.convert', async () => {
    const builder = makeBuilderStub()
    const wrapper = mountDialog(builder)

    await wrapper.find('[data-test="convert-confirm"]').trigger('click')

    expect(builder.convert).toHaveBeenCalledTimes(1)
  })

  it('shows warnings after a successful convert', async () => {
    const builder = makeBuilderStub({ warnings: ['channel mechanics approximated as a single tick'] })
    const wrapper = mountDialog(builder)

    await wrapper.find('[data-test="convert-confirm"]').trigger('click')
    await wrapper.vm.$nextTick()

    const warningsBlock = wrapper.find('[data-test="convert-warnings"]')
    expect(warningsBlock.exists()).toBe(true)
    expect(warningsBlock.text()).toContain('channel mechanics approximated as a single tick')
  })

  it('shows a lossless hint when convert reports no warnings', async () => {
    const builder = makeBuilderStub({ warnings: [] })
    const wrapper = mountDialog(builder)

    await wrapper.find('[data-test="convert-confirm"]').trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[data-test="convert-warnings"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('No degradation notices')
  })

  it('emphasizes degradation when the converted ability is not runnable', async () => {
    const builder = makeBuilderStub({ runnable: false })
    const wrapper = mountDialog(builder)

    await wrapper.find('[data-test="convert-confirm"]').trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[data-test="convert-degraded"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="convert-degraded"]').text()).toContain('display-only')
  })

  it('does not emphasize degradation when the converted ability is runnable', async () => {
    const builder = makeBuilderStub({ runnable: true })
    const wrapper = mountDialog(builder)

    await wrapper.find('[data-test="convert-confirm"]').trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[data-test="convert-degraded"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Converted successfully')
  })

  it('disables the confirm button while busy', () => {
    const builder = makeBuilderStub({ busy: true })
    const wrapper = mountDialog(builder)

    expect(wrapper.find('[data-test="convert-confirm"]').attributes('disabled')).toBeDefined()
  })

  it('disables the close button and blocks backdrop-close while busy', async () => {
    const builder = makeBuilderStub({ busy: true })
    const wrapper = mountDialog(builder)

    expect(wrapper.find('[data-test="convert-close"]').attributes('disabled')).toBeDefined()

    await wrapper.find('[data-test="convert-close"]').trigger('click')
    await wrapper.find('[data-test="convert-dialog-overlay"]').trigger('click')

    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('does not show a stale saveError left over from a prior attempt on a fresh open', () => {
    const builder = makeBuilderStub({ saveError: 'Convert this ability to composable before saving.' })
    const wrapper = mountDialog(builder)

    expect(wrapper.find('[data-test="convert-confirm"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('Convert this ability to composable before saving.')
  })

  it('stays on the confirm step and surfaces the error if convert fails', async () => {
    const builder = makeBuilderStub({
      convertImpl: async () => {
        builder2SaveError()
      },
    })
    // Simulate convert() leaving isLegacy true + setting saveError, as the
    // real composable does on a caught exception.
    function builder2SaveError() {
      builder.saveError.value = 'Failed to convert ability: 500'
    }
    const wrapper = mountDialog(builder)

    await wrapper.find('[data-test="convert-confirm"]').trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[data-test="convert-confirm"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Failed to convert ability: 500')
    expect(wrapper.find('[data-test="convert-warnings"]').exists()).toBe(false)
  })

  it('closing emits close and clicking the backdrop closes too', async () => {
    const builder = makeBuilderStub()
    const wrapper = mountDialog(builder)

    await wrapper.find('[data-test="convert-close"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)

    await wrapper.find('[data-test="convert-dialog-overlay"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(2)
  })
})
