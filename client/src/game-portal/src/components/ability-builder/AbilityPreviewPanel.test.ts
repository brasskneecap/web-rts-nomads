import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref, shallowRef } from 'vue'
import type { AbilityEditorForm } from '@/game/abilities/abilityEditorForm'
import type { AbilityProgram } from '@/game/abilities/program/abilityProgram'
import type { PreviewFrame, PreviewRequest, PreviewResult } from '@/game/abilities/program/programPreview'
import type { MatchSnapshotMessage } from '@/game/network/protocol'
import AbilityPreviewPanel from './AbilityPreviewPanel.vue'
import AbilityPreviewCanvas from './AbilityPreviewCanvas.vue'
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

// A charge-fire passive shape (arcane_missiles): an on_charge_full trigger with
// a charge_fire_volley action carrying chargeRequired — what the panel detects
// to surface the Charge input.
function makeChargeProgram(chargeRequired = 30): AbilityProgram {
  return {
    entry: { type: 'passive', range: 0 },
    triggers: [
      {
        id: 'cf',
        type: 'on_charge_full',
        actions: [{ id: 'v', type: 'charge_fire_volley', config: { chargeRequired, missileCount: 3 } }],
      },
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
    // The execution-timeline highlight reads builder.selected; default to the
    // ability node (no lane highlighted), matching the real builder's default.
    selected: ref({ kind: 'ability' as const }),
    select: vi.fn(),
    // params: read by the panel's `conditionals` computed (collectConditionals),
    // evaluated eagerly since it's passed straight through as a prop.
    params: shallowRef<Record<string, number>>({}),
    // catalogs: the panel reads `perks` from here to offer the caster-perk
    // picker. Two entries so a test can assert the list is offered and scoped,
    // rather than only that an empty list renders nothing.
    catalogs: shallowRef({
      effects: [], projectiles: [], damageTypes: [], categories: [],
      autoCastSelectors: [], unitTypes: [],
      perks: [
        { id: 'lasting_flames', label: 'Lasting Flames', path: 'trapper' },
        { id: 'zealous_march', label: 'Zealous March', path: 'cleric' },
      ],
    }),
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
  it('mounts the canvas immediately, before any run, in an editable (drag-to-place) state', async () => {
    const builder = makeBuilderStub()
    const wrapper = await mountPanel(builder)

    const canvas = wrapper.find('[data-test="ability-preview-canvas"]')
    expect(canvas.exists()).toBe(true)
    // Phase 6b: with no result yet, the canvas is in EDIT mode, not idle —
    // the panel seeds a default scene (1 enemy + 1 pre-damaged ally), so the
    // drag-to-place layer is present and the "run a preview" placeholder is
    // NOT shown (there IS a scene to look at/drag). Replay-only playback
    // controls stay disabled — there's no captured frame sequence yet.
    expect(wrapper.find('[data-test="preview-drag-layer"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="preview-canvas-empty"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="preview-play-toggle"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-test="preview-execution-timeline"]').exists()).toBe(false)
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

  it('renders the timeline + event log once a result comes back', async () => {
    runAbilityPreviewMock.mockResolvedValue(makeResult())
    const builder = makeBuilderStub()
    const wrapper = await mountPanel(builder)

    await wrapper.find('[data-test="preview-run-button"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="preview-execution-timeline"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="preview-event-log"]').exists()).toBe(true)
  })

  it('tabs between the timeline and event log so only one is shown at a time', async () => {
    runAbilityPreviewMock.mockResolvedValue(makeResult())
    const builder = makeBuilderStub()
    const wrapper = await mountPanel(builder)

    await wrapper.find('[data-test="preview-run-button"]').trigger('click')
    await flushPromises()

    const timeline = () => wrapper.find('[data-test="preview-execution-timeline"]')
    const log = () => wrapper.find('[data-test="preview-event-log"]')
    const style = (el: ReturnType<typeof timeline>) => el.attributes('style') ?? ''

    // Defaults to the timeline; the log is present (v-show) but hidden.
    expect(style(timeline())).not.toContain('display: none')
    expect(style(log())).toContain('display: none')

    await wrapper.find('[data-test="preview-view-tab-log"]').trigger('click')
    expect(style(timeline())).toContain('display: none')
    expect(style(log())).not.toContain('display: none')

    await wrapper.find('[data-test="preview-view-tab-timeline"]').trigger('click')
    expect(style(timeline())).not.toContain('display: none')
    expect(style(log())).toContain('display: none')
  })

  it('feeds populated frames to the (already-mounted) canvas once a result arrives, switching out of edit mode', async () => {
    runAbilityPreviewMock.mockResolvedValue(makeResult({ frames: makeFrames(5) }))
    const builder = makeBuilderStub()
    const wrapper = await mountPanel(builder)

    // Canvas is present from the start, in edit mode (drag layer present).
    expect(wrapper.find('[data-test="ability-preview-canvas"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="preview-drag-layer"]').exists()).toBe(true)

    await wrapper.find('[data-test="preview-run-button"]').trigger('click')
    await flushPromises()

    // Same canvas instance now has real frames — edit mode's drag layer is
    // gone, controls enabled, and the rest of the panel (timeline/log)
    // still works.
    expect(wrapper.find('[data-test="preview-drag-layer"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="preview-canvas-empty"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="preview-play-toggle"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('[data-test="preview-scrub"]').attributes('max')).toBe('4')
    expect(wrapper.find('[data-test="preview-execution-timeline"]').exists()).toBe(true)
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
    // Playback paused — the icon toggle's accessible label reads "Play".
    expect(wrapper.find('[data-test="preview-play-toggle"]').attributes('aria-label')).toBe('Play')

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

  // Phase 6b: drag-to-place. AbilityPreviewCanvas owns the actual hit-testing/
  // pointer geometry (covered in AbilityPreviewCanvas.test.ts + previewOverlays.test.ts's
  // screenToWorld inverse); here we only verify the PANEL correctly threads a
  // drag's emitted position into the next Run request, and reconciles scene-unit
  // COUNTS against whatever's already been dragged.
  describe('drag-to-place (Phase 6b)', () => {
    it('a scene-unit drag updates the position sent in the next Run request', async () => {
      runAbilityPreviewMock.mockResolvedValue(makeResult())
      const builder = makeBuilderStub()
      const wrapper = await mountPanel(builder)

      const canvas = wrapper.findComponent(AbilityPreviewCanvas)
      await canvas.vm.$emit('update:scene-unit', { index: 0, x: 999, y: 888 })

      await wrapper.find('[data-test="preview-run-button"]').trigger('click')
      await flushPromises()

      const req = runAbilityPreviewMock.mock.calls[0][0]
      expect(req.units[0].x).toBe(999)
      expect(req.units[0].y).toBe(888)
    })

    it('a caster drag updates casterX/Y sent in the next Run request', async () => {
      runAbilityPreviewMock.mockResolvedValue(makeResult())
      const builder = makeBuilderStub()
      const wrapper = await mountPanel(builder)

      const canvas = wrapper.findComponent(AbilityPreviewCanvas)
      await canvas.vm.$emit('update:caster', { x: 111, y: 222 })

      await wrapper.find('[data-test="preview-run-button"]').trigger('click')
      await flushPromises()

      const req = runAbilityPreviewMock.mock.calls[0][0]
      expect(req.casterX).toBe(111)
      expect(req.casterY).toBe(222)
    })

    it('dragging a unit after a run clears the shown result, returning the canvas to edit mode', async () => {
      runAbilityPreviewMock.mockResolvedValue(makeResult({ frames: makeFrames(5) }))
      const builder = makeBuilderStub()
      const wrapper = await mountPanel(builder)

      await wrapper.find('[data-test="preview-run-button"]').trigger('click')
      await flushPromises()
      expect(wrapper.find('[data-test="preview-execution-timeline"]').exists()).toBe(true)
      expect(wrapper.find('[data-test="preview-drag-layer"]').exists()).toBe(false)

      const canvas = wrapper.findComponent(AbilityPreviewCanvas)
      await canvas.vm.$emit('update:scene-unit', { index: 0, x: 50, y: 50 })
      await flushPromises()

      expect(wrapper.find('[data-test="preview-execution-timeline"]').exists()).toBe(false)
      expect(wrapper.find('[data-test="preview-drag-layer"]').exists()).toBe(true)
    })

    it('increasing enemy count appends a new enemy while preserving the dragged position of the existing one', async () => {
      runAbilityPreviewMock.mockResolvedValue(makeResult())
      const builder = makeBuilderStub()
      const wrapper = await mountPanel(builder)

      const canvas = wrapper.findComponent(AbilityPreviewCanvas)
      await canvas.vm.$emit('update:scene-unit', { index: 0, x: 777, y: 666 })

      await wrapper.find('[data-test="preview-enemy-count"]').setValue(2)
      await flushPromises()

      await wrapper.find('[data-test="preview-run-button"]').trigger('click')
      await flushPromises()

      const req = runAbilityPreviewMock.mock.calls[0][0]
      const enemies = req.units.filter((u) => u.team === 'enemy')
      expect(enemies).toHaveLength(2)
      expect(enemies[0].x).toBe(777)
      expect(enemies[0].y).toBe(666)
    })

    it('decreasing enemy count removes from the end, preserving earlier positions', async () => {
      runAbilityPreviewMock.mockResolvedValue(makeResult())
      const builder = makeBuilderStub()
      const wrapper = await mountPanel(builder)

      await wrapper.find('[data-test="preview-enemy-count"]').setValue(3)
      await flushPromises()

      const canvas = wrapper.findComponent(AbilityPreviewCanvas)
      await canvas.vm.$emit('update:scene-unit', { index: 0, x: 321, y: 654 })

      await wrapper.find('[data-test="preview-enemy-count"]').setValue(1)
      await flushPromises()

      await wrapper.find('[data-test="preview-run-button"]').trigger('click')
      await flushPromises()

      const req = runAbilityPreviewMock.mock.calls[0][0]
      const enemies = req.units.filter((u) => u.team === 'enemy')
      expect(enemies).toHaveLength(1)
      expect(enemies[0].x).toBe(321)
      expect(enemies[0].y).toBe(654)
    })
  })

  // Charge-fire passives (arcane_missiles) can't be cast — they fire when the
  // caster's Arcane Charge crosses a threshold. The panel surfaces a seeded
  // Charge control for exactly these, prefilled to the ability's own threshold.
  describe('charge-fire passive charge seeding', () => {
    it('shows no Charge control for a non-charge ability', async () => {
      const builder = makeBuilderStub() // heal-shaped program
      const wrapper = await mountPanel(builder)
      expect(wrapper.find('[data-test="preview-caster-charge"]').exists()).toBe(false)
    })

    it('surfaces a prefilled Charge control and sends it in the Run request for a charge program', async () => {
      runAbilityPreviewMock.mockResolvedValue(makeResult())
      const builder = makeBuilderStub()
      builder.program.value = makeChargeProgram(30)
      const wrapper = await mountPanel(builder)

      const charge = wrapper.find('[data-test="preview-caster-charge"]')
      expect(charge.exists()).toBe(true)
      expect((charge.element as HTMLInputElement).value).toBe('30')

      await wrapper.find('[data-test="preview-run-button"]').trigger('click')
      await flushPromises()

      const req = runAbilityPreviewMock.mock.calls[0][0]
      expect(req.casterCharge).toBe(30)
    })
  })

  // Escape hatches OUT of a finished run's frozen replay back into the
  // draggable edit scene — so added units appear immediately and units stay
  // movable without having to Run again.
  describe('returning to edit mode after a run', () => {
    it('changing enemy count after a run clears the replay and returns to edit mode', async () => {
      runAbilityPreviewMock.mockResolvedValue(makeResult({ frames: makeFrames(5) }))
      const builder = makeBuilderStub()
      const wrapper = await mountPanel(builder)

      await wrapper.find('[data-test="preview-run-button"]').trigger('click')
      await flushPromises()
      // In replay mode: timeline shown, drag layer gone.
      expect(wrapper.find('[data-test="preview-execution-timeline"]').exists()).toBe(true)
      expect(wrapper.find('[data-test="preview-drag-layer"]').exists()).toBe(false)

      await wrapper.find('[data-test="preview-enemy-count"]').setValue(2)
      await flushPromises()

      // Back in edit mode: replay cleared, drag layer (and the new unit) live.
      expect(wrapper.find('[data-test="preview-execution-timeline"]').exists()).toBe(false)
      expect(wrapper.find('[data-test="preview-drag-layer"]').exists()).toBe(true)
    })

    it('switching to a different ability drops the showing run (no stale replay)', async () => {
      runAbilityPreviewMock.mockResolvedValue(makeResult({ frames: makeFrames(5) }))
      const builder = makeBuilderStub()
      const wrapper = await mountPanel(builder)

      await wrapper.find('[data-test="preview-run-button"]').trigger('click')
      await flushPromises()
      expect(wrapper.find('[data-test="preview-execution-timeline"]').exists()).toBe(true)

      // Selecting a different ability changes the builder form's id — the panel
      // must clear the replay that belonged to the previous ability.
      builder.form.value = { ...builder.form.value, id: 'meteor' }
      await flushPromises()

      expect(wrapper.find('[data-test="preview-execution-timeline"]').exists()).toBe(false)
      expect(wrapper.find('[data-test="preview-drag-layer"]').exists()).toBe(true)
    })

    it('the Edit Scene button is disabled until a run, then clears it back to edit mode', async () => {
      runAbilityPreviewMock.mockResolvedValue(makeResult({ frames: makeFrames(5) }))
      const builder = makeBuilderStub()
      const wrapper = await mountPanel(builder)

      // Always present in the toolbar, but disabled before any run.
      const editBtn = () => wrapper.find('[data-test="preview-edit-scene-button"]')
      expect(editBtn().exists()).toBe(true)
      expect(editBtn().attributes('disabled')).toBeDefined()

      await wrapper.find('[data-test="preview-run-button"]').trigger('click')
      await flushPromises()
      // Enabled once a run is showing.
      expect(editBtn().attributes('disabled')).toBeUndefined()
      expect(wrapper.find('[data-test="preview-drag-layer"]').exists()).toBe(false)

      await editBtn().trigger('click')
      await flushPromises()

      // Replay cleared, canvas draggable again, and Edit is disabled once more.
      expect(wrapper.find('[data-test="preview-execution-timeline"]').exists()).toBe(false)
      expect(wrapper.find('[data-test="preview-drag-layer"]').exists()).toBe(true)
      expect(editBtn().attributes('disabled')).toBeDefined()
    })
  })
})
