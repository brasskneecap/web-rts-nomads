// usePerkBuilder.ts — the single state object PerkBuilderPanel provides. Holds
// the loaded catalog, the open perk's form, the selected modifier, and every
// mutation. `form` stays an AuthoredPerkDef; modifier edits write back into its
// arrays in place, so untouched arrays round-trip byte-for-byte.
import { computed, ref, shallowRef } from 'vue'
import {
  createBlankForm, formFromDef, saveRequestFromForm,
  type AuthoredPerkDef, type PerkEditorForm,
} from '@/game/perks/perkEditorForm'
import {
  fetchAuthoredPerkDefs, saveEditorPerk, deleteEditorPerk, EditorValidationError,
} from '@/game/perks/perkEditorApi'
import {
  fetchAuthoredAbilityDefs,
  fetchAbilityCategories, fetchActionSchema, fetchAutoCastSelectors,
  fetchDamageTypes, fetchEffectIds, fetchProjectileIds,
} from '@/game/abilities/abilityEditorApi'
import type { ActionSchemaBundle } from '@/game/abilities/program/programSchema'
import type { AuthoredAbilityDef } from '@/game/abilities/abilityEditorForm'
import type { AbilityBuilderCatalogs } from '@/components/ability-builder/useAbilityBuilder'
import { listObjectSpriteKeys } from '@/game/rendering/objectSprites'
import { fetchAuthoredUnitDefs } from '@/game/units/unitEditorApi'
import { fetchUnitDefs } from '@/game/maps/catalog'
import { allStatDefs, selfStatDefs } from '@/game/stats/statRegistry'
import { buildModifierList, KIND_META, type ModifierKind, type ModifierLabels } from './perkModifierModel'
import type { AbilityDefLite } from './abilityFieldOptions'

export interface AbilityStatDef { id: string; label: string; flatOnly?: boolean; inflicted?: boolean }
/** Address of the selected modifier: which array + which index. */
export interface Selection { arrayKey: keyof AuthoredPerkDef; index: number }

