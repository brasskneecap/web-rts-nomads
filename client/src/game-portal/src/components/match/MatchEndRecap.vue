<template>
  <div class="match-end-recap" role="dialog" aria-modal="true" :aria-labelledby="`match-recap-${outcome}-title`">
    <div class="match-end-recap__card" :style="{ '--ui-window-image': `url(${mainWindowPanelUrl})` }">
      <div class="match-end-recap__inner">
      <div :id="`match-recap-${outcome}-title`" class="match-end-recap__title" :class="`match-end-recap__title--${titleVariant}`">
        {{ outcomeTitle }}
      </div>
      <div v-if="levelDisplayName" class="match-end-recap__subtitle">
        {{ levelDisplayName }}
      </div>

      <!-- Objective recap. Same icon scheme as the in-match panel
           (MatchObjectivesPanel.vue) so the player recognises the state
           at a glance. Failed rows show ✗ + strikethrough; required
           objectives keep an emphasis treatment. Empty array (Custom
           Game etc.) hides the whole section. -->
      <section v-if="objectives.length" class="match-end-recap__section">
        <h2 class="match-end-recap__section-title">Objectives</h2>
        <ul class="match-end-recap__objectives">
          <li
            v-for="obj in objectives"
            :key="obj.id"
            class="recap-objective"
            :class="{
              'recap-objective--completed': obj.completed,
              'recap-objective--failed': !!obj.failed,
              'recap-objective--required': !!obj.required,
            }"
          >
            <span class="recap-objective__icon" aria-hidden="true">{{ iconFor(obj) }}</span>
            <span class="recap-objective__label">{{ obj.description || obj.id }}</span>
            <span class="recap-objective__progress">
              {{ obj.current }} / {{ obj.requiredCount }}
            </span>
          </li>
        </ul>
      </section>

      <!-- Per-player metrics table. Real HTML table because the layout is
           literally a table — using CSS grid with `display: contents`
           wrappers led to mis-alignment when child counts diverged. AI
           players (enemy waves, neutral camps) are filtered out upstream
           by `playerColumns`; only real human players land here. -->
      <section v-if="playerColumns.length" class="match-end-recap__section">
        <h2 class="match-end-recap__section-title">Match Statistics</h2>
        <table class="recap-metrics-table">
          <thead>
            <tr>
              <th aria-hidden="true"></th>
              <th v-for="row in metricRows" :key="row.label">
                <span v-if="row.groupLabel" class="recap-metrics-table__group-label">{{ row.groupLabel }}</span>
                {{ row.label }}
              </th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="col in playerColumns"
              :key="col.playerId"
              :class="{ 'recap-metrics-table__row--viewer': col.isViewer }"
            >
              <th scope="row">
                {{ playerNameOf(col.playerId) }}<span v-if="col.isViewer" class="recap-player-name__you"> (You)</span>
              </th>
              <td v-for="row in metricRows" :key="row.label">
                {{ row.read(col.metrics) }}
              </td>
            </tr>
          </tbody>
        </table>
      </section>

      <button
        type="button"
        class="match-end-recap__return"
        aria-label="Return to Menu"
        :style="{ backgroundImage: `url(${menuReturnButtonUrl})` }"
        @click="$emit('close')"
      ></button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { MatchMetricsSnapshot, ObjectiveSnapshot, PlayerSnapshot } from '@/game/network/protocol'
import { ENEMY_PLAYER_ID, NEUTRAL_PLAYER_ID } from '@/game/network/protocol'
import { formatDisplayName } from '@/composables/usePlayer'
import type { MatchEndOutcome } from '@/components/match/matchEndOutcome'
import mainWindowPanelUrl from '@/assets/ui/themes/updated/main-window-panel-lg.png'
import menuReturnButtonUrl from '@/assets/ui/buttons/menu-return-button.png'

