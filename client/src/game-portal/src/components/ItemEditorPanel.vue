<template>
  <div class="item-editor-panel">
    <aside class="item-editor-sidebar">
      <div class="sidebar-actions">
        <UiButton size="sm" @click="newItem">New Item</UiButton>
        <input v-model="search" type="text" placeholder="Search items…" aria-label="Search items" />
      </div>
      <div class="sidebar-list">
        <div v-for="group in groupedItems" :key="group.tier" class="sidebar-group">
          <div class="sidebar-group__label" :style="{ color: TIER_COLORS[group.tier] }">{{ group.tier }}</div>
          <button
            v-for="d in group.items"
            :key="d.id"
            type="button"
            class="sidebar-item"
            :class="{ 'sidebar-item--selected': selectedId === d.id }"
            @click="selectItem(d.id)"
          >
            <img :src="getItemImageSourceUrl(d.iconKey)" class="sidebar-item__icon" alt="" />
            <span class="sidebar-item__name" :style="{ color: TIER_COLORS[d.tier] }">{{ d.displayName }}</span>
            <!-- Dev-build quirk: the writable catalog dir mirrors the embed
                 source, so `overridden` reports true for every item in dev
                 builds — this dot is expected to show on all items locally. -->
            <span v-if="d.overridden" class="sidebar-item__overridden" title="Overridden from catalog default">●</span>
          </button>
        </div>
        <p v-if="groupedItems.length === 0" class="sidebar-empty">No items match.</p>
      </div>
    </aside>

    <section v-if="form" class="item-editor-main">
      <!-- Section 1: Identity -->
      <section class="editor-section" :class="{ 'editor-section--open': openSection === 'identity' }">
        <button
          class="editor-section__summary"
          type="button"
          :aria-expanded="openSection === 'identity'"
          @click="toggleSection('identity')"
        >
          Identity
        </button>
        <div v-if="openSection === 'identity'" class="editor-section__body">
          <div class="control-group">
            <label for="ie-id">ID <span class="field-hint">(lowercase, digits, underscores; locked after save)</span></label>
            <input id="ie-id" v-model.trim="form.id" type="text" :disabled="!form.isNew" />
            <!-- Dev-build quirk: see sidebar dot comment — every item reports
                 overridden in dev, so this badge is expected to always show
                 for a selected non-new item locally. -->
            <span v-if="!form.isNew && selectedOverridden" class="field-hint">Overridden from catalog default</span>
          </div>
          <div class="control-group">
            <label for="ie-display-name">Display Name</label>
            <input id="ie-display-name" v-model.trim="form.displayName" type="text" />
          </div>
          <div class="control-group">
            <label for="ie-description">Description</label>
            <textarea id="ie-description" v-model.trim="form.description" rows="3"></textarea>
          </div>
          <div class="control-group">
            <label for="ie-kind">Kind</label>
            <select id="ie-kind" v-model="form.kind" @change="onKindChanged">
              <option value="equipment">Equipment</option>
              <option value="consumable">Consumable</option>
            </select>
          </div>
          <div class="control-group">
            <label for="ie-tier">Tier</label>
            <select id="ie-tier" v-model="form.tier">
              <option v-for="t in TIER_OPTIONS" :key="t" :value="t">{{ t }}</option>
            </select>
          </div>
          <div class="control-group">
            <label for="ie-category">Category</label>
            <select id="ie-category" v-model="form.category">
              <option v-for="c in CATEGORY_OPTIONS" :key="c" :value="c">{{ c }}</option>
            </select>
          </div>
          <div class="control-group">
            <label for="ie-slot-kind">Slot Kind</label>
            <select id="ie-slot-kind" v-model="form.slotKind">
              <option v-for="s in SLOT_KIND_OPTIONS" :key="s" :value="s">{{ s }}</option>
            </select>
          </div>
          <div class="control-group">
            <label for="ie-allowed-unit-types">Allowed unit types <span class="field-hint">(empty = all units)</span></label>
            <input
              id="ie-allowed-unit-types"
              :value="form.allowedUnitTypes.join(', ')"
              type="text"
              @change="onAllowedUnitTypesChanged"
            />
          </div>
        </div>
      </section>

      <!-- Section 2: Icon -->
      <section class="editor-section" :class="{ 'editor-section--open': openSection === 'icon' }">
        <button
          class="editor-section__summary"
          type="button"
          :aria-expanded="openSection === 'icon'"
          @click="toggleSection('icon')"
        >
          Icon
        </button>
        <div v-if="openSection === 'icon'" class="editor-section__body">
          <div class="icon-preview-row">
            <img :src="getItemImageSourceUrl(form.iconKey || form.id)" class="icon-preview" alt="" />
            <div class="icon-preview-actions">
              <UiButton size="sm" @click="galleryOpen = true">Choose from gallery</UiButton>
              <div class="control-group">
                <label for="ie-icon-upload">Upload custom icon <span class="field-hint">(PNG)</span></label>
                <input id="ie-icon-upload" type="file" accept="image/png" @change="onIconFileChosen" />
              </div>
            </div>
          </div>

          <div v-if="galleryOpen" class="icon-gallery-overlay">
            <div class="icon-gallery">
              <div class="icon-gallery__header">
                <span>Choose an icon</span>
                <UiButton size="sm" @click="galleryOpen = false">Close</UiButton>
              </div>
              <div class="icon-gallery__filter">
                <span class="icon-gallery__filter-label">Groups</span>
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
                <span class="icon-gallery__filter-actions">
                  <button type="button" class="icon-gallery__chip" @click="setAllGroups(true)">All</button>
                  <button type="button" class="icon-gallery__chip" @click="setAllGroups(false)">None</button>
                </span>
              </div>
              <div v-if="galleryKeys.length" class="icon-gallery__grid">
                <button
                  v-for="key in galleryKeys"
                  :key="key"
                  type="button"
                  class="icon-gallery__item"
                  @click="pickGalleryIcon(key)"
                >
                  <img :src="getItemImageSourceUrl(key)" alt="" />
                  <span>{{ key }}</span>
                </button>
              </div>
              <p v-else class="icon-gallery__empty">No icon groups selected.</p>
            </div>
          </div>
        </div>
      </section>

      <!-- Consumable (only for kind === 'consumable') -->
      <section
        v-if="form.kind === 'consumable'"
        class="editor-section"
        :class="{ 'editor-section--open': openSection === 'consumable' }"
      >
        <button
          class="editor-section__summary"
          type="button"
          :aria-expanded="openSection === 'consumable'"
          @click="toggleSection('consumable')"
        >
          Consumable
        </button>
        <div v-if="openSection === 'consumable'" class="editor-section__body">
          <div class="control-group">
            <label for="ie-consumable-type">Effect Type</label>
            <select id="ie-consumable-type" v-model="form.consumable.type">
              <option v-for="t in CONSUMABLE_TYPES" :key="t.value" :value="t.value">{{ t.label }}</option>
            </select>
          </div>
          <div class="control-group">
            <label for="ie-consumable-amount">Amount <span class="field-hint">(HP restored / XP granted)</span></label>
            <input id="ie-consumable-amount" v-model.number="form.consumable.amount" type="number" min="0" />
          </div>
          <div class="control-group">
            <label for="ie-consumable-range">AoE Range <span class="field-hint">(world units; 0 = default 100)</span></label>
            <input id="ie-consumable-range" v-model.number="form.consumable.range" type="number" min="0" />
          </div>
          <div class="control-group control-group--checkbox">
            <label for="ie-consumable-split">
              <input id="ie-consumable-split" v-model="form.consumable.split" type="checkbox" />
              Split amount across affected units <span class="field-hint">(unchecked = full amount each)</span>
            </label>
          </div>
          <div class="control-group">
            <label for="ie-consumable-duration">Duration (s) <span class="field-hint">(0 = instant)</span></label>
            <input id="ie-consumable-duration" v-model.number="form.consumable.durationSeconds" type="number" min="0" />
          </div>
          <div class="control-group">
            <label for="ie-max-stacks">Max Stacks <span class="field-hint">(per inventory slot; 0/1 = single)</span></label>
            <input id="ie-max-stacks" v-model.number="form.maxStacks" type="number" min="0" />
          </div>
        </div>
      </section>

      <!-- Section 3: Stats (equipment only) -->
      <section v-if="form.kind === 'equipment'" class="editor-section" :class="{ 'editor-section--open': openSection === 'stats' }">
        <button
          class="editor-section__summary"
          type="button"
          :aria-expanded="openSection === 'stats'"
          @click="toggleSection('stats')"
        >
          Stats
        </button>
        <div v-if="openSection === 'stats'" class="editor-section__body">
          <div v-for="f in FLAT_MOD_FIELDS" :key="f.key" class="control-group">
            <label :for="`ie-mod-${f.key}`">{{ f.label }}</label>
            <input :id="`ie-mod-${f.key}`" v-model.number="form.mods[f.key]" type="number" />
          </div>
          <div class="control-group">
            <label for="ie-mod-dodge">Dodge Chance % <span class="field-hint">(0-99)</span></label>
            <input id="ie-mod-dodge" v-model.number="form.mods.dodgePct" type="number" min="0" max="99" />
          </div>
          <div class="control-group">
            <label for="ie-mod-block">Block Chance % <span class="field-hint">(0-99)</span></label>
            <input id="ie-mod-block" v-model.number="form.mods.blockPct" type="number" min="0" max="99" />
          </div>
        </div>
      </section>

      <!-- Section 4: Elemental (equipment only) -->
      <section v-if="form.kind === 'equipment'" class="editor-section" :class="{ 'editor-section--open': openSection === 'elemental' }">
        <button
          class="editor-section__summary"
          type="button"
          :aria-expanded="openSection === 'elemental'"
          @click="toggleSection('elemental')"
        >
          Elemental
        </button>
        <div v-if="openSection === 'elemental'" class="editor-section__body">
          <div v-for="(row, idx) in form.elemental" :key="idx" class="elemental-row">
            <select v-model="row.type">
              <option v-for="t in ELEMENTAL_TYPES" :key="t" :value="t">{{ t }}</option>
            </select>
            <input v-model.number="row.amount" type="number" />
            <UiButton size="sm" @click="form.elemental.splice(idx, 1)">Remove</UiButton>
          </div>
          <UiButton size="sm" @click="form.elemental.push({ type: 'fire', amount: 5 })">Add elemental damage</UiButton>
        </div>
      </section>

      <!-- Section 5: Procs (equipment only) -->
      <section v-if="form.kind === 'equipment'" class="editor-section" :class="{ 'editor-section--open': openSection === 'procs' }">
        <button
          class="editor-section__summary"
          type="button"
          :aria-expanded="openSection === 'procs'"
          @click="toggleSection('procs')"
        >
          Procs
        </button>
        <div v-if="openSection === 'procs'" class="editor-section__body">
          <div v-for="ps in procSections" :key="ps.key" class="proc-block">
            <div class="control-group control-group--checkbox">
              <label :for="`ie-proc-${ps.key}-enabled`">
                <input :id="`ie-proc-${ps.key}-enabled`" v-model="ps.proc.enabled" type="checkbox" />
                {{ ps.label }}
              </label>
            </div>

            <template v-if="ps.proc.enabled">
              <div class="control-group">
                <label :for="`ie-proc-${ps.key}-effect`">Effect</label>
                <select :id="`ie-proc-${ps.key}-effect`" v-model="ps.proc.effect">
                  <option value="" disabled>Select an effect…</option>
                  <option v-for="p in procEffects" :key="p.id" :value="p.id">
                    {{ p.id }} — {{ p.damage }} {{ p.damageType }}
                  </option>
                </select>
              </div>
              <div class="control-group">
                <label :for="`ie-proc-${ps.key}-chance`">Chance % <span class="field-hint">(1-100)</span></label>
                <input
                  :id="`ie-proc-${ps.key}-chance`"
                  v-model.number="ps.proc.chancePct"
                  type="number"
                  min="1"
                  max="100"
                />
              </div>

              <fieldset class="proc-overrides" :class="{ 'proc-overrides--open': overridesOpen[ps.key] }">
                <button type="button" class="proc-overrides__toggle" @click="overridesOpen[ps.key] = !overridesOpen[ps.key]">
                  Overrides <span class="field-hint">(blank = inherit from effect)</span>
                </button>
                <div v-if="overridesOpen[ps.key]" class="proc-overrides__body">
                  <div v-for="f in PROC_OVERRIDE_FIELDS" :key="f.key" class="control-group">
                    <label :for="`ie-proc-${ps.key}-${f.key}`">{{ f.label }}</label>
                    <input
                      :id="`ie-proc-${ps.key}-${f.key}`"
                      :value="ps.proc[f.key] ?? ''"
                      :placeholder="effectPlaceholder(ps.proc.effect, f.key)"
                      type="number"
                      @input="bindNullable(ps.proc, f.key, $event)"
                    />
                  </div>
                </div>
              </fieldset>
            </template>
          </div>
        </div>
      </section>

      <!-- Section 6: Cost & Availability -->
      <section class="editor-section" :class="{ 'editor-section--open': openSection === 'availability' }">
        <button
          class="editor-section__summary"
          type="button"
          :aria-expanded="openSection === 'availability'"
          @click="toggleSection('availability')"
        >
          Cost &amp; Availability
        </button>
        <div v-if="openSection === 'availability'" class="editor-section__body">
          <div class="control-group">
            <label for="ie-cost-gold">Cost (Gold)</label>
            <input id="ie-cost-gold" v-model.number="form.costGold" type="number" min="0" />
          </div>
          <div class="control-group control-group--checkbox">
            <label for="ie-avail-marketplace">
              <input id="ie-avail-marketplace" v-model="form.availability.marketplace" type="checkbox" />
              Marketplace
            </label>
          </div>
          <div class="control-group control-group--checkbox">
            <label for="ie-avail-wandering">
              <input id="ie-avail-wandering" v-model="form.availability.wanderingMerchant" type="checkbox" />
              Wandering Merchant
            </label>
          </div>
          <div class="control-group control-group--checkbox">
            <label for="ie-avail-loot">
              <input id="ie-avail-loot" v-model="form.availability.lootTable.enabled" type="checkbox" />
              Loot Table
            </label>
          </div>
          <div v-if="form.availability.lootTable.enabled" class="control-group">
            <label for="ie-avail-loot-weight">Weight <span class="field-hint">(share of the merchant roll, 1-90)</span></label>
            <input
              id="ie-avail-loot-weight"
              v-model.number="form.availability.lootTable.weight"
              type="number"
              min="1"
              max="90"
            />
          </div>
          <div class="control-group control-group--checkbox">
            <label for="ie-avail-recipe-list">
              <input
                id="ie-avail-recipe-list"
                v-model="form.availability.recipeList"
                type="checkbox"
                :disabled="!form.crafting.enabled"
              />
              Recipe List <span class="field-hint">(requires crafting)</span>
            </label>
          </div>
        </div>
      </section>

      <!-- Section 7: Crafting (equipment only) -->
      <section v-if="form.kind === 'equipment'" class="editor-section" :class="{ 'editor-section--open': openSection === 'crafting' }">
        <button
          class="editor-section__summary"
          type="button"
          :aria-expanded="openSection === 'crafting'"
          @click="toggleSection('crafting')"
        >
          Crafting
        </button>
        <div v-if="openSection === 'crafting'" class="editor-section__body">
          <div class="control-group control-group--checkbox">
            <label for="ie-crafting-enabled">
              <input id="ie-crafting-enabled" v-model="form.crafting.enabled" type="checkbox" />
              Craftable
            </label>
          </div>
          <template v-if="form.crafting.enabled">
            <div v-for="(_input, idx) in form.crafting.inputs" :key="idx" class="crafting-input-row">
              <div class="control-group">
                <label :for="`ie-crafting-input-${idx}`">Input {{ idx + 1 }}</label>
                <select :id="`ie-crafting-input-${idx}`" v-model="form.crafting.inputs[idx]">
                  <option value="" disabled>Select an item…</option>
                  <option v-for="d in allEquipmentItems" :key="d.id" :value="d.id">{{ d.displayName }} ({{ d.id }})</option>
                </select>
              </div>
              <UiButton
                size="sm"
                :disabled="form.crafting.inputs.length <= 2"
                @click="form.crafting.inputs.splice(idx, 1)"
              >
                Remove
              </UiButton>
            </div>
            <UiButton size="sm" @click="form.crafting.inputs.push('')">Add ingredient</UiButton>
            <div class="control-group">
              <label for="ie-crafting-cost">Craft Cost (Gold)</label>
              <input id="ie-crafting-cost" v-model.number="form.crafting.costGold" type="number" min="0" />
            </div>
          </template>
        </div>
      </section>

      <div class="editor-actions">
        <UiButton :disabled="saving || !form.id" @click="save">{{ saving ? 'Saving…' : 'Save' }}</UiButton>
        <UiButton v-if="!form.isNew" size="sm" @click="removeOrReset">Delete / Reset</UiButton>
        <span v-if="saveError" class="save-error" role="alert">{{ saveError }}</span>
        <span v-else-if="saveOk" class="save-ok">Saved ✓</span>
        <span v-else-if="deleteStatus" class="save-ok">{{ deleteStatus }}</span>
      </div>
    </section>
    <section v-else class="item-editor-main item-editor-main--empty">
      <p v-if="loadError" role="alert">{{ loadError }}</p>
      <p v-else>Select an item or create a new one.</p>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import UiButton from '@/components/ui/UiButton.vue'
