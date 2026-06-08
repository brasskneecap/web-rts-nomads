<template>
  <UiPanel variant="parchment" :padding="0" class="campaign">
    <button
      type="button"
      class="campaign__close"
      aria-label="Close campaign panel"
      @click="emit('close')"
    >
      &times;
    </button>
    <div class="campaign__inner">
      <div class="campaign__header">
        <h1 class="campaign__title">Campaigns</h1>
      </div>

      <!-- Campaign tabs. One tab per campaign in CAMPAIGNS (see
           @/data/campaigns). Always rendered so a single shipped campaign
           still reads as a tab strip, and so locked placeholder campaigns
           (e.g. Swamp) advertise upcoming content. -->
      <div class="campaign__tabs" role="tablist">
        <button
          v-for="entry in campaignsView"
          :key="entry.campaign.id"
          type="button"
          role="tab"
          :aria-selected="entry.campaign.id === activeCampaignId"
          :aria-disabled="entry.campaign.locked ? 'true' : 'false'"
          class="campaign__tab"
          :class="{
            'campaign__tab--active': entry.campaign.id === activeCampaignId,
            'campaign__tab--locked': entry.campaign.locked,
          }"
          @click="selectCampaign(entry.campaign.id)"
        >
          <span class="campaign__tab-label">{{ entry.campaign.displayName }}</span>
          <span
            v-if="entry.campaign.locked"
            class="campaign__tab-lock"
            aria-hidden="true"
          >&#x1f512;</span>
        </button>
      </div>

      <!-- Active campaign banner. With a single campaign there's no tab strip,
           so the name lives in the panel header instead. -->
      <div v-if="activeCampaign" class="campaign__active-header">
        <div class="campaign__active-name">{{ activeCampaign.campaign.displayName }}</div>
        <div v-if="activeCampaign.campaign.description" class="campaign__active-desc">
          {{ activeCampaign.campaign.description }}
        </div>
      </div>

      <div v-if="startError" class="campaign__error" role="alert">{{ startError }}</div>
      <div v-if="catalogLoadError" class="campaign__error" role="alert">{{ catalogLoadError }}</div>
      <div
        v-if="isCatalogLoading && campaignsView.length === 0"
        class="campaign__loading"
      >
        Loading campaigns…
      </div>

      <div v-if="activeCampaign" class="campaign__body">
        <ul class="campaign__levels">
          <li
            v-for="(entry, idx) in activeCampaign.levels"
            :key="entry.level.id"
          >
            <button
              type="button"
              class="campaign-level"
              :class="[
                `campaign-level--${entry.status}`,
                { 'campaign-level--selected': entry.level.id === selectedLevelId },
              ]"
              :aria-pressed="entry.level.id === selectedLevelId"
              @click="selectedLevelId = entry.level.id"
            >
              <div class="campaign-level__index">{{ idx + 1 }}</div>
              <div class="campaign-level__body">
                <div class="campaign-level__name-row">
                  <span class="campaign-level__name">{{ entry.level.displayName }}</span>
                  <span class="campaign-level__status">{{ statusLabel(entry.status) }}</span>
                </div>
                <div v-if="entry.level.description" class="campaign-level__desc">
                  {{ entry.level.description }}
                </div>
              </div>
            </button>
          </li>
        </ul>

        <div class="campaign__detail">
          <div class="campaign__preview">
            <MinimapPreview
              :map="selectedMap"
              :show-metadata="false"
              :max-display-size="200"
            />
            <div v-if="mapCatalogLoadError" class="campaign__preview-error">
              {{ mapCatalogLoadError }}
            </div>
          </div>

          <!-- Objectives — real per-level data. Each row shows whether the
               profile has ever recorded a completion of this objective,
               regardless of how many attempts it took (achievement mode,
               see Decision in design.md). Required objectives are marked
               with a small badge so the player knows which gate victory. -->
          <div class="campaign__objectives">
            <div class="campaign__objectives-header">Objectives</div>
            <div
              v-if="!selectedLevelObjectives.length"
              class="campaign__objectives-empty"
            >
              No objectives for this level.
            </div>
            <ul v-else class="campaign__objectives-list">
              <li
                v-for="obj in selectedLevelObjectives"
                :key="obj.id"
                class="campaign-objective"
                :class="{
                  'campaign-objective--completed': isObjectiveDone(obj.id),
                  'campaign-objective--required': obj.required,
                }"
              >
                <span
                  class="campaign-objective__checkbox"
                  :class="{ 'campaign-objective__checkbox--checked': isObjectiveDone(obj.id) }"
                  aria-hidden="true"
                >{{ isObjectiveDone(obj.id) ? '✓' : '' }}</span>
                <span class="campaign-objective__label">
                  {{ obj.description || obj.id }}
                </span>
                <span v-if="obj.required" class="campaign-objective__required-badge">
                  Required
                </span>
              </li>
            </ul>
          </div>

          <div class="campaign__actions">
            <button
              type="button"
              class="campaign-level__action campaign-level__action--start"
              :disabled="!selectedLevelView || selectedLevelView.status === 'locked' || isStarting"
              :aria-label="selectedLevelView ? `Start ${selectedLevelView.level.displayName}` : 'Start'"
              @click="onStart"
            >
              Start
            </button>
            <button
              type="button"
              class="campaign-level__action campaign-level__action--lobby"
              :disabled="!selectedLevelView || selectedLevelView.status === 'locked' || isStarting"
              :aria-label="selectedLevelView ? `Open lobby for ${selectedLevelView.level.displayName}` : 'Lobby'"
              @click="onLobby"
            >
              Lobby
            </button>
          </div>
        </div>
      </div>
    </div>
  </UiPanel>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import type { CampaignLevel, CampaignLevelStatus } from '@/types/campaign'
