<template>
  <EditorShell class="item-editor">
    <template #sidebar>
      <EditorSidebar
        title="Items"
        new-label="Add New Item"
        :groups="sidebarGroups"
        :selected-id="selectedId"
        :search="search"
        search-placeholder="Search items…"
        empty-text="No items match."
        @update:search="search = $event"
        @select="selectItem"
        @new="newItem"
        @duplicate="duplicateItem"
      />
    </template>

    <template #main>
      <template v-if="form">
        <EditorHeader
          :title="form.displayName"
          :badge="form.tier"
          :badge-color="TIER_COLORS[form.tier as ItemTier]"
          :breadcrumb="breadcrumb"
          :file-path="filePath"
          :id="form.id"
          :id-editable="form.isNew"
          id-input-id="ie-id"
          :saving="saving"
          :save-disabled="saving || !canSave"
          :saved-label="savedLabel"
          :error="saveError"
          :remove-label="removeLabel"
          @update:id="form.id = $event"
          @save="save"
          @remove="removeOrReset(form.id)"
        />

        <GameScrollArea class="item-editor__scroll">
          <div class="item-editor__grid">
            <!-- 1. Identity -->
            <SectionCard title="Identity" :index="sectionIndex('identity')">
              <EditorField label="Display Name" for-id="ie-display-name">
                <input id="ie-display-name" v-model.trim="form.displayName" type="text" />
              </EditorField>
              <!-- Generated, not authored: this is verbatim what a match
                   tooltip shows for the item, so it can never drift from the
                   stats above it. Written to the def on save. -->
              <EditorField label="Description" hint="(generated — what the match tooltip shows)" for-id="ie-description">
                <textarea
                  id="ie-description"
                  :value="generatedDescription"
                  rows="3"
                  readonly
                  class="item-editor__generated"
                ></textarea>
              </EditorField>
              <div class="item-editor__pair">
                <EditorField label="Kind" for-id="ie-kind">
                  <select id="ie-kind" v-model="form.kind" @change="onKindChanged">
                    <option value="equipment">Equipment</option>
                    <option value="consumable">Consumable</option>
                  </select>
                </EditorField>
                <EditorField label="Tier" for-id="ie-tier">
                  <select id="ie-tier" v-model="form.tier">
                    <option v-for="t in TIER_OPTIONS" :key="t" :value="t">{{ t }}</option>
                  </select>
                </EditorField>
              </div>
              <EditorField label="Category" hint="(grouping only; sets the catalog folder)" for-id="ie-category">
                <select id="ie-category" v-model="form.category">
                  <option v-for="c in CATEGORY_OPTIONS" :key="c" :value="c">{{ c }}</option>
                </select>
              </EditorField>
            </SectionCard>

            <!-- Preview: the icon (with its art pickers underneath) on the
                 left, the live description card on the right. -->
            <SectionCard title="Preview" :index="sectionIndex('preview')" class="item-editor__wide">
              <div class="item-editor__preview">
                <div class="item-editor__icon-col">
                  <IconPreview :src="previewIconUrl" :size="112" />
                  <UiButton size="sm" variant="active" data-test="icon-gallery-open" @click="galleryOpen = true">Gallery</UiButton>
                  <EditorField label="Upload PNG" :hint="`(max ${MAX_ITEM_ICON_BYTES / 1024} KB)`" for-id="ie-icon-upload">
                    <input id="ie-icon-upload" type="file" accept="image/png" @change="onIconFileChosen" />
                  </EditorField>
                </div>

                <ItemPreviewCard :def="previewDef" :craft="previewCraft" class="item-editor__preview-card" />
              </div>
            </SectionCard>

            <!-- Consumable -->
            <SectionCard v-if="form.kind === 'consumable'" title="Consumable" :index="sectionIndex('consumable')">
              <EditorField label="Effect Type" for-id="ie-consumable-type">
                <select id="ie-consumable-type" v-model="form.consumable.type">
                  <option v-for="t in CONSUMABLE_TYPES" :key="t.value" :value="t.value">{{ t.label }}</option>
                </select>
              </EditorField>
              <div class="item-editor__pair">
                <EditorField label="Amount" hint="(HP / XP)" for-id="ie-consumable-amount">
                  <input id="ie-consumable-amount" v-model.number="form.consumable.amount" type="number" min="0" />
                </EditorField>
                <EditorField label="AoE Range" hint="(0 = default 100)" for-id="ie-consumable-range">
                  <input id="ie-consumable-range" v-model.number="form.consumable.range" type="number" min="0" />
                </EditorField>
              </div>
              <div class="item-editor__pair">
                <EditorField label="Duration (s)" hint="(0 = instant)" for-id="ie-consumable-duration">
                  <input id="ie-consumable-duration" v-model.number="form.consumable.durationSeconds" type="number" min="0" />
                </EditorField>
                <EditorField label="Max Stacks" hint="(0/1 = single)" for-id="ie-max-stacks">
                  <input id="ie-max-stacks" v-model.number="form.maxStacks" type="number" min="0" />
                </EditorField>
              </div>
              <label class="ed-check" for="ie-consumable-split">
                <input id="ie-consumable-split" v-model="form.consumable.split" type="checkbox" />
                Split amount across affected units
              </label>
            </SectionCard>

            <!-- Stats -->
            <SectionCard v-if="form.kind === 'equipment'" title="Stats" :index="sectionIndex('stats')">
              <div class="item-editor__stats">
                <EditorField v-for="f in FLAT_MOD_FIELDS" :key="f.key" :label="f.label" :for-id="`ie-mod-${f.key}`">
                  <input :id="`ie-mod-${f.key}`" v-model.number="form.mods[f.key]" type="number" />
                </EditorField>
                <EditorField label="Dodge %" hint="(0–99)" for-id="ie-mod-dodge">
                  <input id="ie-mod-dodge" v-model.number="form.mods.dodgePct" type="number" min="0" max="99" />
                </EditorField>
                <EditorField label="Block %" hint="(0–99)" for-id="ie-mod-block">
                  <input id="ie-mod-block" v-model.number="form.mods.blockPct" type="number" min="0" max="99" />
                </EditorField>
              </div>
            </SectionCard>

            <!-- Elemental -->
            <SectionCard v-if="form.kind === 'equipment'" title="Elemental Damage" :index="sectionIndex('elemental')">
              <RepeatableList
                :rows="form.elemental.length"
                add-label="Add Elemental Damage"
                empty-text="No elemental damage."
                @add="form.elemental.push({ type: 'fire', amount: 5 })"
              >
                <div v-for="(row, idx) in form.elemental" :key="idx" class="item-editor__elem-row">
                  <select v-model="row.type" :aria-label="`Element ${idx + 1}`">
                    <option v-for="t in ELEMENTAL_TYPES" :key="t" :value="t">{{ t }}</option>
                  </select>
                  <input v-model.number="row.amount" type="number" :aria-label="`Amount ${idx + 1}`" />
                  <button type="button" class="item-editor__row-del" @click="form.elemental.splice(idx, 1)">✕</button>
                </div>
              </RepeatableList>
            </SectionCard>

            <!-- Procs -->
            <SectionCard v-if="form.kind === 'equipment'" title="Proc Effects" :index="sectionIndex('procs')" class="item-editor__wide">
              <RepeatableList
                :rows="form.procs.length"
                add-label="Add Proc"
                empty-text="No procs. Add one to make this item roll a chance effect in combat."
                @add="addProc"
              >
                <div v-for="(proc, i) in form.procs" :key="i" class="proc-block">
                  <!-- Summary row: everything that identifies the proc at a
                       glance. The nine override fields live behind the pencil. -->
                  <div class="proc-row">
                    <span class="proc-row__n">Proc {{ i + 1 }}</span>
                    <select :id="`ie-proc-${i}-trigger`" v-model="proc.trigger" :aria-label="`Proc ${i + 1} trigger`">
                      <option v-for="t in PROC_TRIGGERS" :key="t.value" :value="t.value">{{ t.label }}</option>
                    </select>
                    <div class="proc-row__pct">
                      <input :id="`ie-proc-${i}-chance`" v-model.number="proc.chancePct" type="number" min="1" max="100" :aria-label="`Proc ${i + 1} chance`" />
                      <span>%</span>
                    </div>
                    <select :id="`ie-proc-${i}-effect`" v-model="proc.effect" :aria-label="`Proc ${i + 1} effect`">
                      <option value="" disabled>Select an effect…</option>
                      <option v-for="p in procEffects" :key="p.id" :value="p.id">
                        {{ p.id }} — {{ p.damage }} {{ p.damageType }}
                      </option>
                    </select>
                    <span class="proc-row__ovr">{{ overrideSummary(proc) }}</span>
                    <button
                      type="button"
                      class="proc-row__act"
                      :aria-expanded="overridesOpen[i] === true"
                      :title="overridesOpen[i] ? 'Hide overrides' : 'Edit overrides'"
                      @click="overridesOpen[i] = !overridesOpen[i]"
                    >✎</button>
                    <button type="button" class="proc-row__act proc-block__remove" title="Remove proc" @click="removeProc(i)">✕</button>
                  </div>

                  <div v-if="overridesOpen[i]" class="proc-overrides">
                    <p class="proc-overrides__hint">Blank = inherit from the effect.</p>
                    <div class="proc-overrides__grid">
                      <EditorField
                        v-for="f in PROC_OVERRIDE_FIELDS"
                        :key="f.key"
                        :label="f.label"
                        :for-id="`ie-proc-${i}-${f.key}`"
                      >
                        <input
                          :id="`ie-proc-${i}-${f.key}`"
                          :value="proc[f.key] ?? ''"
                          :placeholder="effectPlaceholder(proc.effect, f.key)"
                          type="number"
                          @input="bindNullable(proc, f.key, $event)"
                        />
                      </EditorField>
                    </div>
                  </div>
                </div>
              </RepeatableList>
            </SectionCard>

            <!-- Cost -->
            <SectionCard title="Cost" :index="sectionIndex('cost')">
              <EditorField label="Purchase Cost (Gold)" hint="(where it sells is set at the shop level)" for-id="ie-cost-gold">
                <input id="ie-cost-gold" v-model.number="form.costGold" type="number" min="0" />
              </EditorField>
            </SectionCard>

            <!-- Crafting -->
            <SectionCard title="Crafting" :index="sectionIndex('crafting')">
              <EditorField label="Crafting source" for-id="ie-crafting-source">
                <select id="ie-crafting-source" v-model="craftingSource">
                  <option value="none">Not craftable</option>
                  <option value="recipe">Recipe (ingredients at the Artificer)</option>
                </select>
              </EditorField>

              <template v-if="form.crafting.isRecipe">
                <EditorField label="Craft Cost (Gold)" for-id="ie-recipe-cost">
                  <input id="ie-recipe-cost" v-model.number="form.crafting.recipeCost" type="number" min="0" />
                </EditorField>
                <label class="ed-check" for="ie-recipe-starter">
                  <input id="ie-recipe-starter" v-model="form.crafting.starter" type="checkbox" />
                  Automatically learned by every player
                </label>

                <RepeatableList
                  :rows="form.crafting.inputs.length"
                  add-label="Add Ingredient"
                  @add="form.crafting.inputs.push('')"
                >
                  <div v-for="(_input, idx) in form.crafting.inputs" :key="idx" class="item-editor__ingredient">
                    <select :id="`ie-crafting-input-${idx}`" v-model="form.crafting.inputs[idx]" :aria-label="`Ingredient ${idx + 1}`">
                      <option value="" disabled>Select an item…</option>
                      <option v-for="d in allEquipmentItems" :key="d.id" :value="d.id">{{ d.displayName }}</option>
                    </select>
                    <button
                      type="button"
                      class="item-editor__row-del"
                      :disabled="form.crafting.inputs.length <= 2"
                      @click="form.crafting.inputs.splice(idx, 1)"
                    >✕</button>
                  </div>
                </RepeatableList>
              </template>
            </SectionCard>

            <!-- Validation sits last, pinned to the final column, so the
                 checklist reads as the sign-off at the bottom right of the
                 form rather than competing with it in a side rail. -->
            <SectionCard title="Validation" class="item-editor__validation">
              <ValidationChecklist :checks="checks" />
              <span v-if="statusNote" class="item-editor__status-note">{{ statusNote }}</span>
            </SectionCard>
          </div>
        </GameScrollArea>
      </template>

      <div v-else class="item-editor__empty">
        <p v-if="loadError" role="alert">{{ loadError }}</p>
        <p v-else>Select an item or create a new one.</p>
      </div>
    </template>

  </EditorShell>

  <!-- Icon gallery overlay -->
  <div v-if="galleryOpen && form" class="icon-gallery-overlay" @click.self="galleryOpen = false">
    <UiPanel variant="worldMenu" :padding="0" class="icon-gallery">
      <div class="icon-gallery__inner">
        <div class="icon-gallery__header">
          <span>Choose an icon</span>
          <UiButton size="sm" @click="galleryOpen = false">Close</UiButton>
        </div>
        <div class="icon-gallery__filter">
          <button
            v-for="group in iconGroups"
            :key="group.name"
            type="button"
            class="icon-gallery__chip"
            :class="{ 'icon-gallery__chip--on': selectedGroups.has(group.name) }"
            @click="toggleGroup(group.name)"
          >
            {{ group.name }} <span class="icon-gallery__chip-count">{{ group.keys.length }}</span>
          </button>
          <button type="button" class="icon-gallery__chip" @click="setAllGroups(true)">All</button>
          <button type="button" class="icon-gallery__chip" @click="setAllGroups(false)">None</button>
        </div>
        <GameScrollArea class="icon-gallery__scroll">
          <div v-if="galleryKeys.length" class="icon-gallery__grid">
            <button
              v-for="key in galleryKeys"
              :key="key"
              type="button"
              class="icon-gallery__item"
              data-test="icon-gallery-cell"
              @click="pickGalleryIcon(key)"
            >
              <img :src="getItemImageSourceUrl(key)" alt="" />
              <span>{{ key }}</span>
            </button>
          </div>
          <p v-else class="icon-gallery__empty">No icon groups selected.</p>
        </GameScrollArea>
      </div>
    </UiPanel>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, reactive, ref } from 'vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiPanel from '@/components/ui/UiPanel.vue'
