<template>
  <div class="perk-editor">
    <aside class="perk-editor__list">
      <button type="button" class="perk-editor__new" :disabled="busy" @click="newPerk">+ New Perk</button>
      <p v-if="loadError" class="perk-editor__error">{{ loadError }}</p>
      <div v-for="group in groupedPerks" :key="group.unit" class="perk-editor__group">
        <button
          type="button"
          class="perk-editor__group-unit"
          :aria-expanded="expanded.has(group.unit)"
          @click="toggle(group.unit)"
        >
          <span class="perk-editor__chevron" aria-hidden="true">{{ expanded.has(group.unit) ? '▾' : '▸' }}</span>
          {{ unitLabel(group.unit) }}
        </button>
        <template v-if="expanded.has(group.unit)">
          <div v-for="pg in group.paths" :key="pg.path" class="perk-editor__group-path">
            <button
              v-if="pg.path"
              type="button"
              class="perk-editor__group-path-label"
              :aria-expanded="expanded.has(group.unit + '/' + pg.path)"
              @click="toggle(group.unit + '/' + pg.path)"
            >
              <span class="perk-editor__chevron" aria-hidden="true">{{ expanded.has(group.unit + '/' + pg.path) ? '▾' : '▸' }}</span>
              {{ pg.path }}
            </button>
            <ul v-if="!pg.path || expanded.has(group.unit + '/' + pg.path)">
              <li v-for="p in pg.perks" :key="p.id">
                <button
                  type="button"
                  data-test="perk-row"
                  :class="{ 'is-selected': p.id === selectedId }"
                  @click="selectPerk(p)"
                >
                  {{ p.id }} <span v-if="p.displayName">— {{ p.displayName }}</span>
                  <span v-if="!p.wired" class="perk-editor__badge perk-editor__badge--inert">inert</span>
                </button>
              </li>
            </ul>
          </div>
        </template>
      </div>
    </aside>

    <section class="perk-editor__form">
      <!-- Identity -->
      <section class="perk-editor__section">
        <h3 class="perk-editor__section-title">Identity</h3>
        <label>Id <input v-model="form.id" :disabled="selectedId !== null" /></label>
        <label>Display Name <input v-model="form.displayName" /></label>
        <label>Description <textarea v-model="form.description" rows="2" /></label>
        <label>Icon <input v-model="form.icon" /></label>
        <label class="perk-editor__checkbox-label">
          <input type="checkbox" :checked="!!form.wired" disabled />
          Wired <span class="perk-editor__hint">(server-derived, read-only)</span>
        </label>
      </section>

      <!-- Eligibility -->
      <section class="perk-editor__section">
        <h3 class="perk-editor__section-title">Eligibility</h3>
        <label>
          Association <span class="perk-editor__hint">(catalog folder)</span>
          <select v-if="selectedId === null" v-model="associationSelection" data-test="association-select">
            <option value="">Generic</option>
            <optgroup v-for="[unit, ps] in sortedPathsByUnit" :key="unit" :label="unitLabel(unit)">
              <option v-for="p in ps" :key="p" :value="p">{{ p }}</option>
            </optgroup>
          </select>
          <input v-else :value="form.path || 'generic'" disabled />
        </label>
        <label>Requires Perk <input v-model="form.requiresPerk" list="perk-editor-perk-ids" placeholder="(none)" /></label>
        <label>Requires Ability <input v-model="form.requiresAbility" list="perk-editor-ability-ids" placeholder="(none)" /></label>
      </section>

      <!-- Tooltip -->
      <section class="perk-editor__section">
        <h3 class="perk-editor__section-title">Tooltip</h3>
        <label>
          Generated <span class="perk-editor__hint">(from this perk's typed data — read-only)</span>
          <textarea :value="generatedDescriptionDisplay" rows="2" readonly class="perk-editor__generated" />
        </label>
        <p class="perk-editor__hint-line">
          Tooltip Template below, when non-empty, OVERRIDES the generated text above at runtime.
        </p>
        <label>Tooltip Template <textarea v-model="form.tooltipTemplate" rows="3" /></label>
      </section>

      <!-- Config -->
      <section class="perk-editor__section">
        <h3 class="perk-editor__section-title">Config</h3>
        <p v-if="configRows.length === 0" class="perk-editor__hint-line">No config values.</p>
        <div v-for="(row, idx) in configRows" :key="idx" class="perk-editor__map-row">
          <input v-model="row.key" placeholder="key" :aria-label="`Config ${idx + 1} key`" />
          <input v-model.number="row.value" type="number" :aria-label="`Config ${idx + 1} value`" />
          <button type="button" class="perk-editor__row-del" title="Remove" @click="removeConfigRow(idx)">✕</button>
        </div>
        <button type="button" class="perk-editor__row-add" @click="addConfigRow">+ Add Config Value</button>
      </section>

      <!-- Config By Rank -->
      <section class="perk-editor__section">
        <h3 class="perk-editor__section-title">Config By Rank</h3>
        <p class="perk-editor__hint-line">
          JSON object of rank → (key → number). Leave blank for none.
        </p>
        <textarea
          class="perk-editor__json"
          rows="5"
          :value="configByRankText"
          @input="onConfigByRankInput(($event.target as HTMLTextAreaElement).value)"
        />
        <p v-if="configByRankError" class="perk-editor__error">{{ configByRankError }}</p>
      </section>

      <!-- Effect -->
      <section class="perk-editor__section">
        <h3 class="perk-editor__section-title">Effect</h3>
        <label>Name <input v-model="effectDraft.name" /></label>
        <label>
          Target
          <select v-model="effectDraft.target">
            <option value="">(none)</option>
            <option value="self">self</option>
            <option value="enemies">enemies</option>
          </select>
        </label>
        <label>Size Scale <input type="number" v-model.number="effectDraft.sizeScale" /></label>
        <label>Duration Seconds <input type="number" v-model.number="effectDraft.durationSeconds" /></label>
        <label>Variant <input v-model="effectDraft.variant" /></label>
      </section>

      <!-- Grants Abilities -->
      <section class="perk-editor__section">
        <h3 class="perk-editor__section-title">Grants Abilities</h3>
        <label>
          Ability Ids (comma-separated)
          <input
            :value="(form.grantsAbilities ?? []).join(',')"
            @input="updateGrantsAbilities(($event.target as HTMLInputElement).value)"
          />
        </label>
      </section>

      <!-- Ability Modifiers -->
      <section class="perk-editor__section">
        <h3 class="perk-editor__section-title">Ability Modifiers</h3>
        <p class="perk-editor__hint-line">
          Scalar multipliers this perk applies to a named ability. Leave a mult field
          blank to leave that stat unmodified.
        </p>
        <p v-if="abilityModifierRows.length === 0" class="perk-editor__hint-line">No ability modifiers.</p>
        <div v-for="(row, idx) in abilityModifierRows" :key="idx" class="perk-editor__ability-mod-row">
          <label class="perk-editor__ability-mod-target">
            Target Ability
            <input
              v-model="row.target"
              list="perk-editor-ability-ids"
              placeholder="ability id"
              :aria-label="`Ability Modifier ${idx + 1} target`"
            />
          </label>
          <div class="perk-editor__ability-mod-mults">
            <label>Dmg× <input v-model.number="row.damageMult" type="number" step="0.05" placeholder="—" :aria-label="`Ability Modifier ${idx + 1} damage mult`" /></label>
            <label>Heal× <input v-model.number="row.healMult" type="number" step="0.05" placeholder="—" :aria-label="`Ability Modifier ${idx + 1} heal mult`" /></label>
            <label>Mana× <input v-model.number="row.manaCostMult" type="number" step="0.05" placeholder="—" :aria-label="`Ability Modifier ${idx + 1} mana cost mult`" /></label>
            <label>Range× <input v-model.number="row.rangeMult" type="number" step="0.05" placeholder="—" :aria-label="`Ability Modifier ${idx + 1} range mult`" /></label>
            <label>CD× <input v-model.number="row.cooldownMult" type="number" step="0.05" placeholder="—" :aria-label="`Ability Modifier ${idx + 1} cooldown mult`" /></label>
          </div>
          <button type="button" class="perk-editor__row-del" title="Remove" @click="removeAbilityModifierRow(idx)">✕</button>
        </div>
        <button type="button" class="perk-editor__row-add" @click="addAbilityModifierRow">+ Add Ability Modifier</button>
      </section>

      <!-- Unit Stat Modifiers -->
      <section class="perk-editor__section">
        <h3 class="perk-editor__section-title">Unit Stat Modifiers</h3>
        <p class="perk-editor__hint-line">
          Typed bonuses this perk applies to the unit's stats, applied in three stages:
          <strong>Intrinsic</strong> scales the unit's own base stat first, before any outside bonus
          (so a ×1.15 here boosts only this unit's damage, not a zone aura's added damage);
          <strong>Base</strong> modifiers are then summed/multiplied together; <strong>Final</strong>
          modifiers apply last, on top of everything — e.g. a Final ×2 doubles whatever the stat
          ended up at.
        </p>
        <p v-if="statModifierRows.length === 0" class="perk-editor__hint-line">No stat modifiers.</p>
        <div v-for="(row, idx) in statModifierRows" :key="idx" class="perk-editor__stat-mod-row">
          <label>
            Stat
            <select v-model="row.stat" :aria-label="`Stat Modifier ${idx + 1} stat`">
              <option v-for="d in selfStatDefsList" :key="d.id" :value="d.id">{{ d.label }}</option>
            </select>
          </label>
          <label>
            Op
            <select v-model="row.op" :aria-label="`Stat Modifier ${idx + 1} op`">
              <option value="add">Add</option>
              <option value="multiply">Multiply</option>
            </select>
          </label>
          <label>
            Value
            <input v-model.number="row.value" type="number" step="0.05" :aria-label="`Stat Modifier ${idx + 1} value`" />
          </label>
          <label>
            Stage
            <select v-model="row.stage" :aria-label="`Stat Modifier ${idx + 1} stage`">
              <option value="intrinsic">Intrinsic (scales this unit's own base only)</option>
              <option value="base">Base</option>
              <option value="final">Final (applied after everything else)</option>
            </select>
          </label>
          <button type="button" class="perk-editor__row-del" title="Remove" @click="removeStatModifierRow(idx)">✕</button>
        </div>
        <button type="button" class="perk-editor__row-add" @click="addStatModifierRow">+ Add Stat Modifier</button>
      </section>

      <!-- Ability Riders -->
      <section class="perk-editor__section">
        <h3 class="perk-editor__section-title">Ability Riders</h3>
        <p class="perk-editor__hint-line">
          Extra actions this perk grafts onto a target ability's trigger — e.g. Shared Suffering
          grafts a damage tick onto Siphon Life's beam-tick trigger. Authored with the same
          widgets as the Ability editor.
        </p>
        <p v-if="abilityRiders.length === 0" class="perk-editor__hint-line">No ability riders.</p>
        <div v-for="(rider, idx) in abilityRiders" :key="idx" class="perk-editor__rider-row">
          <RiderEditor
            :model-value="rider"
            :ability-ids="abilityIds"
            :schema="riderSchema"
            :catalogs="riderCatalogs"
            @update:model-value="(v) => updateRider(idx, v)"
          />
          <button type="button" class="perk-editor__row-del" title="Remove Rider" @click="removeRider(idx)">✕</button>
        </div>
        <button type="button" class="perk-editor__row-add" @click="addRider">+ Add Rider</button>
      </section>

      <!-- Auras -->
      <section class="perk-editor__section">
        <h3 class="perk-editor__section-title">Auras</h3>
        <p class="perk-editor__hint-line">
          An aura CONTINUOUSLY affects every nearby unit within its radius while this perk's
          owner is alive and in range — unlike Unit Stat Modifiers above, which only ever affect
          this unit itself. Overlapping sources of the same aura don't simply add up: the
          strongest single source wins, and each additional covering source on top of that adds
          "Per Additional Source" (e.g. Zealous March: +30% move speed from one Cleric nearby,
          +35% with two, +40% with three).
        </p>
        <p v-if="auraRows.length === 0" class="perk-editor__hint-line">No auras.</p>
        <div v-for="(row, idx) in auraRows" :key="idx" class="perk-editor__aura-row">
          <AuraEditor
            :model-value="row"
            :stat-defs="auraStatDefsList"
            @update:model-value="(v) => updateAuraRow(idx, v)"
          />
          <button type="button" class="perk-editor__row-del" title="Remove Aura" @click="removeAuraRow(idx)">✕</button>
        </div>
        <button type="button" class="perk-editor__row-add" @click="addAuraRow">+ Add Aura</button>
      </section>

      <p v-if="saveError" class="perk-editor__error">{{ saveError }}</p>
      <p v-if="statusMessage" class="perk-editor__status">{{ statusMessage }}</p>
      <div class="perk-editor__actions">
        <button type="button" :disabled="busy || !form.id" @click="save">Save</button>
        <button type="button" :disabled="busy || selectedId === null" @click="removePerk">Delete / Reset</button>
      </div>
    </section>

    <datalist id="perk-editor-perk-ids">
      <option v-for="p in perks" :key="p.id" :value="p.id" />
    </datalist>
    <datalist id="perk-editor-ability-ids">
      <option v-for="id in abilityIds" :key="id" :value="id" />
    </datalist>
  </div>
</template>

<script setup lang="ts">
import { confirmDelete } from '@/components/editor/confirmDelete'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import {
  createBlankForm, formFromDef, saveRequestFromForm,
  type AbilityModifier, type AbilityRider, type AuthoredPerkDef, type PerkAura, type PerkEditorForm, type PerkStatModifier,
} from '@/game/perks/perkEditorForm'
import { allStatDefs, selfStatDefs } from '@/game/stats/statRegistry'
import {
  fetchAuthoredPerkDefs, saveEditorPerk, deleteEditorPerk, EditorValidationError,
} from '@/game/perks/perkEditorApi'
import {
  fetchAbilityCategories, fetchActionSchema, fetchAuthoredAbilityDefs, fetchAutoCastSelectors,
  fetchDamageTypes, fetchEffectIds, fetchProjectileIds,
} from '@/game/abilities/abilityEditorApi'
import type { ActionSchemaBundle } from '@/game/abilities/program/programSchema'
import type { AbilityBuilderCatalogs } from '@/components/ability-builder/useAbilityBuilder'
import { fetchAuthoredUnitDefs } from '@/game/units/unitEditorApi'
import { fetchUnitDefs } from '@/game/maps/catalog'
import RiderEditor from '@/components/perk-editor/RiderEditor.vue'
import AuraEditor, { type AuraRow } from '@/components/perk-editor/AuraEditor.vue'

const perks = ref<AuthoredPerkDef[]>([])
const form = ref<PerkEditorForm>(createBlankForm())
const selectedId = ref<string | null>(null)
const saveError = ref('')
const loadError = ref('')
const statusMessage = ref('')
const busy = ref(false)

// Config: add/remove key→number rows, kept in sync with form.value.config via
// a deep watch — mirrors UnitTypeEditorPanel's resourceCostRows/mapFromRows idiom.
interface MapRow { key: string; value: number }
const configRows = ref<MapRow[]>([])

function rowsFromMap(map?: Record<string, number>): MapRow[] {
  return Object.entries(map ?? {}).map(([key, value]) => ({ key, value }))
}
function mapFromRows(rows: MapRow[]): Record<string, number> {
  const out: Record<string, number> = {}
  for (const row of rows) if (row.key) out[row.key] = row.value
  return out
}
function addConfigRow() { configRows.value.push({ key: '', value: 0 }) }
function removeConfigRow(idx: number) { configRows.value.splice(idx, 1) }
watch(configRows, (rows) => { form.value.config = mapFromRows(rows) }, { deep: true })

// configByRank: a pragmatic JSON-textarea control (rank → key → number is a
// nested shape not worth a bespoke rows-of-rows UI for). Invalid JSON is held
// in the textarea and flagged inline without touching form.value.configByRank,
// so a typo never silently drops previously-valid data.
const configByRankText = ref('')
const configByRankError = ref('')

function syncConfigByRankText(def?: Record<string, Record<string, number>>) {
  configByRankText.value = def && Object.keys(def).length ? JSON.stringify(def, null, 2) : ''
  configByRankError.value = ''
}

function onConfigByRankInput(value: string) {
  configByRankText.value = value
  const trimmed = value.trim()
  if (!trimmed) {
    form.value.configByRank = undefined
    configByRankError.value = ''
    return
  }
  try {
    form.value.configByRank = JSON.parse(trimmed)
    configByRankError.value = ''
  } catch {
    configByRankError.value = 'Invalid JSON — not saved until fixed.'
  }
}

// Effect: only ever sent as a full object when a name is set (blank name =
// no effect), per saveRequestFromForm's undefined-field-drop contract.
interface EffectDraft { name: string; target: string; sizeScale?: number; durationSeconds?: number; variant: string }
const effectDraft = reactive<EffectDraft>({ name: '', target: '', sizeScale: undefined, durationSeconds: undefined, variant: '' })

function syncEffectDraft(effect?: AuthoredPerkDef['effect']) {
  effectDraft.name = effect?.name ?? ''
  effectDraft.target = effect?.target ?? ''
  effectDraft.sizeScale = effect?.sizeScale
  effectDraft.durationSeconds = effect?.durationSeconds
  effectDraft.variant = effect?.variant ?? ''
}

watch(effectDraft, (draft) => {
  if (!draft.name.trim()) {
    form.value.effect = undefined
    return
  }
  form.value.effect = {
    name: draft.name,
    ...(draft.target ? { target: draft.target } : {}),
    ...(typeof draft.sizeScale === 'number' && !Number.isNaN(draft.sizeScale) ? { sizeScale: draft.sizeScale } : {}),
    ...(typeof draft.durationSeconds === 'number' && !Number.isNaN(draft.durationSeconds) ? { durationSeconds: draft.durationSeconds } : {}),
    ...(draft.variant ? { variant: draft.variant } : {}),
  }
}, { deep: true })

function updateGrantsAbilities(raw: string) {
  const list = raw.split(',').map((s) => s.trim()).filter(Boolean)
  form.value.grantsAbilities = list.length ? list : undefined
}

// Ability Modifiers: same rows-kept-in-sync-via-deep-watch idiom as
// configRows above. Mult fields are `number | ''` (not `number | undefined`)
// because v-model.number leaves a cleared input as '' rather than undefined;
// abilityModifiersFromRows is the single place that strips blanks back out
// so a blank field never round-trips as an explicit 0 (0 would read as an
// intentional "always zero this stat" — the correct way to omit is absence).
interface AbilityModifierRow {
  target: string
  damageMult?: number | ''
  healMult?: number | ''
  manaCostMult?: number | ''
  rangeMult?: number | ''
  cooldownMult?: number | ''
}
const abilityModifierRows = ref<AbilityModifierRow[]>([])
const abilityIds = ref<string[]>([])

// Every mult the Go AbilityModifier struct carries must be listed here — a key
// missing from this list is silently STRIPPED on save (abilityModifiersFromRows
// rebuilds each entry from these keys only), which would quietly delete a
// shipped perk's behavior.
const ABILITY_MOD_MULT_KEYS = [
  'damageMult', 'healMult', 'manaCostMult', 'rangeMult', 'cooldownMult',
] as const

function rowsFromAbilityModifiers(mods?: AbilityModifier[]): AbilityModifierRow[] {
  return (mods ?? []).map((m) => {
    const row: AbilityModifierRow = { target: m.target }
    for (const key of ABILITY_MOD_MULT_KEYS) row[key] = m[key] ?? ''
    return row
  })
}

function abilityModifiersFromRows(rows: AbilityModifierRow[]): AbilityModifier[] {
  const out: AbilityModifier[] = []
  for (const row of rows) {
    const target = row.target.trim()
    if (!target) continue
    const mod: AbilityModifier = { target }
    let hasAny = false
    for (const key of ABILITY_MOD_MULT_KEYS) {
      const v = row[key]
      if (typeof v === 'number' && !Number.isNaN(v)) { mod[key] = v; hasAny = true }
    }
    if (!hasAny) continue // fully-blank row (target-only) contributes nothing — drop it
    out.push(mod)
  }
  return out
}

function addAbilityModifierRow() { abilityModifierRows.value.push({ target: '' }) }
function removeAbilityModifierRow(idx: number) { abilityModifierRows.value.splice(idx, 1) }
watch(abilityModifierRows, (rows) => {
  const mods = abilityModifiersFromRows(rows)
  form.value.abilityModifiers = mods.length ? mods : undefined
}, { deep: true })

// Ability Riders: unlike configRows/abilityModifierRows, AbilityRider is
// ALREADY the wire shape (no row-transform needed — target/trigger/actions
// map 1:1), so this is a thin computed over form.value.abilityRiders rather
// than a separate rows-array synced by a deep watch. Reads/resets
// automatically follow form.value being replaced wholesale on
// selectPerk/newPerk, same as every other directly-modeled field.
const abilityRiders = computed<AbilityRider[]>({
  get: () => form.value.abilityRiders ?? [],
  set: (list) => { form.value.abilityRiders = list.length ? list : undefined },
})
function addRider() { abilityRiders.value = [...abilityRiders.value, { target: '', trigger: '', actions: [] }] }
function removeRider(idx: number) {
  const next = abilityRiders.value.slice()
  next.splice(idx, 1)
  abilityRiders.value = next
}
function updateRider(idx: number, rider: AbilityRider) {
  const next = abilityRiders.value.slice()
  next[idx] = rider
  abilityRiders.value = next
}

// Unit Stat Modifiers: same rows-kept-in-sync-via-deep-watch idiom as
// abilityModifierRows above. `stat` is picked from STAT_DEFS (the client
// mirror of the Go statRegistry) via a <select> — no freeform stat entry.
// `value` is `number | ''` (not `number`) for the same v-model.number reason
// as the ability-modifier mult fields: a cleared input reads as '', and
// statModifiersFromRows is the single place that coerces that back to a
// real number before it reaches form.value.statModifiers.
interface StatModifierRow {
  stat: string
  op: 'add' | 'multiply'
  value: number | ''
  stage: 'intrinsic' | 'base' | 'final'
}
const statModifierRows = ref<StatModifierRow[]>([])
// Unit Stat Modifiers (self, top-level) may only offer stats with a
// top-level fold site — aura-only stats (armorPercent,
// projectileDamageReduction) are filtered out here because the server
// rejects them on a top-level entry (validatePerkDef, perk_defs.go). The
// Auras section below uses allStatDefs() instead, since aura-only stats are
// valid there.
const selfStatDefsList = selfStatDefs()
const auraStatDefsList = allStatDefs()

function rowsFromStatModifiers(mods?: PerkStatModifier[]): StatModifierRow[] {
  return (mods ?? []).map((m) => ({
    stat: m.stat,
    op: m.op,
    value: m.value,
    stage: m.stage ?? 'base',
  }))
}

function statModifiersFromRows(rows: StatModifierRow[]): PerkStatModifier[] {
  const out: PerkStatModifier[] = []
  for (const row of rows) {
    if (!row.stat) continue
    const value = typeof row.value === 'number' && !Number.isNaN(row.value) ? row.value : 0
    // Omit stage when "base" (the server default) so an untouched row
    // round-trips byte-for-byte with a source def that never wrote it,
    // matching the omit-when-default convention used by effect/ability-mod
    // fields elsewhere in this form.
    out.push({ stat: row.stat, op: row.op, value, ...(row.stage !== 'base' ? { stage: row.stage } : {}) })
  }
  return out
}

function addStatModifierRow() {
  statModifierRows.value.push({ stat: selfStatDefsList[0]?.id ?? '', op: 'add', value: 0, stage: 'base' })
}
function removeStatModifierRow(idx: number) { statModifierRows.value.splice(idx, 1) }
watch(statModifierRows, (rows) => {
  const mods = statModifiersFromRows(rows)
  form.value.statModifiers = mods.length ? mods : undefined
}, { deep: true })

// Auras: same rows-kept-in-sync-via-deep-watch idiom as statModifierRows,
// one level deeper (each row owns its own nested statRows list, edited via
// AuraEditor.vue — see that file's module doc for why it's extracted).
// `op`/`stage` are never read from or written to the row shape — the server
// only accepts `op: "add"` with `stage` omitted for aura stat modifiers, so
// aurasFromRows always emits exactly that, and rowsFromAuras never surfaces
// the source op/stage back into the UI. `stacking` has exactly one
// server-supported value ("max"): rather than render a one-option select
// (pure noise) this form omits the control entirely and always emits
// "max" — matching every shipped aura in the catalog (e.g. zealous_march),
// which already writes "stacking": "max" explicitly.
const auraRows = ref<AuraRow[]>([])

function rowsFromAuras(auras?: PerkAura[]): AuraRow[] {
  return (auras ?? []).map((a) => ({
    radius: a.radius,
    targets: a.targets,
    includeSelf: a.includeSelf ?? false,
    perAdditionalSource: a.perAdditionalSource ?? '',
    statRows: (a.statModifiers ?? []).map((m) => ({ stat: m.stat, value: m.value })),
    ringColor: a.ringColor ?? '',
  }))
}

function aurasFromRows(rows: AuraRow[]): PerkAura[] {
  const out: PerkAura[] = []
  for (const row of rows) {
    const radius = typeof row.radius === 'number' && !Number.isNaN(row.radius) ? row.radius : 0
    if (radius <= 0) continue // server rejects radius <= 0 — drop rather than round-trip a def it would refuse

    const statModifiers: PerkStatModifier[] = []
    for (const sr of row.statRows) {
      if (!sr.stat) continue
      const value = typeof sr.value === 'number' && !Number.isNaN(sr.value) ? sr.value : 0
      statModifiers.push({ stat: sr.stat, op: 'add', value })
    }
    if (statModifiers.length === 0) continue // an aura with no contributions does nothing — drop it

    const aura: PerkAura = { radius, targets: row.targets, stacking: 'max', statModifiers }
    if (row.includeSelf) aura.includeSelf = true
    const perAdditionalSource = row.perAdditionalSource
    if (typeof perAdditionalSource === 'number' && !Number.isNaN(perAdditionalSource)) {
      aura.perAdditionalSource = perAdditionalSource
    }
    // Omit ringColor entirely when unset ('') — round-trips byte-for-byte
    // with a source def that never authored it, same omit-when-default
    // convention as stage/perAdditionalSource above.
    if (row.ringColor) aura.ringColor = row.ringColor
    out.push(aura)
  }
  return out
}

function addAuraRow() {
  auraRows.value.push({ radius: 128, targets: 'allies', includeSelf: false, perAdditionalSource: '', statRows: [], ringColor: '' })
}
function removeAuraRow(idx: number) { auraRows.value.splice(idx, 1) }
function updateAuraRow(idx: number, row: AuraRow) { auraRows.value[idx] = row }
watch(auraRows, (rows) => {
  const auras = aurasFromRows(rows)
  form.value.auras = auras.length ? auras : undefined
}, { deep: true })

// Ability builder schema + catalogs: fetched ONCE here and shared across
// every RiderEditor instance (via props) rather than each rider row
// re-fetching the same action schema / display catalogs — mirrors
// useAbilityBuilder.load()'s own parallel fetch, minus the ability list and
// undo/redo/validation machinery a rider fragment doesn't need.
const riderSchema = ref<ActionSchemaBundle | null>(null)
const riderCatalogs = ref<AbilityBuilderCatalogs>({
  effects: [], projectiles: [], damageTypes: [], categories: [], autoCastSelectors: [], unitTypes: [],
})

async function reload() {
  try {
    perks.value = await fetchAuthoredPerkDefs()
    loadError.value = ''
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e)
  }
}

// Sidebar grouping: Unit -> Path -> perks, with a Generic bucket for
// path-agnostic perks and for perks whose path doesn't map to any known unit
// (topology fetch failed, or the path was orphaned). Topology comes from
// fetchUnitDefs().pathsByUnit and is loaded non-fatally alongside the perk list.
const pathsByUnit = ref<Record<string, string[]>>({})

const pathToUnit = computed(() => {
  const m = new Map<string, string>()
  for (const [u, ps] of Object.entries(pathsByUnit.value)) for (const p of ps) m.set(p, u)
  return m
})

interface PerkGroup { unit: string; paths: Array<{ path: string; perks: AuthoredPerkDef[] }> }

const groupedPerks = computed<PerkGroup[]>(() => {
  const byUnitPath = new Map<string, Map<string, AuthoredPerkDef[]>>()
  const generic: AuthoredPerkDef[] = []
  for (const p of perks.value) {
    const path = p.path ?? ''
    const unit = path ? pathToUnit.value.get(path) : undefined
    if (!path || !unit) { generic.push(p); continue }
    if (!byUnitPath.has(unit)) byUnitPath.set(unit, new Map())
    const paths = byUnitPath.get(unit)!
    if (!paths.has(path)) paths.set(path, [])
    paths.get(path)!.push(p)
  }
  const groups: PerkGroup[] = [...byUnitPath.entries()]
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([unit, paths]) => ({
      unit,
      paths: [...paths.entries()]
        .sort((a, b) => a[0].localeCompare(b[0]))
        .map(([path, ps]) => ({ path, perks: [...ps].sort((x, y) => x.id.localeCompare(y.id)) })),
    }))
  if (generic.length) {
    groups.push({ unit: 'Generic', paths: [{ path: '', perks: [...generic].sort((x, y) => x.id.localeCompare(y.id)) }] })
  }
  return groups
})

