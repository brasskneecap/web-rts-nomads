<template>
  <div class="unit-editor">
    <header class="unit-editor__topbar">
      <div class="unit-editor__pills">
        <button
          type="button"
          class="unit-editor__pill"
          :class="{ 'is-active': factionFilter === '' }"
          @click="factionFilter = ''"
        >
          All <span class="unit-editor__pill-count">{{ units.length }}</span>
        </button>
        <button
          v-for="f in factions"
          :key="f.id"
          type="button"
          class="unit-editor__pill"
          :class="{ 'is-active': factionFilter === f.id }"
          @click="factionFilter = f.id"
        >
          {{ f.displayName }} <span class="unit-editor__pill-count">{{ unitCountByFaction[f.id] ?? 0 }}</span>
        </button>

        <span class="unit-editor__pill-spacer"></span>

        <button
          type="button"
          class="unit-editor__pill unit-editor__pill--add"
          :disabled="busy"
          @click="showNewFaction = !showNewFaction"
        >
          + Faction
        </button>
        <button
          v-if="factionFilter"
          type="button"
          class="unit-editor__pill unit-editor__pill--danger"
          :disabled="busy"
          @click="removeFaction(factionFilter)"
        >
          Delete Faction
        </button>
      </div>

      <div v-if="showNewFaction" class="unit-editor__new-faction">
        <input v-model="newFactionId" placeholder="id (a-z0-9_)" />
        <input v-model="newFactionName" placeholder="Display Name" />
        <button type="button" :disabled="busy || !newFactionId.trim()" @click="createFaction">Create</button>
        <button type="button" @click="showNewFaction = false">Cancel</button>
      </div>
      <p v-if="factionError" class="unit-editor__error">{{ factionError }}</p>
    </header>

    <div class="unit-editor__body">
      <aside class="unit-editor__list">
        <button type="button" class="unit-editor__new" :disabled="busy" @click="newUnit">+ New Unit</button>
        <p v-if="loadError" class="unit-editor__error">{{ loadError }}</p>
        <ul>
          <li v-for="u in visibleUnits" :key="u.type">
            <button
              type="button"
              :class="{ 'is-selected': u.type === selectedType }"
              @click="selectUnit(u)"
            >
              {{ u.name || u.type }}
            </button>
          </li>
        </ul>
      </aside>

      <section class="unit-editor__form">
      <!-- Preview -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('preview') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('preview')">Preview</button>
        <div v-if="openSections.has('preview')" class="unit-editor__section-body">
          <UnitSpritePreview ref="preview" :unit-key="form.type" />

          <div
            class="unit-editor__art-ingest"
            @dragover.prevent
            @drop.prevent="onFolderDropped"
          >
            <label v-if="form.type && form.faction" class="unit-editor__art-drop">
              <input
                type="file"
                webkitdirectory
                multiple
                :disabled="ingesting"
                @change="onFolderInputChanged(($event.target as HTMLInputElement).files)"
              />
              {{ ingesting ? 'Packing…' : 'Drop / choose a PixelLab export folder' }}
            </label>
            <p v-else class="unit-editor__hint">Set the unit's type and faction to ingest art.</p>

            <div v-if="pendingArt" class="unit-editor__art-actions">
              <button type="button" :disabled="busy" @click="saveArt">Save Art</button>
              <button type="button" :disabled="busy" @click="discardPendingArt">Discard</button>
            </div>

            <p v-for="w in ingestWarnings" :key="w" class="unit-editor__warning">{{ w }}</p>
            <p v-if="ingestError" class="unit-editor__error">{{ ingestError }}</p>
          </div>
        </div>
      </section>

      <!-- Identity -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('identity') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('identity')">Identity</button>
        <div v-if="openSections.has('identity')" class="unit-editor__section-body">
          <label>Name <input v-model="form.name" /></label>
          <label>
            ID
            <span class="unit-editor__hint">
              {{ selectedType === null ? 'auto from Name · a–z 0–9 _ · edit to override' : "an existing unit's id can't change" }}
            </span>
            <input
              :value="form.type"
              :disabled="selectedType !== null"
              placeholder="e.g. raider_brute"
              @input="onTypeIdInput(($event.target as HTMLInputElement).value)"
            />
          </label>
          <label>
            Faction
            <select v-model="form.faction">
              <option value="" disabled>— select a faction —</option>
              <option v-for="f in factions" :key="f.id" :value="f.id">{{ f.displayName }}</option>
            </select>
          </label>
          <label>
            Archetype
            <input v-model="form.archetype" list="unit-editor-archetypes" placeholder="(defaults to the unit type)" />
          </label>
          <p v-if="archetypeWarning" class="unit-editor__warning">{{ archetypeWarning }}</p>
          <label>Train Label <input v-model="form.trainLabel" /></label>
        </div>
      </section>

      <!-- Stats -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('stats') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('stats')">Stats</button>
        <div v-if="openSections.has('stats')" class="unit-editor__section-body">
          <label>HP <input type="number" v-model.number="form.hp" /></label>
          <!-- Blank and 0 mean DIFFERENT things here (blank = inherit the
               server default, 0 = never regenerates), so this cannot use
               v-model.number — it would coerce a cleared field to 0 and
               silently turn off regeneration. -->
          <label>
            HP Regen Rate <span class="unit-editor__hint">(per second; blank = server default)</span>
            <input
              type="number"
              step="0.1"
              :value="form.healthRegenRate ?? ''"
              @input="form.healthRegenRate = ($event.target as HTMLInputElement).value === '' ? undefined : Number(($event.target as HTMLInputElement).value)"
            />
          </label>
          <label>Mana <input type="number" v-model.number="form.maxMana" /></label>
          <label>Mana Regen Rate <input type="number" step="0.1" v-model.number="form.manaRegenRate" /></label>
          <label>Armor <input type="number" v-model.number="form.armor" /></label>
          <label>Damage <input type="number" v-model.number="form.damage" /></label>
          <label>Attack Range <input type="number" v-model.number="form.attackRange" /></label>
          <label>Attack Speed <input type="number" v-model.number="form.attackSpeed" /></label>
          <label>Splash Radius <input type="number" v-model.number="form.splashRadius" /></label>
          <label>Move Speed <input type="number" v-model.number="form.moveSpeed" /></label>
          <label>Vision Range <input type="number" v-model.number="form.visionRange" /></label>
        </div>
      </section>

      <!-- Cost -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('cost') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('cost')">Cost</button>
        <div v-if="openSections.has('cost')" class="unit-editor__section-body">
          <div class="unit-editor__map">
            <span class="unit-editor__map-label">Resource Cost</span>
            <div v-for="(row, idx) in resourceCostRows" :key="idx" class="unit-editor__map-row">
              <input v-model="row.key" placeholder="resource key" />
              <input type="number" v-model.number="row.value" />
              <button type="button" @click="removeResourceCostRow(idx)">Remove</button>
            </div>
            <button type="button" @click="addResourceCostRow">+ Add resource cost</button>
          </div>
          <label>Meat Cost <input type="number" v-model.number="form.meatCost" /></label>
          <label>Spawn Seconds <input type="number" v-model.number="form.spawnSeconds" /></label>
        </div>
      </section>

      <!-- Combat -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('combat') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('combat')">Combat</button>
        <div v-if="openSections.has('combat')" class="unit-editor__section-body">
          <label>
            Combat Profile
            <input v-model="form.combatProfile" list="unit-editor-archetypes" placeholder="(inferred from archetype)" />
          </label>
          <label>Attack Type <input v-model="form.attackType" /></label>
          <label>
            Damage Type
            <input v-model="form.damageType" list="unit-editor-damage-types" placeholder="(unspecified = physical)" />
            <datalist id="unit-editor-damage-types">
              <option v-for="d in damageTypes" :key="d" :value="d" />
            </datalist>
          </label>
          <label>
            Targetable Types (comma-separated)
            <input
              :value="(form.targetableTypes ?? []).join(',')"
              @input="updateStringList('targetableTypes', ($event.target as HTMLInputElement).value)"
            />
          </label>
          <label>
            Projectile
            <input v-model="form.projectile" list="unit-editor-projectiles" />
            <datalist id="unit-editor-projectiles">
              <option v-for="p in projectileIds" :key="p" :value="p" />
            </datalist>
          </label>
          <label>Projectile Scale <input type="number" v-model.number="form.projectileScale" /></label>
        </div>
      </section>

      <!-- Resources -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('resources') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('resources')">Resources</button>
        <div v-if="openSections.has('resources')" class="unit-editor__section-body">
          <label>Gold Gather Amount <input type="number" v-model.number="form.goldGatherAmount" /></label>
          <label>Wood Gather Amount <input type="number" v-model.number="form.woodGatherAmount" /></label>
        </div>
      </section>

      <!-- Abilities -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('abilities') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('abilities')">Abilities</button>
        <div v-if="openSections.has('abilities')" class="unit-editor__section-body">
          <label>
            Abilities (comma-separated)
            <input
              :value="(form.abilities ?? []).join(',')"
              @input="updateStringList('abilities', ($event.target as HTMLInputElement).value)"
            />
            <span class="unit-editor__hint">Known: {{ abilityIds.join(', ') || '—' }}</span>
          </label>
          <label>
            Capabilities (comma-separated)
            <input
              :value="(form.capabilities ?? []).join(',')"
              @input="updateStringList('capabilities', ($event.target as HTMLInputElement).value)"
            />
          </label>
          <!-- Not a stat: start/end frame indices of the casting animation to
               loop while a channelled ability is being cast. Lives with
               Abilities because that is the only thing it affects. -->
          <div class="unit-editor__channel-loop">
            <span class="unit-editor__map-label">Channel Loop <span class="unit-editor__hint">(casting-animation frames to loop; leave both blank to unset)</span></span>
            <label>
              Start
              <input
                type="number"
                :value="channelLoopStart ?? ''"
                @input="channelLoopStart = ($event.target as HTMLInputElement).value === '' ? undefined : Number(($event.target as HTMLInputElement).value)"
              />
            </label>
            <label>
              End
              <input
                type="number"
                :value="channelLoopEnd ?? ''"
                @input="channelLoopEnd = ($event.target as HTMLInputElement).value === '' ? undefined : Number(($event.target as HTMLInputElement).value)"
              />
            </label>
          </div>
        </div>
      </section>

      <!-- Gating -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('gating') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('gating')">Gating</button>
        <div v-if="openSections.has('gating')" class="unit-editor__section-body">
          <label>
            Requires Buildings (comma-separated)
            <input
              :value="(form.requiresBuildings ?? []).join(',')"
              @input="updateStringList('requiresBuildings', ($event.target as HTMLInputElement).value)"
            />
            <span class="unit-editor__hint">Known: {{ buildingIds.join(', ') || '—' }}</span>
          </label>
          <div class="unit-editor__map">
            <span class="unit-editor__map-label">Path Chances</span>
            <div v-for="(row, idx) in pathChancesRows" :key="idx" class="unit-editor__map-row">
              <input v-model="row.key" placeholder="path key" />
              <input type="number" v-model.number="row.value" />
              <button type="button" @click="removePathChanceRow(idx)">Remove</button>
            </div>
            <button type="button" @click="addPathChanceRow">+ Add path chance</button>
          </div>
        </div>
      </section>

      <!-- Rewards -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('rewards') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('rewards')">Rewards</button>
        <div v-if="openSections.has('rewards')" class="unit-editor__section-body">
          <label>Dominion Point Drop Chance <input type="number" v-model.number="form.dominionPointDropChance" /></label>
          <label>Dominion Point Amount <input type="number" v-model.number="form.dominionPointAmount" /></label>
          <label>Spawn Exp <input type="number" v-model.number="form.spawnExp" /></label>
          <label>Experience <input type="number" v-model.number="form.experience" /></label>
        </div>
      </section>

      <!-- Flags -->
      <section class="unit-editor__section" :class="{ 'unit-editor__section--open': openSections.has('flags') }">
        <button type="button" class="unit-editor__section-summary" @click="toggleSection('flags')">Flags</button>
        <div v-if="openSections.has('flags')" class="unit-editor__section-body">
          <label class="unit-editor__checkbox-label">
            <input type="checkbox" v-model="form.flyer" /> Flyer
          </label>
          <label class="unit-editor__checkbox-label">
            <input type="checkbox" v-model="form.nonCombat" /> Non-combat
          </label>
        </div>
      </section>

        <p v-if="saveError" class="unit-editor__error">{{ saveError }}</p>
        <div class="unit-editor__actions">
          <button type="button" :disabled="busy || !form.type || !form.faction" @click="save">Save</button>
          <button type="button" :disabled="busy || selectedType === null" @click="removeUnit">Delete</button>
        </div>
      </section>
    </div>
  </div>
  <!-- Shared by both Archetype (Identity) and Combat Profile (Combat): one
       combat-profile key set backs both fields. Mounted at the root so a
       collapsed Identity section can't strip the Combat picker's suggestions. -->
  <datalist id="unit-editor-archetypes">
    <option v-for="a in archetypes" :key="a" :value="a" />
  </datalist>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import {
  createBlankForm, formFromDef, saveRequestFromForm, pickTemplateStats,
  type AuthoredUnitDef, type UnitEditorForm,
} from '@/game/units/unitEditorForm'
import {
  fetchAuthoredUnitDefs, saveEditorUnit, deleteEditorUnit, EditorValidationError,
} from '@/game/units/unitEditorApi'
import {
  fetchFactions, saveFaction, deleteFaction, fetchArchetypes,
  fetchProjectileIds, fetchAbilityIds, fetchDamageTypes, fetchBuildingIds,
  saveUnitArt, type FactionDef, type UnitArtUploadFile,
} from '@/game/units/editorCatalogApi'
import {
  ingestExportFolder, packedSheetToObjectUrls, blobToBase64,
  type DroppedFile, type IngestResult,
} from '@/game/units/spriteIngest'
import {
  buildSpriteSet, registerRuntimeSpriteSet, loadRuntimeSpriteSets, type SpriteManifest,
} from '@/game/rendering/unitSprites'
import UnitSpritePreview from '@/components/UnitSpritePreview.vue'