import { fetchItemDefs, fetchRecipeDefs } from '@/game/maps/catalog'
import type { ItemDef, ItemTier } from '@/game/maps/itemDefs'
import { EditorValidationError, deleteEditorItem, fetchItemAvailability, fetchProcEffectDefs, uploadItemIcon, saveEditorItem } from '@/game/items/itemEditorApi'
import type { ProcEffectDef } from '@/game/items/itemEditorApi'
import { createBlankForm, formFromDef, saveRequestFromForm } from '@/game/items/itemEditorForm'
import type { ItemEditorForm, ProcForm } from '@/game/items/itemEditorForm'
import { getItemImageSourceUrl, listIconGroups } from '@/game/rendering/itemAssets'
import { TIER_COLORS } from '@/game/items/itemRules'

const items = ref<ItemDef[]>([])            // full catalog, refreshed after saves
const recipesByOutput = ref(new Map<string, { inputs: string[]; costGold: number }>())
const procEffects = ref<ProcEffectDef[]>([])
const loadError = ref('')
const search = ref('')
const selectedId = ref('')                  // '' = nothing selected
const form = ref<ItemEditorForm | null>(null)
const openSection = ref('identity')         // accordion state
const saving = ref(false)
const saveError = ref('')                   // EditorValidationError message shown beside Save
const saveOk = ref(false)
const deleteStatus = ref('')                // transient feedback after removeOrReset
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
const overridesOpen = reactive<{ onHit: boolean; onStruck: boolean }>({ onHit: false, onStruck: false })