// Which groups are OPEN. Empty by default → everything starts collapsed; the
// user expands the unit(s)/path(s) they want. Keys: `<unit>` and `<unit>/<path>`.
const expanded = ref(new Set<string>())
function toggle(key: string) {
  const s = new Set(expanded.value)
  s.has(key) ? s.delete(key) : s.add(key)
  expanded.value = s
}

function unitLabel(unit: string): string {
  return unit && unit !== 'Generic' ? unit[0].toUpperCase() + unit.slice(1) : unit
}

// generatedDescription is server-derived (see perkEditorForm.ts) — shown
// read-only so a designer can see what their typed statModifiers/
// abilityModifiers/riders produce, without it ever being sent back on save.
// Empty means the perk has no typed data to generate from (e.g. a purely
// tooltipTemplate-authored perk, or one whose effect is config/effect-only).
const generatedDescriptionDisplay = computed(() =>
  form.value.generatedDescription?.trim() || '(no typed data to generate from)')

// New perk: user picks the target association so SavePerkDef writes it into
// catalog/perks/<path|generic>/. '' = generic (form.path undefined → omitted →
// server defaults to generic). Existing perks keep association read-only.
const associationSelection = computed<string>({
  get: () => form.value.path ?? '',
  set: (v) => { form.value.path = v || undefined },
})
const sortedPathsByUnit = computed<Array<[string, string[]]>>(() =>
  Object.entries(pathsByUnit.value)
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([u, ps]) => [u, [...ps].sort((x, y) => x.localeCompare(y))]),
)

