<template>
  <div class="anp" data-test="animation-picker" @click.self="emit('close')">
    <div class="anp__panel" role="dialog" aria-label="Choose animation">
      <header class="anp__head">
        <span class="anp__title">Choose Animation</span>
        <button type="button" class="anp__x" aria-label="Close" @click="emit('close')">&times;</button>
      </header>

      <!-- Live animated preview of the current draft, drawn by the SAME resolver
           the in-game decal renderer uses. -->
      <div class="anp__preview">
        <div class="anp__preview-cell">
          <AnimationRefCanvas :animation="draftAnimation" :size="64" />
        </div>
        <code class="anp__preview-code">{{ draftAnimation || '(none)' }}</code>
      </div>

      <div class="anp__tabs" role="tablist">
        <button
          v-for="t in TABS"
          :key="t.id"
          type="button"
          role="tab"
          class="anp__tab"
          :class="{ 'anp__tab--active': tab === t.id }"
          :data-test="`animation-tab-${t.id}`"
          @click="tab = t.id"
        >{{ t.label }}</button>
      </div>

      <div v-if="tab === 'effect'" class="anp__gallery" data-test="animation-effects">
        <button
          v-for="name in effectNames"
          :key="name"
          type="button"
          class="anp__item"
          :class="{ 'anp__item--sel': source === 'effect' && ref === name }"
          :title="name"
          @click="selectSimple('effect', name)"
        >
          <AnimationRefCanvas :animation="`effect:${name}`" :size="48" />
          <span class="anp__item-label">{{ name }}</span>
        </button>
        <p v-if="!effectNames.length" class="anp__hint">No effects available.</p>
      </div>

      <div v-else-if="tab === 'projectile'" class="anp__gallery" data-test="animation-projectiles">
        <button
          v-for="id in projectileIds"
          :key="id"
          type="button"
          class="anp__item"
          :class="{ 'anp__item--sel': source === 'projectile' && ref === id }"
          :title="id"
          @click="selectSimple('projectile', id)"
        >
          <AnimationRefCanvas :animation="`projectile:${id}`" :size="48" />
          <span class="anp__item-label">{{ id }}</span>
        </button>
        <p v-if="!projectileIds.length" class="anp__hint">No projectiles available.</p>
      </div>

      <div v-else-if="tab === 'beam'" class="anp__gallery" data-test="animation-beams">
        <button
          v-for="name in beamNames"
          :key="name"
          type="button"
          class="anp__item"
          :class="{ 'anp__item--sel': source === 'beam' && ref === name }"
          :title="name"
          @click="selectSimple('beam', name)"
        >
          <AnimationRefCanvas :animation="`beam:${name}`" :size="48" />
          <span class="anp__item-label">{{ name }}</span>
        </button>
        <p v-if="!beamNames.length" class="anp__hint">No beam sprites available.</p>
      </div>

      <div v-else-if="tab === 'object'" class="anp__object" data-test="animation-objects">
        <div class="anp__gallery">
          <button
            v-for="key in objectKeys"
            :key="key"
            type="button"
            class="anp__item"
            :class="{ 'anp__item--sel': source === 'object' && ref === key }"
            :title="key"
            @click="selectObject(key)"
          >
            <AnimationRefCanvas :animation="objectThumbAnim(key)" :size="48" />
            <span class="anp__item-label">{{ key }}</span>
          </button>
          <p v-if="!objectKeys.length" class="anp__hint">No objects available.</p>
        </div>

        <!-- Animation-state selector — an object can define several states
             (idle, electrified, exploding). Pick WHICH one this reference
             plays. Only shown once an object is chosen and it has >1 state. -->
        <div v-if="source === 'object' && objectStates.length > 1" class="anp__states" data-test="animation-object-states">
          <span class="anp__states-label">Animation</span>
          <div class="anp__states-row">
            <button
              v-for="st in objectStates"
              :key="st"
              type="button"
              class="anp__state"
              :class="{ 'anp__state--sel': objectState === st }"
              :data-test="`animation-object-state-${st}`"
              @click="objectState = st"
            >{{ st }}</button>
          </div>
        </div>
      </div>

      <div v-else class="anp__upload">
        <label class="anp__upload-btn">
          <input type="file" accept="image/png" data-test="animation-upload" @change="onFile" />
          Choose PNG…
        </label>
        <p class="anp__hint">
          Stored per-ability as a single static image. Save the ability first (uploads key to its id).
        </p>
        <p v-if="uploadState" class="anp__upload-state" :class="{ 'anp__upload-state--err': uploadError }">
          {{ uploadState }}
        </p>
      </div>

      <footer class="anp__foot">
        <button type="button" class="anp__btn" @click="emit('close')">Cancel</button>
        <button
          type="button"
          class="anp__btn anp__btn--primary"
          :disabled="!draftAnimation"
          data-test="animation-confirm"
          @click="confirm"
        >Use Animation</button>
      </footer>
    </div>
  </div>
