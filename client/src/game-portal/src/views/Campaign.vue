<template>
  <!-- Outer world-menu frame (black chrome), mirroring Custom Game: brass
       header bar, button-art tabs, dark inner panels, footer with Back. -->
  <UiPanel variant="worldMenu" :padding="0" class="campaign" :style="assetVars">
    <div class="campaign__frame">
      <header class="campaign__titlebar">
        <span class="campaign__title">Campaigns</span>
      </header>

      <!-- In-panel lobby view. When the player clicks Lobby on a level, the
           campaign lobby is hosted here inline; its Back button pops back to
           the level list (@back → view = 'levels'). -->
      <PanelLobby
        v-if="view === 'lobby'"
        class="campaign__lobby"
        :lobby-id="activeLobbyId"
        @back="view = 'levels'"
      />

      <template v-else>
        <!-- Campaign tabs. Active tab uses the blue war-room button art, others
             the dark art. Locked campaigns advertise upcoming content. -->
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

        <div class="campaign__content">
          <div v-if="startError" class="campaign__error" role="alert">{{ startError }}</div>
          <div v-if="catalogLoadError" class="campaign__error" role="alert">{{ catalogLoadError }}</div>
          <div
            v-if="isCatalogLoading && campaignsView.length === 0"
            class="campaign__loading"
          >
            Loading campaigns…
          </div>

          <UiPanel
            v-if="activeCampaign"
            variant="warRoomInner"
            :padding="0"
            class="campaign__panel"
          >
            <div class="campaign__body">
              <!-- Left: level list (the active campaign is named by its tab). -->
              <div class="campaign__left">
                <UiPanel variant="innerPanel" :padding="0" class="campaign__levels-panel">
                  <GameScrollArea class="campaign__levels-scroll">
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
                  </GameScrollArea>
                </UiPanel>
              </div>

              <!-- Right: map preview (left) with objectives beside it (right). -->
              <div class="campaign__right">
                <div class="campaign__preview-col">
                  <UiPanel variant="worldInner" :padding="0" class="campaign__preview-panel">
                    <div class="campaign__preview">
                      <MinimapPreview
                        :map="selectedMap"
                        :show-metadata="false"
                        :max-display-size="200"
                      />
                    </div>
                  </UiPanel>
                  <div v-if="mapCatalogLoadError" class="campaign__preview-error">
                    {{ mapCatalogLoadError }}
                  </div>
                </div>

                <UiPanel variant="innerPanel" :padding="0" class="campaign__objectives-panel">
                  <div class="campaign__objectives">
                    <div class="campaign__objectives-header">
                      <span class="campaign__objectives-header-title">Objectives</span>
                      <span
                        v-if="anyObjectiveReward"
                        class="campaign__objectives-header-rewards"
                      >Rewards</span>
                    </div>
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
                          {{ obj.description || obj.id }}<span
                            v-if="obj.required"
                            class="campaign-objective__required-note"
                          > (required)</span>
                        </span>
                        <span class="campaign-objective__reward-cell">
                          <span
                            v-if="(obj.rewardDominionPoints ?? 0) > 0"
                            class="campaign-objective__reward"
                            title="Dominion Points, awarded the first time you complete this objective"
                          >{{ obj.rewardDominionPoints }} DP</span>
                        </span>
                        <span class="campaign-objective__reward-cell">
                          <span
                            v-if="(obj.rewardConquestBadges ?? 0) > 0"
                            class="campaign-objective__reward campaign-objective__reward--badge"
                            title="Conquest Badges, awarded the first time you complete this objective"
                          >{{ obj.rewardConquestBadges }}<img :src="badgeIconUrl" class="campaign-objective__reward-icon" alt="Conquest Badges" /></span>
                        </span>
                      </li>
                    </ul>
                  </div>
                </UiPanel>
              </div>
            </div>
          </UiPanel>
        </div>

        <!-- Footer: Back (left) + level actions (right). -->
        <div class="campaign__footer">
          <BackButton @click="emit('close')" />
          <div class="campaign__footer-right">
            <button
              type="button"
              class="cg-btn cg-btn--lobby"
              :disabled="!selectedLevelView || selectedLevelView.status === 'locked' || isStarting"
              :aria-label="selectedLevelView ? `Open lobby for ${selectedLevelView.level.displayName}` : 'Lobby'"
              @click="onLobby"
            >
              <span class="cg-btn__label">Lobby</span>
            </button>
            <button
              type="button"
              class="cg-btn cg-btn--start"
              :disabled="!selectedLevelView || selectedLevelView.status === 'locked' || isStarting"
              :aria-label="selectedLevelView ? `Start ${selectedLevelView.level.displayName}` : 'Start'"
              @click="onStart"
            >
              <span class="cg-btn__label">{{ isStarting ? 'Starting…' : 'Start' }}</span>
            </button>
          </div>
        </div>
      </template>
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
import GameScrollArea from '@/components/ui/GameScrollArea.vue'
import MinimapPreview from '@/components/menu/MinimapPreview.vue'
import PanelLobby from '@/components/menu/PanelLobby.vue'
import BackButton from '@/components/menu/custom-game/BackButton.vue'
import badgeIconUrl from '@/assets/ui/buttons/war_room/advancement/medal-slot.png'
import activeBtnUrl from '@/assets/ui/themes/updated/war-room/war-room-active-button.png'
import inactiveBtnUrl from '@/assets/ui/themes/updated/war-room/war-room-inactive-button.png'
import headerUrl from '@/assets/ui/themes/updated/world-panel-header.png'
import levelRowUrl from '@/assets/ui/themes/updated/war-room/war-room-inner-panel.png'

