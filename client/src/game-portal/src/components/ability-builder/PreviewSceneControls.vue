<template>
  <SectionCard title="Preview Scene" data-test="preview-scene-controls">
    <div class="pv-scene__grid">
      <EditorField label="Enemies" for-id="pv-enemy-count">
        <input
          id="pv-enemy-count"
          type="number"
          min="0"
          max="8"
          :value="enemyCount"
          data-test="preview-enemy-count"
          @input="onEnemyCountInput"
        />
      </EditorField>
      <EditorField label="Allies" hint="(pre-damaged, so heals show)" for-id="pv-ally-count">
        <input
          id="pv-ally-count"
          type="number"
          min="0"
          max="8"
          :value="allyCount"
          data-test="preview-ally-count"
          @input="onAllyCountInput"
        />
      </EditorField>
    </div>

    <EditorField label="Target" for-id="pv-target">
      <select id="pv-target" :value="targetSelector" data-test="preview-target-selector" @change="onTargetSelectorChange">
        <option value="first_enemy">First enemy</option>
        <option value="first_ally">First ally</option>
        <option value="self">Self</option>
        <option value="point">Point</option>
      </select>
    </EditorField>

    <div class="pv-scene__grid">
      <EditorField label="Seed" for-id="pv-seed">
        <input id="pv-seed" type="number" :value="seed" data-test="preview-seed" @input="onSeedInput" />
      </EditorField>
      <EditorField label="Duration" hint="(seconds)" for-id="pv-duration">
        <input
          id="pv-duration"
          type="number"
          min="0.1"
          step="0.5"
          :value="durationSeconds"
          data-test="preview-duration"
          @input="onDurationInput"
        />
      </EditorField>
    </div>
  </SectionCard>
</template>

<script setup lang="ts">
// PreviewSceneControls: a compact, count-based scene editor for the preview
// panel. Enemies/allies are spread along the x-axis at fixed increments
// rather than individually placed — good enough to see an ability's effect
// radius/targeting fan out, not a full scene designer.
// TODO(phase-6b?): per-unit drag placement on a mini preview canvas, once
// the preview panel needs finer scene control than "how many, roughly where".
import { computed, ref, watch } from 'vue'
import EditorField from '@/components/editor/EditorField.vue'
import SectionCard from '@/components/editor/SectionCard.vue'
import type { PreviewRequest, PreviewSceneUnit } from '@/game/abilities/program/programPreview'
import { defaultPreviewRequest } from '@/game/abilities/program/programPreview'
import { PREVIEW_SCENE_ORIGIN } from './previewScene'

// PreviewScene is the subset of PreviewRequest this control owns — the
// caster's own position (casterX/casterY) and the ability under test are
// the panel's concern, not the scene's.
export type PreviewScene = Pick<PreviewRequest, 'units' | 'target' | 'castX' | 'castY' | 'seed' | 'durationSeconds'>

const emit = defineEmits<{ 'update:modelValue': [scene: PreviewScene] }>()

// Seeded from defaultPreviewRequest's own scene (1 enemy, 1 pre-damaged
// ally, seed 1, 3s) — the ability param it takes is unused by the scene
// fields, so a throwaway blank def is fine here.
const seedDefaults = defaultPreviewRequest({ id: '' })

const enemyCount = ref(seedDefaults.units.filter((u) => u.team === 'enemy').length)
const allyCount = ref(seedDefaults.units.filter((u) => u.team === 'ally').length)
type TargetSelector = 'first_enemy' | 'first_ally' | 'self' | 'point'
const targetSelector = ref<TargetSelector>('first_enemy')
const seed = ref(seedDefaults.seed)
const durationSeconds = ref(seedDefaults.durationSeconds)