import GameScrollArea from '@/components/ui/GameScrollArea.vue'
import EditorShell from '@/components/editor/EditorShell.vue'
import EditorSidebar from '@/components/editor/EditorSidebar.vue'
import type { SidebarGroup } from '@/components/editor/EditorSidebar.vue'
import EditorHeader from '@/components/editor/EditorHeader.vue'
import EditorField from '@/components/editor/EditorField.vue'
import SectionCard from '@/components/editor/SectionCard.vue'
import RepeatableList from '@/components/editor/RepeatableList.vue'
import ValidationChecklist from '@/components/editor/ValidationChecklist.vue'
import IconPreview from '@/components/editor/IconPreview.vue'
import ItemPreviewCard from '@/components/ItemPreviewCard.vue'
import { fetchItemDefs, fetchRecipeDefs } from '@/game/maps/catalog'
import type { ItemDef, ItemTier } from '@/game/maps/itemDefs'
import { EditorValidationError, MAX_ITEM_ICON_BYTES, deleteEditorItem, fetchProcEffectDefs, uploadItemIcon, saveEditorItem } from '@/game/items/itemEditorApi'
import type { ProcEffectDef } from '@/game/items/itemEditorApi'
import { blankProc, createBlankForm, formFromDef, saveRequestFromForm } from '@/game/items/itemEditorForm'
import type { ItemEditorForm, ProcForm } from '@/game/items/itemEditorForm'
import { previewDefFromForm } from '@/game/items/itemEditorPreview'
import { isFormSaveable, validateItemForm } from '@/game/items/itemEditorValidation'
import { getItemImageSourceUrl, listIconGroups } from '@/game/rendering/itemAssets'
import { TIER_COLORS, buildItemTooltipBody } from '@/game/items/itemRules'

