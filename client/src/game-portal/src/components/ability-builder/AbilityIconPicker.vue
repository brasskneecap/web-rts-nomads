<template>
  <div class="ico" data-test="ability-icon-picker" @click.self="emit('close')">
    <div class="ico__panel" role="dialog" aria-label="Choose ability icon">
      <header class="ico__head">
        <span class="ico__title">Choose Icon</span>
        <button type="button" class="ico__x" aria-label="Close" @click="emit('close')">&times;</button>
      </header>

      <!-- Live preview of the current draft, rendered by the SAME canvas the
           action bar uses, so this is exactly what shows in game. -->
      <div class="ico__preview">
        <div class="ico__preview-cell">
          <AbilityIconCanvas :icon="draftIcon" :ability-id="abilityId" :projectile="projectile" :size="64" />
        </div>
        <code class="ico__preview-code">{{ draftIcon || '(none)' }}</code>
      </div>

      <div class="ico__tabs" role="tablist">
        <button
          v-for="t in TABS"
          :key="t.id"
          type="button"
          role="tab"
          class="ico__tab"
          :class="{ 'ico__tab--active': tab === t.id }"
          :data-test="`ability-icon-tab-${t.id}`"
          @click="tab = t.id"
        >{{ t.label }}</button>
      </div>

      <div v-if="tab === 'effect'" class="ico__gallery" data-test="ability-icon-effects">
        <button
          v-for="name in effectNames"
          :key="name"
          type="button"
          class="ico__item"
          :class="{ 'ico__item--sel': source === 'effect' && ref === name }"
          :title="name"
          @click="select('effect', name)"
        >
          <AbilityIconCanvas :icon="`effect:${name}`" :size="48" />
          <span class="ico__item-label">{{ name }}</span>
        </button>
      </div>

      <div v-else-if="tab === 'projectile'" class="ico__gallery" data-test="ability-icon-projectiles">
        <button
          v-for="id in projectileIds"
          :key="id"
          type="button"
          class="ico__item"
          :class="{ 'ico__item--sel': source === 'projectile' && ref === id }"
          :title="id"
          @click="select('projectile', id)"
        >
          <AbilityIconCanvas :icon="`projectile:${id}`" :size="48" />
          <span class="ico__item-label">{{ id }}</span>
        </button>
      </div>

      <div v-else-if="tab === 'beam'" class="ico__gallery" data-test="ability-icon-beams">
        <button
          v-for="name in beamNames"
          :key="name"
          type="button"
          class="ico__item"
          :class="{ 'ico__item--sel': source === 'beam' && ref === name }"
          :title="name"
          @click="select('beam', name)"
        >
          <AbilityIconCanvas :icon="`beam:${name}`" :size="48" />
          <span class="ico__item-label">{{ name }}</span>
        </button>
        <p v-if="!beamNames.length" class="ico__hint">No beam sprites available.</p>
      </div>

      <div v-else class="ico__upload">
        <label class="ico__upload-btn">
          <input type="file" accept="image/png" data-test="ability-icon-upload" @change="onFile" />
          Choose PNG…
        </label>
        <p class="ico__hint">
          Stored per-ability. Save the ability first (uploads key to its id).
        </p>
        <p v-if="uploadState" class="ico__upload-state" :class="{ 'ico__upload-state--err': uploadError }">
          {{ uploadState }}
        </p>
      </div>

      <!-- Frame slider: only for a multi-frame asset (effect sheet / projectile strip). -->
      <div v-if="frameCount > 1" class="ico__frames" data-test="ability-icon-frames">
        <label class="ico__frames-label" for="ico-frame">Frame</label>
        <input
          id="ico-frame"
          type="range"
          min="0"
          :max="frameCount - 1"
          :value="frame"
          data-test="ability-icon-frame-slider"
          @input="frame = Number(($event.target as HTMLInputElement).value)"
        />
        <span class="ico__frames-num">{{ frame }} / {{ frameCount - 1 }}</span>
      </div>

      <footer class="ico__foot">
        <button type="button" class="ico__btn" @click="emit('close')">Cancel</button>
        <button
          type="button"
          class="ico__btn ico__btn--primary"
          :disabled="!draftIcon"
          data-test="ability-icon-confirm"
          @click="confirm"
        >Use Icon</button>
      </footer>
    </div>
  </div>