const props = defineProps<{
  outcome: MatchEndOutcome
  /** Objectives at the moment the match ended. Failed objectives stay
   *  rendered (strikethrough) so the player sees what they missed. */
  objectives: ObjectiveSnapshot[]
  /** Per-player snapshot blocks. The component sorts viewer-first; AI
   *  players (enemyPlayerID / neutralPlayerID) should already be filtered
   *  out upstream. */
  players: PlayerSnapshot[]
  /** Which playerId is the local viewer. Drives the (You) annotation
   *  and column ordering. */
  viewerId: string
  /** Optional level display name for the subtitle, e.g. "Forest 1 —
   *  Establish a foothold at the forest edge." */
  levelDisplayName?: string
}>()

defineEmits<{
  (e: 'close'): void
}>()

/** Player-facing title text. Forfeit is collapsed into Defeat — exiting
 *  mid-match reads as a loss to the player regardless of how we track it
 *  internally; the distinct `'forfeit'` outcome value still exists on
 *  the wire for future analytics. */
const outcomeTitle = computed(() => {
  switch (props.outcome) {
    case 'victory': return '★  Victory  ★'
    case 'defeat':
    case 'forfeit':
      return 'Defeat'
  }
})

/** CSS modifier class. Mirrors outcomeTitle's collapse so forfeit uses
 *  the defeat color palette. */
const titleVariant = computed(() => {
  return props.outcome === 'victory' ? 'victory' : 'defeat'
})

function iconFor(obj: ObjectiveSnapshot): string {
  if (obj.failed) return '✗'
  if (obj.completed) return '✓'
  return '□'
}

interface PlayerColumn {
  playerId: string
  isViewer: boolean
  metrics: MatchMetricsSnapshot
}

/** Build the per-player column list. Filters out AI sentinels — neutral
 *  camp mobs and wave enemies have metrics blocks too (the engine
 *  initialises NewMatchMetrics() on every Player), but those numbers
 *  belong to nobody and clutter the recap. The viewer column sorts
 *  first so the player's own row anchors the left edge.
 *
 *  Older servers may omit `metrics` (e.g. before §10 landed); default-
 *  coalesce to a zero struct so the row still renders, which reads as
 *  a backend problem to anyone QA'ing rather than a silent crash. */
const playerColumns = computed<PlayerColumn[]>(() => {
  const empty: MatchMetricsSnapshot = {
    totalGoldEarned: 0,
    totalWoodEarned: 0,
    totalEnemiesKilled: 0,
    buildingsBuilt: 0,
    buildingsBuiltByType: {},
    neutralCampsKilled: 0,
    neutralCampsKilledByTier: {},
    unitsTrained: 0,
    unitsTrainedByType: {},
    unitsByRank: {},
    wavesCleared: 0,
  }
  const list: PlayerColumn[] = props.players
    .filter((p) => p.playerId !== ENEMY_PLAYER_ID && p.playerId !== NEUTRAL_PLAYER_ID)
    .map((p) => ({
      playerId: p.playerId,
      isViewer: p.playerId === props.viewerId,
      metrics: p.metrics ?? empty,
    }))
  // Viewer-first, then stable by playerId.
  list.sort((a, b) => {
    if (a.isViewer && !b.isViewer) return -1
    if (!a.isViewer && b.isViewer) return 1
    return a.playerId.localeCompare(b.playerId)
  })
  return list
})

/** Static description of the metrics table's body rows. Each row knows
 *  its label and how to read its value from a MatchMetricsSnapshot.
 *  Centralising this here means the header (player columns) and body
 *  (label + per-player values) stay in lockstep automatically —
 *  there's no way for the column count to diverge from the value
 *  count, which was the failure mode of the old grid layout. */
interface MetricRow {
  label: string
  groupLabel?: string
  read: (m: MatchMetricsSnapshot) => string | number
}