const items = ref<ItemDef[]>([])            // full catalog, refreshed after saves
const recipesByOutput = ref(new Map<string, { inputs: string[]; costGold: number; starter?: boolean }>())
const procEffects = ref<ProcEffectDef[]>([])
const loadError = ref('')
const search = ref('')
const selectedId = ref('')                  // '' = nothing selected
const form = ref<ItemEditorForm | null>(null)
const saving = ref(false)
const saveError = ref('')                   // EditorValidationError message shown beside Save
const statusNote = ref('')                // transient feedback after removeOrReset
const galleryOpen = ref(false)

// The icon gallery only surfaces the icon library (assets/icons/**), grouped by
// subdirectory. Groups start all-selected; toggling a group filters the grid.
const iconGroups = listIconGroups()
const selectedGroups = reactive(new Set<string>(iconGroups.map((g) => g.name)))
const galleryKeys = computed<string[]>(() =>
  iconGroups.filter((g) => selectedGroups.has(g.name)).flatMap((g) => g.keys),
)
function toggleGroup(name: string) {
  if (selectedGroups.has(name)) selectedGroups.delete(name)
  else selectedGroups.add(name)
}
function setAllGroups(on: boolean) {
  selectedGroups.clear()
  if (on) for (const g of iconGroups) selectedGroups.add(g.name)
}

