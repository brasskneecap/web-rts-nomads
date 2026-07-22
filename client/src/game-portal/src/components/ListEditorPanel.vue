<template>
  <EditorShell class="list-editor">
    <template #sidebar>
      <EditorSidebar
        title="Lists"
        new-label="Add New List"
        :groups="sidebarGroups"
        :selected-id="selectedId"
        :search="search"
        search-placeholder="Search lists…"
        empty-text="No lists match."
        @update:search="search = $event"
        @select="selectList"
        @new="newList"
        @duplicate="duplicateList"
      />
    </template>

    <template #main>
      <template v-if="form">
        <EditorHeader
          :title="form.name || 'New List'"
          :file-path="filePath"
          :id="form.id"
          :id-editable="form.isNew"
          id-input-id="le-id"
          :saving="saving"
          :save-disabled="saving || !canSave"
          :saved-label="savedLabel"
          :error="saveError"
          remove-label="Delete"
          @update:id="onIdInput"
          @save="save"
          @remove="remove"
        />

        <GameScrollArea class="list-editor__scroll">
          <div class="list-editor__grid">
            <SectionCard title="Identity" :index="1" class="list-editor__identity">
              <EditorField label="Name" for-id="le-name">
                <input id="le-name" v-model.trim="form.name" type="text" />
              </EditorField>
              <!-- A list does not declare what it is FOR. Say so here, because
                   it is the one thing an author is most likely to assume wrong. -->
              <p class="list-editor__note">
                A list is just a set of items. What it <em>means</em> is decided by whatever you
                bind it to:
              </p>
              <ul class="list-editor__uses">
                <li><strong>Shop</strong> — sells the items, at each item's cost.</li>
                <li><strong>Recipe Shop</strong> — sells their <em>recipes</em>, at each item's recipe cost.</li>
                <li><strong>Artificer</strong> — crafts them, at each item's craft cost, limited to recipes you've learned.</li>
                <li><strong>Camp</strong> — drops one of them when cleared.</li>
              </ul>
            </SectionCard>

            <SectionCard title="List" :index="3" class="list-editor__list">
              <EditorField label="Weighting" for-id="le-mode">
                <select id="le-mode" :value="form.weighted ? 'weighted' : 'uniform'" @change="setMode(($event.target as HTMLSelectElement).value)">
                  <option value="uniform">Uniform — every item equally likely</option>
                  <option value="weighted">Weighted — each item owns a range of the roll</option>
                </select>
              </EditorField>
              <EditorField v-if="form.weighted" label="Max Roll" hint="(items must cover 1..this)" for-id="le-maxroll">
                <input id="le-maxroll" v-model.number="form.maxRoll" type="number" min="1" />
              </EditorField>

              <!-- Drop zone: drag an icon from the Items browser onto this
                   area to append it. The whole members block is the target so
                   there is generous room to aim at, even when the list is
                   empty. -->
              <div
                class="list-editor__drop"
                :class="{ 'is-drag-over': dragOver }"
                @dragover.prevent
                @dragenter.prevent="dragOver = true"
                @dragleave="onDragLeave"
                @drop.prevent="onDrop"
              >
              <p v-if="form.members.length === 0" class="list-editor__rows-empty">
                Drag items from the Items browser — or click one — to add them.
              </p>
              <div v-else class="list-editor__rows">
                <!-- The item is chosen from the Items browser now, so each row
                     just SHOWS what it holds: its icon and name. -->
                <div v-for="(m, idx) in form.members" :key="idx" class="list-editor__row">
                  <span
                    class="list-editor__row-item"
                    @mouseenter="onRowEnter($event, m.item)"
                    @mousemove="onRowMove"
                    @mouseleave="onItemLeave"
                  >
                    <img
                      v-if="memberIcon(m.item)"
                      :src="memberIcon(m.item) ?? ''"
                      :alt="memberName(m.item)"
                      class="list-editor__row-icon"
                    />
                    <span v-else class="list-editor__row-icon list-editor__row-icon--missing">?</span>
                    <span class="list-editor__row-name">{{ memberName(m.item) }}</span>
                  </span>
                  <template v-if="form.weighted">
                    <input
                      v-model.number="m.min" type="number" min="1" class="list-editor__num"
                      :aria-label="`${memberName(m.item)} min`"
                    />
                    <span>–</span>
                    <input
                      v-model.number="m.max" type="number" min="1" class="list-editor__num"
                      :aria-label="`${memberName(m.item)} max`"
                    />
                  </template>
                  <button
                    type="button"
                    class="list-editor__row-del"
                    :aria-label="`Remove ${memberName(m.item)}`"
                    @click="form.members.splice(idx, 1)"
                  >✕</button>
                </div>
              </div>
              <p v-if="form.members.length" class="list-editor__drop-hint">
                Drag item here to add to list
              </p>
              </div>
            </SectionCard>

            <!-- A visual palette of every item. Type to narrow, filter by kind /
                 tier / craftable, and click an icon to add it to the list. The
                 hover tooltip is the SAME one the shop and vault use. -->
            <SectionCard title="Items" :index="2" class="list-editor__browser">
              <div class="list-editor__browser-controls">
                <input
                  v-model.trim="itemSearch"
                  type="text"
                  class="list-editor__browser-search"
                  placeholder="Search items…"
                  aria-label="Search items"
                />
                <div class="list-editor__browser-filters">
                  <select v-model="filterKind" aria-label="Filter by kind">
                    <option value="all">All kinds</option>
                    <option value="equipment">Equipment</option>
                    <option value="consumable">Consumable</option>
                  </select>
                  <select v-model="filterTier" aria-label="Filter by tier">
                    <option value="all">All tiers</option>
                    <option v-for="t in TIERS" :key="t" :value="t">{{ capitalize(t) }}</option>
                  </select>
                  <!-- category is a freeform, optional label, so this filter
                       only appears once some items actually declare one. -->
                  <select v-if="categories.length" v-model="filterCategory" aria-label="Filter by category">
                    <option value="all">All categories</option>
                    <option v-for="c in categories" :key="c" :value="c">{{ c }}</option>
                  </select>
                  <label class="list-editor__browser-check">
                    <input v-model="filterCraftable" type="checkbox" />
                    Craftable
                  </label>
                </div>
              </div>

              <div v-if="browserItems.length" class="list-editor__browser-grid">
                <button
                  v-for="d in browserItems"
                  :key="d.id"
                  type="button"
                  class="list-editor__browser-item"
                  :aria-label="`Add ${d.displayName}`"
                  draggable="true"
                  @dragstart="onItemDragStart($event, d)"
                  @mouseenter="onItemEnter($event, d)"
                  @mouseleave="onItemLeave"
                  @focus="onItemEnter($event, d)"
                  @blur="onItemLeave"
                  @click="addItemToList(d)"
                >
                  <img :src="itemIconUrl(d)" :alt="d.displayName" />
                </button>
              </div>
              <p v-else class="list-editor__browser-empty">No items match.</p>

              <!-- Teleports to <body>, so it floats above the scroll area. -->
              <ItemHoverTooltip :item="hoveredItem" :anchor="hoverAnchor" />
            </SectionCard>

            <SectionCard title="Validation" class="list-editor__validation">
              <ValidationChecklist :checks="checks" />
              <!-- Not a blocking check: the same list can be nonsense on an
                   Artificer and exactly right as a loot pool. Say what will
                   happen and let the author decide. -->
              <p v-if="warning" class="list-editor__warning" role="status">{{ warning }}</p>
              <span v-if="statusNote" class="list-editor__status-note">{{ statusNote }}</span>
            </SectionCard>
          </div>
        </GameScrollArea>
      </template>

      <div v-else class="list-editor__empty">
        <p v-if="loadError" role="alert">{{ loadError }}</p>
        <p v-else>Select a list or create a new one.</p>
      </div>
    </template>
  </EditorShell>