</template>

<script setup lang="ts">
// AbilityIconPicker: a modal for choosing an ability's action icon from the
// effect sprite sheets, projectile art, or a custom uploaded PNG — with a frame
// slider for multi-frame assets. Every thumbnail + the live preview render
// through AbilityIconCanvas (the shared drawAbilityIcon), so what you pick is
// exactly what the in-game action bar shows. Emits the resulting icon string.
import { computed, ref as vref, watch } from 'vue'
import { listEffectNames } from '@/game/rendering/effectSprites'
import { listBeamNames } from '@/game/rendering/beamSprites'
import { registeredProjectileSpriteIds } from '@/game/rendering/projectileSpriteSheets'
import {
  parseAbilityIcon,
  formatAbilityIcon,
  abilityIconFrameCount,
  type AbilityIconSource,
} from '@/game/rendering/abilityIconRender'
import { invalidateServerAbilityIcon, getAbilityIconImageByKey } from '@/game/rendering/abilityAssets'
import { uploadAbilityIcon } from '@/game/abilities/abilityEditorApi'
import AbilityIconCanvas from './AbilityIconCanvas.vue'

const props = defineProps<{
  /** Current stored icon string. */
  modelIcon?: string
  /** The ability id — the fallback art source and the custom-upload key. */
  abilityId: string
  projectile?: string
}>()

const emit = defineEmits<{
  'update:icon': [icon: string]
  close: []
}>()

const TABS = [
  { id: 'effect', label: 'Effects' },
  { id: 'projectile', label: 'Projectiles' },
  { id: 'beam', label: 'Beams' },
  { id: 'upload', label: 'Upload' },
] as const
type TabId = (typeof TABS)[number]['id']

const effectNames = listEffectNames()
const beamNames = listBeamNames()
const projectileIds = registeredProjectileSpriteIds().sort()

// Draft selection, seeded from the current icon.
const parsed = parseAbilityIcon(props.modelIcon)
const source = vref<AbilityIconSource>(parsed?.source ?? 'effect')
const ref = vref<string>(parsed?.ref ?? '')
const frame = vref<number>(parsed?.source === 'effect' || parsed?.source === 'projectile' ? parsed.frame : 0)
const tab = vref<TabId>(source.value === 'key' ? 'effect' : source.value)

// frameCount is re-derived whenever the chosen asset changes. Projectile counts
// need the image decoded, so a fresh selection nudges it once the image lands.
const frameCount = vref(1)
function refreshFrameCount() {
  frameCount.value = ref.value ? abilityIconFrameCount(source.value, ref.value) : 1
}
watch([source, ref], () => {
  refreshFrameCount()
  // Projectile art may still be decoding — re-check on load so a strip reveals
  // its frames. Effects already know their count from the manifest.
  if (source.value === 'projectile' && frameCount.value <= 1) {
    const img = getAbilityIconImageByKey(ref.value) // bundled/none; harmless
    void img
    // Best-effort: schedule a re-check next frame after decode.
    setTimeout(refreshFrameCount, 120)
  }
}, { immediate: true })

// Clamp the frame if the newly chosen asset has fewer frames.
watch(frameCount, (n) => {
  if (frame.value > n - 1) frame.value = Math.max(0, n - 1)
})

const draftIcon = computed(() => (ref.value ? formatAbilityIcon(source.value, ref.value, frame.value) : ''))

function select(s: AbilityIconSource, r: string) {
  source.value = s
  ref.value = r
  frame.value = 0
}

function confirm() {
  if (draftIcon.value) emit('update:icon', draftIcon.value)
  emit('close')
}

// ── custom upload ───────────────────────────────────────────────────────────
const uploadState = vref('')
const uploadError = vref(false)

async function onFile(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  uploadError.value = false
  uploadState.value = 'Uploading…'
  try {
    await uploadAbilityIcon(props.abilityId, file)
    invalidateServerAbilityIcon(props.abilityId)
    // The server keys the uploaded icon to the ability id — select that key.
    source.value = 'key'
    ref.value = props.abilityId
    frame.value = 0
    uploadState.value = 'Uploaded. Preview above; click Use Icon.'
  } catch (err) {
    uploadError.value = true
    uploadState.value = err instanceof Error ? err.message : 'Upload failed'
  }
}
</script>