// Overrides-expanded state per proc, keyed by list index. Indices shift when a
// proc is removed, so removeProc re-keys it rather than leaving it stale.
const overridesOpen = reactive<Record<number, boolean>>({})

const TIER_OPTIONS: ItemTier[] = ['common', 'uncommon', 'rare', 'epic', 'legendary']
// Organizational grouping only — also picks the catalog subdirectory the def is
// written to (Weapon → catalog/items/weapons/…). Not an equip restriction.
const CATEGORY_OPTIONS = ['Weapon', 'Armor', 'Shield', 'Accessory', 'Consumable']
// Mirrors itemCategorySubdir on the server; drives the file path in the header.
const CATEGORY_SUBDIR: Record<string, string> = {
  Weapon: 'weapons', Armor: 'armor', Shield: 'shields', Accessory: 'accessories', Consumable: 'consumables',
}
// Only the consumable effect types the server actually implements
// (applyConsumableToUnitLocked). Future types (buffs, mana) add cases there
// first, then an option here.
const CONSUMABLE_TYPES = [
  { value: 'heal', label: 'Heal (restore HP)' },
  { value: 'grant_xp', label: 'Grant XP' },
]
const ELEMENTAL_TYPES = ['fire', 'cold', 'lightning', 'holy', 'shadow', 'physical']

const FLAT_MOD_FIELDS: { key: keyof ItemEditorForm['mods']; label: string }[] = [
  { key: 'hp', label: 'HP' },
  { key: 'damage', label: 'Damage' },
  { key: 'armor', label: 'Armor' },
  { key: 'attackSpeed', label: 'Attack Speed' },
  { key: 'moveSpeed', label: 'Move Speed' },
  { key: 'healthRegen', label: 'Health Regen' },
  { key: 'maxShield', label: 'Max Shield' },
]