</template>

<script setup lang="ts">
import { confirmDelete } from '@/components/editor/confirmDelete'
import { computed, onMounted, ref, watch } from 'vue'
import EditorShell from '@/components/editor/EditorShell.vue'
import EditorSidebar from '@/components/editor/EditorSidebar.vue'
import type { SidebarGroup } from '@/components/editor/EditorSidebar.vue'
import EditorHeader from '@/components/editor/EditorHeader.vue'
import EditorField from '@/components/editor/EditorField.vue'
import SectionCard from '@/components/editor/SectionCard.vue'
import ValidationChecklist from '@/components/editor/ValidationChecklist.vue'
import GameScrollArea from '@/components/ui/GameScrollArea.vue'
import ItemHoverTooltip from '@/components/ItemHoverTooltip.vue'
import type { ItemTooltipData } from '@/components/ItemHoverTooltip.vue'
import { fetchItemDefs, fetchLists } from '@/game/maps/catalog'
import type { ItemDef, ItemKind, ItemTier } from '@/game/maps/itemDefs'
import { getItemImageSourceUrl } from '@/game/rendering/itemAssets'
import { TIER_COLORS, buildItemTooltipBody } from '@/game/items/itemRules'
import type { ListDef } from '@/game/maps/listDefs'
import { isWeightedList } from '@/game/maps/listDefs'
import { EditorValidationError } from '@/game/items/itemEditorApi'
import { deleteEditorList, saveEditorList } from '@/game/items/listEditorApi'
import {
  nonCraftableWarning,
  validateListForm,
  type ListEditorForm,
  type ListMemberForm,
} from '@/game/items/listEditorValidation'

