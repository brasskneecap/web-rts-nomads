import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref, shallowRef } from 'vue'
import type { AbilityEditorForm } from '@/game/abilities/abilityEditorForm'
import type { AbilityProgram } from '@/game/abilities/program/abilityProgram'
import type { PreviewFrame, PreviewRequest, PreviewResult } from '@/game/abilities/program/programPreview'
import type { MatchSnapshotMessage } from '@/game/network/protocol'
import AbilityPreviewPanel from './AbilityPreviewPanel.vue'
import { AbilityBuilderKey } from './AbilityBuilderContext'
import type { NodeRef } from './programTree'

const runAbilityPreviewMock = vi.fn<(req: PreviewRequest) => Promise<PreviewResult>>()

vi.mock('@/game/abilities/abilityEditorApi', () => ({
  runAbilityPreview: (req: PreviewRequest) => runAbilityPreviewMock(req),
}))

function makeForm(): AbilityEditorForm {
  return { id: 'fireball', displayName: 'Fireball', remainder: {} }
}

function makeProgram(): AbilityProgram {
  return {
    entry: { type: 'unit', range: 300 },
    triggers: [
      { id: 't1', type: 'on_cast_complete', actions: [{ id: 'a1', type: 'deal_damage', config: { amount: 10 } }] },
    ],
  }
}

// makeFrames: minimal PreviewFrame stubs at the real 20fps capture cadence
// (PREVIEW_FRAME_DT_SECONDS) — enough for AbilityPreviewCanvas's scrub range
// and the panel's t -> tick mapping. jsdom has no real 2D canvas context, so
// AbilityPreviewCanvas's onMounted bails before ever reading `snapshot`'s
// contents — the cast below is safe for test purposes only.
function makeFrames(n: number): PreviewFrame[] {
  return Array.from({ length: n }, (_, i) => ({
    tick: i,
    t: i * 0.05,
    snapshot: {} as MatchSnapshotMessage,
  }))
}

function makeResult(overrides: Partial<PreviewResult> = {}): PreviewResult {
  return {
    trace: [
      { t: 0, type: 'trigger_fired', path: 'triggers[0]', payload: { type: 'on_cast_complete' } },
      {
        t: 0.5,
        type: 'damage_applied',
        path: 'triggers[0].actions[0]',
        payload: { unit: 3, amount: 10 },
      },
    ],
    units: [{ index: 0, team: 'enemy', hpBefore: 200, hpAfter: 190 }],
    casterManaSpent: 15,
    runnable: true,
    warnings: [],
    frames: [],
    ...overrides,
  }
}

function makeBuilderStub() {
  return {
    form: shallowRef<AbilityEditorForm>(makeForm()),
    program: shallowRef<AbilityProgram>(makeProgram()),
    busy: ref(false),
    select: vi.fn(),
  }
}

// mountPanel flushes once before returning: PreviewSceneControls emits its
// initial scene from an `immediate: true` watch during its OWN setup, which
// schedules a reactive update on the parent's `scene` ref — that update
// flushes to the DOM asynchronously (Vue's scheduler), not within the same
// synchronous mount() call. Without this flush, `runDisabled` would still
// read as true (the child's default scene hasn't landed in the DOM yet) and
// a synchronous `.trigger('click')` on the (still natively `disabled`) Run
// button would silently no-op, exactly like a real disabled <button>.
async function mountPanel(builder: ReturnType<typeof makeBuilderStub>) {
  const wrapper = mount(AbilityPreviewPanel, {
    global: { provide: { [AbilityBuilderKey as unknown as string]: builder } },
  })
  await flushPromises()
  return wrapper
}

afterEach(() => {
  vi.restoreAllMocks()
  runAbilityPreviewMock.mockReset()
})

