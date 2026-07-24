<template>
  <SectionCard v-if="entry" :title="entry.meta.label" :style="{ '--mk-accent': entry.meta.accent }" data-test="perk-inspector">
    <!-- Unit Stat Modifier -->
    <template v-if="entry.kind === 'unitStat'">
      <EditorField label="Stat">
        <select v-model="unitStat.stat" aria-label="Stat" @change="commitUnitStat">
          <option v-for="d in builder.selfStatDefsList" :key="d.id" :value="d.id">{{ d.label }}</option>
        </select>
      </EditorField>
      <EditorField label="Operation">
        <select v-model="unitStat.op" aria-label="Operation" @change="commitUnitStat">
          <option value="add">Add</option>
          <option value="multiply">Multiply</option>
        </select>
      </EditorField>
      <EditorField label="Value">
        <input v-model.number="unitStat.value" type="number" step="0.05" aria-label="Value" @input="commitUnitStat" />
      </EditorField>
      <EditorField label="Stage">
        <select v-model="unitStat.stage" aria-label="Stage" @change="commitUnitStat">
          <option value="intrinsic">Intrinsic (scales this unit's own base only)</option>
          <option value="base">Base</option>
          <option value="final">Final (applied after everything else)</option>
        </select>
      </EditorField>
    </template>

    <!-- Ability Stat Modifier -->
    <template v-else-if="entry.kind === 'abilityStat'">
      <EditorField label="Stat">
        <select v-model="abilityStat.stat" aria-label="Ability Stat" @change="commitAbilityStat">
          <option value="">Pick a stat…</option>
          <optgroup label="Ability shape">
            <option v-for="d in shapeStats" :key="d.id" :value="d.id">{{ d.label }}</option>
          </optgroup>
          <optgroup label="Applies to target">
            <option v-for="d in targetStats" :key="d.id" :value="d.id">{{ d.label }}</option>
          </optgroup>
          <option v-if="abilityStat.stat && !knownStat" :value="abilityStat.stat">{{ abilityStat.stat }}</option>
        </select>
      </EditorField>
      <EditorField label="Ability Filter" hint="(blank = all abilities)">
        <button type="button" class="pi-dropdown" aria-label="Ability Filter"
          @click="openAbilityPicker(abilityStat.ability, (id) => { abilityStat.ability = id; commitAbilityStat() })">
          <span class="pi-dropdown__val" :class="{ 'pi-dropdown__val--empty': !abilityStat.ability }">{{ abilityStat.ability || 'All abilities' }}</span>
          <span class="pi-dropdown__caret" aria-hidden="true">▾</span>
        </button>
      </EditorField>
      <EditorField label="Flat">
        <input v-model.number="abilityStat.flat" type="number" step="0.5" aria-label="Flat" @input="commitAbilityStat" />
      </EditorField>
      <EditorField v-if="allowsPct" label="Percent" hint="(whole %)">
        <input v-model.number="abilityStat.pct" type="number" step="5" aria-label="Percent" @input="commitAbilityStat" />
      </EditorField>
    </template>

    <!-- Ability Field Modifier -->
    <template v-else-if="entry.kind === 'abilityField'">
      <EditorField label="Ability">
        <button type="button" class="pi-dropdown" aria-label="Ability"
          @click="openAbilityPicker(abilityField.target, (id) => { abilityField.target = id; commitAbilityField() })">
          <span class="pi-dropdown__val" :class="{ 'pi-dropdown__val--empty': !abilityField.target }">{{ abilityField.target || 'Choose ability or tag…' }}</span>
          <span class="pi-dropdown__caret" aria-hidden="true">▾</span>
        </button>
      </EditorField>
      <EditorField label="Action">
        <select v-model="abilityField.action" aria-label="Action" @change="commitAbilityField">
          <option value="">Pick an action…</option>
          <option v-for="a in actions" :key="a.id" :value="a.id">{{ a.label }}</option>
          <option v-if="abilityField.action && !actions.some((a) => a.id === abilityField.action)" :value="abilityField.action">
            {{ abilityField.action }} (not in this ability)
          </option>
        </select>
      </EditorField>
      <EditorField label="Field">
        <select v-model="abilityField.field" aria-label="Field" @change="commitAbilityField">
          <option value="">Pick a field…</option>
          <option v-for="f in fields" :key="f" :value="f">{{ f }}</option>
          <option v-if="abilityField.field && !fields.includes(abilityField.field)" :value="abilityField.field">
            {{ abilityField.field }} (not on this action)
          </option>
        </select>
      </EditorField>
      <EditorField label="Operation">
        <select v-model="abilityField.op" aria-label="Operation" @change="commitAbilityField">
          <option value="multiply">× multiply</option>
          <option value="add">+ add</option>
          <option value="amplify">amplify</option>
        </select>
      </EditorField>
      <EditorField label="Value">
        <input v-model.number="abilityField.value" type="number" step="0.05" aria-label="Value" @input="commitAbilityField" />
      </EditorField>
      <EditorField label="Stage">
        <select v-model="abilityField.stage" aria-label="Stage" @change="commitAbilityField">
          <option value="">Base</option>
          <option value="intrinsic">Intrinsic</option>
          <option value="final">Final</option>
        </select>
      </EditorField>
      <p class="pi-preview">{{ fieldPreview(abilityField.op, abilityField.value as number) }}</p>
    </template>

    <!-- Ability Modifier -->
    <template v-else-if="entry.kind === 'abilityModifier'">
      <EditorField label="Target Ability">
        <button type="button" class="pi-dropdown" aria-label="Target Ability"
          @click="openAbilityPicker(abilityMod.target, (id) => { abilityMod.target = id; commitAbilityMod() })">
          <span class="pi-dropdown__val" :class="{ 'pi-dropdown__val--empty': !abilityMod.target }">{{ abilityMod.target || 'Choose ability…' }}</span>
          <span class="pi-dropdown__caret" aria-hidden="true">▾</span>
        </button>
      </EditorField>
      <EditorField label="Mana cost ×"><input v-model.number="abilityMod.manaCostMult" type="number" step="0.05" placeholder="—" aria-label="Mana mult" @input="commitAbilityMod" /></EditorField>
      <EditorField label="Range ×"><input v-model.number="abilityMod.rangeMult" type="number" step="0.05" placeholder="—" aria-label="Range mult" @input="commitAbilityMod" /></EditorField>
      <EditorField label="Cooldown ×"><input v-model.number="abilityMod.cooldownMult" type="number" step="0.05" placeholder="—" aria-label="Cooldown mult" @input="commitAbilityMod" /></EditorField>
    </template>

    <!-- Grant Ability -->
    <template v-else-if="entry.kind === 'grantAbility'">
      <EditorField label="Granted Ability" hint="(ability id this perk adds to the unit)">
        <button type="button" class="pi-dropdown" aria-label="Granted Ability"
          @click="openAbilityPicker(grant.id, (id) => { grant.id = id; commitGrant() })">
          <span class="pi-dropdown__val" :class="{ 'pi-dropdown__val--empty': !grant.id }">{{ grant.id || 'Choose ability…' }}</span>
          <span class="pi-dropdown__caret" aria-hidden="true">▾</span>
        </button>
      </EditorField>
    </template>

    <!-- Perk Modifier -->
    <template v-else-if="entry.kind === 'perkModifier'">
      <EditorField label="Target Perk" hint="(enhanced when the owner also has it)">
        <input v-model="perkMod.target" list="perk-builder-perk-ids" placeholder="perk id" aria-label="Target Perk" @input="commitPerkMod" />
      </EditorField>
      <div v-for="(op, i) in perkMod.ops" :key="i" class="pi-op-row">
        <EditorField label="Target key"><input v-model="op.targetKey" :aria-label="`Op ${i + 1} target key`" @input="commitPerkMod" /></EditorField>
        <EditorField label="Op">
          <select v-model="op.op" :aria-label="`Op ${i + 1} op`" @change="commitPerkMod">
            <option value="mult">× multiply</option>
            <option value="add">+ add</option>
          </select>
        </EditorField>
        <EditorField label="Source key"><input v-model="op.sourceKey" list="perk-builder-own-config-keys" :aria-label="`Op ${i + 1} source key`" @input="commitPerkMod" /></EditorField>
        <button type="button" class="pi-op-del" title="Remove op" @click="removePerkModOp(i)">✕</button>
      </div>
      <button type="button" class="pi-op-add" @click="addPerkModOp">+ Add Op</button>
      <datalist id="perk-builder-own-config-keys">
        <option v-for="k in ownConfigKeys" :key="k" :value="k" />
      </datalist>
    </template>

    <!-- Aura -->
    <template v-else-if="entry.kind === 'aura'">
      <AuraEditor :model-value="auraRow" :stat-defs="builder.auraStatDefsList" @update:model-value="onAuraRow" />
    </template>

    <!-- Ability Rider -->
    <template v-else-if="entry.kind === 'abilityRider'">
      <RiderEditor
        v-if="builder.riderSchema.value"
        :model-value="riderModel"
        :ability-ids="builder.abilityIds.value"
        :schema="builder.riderSchema.value"
        :catalogs="builder.riderCatalogs.value"
        @update:model-value="onRider"
      />
      <p v-else class="pi-note pi-note--dim">Loading ability schema…</p>
    </template>

    <!-- Cosmetic Effect -->
    <template v-else-if="entry.kind === 'effect'">
      <EditorField label="Effect name"><input v-model="effect.name" aria-label="Effect name" @input="commitEffect" /></EditorField>
      <EditorField label="Target">
        <select v-model="effect.target" aria-label="Effect target" @change="commitEffect">
          <option value="">(none)</option>
          <option value="self">self</option>
          <option value="enemies">enemies</option>
        </select>
      </EditorField>
      <EditorField label="Size scale"><input v-model.number="effect.sizeScale" type="number" step="0.1" placeholder="—" aria-label="Effect size scale" @input="commitEffect" /></EditorField>
      <EditorField label="Duration (s)"><input v-model.number="effect.durationSeconds" type="number" step="0.5" placeholder="—" aria-label="Effect duration" @input="commitEffect" /></EditorField>
      <EditorField label="Variant"><input v-model="effect.variant" aria-label="Effect variant" @input="commitEffect" /></EditorField>
    </template>

    <!-- Un-migrated kinds -->
    <template v-else>
      <p class="pi-note"><strong>{{ entry.summary }}</strong></p>
      <p class="pi-note pi-note--dim">
        Editing <em>{{ entry.meta.label }}</em> in the new builder is coming soon.
        Open this perk in the <strong>Classic</strong> editor to change it. Saving here
        preserves it unchanged.
      </p>
    </template>

    <AbilityPicker
      v-if="pickerOpen"
      :abilities="builder.abilityDefs.value"
      :model-value="pickerCurrent"
      @select="onPickAbility"
      @close="pickerOpen = false"
    />
  </SectionCard>

  <div v-else class="pi-empty" data-test="perk-inspector-empty">
    <p>Select a modifier to edit it, or add one from the stack.</p>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import SectionCard from '@/components/editor/SectionCard.vue'
import EditorField from '@/components/editor/EditorField.vue'
import { usePerkBuilderContext } from './PerkBuilderContext'
import { actionsForTarget, fieldsForAction, fieldPreview } from './abilityFieldOptions'
import AuraEditor, { type AuraRow } from './AuraEditor.vue'
import RiderEditor from './RiderEditor.vue'
import AbilityPicker from '@/components/ability-builder/AbilityPicker.vue'
import type { PerkStatModifier, PerkAbilityStat, AbilityFieldModifier, AbilityModifier, PerkModifier, PerkConfigOp, PerkAura, AbilityRider, PerkEffectShape } from '@/game/perks/perkEditorForm'

const builder = usePerkBuilderContext()
const entry = computed(() => builder.selectedEntry.value)

const unitStat = reactive({ stat: '', op: 'add' as 'add' | 'multiply', value: 0 as number | '', stage: 'base' as 'intrinsic' | 'base' | 'final' })
const abilityStat = reactive({ stat: '', ability: '', flat: '' as number | '', pct: '' as number | '' })
const abilityField = reactive({ target: '', action: '', field: '', op: 'multiply', value: '' as number | '', stage: '' })
const abilityMod = reactive({ target: '', manaCostMult: '' as number | '', rangeMult: '' as number | '', cooldownMult: '' as number | '' })
const ABILITY_MOD_MULT_KEYS = ['manaCostMult', 'rangeMult', 'cooldownMult'] as const
const grant = reactive({ id: '' })
const perkMod = reactive<{ target: string; ops: { targetKey: string; op: 'mult' | 'add'; sourceKey: string }[] }>({ target: '', ops: [] })
const ownConfigKeys = computed(() => Object.keys(builder.form.value.config ?? {}))
const auraRow = ref<AuraRow>({ radius: 128, targets: 'allies', includeSelf: false, perAdditionalSource: '', statRows: [], ringColor: '' })
const effect = reactive({ name: '', target: '', sizeScale: '' as number | '', durationSeconds: '' as number | '', variant: '' })

const pickerOpen = ref(false)
const pickerCurrent = ref('')
let pickerApply: (id: string) => void = () => {}
function openAbilityPicker(current: string, apply: (id: string) => void) {
  pickerCurrent.value = current
  pickerApply = apply
  pickerOpen.value = true
}
function onPickAbility(id: string) {
  pickerApply(id)
  pickerOpen.value = false
}

// rowFromAura/auraFromRow mirror PerkEditorPanel.vue's rowsFromAuras/aurasFromRows
// EXACTLY (see that file for the full rationale) — stacking is always 'max',
// includeSelf/perAdditionalSource/ringColor are omitted when unset, and every
// stat row always emits op: 'add' (the server rejects anything else for auras).
function rowFromAura(a: PerkAura): AuraRow {
  return {
    radius: a.radius,
    targets: a.targets,
    includeSelf: a.includeSelf ?? false,
    perAdditionalSource: a.perAdditionalSource ?? '',
    statRows: (a.statModifiers ?? []).map((m) => ({ stat: m.stat, value: m.value })),
    ringColor: a.ringColor ?? '',
  }
}
function auraFromRow(row: AuraRow): PerkAura {
  const radius = typeof row.radius === 'number' && !Number.isNaN(row.radius) ? row.radius : 0
  const statModifiers: PerkStatModifier[] = []
  for (const sr of row.statRows) {
    if (!sr.stat) continue
    const value = typeof sr.value === 'number' && !Number.isNaN(sr.value) ? sr.value : 0
    statModifiers.push({ stat: sr.stat, op: 'add', value })
  }
  const aura: PerkAura = { radius, targets: row.targets, stacking: 'max', statModifiers }
  if (row.includeSelf) aura.includeSelf = true
  if (typeof row.perAdditionalSource === 'number' && !Number.isNaN(row.perAdditionalSource)) aura.perAdditionalSource = row.perAdditionalSource
  if (row.ringColor) aura.ringColor = row.ringColor
  return aura
}

function current<T>(): T | undefined {
  const e = entry.value
  if (!e) return undefined
  const source = builder.form.value[e.arrayKey]
  if (e.meta.shape === 'single') return source as T
  return (source as unknown[])?.[e.index] as T
}

watch(entry, (e) => {
  if (!e) return
  if (e.kind === 'unitStat') {
    const m = current<PerkStatModifier>()!
    Object.assign(unitStat, { stat: m.stat, op: m.op, value: m.value, stage: m.stage ?? 'base' })
  } else if (e.kind === 'abilityStat') {
    const m = current<PerkAbilityStat>()!
    Object.assign(abilityStat, {
      stat: m.stat, ability: m.ability ?? '',
      flat: m.flat ?? '', pct: m.pct === undefined ? '' : Math.round(m.pct * 1000) / 10,
    })
  } else if (e.kind === 'abilityField') {
    const m = current<AbilityFieldModifier>()!
    Object.assign(abilityField, { target: m.target ?? '', action: m.action ?? '', field: m.field ?? '', op: m.op || 'multiply', value: m.value ?? '', stage: m.stage ?? '' })
  } else if (e.kind === 'abilityModifier') {
    const m = current<AbilityModifier>()!
    abilityMod.target = m.target
    for (const k of ABILITY_MOD_MULT_KEYS) abilityMod[k] = m[k] ?? ''
  } else if (e.kind === 'grantAbility') {
    grant.id = current<string>() ?? ''
  } else if (e.kind === 'perkModifier') {
    const m = current<PerkModifier>()!
    perkMod.target = m.target
    perkMod.ops = (m.ops ?? []).map((o) => ({ targetKey: o.targetKey, op: o.op, sourceKey: o.sourceKey }))
  } else if (e.kind === 'aura') {
    auraRow.value = rowFromAura(current<PerkAura>()!)
  } else if (e.kind === 'effect') {
    const m = current<PerkEffectShape>()!
    Object.assign(effect, { name: m.name ?? '', target: m.target ?? '', sizeScale: m.sizeScale ?? '', durationSeconds: m.durationSeconds ?? '', variant: m.variant ?? '' })
  }
}, { immediate: true })

function commitUnitStat() {
  const value = typeof unitStat.value === 'number' && !Number.isNaN(unitStat.value) ? unitStat.value : 0
  const next: PerkStatModifier = { stat: unitStat.stat, op: unitStat.op, value, ...(unitStat.stage !== 'base' ? { stage: unitStat.stage } : {}) }
  builder.updateSelected(next)
}

const knownStat = computed(() => builder.abilityStatDefs.value.some((d) => d.id === abilityStat.stat))
const shapeStats = computed(() => builder.abilityStatDefs.value.filter((d) => !d.inflicted))
const targetStats = computed(() => builder.abilityStatDefs.value.filter((d) => d.inflicted))
const allowsPct = computed(() => {
  const def = builder.abilityStatDefs.value.find((d) => d.id === abilityStat.stat)
  return !def?.flatOnly
})
function commitAbilityStat() {
  const next: PerkAbilityStat = { stat: abilityStat.stat.trim() }
  const ability = abilityStat.ability.trim()
  if (ability) next.ability = ability
  if (typeof abilityStat.flat === 'number' && !Number.isNaN(abilityStat.flat) && abilityStat.flat !== 0) next.flat = abilityStat.flat
  if (allowsPct.value && typeof abilityStat.pct === 'number' && !Number.isNaN(abilityStat.pct) && abilityStat.pct !== 0) next.pct = abilityStat.pct / 100
  builder.updateSelected(next)
}

const actions = computed(() => actionsForTarget(builder.abilityDefsById.value, abilityField.target))
const fields = computed(() => fieldsForAction(builder.abilityDefsById.value, abilityField.target, abilityField.action))
function commitAbilityField() {
  const value = typeof abilityField.value === 'number' && !Number.isNaN(abilityField.value) ? abilityField.value : 0
  const next: AbilityFieldModifier = { target: abilityField.target.trim(), action: abilityField.action.trim(), field: abilityField.field.trim(), value }
  if (abilityField.op && abilityField.op !== 'multiply') next.op = abilityField.op
  if (abilityField.stage) next.stage = abilityField.stage
  builder.updateSelected(next)
}

function commitAbilityMod() {
  const next: AbilityModifier = { target: abilityMod.target.trim() }
  for (const k of ABILITY_MOD_MULT_KEYS) {
    const v = abilityMod[k]
    if (typeof v === 'number' && !Number.isNaN(v)) next[k] = v
  }
  builder.updateSelected(next)
}

function commitGrant() { builder.updateSelected(grant.id.trim()) }

function addPerkModOp() { perkMod.ops.push({ targetKey: '', op: 'mult', sourceKey: '' }); commitPerkMod() }
function removePerkModOp(i: number) { perkMod.ops.splice(i, 1); commitPerkMod() }
// commitPerkMod writes VERBATIM — no trim, no filter. Filtering here caused a
// well-known trap: selectedEntry/entry mint a fresh object on every commit
// (buildModifierList), so watch(entry) immediately reseeds perkMod.ops from
// the just-committed (filtered) value. A blank new op would vanish the instant
// it was added, and typing targetKey before sourceKey would delete the row.
// Cleaning (dropping incomplete ops, trimming) happens once, at save() —
// mirrors the hub's established pattern for blank-name effect / blank grants.
function commitPerkMod() {
  const ops: PerkConfigOp[] = perkMod.ops.map((o) => ({ targetKey: o.targetKey, op: o.op, sourceKey: o.sourceKey }))
  const next: PerkModifier = { target: perkMod.target, ops }
  builder.updateSelected(next)
}

function onAuraRow(row: AuraRow) {
  auraRow.value = row
  builder.updateSelected(auraFromRow(row))
}

function commitEffect() {
  const next: PerkEffectShape = { name: effect.name }
  if (effect.target) next.target = effect.target
  if (typeof effect.sizeScale === 'number' && !Number.isNaN(effect.sizeScale)) next.sizeScale = effect.sizeScale
  if (typeof effect.durationSeconds === 'number' && !Number.isNaN(effect.durationSeconds)) next.durationSeconds = effect.durationSeconds
  if (effect.variant) next.variant = effect.variant
  builder.updateSelected(next)
}

// RiderEditor is CONTROLLED: it reads `riderModel` (re-derived from `current`
// on every render, so it always reflects the currently-selected entry — no
// local draft/watch(entry) case needed here, unlike the reactive() draft
// objects above) and emits the full edited AbilityRider straight back through
// updateSelected, mirroring onAuraRow's write-through pattern.
const riderModel = computed<AbilityRider>(() => current<AbilityRider>() ?? { target: '', trigger: '', actions: [] })
function onRider(r: AbilityRider) { builder.updateSelected(r) }
</script>

<style scoped>
.pi-empty { padding: 16px; color: var(--ed-text-dim); font-size: 0.82rem; }
.pi-note { margin: 0; font-size: 0.82rem; color: var(--ed-text); }
.pi-note--dim { color: var(--ed-text-dim); }
.pi-preview { margin: 0; font-size: 0.8rem; color: var(--mk-accent); font-weight: 700; }
.pi-op-row { display: grid; grid-template-columns: 1fr auto 1fr auto; gap: 6px; align-items: end; }
.pi-op-add { align-self: flex-start; padding: 4px 8px; font-size: 0.74rem; border: 1px solid var(--ed-line); border-radius: 4px; background: var(--ed-field); color: var(--ed-brass); }
.pi-op-del { padding: 2px 6px; border: 1px solid transparent; border-radius: 4px; background: none; color: var(--ed-text-dim); }
.pi-op-del:hover { color: var(--ed-danger); border-color: var(--ed-line); }
.pi-dropdown { display: flex; align-items: center; justify-content: space-between; gap: 8px; width: 100%; padding: 6px 8px; background: var(--ed-field); border: 1px solid var(--ed-line); border-radius: 4px; color: var(--ed-text); text-align: left; font: inherit; }
.pi-dropdown:hover { border-color: var(--ed-line-strong); }
.pi-dropdown__val { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pi-dropdown__val--empty { color: var(--ed-text-dim); }
.pi-dropdown__caret { flex: 0 0 auto; color: var(--ed-text-dim); font-size: 0.7rem; }
</style>