const TIER_OPTIONS: ItemTier[] = ['common', 'uncommon', 'rare', 'epic', 'legendary']
const CATEGORY_OPTIONS = ['Weapon', 'Armor', 'Shield', 'Accessory', 'Consumable']
const SLOT_KIND_OPTIONS = ['any', 'weapon', 'armor', 'accessory']
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
type ProcNullableKey = Exclude<keyof ProcForm, 'enabled' | 'effect' | 'chancePct'>
const PROC_OVERRIDE_FIELDS: { key: ProcNullableKey; label: string }[] = [
  { key: 'damage', label: 'Damage' },
  { key: 'projectileScale', label: 'Projectile Scale' },
  { key: 'bounceCount', label: 'Bounce Count' },
  { key: 'bounceRange', label: 'Bounce Range' },
  { key: 'bounceDamageFalloff', label: 'Bounce Damage Falloff' },
  { key: 'slowMultiplier', label: 'Slow Multiplier' },
  { key: 'slowDurationSeconds', label: 'Slow Duration (s)' },
  { key: 'burnDamagePerSecond', label: 'Burn Damage/s' },
  { key: 'burnDurationSeconds', label: 'Burn Duration (s)' },
]

// Sidebar list: every item (equipment AND consumable), search-filtered.
const filteredItems = computed(() =>
  items.value.filter((d) =>
    search.value === '' || d.id.includes(search.value.toLowerCase()) || d.displayName.toLowerCase().includes(search.value.toLowerCase())))