</template>

<script setup lang="ts">
// AnimationPicker: a modal for choosing a create_zone visual — the presentation
// effect or the visible sprite — from any of the game's sprite sources: effect
// sheets, projectile strips, beam sheets, object sprite-sets (with a per-object
// animation-STATE choice), or a custom uploaded PNG. Every thumbnail + the live
// preview play through AnimationRefCanvas (the shared drawAnimationDecal), so
// what you pick is exactly what renders in the match. Emits the scheme string.
import { computed, ref as vref } from 'vue'
import { listEffectNames } from '@/game/rendering/effectSprites'
import { listBeamNames } from '@/game/rendering/beamSprites'
import { registeredProjectileSpriteIds } from '@/game/rendering/projectileSpriteSheets'
import { listObjectSpriteKeys, listObjectAnimationStates } from '@/game/rendering/objectSprites'
import { parseAnimationRef, formatAnimationRef, type AnimationSource } from '@/game/rendering/animationRef'
import { invalidateServerAbilityIcon } from '@/game/rendering/abilityAssets'
import { uploadAbilityIcon } from '@/game/abilities/abilityEditorApi'
import AnimationRefCanvas from './AnimationRefCanvas.vue'

const props = defineProps<{
  /** Current stored animation scheme string. */
  modelAnimation?: string
  /** The ability id — the custom-upload key. */
  abilityId: string
}>()

const emit = defineEmits<{
  'update:animation': [animation: string]
  close: []
}>()

const TABS = [
  { id: 'effect', label: 'Effects' },
  { id: 'projectile', label: 'Projectiles' },
  { id: 'beam', label: 'Beams' },
  { id: 'object', label: 'Objects' },
  { id: 'upload', label: 'Upload' },
] as const
type TabId = (typeof TABS)[number]['id']

const effectNames = listEffectNames()
const beamNames = listBeamNames()
const projectileIds = registeredProjectileSpriteIds().sort()
const objectKeys = listObjectSpriteKeys()

// Draft selection, seeded from the current animation.
const parsed = parseAnimationRef(props.modelAnimation)
const source = vref<AnimationSource>(parsed?.source ?? 'effect')
const ref = vref<string>(parsed?.ref ?? '')
const objectState = vref<string>(parsed?.source === 'object' ? (parsed.state ?? 'idle') : 'idle')
const tab = vref<TabId>(
  parsed?.source === 'object' ? 'object'
  : parsed?.source === 'image' ? 'upload'
  : (parsed?.source ?? 'effect') as TabId,
)

// The animation states the currently-selected object defines.
const objectStates = computed(() => (source.value === 'object' && ref.value ? listObjectAnimationStates(ref.value) : []))

// An object thumbnail always shows its idle unless the selected object is this
// one AND a non-idle state is chosen (so the selected object's tile animates the
// picked state, others show idle).
function objectThumbAnim(key: string): string {
  if (source.value === 'object' && ref.value === key && objectState.value !== 'idle') {
    return `object:${key}@${objectState.value}`
  }
  return `object:${key}`
}

const draftAnimation = computed(() => {
  if (!ref.value) return ''
  if (source.value === 'object') return formatAnimationRef('object', ref.value, objectState.value)
  return formatAnimationRef(source.value, ref.value)
})

function selectSimple(s: AnimationSource, r: string) {
  source.value = s
  ref.value = r
}

function selectObject(key: string) {
  source.value = 'object'
  ref.value = key
  // Default to idle when switching objects; the state row lets the author change it.
  objectState.value = 'idle'
}