const lists = ref<ListDef[]>([])
const items = ref<ItemDef[]>([])
const selectedId = ref('')
const search = ref('')
const form = ref<ListEditorForm | null>(null)
const saving = ref(false)
const saveError = ref('')
const statusNote = ref('')
const loadError = ref('')
const savedLabel = ref('')

const itemOptions = computed(() =>
  [...items.value].sort((a, b) => a.displayName.localeCompare(b.displayName)),
)

// id → def, so a member row can render its item's icon and name. A row can
// outlive the item it names (the item was deleted), so both lookups tolerate a
// miss: no icon (a `?` placeholder) and the raw id as the name.
const itemById = computed(() => new Map(items.value.map((d) => [d.id, d])))

function memberIcon(id: string): string | null {
  const d = itemById.value.get(id)
  return d ? getItemImageSourceUrl(d.iconKey) : null
}

function memberName(id: string): string {
  return itemById.value.get(id)?.displayName || id || 'Unknown item'
}

// ─── Items browser ───────────────────────────────────────────────────────────
// A searchable/filterable icon palette of the whole catalog. It shares the same
// sorted set as the per-row selects (itemOptions); the filters and the text
// search only narrow what is shown, and clicking an icon appends that item.

const TIERS: ItemTier[] = ['common', 'uncommon', 'rare', 'epic', 'legendary']

const itemSearch = ref('')
const filterKind = ref<'all' | ItemKind>('all')
const filterTier = ref<'all' | ItemTier>('all')
const filterCategory = ref('all')
const filterCraftable = ref(false)

// The distinct, present categories — derived from the catalog rather than hard
// coded, since `category` is a freeform label an author can set to anything.
const categories = computed(() => {
  const set = new Set<string>()
  for (const d of items.value) if (d.category) set.add(d.category)
  return [...set].sort((a, b) => a.localeCompare(b))
})

const browserItems = computed(() => {
  const term = itemSearch.value.trim().toLowerCase()
  return itemOptions.value.filter((d) => {
    if (term && !d.displayName.toLowerCase().includes(term) && !d.id.includes(term)) return false
    if (filterKind.value !== 'all' && d.kind !== filterKind.value) return false
    if (filterTier.value !== 'all' && d.tier !== filterTier.value) return false
    if (filterCategory.value !== 'all' && d.category !== filterCategory.value) return false
    if (filterCraftable.value && !d.crafting) return false
    return true
  })
})

function itemIconUrl(d: ItemDef): string {
  return getItemImageSourceUrl(d.iconKey)
}

function capitalize(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1)
}