// Crafting inputs stay equipment-only and are never constrained by the
// sidebar search (a leftover search term must not truncate the dropdown).
const allEquipmentItems = computed(() => items.value.filter((d) => d.kind === 'equipment'))

// group by tier for the sidebar; TIER_COLORS drives the badge color.
const groupedItems = computed(() => {
  const groups = new Map<ItemTier, ItemDef[]>()
  for (const t of TIER_OPTIONS) groups.set(t, [])
  for (const d of filteredItems.value) {
    const list = groups.get(d.tier)
    if (list) list.push(d)
  }
  return TIER_OPTIONS
    .map((tier) => ({ tier, items: groups.get(tier) ?? [] }))
    .filter((g) => g.items.length > 0)
})

// selectedOverridden: items.find(selectedId)?.overridden (needs `overridden?:
// boolean` on the client ItemDef type). In dev builds every item reports
// overridden (writable dir == embed source) — see sidebar dot comment above.
const selectedOverridden = computed(() => items.value.find((d) => d.id === selectedId.value)?.overridden ?? false)

// Two proc sub-blocks driven from the same markup — form.onHit / form.onStruck
// are reactive objects nested inside the `form` ref, so pulling references out
// into this array preserves reactivity (no destructuring of primitives).
const procSections = computed(() => {
  if (!form.value) return []
  return [
    { key: 'onHit' as const, label: 'On hit', proc: form.value.onHit },
    { key: 'onStruck' as const, label: 'When struck', proc: form.value.onStruck },
  ]
})

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