// The 9 nullable proc override fields — shared between ProcForm's type and
// ProcEffectDef's optional fields, so `effect[key]` below type-checks without
// a cast.
type ProcNullableKey = Exclude<keyof ProcForm, 'trigger' | 'effect' | 'chancePct'>
const PROC_OVERRIDE_FIELDS: { key: ProcNullableKey; label: string }[] = [
  { key: 'damage', label: 'Damage' },
  { key: 'projectileScale', label: 'Projectile Scale' },
  { key: 'bounceCount', label: 'Bounce Count' },
  { key: 'bounceRange', label: 'Bounce Range' },
  { key: 'bounceDamageFalloff', label: 'Bounce Falloff' },
  { key: 'slowMultiplier', label: 'Slow Multiplier' },
  { key: 'slowDurationSeconds', label: 'Slow Duration (s)' },
  { key: 'burnDamagePerSecond', label: 'Burn Damage/s' },
  { key: 'burnDurationSeconds', label: 'Burn Duration (s)' },
]

// The combat events a proc can hang off. Adding a trigger here requires the
// server to handle it too (game.ItemProcTrigger + rollEquipment*ProcsLocked).
const PROC_TRIGGERS: { value: ProcForm['trigger']; label: string }[] = [
  { value: 'onHit', label: 'On hit' },
  { value: 'onStruck', label: 'When struck' },
]

// ── Sidebar ─────────────────────────────────────────────────────────────────

const filteredItems = computed(() =>
  items.value.filter((d) =>
    search.value === '' || d.id.includes(search.value.toLowerCase()) || d.displayName.toLowerCase().includes(search.value.toLowerCase())))

const sidebarGroups = computed<SidebarGroup[]>(() =>
  TIER_OPTIONS
    .map((tier) => ({
      label: tier,
      color: TIER_COLORS[tier],
      entries: filteredItems.value
        .filter((d) => d.tier === tier)
        .map((d) => ({
          id: d.id,
          name: d.displayName,
          iconUrl: getItemImageSourceUrl(d.iconKey),
          color: TIER_COLORS[d.tier],
        })),
    }))
    .filter((g) => g.entries.length > 0))

// Crafting inputs stay equipment-only and are never constrained by the sidebar
// search (a leftover search term must not truncate the dropdown).
const allEquipmentItems = computed(() => items.value.filter((d) => d.kind === 'equipment'))

// ── Header ──────────────────────────────────────────────────────────────────

const breadcrumb = computed(() => {
  if (!form.value) return ''
  const kind = form.value.kind === 'consumable' ? 'Consumable' : 'Equipment'
  return `${kind} • ${form.value.category} • Tier: ${form.value.tier}`
})

const filePath = computed(() => {
  if (!form.value?.id) return ''
  const dir = CATEGORY_SUBDIR[form.value.category] ?? 'misc'
  return `server/internal/game/catalog/items/${dir}/${form.value.tier}/${form.value.id}.json`
})

/** True when the open item is one the author created rather than a shipped one.
 *  `custom` (not `overridden`) is the reliable signal — see ListItemDefs. */
const selectedIsCustom = computed(() =>
  items.value.find((d) => d.id === selectedId.value)?.custom === true)

// Deleting an item you made and resetting a shipped one to its catalog default
// are different acts, so the button says which it is. An unsaved draft has
// nothing to remove, so it shows no button at all.
const removeLabel = computed(() => {
  if (!form.value || form.value.isNew) return ''
  return selectedIsCustom.value ? 'Delete' : 'Reset'
})

// "Last saved" is session-only: it starts at the moment the Save button
// succeeds and ages from there. The server stores no modified time, so a page
// reload legitimately shows nothing until the next save.
const lastSavedAt = ref<number | null>(null)
const now = ref(Date.now())
let clock: ReturnType<typeof setInterval> | null = null

const savedLabel = computed(() => {
  if (lastSavedAt.value === null) return ''
  const secs = Math.max(0, Math.round((now.value - lastSavedAt.value) / 1000))
  if (secs < 45) return 'just now'
  const mins = Math.round(secs / 60)
  if (mins < 60) return `${mins} min ago`
  const hours = Math.round(mins / 60)
  return `${hours} hr ago`
})

// ── Sections ────────────────────────────────────────────────────────────────

// Section numbers follow what is actually on screen, so a consumable doesn't
// show gaps where the equipment-only cards would have been.
const visibleSections = computed(() => {
  const equipment = form.value?.kind === 'equipment'
  return [
    'identity',
    'preview',
    ...(equipment ? ['stats', 'elemental', 'procs'] : ['consumable']),
    'cost',
    'crafting',
  ]
})
function sectionIndex(key: string): number {
  return visibleSections.value.indexOf(key) + 1
}

// Crafting source select ↔ the item's isRecipe flag. Modeled as an explicit
// choice ("Not craftable" / "Recipe") rather than a bare checkbox so future
// non-recipe crafting sources can be added as options without reworking the UI.
const craftingSource = computed<'none' | 'recipe'>({
  get: () => (form.value?.crafting.isRecipe ? 'recipe' : 'none'),
  set: (v) => {
    if (form.value) form.value.crafting.isRecipe = v === 'recipe'
  },
})

// ── Procs ───────────────────────────────────────────────────────────────────

