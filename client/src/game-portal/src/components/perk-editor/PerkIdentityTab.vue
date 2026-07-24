<template>
  <GameScrollArea class="ps-setup">
    <SectionCard title="Identity">
      <EditorField label="Display Name">
        <input v-model="form.displayName" aria-label="Display Name" />
      </EditorField>
      <EditorField label="Description">
        <textarea v-model="form.description" rows="2" aria-label="Description" />
      </EditorField>
      <EditorField label="Icon">
        <input v-model="form.icon" aria-label="Icon" />
      </EditorField>
    </SectionCard>

    <SectionCard title="Eligibility">
      <EditorField label="Association" hint="(catalog folder)">
        <select v-if="builder.selectedId.value === null" v-model="association" data-test="association-select">
          <option value="">Generic</option>
          <optgroup v-for="[unit, ps] in sortedPathsByUnit" :key="unit" :label="unitLabel(unit)">
            <option v-for="p in ps" :key="p" :value="p">{{ p }}</option>
          </optgroup>
        </select>
        <input v-else :value="form.path || 'generic'" disabled />
      </EditorField>
      <EditorField label="Requires Perk">
        <input v-model="form.requiresPerk" list="perk-builder-perk-ids" placeholder="(none)" />
      </EditorField>
      <EditorField label="Requires Ability">
        <button type="button" class="ps-dropdown" aria-label="Requires Ability" @click="reqAbilityPicker = true">
          <span class="ps-dropdown__val" :class="{ 'ps-dropdown__val--empty': !form.requiresAbility }">{{ form.requiresAbility || '(none)' }}</span>
          <span class="ps-dropdown__caret" aria-hidden="true">▾</span>
        </button>
        <AbilityPicker
          v-if="reqAbilityPicker"
          :abilities="builder.abilityDefs.value"
          :model-value="form.requiresAbility || ''"
          @select="(id) => { form.requiresAbility = id || undefined; reqAbilityPicker = false }"
          @close="reqAbilityPicker = false"
        />
      </EditorField>
    </SectionCard>

    <SectionCard title="Tooltip">
      <EditorField label="Generated" hint="(read-only)">
        <textarea :value="generated" rows="2" readonly class="perk-editor__generated" />
      </EditorField>
      <EditorField label="Override Template" hint="(overrides generated when set)">
        <textarea v-model="form.tooltipTemplate" rows="3" />
      </EditorField>
    </SectionCard>
  </GameScrollArea>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import SectionCard from '@/components/editor/SectionCard.vue'
import EditorField from '@/components/editor/EditorField.vue'
import GameScrollArea from '@/components/ui/GameScrollArea.vue'
import AbilityPicker from '@/components/ability-builder/AbilityPicker.vue'
import { usePerkBuilderContext } from './PerkBuilderContext'

const builder = usePerkBuilderContext()
const form = computed(() => builder.form.value)
const reqAbilityPicker = ref(false)

const generated = computed(() => form.value.generatedDescription?.trim() || '(no typed data to generate from)')

const association = computed<string>({
  get: () => form.value.path ?? '',
  set: (v) => { builder.form.value = { ...builder.form.value, path: v || undefined } },
})
const sortedPathsByUnit = computed<Array<[string, string[]]>>(() =>
  Object.entries(builder.pathsByUnit.value)
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([u, ps]) => [u, [...ps].sort((x, y) => x.localeCompare(y))]))
function unitLabel(u: string): string { return u ? u[0].toUpperCase() + u.slice(1) : u }
</script>

<style scoped>
.ps-setup { display: flex; flex-direction: column; gap: var(--ed-gap); min-height: 0; padding-right: 4px; }
.ps-dropdown { display: flex; align-items: center; justify-content: space-between; gap: 8px; width: 100%; padding: 6px 8px; background: var(--ed-field); border: 1px solid var(--ed-line); border-radius: 4px; color: var(--ed-text); text-align: left; font: inherit; }
.ps-dropdown:hover { border-color: var(--ed-line-strong); }
.ps-dropdown__val { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ps-dropdown__val--empty { color: var(--ed-text-dim); }
.ps-dropdown__caret { flex: 0 0 auto; color: var(--ed-text-dim); font-size: 0.7rem; }
</style>