async function reloadCatalog() {
  const [defs, recipes] = await Promise.all([fetchItemDefs(), fetchRecipeDefs().catch(() => [])])
  items.value = defs
  const map = new Map<string, { inputs: string[]; costGold: number }>()
  for (const r of recipes) map.set(r.output, { inputs: r.inputs, costGold: r.costGold })
  recipesByOutput.value = map
}

onMounted(async () => {
  try {
    await reloadCatalog()
    procEffects.value = await fetchProcEffectDefs()
  } catch (err) {
    loadError.value = err instanceof Error ? err.message : String(err)
  }
})

async function selectItem(id: string) {
  const def = items.value.find((d) => d.id === id)
  if (!def) return
  selectedId.value = id
  saveError.value = ''
  saveOk.value = false
  deleteStatus.value = ''
  const availability = await fetchItemAvailability(id).catch(() => ({
    marketplace: false, wanderingMerchant: false, lootTable: { enabled: false, weight: 10 }, recipeList: false }))
  form.value = formFromDef(def, availability, recipesByOutput.value.get(id) ?? null)
}

function newItem() {
  selectedId.value = ''
  saveError.value = ''
  saveOk.value = false
  deleteStatus.value = ''
  form.value = createBlankForm()
}

async function save() {
  if (!form.value) return
  saving.value = true
  saveError.value = ''
  saveOk.value = false
  deleteStatus.value = ''
  try {
    await saveEditorItem(saveRequestFromForm(form.value))
    saveOk.value = true
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

async function removeOrReset() {
  if (!form.value || form.value.isNew) return
  saveError.value = ''
  saveOk.value = false
  deleteStatus.value = ''
  try {
    const status = await deleteEditorItem(form.value.id)
    await reloadCatalog()
    if (status === 'deleted') {
      newItem()
      deleteStatus.value = 'Item deleted.'
    } else {
      await selectItem(form.value.id) // reset: reload the embedded version
      deleteStatus.value = 'Reset to catalog default.'
    }
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
  try {
    await uploadItemIcon(form.value.id, file)
    form.value.iconKey = form.value.id // server forces iconKey to the id
    await reloadCatalog()
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

function toggleSection(key: string) {
  openSection.value = openSection.value === key ? '' : key
}

// When the item's kind flips, keep category and the open accordion coherent:
// consumables default to the Consumable category (drives the consumables/
// catalog subdir + merchant_potions loot bucket) and open the Consumable
// section; switching back to equipment clears the Consumable category.
const EQUIPMENT_ONLY_SECTIONS = ['stats', 'elemental', 'procs', 'crafting']
function onKindChanged() {
  if (!form.value) return
  if (form.value.kind === 'consumable') {
    form.value.category = 'Consumable'
    if (EQUIPMENT_ONLY_SECTIONS.includes(openSection.value)) openSection.value = 'consumable'
  } else if (form.value.category === 'Consumable') {
    form.value.category = 'Weapon'
    if (openSection.value === 'consumable') openSection.value = 'identity'
  }
}

// Comma-separated text input <-> string[] binding for allowedUnitTypes —
// mirrors the nullable-override idiom (:value + @change) instead of v-model
// since the model is an array, not a scalar.
function onAllowedUnitTypesChanged(ev: Event) {
  if (!form.value) return
  const raw = (ev.target as HTMLInputElement).value
  form.value.allowedUnitTypes = raw.split(',').map((s) => s.trim()).filter(Boolean)
}
</script>

<style scoped>
.item-editor-panel {
  display: flex;
  width: 100%;
  height: 100%;
  min-height: 0;
  min-width: 0;
  gap: 12px;
  padding: 16px;
  box-sizing: border-box;
}

.item-editor-sidebar {
  flex: 0 0 280px;
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

.item-editor-main {
  flex: 1;
  min-width: 0;
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

.item-editor-main--empty {
  align-items: center;
  justify-content: center;
  color: rgba(226, 232, 240, 0.72);
}

.sidebar-actions {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.sidebar-actions input {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
}

.sidebar-list {
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.sidebar-group__label {
  font-size: 0.7rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  padding: 6px 4px 2px;
}

.sidebar-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  border: 1px solid transparent;
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.6);
  padding: 6px 8px;
  text-align: left;
}

.sidebar-item--selected {
  border-color: rgba(215, 187, 132, 0.6);
  background: rgba(30, 41, 59, 0.9);
}

.sidebar-item__icon {
  width: 24px;
  height: 24px;
  image-rendering: pixelated;
  flex: 0 0 auto;
}

.sidebar-item__name {
  flex: 1;
  font-size: 0.78rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sidebar-item__overridden {
  color: #86efac;
  font-size: 0.7rem;
}

.sidebar-empty {
  color: rgba(226, 232, 240, 0.6);
  font-size: 0.78rem;
  padding: 8px 4px;
}

.icon-preview-row {
  display: flex;
  gap: 12px;
  align-items: flex-start;
}

.icon-preview {
  width: 64px;
  height: 64px;
  image-rendering: pixelated;
  border: 1px solid rgba(148, 163, 184, 0.24);
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.92);
}

.icon-preview-actions {
  display: flex;
  flex-direction: column;
  gap: 8px;
  flex: 1;
}

.icon-gallery-overlay {
  position: fixed;
  inset: 0;
  background: rgba(3, 8, 14, 0.72);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 50;
}

.icon-gallery {
  width: min(640px, 90vw);
  max-height: 80vh;
  overflow-y: auto;
  background: rgba(8, 14, 24, 0.96);
  border: 1px solid rgba(148, 163, 184, 0.24);
  border-radius: 12px;
  padding: 12px;
}

.icon-gallery__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
  color: #f8fafc;
  font-weight: 700;
}

.icon-gallery__filter {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  margin-bottom: 10px;
  padding-bottom: 10px;
  border-bottom: 1px solid rgba(148, 163, 184, 0.16);
}

.icon-gallery__filter-label {
  font-size: 0.7rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: rgba(148, 163, 184, 0.9);
  margin-right: 2px;
}

.icon-gallery__filter-actions {
  display: inline-flex;
  gap: 6px;
  margin-left: auto;
}

.icon-gallery__chip {
  font-size: 0.68rem;
  color: rgba(226, 232, 240, 0.82);
  background: rgba(15, 23, 42, 0.72);
  border: 1px solid rgba(148, 163, 184, 0.24);
  border-radius: 999px;
  padding: 3px 9px;
}

.icon-gallery__chip--on {
  color: #f8fafc;
  background: rgba(56, 189, 248, 0.22);
  border-color: rgba(56, 189, 248, 0.55);
}

.icon-gallery__chip-count {
  opacity: 0.6;
  font-variant-numeric: tabular-nums;
}

.icon-gallery__empty {
  color: rgba(148, 163, 184, 0.9);
  font-size: 0.8rem;
  text-align: center;
  padding: 24px 0;
}

.icon-gallery__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(72px, 1fr));
  gap: 8px;
}

.icon-gallery__item {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.72);
  padding: 6px;
}

.icon-gallery__item img {
  width: 40px;
  height: 40px;
  image-rendering: pixelated;
}

.icon-gallery__item span {
  font-size: 0.62rem;
  color: rgba(226, 232, 240, 0.82);
  text-align: center;
  word-break: break-all;
}

.elemental-row {
  display: flex;
  gap: 8px;
  align-items: center;
}

.elemental-row select,
.elemental-row input {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
}

.crafting-input-row {
  display: flex;
  gap: 8px;
  align-items: flex-end;
}

.crafting-input-row .control-group {
  flex: 1;
}

.proc-block {
  border: 1px solid rgba(148, 163, 184, 0.14);
  border-radius: 10px;
  padding: 8px;
  display: grid;
  gap: 8px;
}

.proc-overrides {
  border: 1px solid rgba(148, 163, 184, 0.16);
  border-radius: 8px;
  padding: 0;
  margin: 0;
}

.proc-overrides__toggle {
  width: 100%;
  border: 0;
  background: rgba(15, 23, 42, 0.6);
  color: rgba(226, 232, 240, 0.86);
  text-align: left;
  padding: 6px 8px;
  font-size: 0.74rem;
  font-weight: 700;
}

.proc-overrides__body {
  display: grid;
  gap: 8px;
  padding: 8px;
}

.editor-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: auto;
  padding-top: 8px;
}

.save-error {
  color: #fca5a5;
  font-size: 0.78rem;
}

.save-ok {
  color: #86efac;
  font-size: 0.78rem;
}

/* editor-section / control-group idiom, copied locally from MapEditorPanel.vue
   (scoped styles aren't shared across SFCs — duplication accepted per plan). */
.editor-section {
  border: 1px solid rgba(148, 163, 184, 0.18);
  border-radius: 12px;
  background: rgba(8, 14, 24, 0.55);
  overflow: clip;
  flex: 0 0 auto;
}

.editor-section--open {
  background: rgba(8, 14, 24, 0.72);
}

.editor-section__summary {
  width: 100%;
  border: 0;
  padding: 10px 12px;
  font-size: 0.78rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #f8fafc;
  text-align: left;
  background: linear-gradient(180deg, rgba(25, 35, 52, 0.92), rgba(14, 22, 36, 0.94));
}

.editor-section__summary::after {
  content: '+';
  float: right;
  color: #d7bb84;
}

.editor-section--open .editor-section__summary::after {
  content: '-';
}

.editor-section__body {
  display: grid;
  gap: 8px;
  padding: 10px;
}

.control-group {
  display: grid;
  gap: 4px;
}

.control-group--checkbox label {
  display: flex;
  align-items: center;
  gap: 6px;
}

.control-group label {
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.75rem;
}

.control-group input,
.control-group select,
.control-group textarea {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
}

.field-hint {
  font-weight: 400;
  opacity: 0.65;
  text-transform: none;
  letter-spacing: 0;
}
</style>