const units = ref<AuthoredUnitDef[]>([])
// The preview re-resolves its sprite set from the runtime overlay on refresh().
// Called after selecting a unit and after a save — including an art save,
// which reloads the runtime overlay first (see saveArt) so refresh() picks
// up the newly-served art.
const preview = ref<InstanceType<typeof UnitSpritePreview> | null>(null)
const form = ref<UnitEditorForm>(createBlankForm())
const selectedType = ref<string | null>(null)
const saveError = ref('')
const loadError = ref('')
const busy = ref(false)

// Server-sourced catalogs backing the selects. Free-text fallbacks stay
// available (datalist, not select) because the SERVER is the validator — a
// stale dropdown must never block a legal value.
const factions = ref<FactionDef[]>([])
const archetypes = ref<string[]>([])
const projectileIds = ref<string[]>([])
const abilityIds = ref<string[]>([])
const damageTypes = ref<string[]>([])
const buildingIds = ref<string[]>([])

const factionFilter = ref<string>('')   // '' = All
const newFactionId = ref('')
const newFactionName = ref('')
const showNewFaction = ref(false)
const factionError = ref('')

const visibleUnits = computed(() =>
  factionFilter.value
    ? units.value.filter((u) => u.faction === factionFilter.value)
    : units.value,
)