function confirm() {
  if (draftAnimation.value) emit('update:animation', draftAnimation.value)
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
    source.value = 'image'
    ref.value = props.abilityId
    uploadState.value = 'Uploaded. Preview above; click Use Animation.'
  } catch (err) {
    uploadError.value = true
    uploadState.value = err instanceof Error ? err.message : 'Upload failed'
  }
}
</script>

<style scoped>
.anp {
  position: fixed;
  inset: 0;
  z-index: 60;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: rgba(4, 8, 14, 0.66);
}

.anp__panel {
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

.anp__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.anp__title {
  font-family: var(--font-title);
  font-size: 0.9rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--ed-brass);
}

.anp__x {
  background: none;
  border: 0;
  color: var(--ed-text-dim);
  font-size: 1.3rem;
  line-height: 1;
  padding: 0 4px;
}

.anp__preview {
  display: flex;
  align-items: center;
  gap: 12px;
}

.anp__preview-cell {
  flex: 0 0 auto;
  width: 56px;
  height: 56px;
  padding: 3px;
  background: rgba(8, 14, 24, 0.6);
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
}

.anp__preview-code {
  /* Strip the global `code` rule's light box (background:var(--code-bg),
     padding, mono) — inside the dark editor it renders as an unreadable
     near-white block. Just gold text here. */
  font-family: var(--font-body);
  font-size: 0.76rem;
  color: var(--ed-brass);
  background: transparent;
  padding: 0;
}

.anp__tabs {
  display: flex;
  gap: 4px;
  border-bottom: 1px solid var(--ed-line);
}

.anp__tab {
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

.anp__tab:hover {
  color: var(--ed-brass);
}

.anp__tab--active {
  color: var(--ed-brass);
  border-color: var(--ed-line);
  border-bottom-color: transparent;
  background: rgba(212, 168, 71, 0.08);
}

.anp__object {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.anp__gallery {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(72px, 1fr));
  gap: 8px;
  overflow-y: auto;
  min-height: 120px;
  max-height: 320px;
  padding: 2px;
}

.anp__item {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  padding: 6px 4px;
  background: rgba(8, 14, 24, 0.4);
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
}

.anp__item > :first-child {
  width: 44px;
  height: 44px;
}

.anp__item:hover {
  border-color: var(--ed-line-strong);
}

.anp__item--sel {
  border-color: var(--ed-brass);
  background: rgba(212, 168, 71, 0.12);
}

.anp__item-label {
  font-size: 0.62rem;
  color: var(--ed-text-dim);
  text-align: center;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
}

.anp__states {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  padding: 6px 2px 0;
  border-top: 1px solid var(--ed-line);
}

.anp__states-label {
  font-size: 0.72rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--ed-text-dim);
}

.anp__states-row {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.anp__state {
  padding: 4px 10px;
  font-size: 0.72rem;
  font-weight: 600;
  color: var(--ed-text-dim);
  background: rgba(8, 14, 24, 0.4);
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
}

.anp__state--sel {
  color: var(--ed-brass);
  border-color: var(--ed-brass);
  background: rgba(212, 168, 71, 0.12);
}

.anp__upload {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-height: 120px;
  padding: 12px 2px;
}

.anp__upload-btn {
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

.anp__upload-btn input {
  max-width: 200px;
  font-size: 0.72rem;
}

.anp__hint {
  margin: 0;
  font-size: 0.74rem;
  color: var(--ed-text-dim);
}

.anp__upload-state {
  margin: 0;
  font-size: 0.76rem;
  color: var(--ed-ok);
}

.anp__upload-state--err {
  color: var(--ed-danger);
}

.anp__foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.anp__btn {
  padding: 6px 14px;
  font-family: var(--font-body);
  font-size: 0.78rem;
  font-weight: 600;
  color: var(--ed-text);
  background: rgba(15, 23, 42, 0.35);
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
}

.anp__btn--primary {
  color: var(--ed-brass);
  border-color: var(--ed-line-strong);
  background: rgba(212, 168, 71, 0.14);
}

.anp__btn--primary:disabled {
  opacity: 0.4;
}
</style>