const emit = defineEmits<{
  (e: 'close'): void
}>()

// Asset URLs exposed to scoped CSS as custom properties.
const assetVars = computed(() => ({
  '--cg-header': `url(${headerUrl})`,
  '--camp-active': `url(${activeBtnUrl})`,
  '--camp-inactive': `url(${inactiveBtnUrl})`,
  '--camp-level': `url(${levelRowUrl})`,
}))

const {
  campaignsView,
  isLoading: isCatalogLoading,
  loadError: catalogLoadError,
  initialize: initCampaigns,
  startCampaignLevel,
  createCampaignLobby,
  isObjectiveCompletedForLevel,
} = useCampaign()

// Which sub-view the panel is showing. 'levels' is the campaign level list;
// 'lobby' hosts the created lobby inline (PanelLobby) so the Lobby button never
// leaves the war-room. `activeLobbyId` is the lobby the in-panel view polls
// while `view === 'lobby'`.
const view = ref<'levels' | 'lobby'>('levels')
const activeLobbyId = ref<string>('')
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

// True when at least one objective on the selected level pays out a reward, so
// the "Rewards" column header only shows when there's something under it.
const anyObjectiveReward = computed(() =>
  selectedLevelObjectives.value.some(
    (o) => (o.rewardDominionPoints ?? 0) > 0 || (o.rewardConquestBadges ?? 0) > 0,
  ),
)

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

/** Lobby button: create the campaign lobby and host it inline in this panel
 *  (view = 'lobby') instead of routing to /lobby/:id. The in-panel lobby's
 *  Back button pops back to the level list. */
