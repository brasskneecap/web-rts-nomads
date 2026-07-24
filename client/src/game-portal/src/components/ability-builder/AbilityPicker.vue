<template>
  <div class="abp" data-test="ability-picker" @click.self="emit('close')">
    <div class="abp__panel" role="dialog" aria-label="Choose ability">
      <header class="abp__head">
        <span class="abp__title">Choose Ability</span>
        <button type="button" class="abp__x" aria-label="Close" @click="emit('close')">&times;</button>
      </header>

      <input
        v-model="search"
        type="search"
        class="abp__search"
        placeholder="Search abilities…"
        aria-label="Search abilities"
      />

      <div class="abp__body">
        <ul class="abp__list" data-test="ability-picker-list">
          <li>
            <button
              type="button"
              class="abp__item abp__item--meta"
              :class="{ 'abp__item--sel': chosen && draftId === '' }"
              data-test="ability-picker-none"
              @click="draftId = ''; chosen = true"
              @dblclick="confirm"
            >
              <span class="abp__item-text"><span class="abp__item-name">— None —</span><span class="abp__item-id">clear this field</span></span>
            </button>
          </li>
          <li v-for="ab in filtered" :key="ab.id">
            <button
              type="button"
              class="abp__item"
              :class="{ 'abp__item--sel': ab.id === draftId }"
              :data-test="`ability-picker-item-${ab.id}`"
              @click="draftId = ab.id; chosen = true"
              @dblclick="confirm"
            >
              <span class="abp__item-icon">
                <AbilityIconCanvas :icon="ab.icon" :ability-id="ab.id" :projectile="ab.projectile" :size="40" />
              </span>
              <span class="abp__item-text">
                <span class="abp__item-name">{{ ab.displayName || ab.id }}</span>
                <span class="abp__item-id">{{ ab.id }}</span>
              </span>
            </button>
          </li>
          <li v-if="!filtered.length" class="abp__empty">No abilities match.</li>
          <li v-if="freeValue">
            <button
              type="button"
              class="abp__item abp__item--meta"
              :class="{ 'abp__item--sel': chosen && draftId === freeValue }"
              data-test="ability-picker-use"
              @click="draftId = freeValue; chosen = true"
              @dblclick="confirm"
            >
              <span class="abp__item-text"><span class="abp__item-name">Use “{{ freeValue }}”</span><span class="abp__item-id">custom id / tag</span></span>
            </button>
          </li>
        </ul>

        <div class="abp__detail">
          <template v-if="selected">
            <div class="abp__detail-head">
              <span class="abp__detail-icon">
                <AbilityIconCanvas :icon="selected.icon" :ability-id="selected.id" :projectile="selected.projectile" :size="56" />
              </span>
              <div class="abp__detail-title">
                <span class="abp__detail-name">{{ selected.displayName || selected.id }}</span>
                <code class="abp__detail-id">{{ selected.id }}</code>
              </div>
            </div>
            <p class="abp__detail-desc">{{ description }}</p>
          </template>
          <p v-else class="abp__detail-empty">Select an ability to see its details.</p>
        </div>
      </div>

      <footer class="abp__foot">
        <button type="button" class="abp__btn" @click="emit('close')">Cancel</button>
        <button
          type="button"
          class="abp__btn abp__btn--primary"
          :disabled="!chosen"
          data-test="ability-picker-confirm"
          @click="confirm"
        >Select</button>
      </footer>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import AbilityIconCanvas from './AbilityIconCanvas.vue'
import type { AuthoredAbilityDef } from '@/game/abilities/abilityEditorForm'

const props = defineProps<{
  abilities: AuthoredAbilityDef[]
  modelValue?: string
}>()
const emit = defineEmits<{ select: [id: string]; close: [] }>()

const search = ref('')
const draftId = ref(props.modelValue ?? '')
const chosen = ref(props.modelValue !== undefined && props.modelValue !== '')

const filtered = computed(() => {
  const list = [...props.abilities].sort((a, b) => (a.displayName || a.id).localeCompare(b.displayName || b.id))
  const q = search.value.trim().toLowerCase()
  if (!q) return list
  return list.filter((a) => a.id.toLowerCase().includes(q) || (a.displayName ?? '').toLowerCase().includes(q))
})

const freeValue = computed(() => {
  const q = search.value.trim()
  return q && !props.abilities.some((a) => a.id === q) ? q : ''
})

