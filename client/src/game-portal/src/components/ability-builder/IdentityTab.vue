<template>
  <div class="ab-identity-tab" data-test="identity-tab">
    <!-- Ability-level validation issues, always at the top of the tab. Unlike
         the old rail (which showed issues for WHATEVER was selected), this
         tab is no longer selection-gated, so it only ever needs the
         ability-level subset — trigger/action issues surface in the bottom
         InspectorBar instead, next to the fields they're about. -->
    <div v-if="abilityIssues.length" class="ins-issues" data-test="identity-issues">
      <p
        v-for="(iss, idx) in abilityIssues"
        :key="idx"
        class="ins-issue"
        :class="iss.severity === 'error' ? 'ins-issue--error' : 'ins-issue--warning'"
      >{{ iss.message }}</p>
    </div>

    <SectionCard title="Identity">
      <SchemaField
        :field="{ key: 'displayName', label: 'Display Name', control: 'text' }"
        :model-value="builder.form.value.displayName"
        :enums="enumsValue"
        :catalogs="builder.catalogs.value"
        @update:model-value="(v) => builder.updateForm({ displayName: v as string })"
      />
      <SchemaField
        :field="{ key: 'type', label: 'Type', control: 'enum', options: ['', 'spell', 'passive'] }"
        :model-value="builder.form.value.type ?? ''"
        :enums="enumsValue"
        :catalogs="builder.catalogs.value"
        @update:model-value="(v) => builder.updateForm({ type: v as AuthoredAbilityDef['type'] })"
      />
      <SchemaField
        :field="{ key: 'category', label: 'Category', control: 'enum', options: categoryOptions }"
        :model-value="builder.form.value.category ?? ''"
        :enums="enumsValue"
        :catalogs="builder.catalogs.value"
        @update:model-value="(v) => builder.updateForm({ category: v as string })"
      />
      <SchemaField
        :field="{ key: 'damageType', label: 'Damage Type', control: 'enum', options: damageTypeOptions }"
        :model-value="builder.form.value.damageType ?? ''"
        :enums="enumsValue"
        :catalogs="builder.catalogs.value"
        @update:model-value="(v) => builder.updateForm({ damageType: v as string })"
      />
      <EditorField label="Tags" hint="(comma-separated)" for-id="ins-tags">
        <input id="ins-tags" type="text" :value="tagsText" @input="onTagsInput" @change="commitTags" />
      </EditorField>
    </SectionCard>

    <!-- Entry is READ-ONLY here on purpose: the program's `entry` (type/
         relations/range) has no builder op to edit it directly (the
         composable only exposes updateForm/updateTrigger/updateAction, all
         of which operate on the FORM or the TRIGGER/ACTION tree — nothing
         mutates `program.entry`). The cast gates (Phase-4) read the FORM's
         canTarget*/targetsPoint/castRange fields, not `entry`, so those are
         what this tab makes editable below; `entry` is shown as a derived
         summary so an author isn't left wondering where it went.
         TODO: add an `updateEntry` composable op if authoring `entry`
         directly (rather than via the cast-setup fields) becomes required. -->
    <SectionCard title="Entry (read-only)">
      <p class="ins-note">
        Type: {{ builder.program.value.entry.type }}<br />
        Relations: {{ (builder.program.value.entry.relations ?? []).join(', ') || '—' }}<br />
        Range: {{ builder.program.value.entry.range }}
      </p>
      <p class="ins-hint">
        Targeting is edited via the Cast Setup fields below (Can Target */Targets Point/Cast Range) —
        those are what the cast gates read.
      </p>
    </SectionCard>

    <SectionCard title="Cast Setup">
      <div class="ins-pair">
        <SchemaField
          :field="{ key: 'manaCost', label: 'Mana Cost', control: 'number' }"
          :model-value="builder.form.value.manaCost ?? 0"
          :enums="enumsValue"
          :catalogs="builder.catalogs.value"
          @update:model-value="(v) => builder.updateForm({ manaCost: v as number })"
        />
        <SchemaField
          :field="{ key: 'cooldown', label: 'Cooldown (s)', control: 'number' }"
          :model-value="builder.form.value.cooldown ?? 0"
          :enums="enumsValue"
          :catalogs="builder.catalogs.value"
          @update:model-value="(v) => builder.updateForm({ cooldown: v as number })"
        />
      </div>
      <SchemaField
        :field="{ key: 'castTime', label: 'Cast Time (s)', control: 'number' }"
        :model-value="builder.form.value.castTime ?? 0"
        :enums="enumsValue"
        :catalogs="builder.catalogs.value"
        @update:model-value="(v) => builder.updateForm({ castTime: v as number })"
      />
      <SchemaField
        :field="{ key: 'castRange', label: 'Cast Range', control: 'sentinel_number' }"
        :model-value="builder.form.value.castRange ?? 0"
        :enums="enumsValue"
        :catalogs="builder.catalogs.value"
        @update:model-value="(v) => builder.updateForm({ castRange: v as number | 'match_attack_range' })"
      />
      <SchemaField
        :field="{ key: 'canTargetSelf', label: 'Can target self', control: 'boolean' }"
        :model-value="!!builder.form.value.canTargetSelf"
        :enums="enumsValue"
        :catalogs="builder.catalogs.value"
        @update:model-value="(v) => builder.updateForm({ canTargetSelf: v as boolean })"
      />
      <SchemaField
        :field="{ key: 'canTargetAllies', label: 'Can target allies', control: 'boolean' }"
        :model-value="!!builder.form.value.canTargetAllies"
        :enums="enumsValue"
        :catalogs="builder.catalogs.value"
        @update:model-value="(v) => builder.updateForm({ canTargetAllies: v as boolean })"
      />
      <SchemaField
        :field="{ key: 'canTargetEnemies', label: 'Can target enemies', control: 'boolean' }"
        :model-value="!!builder.form.value.canTargetEnemies"
        :enums="enumsValue"
        :catalogs="builder.catalogs.value"
        @update:model-value="(v) => builder.updateForm({ canTargetEnemies: v as boolean })"
      />
      <SchemaField
        :field="{ key: 'targetsPoint', label: 'Targets a ground point', control: 'boolean' }"
        :model-value="!!builder.form.value.targetsPoint"
        :enums="enumsValue"
        :catalogs="builder.catalogs.value"
        @update:model-value="(v) => builder.updateForm({ targetsPoint: v as boolean })"
      />
      <SchemaField
        :field="{ key: 'supportsAutoCast', label: 'Supports auto-cast', control: 'boolean' }"
        :model-value="!!builder.form.value.supportsAutoCast"
        :enums="enumsValue"
        :catalogs="builder.catalogs.value"
        @update:model-value="(v) => builder.updateForm({ supportsAutoCast: v as boolean })"
      />
      <template v-if="builder.form.value.supportsAutoCast">
        <SchemaField
          :field="{ key: 'autoCastTargetSelector', label: 'Target Selector', control: 'enum', options: autoCastOptions }"
          :model-value="builder.form.value.autoCastTargetSelector ?? ''"
          :enums="enumsValue"
          :catalogs="builder.catalogs.value"
          @update:model-value="(v) => builder.updateForm({ autoCastTargetSelector: v as string })"
        />
        <SchemaField
          :field="{ key: 'defaultAutoCast', label: 'Enabled by default', control: 'boolean' }"
          :model-value="!!builder.form.value.defaultAutoCast"
          :enums="enumsValue"
          :catalogs="builder.catalogs.value"
          @update:model-value="(v) => builder.updateForm({ defaultAutoCast: v as boolean })"
        />
      </template>
    </SectionCard>
  </div>