function addProc() {
  if (!form.value) return
  form.value.procs.push(blankProc())
  // A fresh proc opens with its overrides collapsed (blank = inherit).
  overridesOpen[form.value.procs.length - 1] = false
}

function removeProc(index: number) {
  if (!form.value) return
  form.value.procs.splice(index, 1)
  // overridesOpen is index-keyed: shift the tail down so the open/closed state
  // still belongs to the proc the user opened.
  const open = form.value.procs.map((_, i) => overridesOpen[i >= index ? i + 1 : i] ?? false)
  for (const key of Object.keys(overridesOpen)) delete overridesOpen[Number(key)]
  open.forEach((v, i) => { overridesOpen[i] = v })
}

/** "2 overrides" / "no overrides" — the count of fields the item re-tunes. */
function overrideSummary(proc: ProcForm): string {
  const n = PROC_OVERRIDE_FIELDS.filter((f) => proc[f.key] !== null).length
  if (n === 0) return 'no overrides'
  return `${n} override${n > 1 ? 's' : ''}`
}

function effectPlaceholder(effectId: string, key: ProcNullableKey): string {
  const effect = procEffects.value.find((p) => p.id === effectId)
  if (!effect) return ''
  const value = effect[key]
  return value === undefined ? '' : String(value)
}

// Nullable-override binding: v-model.number turns a cleared input into 0,
// which is indistinguishable from "explicitly override to 0". Bind :value +
// @input directly instead, so an empty string maps to null ("inherit").
function bindNullable(proc: ProcForm, key: ProcNullableKey, ev: Event) {
  const value = (ev.target as HTMLInputElement).value
  proc[key] = value === '' ? null : Number(value)
}

// ── Preview + validation ────────────────────────────────────────────────────

// The draft as the server would serve it — procs resolved against the effect
// catalog so the preview can show "25 Fire bolt" for an unsaved item.
const previewDef = computed<ItemDef>(() =>
  previewDefFromForm(form.value ?? createBlankForm(), procEffects.value))

// The recipe as the preview card renders it. Ingredients are ids on the form,
// so resolve each to the icon a player would recognise; an id that names no
// item (a half-filled row) is dropped rather than shown as a broken icon.
const previewCraft = computed(() => {
  if (!form.value?.crafting.isRecipe) return undefined
  const inputs = form.value.crafting.inputs
    .map((id) => items.value.find((d) => d.id === id))
    .filter((d): d is ItemDef => d !== undefined)
    .map((d) => ({ def: d, iconUrl: getItemImageSourceUrl(d.iconKey) }))
  return { costGold: form.value.crafting.recipeCost, inputs }
})

// The icon's <img src>. Reading iconUploadedAt off the catalog entry is what
// makes this recompute after an upload: reloadCatalog() refreshes `items`, the
// asset layer learns about the new icon, and getItemImageSourceUrl then hands
// back the versioned server URL instead of the bundled art.
const previewIconUrl = computed(() => {
  if (!form.value) return ''
  void items.value.find((d) => d.id === form.value?.id)?.iconUploadedAt
  return getItemImageSourceUrl(form.value.iconKey || form.value.id)
})

const checks = computed(() => {
  if (!form.value) return []
  return validateItemForm(form.value, { knownItemIds: new Set(items.value.map((d) => d.id)) })
})

// Save is blocked while the checklist has a failure — most importantly an id
// collision, which would otherwise overwrite an existing item.
const canSave = computed(() => !!form.value?.id && isFormSaveable(checks.value))

// The item's description is DERIVED, not authored: it is exactly the stat block
// a match tooltip renders (buildItemTooltipBody). The shop tooltip in a match
// prints `description` as its stat text, which is why the shipped items
// hand-copied "+5 damage, …" into it — this keeps that text in lockstep with
// the stats instead. Written to the def on save.
const generatedDescription = computed(() => buildItemTooltipBody(previewDef.value))

// ── Catalog + CRUD ──────────────────────────────────────────────────────────

async function reloadCatalog() {
  const [defs, recipes] = await Promise.all([fetchItemDefs(), fetchRecipeDefs().catch(() => [])])
  items.value = defs
  const map = new Map<string, { inputs: string[]; costGold: number; starter?: boolean }>()
  for (const r of recipes) map.set(r.output, { inputs: r.inputs, costGold: r.costGold, starter: r.starter })
  recipesByOutput.value = map
}

onMounted(async () => {
  clock = setInterval(() => { now.value = Date.now() }, 15_000)
  try {
    await reloadCatalog()
    procEffects.value = await fetchProcEffectDefs()
  } catch (err) {
    loadError.value = err instanceof Error ? err.message : String(err)
  }
})

onBeforeUnmount(() => {
  if (clock !== null) clearInterval(clock)
})

function resetStatus() {
  saveError.value = ''
  statusNote.value = ''
  lastSavedAt.value = null
  for (const key of Object.keys(overridesOpen)) delete overridesOpen[Number(key)]
}