const metricRows: MetricRow[] = [
  { label: 'Buildings Built', read: (m) => m.buildingsBuilt ?? 0 },
  { label: 'Gold Earned', read: (m) => m.totalGoldEarned ?? 0 },
  { label: 'Wood Earned', read: (m) => m.totalWoodEarned ?? 0 },
  { label: 'Enemies Killed', read: (m) => m.totalEnemiesKilled ?? 0 },
  { label: 'Camps Cleared', read: (m) => m.neutralCampsKilled ?? 0 },
  { label: 'Waves Cleared', read: (m) => m.wavesCleared ?? 0 },
  {
    label: 'Bronze / Silver / Gold',
    groupLabel: 'Unit Ranks',
    read: (m) => `${m.unitsByRank?.bronze ?? 0} / ${m.unitsByRank?.silver ?? 0} / ${m.unitsByRank?.gold ?? 0}`,
  },
]

function playerNameOf(playerId: string): string {
  return formatDisplayName(playerId)
}
</script>

<style scoped>
/* Full-screen route layout. The host route (/match-end) paints the
   match-end wood backdrop behind this component; this centres the wood
   panel both axes within that backdrop, leaving a margin so the ornate
   frame of the backdrop shows around it. */
.match-end-recap {
  width: 100%;
  min-height: 100dvh;
  box-sizing: border-box;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #e6d5ac;
  font-family: var(--font-body);
}

/* Wood panel — main-window-panel-lg drawn as a single full-stretch
   background image (NOT a 9-slice), so the thick brass frame doesn't push
   the content box inward. The frame scales with the panel; percentage
   padding on __inner keeps the content clear of the frame at any size.
   Large source art, so it stretches without looking pixelated. */
.match-end-recap__card {
  width: 90vw;
  max-width: 1400px;
  height: 86vh;
  box-sizing: border-box;
  background: var(--ui-window-image) center / 100% 100% no-repeat;
  image-rendering: auto;
  box-shadow: 0 18px 48px rgba(0, 0, 0, 0.7);
}

/* Inner content sits inside the painted frame. Percentage padding tracks
   the frame thickness as the panel scales, so content never rides onto the
   brass border. height:100% + overflow scroll lets the content scroll
   independently on shorter viewports / longer recaps. */
.match-end-recap__inner {
  height: 100%;
  box-sizing: border-box;
  padding: 7% 6%;
  display: flex;
  flex-direction: column;
  gap: 32px;
  color: #e6d5ac;
  overflow-y: auto;
}

.match-end-recap__title {
  font-family: var(--font-title);
  font-size: 56px;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-align: center;
  text-transform: uppercase;
}

/* Victory: deep gold/amber that sings on the parchment without going
   gaudy. The text-shadow gives a soft halo without losing legibility. */
.match-end-recap__title--victory {
  color: #e2b74e;
  text-shadow: 0 0 18px rgba(212, 168, 71, 0.5);
}

/* Defeat: warm brick red, brightened so it reads against the dark wood
   while staying tonal with the brass frame. */
.match-end-recap__title--defeat {
  color: #c85440;
}

.match-end-recap__subtitle {
  font-family: var(--font-title);
  font-size: 18px;
  letter-spacing: 0.08em;
  text-align: center;
  color: rgba(230, 213, 172, 0.75);
  margin-top: -16px;
}