// Per-faction unit counts for the filter pills. Derived from the units the
// editor actually loaded, not from the faction records — so a faction with a
// record but no units correctly reads 0, which is the signal that it can be
// deleted.
const unitCountByFaction = computed(() => {
  const counts: Record<string, number> = {}
  for (const unit of units.value) {
    if (!unit.faction) continue
    counts[unit.faction] = (counts[unit.faction] ?? 0) + 1
  }
  return counts
})

// An archetype outside the combat-profile set is NOT rejected by the server —
// it silently falls back to the soldier profile. So warn, don't block.
// (combatProfile IS validated server-side and will hard-fail on save; archetype
// is not. Same list, different strictness — that's why this is a warning.)
const archetypeWarning = computed(() => {
  const value = form.value.archetype
  if (!value || archetypes.value.length === 0) return ''
  if (archetypes.value.includes(value)) return ''
  return `"${value}" is not a known combat profile — this unit will fall back to the soldier profile.`
})

async function reloadCatalogs() {
  const [f, a, p, ab, dt, b] = await Promise.all([
    fetchFactions(), fetchArchetypes(), fetchProjectileIds(),
    fetchAbilityIds(), fetchDamageTypes(), fetchBuildingIds(),
  ])
  factions.value = f
  archetypes.value = a
  projectileIds.value = p
  abilityIds.value = ab
  damageTypes.value = dt
  buildingIds.value = b
}