import type { MapCatalogEntry } from '@/game/network/protocol'
import { fetchMapCatalog } from '@/game/maps/catalog'
import { useCampaign } from '@/composables/useCampaign'
import { useProfile } from '@/composables/useProfile'
import UiPanel from '@/components/ui/UiPanel.vue'
import MinimapPreview from '@/components/menu/MinimapPreview.vue'

const emit = defineEmits<{
  (e: 'close'): void
}>()

const {
  campaignsView,
  isLoading: isCatalogLoading,
  loadError: catalogLoadError,
  initialize: initCampaigns,
  startCampaignLevel,
  openCampaignLobby,
  isObjectiveCompletedForLevel,
} = useCampaign()
const { initialize: initProfile } = useProfile()

// Currently-selected campaign id. Empty until the catalog arrives from the
// server; the watcher below seeds it to the first UNLOCKED campaign once
// `campaignsView` is populated, so locked placeholders (e.g. Swamp) don't
// render a frozen panel on first open. If the catalog is entirely locked,
// falls back to the first entry so the user still sees what's coming.
const activeCampaignId = ref<string>('')

watch(
  campaignsView,
  (next) => {
    if (activeCampaignId.value) {
      const stillValid = next.some((c) => c.campaign.id === activeCampaignId.value)
      if (stillValid) return
    }
    const firstUnlocked = next.find((c) => !c.campaign.locked)?.campaign.id
    activeCampaignId.value = firstUnlocked ?? next[0]?.campaign.id ?? ''
  },
  { immediate: true },
)

const activeCampaign = computed(
  () => campaignsView.value.find((c) => c.campaign.id === activeCampaignId.value) ?? null,
)

/** Tab click handler. Locked campaigns swallow the click — they're shown
 *  to advertise upcoming content, not to be selected. EXT-LOCK: when
 *  unlock conditions become dynamic, route through the unlock check here. */
function selectCampaign(id: string) {
  const entry = campaignsView.value.find((c) => c.campaign.id === id)
  if (!entry || entry.campaign.locked) return
  activeCampaignId.value = id
}

const isStarting = ref(false)
const startError = ref<string>('')

/** The selected level's authored objectives (server catalog data). Empty
 *  when no level is selected, or when the level was authored without an
 *  `objectives` array. */
const selectedLevelObjectives = computed(() => {
  return selectedLevelView.value?.level.objectives ?? []
})

/** Is the given objective recorded as completed in the player profile?
 *  Reads via useCampaign so the lookup is reactive to profile refreshes
 *  (e.g. after a match-end write triggers a profile re-fetch). */
function isObjectiveDone(objectiveId: string): boolean {
  if (!activeCampaign.value || !selectedLevelView.value) return false
  return isObjectiveCompletedForLevel(
    activeCampaign.value.campaign.id,
    selectedLevelView.value.level.id,
    objectiveId,
  )
}

// Map catalog: needed so the right-column MinimapPreview can render the
// selected level's terrain. Mirrors CreateGame.vue's load pattern — one
// fetch on mount, errors surfaced in the level row as a faint warning
// (the campaign panel still works without a preview).
const mapCatalog = ref<MapCatalogEntry[]>([])
const mapCatalogLoadError = ref<string>('')