.match-end-recap__section {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.match-end-recap__section-title {
  font-family: var(--font-title);
  font-size: 18px;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  text-align: center;
  color: #c9a24a;
  margin: 0;
  border-bottom: 1px solid rgba(201, 162, 74, 0.3);
  padding-bottom: 8px;
}

.match-end-recap__objectives {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.recap-objective {
  display: grid;
  grid-template-columns: 24px 1fr auto;
  align-items: center;
  gap: 14px;
  font-size: 16px;
  color: #e6d5ac;
}

.recap-objective__icon {
  font-family: var(--font-title);
  font-weight: 700;
  text-align: center;
  font-size: 20px;
  color: rgba(230, 213, 172, 0.55);
}

/* Completed objectives: bright green that reads as "achievement" on the
   dark wood. Failed objectives: brick red, matching the defeat title. */
.recap-objective--completed .recap-objective__icon { color: #8fbf4a; }
.recap-objective--failed .recap-objective__icon { color: #c85440; }

.recap-objective__progress {
  font-size: 14px;
  font-variant-numeric: tabular-nums;
  color: rgba(230, 213, 172, 0.7);
}

.recap-objective--required .recap-objective__label { font-weight: 700; }
.recap-objective--failed .recap-objective__label {
  text-decoration: line-through;
  color: rgba(230, 213, 172, 0.45);
}

/* Metrics table. Real HTML table — labels in the first column, one column
   per player, each metric is one row. Browser handles row alignment so
   the column count can't drift from the value count. */
.recap-metrics-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 15px;
  font-variant-numeric: tabular-nums;
}

.recap-metrics-table th,
.recap-metrics-table td {
  padding: 10px 18px;
  text-align: right;
  color: #e6d5ac;
  border-bottom: 1px solid rgba(201, 162, 74, 0.14);
}

.recap-metrics-table tbody tr:last-child th,
.recap-metrics-table tbody tr:last-child td {
  border-bottom: none;
}

/* Column headers now hold metric labels (Buildings Built, Gold Earned…).
   These are secondary descriptive labels, so they read a bit lighter and
   smaller than the player-name row headers below. */
.recap-metrics-table thead th {
  font-family: var(--font-body);
  font-size: 14px;
  font-weight: 600;
  letter-spacing: 0.06em;
  color: #c9a24a;
  border-bottom: 1px solid rgba(201, 162, 74, 0.4);
  padding-top: 4px;
  padding-bottom: 12px;
  white-space: nowrap;
  /* Bottom-align so every metric label sits on the same baseline row.
     The "Unit Ranks" column carries a group label stacked above it,
     making that cell two lines tall; without this, the single-line
     labels (Waves Cleared, etc.) center against it and drift upward. */
  vertical-align: bottom;
}

.recap-metrics-table__group-label {
  display: block;
  text-align: center;
  font-family: var(--font-title);
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: rgba(201, 162, 74, 0.75);
  margin-bottom: 2px;
}

/* Row headers now hold player names. These anchor the table — bold,
   left-aligned, in the Cinzel display face. The viewer's row uses the
   victory-gold accent so they can spot their own line at a glance. */
.recap-metrics-table tbody th {
  text-align: left;
  font-family: var(--font-title);
  font-size: 16px;
  font-weight: 700;
  letter-spacing: 0.06em;
  color: #e6d5ac;
  padding-right: 24px;
}

.recap-metrics-table__row--viewer th,
.recap-metrics-table__row--viewer td {
  color: #e2b74e;
}

.recap-player-name__you {
  font-weight: 400;
  font-style: italic;
  color: rgba(230, 213, 172, 0.6);
  text-transform: none;
}

/* Return button — the label + lion crest are baked into the PNG art
   (menu-return-button.png, 503×128), so the button is the image itself.
   The text lives on aria-label for screen readers. aspect-ratio keeps
   it proportional as the width scales with the card. */
.match-end-recap__return {
  /* Pin toward the bottom of the panel: margin-top:auto pushes it to the
     end of the flex column (absorbing free space when the recap is short),
     and margin-bottom + the inner's 40px padding leaves ~100px above the
     panel's bottom edge. If the recap overflows, the auto margin collapses
     and the button flows after the scrolling content instead of overlapping. */
  margin-top: auto;
  margin-bottom: 60px;
  align-self: center;
  width: clamp(260px, 32%, 420px);
  aspect-ratio: 503 / 128;
  padding: 0;
  border: none;
  background-color: transparent;
  background-repeat: no-repeat;
  background-position: center;
  background-size: contain;
  transition: filter 120ms ease;
}

.match-end-recap__return:hover {
  filter: brightness(1.15);
}

.match-end-recap__return:active {
  filter: brightness(1.05);
}
</style>