function selectItem(id: string) {
  const def = items.value.find((d) => d.id === id)
  if (!def) return
  selectedId.value = id
  resetStatus()
  form.value = formFromDef(def, recipesByOutput.value.get(id) ?? null)
}

function newItem() {
  selectedId.value = ''
  resetStatus()
  form.value = createBlankForm()
}

/** Clone the selected item as a brand-new, unsaved def. The id is blanked (it
 *  must be unique, and a saved id is immutable), so the author names it. */
function duplicateItem(id: string) {
  const def = items.value.find((d) => d.id === id)
  if (!def) return
  selectedId.value = ''
  resetStatus()
  const copy = formFromDef(def, recipesByOutput.value.get(id) ?? null)
  copy.id = ''
  copy.isNew = true
  copy.displayName = `${def.displayName} Copy`
  form.value = copy
}

async function save() {
  if (!form.value) return
  saving.value = true
  saveError.value = ''
  statusNote.value = ''
  try {
    // Description is derived, so stamp the current generated text onto the form
    // right before saving — that is what the match tooltip will show.
    form.value.description = generatedDescription.value
    await saveEditorItem(saveRequestFromForm(form.value))
    lastSavedAt.value = Date.now()
    now.value = lastSavedAt.value
    form.value.isNew = false
    selectedId.value = form.value.id
    await reloadCatalog()
  } catch (err) {
    saveError.value = err instanceof EditorValidationError ? err.serverMessage
      : err instanceof Error ? err.message : String(err)
  } finally {
    saving.value = false
  }
}

/**
 * Deletes an author-created item, or undoes the last save on a shipped one.
 * The server decides which and says so in its status (see DeleteEditorItem):
 * `reverted` = back to the state before the last save, `reset` = back to the
 * catalog default (nothing left to undo).
 */
async function removeOrReset(id: string) {
  const def = items.value.find((d) => d.id === id)
  if (!def) return
  const custom = def.custom === true
  const ok = window.confirm(custom
    ? `Delete "${def.displayName}" permanently?`
    : `Undo the last save on "${def.displayName}"? It goes back to how it was before you last saved (or to the catalog default if there is nothing left to undo).`)
  if (!ok) return

  saveError.value = ''
  statusNote.value = ''
  try {
    const status = await deleteEditorItem(id)
    await reloadCatalog()
    if (status === 'deleted') {
      if (selectedId.value === id) newItem()
      statusNote.value = 'Item deleted.'
      return
    }
    selectItem(id) // reload the restored def into the form
    statusNote.value = status === 'reverted'
      ? 'Reverted to the state before your last save.'
      : 'Reset to the catalog default.'
  } catch (err) {
    saveError.value = err instanceof Error ? err.message : String(err)
  }
}

async function onIconFileChosen(ev: Event) {
  const input = ev.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file || !form.value) return
  if (form.value.isNew || !form.value.id) {
    saveError.value = 'Save the item once before uploading an icon.'
    input.value = ''
    return
  }
  saveError.value = ''
  statusNote.value = ''
  try {
    await uploadItemIcon(form.value.id, file)
    form.value.iconKey = form.value.id // server forces iconKey to the id
    // Refreshes iconUploadedAt, which is what tells the asset layer to serve
    // the upload instead of the bundled art (and busts the browser cache).
    await reloadCatalog()
    statusNote.value = 'Icon uploaded.'
  } catch (err) {
    saveError.value = err instanceof Error ? err.message : String(err)
  } finally {
    input.value = ''
  }
}

function pickGalleryIcon(key: string) {
  if (form.value) form.value.iconKey = key
  galleryOpen.value = false
}

// When the item's kind flips, keep the category coherent: consumables move to
// the Consumable category (which drives the consumables/ catalog subdir + the
// merchant_potions loot bucket); switching back to equipment clears it.
function onKindChanged() {
  if (!form.value) return
  if (form.value.kind === 'consumable') {
    form.value.category = 'Consumable'
  } else if (form.value.category === 'Consumable') {
    form.value.category = 'Weapon'
  }
}
</script>

<style scoped>
.item-editor {
  font-family: var(--font-body);
  color: var(--ed-text);
}

.item-editor__scroll {
  flex: 1 1 auto;
  min-height: 0;
}

/* Cards flow into as many columns as fit; the proc card asks for two because
   its rows are wide. */
.item-editor__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: var(--ed-gap);
  align-content: start;
  padding-right: 4px;
}

.item-editor__wide {
  grid-column: span 2;
}

@media (max-width: 1500px) {
  .item-editor__wide {
    grid-column: span 1;
  }
}

.item-editor__pair {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}

.item-editor__stats {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}

.item-editor__empty {
  flex: 1 1 auto;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--ed-text-dim);
}

/* A generated, read-only field: dimmer text and no focus ring, so it reads as
   output rather than an input someone forgot to enable. */
.item-editor__generated {
  color: var(--ed-text-dim);
  font-style: italic;
  resize: none;
}