async function createFaction() {
  factionError.value = ''
  busy.value = true
  try {
    await saveFaction({ id: newFactionId.value.trim(), displayName: newFactionName.value.trim() })
    await reloadCatalogs()
    factionFilter.value = newFactionId.value.trim()
    // Only seed the faction of a NEW unit. Writing this while an existing unit
    // is loaded would silently reassign that unit's faction, with an enabled
    // Save button sitting right under it.
    if (selectedType.value === null) form.value.faction = factionFilter.value
    newFactionId.value = ''
    newFactionName.value = ''
    showNewFaction.value = false
  } catch (e) {
    factionError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

// The server refuses to delete a faction that still owns units, and its message
// names them. Surface it verbatim — it tells the author exactly what to fix.
async function removeFaction(id: string) {
  factionError.value = ''
  busy.value = true
  try {
    await deleteFaction(id)
    if (factionFilter.value === id) factionFilter.value = ''
    await reloadCatalogs()
  } catch (e) {
    factionError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

// Collapsible sections — Identity starts open so a freshly loaded panel isn't
// entirely collapsed; every other section starts closed like ItemEditorPanel's
// accordion, but as a Set so multiple sections can be open at once.
const openSections = reactive(new Set<string>(['preview', 'identity']))
function toggleSection(key: string) {
  if (openSections.has(key)) openSections.delete(key)
  else openSections.add(key)
}

// resourceCost / pathChances are string->number maps on the form. Rows are
// kept as local {key,value} arrays (so add/remove/rename is simple array
// editing) and mirrored back into form.<field> via watch — saveRequestFromForm
// reads form.<field> directly, so the mirrored map is what actually saves.
interface MapRow { key: string; value: number }
const resourceCostRows = ref<MapRow[]>([])
const pathChancesRows = ref<MapRow[]>([])

function rowsFromMap(map?: Record<string, number>): MapRow[] {
  return Object.entries(map ?? {}).map(([key, value]) => ({ key, value }))
}
function mapFromRows(rows: MapRow[]): Record<string, number> {
  const out: Record<string, number> = {}
  for (const row of rows) if (row.key) out[row.key] = row.value
  return out
}
function addResourceCostRow() { resourceCostRows.value.push({ key: '', value: 0 }) }
function removeResourceCostRow(idx: number) { resourceCostRows.value.splice(idx, 1) }
function addPathChanceRow() { pathChancesRows.value.push({ key: '', value: 0 }) }
function removePathChanceRow(idx: number) { pathChancesRows.value.splice(idx, 1) }

watch(resourceCostRows, (rows) => { form.value.resourceCost = mapFromRows(rows) }, { deep: true })
watch(pathChancesRows, (rows) => { form.value.pathChances = mapFromRows(rows) }, { deep: true })

// channelLoop is a nested {start,end} object that must stay entirely
// undefined when both fields are unset. Tracked as two local optional refs
// and merged back into form.channelLoop via watch.
const channelLoopStart = ref<number | undefined>(undefined)
const channelLoopEnd = ref<number | undefined>(undefined)
watch([channelLoopStart, channelLoopEnd], ([s, e]) => {
  form.value.channelLoop = (s === undefined && e === undefined) ? undefined : { start: s ?? 0, end: e ?? 0 }
})

// Generic comma-separated string[] binding, shared by abilities/capabilities/
// requiresBuildings/targetableTypes.
type StringListField = 'abilities' | 'capabilities' | 'requiresBuildings' | 'targetableTypes'
function updateStringList(field: StringListField, raw: string) {
  form.value[field] = raw.split(',').map((s) => s.trim()).filter(Boolean)
}

// --- Art ingest (Phase 3): drop a PixelLab export folder, pack it entirely
// in-browser, preview it live via the Phase 2 runtime overlay (P3-B — reuses
// that overlay rather than a second preview path), and persist on Save. ---

// Packed-but-unsaved ingest result. Feeds the Phase 2 overlay via object URLs
// so the preview plays the freshly-packed art before anything is saved.
const pendingArt = ref<IngestResult | null>(null)
let pendingRevoke: (() => void) | null = null
const ingestError = ref('')
const ingestWarnings = ref<string[]>([])

// Guards runIngest against overlapping drops: without this, starting a
// second drop while the first is still decoding/rasterizing races
// `pendingRevoke = revoke` between the two in-flight calls — whichever
// resolves last silently overwrites the other's revoke function, leaking
// every blob URL from the earlier drop for the page's lifetime (and leaving
// its set registered in the shared overlay, see flushPreviewOverlay below).
const ingesting = ref(false)

// Revokes the pending object URLs (if any) and clears local ingest state.
// This is PURELY local cleanup — it does NOT touch the shared runtime
// overlay (`runtimeSprites`, module-global in unitSprites.ts). Callers that
// abandon a registered preview set outside save/discard must additionally
// call `flushPreviewOverlay()` — see its doc comment for why.
function clearPending() {
  pendingRevoke?.()
  pendingRevoke = null
  pendingArt.value = null
  ingestWarnings.value = []
}

// The shared runtime sprite overlay (`registerRuntimeSpriteSet` /
// `getUnitSpriteSet`) is a MODULE-GLOBAL that the live game map also reads —
// it outlives this panel. If the panel registers an in-browser preview set
// (backed by object URLs) and the URLs are then revoked by `clearPending()`
// without also flushing the overlay, that unit renders blank in an actual
// match for the rest of the session (the overlay only reloads at app boot
// otherwise). `loadRuntimeSpriteSets()` is the correct fix, not a plain
// "unregister this key": it fully swaps the overlay back to whatever the
// SERVER actually has for every unit, which correctly restores prior
// server-persisted art for this unit (if any) rather than just clearing it.
async function flushPreviewOverlay() {
  await loadRuntimeSpriteSets()
  preview.value?.refresh()
}

// spritePacking.ts's pure core can't derive `key` (no directory context —
// see spritePacking.ts's SpriteManifestJSON doc comment), so we attach it
// here from the unit type, mirroring the CLI's `path.basename(unitDir)`
// rule. Used both for the live preview manifest and the uploaded sprites.json.
function manifestWithKey(manifest: IngestResult['manifest'], key: string): SpriteManifest {
  return { ...manifest, key }
}

async function runIngest(files: DroppedFile[]) {
  // A drop that arrives while a previous one is still decoding/rasterizing
  // is ignored outright rather than raced (see `ingesting`'s doc comment).
  if (ingesting.value) return
  ingesting.value = true
  ingestError.value = ''
  // Captured BEFORE clearPending() revokes it: clearPending only revokes this
  // drop's OWN previous object URLs, it never touches the shared overlay. If
  // a preview set was actually registered (from a prior successful drop),
  // every exit below that does NOT go on to register a replacement set must
  // flush the overlay back to server truth — otherwise runtimeSprites is left
  // pointing at the just-revoked URLs (see flushPreviewOverlay's doc comment).
  const hadPending = pendingArt.value !== null
  clearPending()
  try {
    if (files.length === 0) {
      if (hadPending) void flushPreviewOverlay()
      return
    }
    const result = await ingestExportFolder(files)
    ingestWarnings.value = result.warnings

    const { urls, revoke } = packedSheetToObjectUrls(result)
    pendingRevoke = revoke
    pendingArt.value = result

    // Feed the Phase 2 overlay so the preview shows the packed art live,
    // before saving — this IS the preview-before-persist path (P3-B).
    const key = form.value.type.toLowerCase()
    const manifest = manifestWithKey(result.manifest, form.value.type)
    const set = buildSpriteSet(key, manifest, (rel) => urls[rel])
    if (set) registerRuntimeSpriteSet(set)
    preview.value?.refresh()
  } catch (e) {
    ingestError.value = e instanceof Error ? e.message : String(e)
    clearPending()
    // The previous drop's preview set (if any) is now dead (URLs revoked, no
    // replacement registered) — restore server truth so it doesn't stay
    // shadowed-and-broken in the shared overlay.
    if (hadPending) void flushPreviewOverlay()
  } finally {
    ingesting.value = false
  }
}

// Primary ingest path: <input type="file" webkitdirectory> — the reliable
// cross-browser way to pick a whole folder (prioritized per the plan; drag
// and drop of folders is finicky). webkitRelativePath is
// "<wrapperFolder>/<rest...>"; strip the wrapper segment so paths are
// relative to the export folder root, matching what ingestExportFolder expects.
function onFolderInputChanged(fileList: FileList | null) {
  if (!fileList) return
  const files: DroppedFile[] = [...fileList].map((f) => ({
    path: f.webkitRelativePath.split('/').slice(1).join('/'),
    blob: f,
  }))
  void runIngest(files)
}

// Secondary ingest path: dragging a folder onto the zone. Best-effort via the
// non-standard-but-broadly-supported `DataTransferItem.webkitGetAsEntry`
// FileSystem API. A dropped top-level DIRECTORY is treated as the export
// folder's own wrapper (its name is stripped, matching the input path's
// convention above); a top-level FILE (or a directory dropped alongside
// loose files, without an enclosing wrapper) keeps its own relative path.
function readDirectoryEntries(reader: FileSystemDirectoryReader): Promise<FileSystemEntry[]> {
  return new Promise((resolve, reject) => {
    const all: FileSystemEntry[] = []
    const readBatch = () => {
      reader.readEntries((batch) => {
        if (batch.length === 0) resolve(all)
        else {
          all.push(...batch)
          readBatch()
        }
      }, reject)
    }
    readBatch()
  })
}

async function collectEntry(entry: FileSystemEntry, prefix: string): Promise<DroppedFile[]> {
  const relPath = prefix ? `${prefix}/${entry.name}` : entry.name
  if (entry.isFile) {
    const file = await new Promise<File>((resolve, reject) => {
      (entry as FileSystemFileEntry).file(resolve, reject)
    })
    return [{ path: relPath, blob: file }]
  }
  if (entry.isDirectory) {
    const children = await readDirectoryEntries((entry as FileSystemDirectoryEntry).createReader())
    const nested = await Promise.all(children.map((child) => collectEntry(child, relPath)))
    return nested.flat()
  }
  return []
}

async function collectTopLevelEntry(entry: FileSystemEntry): Promise<DroppedFile[]> {
  if (!entry.isDirectory) return collectEntry(entry, '')
  const children = await readDirectoryEntries((entry as FileSystemDirectoryEntry).createReader())
  const nested = await Promise.all(children.map((child) => collectEntry(child, '')))
  return nested.flat()
}

async function onFolderDropped(event: DragEvent) {
  if (!form.value.type || !form.value.faction) return
  if (ingesting.value) return
  const items = event.dataTransfer?.items
  if (!items || items.length === 0) return
  const topEntries: FileSystemEntry[] = []
  for (const item of items) {
    const entry = item.webkitGetAsEntry?.()
    if (entry) topEntries.push(entry)
  }
  if (topEntries.length === 0) return
  try {
    const nested = await Promise.all(topEntries.map(collectTopLevelEntry))
    await runIngest(nested.flat())
  } catch (e) {
    ingestError.value = e instanceof Error ? e.message : String(e)
    clearPending()
  }
}

async function saveArt() {
  const art = pendingArt.value
  if (!art) return
  ingestError.value = ''
  busy.value = true
  try {
    const manifest = { ...manifestWithKey(art.manifest, form.value.type), packedAt: new Date().toISOString() }
    const files: UnitArtUploadFile[] = [
      { name: 'sprites.json', contentBase64: await blobToBase64(new Blob([JSON.stringify(manifest, null, 2)])) },
    ]
    for (const sheet of art.sheets) {
      files.push({ name: sheet.name, contentBase64: await blobToBase64(sheet.blob) })
    }
    if (art.portrait) {
      files.push({ name: 'portrait.png', contentBase64: await blobToBase64(art.portrait) })
    }
    await saveUnitArt({ faction: form.value.faction, unit: form.value.type, files })
    // Replace the in-memory object-URL preview set with the persisted server art.
    clearPending()
    await flushPreviewOverlay()
  } catch (e) {
    ingestError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function discardPendingArt() {
  clearPending()
  ingestError.value = ''
  // Reset the runtime overlay to server truth — the discarded in-memory set
  // must stop shadowing whatever (if anything) is actually persisted.
  await flushPreviewOverlay()
}

onBeforeUnmount(() => {
  // Same hazard as selectUnit/newUnit: closing the editor with an unsaved
  // drop still registered must flush the shared overlay back to server
  // truth, or the abandoned unit stays broken in a live match after the
  // panel is long gone. Fire-and-forget — the component itself is unmounting,
  // there is nothing left here to await into.
  const hadPending = pendingArt.value !== null
  clearPending()
  if (hadPending) void loadRuntimeSpriteSets()
})

async function reload() {
  try {
    units.value = await fetchAuthoredUnitDefs()
    loadError.value = ''
    // A successful unit list refresh means the list itself is current — any
    // stale faction-delete error (e.g. naming units the author just deleted)
    // no longer applies.
    factionError.value = ''
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e)
  }
}

function selectUnit(def: AuthoredUnitDef) {
  // A unit switch abandons any not-yet-saved ingest for the PREVIOUS unit —
  // its object URLs must be revoked (clearPending), AND — if a preview set
  // was actually registered into the shared overlay — that overlay must be
  // flushed back to server truth (flushPreviewOverlay), or the abandoned
  // unit renders blank in a real match. See flushPreviewOverlay's doc comment.
  const hadPending = pendingArt.value !== null
  clearPending()
  ingestError.value = ''
  if (hadPending) void flushPreviewOverlay()
  form.value = formFromDef(def)
  selectedType.value = def.type
  saveError.value = ''
  resourceCostRows.value = rowsFromMap(def.resourceCost)
  pathChancesRows.value = rowsFromMap(def.pathChances)
  channelLoopStart.value = def.channelLoop?.start
  channelLoopEnd.value = def.channelLoop?.end
  preview.value?.refresh()
}

// The `type` field is the unit's id (its directory name, sprite key, and what
// pathChances/requiresBuildings/spawn all reference), so it must match
// `^[a-z0-9_]+$`. Rather than make the author type both a display Name and a
// matching id, the id is a slug auto-derived from the Name — lowercase, with any
// run of non-alphanumeric characters collapsed to a single underscore.
function slugifyUnitId(raw: string): string {
  return raw.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '')
}

// True once the author has hand-edited the id, which stops it from tracking the
// Name (standard slug-field behaviour). Reset per new unit.
const typeIdManuallyEdited = ref(false)

function onTypeIdInput(raw: string) {
  // Slug the manual input too, so a typed "Raider Brute" becomes a valid id
  // rather than failing server validation on save. No trailing-underscore trim
  // here so typing mid-word (e.g. "raider_") isn't fought.
  form.value.type = raw.toLowerCase().replace(/[^a-z0-9]+/g, '_').replace(/^_+/, '')
  typeIdManuallyEdited.value = true
}

// Auto-derive the id from the Name for NEW units only, until the author edits the
// id by hand. Existing units keep their id — it's the primary key, and renaming
// it would orphan the unit's art, promotion paths, and every reference to it.
watch(() => form.value.name, (name) => {
  if (selectedType.value === null && !typeIdManuallyEdited.value) {
    form.value.type = slugifyUnitId(name ?? '')
  }
})

function newUnit() {
  const hadPending = pendingArt.value !== null
  clearPending()
  ingestError.value = ''
  if (hadPending) void flushPreviewOverlay()
  form.value = createBlankForm(pickTemplateStats(units.value))
  form.value.faction = factionFilter.value || ''
  selectedType.value = null
  typeIdManuallyEdited.value = false
  saveError.value = ''
  resourceCostRows.value = []
  pathChancesRows.value = []
  channelLoopStart.value = undefined
  channelLoopEnd.value = undefined
}

async function save() {
  saveError.value = ''
  busy.value = true
  try {
    await saveEditorUnit(saveRequestFromForm(form.value))
    await reload()
    selectedType.value = form.value.type
    preview.value?.refresh()
  } catch (e) {
    saveError.value = e instanceof EditorValidationError ? e.serverMessage
      : e instanceof Error ? e.message : String(e)
  } finally {
    busy.value = false
  }
}

async function removeUnit() {
  if (!selectedType.value) return
  saveError.value = ''
  busy.value = true
  try {
    await deleteEditorUnit(selectedType.value)
    await reload()
    newUnit()
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
    await reloadCatalogs()
  } catch (e) {
    // A catalog fetch failure degrades the selects to free text — it must not
    // take the whole panel down, because unit editing still works without them.
    loadError.value = e instanceof Error ? e.message : String(e)
  }
})
</script>

<style scoped>
.unit-editor {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  min-height: 0;
  min-width: 0;
  gap: 12px;
  padding: 16px;
  box-sizing: border-box;
}

/* The two-column body sits under the full-width faction pill bar. */
.unit-editor__body {
  display: flex;
  flex: 1;
  min-height: 0;
  min-width: 0;
  gap: 12px;
}

.unit-editor__list {
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

.unit-editor__list ul {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.unit-editor__list button {
  width: 100%;
  border: 1px solid transparent;
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.6);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
  text-align: left;
}

.unit-editor__list button.is-selected {
  border-color: rgba(215, 187, 132, 0.6);
  background: rgba(30, 41, 59, 0.9);
}

.unit-editor__new {
  font-weight: 700;
}

.unit-editor__form {
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

.unit-editor__section {
  border: 1px solid rgba(148, 163, 184, 0.18);
  border-radius: 12px;
  background: rgba(8, 14, 24, 0.55);
  overflow: clip;
  flex: 0 0 auto;
}

.unit-editor__section--open {
  background: rgba(8, 14, 24, 0.72);
}

.unit-editor__section-summary {
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

.unit-editor__section-summary::after {
  content: '+';
  float: right;
  color: #d7bb84;
}

.unit-editor__section--open .unit-editor__section-summary::after {
  content: '-';
}

.unit-editor__section-body {
  display: grid;
  gap: 8px;
  padding: 10px;
}

.unit-editor__section-body label {
  display: grid;
  gap: 4px;
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.75rem;
}

.unit-editor__section-body input,
.unit-editor__section-body select {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
}

.unit-editor__checkbox-label {
  flex-direction: row !important;
  align-items: center;
  gap: 6px !important;
}

.unit-editor__map {
  display: grid;
  gap: 6px;
}

.unit-editor__map-label {
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.75rem;
  font-weight: 700;
}

.unit-editor__hint {
  font-weight: 400;
  opacity: 0.65;
  text-transform: none;
  letter-spacing: 0;
}

.unit-editor__map-row {
  display: flex;
  gap: 6px;
  align-items: center;
}

.unit-editor__map-row input {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
  flex: 1;
}

.unit-editor__channel-loop {
  display: grid;
  gap: 6px;
}

.unit-editor__actions {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: auto;
  padding-top: 8px;
}

.unit-editor__error {
  color: #fca5a5;
  font-size: 0.78rem;
}

/* Faction pill bar — spans the full panel width above both columns. */
.unit-editor__topbar {
  flex: 0 0 auto;
  display: grid;
  gap: 8px;
  background: rgba(3, 8, 14, 0.86);
  border: 1px solid rgba(148, 163, 184, 0.22);
  border-radius: 16px;
  padding: 10px 12px;
}

.unit-editor__pills {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}

/* Pushes the +Faction / Delete actions to the right edge, so the faction
   filters read as one group and the destructive action is nowhere near them. */
.unit-editor__pill-spacer {
  flex: 1 1 auto;
}

.unit-editor__pill {
  border: 1px solid rgba(148, 163, 184, 0.25);
  border-radius: 999px;
  background: rgba(15, 23, 42, 0.72);
  color: rgba(226, 232, 240, 0.86);
  padding: 5px 12px;
  font-size: 0.76rem;
  font-weight: 600;
  white-space: nowrap;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.unit-editor__pill:hover:not(:disabled) {
  border-color: rgba(215, 187, 132, 0.45);
  background: rgba(30, 41, 59, 0.9);
}

.unit-editor__pill.is-active {
  border-color: rgba(215, 187, 132, 0.75);
  background: rgba(215, 187, 132, 0.16);
  color: #f8fafc;
}

.unit-editor__pill-count {
  font-size: 0.68rem;
  font-weight: 700;
  color: rgba(226, 232, 240, 0.6);
  background: rgba(2, 6, 12, 0.55);
  border-radius: 999px;
  padding: 1px 6px;
}

.unit-editor__pill.is-active .unit-editor__pill-count {
  color: #f8fafc;
}

.unit-editor__pill--add {
  border-style: dashed;
  border-color: rgba(215, 187, 132, 0.5);
  color: #d7bb84;
}

.unit-editor__pill--danger {
  border-color: rgba(248, 113, 113, 0.45);
  color: #fca5a5;
}

.unit-editor__pill--danger:hover:not(:disabled) {
  border-color: rgba(248, 113, 113, 0.8);
  background: rgba(127, 29, 29, 0.35);
}

.unit-editor__new-faction {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  padding: 8px;
  border: 1px solid rgba(215, 187, 132, 0.35);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.6);
}

.unit-editor__new-faction input {
  flex: 1 1 140px;
  min-width: 0;
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
}

.unit-editor__warning {
  color: #fcd34d;
  font-size: 0.72rem;
  margin: 0;
}

/* Art ingest drop zone — sits under the preview, in the panel's dark aesthetic. */
.unit-editor__art-ingest {
  display: flex;
  flex-direction: column;
  gap: 8px;
  border: 1px dashed rgba(148, 163, 184, 0.3);
  border-radius: 12px;
  padding: 12px;
  background: rgba(8, 14, 24, 0.55);
}

.unit-editor__art-drop {
  display: flex;
  flex-direction: column;
  gap: 4px;
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.78rem;
  font-weight: 600;
}

.unit-editor__art-drop input[type='file'] {
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.74rem;
}

.unit-editor__art-actions {
  display: flex;
  gap: 8px;
}
</style>