// Currently-selected level inside the active campaign. Defaults to the
// first level whenever the active campaign changes; the watch below
// keeps it in a valid state if the active campaign is swapped via tabs.
const selectedLevelId = ref<string>('')

watch(
  activeCampaign,
  (next) => {
    if (!next) {
      selectedLevelId.value = ''
      return
    }
    const stillValid = next.levels.some((l) => l.level.id === selectedLevelId.value)
    if (!stillValid) {
      selectedLevelId.value = next.levels[0]?.level.id ?? ''
    }
  },
  { immediate: true },
)

const selectedLevelView = computed(() => {
  if (!activeCampaign.value) return null
  return activeCampaign.value.levels.find((l) => l.level.id === selectedLevelId.value) ?? null
})

const selectedMap = computed<MapCatalogEntry | null>(() => {
  const lv = selectedLevelView.value
  if (!lv) return null
  return mapCatalog.value.find((m) => m.id === lv.level.mapId) ?? null
})

function statusLabel(status: CampaignLevelStatus): string {
  switch (status) {
    case 'locked':
      return 'Locked'
    case 'unlocked':
      return 'Available'
    case 'completed':
      return 'Completed'
  }
}

async function runSelectedAction(
  action: (level: CampaignLevel) => Promise<void>,
  failureMessage: string,
) {
  const level = selectedLevelView.value?.level
  if (!level || isStarting.value) return
  isStarting.value = true
  startError.value = ''
  try {
    await action(level)
  } catch (err) {
    startError.value = err instanceof Error ? err.message : failureMessage
  } finally {
    isStarting.value = false
  }
}

function onStart() {
  void runSelectedAction(startCampaignLevel, 'Failed to start level.')
}

function onLobby() {
  void runSelectedAction(openCampaignLobby, 'Failed to open lobby.')
}

async function loadMapCatalog() {
  try {
    mapCatalog.value = await fetchMapCatalog()
  } catch (err) {
    mapCatalogLoadError.value = err instanceof Error ? err.message : 'Failed to load map previews.'
  }
}

onMounted(() => {
  // Ensures completedCampaignLevels is populated before deriving status. No-op
  // when the profile is already loaded; cheap to call.
  void initProfile()
  // Fetches the campaign catalog from the server (cached at the module level
  // by useCampaign, so re-mounts are free).
  void initCampaigns()
  void loadMapCatalog()
})
</script>

<style scoped>
/* Parchment-panel wrapper. Fills the war-room parchment slot; the UiPanel
   itself draws the 9-slice parchment border-image via its own scoped CSS.
   `inset: 0` makes it cover the slot, and `display: flex` lets the inner
   content stretch to the available space inside the border. */
.campaign {
  position: absolute;
  inset: 0;
  display: flex;
  box-sizing: border-box;
}

/* Close X — sits in the top-right of the parchment frame. Anchored to the
   panel root so it floats over the inner scroll content and stays visible
   while the level list scrolls. */
.campaign__close {
  position: absolute;
  top: calc(var(--s) * 18);
  right: calc(var(--s) * 22);
  z-index: 2;
  width: calc(var(--s) * 36);
  height: calc(var(--s) * 36);
  display: flex;
  align-items: center;
  justify-content: center;
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: calc(var(--s) * 30);
  font-weight: 700;
  line-height: 1;
  color: #3a1f0a;
  background: transparent;
  border: 0;
  padding: 0;
  /* Inherits --s from .campaign__inner via the cascade — but the close
     button is a sibling, not a descendant, so we redeclare it here. */
  --s: 0.0929cqw;
}

.campaign__close:hover,
.campaign__close:focus-visible {
  color: #7a3a10;
  outline: none;
}

/* Inner content. The previous .campaign rules live here so the parchment
   image surrounds the layout without inheriting the absolute positioning. */
.campaign__inner {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  padding: 2% 3%;
  box-sizing: border-box;
  color: #3a1f0a;
  /* Single scale unit driving sizes below. Matches the Advancements panel
     so both content slots share visual scale at the same parchment size. */
  --s: 0.0929cqw;
  gap: calc(var(--s) * 12);
  overflow-y: auto;
  min-height: 0;
}

.campaign__header {
  flex: 0 0 auto;
  display: flex;
  justify-content: flex-start;
}

.campaign__title {
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: calc(var(--s) * 28);
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  margin: 0;
}

.campaign__tabs {
  flex: 0 0 auto;
  display: flex;
  gap: calc(var(--s) * 8);
  border-bottom: 1px solid rgba(58, 31, 10, 0.25);
}