// Hover tooltip — the real in-game ItemHoverTooltip, fed the same content the
// shop/vault show so an item reads identically here.
const hoveredItem = ref<ItemTooltipData | null>(null)
const hoverAnchor = ref<DOMRect | null>(null)

function showItemTooltip(d: ItemDef, anchor: DOMRect) {
  hoverAnchor.value = anchor
  hoveredItem.value = {
    displayName: d.displayName,
    tier: d.tier,
    tierColor: TIER_COLORS[d.tier],
    body: buildItemTooltipBody(d),
  }
}

// A browser icon is small, so it anchors to itself (centered above the icon).
function onItemEnter(event: Event, d: ItemDef) {
  showItemTooltip(d, (event.currentTarget as HTMLElement).getBoundingClientRect())
}

function onItemLeave() {
  hoveredItem.value = null
}

// A list ROW is wide, so anchoring to its center would park the tooltip far
// from the pointer. Instead anchor a zero-size point at the cursor's x, just
// above the row's top edge, and update it as the pointer moves — so the tooltip
// tracks the mouse along the row rather than sitting in the middle.
function rowCursorAnchor(event: MouseEvent): DOMRect {
  const top = (event.currentTarget as HTMLElement).getBoundingClientRect().top
  return new DOMRect(event.clientX, top, 0, 0)
}

function onRowEnter(event: MouseEvent, id: string) {
  const d = itemById.value.get(id)
  if (d) showItemTooltip(d, rowCursorAnchor(event))
}

function onRowMove(event: MouseEvent) {
  if (hoveredItem.value) hoverAnchor.value = rowCursorAnchor(event)
}

// ─── Drag an item into the list ──────────────────────────────────────────────
// The browser icons are HTML5-draggable; the list's members block is the drop
// target. The payload is just the item id (text/plain), which the drop handler
// resolves back to a def and appends via the same addItemToList path as a click.

const dragOver = ref(false)

function onItemDragStart(event: DragEvent, d: ItemDef) {
  if (!event.dataTransfer) return
  event.dataTransfer.setData('text/plain', d.id)
  event.dataTransfer.effectAllowed = 'copy'
  // The tooltip would otherwise stick around under the drag image.
  hoveredItem.value = null
}

// dragleave also fires when moving between child elements — only clear the
// highlight when the pointer actually leaves the drop zone.
function onDragLeave(event: DragEvent) {
  const zone = event.currentTarget as HTMLElement
  if (!zone.contains(event.relatedTarget as Node | null)) dragOver.value = false
}

function onDrop(event: DragEvent) {
  dragOver.value = false
  const id = event.dataTransfer?.getData('text/plain')
  if (!id) return
  const d = items.value.find((x) => x.id === id)
  if (d) addItemToList(d)
}

// Clicking (or dropping) a browser icon appends the item to the list. In
// weighted mode the new row starts where the last one left off, so filling the
// die is a matter of setting the max, not doing arithmetic on both ends.
function addItemToList(d: ItemDef) {
  if (!form.value) return
  const members = form.value.members
  const m = blankMember()
  m.item = d.id
  if (form.value.weighted) {
    const last = members[members.length - 1]
    m.min = last ? last.max + 1 : 1
    m.max = Math.max(m.min, form.value.maxRoll)
  }
  members.push(m)
}

const sidebarGroups = computed<SidebarGroup[]>(() => {
  const term = search.value.trim().toLowerCase()
  const entries = lists.value
    .filter((l) => !term || l.name.toLowerCase().includes(term) || l.id.includes(term))
    .sort((a, b) => (a.name || a.id).localeCompare(b.name || b.id))
    .map((l) => ({ id: l.id, name: l.name || l.id }))
  return entries.length > 0 ? [{ label: 'Lists', entries }] : []
})

const filePath = computed(() =>
  form.value?.id ? `catalog/lists/${form.value.id}.json` : 'catalog/lists/',
)

const validationCtx = computed(() => ({
  knownListIds: new Set(lists.value.map((l) => l.id)),
  itemsById: new Map(items.value.map((d) => [d.id, d])),
}))