// The layout below is authored RELATIVE to the caster (allies at negative X,
// enemies at positive X); PREVIEW_SCENE_ORIGIN shifts the whole thing onto
// the map's terrain — see previewScene.ts for why.
const ENEMY_START_X = PREVIEW_SCENE_ORIGIN.x + 120
const ENEMY_STEP_X = 40
const ALLY_START_X = PREVIEW_SCENE_ORIGIN.x - 80
const ALLY_STEP_X = -40
const SCENE_Y = PREVIEW_SCENE_ORIGIN.y
// POINT_CAST: fixed ground point used for the "Point" target selector — not
// individually configurable in v1 (see the module doc comment's TODO).
const POINT_CAST = { x: PREVIEW_SCENE_ORIGIN.x + 150, y: PREVIEW_SCENE_ORIGIN.y }

function buildUnits(): PreviewSceneUnit[] {
  const enemies: PreviewSceneUnit[] = Array.from({ length: enemyCount.value }, (_, i) => ({
    team: 'enemy',
    x: ENEMY_START_X + i * ENEMY_STEP_X,
    y: SCENE_Y,
    hp: 200,
    maxHp: 200,
  }))
  const allies: PreviewSceneUnit[] = Array.from({ length: allyCount.value }, (_, i) => ({
    team: 'ally',
    x: ALLY_START_X + i * ALLY_STEP_X,
    y: SCENE_Y,
    hp: 40,
    maxHp: 100,
  }))
  return [...enemies, ...allies]
}

const scene = computed<PreviewScene>(() => {
  const units = buildUnits()
  const firstAllyIndex = enemyCount.value // allies are appended after every enemy
  switch (targetSelector.value) {
    case 'first_enemy':
      return enemyCount.value > 0
        ? { units, target: 0, castX: units[0].x, castY: units[0].y, seed: seed.value, durationSeconds: durationSeconds.value }
        : { units, target: -1, castX: POINT_CAST.x, castY: POINT_CAST.y, seed: seed.value, durationSeconds: durationSeconds.value }
    case 'first_ally':
      return allyCount.value > 0
        ? {
            units,
            target: firstAllyIndex,
            castX: units[firstAllyIndex].x,
            castY: units[firstAllyIndex].y,
            seed: seed.value,
            durationSeconds: durationSeconds.value,
          }
        : { units, target: -1, castX: POINT_CAST.x, castY: POINT_CAST.y, seed: seed.value, durationSeconds: durationSeconds.value }
    case 'self':
      // The caster stands at the scene origin (AbilityPreviewPanel spawns it
      // there), so a self-cast's ground point is the origin — not (0,0).
      return {
        units,
        target: -1,
        castX: PREVIEW_SCENE_ORIGIN.x,
        castY: PREVIEW_SCENE_ORIGIN.y,
        seed: seed.value,
        durationSeconds: durationSeconds.value,
      }
    case 'point':
    default:
      return { units, target: -1, castX: POINT_CAST.x, castY: POINT_CAST.y, seed: seed.value, durationSeconds: durationSeconds.value }
  }
})

watch(scene, (v) => emit('update:modelValue', v), { immediate: true })

function onEnemyCountInput(e: Event) {
  const n = Number((e.target as HTMLInputElement).value)
  enemyCount.value = Number.isFinite(n) ? Math.max(0, Math.min(8, Math.trunc(n))) : 0
}

function onAllyCountInput(e: Event) {
  const n = Number((e.target as HTMLInputElement).value)
  allyCount.value = Number.isFinite(n) ? Math.max(0, Math.min(8, Math.trunc(n))) : 0
}

function onTargetSelectorChange(e: Event) {
  targetSelector.value = (e.target as HTMLSelectElement).value as TargetSelector
}

function onSeedInput(e: Event) {
  const n = Number((e.target as HTMLInputElement).value)
  seed.value = Number.isFinite(n) ? Math.trunc(n) : 0
}

function onDurationInput(e: Event) {
  const n = Number((e.target as HTMLInputElement).value)
  durationSeconds.value = Number.isFinite(n) && n > 0 ? n : 0.1
}
</script>

<style scoped>
.pv-scene__grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}
</style>