<style scoped>
.ico {
  position: fixed;
  inset: 0;
  z-index: 60;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgba(4, 8, 14, 0.66);
}

.ico__panel {
  display: flex;
  flex-direction: column;
  gap: 12px;
  width: min(560px, 100%);
  max-height: 100%;
  padding: 16px;
  background: var(--ed-bg, #14100a);
  border: 1px solid var(--ed-line-strong, #4a3b22);
  border-radius: var(--ed-radius);
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.5);
}

.ico__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.ico__title {
  font-family: var(--font-title);
  font-size: 0.9rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--ed-brass);
}

.ico__x {
  background: none;
  border: 0;
  color: var(--ed-text-dim);
  font-size: 1.3rem;
  line-height: 1;
  padding: 0 4px;
}

.ico__preview {
  display: flex;
  align-items: center;
  gap: 12px;
}

.ico__preview-cell {
  flex: 0 0 auto;
  width: 56px;
  height: 56px;
  padding: 3px;
  background: rgba(8, 14, 24, 0.6);
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
}

.ico__preview-code {
  font-family: var(--font-body);
  font-size: 0.76rem;
  color: var(--ed-text-dim);
}

.ico__tabs {
  display: flex;
  gap: 4px;
  border-bottom: 1px solid var(--ed-line);
}

.ico__tab {
  padding: 5px 12px;
  font-family: var(--font-body);
  font-size: 0.74rem;
  font-weight: 600;
  color: var(--ed-text-dim);
  background: none;
  border: 1px solid transparent;
  border-bottom: none;
  border-radius: var(--ed-radius) var(--ed-radius) 0 0;
  margin-bottom: -1px;
}

.ico__tab:hover {
  color: var(--ed-brass);
}

.ico__tab--active {
  color: var(--ed-brass);
  border-color: var(--ed-line);
  border-bottom-color: transparent;
  background: rgba(212, 168, 71, 0.08);
}

.ico__gallery {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(72px, 1fr));
  gap: 8px;
  overflow-y: auto;
  min-height: 120px;
  max-height: 320px;
  padding: 2px;
}

.ico__item {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  padding: 6px 4px;
  background: rgba(8, 14, 24, 0.4);
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
}

.ico__item > :first-child {
  width: 44px;
  height: 44px;
}

.ico__item:hover {
  border-color: var(--ed-line-strong);
}

.ico__item--sel {
  border-color: var(--ed-brass);
  background: rgba(212, 168, 71, 0.12);
}

.ico__item-label {
  font-size: 0.62rem;
  color: var(--ed-text-dim);
  text-align: center;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
}

.ico__upload {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-height: 120px;
  padding: 12px 2px;
}

.ico__upload-btn {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  align-self: flex-start;
  padding: 6px 12px;
  font-size: 0.78rem;
  color: var(--ed-text);
  background: rgba(15, 23, 42, 0.35);
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
}

.ico__upload-btn input {
  max-width: 200px;
  font-size: 0.72rem;
}

.ico__hint {
  margin: 0;
  font-size: 0.74rem;
  color: var(--ed-text-dim);
}

.ico__upload-state {
  margin: 0;
  font-size: 0.76rem;
  color: var(--ed-ok);
}

.ico__upload-state--err {
  color: var(--ed-danger);
}

.ico__frames {
  display: flex;
  align-items: center;
  gap: 10px;
}

.ico__frames-label {
  font-size: 0.72rem;
  font-weight: 600;
  color: var(--ed-text-dim);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

.ico__frames input {
  flex: 1 1 auto;
}

.ico__frames-num {
  font-size: 0.74rem;
  font-variant-numeric: tabular-nums;
  color: var(--ed-text-dim);
}

.ico__foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.ico__btn {
  padding: 6px 14px;
  font-family: var(--font-body);
  font-size: 0.78rem;
  font-weight: 600;
  color: var(--ed-text);
  background: rgba(15, 23, 42, 0.35);
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
}

.ico__btn--primary {
  color: var(--ed-brass);
  border-color: var(--ed-line-strong);
  background: rgba(212, 168, 71, 0.14);
}

.ico__btn--primary:disabled {
  opacity: 0.4;
}
</style>