function selectPerk(def: AuthoredPerkDef) {
  form.value = formFromDef(def)
  selectedId.value = def.id
  configRows.value = rowsFromMap(def.config)
  syncConfigByRankText(def.configByRank)
  syncEffectDraft(def.effect)
  abilityModifierRows.value = rowsFromAbilityModifiers(def.abilityModifiers)
  statModifierRows.value = rowsFromStatModifiers(def.statModifiers)
  auraRows.value = rowsFromAuras(def.auras)
  saveError.value = ''
  statusMessage.value = ''
}

function newPerk() {
  form.value = createBlankForm()
  selectedId.value = null
  configRows.value = []
  syncConfigByRankText(undefined)
  syncEffectDraft(undefined)
  abilityModifierRows.value = []
  statModifierRows.value = []
  auraRows.value = []
  saveError.value = ''
  statusMessage.value = ''
}

async function save() {
  saveError.value = ''
  statusMessage.value = ''
  busy.value = true
  try {
    await saveEditorPerk(saveRequestFromForm(form.value))
    await reload()
    selectedId.value = form.value.id
    statusMessage.value = 'Saved.'
  } catch (e) {
    saveError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function removePerk() {
  if (!selectedId.value) return
  if (!(await confirmDelete('perk', selectedId.value, undefined, 'If it ships with the game it will reset to its built-in default; a custom one is removed.'))) return
  saveError.value = ''
  statusMessage.value = ''
  busy.value = true
  try {
    const status = await deleteEditorPerk(selectedId.value)
    await reload()
    newPerk()
    statusMessage.value = status === 'deleted' ? 'Deleted.' : 'Reset to default.'
  } catch (e) {
    saveError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

onMounted(async () => {
  await reload()
  try {
    pathsByUnit.value = (await fetchUnitDefs()).pathsByUnit
  } catch {
    pathsByUnit.value = {} // non-fatal: fall back to Generic-only grouping
  }
  try {
    abilityIds.value = (await fetchAuthoredAbilityDefs()).map((a) => a.id)
  } catch {
    abilityIds.value = [] // non-fatal: target-ability input just loses autocomplete
  }
  try {
    const [schema, effects, projectiles, damageTypes, categories, autoCastSelectors, units] = await Promise.all([
      fetchActionSchema(),
      fetchEffectIds(),
      fetchProjectileIds(),
      fetchDamageTypes(),
      fetchAbilityCategories(),
      fetchAutoCastSelectors(),
      fetchAuthoredUnitDefs(),
    ])
    riderSchema.value = schema
    riderCatalogs.value = {
      effects, projectiles, damageTypes, categories, autoCastSelectors,
      unitTypes: units.map((u) => u.type),
    }
  } catch {
    // non-fatal: Ability Riders' trigger/action pickers fall back to their
    // curated lists and SchemaField's enum/asset controls fall back to plain
    // text inputs — still fully functional, just without the nice pickers.
    riderSchema.value = null
  }
})
</script>

<style scoped>
.perk-editor {
  display: flex;
  width: 100%;
  height: 100%;
  min-height: 0;
  min-width: 0;
  gap: 12px;
  padding: 16px;
  box-sizing: border-box;
}

.perk-editor__list {
  flex: 0 0 220px;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 8px;
  background: rgba(3, 8, 14, 0.86);
  border: 1px solid rgba(148, 163, 184, 0.22);
  border-radius: 16px;
  padding: 12px;
}

.perk-editor__list ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.perk-editor__list button {
  width: 100%;
  border: 1px solid transparent;
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.6);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
  text-align: left;
}

.perk-editor__list button.is-selected {
  border-color: rgba(215, 187, 132, 0.6);
  background: rgba(30, 41, 59, 0.9);
}

.perk-editor__new {
  font-weight: 700;
}

.perk-editor__group {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.perk-editor__group-unit {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  margin: 8px 0 0;
  padding: 3px 4px;
  background: none;
  border: none;
  border-radius: 4px;
  text-align: left;
  font-size: 0.82rem;
  font-weight: 700;
  color: #d7bb84;
}

.perk-editor__group-unit:hover {
  background: rgba(215, 187, 132, 0.12);
}

.perk-editor__group-path {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.perk-editor__group-path-label {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  margin: 2px 0 0 8px;
  padding: 2px 4px;
  background: none;
  border: none;
  border-radius: 4px;
  text-align: left;
  font-size: 0.72rem;
  font-weight: 600;
  color: rgba(226, 232, 240, 0.6);
}

.perk-editor__group-path-label:hover {
  background: rgba(226, 232, 240, 0.08);
}

.perk-editor__chevron {
  display: inline-block;
  width: 0.7em;
  flex-shrink: 0;
  font-size: 0.7em;
  opacity: 0.7;
}

.perk-editor__badge {
  margin-left: 6px;
  border-radius: 999px;
  padding: 1px 7px;
  font-size: 0.64rem;
  font-weight: 700;
  white-space: nowrap;
}

.perk-editor__badge--inert {
  background: rgba(248, 113, 113, 0.18);
  color: #fca5a5;
  border: 1px solid rgba(248, 113, 113, 0.55);
}

.perk-editor__form {
  flex: 1;
  min-width: 0;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 12px;
  background: rgba(3, 8, 14, 0.86);
  border: 1px solid rgba(148, 163, 184, 0.22);
  border-radius: 16px;
  padding: 12px;
}

.perk-editor__section {
  border: 1px solid rgba(148, 163, 184, 0.18);
  border-radius: 12px;
  background: rgba(8, 14, 24, 0.55);
  padding: 10px;
  display: grid;
  gap: 8px;
}

.perk-editor__section-title {
  margin: 0;
  font-size: 0.78rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #d7bb84;
}

.perk-editor__section label {
  display: grid;
  gap: 4px;
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.75rem;
}

.perk-editor__section input,
.perk-editor__section select,
.perk-editor__section textarea {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
  font-family: inherit;
}

.perk-editor__checkbox-label {
  flex-direction: row !important;
  align-items: center;
  gap: 6px !important;
}

.perk-editor__hint {
  font-weight: 400;
  opacity: 0.65;
}

/* Generated, read-only field: dimmer + italic, so it reads as output rather
   than an input someone forgot to enable. Mirrors .item-editor__generated. */
.perk-editor__generated {
  color: rgba(226, 232, 240, 0.55);
  font-style: italic;
  resize: none;
}

.perk-editor__hint-line {
  margin: 0;
  color: rgba(226, 232, 240, 0.55);
  font-size: 0.72rem;
  font-style: italic;
}

.perk-editor__map-row {
  display: flex;
  align-items: center;
  gap: 6px;
}

.perk-editor__map-row input {
  flex: 1 1 auto;
  min-width: 0;
}

.perk-editor__row-del {
  flex: 0 0 auto;
  border: 1px solid rgba(148, 163, 184, 0.25);
  border-radius: 6px;
  background: rgba(15, 23, 42, 0.6);
  color: #f8fafc;
  padding: 4px 8px;
  font-size: 0.72rem;
}

.perk-editor__rider-row {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}

.perk-editor__aura-row {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}

.perk-editor__ability-mod-row {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 10px;
  padding: 8px;
  border: 1px solid rgba(148, 163, 184, 0.16);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.35);
}

.perk-editor__ability-mod-target {
  flex: 1 1 160px;
  min-width: 140px;
}

.perk-editor__ability-mod-mults {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  flex: 2 1 260px;
}

.perk-editor__ability-mod-mults label {
  display: grid;
  gap: 4px;
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.72rem;
  width: 74px;
}

.perk-editor__stat-mod-row {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 10px;
  padding: 8px;
  border: 1px solid rgba(148, 163, 184, 0.16);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.35);
}

.perk-editor__stat-mod-row label {
  min-width: 120px;
}

.perk-editor__row-add {
  align-self: flex-start;
  border: 1px solid rgba(215, 187, 132, 0.5);
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.6);
  color: #d7bb84;
  padding: 6px 10px;
  font-size: 0.76rem;
  font-weight: 700;
}

.perk-editor__json {
  resize: vertical;
  font-family: monospace;
}

.perk-editor__actions {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: auto;
  padding-top: 8px;
}

.perk-editor__error {
  color: #fca5a5;
  font-size: 0.78rem;
}

.perk-editor__status {
  color: #86efac;
  font-size: 0.78rem;
}
</style>