export function usePerkBuilder() {
  const perks = ref<AuthoredPerkDef[]>([])
  const form = ref<PerkEditorForm>(createBlankForm())
  const selectedId = ref<string | null>(null)
  const selected = shallowRef<Selection | null>(null)
  const busy = ref(false)
  const loadError = ref('')
  const saveError = ref('')
  const statusMessage = ref('')

  // Catalogs
  const abilityIds = ref<string[]>([])
  const abilityDefsById = ref<Record<string, AbilityDefLite>>({})
  const abilityDefs = ref<AuthoredAbilityDef[]>([])
  const abilityStatDefs = ref<AbilityStatDef[]>([])
  const pathsByUnit = ref<Record<string, string[]>>({})

  const riderSchema = ref<ActionSchemaBundle | null>(null)
  const riderCatalogs = ref<AbilityBuilderCatalogs>({
    effects: [], projectiles: [], damageTypes: [], categories: [], autoCastSelectors: [], unitTypes: [],
    objectSprites: listObjectSpriteKeys(),
    perks: [],
  })

  const selfStatDefsList = selfStatDefs()
  const auraStatDefsList = allStatDefs()

  // Label lookups for the projection summaries.
  const labels = computed<ModifierLabels>(() => ({
    statLabel: (id) => selfStatDefsList.find((d) => d.id === id)?.label
      ?? auraStatDefsList.find((d) => d.id === id)?.label ?? id,
    abilityStatLabel: (id) => abilityStatDefs.value.find((d) => d.id === id)?.label ?? id,
    abilityLabel: (id) => id, // ability display names not fetched; id is the label
  }))

  const modifiers = computed(() => buildModifierList(form.value, labels.value))
  const selectedEntry = computed(() =>
    selected.value == null ? null
      : modifiers.value.find((e) => e.arrayKey === selected.value!.arrayKey && e.index === selected.value!.index) ?? null)

  function selectModifier(sel: Selection | null) { selected.value = sel }

  // ── mutation helpers ───────────────────────────────────────────────────────
  function replaceArray(kind: ModifierKind, next: unknown[]) {
    const key = KIND_META[kind].arrayKey
    form.value = { ...form.value, [key]: next.length ? next : undefined }
  }
  function listFor(kind: ModifierKind): unknown[] {
    return ((form.value[KIND_META[kind].arrayKey] as unknown[]) ?? []).slice()
  }
  function kindForArrayKey(arrayKey: keyof AuthoredPerkDef): ModifierKind {
    return (Object.keys(KIND_META) as ModifierKind[]).find((k) => KIND_META[k].arrayKey === arrayKey)!
  }

  const DEFAULTS: Partial<Record<ModifierKind, () => unknown>> = {
    unitStat: () => ({ stat: selfStatDefsList[0]?.id ?? '', op: 'add', value: 0 }),
    abilityStat: () => ({ stat: '' }),
    abilityField: () => ({ target: '', action: '', field: '', op: 'multiply', value: 0 }),
    abilityModifier: () => ({ target: '' }),
    abilityRider: () => ({ target: '', trigger: '', actions: [] }),
    grantAbility: () => '',
    perkModifier: () => ({ target: '', ops: [{ targetKey: '', op: 'mult', sourceKey: '' }] }),
    aura: () => ({ radius: 128, targets: 'allies', stacking: 'max', statModifiers: [] }),
    effect: () => ({ name: '' }),
  }

  function addModifier(kind: ModifierKind) {
    const make = DEFAULTS[kind]
    if (!make) return // un-migrated kinds are added via the classic editor for now
    const meta = KIND_META[kind]
    if (meta.shape === 'single') {
      if (form.value[meta.arrayKey] == null) form.value = { ...form.value, [meta.arrayKey]: make() }
      selected.value = { arrayKey: meta.arrayKey, index: 0 }
      return
    }
    if (meta.shape !== 'list') return
    const next = listFor(kind)
    next.push(make())
    replaceArray(kind, next)
    selected.value = { arrayKey: meta.arrayKey, index: next.length - 1 }
  }

  function removeModifier(sel: Selection) {
    const kind = kindForArrayKey(sel.arrayKey)
    if (KIND_META[kind].shape === 'single') {
      form.value = { ...form.value, [sel.arrayKey]: undefined }
      if (selected.value && selected.value.arrayKey === sel.arrayKey) selected.value = null
      return
    }
    if (KIND_META[kind].shape !== 'list') return
    const next = listFor(kind)
    next.splice(sel.index, 1)
    replaceArray(kind, next)
    const s = selected.value
    if (s && s.arrayKey === sel.arrayKey) {
      if (s.index === sel.index) selected.value = null            // deleted the selected one
      else if (s.index > sel.index) selected.value = { arrayKey: s.arrayKey, index: s.index - 1 } // shift up to track it
    }
  }

  function duplicateModifier(sel: Selection) {
    const kind = kindForArrayKey(sel.arrayKey)
    if (KIND_META[kind].shape !== 'list') return
    const next = listFor(kind)
    next.splice(sel.index + 1, 0, structuredClone(next[sel.index]))
    replaceArray(kind, next)
    selected.value = { arrayKey: sel.arrayKey, index: sel.index + 1 }
  }

  // updateSelected replaces the selected element with a new object (callers
  // build the cleaned wire object; blank-stripping lives in the inspector).
  function updateSelected(next: unknown) {
    if (!selected.value) return
    const kind = kindForArrayKey(selected.value.arrayKey)
    if (KIND_META[kind].shape === 'single') {
      form.value = { ...form.value, [selected.value.arrayKey]: next }
      return
    }
    if (KIND_META[kind].shape !== 'list') return
    const list = listFor(kind)
    list[selected.value.index] = next
    replaceArray(kind, list)
  }

  // ── load / select / persist ────────────────────────────────────────────────
  async function reload() {
    try { perks.value = await fetchAuthoredPerkDefs(); loadError.value = '' }
    catch (e) { loadError.value = e instanceof Error ? e.message : String(e) }
  }

  function selectPerk(def: AuthoredPerkDef) {
    form.value = formFromDef(def)
    selectedId.value = def.id
    selected.value = null
    saveError.value = ''; statusMessage.value = ''
  }

  function newPerk() {
    form.value = createBlankForm()
    selectedId.value = null
    selected.value = null
    saveError.value = ''; statusMessage.value = ''
  }

  async function save() {
    saveError.value = ''; statusMessage.value = ''; busy.value = true
    try {
      const cleaned: PerkEditorForm = { ...form.value }
      if (cleaned.effect && !cleaned.effect.name?.trim()) cleaned.effect = undefined
      if (cleaned.grantsAbilities) {
        const grants = cleaned.grantsAbilities.map((g) => g.trim()).filter(Boolean)
        cleaned.grantsAbilities = grants.length ? grants : undefined
      }
      if (cleaned.perkModifiers) {
        const mods = cleaned.perkModifiers
          .map((m) => ({
            target: (m.target ?? '').trim(),
            ops: (m.ops ?? [])
              .map((o) => ({ targetKey: (o.targetKey ?? '').trim(), op: o.op, sourceKey: (o.sourceKey ?? '').trim() }))
              .filter((o) => o.targetKey && o.sourceKey),
          }))
          .filter((m) => m.target && m.ops.length)
        cleaned.perkModifiers = mods.length ? mods : undefined
      }
      await saveEditorPerk(saveRequestFromForm(cleaned))
      await reload()
      selectedId.value = form.value.id
      statusMessage.value = 'Saved.'
    } catch (e) {
      saveError.value = e instanceof EditorValidationError ? e.serverMessage : e instanceof Error ? e.message : String(e)
    } finally { busy.value = false }
  }

  // resetPerk discards unsaved edits, reverting to the last-saved version of the
  // open perk. `perks` reflects the catalog as of the last load/save, so
  // re-selecting from it is exactly "back to where it was last saved". A brand-
  // new (never-saved) perk has no saved version, so it clears to blank.
  function resetPerk() {
    if (selectedId.value) {
      const saved = perks.value.find((p) => p.id === selectedId.value)
      if (saved) { selectPerk(saved); return }
    }
    newPerk()
  }

  async function removePerk(): Promise<'deleted' | 'reset' | null> {
    if (!selectedId.value) return null
    busy.value = true; saveError.value = ''; statusMessage.value = ''
    try {
      const status = await deleteEditorPerk(selectedId.value)
      await reload(); newPerk()
      statusMessage.value = status === 'deleted' ? 'Deleted.' : 'Reset to default.'
      return status
    } catch (e) {
      saveError.value = e instanceof EditorValidationError ? e.serverMessage : e instanceof Error ? e.message : String(e)
      return null
    } finally { busy.value = false }
  }

  async function load() {
    await reload()
    try { pathsByUnit.value = (await fetchUnitDefs()).pathsByUnit } catch { pathsByUnit.value = {} }
    try {
      const defs = await fetchAuthoredAbilityDefs()
      abilityIds.value = defs.map((a) => a.id)
      abilityDefsById.value = Object.fromEntries(defs.map((a) => [a.id, a]))
      abilityDefs.value = defs
    } catch { abilityIds.value = []; abilityDefsById.value = {}; abilityDefs.value = [] }
    try {
      const res = await fetch('/catalog/ability-stats')
      if (res.ok) abilityStatDefs.value = ((await res.json()) as { stats?: AbilityStatDef[] }).stats ?? []
    } catch { /* offline: leave empty */ }
    try {
      const [schema, effects, projectiles, damageTypes, categories, autoCastSelectors, units] = await Promise.all([
        fetchActionSchema(), fetchEffectIds(), fetchProjectileIds(), fetchDamageTypes(),
        fetchAbilityCategories(), fetchAutoCastSelectors(), fetchAuthoredUnitDefs(),
      ])
      riderSchema.value = schema
      riderCatalogs.value = {
        effects, projectiles, damageTypes, categories, autoCastSelectors,
        unitTypes: units.map((u) => u.type), objectSprites: listObjectSpriteKeys(), perks: [],
      }
    } catch { riderSchema.value = null }
  }

  return {
    perks, form, selectedId, selected, busy, loadError, saveError, statusMessage,
    abilityIds, abilityDefsById, abilityDefs, abilityStatDefs, pathsByUnit, selfStatDefsList, auraStatDefsList,
    riderSchema, riderCatalogs,
    modifiers, selectedEntry,
    load, reload, selectPerk, newPerk, save, resetPerk, removePerk,
    selectModifier, addModifier, removeModifier, duplicateModifier, updateSelected,
  }
}