async function onLobby() {
  const level = selectedLevelView.value?.level
  if (!level || isStarting.value) return
  isStarting.value = true
  startError.value = ''
  try {
    activeLobbyId.value = await createCampaignLobby(level)
    view.value = 'lobby'
  } catch (err) {
    startError.value = err instanceof Error ? err.message : 'Failed to open lobby.'
  } finally {
    isStarting.value = false
  }
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
.campaign {
  position: absolute;
  inset: 0;
  display: flex;
  box-sizing: border-box;
}

/* Frame stacks the header bar, tab strip, content and footer. --s (the
   container-query scale unit) is declared here so all children share one
   scale, matching Custom Game. */
.campaign__frame {
  position: relative;
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  align-items: stretch;
  min-height: 0;
  min-width: 0;
  --s: 0.0929cqw;
  gap: calc(var(--s) * 12);
  color: #e9dbb8;
}

/* Header bar — the world-panel-header art (shield + wood plaque), raised so it
   straddles the panel's top edge (same as Custom Game). */
.campaign__titlebar {
  position: relative;
  align-self: center;
  flex: 0 0 auto;
  width: min(100%, calc(var(--s) * 760));
  aspect-ratio: 740 / 140;
  background: var(--cg-header) center / 100% 100% no-repeat;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-top: calc(var(--s) * -44 - 40px);
  z-index: 2;
}

.campaign__title {
  font-family: var(--font-title);
  font-size: calc(var(--s) * 34);
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #e7c88a;
  text-shadow:
    0 1px 2px rgba(0, 0, 0, 0.85),
    0 0 12px rgba(212, 168, 71, 0.3);
  transform: translate(25px, calc(var(--s) * 8));
}

/* Tab strip — button-art plaques (border-image keeps the brass frame crisp). */
.campaign__tabs {
  flex: 0 0 auto;
  display: flex;
  gap: calc(var(--s) * 10);
  justify-content: flex-start;
  padding: 0 calc(var(--s) * 6);
  flex-wrap: wrap;
}

.campaign__tab {
  flex: 0 0 auto;
  min-width: calc(var(--s) * 150);
  padding: calc(var(--s) * 6) calc(var(--s) * 20);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: calc(var(--s) * 6);
  background: none;
  border: calc(var(--s) * 14) solid transparent;
  border-image-source: var(--camp-inactive);
  border-image-slice: 14 fill;
  border-image-width: calc(var(--s) * 14);
  border-image-repeat: stretch;
  image-rendering: pixelated;
  transition:
    filter 120ms ease,
    transform 80ms ease;
}

.campaign__tab-label {
  font-family: var(--font-title);
  font-size: calc(var(--s) * 16);
  font-weight: 700;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: rgba(233, 219, 184, 0.6);
  white-space: nowrap;
}

.campaign__tab:not(.campaign__tab--active):not(.campaign__tab--locked):hover {
  filter: brightness(1.15);
}

.campaign__tab:not(.campaign__tab--active):not(.campaign__tab--locked):active {
  filter: brightness(0.9);
  transform: translateY(1px);
}

.campaign__tab:hover .campaign__tab-label {
  color: #e9dbb8;
}

.campaign__tab--active {
  border-image-source: var(--camp-active);
}

.campaign__tab--active .campaign__tab-label {
  color: #f4e3b6;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.7);
}

/* Locked: shown but not selectable. */
.campaign__tab--locked {
  filter: grayscale(0.5) brightness(0.7);
  /* `cursor: not-allowed` is the system semantic for "forbidden action" —
     allowed by the project's cursor rules on locked states. */
  cursor: not-allowed;
}

.campaign__tab--locked .campaign__tab-label {
  color: rgba(233, 219, 184, 0.4);
}

.campaign__tab-lock {
  font-size: calc(var(--s) * 12);
  line-height: 1;
}

/* Content region fills the frame between the tabs and the footer. */
.campaign__content {
  flex: 1 1 auto;
  min-height: 0;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 6);
  padding: 0 calc(var(--s) * 6);
}

.campaign__error {
  font-size: calc(var(--s) * 13);
  color: #e88a6a;
  text-align: center;
}

.campaign__loading {
  font-size: calc(var(--s) * 13);
  font-style: italic;
  color: rgba(233, 219, 184, 0.7);
  text-align: center;
}

/* Dark inner panel wrapping the two-column body. */
.campaign__panel {
  flex: 1 1 auto;
  min-height: 0;
  min-width: 0;
  display: flex;
}

.campaign__body {
  flex: 1 1 auto;
  display: grid;
  grid-template-columns:
    minmax(0, calc(var(--s) * 320))
    minmax(0, 1fr);
  grid-template-rows: minmax(0, 1fr);
  gap: calc(var(--s) * 16);
  padding: calc(var(--s) * 14) calc(var(--s) * 16);
  min-height: 0;
}