const checks = computed(() =>
  form.value ? validateListForm(form.value, validationCtx.value) : [],
)
const canSave = computed(() => checks.value.every((c) => c.ok))
const warning = computed(() =>
  form.value ? nonCraftableWarning(form.value, validationCtx.value) : '',
)

// ─── Form shape ↔ ListDef ────────────────────────────────────────────────────
// The form always carries member ROWS (item + range) plus a `weighted` flag, so
// toggling the mode never discards the item selection. Conversion to/from the
// wire ListDef happens only at load and save.

function blankMember(): ListMemberForm {
  return { item: '', min: 1, max: 1 }
}

function formFromDef(def: ListDef, isNew: boolean, name = def.name): ListEditorForm {
  const weighted = isWeightedList(def)
  const members: ListMemberForm[] = weighted
    ? (def.entries ?? []).map((e) => ({ item: e.item, min: e.min, max: e.max }))
    : (def.items ?? []).map((item) => ({ item, min: 1, max: 1 }))
  return {
    id: isNew ? '' : def.id,
    isNew,
    name,
    weighted,
    maxRoll: def.maxRoll ?? 100,
    members,
  }
}

function defFromForm(form: ListEditorForm): ListDef {
  const rows = form.members.filter((m) => m.item)
  if (form.weighted) {
    return {
      id: form.id,
      name: form.name,
      maxRoll: form.maxRoll,
      entries: rows.map((m) => ({ item: m.item, min: m.min, max: m.max })),
    }
  }
  return { id: form.id, name: form.name, items: rows.map((m) => m.item) }
}

// Switching to weighted seeds sensible ranges (evenly split across the die) so
// the coverage strip starts complete rather than showing an all-red gap the
// author then has to chase.
function setMode(mode: string) {
  if (!form.value) return
  const weighted = mode === 'weighted'
  if (weighted === form.value.weighted) return
  form.value.weighted = weighted
  if (weighted) {
    const rows = form.value.members.filter((m) => m.item)
    const n = Math.max(rows.length, 1)
    const per = Math.max(1, Math.floor(form.value.maxRoll / n))
    rows.forEach((m, i) => {
      m.min = i * per + 1
      m.max = i === n - 1 ? form.value!.maxRoll : (i + 1) * per
    })
    if (rows.length) form.value.members = rows
  }
}

// ─── Id ← Name ──────────────────────────────────────────────────────────────
// Same slug behaviour as the item editor: the id follows the name until the
// author edits it by hand. A saved list's id is its primary key (buildings and
// camps bind to it), so it is immutable once saved.

function slugify(raw: string): string {
  return raw.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '')
}

const idManuallyEdited = ref(false)

function onIdInput(raw: string) {
  if (!form.value) return
  form.value.id = raw.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+/, '')
  idManuallyEdited.value = true
}

function syncIdFromName() {
  if (form.value?.isNew && !idManuallyEdited.value) {
    form.value.id = slugify(form.value.name)
  }
}

// ─── Catalog + CRUD ─────────────────────────────────────────────────────────

async function reload() {
  const [defs, listDefs] = await Promise.all([fetchItemDefs(), fetchLists()])
  items.value = defs
  lists.value = listDefs
}

onMounted(async () => {
  try {
    await reload()
  } catch (err) {
    loadError.value = err instanceof Error ? err.message : String(err)
  }
})

function resetStatus() {
  saveError.value = ''
  statusNote.value = ''
  savedLabel.value = ''
}

function selectList(id: string) {
  const def = lists.value.find((l) => l.id === id)
  if (!def) return
  selectedId.value = id
  resetStatus()
  idManuallyEdited.value = false
  form.value = formFromDef(def, false)
}

function newList() {
  selectedId.value = ''
  resetStatus()
  idManuallyEdited.value = false
  form.value = { id: '', isNew: true, name: '', weighted: false, maxRoll: 100, members: [] }
}

function duplicateList(id: string) {
  const def = lists.value.find((l) => l.id === id)
  if (!def) return
  selectedId.value = ''
  resetStatus()
  idManuallyEdited.value = false
  form.value = formFromDef(def, true, `${def.name} Copy`)
  syncIdFromName()
}

