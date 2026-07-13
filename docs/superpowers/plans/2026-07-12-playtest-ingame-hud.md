# Playtest In-Game HUD Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When the world editor's Play is running, render the core in-game HUD (unit selection + stats, commands, resources, minimap frame, commander abilities, objectives) wired to the live match, so playtesting feels like a real game.

**Architecture:** Route the playtest through the existing `useGameClient` singleton (instead of `usePlaytest`'s own bare `GameClient`) so the HUD — which reads that singleton's reactive `ui` snapshot — sees the live match. `usePlaytest` owns the single `useGameClient()` instance and exposes it; a new `PlaytestHud.vue` receives it as a prop and mounts the core HUD components (`MatchHud`, `SelectionHud`, `CommanderActionBar`, objective/zone panels) wired to the composable's `ui` + command forwarders — copying only the relevant subset of `Match.vue`'s wiring, none of its route logic.

**Tech Stack:** Vue 3 + TypeScript SPA (`client/src/game-portal`, vitest + @vue/test-utils).

**Spec:** `docs/superpowers/specs/2026-07-12-playtest-ingame-hud-design.md`

## Global Constraints

- Branch: all work on `unit-types-editor` (verify, never switch).
- **Never modify** `views/Match.vue` or the item/map editors. Reuse the HUD components read-only (props + emits); centralize client access via `useGameClient()`.
- The playtest runs an **ephemeral** match: fresh match per Play (the `NetworkClient` already omits `matchId` when ephemeral), no reward persistence. Preserve this.
- Preserve the existing playtest behavior: reentry guard (`playing` + synchronous `starting`), error surfacing on a failed start, Pause/Resume, and Reset (teardown → editor).
- No literal `cursor:` declarations in new component CSS except `cursor: not-allowed` on forbidden-action states.
- Server-authoritative: the HUD only renders `ui` snapshots and forwards command intents; no client-side gameplay simulation.
- Excluded from the playtest HUD (v1): match menu / settings modal, wave-upgrade modal, victory modal, disconnect overlay. Audio unchanged.
- Client commands from `client/src/game-portal` (`npm run test`, `npm run build`). `npx vue-tsc --noEmit` (or the build) is the typecheck gate.

---

### Task 1: Route `usePlaytest` through `useGameClient` (client unification)

**Files:**
- Modify: `client/src/game-portal/src/composables/useGameClient.ts` (widen `init` options type)
- Rewrite: `client/src/game-portal/src/components/world-editor/usePlaytest.ts`
- Rewrite: `client/src/game-portal/src/components/world-editor/usePlaytest.test.ts`

**Interfaces:**
- Consumes: `useGameClient()` returning `{ init, destroy, sendSetPause, ui, performSelectionAction, selectUnitOnly, deselectUnit, setMinimapPanelRect, sendUseConsumable, sendUnequipItem, sendEquipItem, beginCommanderAbility, cancelCommanderAbility, ... }`. `init(canvas: HTMLCanvasElement, mapId?: string, options?: { resume?: boolean; ephemeral?: boolean })` (widened here). `ui` is `Ref<GameUiSnapshot>`.
- Produces: `usePlaytest(getPlayCanvas: () => HTMLCanvasElement | null)` returns `{ playing: Ref<boolean>, paused: ComputedRef<boolean>, start(file: MapCatalogFile): Promise<void>, stop(): void, togglePause(): void, gameClient: ReturnType<typeof useGameClient> }`. Still exports `scratchMapId` and `resolvePlaytestMapId`.

- [ ] **Step 1: Widen `init`'s options type**

In `client/src/game-portal/src/composables/useGameClient.ts`, change the `init` signature's options type from `{ resume?: boolean }` to include `ephemeral`. Find:
```ts
async function init(
  canvas: HTMLCanvasElement,
  mapId = '',
  options: { resume?: boolean } = {},
) {
```
Change the options type to:
```ts
async function init(
  canvas: HTMLCanvasElement,
  mapId = '',
  options: { resume?: boolean; ephemeral?: boolean } = {},
) {
```
No body change — `init` already does `await client.start(options)`, and `GameClient.start` already reads `options.ephemeral`.

- [ ] **Step 2: Write the failing test (rewrite `usePlaytest.test.ts`)**

Replace the entire contents of `client/src/game-portal/src/components/world-editor/usePlaytest.test.ts` with:
```ts
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { ref } from 'vue'
import { scratchMapId, resolvePlaytestMapId } from './usePlaytest'

describe('playtest map id resolution', () => {
  it('uses the working map id when saved, else the scratch id', () => {
    expect(resolvePlaytestMapId({ id: 'my_map' } as any)).toBe('my_map')
    expect(resolvePlaytestMapId({ id: 'editor-draft' } as any)).toBe(scratchMapId)
    expect(resolvePlaytestMapId({ id: '' } as any)).toBe(scratchMapId)
  })
})

vi.mock('@/game/maps/catalog', () => ({
  saveMapCatalogFile: vi.fn().mockResolvedValue(undefined),
}))

// A single shared mock GameClient handle. useGameClient() returns it every call
// (mirrors the real module-singleton client); tests reset its spies each run.
const { gc } = vi.hoisted(() => ({
  gc: {
    ui: { value: { paused: false } },
    init: vi.fn().mockResolvedValue(undefined),
    destroy: vi.fn(),
    sendSetPause: vi.fn(),
  },
}))
vi.mock('@/composables/useGameClient', () => ({
  useGameClient: () => gc,
}))

beforeEach(() => {
  gc.init.mockClear()
  gc.destroy.mockClear()
  gc.sendSetPause.mockClear()
  gc.ui.value.paused = false
})

describe('usePlaytest lifecycle', () => {
  it('start() inits an ephemeral match and marks playing', async () => {
    const { usePlaytest } = await import('./usePlaytest')
    const canvas = {} as HTMLCanvasElement
    const { playing, start } = usePlaytest(() => canvas)

    await start({ id: 'my_map' } as any)

    expect(playing.value).toBe(true)
    expect(gc.init).toHaveBeenCalledTimes(1)
    expect(gc.init).toHaveBeenCalledWith(canvas, 'my_map', { ephemeral: true })
  })

  it('start() is reentrancy-guarded once playing', async () => {
    const { usePlaytest } = await import('./usePlaytest')
    const { start } = usePlaytest(() => ({}) as HTMLCanvasElement)
    await start({ id: 'm1' } as any)
    await start({ id: 'm1' } as any)
    expect(gc.init).toHaveBeenCalledTimes(1)
  })

  it('rejects a concurrent in-flight start()', async () => {
    const { usePlaytest } = await import('./usePlaytest')
    const { start } = usePlaytest(() => ({}) as HTMLCanvasElement)
    const a = start({ id: 'm2' } as any)
    const b = start({ id: 'm2' } as any)
    await Promise.all([a, b])
    expect(gc.init).toHaveBeenCalledTimes(1)
  })

  it('stop() destroys the client and clears playing', async () => {
    const { usePlaytest } = await import('./usePlaytest')
    const { playing, start, stop } = usePlaytest(() => ({}) as HTMLCanvasElement)
    await start({ id: 'm3' } as any)
    stop()
    expect(gc.destroy).toHaveBeenCalledTimes(1)
    expect(playing.value).toBe(false)
  })

  it('togglePause forwards the negated authoritative paused state', async () => {
    const { usePlaytest } = await import('./usePlaytest')
    const { start, togglePause } = usePlaytest(() => ({}) as HTMLCanvasElement)
    await start({ id: 'm4' } as any)

    gc.ui.value.paused = false
    togglePause()
    expect(gc.sendSetPause).toHaveBeenLastCalledWith(true)

    gc.ui.value.paused = true
    togglePause()
    expect(gc.sendSetPause).toHaveBeenLastCalledWith(false)
  })

  it('togglePause is a no-op when not playing', async () => {
    const { usePlaytest } = await import('./usePlaytest')
    const { togglePause } = usePlaytest(() => ({}) as HTMLCanvasElement)
    togglePause()
    expect(gc.sendSetPause).not.toHaveBeenCalled()
  })
})
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd client/src/game-portal && npm run test -- usePlaytest`
Expected: FAIL — current `usePlaytest` calls `new GameClient`, not `useGameClient().init`; assertions on `gc.init`/`gc.destroy` fail.

- [ ] **Step 4: Rewrite `usePlaytest.ts`**

Replace the entire contents of `client/src/game-portal/src/components/world-editor/usePlaytest.ts` with:
```ts
import { computed, ref } from 'vue'
import type { MapConfig, MapCatalogFile } from '@/game/network/protocol'
import { saveMapCatalogFile } from '@/game/maps/catalog'
import { useGameClient } from '@/composables/useGameClient'

export const scratchMapId = '__world_editor_scratch__'

// resolvePlaytestMapId picks the id to run: the working map's real id, or the
// reserved scratch id for a never-saved draft.
export function resolvePlaytestMapId(map: Pick<MapConfig, 'id'>): string {
  if (!map.id || map.id === 'editor-draft') return scratchMapId
  return map.id
}

// usePlaytest owns the single useGameClient() instance the world-editor
// playtest uses. Running the match through the shared composable (rather than a
// private GameClient) is what lets the in-game HUD — which reads the composable
// snapshot — render the live playtest. The returned `gameClient` is handed to
// PlaytestHud so it can read `ui` and forward commands.
export function usePlaytest(getPlayCanvas: () => HTMLCanvasElement | null) {
  const gameClient = useGameClient()
  const playing = ref(false)
  // Authoritative pause state comes from the server snapshot.
  const paused = computed(() => gameClient.ui.value.paused)
  // Synchronous in-flight marker (playing flips true only after the awaits).
  let starting = false

  async function start(file: MapCatalogFile) {
    if (playing.value || starting) return
    const canvas = getPlayCanvas()
    if (!canvas) return
    starting = true
    try {
      const mapId = resolvePlaytestMapId(file)
      await saveMapCatalogFile({ ...file, id: mapId })
      await gameClient.init(canvas, mapId, { ephemeral: true })
      playing.value = true
    } catch (err) {
      gameClient.destroy()
      playing.value = false
      throw err
    } finally {
      starting = false
    }
  }

  // togglePause freezes/continues the running match via the server set_pause
  // command; the button label reads the authoritative `paused` computed.
  function togglePause() {
    if (!playing.value) return
    gameClient.sendSetPause(!gameClient.ui.value.paused)
  }

  // stop tears the match down. The editor's MapConfig is untouched, so
  // re-showing the editor canvas restores the placements.
  function stop() {
    gameClient.destroy()
    playing.value = false
  }

  return { playing, paused, start, stop, togglePause, gameClient }
}
```

- [ ] **Step 5: Run tests + typecheck**

Run: `cd client/src/game-portal && npm run test -- usePlaytest`
Expected: PASS (all lifecycle + map-id tests).
Run: `cd client/src/game-portal && npx vue-tsc --noEmit`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add client/src/game-portal/src/composables/useGameClient.ts client/src/game-portal/src/components/world-editor/usePlaytest.ts client/src/game-portal/src/components/world-editor/usePlaytest.test.ts
git commit -m "World editor playtest: run through the shared useGameClient"
```

Note: `WorldEditorPanel.vue` still destructures `usePlaytest()` without `gameClient` — it compiles (extra return keys are ignored). Task 3 wires the new key. Do not edit `WorldEditorPanel.vue` in this task beyond what compiles; the panel's existing `paused` prop usage is unaffected (`paused` is now a computed ref, still a valid prop value).

---

### Task 2: `PlaytestHud.vue` — mount the core HUD

**Files:**
- Create: `client/src/game-portal/src/components/world-editor/PlaytestHud.vue`
- Test: `client/src/game-portal/src/components/world-editor/PlaytestHud.test.ts`

**Interfaces:**
- Consumes: the `gameClient` object from `usePlaytest` (Task 1) — typed `ReturnType<typeof import('@/composables/useGameClient').useGameClient>`. Reads `.ui.value` (a `GameUiSnapshot`) and calls forwarders `performSelectionAction`, `selectUnitOnly`, `deselectUnit`, `setMinimapPanelRect`, `sendUseConsumable`, `sendUnequipItem`, `sendEquipItem`, `beginCommanderAbility`, `cancelCommanderAbility`.
- Produces: `<PlaytestHud :hud="gameClient" />` — a self-contained overlay. No emits.
- Reuses (read-only): `@/components/MatchHud.vue`, `@/components/SelectionHud.vue`, `@/components/CommanderActionBar.vue`, `@/components/MatchObjectivesPanel.vue`, `@/components/ZoneCapturePanel.vue`, `@/components/ZoneInspectionPanel.vue`.

- [ ] **Step 1: Write the failing test**

Create `client/src/game-portal/src/components/world-editor/PlaytestHud.test.ts`:
```ts
import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { shallowMount } from '@vue/test-utils'
import PlaytestHud from './PlaytestHud.vue'
import MatchHud from '@/components/MatchHud.vue'
import SelectionHud from '@/components/SelectionHud.vue'
import CommanderActionBar from '@/components/CommanderActionBar.vue'

function mkHud(overrides: Record<string, unknown> = {}) {
  const ui = ref({
    selectedUnits: [],
    selection: { kind: 'none', title: '', subtitle: '', details: [], actions: [] },
    commanderAbilities: [],
    commanderTargetingAbilityId: null,
    objectives: [],
    zoneCaptureCards: [],
    zoneInspection: null,
    paused: false,
    player: { playerId: 'p1', color: '#fff', totalUnits: 0, selectedUnits: 0, totalHp: 0, resources: [] },
    wave: { enabled: false },
    notifications: [],
    ...overrides,
  })
  return {
    ui,
    performSelectionAction: vi.fn(),
    selectUnitOnly: vi.fn(),
    deselectUnit: vi.fn(),
    setMinimapPanelRect: vi.fn(),
    sendUseConsumable: vi.fn(),
    sendUnequipItem: vi.fn(),
    sendEquipItem: vi.fn(),
    beginCommanderAbility: vi.fn(),
    cancelCommanderAbility: vi.fn(),
  }
}

describe('PlaytestHud', () => {
  it('renders the core HUD components fed by the hud ui', () => {
    const hud = mkHud()
    const wrapper = shallowMount(PlaytestHud, { props: { hud: hud as any } })
    expect(wrapper.findComponent(MatchHud).exists()).toBe(true)
    expect(wrapper.findComponent(SelectionHud).exists()).toBe(true)
    expect(wrapper.findComponent(CommanderActionBar).exists()).toBe(true)
    expect(wrapper.findComponent(SelectionHud).props('ui')).toBe(hud.ui.value)
  })

  it('forwards a selection action to the client', () => {
    const hud = mkHud()
    const wrapper = shallowMount(PlaytestHud, { props: { hud: hud as any } })
    wrapper.findComponent(SelectionHud).vm.$emit('action', 'move')
    expect(hud.performSelectionAction).toHaveBeenCalledWith('move')
  })

  it('casts a commander ability, toggling cancel when already targeting it', () => {
    const hud = mkHud()
    const wrapper = shallowMount(PlaytestHud, { props: { hud: hud as any } })
    wrapper.findComponent(CommanderActionBar).vm.$emit('cast', 'fireball')
    expect(hud.beginCommanderAbility).toHaveBeenCalledWith('fireball')

    hud.ui.value.commanderTargetingAbilityId = 'fireball'
    wrapper.findComponent(CommanderActionBar).vm.$emit('cast', 'fireball')
    expect(hud.cancelCommanderAbility).toHaveBeenCalledTimes(1)
  })
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd client/src/game-portal && npm run test -- PlaytestHud`
Expected: FAIL — `PlaytestHud.vue` does not exist.

- [ ] **Step 3: Create `PlaytestHud.vue`**

```vue
<template>
  <div class="playtest-hud">
    <MatchHud :ui="ui" />
    <SelectionHud
      :ui="ui"
      @action="hud.performSelectionAction"
      @select-unit="hud.selectUnitOnly"
      @deselect-unit="hud.deselectUnit"
      @minimap-rect="hud.setMinimapPanelRect"
      @use-consumable="({ unitId, slotIndex }) => hud.sendUseConsumable(unitId, slotIndex)"
      @unequip-item="({ unitId, slotIndex }) => hud.sendUnequipItem(unitId, slotIndex)"
      @equip-item="({ unitId, slotIndex, instanceId }) => hud.sendEquipItem(unitId, slotIndex, instanceId)"
    />
    <CommanderActionBar
      :abilities="ui.commanderAbilities"
      :active-ability-id="ui.commanderTargetingAbilityId"
      @cast="onCommanderCast"
    />
    <div
      v-if="ui.objectives.length || ui.zoneCaptureCards.length || ui.zoneInspection"
      class="playtest-hud__objectives"
    >
      <MatchObjectivesPanel v-if="ui.objectives.length" :objectives="ui.objectives" />
      <ZoneCapturePanel v-if="ui.zoneCaptureCards.length" :cards="ui.zoneCaptureCards" />
      <ZoneInspectionPanel v-if="ui.zoneInspection" :info="ui.zoneInspection" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import MatchHud from '@/components/MatchHud.vue'
import SelectionHud from '@/components/SelectionHud.vue'
import CommanderActionBar from '@/components/CommanderActionBar.vue'
import MatchObjectivesPanel from '@/components/MatchObjectivesPanel.vue'
import ZoneCapturePanel from '@/components/ZoneCapturePanel.vue'
import ZoneInspectionPanel from '@/components/ZoneInspectionPanel.vue'

type HudApi = ReturnType<typeof import('@/composables/useGameClient').useGameClient>

const props = defineProps<{ hud: HudApi }>()

// The live GameUiSnapshot from the shared composable, refreshed per frame by
// the composable's rAF loop.
const ui = computed(() => props.hud.ui.value)

// Mirrors Match.vue's onCommanderCast: clicking the ability that is already
// armed cancels targeting; otherwise begin targeting it.
function onCommanderCast(abilityId: string) {
  if (props.hud.ui.value.commanderTargetingAbilityId === abilityId) {
    props.hud.cancelCommanderAbility()
    return
  }
  props.hud.beginCommanderAbility(abilityId)
}
</script>

<style scoped>
/* Full-viewport passthrough overlay: the child HUD components position
   themselves (fixed/absolute), so this wrapper just needs to not block the
   canvas beneath. No literal cursor declarations (global rules own the cursor). */
.playtest-hud { position: absolute; inset: 0; pointer-events: none; }
.playtest-hud > * { pointer-events: auto; }
.playtest-hud__objectives { position: absolute; top: 12px; left: 12px; z-index: 20; }
</style>
```
(The `pointer-events: none` on the wrapper + `auto` on children keeps canvas input working through the gaps between HUD panels, matching how the match screen layers overlays over the canvas.)

- [ ] **Step 4: Run to verify pass + typecheck**

Run: `cd client/src/game-portal && npm run test -- PlaytestHud`
Expected: PASS (3 tests).
Run: `cd client/src/game-portal && npx vue-tsc --noEmit`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add client/src/game-portal/src/components/world-editor/PlaytestHud.vue client/src/game-portal/src/components/world-editor/PlaytestHud.test.ts
git commit -m "Add PlaytestHud: core in-game HUD for world editor playtest"
```

---

### Task 3: Mount `PlaytestHud` in the world editor

**Files:**
- Modify: `client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue`
- Test: manual (build + open)

**Interfaces:**
- Consumes: `usePlaytest()` now also returns `gameClient` (Task 1); `PlaytestHud` (Task 2).

- [ ] **Step 1: Import `PlaytestHud`**

In `client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue`, add alongside the existing `PlaytestBar` import (`import PlaytestBar from '@/components/world-editor/PlaytestBar.vue'`):
```ts
import PlaytestHud from '@/components/world-editor/PlaytestHud.vue'
```

- [ ] **Step 2: Capture `gameClient` from `usePlaytest`**

Find the `usePlaytest` destructure (currently):
```ts
const {
  playing: playtestPlaying,
  paused: playtestPaused,
  start: startPlaytestMatch,
  stop: stopPlaytestMatch,
  togglePause: togglePlaytestPause,
} = usePlaytest(() => playCanvas.value)
```
Add the `gameClient` key:
```ts
const {
  playing: playtestPlaying,
  paused: playtestPaused,
  start: startPlaytestMatch,
  stop: stopPlaytestMatch,
  togglePause: togglePlaytestPause,
  gameClient: playtestGameClient,
} = usePlaytest(() => playCanvas.value)
```

- [ ] **Step 3: Render `PlaytestHud` over the play canvas**

Find the play-canvas + PlaytestBar region (the `<canvas v-show="playtestPlaying" ref="playCanvas" ...>` and `<PlaytestBar ... />`). Add `PlaytestHud` immediately after the play canvas, gated the same way:
```html
        <canvas v-show="playtestPlaying" ref="playCanvas" class="we-play-canvas"></canvas>
        <PlaytestHud v-if="playtestPlaying" :hud="playtestGameClient" />
        <PlaytestBar
          v-if="playtestPlaying"
          :paused="playtestPaused"
          @toggle-pause="togglePlaytestPause"
          @reset="stopPlaytest"
        />
```
(`PlaytestHud` sits between the play canvas and the PlaytestBar so the bar's `z-index: 30` stays on top of the HUD.)

- [ ] **Step 4: Build + manual E2E + commit**

Run: `cd client/src/game-portal && npm run build`
Expected: clean.
Manual E2E (the proof): world editor → drop two hostile units → ▶ Play → the in-game HUD appears → click-select a unit → its stats show in the selection panel → right-click to move/attack → cast a commander ability (if the unit has one) → ⏸ Pause freezes it, ▶ Resume continues → ↺ Reset returns to the editor with the HUD gone and no console errors.
```bash
git add client/src/game-portal/src/components/world-editor/WorldEditorPanel.vue
git commit -m "World editor: show the in-game HUD during playtest"
```

---

### Task 4: Verification sweep

**Files:** fixes only.

- [ ] **Step 1: Full client gates**

Run: `cd client/src/game-portal && npm run test`
Expected: green (includes the new `usePlaytest` + `PlaytestHud` suites).
Run: `cd client/src/game-portal && npm run build`
Expected: clean.

- [ ] **Step 2: Match.vue untouched**

Run: `cd "$(git rev-parse --show-toplevel)" && git diff --stat world-editor..unit-types-editor -- client/src/game-portal/src/views/Match.vue`
Expected: NO changes (Match.vue must be untouched by this feature).

- [ ] **Step 3: No stray literal cursor in new components**

Run: `cd "$(git rev-parse --show-toplevel)" && grep -n "cursor:" client/src/game-portal/src/components/world-editor/PlaytestHud.vue || echo "none (good)"`
Expected: `none (good)`.

- [ ] **Step 4: Commit any fixes**

```bash
git add -A client/
git commit -m "Playtest HUD: verification fixes"
```
(Skip if clean.)