/* Left column — campaign intro + level list. */
.campaign__left {
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 6);
  min-height: 0;
  min-width: 0;
}

.campaign__levels-panel {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
}

.campaign__levels-scroll {
  flex: 1 1 auto;
  min-height: 0;
  padding: calc(var(--s) * 6);
  box-sizing: border-box;
}

.campaign__levels {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 6);
}

/* Each level row is its own war-room-inner-panel plaque (border-image keeps the
   brass corner brackets crisp; the dark wood fills the row). */
.campaign-level {
  width: 100%;
  display: grid;
  grid-template-columns: calc(var(--s) * 36) 1fr;
  gap: calc(var(--s) * 12);
  align-items: center;
  text-align: left;
  padding: calc(var(--s) * 2) calc(var(--s) * 6);
  background: none;
  border: calc(var(--s) * 14) solid transparent;
  border-image-source: var(--camp-level);
  border-image-slice: 24 fill;
  border-image-width: calc(var(--s) * 14);
  border-image-repeat: stretch;
  image-rendering: pixelated;
  color: inherit;
  font: inherit;
  transition: filter 120ms ease;
}

.campaign-level:hover {
  filter: brightness(1.12);
}

.campaign-level--selected {
  filter: brightness(1.3);
}

.campaign-level--locked {
  opacity: 0.55;
}

.campaign-level__index {
  font-family: var(--font-title);
  font-size: calc(var(--s) * 20);
  font-weight: 700;
  text-align: center;
  color: rgba(224, 189, 127, 0.75);
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
  font-family: var(--font-title);
  font-size: calc(var(--s) * 16);
  font-weight: 700;
  letter-spacing: 0.04em;
  color: #f0e2c0;
}

.campaign-level__status {
  font-size: calc(var(--s) * 10);
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: rgba(233, 219, 184, 0.6);
}

.campaign-level--completed .campaign-level__status {
  color: #a8d08a;
}

.campaign-level__desc {
  font-size: calc(var(--s) * 12);
  color: rgba(233, 219, 184, 0.75);
}

/* Right area — map preview (left) with objectives beside it (right). */
.campaign__right {
  display: flex;
  flex-direction: row;
  gap: calc(var(--s) * 12);
  min-height: 0;
  min-width: 0;
}

.campaign__preview-col {
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 6);
  min-height: 0;
}

.campaign__preview-panel {
  flex: 0 0 auto;
  display: flex;
}

.campaign__preview {
  flex: 1 1 auto;
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 0;
}

.campaign__preview :deep(.minimap-preview--bare) {
  width: fit-content;
  height: auto;
  min-height: 0;
  border: 0;
  background: transparent;
  padding: 0;
  box-sizing: border-box;
}

.campaign__preview :deep(.minimap-preview__empty--bare) {
  color: rgba(233, 219, 184, 0.5);
}

.campaign__preview-error {
  font-size: calc(var(--s) * 11);
  color: #e88a6a;
  text-align: center;
}

.campaign__objectives-panel {
  flex: 1 1 auto;
  min-height: 0;
  min-width: 0;
  display: flex;
}

/* Objectives — gold labels, cream values on the dark panel. */
.campaign__objectives {
  --obj-grid: auto minmax(0, 1fr) calc(var(--s) * 58) calc(var(--s) * 48);
  --obj-col-gap: calc(var(--s) * 8);
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 4);
  padding: calc(var(--s) * 10) calc(var(--s) * 12);
}

.campaign__objectives-header {
  display: grid;
  grid-template-columns: var(--obj-grid);
  column-gap: var(--obj-col-gap);
  align-items: end;
  font-family: var(--font-title);
  font-size: calc(var(--s) * 12);
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #c7a768;
}

.campaign__objectives-header-title {
  grid-column: 1 / 3;
}