async function save() {
  if (!form.value || !canSave.value) return
  saving.value = true
  resetStatus()
  try {
    const def = defFromForm(form.value)
    await saveEditorList(def)
    await reload()
    savedLabel.value = 'Saved'
    selectedId.value = def.id
    form.value = { ...form.value, isNew: false }
  } catch (err) {
    saveError.value = err instanceof EditorValidationError
      ? err.serverMessage
      : err instanceof Error ? err.message : String(err)
  } finally {
    saving.value = false
  }
}

async function remove() {
  if (!form.value || form.value.isNew) return
  const id = form.value.id
  if (!(await confirmDelete('list', id, undefined, 'If it ships with the game it will reset to its built-in default; a custom one is removed.'))) return
  resetStatus()
  try {
    await deleteEditorList(id)
    await reload()
    // A list that ships in the embedded catalog resurfaces after its overlay
    // copy is removed — say so rather than letting it look like a failed delete.
    const stillThere = lists.value.some((l) => l.id === id)
    statusNote.value = stillThere
      ? 'Your edits were removed — this list ships with the game, so its default is back.'
      : 'List deleted.'
    if (stillThere) selectList(id)
    else {
      form.value = null
      selectedId.value = ''
    }
  } catch (err) {
    // A blocked delete (list still bound by a table, map, or neutral group)
    // arrives as an EditorValidationError whose message names every referrer.
    saveError.value = err instanceof EditorValidationError
      ? err.serverMessage
      : err instanceof Error ? err.message : String(err)
  }
}

// Keep the id tracking the name while the list is new.
watch(() => form.value?.name, syncIdFromName)
</script>

<style scoped>
.list-editor__scroll {
  flex: 1;
  min-height: 0;
}

.list-editor__grid {
  display: grid;
  /* Two equal columns. Top row is Identity | Items, bottom row is List |
     Validation — both columns carry one width-hungry card (Items' icon grid on
     the right, the List's rows on the left), so neither column is starved. */
  grid-template-columns: minmax(300px, 1fr) minmax(300px, 1fr);
  grid-template-areas:
    "identity items"
    "list     validation";
  gap: var(--ed-gap);
  align-items: start;
}

.list-editor__identity { grid-area: identity; }
.list-editor__browser { grid-area: items; }
.list-editor__list { grid-area: list; }
.list-editor__validation { grid-area: validation; }

/* ─── Items browser ───────────────────────────────────────────────────────── */
.list-editor__browser-controls {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 10px;
}

.list-editor__browser-filters {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}

/* Both selects share the leftover width so the filter row stays tidy when it
   wraps in the narrow left column. */
.list-editor__browser-filters select {
  flex: 1 1 120px;
  min-width: 0;
}

.list-editor__browser-check {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 0.85rem;
  color: var(--ed-text-dim, #9a8f7d);
  white-space: nowrap;
}

.list-editor__browser-check input {
  flex: 0 0 auto;
}

/* An auto-filling icon grid that scrolls once it outgrows its box, so the
   browser never pushes the panel taller than the editor. */
.list-editor__browser-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(44px, 1fr));
  gap: 6px;
  max-height: 300px;
  overflow-y: auto;
  padding: 2px;
}

.list-editor__browser-item {
  display: flex;
  align-items: center;
  justify-content: center;
  aspect-ratio: 1;
  padding: 4px;
  background: rgba(0, 0, 0, 0.22);
  border: 1px solid var(--ed-border, rgba(184, 150, 79, 0.3));
  border-radius: 6px;
}

