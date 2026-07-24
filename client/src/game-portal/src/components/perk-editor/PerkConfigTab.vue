<template>
  <GameScrollArea class="ps-setup">
    <SectionCard title="Config">
      <p v-if="configRows.length === 0" class="ps-setup__hint">No config values.</p>
      <div v-for="(row, idx) in configRows" :key="idx" class="ps-setup__map-row">
        <input v-model="row.key" placeholder="key" :aria-label="`Config ${idx + 1} key`" />
        <input v-model.number="row.value" type="number" :aria-label="`Config ${idx + 1} value`" />
        <button type="button" class="ps-setup__del" title="Remove" @click="removeConfig(idx)">✕</button>
      </div>
      <button type="button" class="ps-setup__add" @click="addConfig">+ Add Config Value</button>
    </SectionCard>

    <SectionCard title="Rank Config">
      <p class="ps-setup__hint">JSON: rank → (key → number). Blank for none.</p>
      <textarea class="ps-setup__json" rows="5" :value="configByRankText" @input="onRankInput(($event.target as HTMLTextAreaElement).value)" />
      <p v-if="configByRankError" class="ps-setup__error">{{ configByRankError }}</p>
    </SectionCard>
  </GameScrollArea>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import SectionCard from '@/components/editor/SectionCard.vue'
import GameScrollArea from '@/components/ui/GameScrollArea.vue'
import { usePerkBuilderContext } from './PerkBuilderContext'

const builder = usePerkBuilderContext()
const form = computed(() => builder.form.value)

interface MapRow { key: string; value: number }
const configRows = ref<MapRow[]>([])
const configByRankText = ref('')
const configByRankError = ref('')
function syncRankText(def?: Record<string, Record<string, number>>) {
  configByRankText.value = def && Object.keys(def).length ? JSON.stringify(def, null, 2) : ''
  configByRankError.value = ''
}
watch(() => builder.selectedId.value, () => {
  configRows.value = Object.entries(form.value.config ?? {}).map(([key, value]) => ({ key, value }))
  syncRankText(form.value.configByRank)
}, { immediate: true })
function addConfig() { configRows.value.push({ key: '', value: 0 }) }
function removeConfig(i: number) { configRows.value.splice(i, 1) }
watch(configRows, (rows) => {
  const out: Record<string, number> = {}
  for (const r of rows) if (r.key) out[r.key] = r.value
  builder.form.value = { ...builder.form.value, config: Object.keys(out).length ? out : undefined }
}, { deep: true })
function onRankInput(value: string) {
  configByRankText.value = value
  const trimmed = value.trim()
  if (!trimmed) { builder.form.value = { ...builder.form.value, configByRank: undefined }; configByRankError.value = ''; return }
  try { builder.form.value = { ...builder.form.value, configByRank: JSON.parse(trimmed) }; configByRankError.value = '' }
  catch { configByRankError.value = 'Invalid JSON — not saved until fixed.' }
}
</script>

<style scoped>
.ps-setup { display: flex; flex-direction: column; gap: var(--ed-gap); min-height: 0; padding-right: 4px; }
.ps-setup__hint { margin: 0; font-size: 0.76rem; color: var(--ed-text-dim); }
.ps-setup__error { margin: 0; font-size: 0.76rem; color: var(--ed-danger); }
.ps-setup__map-row { display: grid; grid-template-columns: 1fr 1fr auto; gap: 6px; align-items: center; }
.ps-setup__add { align-self: flex-start; padding: 4px 8px; font-size: 0.74rem; border: 1px solid var(--ed-line); border-radius: 4px; background: var(--ed-field); color: var(--ed-brass); }
.ps-setup__del { padding: 2px 6px; border: 1px solid transparent; border-radius: 4px; background: none; color: var(--ed-text-dim); }
.ps-setup__del:hover { color: var(--ed-danger); border-color: var(--ed-line); }
.ps-setup__json { width: 100%; font-family: var(--font-mono, monospace); }
</style>