describe('AbilityPreviewPanel', () => {
  it('mounts the canvas immediately, before any run, showing its idle placeholder', async () => {
    const builder = makeBuilderStub()
    const wrapper = await mountPanel(builder)

    const canvas = wrapper.find('[data-test="ability-preview-canvas"]')
    expect(canvas.exists()).toBe(true)
    expect(wrapper.find('[data-test="preview-canvas-empty"]').exists()).toBe(true)
    // Playback controls are inert with no frames.
    expect(wrapper.find('[data-test="preview-play-toggle"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-test="preview-timeline"]').exists()).toBe(false)
  })

  it('renders the canvas ahead of the Run Preview button in the DOM (renderer is top-most)', async () => {
    const builder = makeBuilderStub()
    const wrapper = await mountPanel(builder)

    const html = wrapper.html()
    const canvasIdx = html.indexOf('data-test="ability-preview-canvas"')
    const runButtonIdx = html.indexOf('data-test="preview-run-button"')
    expect(canvasIdx).toBeGreaterThanOrEqual(0)
    expect(runButtonIdx).toBeGreaterThan(canvasIdx)
  })

  it('Run builds a request from form+program+scene and calls the API', async () => {
    runAbilityPreviewMock.mockResolvedValue(makeResult())
    const builder = makeBuilderStub()
    const wrapper = await mountPanel(builder)

    await wrapper.find('[data-test="preview-run-button"]').trigger('click')
    await flushPromises()

    expect(runAbilityPreviewMock).toHaveBeenCalledTimes(1)
    const req = runAbilityPreviewMock.mock.calls[0][0]
    expect(req.ability.id).toBe('fireball')
    expect(req.ability.schemaVersion).toBe(2)
    expect(req.ability.program).toBeTruthy()
    // Scene fields (from PreviewSceneControls' default scene) rode along.
    expect(Array.isArray(req.units)).toBe(true)
    expect(req.units.length).toBeGreaterThan(0)
    expect(typeof req.seed).toBe('number')
    expect(typeof req.durationSeconds).toBe('number')
  })

  it('renders timeline + log + summary once a result comes back', async () => {
    runAbilityPreviewMock.mockResolvedValue(makeResult())
    const builder = makeBuilderStub()
    const wrapper = await mountPanel(builder)

    await wrapper.find('[data-test="preview-run-button"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="preview-timeline"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="preview-event-log"]').exists()).toBe(true)
    const summary = wrapper.find('[data-test="preview-summary"]')
    expect(summary.exists()).toBe(true)
    expect(summary.text()).toContain('15') // caster mana spent
    expect(summary.text()).toContain('200')
    expect(summary.text()).toContain('190')
  })

  it('feeds populated frames to the (already-mounted) canvas once a result arrives, clearing its idle placeholder', async () => {
    runAbilityPreviewMock.mockResolvedValue(makeResult({ frames: makeFrames(5) }))
    const builder = makeBuilderStub()
    const wrapper = await mountPanel(builder)

    // Canvas is present from the start, showing its idle placeholder.
    expect(wrapper.find('[data-test="ability-preview-canvas"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="preview-canvas-empty"]').exists()).toBe(true)

    await wrapper.find('[data-test="preview-run-button"]').trigger('click')
    await flushPromises()

    // Same canvas instance now has real frames — placeholder gone, controls
    // enabled, and the rest of the panel (timeline/log/summary) still works.
    expect(wrapper.find('[data-test="preview-canvas-empty"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="preview-play-toggle"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('[data-test="preview-scrub"]').attributes('max')).toBe('4')
    expect(wrapper.find('[data-test="preview-timeline"]').exists()).toBe(true)
  })

  it('shows the warnings banner when runnable is false', async () => {
    runAbilityPreviewMock.mockResolvedValue(
      makeResult({ runnable: false, warnings: ['create_zone has no executor yet'] }),
    )
    const builder = makeBuilderStub()
    const wrapper = await mountPanel(builder)

    await wrapper.find('[data-test="preview-run-button"]').trigger('click')
    await flushPromises()

    const banner = wrapper.find('[data-test="preview-warnings-banner"]')
    expect(banner.exists()).toBe(true)
    expect(banner.text()).toContain('create_zone has no executor yet')
  })

  it('does not show the warnings banner for a clean, fully-runnable result', async () => {
    runAbilityPreviewMock.mockResolvedValue(makeResult())
    const builder = makeBuilderStub()
    const wrapper = await mountPanel(builder)

    await wrapper.find('[data-test="preview-run-button"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="preview-warnings-banner"]').exists()).toBe(false)
  })

  it('selecting a log row with a path calls builder.select with the resolved NodeRef', async () => {
    runAbilityPreviewMock.mockResolvedValue(makeResult())
    const builder = makeBuilderStub()
    const wrapper = await mountPanel(builder)

    await wrapper.find('[data-test="preview-run-button"]').trigger('click')
    await flushPromises()

    const rows = wrapper.findAll('[data-test="preview-log-row"]')
    // rows[1] is the damage_applied event, path "triggers[0].actions[0]".
    await rows[1].find('button').trigger('click')

    const expectedRef: NodeRef = {
      kind: 'action',
      path: [{ kind: 'trigger', id: 't1' }, { kind: 'action', id: 'a1' }],
    }
    expect(builder.select).toHaveBeenCalledWith(expectedRef)
  })

  it('clicking a trace event row seeks the canvas playhead and pauses playback', async () => {
    runAbilityPreviewMock.mockResolvedValue(
      makeResult({
        frames: makeFrames(8),
        trace: [
          { t: 0, type: 'trigger_fired', path: 'triggers[0]', payload: { type: 'on_cast_complete' } },
          { t: 0.35, type: 'damage_applied', path: 'triggers[0].actions[0]', payload: { unit: 3, amount: 10 } },
        ],
      }),
    )
    const builder = makeBuilderStub()
    const wrapper = await mountPanel(builder)

    await wrapper.find('[data-test="preview-run-button"]').trigger('click')
    await flushPromises()

    // Sanity: the canvas mounted and starts at frame 0.
    const scrub = () => wrapper.find('[data-test="preview-scrub"]').element as HTMLInputElement
    expect(scrub().value).toBe('0')

    const rows = wrapper.findAll('[data-test="preview-log-row"]')
    // rows[1] is the t=0.35 damage_applied event, path "triggers[0].actions[0]".
    await rows[1].find('button').trigger('click')
    await flushPromises()

    // Math.round(0.35 / 0.05) = 7.
    expect(scrub().value).toBe('7')
    // Playback paused — the canvas's play/pause toggle reads "Play".
    expect(wrapper.find('[data-test="preview-play-toggle"]').text()).toBe('Play')

    // The existing click-to-inspect jump still fires alongside the seek.
    const expectedRef: NodeRef = {
      kind: 'action',
      path: [{ kind: 'trigger', id: 't1' }, { kind: 'action', id: 'a1' }],
    }
    expect(builder.select).toHaveBeenCalledWith(expectedRef)
  })

  it('advancing currentTick to 7 marks the t=0.35 event active in the log', async () => {
    runAbilityPreviewMock.mockResolvedValue(
      makeResult({
        frames: makeFrames(8),
        trace: [
          { t: 0, type: 'trigger_fired', path: 'triggers[0]', payload: { type: 'on_cast_complete' } },
          { t: 0.35, type: 'damage_applied', path: 'triggers[0].actions[0]', payload: { unit: 3, amount: 10 } },
        ],
      }),
    )
    const builder = makeBuilderStub()
    const wrapper = await mountPanel(builder)

    await wrapper.find('[data-test="preview-run-button"]').trigger('click')
    await flushPromises()

    const rows = wrapper.findAll('[data-test="preview-log-row"]')
    // Before seeking, only the t=0 event's window [0, 0.05) is active.
    expect(rows[0].classes()).toContain('pv-log__row--active')
    expect(rows[1].classes()).not.toContain('pv-log__row--active')

    // Seek to tick 7 (t=0.35) via the same row click Task 8C wires up.
    await rows[1].find('button').trigger('click')
    await flushPromises()

    expect(rows[0].classes()).not.toContain('pv-log__row--active')
    expect(rows[1].classes()).toContain('pv-log__row--active')
  })

  it('shows a run error when the API call rejects', async () => {
    runAbilityPreviewMock.mockRejectedValue(new Error('bad ability program'))
    const builder = makeBuilderStub()
    const wrapper = await mountPanel(builder)

    await wrapper.find('[data-test="preview-run-button"]').trigger('click')
    await flushPromises()

    const err = wrapper.find('[data-test="preview-run-error"]')
    expect(err.exists()).toBe(true)
    expect(err.text()).toContain('bad ability program')
  })
})