.item-editor__generated:focus {
  border-color: var(--ed-line);
  box-shadow: none;
}

/* ── Preview: icon + its art pickers on the left, description on the right ── */
.item-editor__preview {
  display: grid;
  /* Both tracks are minmax(0, …): a fixed `auto` left track lets the file
     input's wide intrinsic size push the column open and shove content out of
     the card. */
  grid-template-columns: minmax(0, 132px) minmax(0, 1fr);
  gap: 14px;
  align-items: start;
}

.item-editor__icon-col {
  min-width: 0;
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 8px;
}

.item-editor__icon-col :deep(.ed-icon) {
  align-self: center;
}

.item-editor__preview-card {
  min-width: 0;
}

@media (max-width: 760px) {
  .item-editor__preview {
    grid-template-columns: minmax(0, 1fr);
  }
}

/* ── Repeatable rows ── */
.item-editor__elem-row {
  display: grid;
  grid-template-columns: 1fr 90px auto;
  gap: 6px;
  align-items: center;
}

.item-editor__ingredient {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 6px;
  align-items: center;
}

.item-editor__row-del {
  padding: 4px 8px;
  font-size: 0.76rem;
  color: var(--ed-text-dim);
  background: none;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
}

.item-editor__row-del:hover:not(:disabled) {
  color: var(--ed-danger);
  border-color: rgba(240, 132, 108, 0.4);
}

.item-editor__row-del:disabled {
  opacity: 0.4;
}

/* ── Procs ── */
.proc-block {
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
  padding: 6px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.proc-row {
  display: grid;
  grid-template-columns: auto 130px 74px minmax(0, 1fr) auto auto auto;
  gap: 6px;
  align-items: center;
}

.proc-row__n {
  font-size: 0.72rem;
  font-weight: 700;
  color: var(--ed-brass-dim);
  white-space: nowrap;
}

.proc-row__pct {
  display: flex;
  align-items: center;
  gap: 3px;
  font-size: 0.76rem;
  color: var(--ed-text-dim);
}

.proc-row__ovr {
  font-size: 0.7rem;
  color: var(--ed-text-dim);
  white-space: nowrap;
}

.proc-row__act {
  padding: 4px 7px;
  font-size: 0.78rem;
  line-height: 1;
  color: var(--ed-text-dim);
  background: none;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
}

.proc-row__act:hover {
  color: var(--ed-brass);
  border-color: var(--ed-line-strong);
}

.proc-block__remove:hover {
  color: var(--ed-danger);
  border-color: rgba(240, 132, 108, 0.4);
}

.proc-overrides {
  border-top: 1px solid var(--ed-line);
  padding-top: 6px;
}

.proc-overrides__hint {
  margin: 0 0 6px;
  font-size: 0.7rem;
  color: var(--ed-text-dim);
}

.proc-overrides__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(130px, 1fr));
  gap: 8px;
}

/* ── Validation ──
   Last card in the grid AND pinned to the final column, so it lands bottom
   right of the form no matter how many columns fit. */
.item-editor__validation {
  grid-column: -2 / -1;
}

.item-editor__status-note {
  font-size: 0.76rem;
  color: var(--ed-ok);
}

/* ── Icon gallery ── */
.icon-gallery-overlay {
  position: fixed;
  inset: 0;
  z-index: 200;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(6, 8, 14, 0.72);
}

.icon-gallery {
  width: min(900px, 92vw);
  height: min(680px, 86vh);
}

.icon-gallery__inner {
  height: 100%;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px;
  box-sizing: border-box;
}

.icon-gallery__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-family: var(--font-title);
  font-size: 0.9rem;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--ed-brass);
}

.icon-gallery__filter {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.icon-gallery__chip {
  padding: 3px 8px;
  font-size: 0.7rem;
  color: var(--ed-text-dim);
  background: rgba(212, 168, 71, 0.06);
  border: 1px solid var(--ed-line);
  border-radius: 999px;
}

.icon-gallery__chip--on {
  color: var(--ed-brass);
  border-color: var(--ed-line-strong);
}

.icon-gallery__chip-count {
  opacity: 0.6;
}

.icon-gallery__scroll {
  flex: 1 1 auto;
  min-height: 0;
}

.icon-gallery__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(88px, 1fr));
  gap: 6px;
  padding-right: 4px;
}

.icon-gallery__item {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 3px;
  padding: 6px 4px;
  background: rgba(8, 6, 4, 0.4);
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
}

.icon-gallery__item:hover {
  border-color: var(--ed-line-strong);
}

.icon-gallery__item img {
  width: 40px;
  height: 40px;
  object-fit: contain;
  image-rendering: pixelated;
}

.icon-gallery__item span {
  font-size: 0.6rem;
  color: var(--ed-text-dim);
  text-align: center;
  word-break: break-all;
}

.icon-gallery__empty {
  color: var(--ed-text-dim);
  font-size: 0.8rem;
}
</style>