.campaign__tab {
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: calc(var(--s) * 16);
  font-weight: 700;
  letter-spacing: 0.05em;
  padding: calc(var(--s) * 6) calc(var(--s) * 12);
  background: transparent;
  border: 0;
  color: rgba(58, 31, 10, 0.6);
  border-bottom: 2px solid transparent;
  display: inline-flex;
  align-items: center;
  gap: calc(var(--s) * 6);
}

.campaign__tab--active {
  color: #3a1f0a;
  border-bottom-color: #8a5a2a;
}

/* Locked: shown but not selectable. Greyed out + a small padlock glyph so
   the user sees "more campaigns coming" without being able to click in. */
.campaign__tab--locked {
  color: rgba(58, 31, 10, 0.35);
  /* `cursor: not-allowed` is the system semantic for "forbidden action" —
     allowed by the project's cursor rules on locked states. */
  cursor: not-allowed;
}

.campaign__tab--locked:hover {
  border-bottom-color: transparent;
}

.campaign__tab-lock {
  font-size: calc(var(--s) * 12);
  line-height: 1;
}

.campaign__active-header {
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: calc(var(--s) * 4);
}

.campaign__active-name {
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: calc(var(--s) * 20);
  font-weight: 700;
  letter-spacing: 0.06em;
}

.campaign__active-desc {
  font-size: calc(var(--s) * 13);
  font-style: italic;
  opacity: 0.85;
}

.campaign__error {
  font-size: calc(var(--s) * 13);
  color: #7a1a1a;
  text-align: center;
}

.campaign__loading {
  font-size: calc(var(--s) * 13);
  font-style: italic;
  color: rgba(58, 31, 10, 0.7);
  text-align: center;
}

/* Two-column body: levels left, map + actions right. Both columns are sized
   in scale units so the layout matches CreateGame's lobby view — the right
   column is wide enough to frame the 240px minimap canvas the same way the
   custom-game lobby does, and the left column is narrowed so the level
   rows don't sprawl across the parchment. `justify-content: center` keeps
   the pair centered if the parchment slot is wider than the two columns
   plus gap. */
.campaign__body {
  flex: 1 1 auto;
  display: grid;
  grid-template-columns:
    minmax(0, calc(var(--s) * 360))
    minmax(0, calc(var(--s) * 480));
  gap: calc(var(--s) * 18);
  justify-content: center;
  min-height: 0;
}

.campaign__levels {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 10);
  min-height: 0;
  overflow-y: auto;
}

.campaign__detail {
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 8);
  min-height: 0;
}

/* `flex: 0 0 auto` keeps the preview wrapper from claiming leftover vertical
   space; without this it grew to fill the column and pushed the objectives
   list down. Now the map frame's natural height defines the wrapper. */
.campaign__preview {
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 6);
}

/* Style the bare MinimapPreview with the same accent border the selected
   level row uses (`#8a5a2a`). The frame stays snug to the canvas in both
   axes — `width: fit-content` + `align-self: flex-start` shrink it
   horizontally, and `height: auto` + `min-height: 0` undo the
   `height: 100%` baked into the base `.minimap-preview` rule so it
   doesn't stretch vertically inside the flex column. Vertical padding is
   fixed at 8px so the frame sits tight around the map; horizontal padding
   mirrors that. */
.campaign__preview :deep(.minimap-preview--bare) {
  align-self: flex-start;
  width: fit-content;
  height: auto;
  min-height: 0;
  border: 1px solid #8a5a2a;
  border-radius: calc(var(--s) * 4);
  background: rgba(245, 234, 210, 0.45);
  padding: 8px;
  box-sizing: border-box;
}

.campaign__preview-error {
  font-size: calc(var(--s) * 11);
  color: #7a1a1a;
  text-align: center;
}

/* Objectives — sits between the map and the action buttons. Static
   placeholder list for now; the checkbox is a CSS-drawn square with the
   same parchment-friendly palette as the level rows. */
.campaign__objectives {
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 4);
}

.campaign__objectives-header {
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: calc(var(--s) * 14);
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: rgba(58, 31, 10, 0.75);
}

.campaign__objectives-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 4);
}

.campaign-objective {
  display: flex;
  align-items: center;
  gap: calc(var(--s) * 8);
  font-size: calc(var(--s) * 13);
  color: #3a1f0a;
}