const selected = computed(() => props.abilities.find((a) => a.id === draftId.value) ?? null)
const description = computed(() =>
  selected.value?.description?.trim() || selected.value?.generatedDescription?.trim() || '(no description)')

function confirm() {
  if (chosen.value) { emit('select', draftId.value); emit('close') }
}
</script>

<style scoped>
/* Mirrors AbilityIconPicker's modal chrome (overlay, panel, brass footer). */
.abp { position: fixed; inset: 0; z-index: 60; display: flex; align-items: center; justify-content: center; padding: 24px; background: rgba(4, 8, 14, 0.66); }
.abp__panel { display: flex; flex-direction: column; gap: 12px; width: min(640px, 100%); max-height: 100%; padding: 16px; background: var(--ed-bg, #14100a); border: 1px solid var(--ed-line-strong, #4a3b22); border-radius: var(--ed-radius); box-shadow: 0 12px 40px rgba(0, 0, 0, 0.5); }
.abp__head { display: flex; align-items: center; justify-content: space-between; }
.abp__title { font-family: var(--font-title); font-size: 0.9rem; font-weight: 700; letter-spacing: 0.08em; text-transform: uppercase; color: var(--ed-brass); }
.abp__x { background: none; border: 0; color: var(--ed-text-dim); font-size: 1.3rem; line-height: 1; padding: 0 4px; }
.abp__search { width: 100%; }
.abp__body { display: grid; grid-template-columns: minmax(0, 1fr) minmax(200px, 260px); gap: 12px; min-height: 0; }
.abp__list { list-style: none; margin: 0; padding: 2px; display: flex; flex-direction: column; gap: 4px; overflow-y: auto; min-height: 160px; max-height: 360px; }
.abp__item { display: flex; align-items: center; gap: 10px; width: 100%; padding: 6px 8px; background: rgba(8, 14, 24, 0.4); border: 1px solid var(--ed-line); border-radius: var(--ed-radius); text-align: left; }
.abp__item:hover { border-color: var(--ed-line-strong); }
.abp__item--sel { border-color: var(--ed-brass); background: rgba(212, 168, 71, 0.12); }
.abp__item--meta { border-style: dashed; }
.abp__item--meta .abp__item-name { font-style: italic; }
.abp__item-icon { flex: 0 0 auto; width: 40px; height: 40px; }
.abp__item-text { display: flex; flex-direction: column; min-width: 0; }
.abp__item-name { font-size: 0.84rem; color: var(--ed-text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.abp__item-id { font-size: 0.66rem; color: var(--ed-text-dim); font-family: var(--mono, monospace); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.abp__empty { padding: 12px 8px; font-size: 0.8rem; color: var(--ed-text-dim); }
.abp__detail { display: flex; flex-direction: column; gap: 10px; padding: 10px; background: rgba(8, 14, 24, 0.4); border: 1px solid var(--ed-line); border-radius: var(--ed-radius); min-height: 0; overflow-y: auto; }
.abp__detail-head { display: flex; align-items: center; gap: 10px; }
.abp__detail-icon { flex: 0 0 auto; width: 56px; height: 56px; }
.abp__detail-title { display: flex; flex-direction: column; min-width: 0; }
.abp__detail-name { font-family: var(--font-title); font-size: 0.9rem; font-weight: 700; color: var(--ed-text); }
.abp__detail-id { font-size: 0.68rem; color: var(--ed-brass); background: transparent; padding: 0; }
.abp__detail-desc { margin: 0; font-size: 0.8rem; line-height: 1.5; color: var(--ed-text-dim); white-space: pre-wrap; }
.abp__detail-empty { margin: auto; font-size: 0.8rem; color: var(--ed-text-dim); text-align: center; }
.abp__foot { display: flex; justify-content: flex-end; gap: 8px; }
.abp__btn { padding: 6px 14px; font-family: var(--font-body); font-size: 0.78rem; font-weight: 600; color: var(--ed-text); background: rgba(15, 23, 42, 0.35); border: 1px solid var(--ed-line); border-radius: var(--ed-radius); }
.abp__btn--primary { color: var(--ed-brass); border-color: var(--ed-line-strong); background: rgba(212, 168, 71, 0.14); }
.abp__btn--primary:disabled { opacity: 0.4; }
</style>