.campaign__objectives-header-rewards {
  grid-column: 3 / 5;
  font-size: calc(var(--s) * 10);
  letter-spacing: 0.12em;
  color: rgba(199, 167, 104, 0.75);
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
  display: grid;
  grid-template-columns: var(--obj-grid);
  column-gap: var(--obj-col-gap);
  align-items: center;
  font-size: calc(var(--s) * 12);
  color: #e9dbb8;
}

.campaign-objective__reward-cell {
  display: flex;
  align-items: center;
  min-width: 0;
}

.campaign-objective__checkbox {
  flex: 0 0 auto;
  width: calc(var(--s) * 14);
  height: calc(var(--s) * 14);
  border: 1px solid rgba(198, 158, 90, 0.6);
  background: rgba(0, 0, 0, 0.4);
  border-radius: calc(var(--s) * 2);
  box-sizing: border-box;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: calc(var(--s) * 11);
  font-weight: 700;
  line-height: 1;
  color: transparent;
}

.campaign-objective__checkbox--checked {
  background: rgba(160, 120, 50, 0.6);
  border-color: rgba(224, 189, 127, 0.9);
  color: #f4e3b6;
}

.campaign-objective__reward {
  display: inline-flex;
  align-items: center;
  gap: calc(var(--s) * 3);
  flex: 0 0 auto;
  font-family: var(--font-title);
  font-size: calc(var(--s) * 10);
  font-weight: 700;
  letter-spacing: 0.04em;
  color: #e6d3a3;
  border: 1px solid rgba(198, 158, 90, 0.45);
  border-radius: calc(var(--s) * 2);
  padding: calc(var(--s) * 1) calc(var(--s) * 4);
  background: rgba(0, 0, 0, 0.35);
  white-space: nowrap;
}

.campaign-objective__reward-icon {
  width: calc(var(--s) * 14);
  height: calc(var(--s) * 14);
  object-fit: contain;
}

.campaign-objective__required-note {
  font-weight: 600;
  color: rgba(233, 219, 184, 0.6);
}

.campaign-objective--required .campaign-objective__label {
  font-weight: 600;
}

.campaign__objectives-empty {
  font-size: calc(var(--s) * 12);
  font-style: italic;
  color: rgba(233, 219, 184, 0.55);
}

/* Footer — Back (left) + level actions (right). Matches the Custom Game
   footer geometry so the Back button stays put. */
.campaign__footer {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: calc(var(--s) * 12);
  padding: calc(var(--s) * 12) calc(var(--s) * 6) calc(var(--s) * 6);
}

.campaign__footer-right {
  display: flex;
  align-items: center;
  gap: calc(var(--s) * 12);
}

/* Footer action buttons use the war-room button art. */
.cg-btn {
  flex: 0 0 auto;
  min-width: calc(var(--s) * 130);
  padding: calc(var(--s) * 4) calc(var(--s) * 16);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: none;
  border: calc(var(--s) * 16) solid transparent;
  border-image-slice: 14 fill;
  border-image-width: calc(var(--s) * 16);
  border-image-repeat: stretch;
  image-rendering: pixelated;
  transition:
    filter 120ms ease,
    transform 80ms ease;
}

.cg-btn--start {
  border-image-source: var(--camp-active);
}

.cg-btn--lobby {
  border-image-source: var(--camp-inactive);
}

.cg-btn__label {
  font-family: var(--font-title);
  font-size: calc(var(--s) * 15);
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #f4e3b6;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.7);
}

.cg-btn:hover:not(:disabled) {
  filter: brightness(1.12);
}

.cg-btn:active:not(:disabled) {
  filter: brightness(0.9);
  transform: translateY(1px);
}

.cg-btn:disabled {
  /* `cursor: not-allowed` is the system semantic for "forbidden action" — the
     project rule (CLAUDE.md → AI_RULES.md) allows it on disabled states. */
  cursor: not-allowed;
  filter: grayscale(0.4) brightness(0.8);
}

.cg-btn:disabled .cg-btn__label {
  color: rgba(244, 227, 182, 0.4);
}
</style>