</template>

<script setup lang="ts">
// IdentityTab: the main-area "Identity" tab (was the `ability`-kind branch
// of the old rail ItemInspector — see docs/superpowers/plans/
// 2026-07-16-ability-builder-ui-corrections.md Task 3). Unlike the old rail,
// this is NOT selection-gated: it always shows the current ability's
// identity/entry/cast-setup fields regardless of what's selected in the flow
// (trigger/action editing lives in the bottom InspectorBar now).
import { computed, ref, watch } from 'vue'
import type { AuthoredAbilityDef } from '@/game/abilities/abilityEditorForm'
import EditorField from '@/components/editor/EditorField.vue'
import SectionCard from '@/components/editor/SectionCard.vue'
import type { ValidationIssue } from '@/game/abilities/program/programValidation'
import { useAbilityBuilderContext } from './AbilityBuilderContext'
import SchemaField from './SchemaField.vue'

const builder = useAbilityBuilderContext()

const enumsValue = computed(() => builder.schema.value?.enums ?? {})

// Ability-level issues live at fixed paths (identity.id, identity.
// damageType, identity.category, mechanics.burn — see EditorAbilityIssues in
// ability_editor.go). Filtering by "not under a trigger index" is robust to
// new ability-level paths appearing later without needing an exact list
// kept in sync with the server here.
const abilityIssues = computed<ValidationIssue[]>(() =>
  builder.issues.value.filter((i) => !i.path.startsWith('triggers[')),
)

// ── Identity option lists ────────────────────────────────────────────────
const categoryOptions = computed(() => ['', ...builder.catalogs.value.categories])
const damageTypeOptions = computed(() => ['', ...builder.catalogs.value.damageTypes])
const autoCastOptions = computed(() => ['', ...builder.catalogs.value.autoCastSelectors])

// Tags: comma-separated text <-> string[]. Kept local-copy-then-commit like
// every other text control (see SchemaField's doc comment on the pattern) —
// not routed through SchemaField itself because its `text` control commits a
// raw string, not a parsed array.
const tagsText = ref((builder.form.value.tags ?? []).join(', '))
watch(
  () => builder.form.value.tags,
  (v) => {
    tagsText.value = (v ?? []).join(', ')
  },
)
function onTagsInput(e: Event) {
  tagsText.value = (e.target as HTMLInputElement).value
}
function commitTags() {
  const list = tagsText.value.split(',').map((s) => s.trim()).filter(Boolean)
  builder.updateForm({ tags: list })
}
</script>

<style scoped>
.ab-identity-tab {
  display: flex;
  flex-direction: column;
  gap: var(--ed-gap);
  min-width: 0;
}

.ins-issues {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 8px 10px;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
  background: rgba(15, 23, 42, 0.25);
}

.ins-issue {
  margin: 0;
  font-size: 0.76rem;
}

.ins-issue--error {
  color: var(--ed-danger);
}

.ins-issue--warning {
  color: #e0b258;
}

.ins-note {
  margin: 0;
  font-size: 0.8rem;
  color: var(--ed-text);
  line-height: 1.5;
}

.ins-hint {
  margin: 0;
  font-size: 0.78rem;
  color: var(--ed-text-dim);
}

.ins-pair {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}
</style>