.campaign-objective__checkbox {
  flex: 0 0 auto;
  width: calc(var(--s) * 14);
  height: calc(var(--s) * 14);
  border: 1px solid rgba(58, 31, 10, 0.7);
  background: rgba(245, 234, 210, 0.4);
  border-radius: calc(var(--s) * 2);
  box-sizing: border-box;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: calc(var(--s) * 12);
  font-weight: 700;
  line-height: 1;
  color: transparent;
}

/* Completed: bronze ✓ on a slightly darker background so the row reads as
   "done". Mirrors the level-row completed treatment. */
.campaign-objective__checkbox--checked {
  background: rgba(200, 180, 110, 0.55);
  border-color: rgba(58, 31, 10, 0.85);
  color: #3a1f0a;
}

/* Required objectives get a small badge so the player knows which gate
   victory. Optional objectives have no badge — they read as bonus tasks. */
.campaign-objective__required-badge {
  margin-left: auto;
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: calc(var(--s) * 9);
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: rgba(58, 31, 10, 0.75);
  border: 1px solid rgba(58, 31, 10, 0.4);
  border-radius: calc(var(--s) * 2);
  padding: calc(var(--s) * 1) calc(var(--s) * 4);
  background: rgba(245, 234, 210, 0.5);
}

.campaign-objective--required .campaign-objective__label {
  font-weight: 600;
}

/* Empty-state hint when a level has no objectives authored. */
.campaign__objectives-empty {
  font-size: calc(var(--s) * 12);
  font-style: italic;
  color: rgba(58, 31, 10, 0.55);
}

.campaign__actions {
  flex: 0 0 auto;
  display: flex;
  flex-direction: row;
  gap: calc(var(--s) * 8);
  align-items: stretch;
}

/* Each button claims an equal share of the row. `flex: 1 1 0` plus the
   existing `min-width` on the action style keeps them from collapsing
   below a readable width even when the right column is tight. */
.campaign__actions .campaign-level__action {
  flex: 1 1 0;
}

.campaign-level {
  width: 100%;
  display: grid;
  grid-template-columns: calc(var(--s) * 40) 1fr;
  gap: calc(var(--s) * 14);
  align-items: center;
  text-align: left;
  padding: calc(var(--s) * 10) calc(var(--s) * 14);
  background: rgba(245, 234, 210, 0.45);
  border: 1px solid rgba(58, 31, 10, 0.25);
  border-radius: calc(var(--s) * 4);
  color: inherit;
  font: inherit;
}

.campaign-level--selected {
  border-color: #8a5a2a;
  box-shadow: 0 0 0 2px rgba(138, 90, 42, 0.45);
}

.campaign-level--completed {
  background: rgba(200, 180, 110, 0.55);
  border-color: rgba(58, 31, 10, 0.45);
}

.campaign-level--completed.campaign-level--selected {
  border-color: #8a5a2a;
}

.campaign-level--locked {
  opacity: 0.6;
}

.campaign-level__index {
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: calc(var(--s) * 22);
  font-weight: 700;
  text-align: center;
  color: rgba(58, 31, 10, 0.65);
}

.campaign-level__body {
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 2);
  min-width: 0;
}

.campaign-level__name-row {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: calc(var(--s) * 12);
}

.campaign-level__name {
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: calc(var(--s) * 18);
  font-weight: 700;
  letter-spacing: 0.04em;
}

.campaign-level__status {
  font-size: calc(var(--s) * 11);
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  opacity: 0.7;
}

.campaign-level--completed .campaign-level__status {
  color: #2d4a16;
  opacity: 1;
}

.campaign-level__desc {
  font-size: calc(var(--s) * 13);
  opacity: 0.85;
}

.campaign-level__action {
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: calc(var(--s) * 13);
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  padding: calc(var(--s) * 6) calc(var(--s) * 18);
  border-radius: calc(var(--s) * 4);
  border: 1px solid rgba(58, 31, 10, 0.55);
  color: #2a1505;
  min-width: calc(var(--s) * 110);
}

.campaign-level__action--start {
  background: linear-gradient(180deg, #d8b06a 0%, #a87a36 100%);
}

.campaign-level__action--lobby {
  background: linear-gradient(180deg, #c0a98a 0%, #8a7350 100%);
}

.campaign-level__action:disabled {
  background: rgba(180, 160, 110, 0.4);
  color: rgba(58, 31, 10, 0.45);
  /* `cursor: not-allowed` is the system semantic for "forbidden action" — the
     project rule (CLAUDE.md → AI_RULES.md) allows it on locked states.
     Other cursor literals are disallowed here. */
  cursor: not-allowed;
}
</style>