.list-editor__browser-item:hover {
  border-color: var(--ed-accent, #b8964f);
  background: rgba(184, 150, 79, 0.15);
}

.list-editor__browser-item img {
  width: 100%;
  height: 100%;
  object-fit: contain;
  image-rendering: pixelated;
}

.list-editor__browser-empty {
  margin: 4px 0 0;
  color: var(--ed-text-dim, #9a8f7d);
  font-size: 0.85rem;
  font-style: italic;
}

/* Drop zone: invisible until something is dragged over it, then it lights up so
   the author knows the drop will land. Padding keeps the dashed frame off the
   list rows. */
.list-editor__drop {
  border: 1px dashed transparent;
  border-radius: 8px;
  padding: 4px;
  margin: 4px -4px 0;
  transition: border-color 0.12s ease, background 0.12s ease;
}

.list-editor__drop.is-drag-over {
  border-color: var(--ed-accent, #b8964f);
  background: rgba(184, 150, 79, 0.1);
}

.list-editor__rows {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.list-editor__rows-empty {
  margin: 0;
  color: var(--ed-text-dim, #9a8f7d);
  font-size: 0.85rem;
}

/* Persistent hint under the stacked rows, so the drag affordance is visible
   even once the list has items in it. */
.list-editor__drop-hint {
  margin: 8px 0 0;
  text-align: center;
  font-size: 0.8rem;
  font-style: italic;
  color: var(--ed-text-dim, #9a8f7d);
}

.list-editor__row {
  display: flex;
  gap: 6px;
  align-items: center;
  /* One line, never wrapping. The item (icon + name) takes the leftover width;
     the range inputs and the delete button keep their fixed sizes. */
  flex-wrap: nowrap;
}

/* Icon + name grow to fill the leftover width; flex-basis 0 keeps their size
   purely the leftover so the range inputs stay put. The panel background lives
   HERE (not on the whole row) so it sits only under the item — never under the
   min/max inputs or the delete button — and hovering those does not trigger the
   tooltip. */
.list-editor__row-item {
  flex: 1 1 0;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 5px 8px;
  background: rgba(0, 0, 0, 0.2);
  border: 1px solid var(--ed-border, rgba(184, 150, 79, 0.25));
  border-radius: 6px;
}

.list-editor__row-icon {
  flex: 0 0 auto;
  width: 28px;
  height: 28px;
  object-fit: contain;
  image-rendering: pixelated;
}

/* Placeholder shown when a row's item id no longer resolves (item deleted). */
.list-editor__row-icon--missing {
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.25);
  border: 1px solid var(--ed-border, rgba(184, 150, 79, 0.3));
  border-radius: 4px;
  color: var(--ed-text-dim, #9a8f7d);
  font-weight: 700;
}

.list-editor__row-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--ed-text, #e8dcc4);
  font-size: 0.9rem;
}

/* The range inputs are FIXED at 64px. flex-basis 64px (non-auto) overrides the
   `width:100%` that .ed-shell input[type=number] otherwise forces — that width
   rule is what was blowing the number fields up to fill the row. */
.list-editor__num { flex: 0 0 64px; width: 64px; }
/* Bordered "X box", mirroring the item editor's Proc Effects remove button:
   a dim outlined box that turns danger-red on hover. */
.list-editor__row-del {
  flex: 0 0 auto;
  padding: 4px 7px;
  font-size: 0.78rem;
  line-height: 1;
  color: var(--ed-text-dim);
  background: none;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
}
.list-editor__row-del:hover {
  color: var(--ed-danger);
  border-color: rgba(240, 132, 108, 0.4);
}

.list-editor__note {
  margin: 10px 0 4px;
  color: var(--ed-text-dim, #9a8f7d);
  font-size: 0.85rem;
}

.list-editor__uses {
  margin: 0;
  padding-left: 18px;
  color: var(--ed-text-dim, #9a8f7d);
  font-size: 0.85rem;
  line-height: 1.6;
}

.list-editor__warning {
  margin: 10px 0 0;
  padding: 8px 10px;
  border-left: 3px solid var(--ed-accent, #b8964f);
  background: rgba(184, 150, 79, 0.1);
  color: var(--ed-text, #e8dcc4);
  font-size: 0.85rem;
}

.list-editor__status-note {
  display: block;
  margin-top: 8px;
  color: var(--ed-text-dim, #9a8f7d);
  font-size: 0.85rem;
}

.list-editor__empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--ed-text-dim, #9a8f7d);
}
</style>
