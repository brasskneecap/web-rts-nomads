<template>
  <div class="match-view">
    <div v-if="showResumePrompt" class="menu">
      <div class="menu-title">Return to previous game?</div>
      <div class="menu-text">
        You have a saved player session on
        <strong>{{ resumeMapName }}</strong>.
      </div>

      <div class="menu-actions">
        <button @click="returnToPreviousGame">Return</button>
        <button @click="startNewGame">Start New Game</button>
      </div>
    </div>

    <!-- Campaign-victory popup: opens once when all required objectives
         flip to complete. Sticky-dismissed after "Continue Playing" so it
         never re-shows; "Exit" routes to the recap. Only relevant for
         matches with required objectives (the watcher gate). -->
    <CampaignVictoryModal
      v-if="hasStarted && victoryPopupOpen"
      @continue="onCampaignVictoryContinue"
      @exit="onCampaignVictoryExit"
    />

    <!-- The end-of-match recap is now a separate route (/match-end). The
         watcher on `endOfMatchOutcome` below captures the recap data and
         navigates over there. No overlay markup here. -->


    <div
      v-if="hasStarted && (connectionState === 'reconnecting' || connectionState === 'failed')"
      class="disconnect-overlay"
      role="dialog"
      aria-modal="true"
      :aria-labelledby="connectionState === 'reconnecting' ? 'disconnect-title-reconnecting' : 'disconnect-title-failed'"
      :aria-describedby="connectionState === 'reconnecting' ? 'disconnect-desc-reconnecting' : 'disconnect-desc-failed'"
    >
      <div class="disconnect-card">
        <template v-if="connectionState === 'reconnecting'">
          <div id="disconnect-title-reconnecting" class="disconnect-title">Connection Lost</div>
          <div id="disconnect-desc-reconnecting" class="disconnect-desc">
            Reconnecting...
            <span v-if="reconnectAttempt > 0">(attempt {{ reconnectAttempt }} of {{ maxReconnectAttempts }})</span>
          </div>
          <div class="disconnect-spinner" aria-hidden="true"></div>
        </template>

        <template v-else>
          <div id="disconnect-title-failed" class="disconnect-title disconnect-title--failed">
            Unable to Reconnect
          </div>
          <div id="disconnect-desc-failed" class="disconnect-desc">
            Could not reach the server after {{ maxReconnectAttempts }} attempts.
          </div>
          <div class="disconnect-actions">
            <button type="button" class="disconnect-button disconnect-button--retry" @click="retryReconnect">
              Retry
            </button>
            <!-- Routes through the same forfeit → recap flow as a manual
                 Exit Game so any objectives the player completed before
                 the disconnect still get a write attempt. The recap's
                 `markCampaignObjectivesComplete` call is wrapped in
                 try/catch and won't block exit when the server is
                 unreachable, so the failed-reconnect case still ends
                 cleanly at the main menu. -->
            <button type="button" class="disconnect-button disconnect-button--exit" @click="requestForfeit">
              Return to Menu
            </button>
          </div>
        </template>
      </div>
    </div>

    <InGameHud v-if="hasStarted" :hud="gameClientApi" @exit="requestForfeit">
      <canvas ref="canvas" class="game-canvas"></canvas>
    </InGameHud>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import CampaignVictoryModal from '@/components/match/CampaignVictoryModal.vue'
import type { MatchEndOutcome } from '@/components/match/matchEndOutcome'
import { setMatchEndSnapshot } from '@/state/matchEndState'
import InGameHud from '@/components/InGameHud.vue'
import { useGameClient } from '@/composables/useGameClient'
import { useMapSelection } from '@/composables/useMapSelection'
import { setCursorGrab } from '@/services/desktopBridge'
import { getOrCreatePlayerId, markCampaignLevelComplete } from '@/services/profileApi'
import { campaignSession, clearCampaignSession } from '@/state/campaignSession'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

const MAP_ID_STORAGE_KEY = 'webrts.mapId'
const MATCH_ID_STORAGE_KEY = 'webrts.matchId'
const HAS_ACTIVE_SESSION_KEY = 'webrts.hasActiveSession'
// §14R: joiners arrive via ?proxy=steam — their LOCAL Go server has no
// match registered for the host's matchId, so the preflight
// /matches/<id>/status would 404 → main-menu kick. Detect proxy mode
// and skip the preflight; the WS open will reach the host's hub via
// the parked Steam transport, and the hub does its own join validation.
const STEAM_PROXY_FLAG_KEY = 'webrts.steam.proxyActive'

const router = useRouter()
const route = useRoute()
const canvas = ref<HTMLCanvasElement | null>(null)
const hasStarted = ref(false)

const { selectedMapId, selectedMapName, setSelectedMapId } = useMapSelection()

const hasPreviousSession = ref(
  localStorage.getItem(HAS_ACTIVE_SESSION_KEY) === 'true' &&
    !!localStorage.getItem(MATCH_ID_STORAGE_KEY),
)

const resumeMapName = computed(() => {
  if (selectedMapName.value) return selectedMapName.value
  if (selectedMapId.value) return selectedMapId.value
  return 'Unknown Map'
})

const showResumePrompt = computed(() => !hasStarted.value && hasPreviousSession.value)

const gameClientApi = useGameClient()
const {
  init,
  destroy,
  leaveStoredMatch,
  retryReconnect,
  ui,
  connectionState,
  currentMatchId,
  reconnectAttempt,
  maxReconnectAttempts,
} = gameClientApi

function clearStaleSession() {
  localStorage.removeItem(MAP_ID_STORAGE_KEY)
  localStorage.removeItem(MATCH_ID_STORAGE_KEY)
  localStorage.removeItem(HAS_ACTIVE_SESSION_KEY)
}

async function startGame(mapId: string, options: { resume?: boolean } = {}) {
  if (!canvas.value || !mapId) return
  await init(canvas.value, mapId, options)
  hasStarted.value = true
  localStorage.setItem(MAP_ID_STORAGE_KEY, mapId)
  localStorage.setItem(HAS_ACTIVE_SESSION_KEY, 'true')
}

async function returnToPreviousGame() {
  const mapId = selectedMapId.value || localStorage.getItem(MAP_ID_STORAGE_KEY) || ''
  await startGame(mapId, { resume: true })
}

async function startNewGame() {
  await leaveStoredMatch()
  localStorage.removeItem(MAP_ID_STORAGE_KEY)
  localStorage.removeItem(MATCH_ID_STORAGE_KEY)
  localStorage.removeItem(HAS_ACTIVE_SESSION_KEY)
  hasPreviousSession.value = false
  const mapId = selectedMapId.value
  if (!mapId) {
    void router.push('/')
    return
  }
  await startGame(mapId, { resume: false })
}

async function exitGame() {
  await leaveStoredMatch()
  destroy()
  hasStarted.value = false
  hasPreviousSession.value = false
  localStorage.removeItem(MAP_ID_STORAGE_KEY)
  localStorage.removeItem(MATCH_ID_STORAGE_KEY)
  localStorage.removeItem(HAS_ACTIVE_SESSION_KEY)
  // Reset campaign tracking: a fresh match (campaign or otherwise) should
  // start with no session and no fired-completion guard.
  clearCampaignSession()
  campaignCompletionFired = false
  forfeitRequested.value = false
  setSelectedMapId('', '')
  void router.push('/')
}

/** Reactive flag set by `requestForfeit` (the MatchHud's Exit button while
 *  the match is live). Drives endOfMatchOutcome → 'forfeit', which opens
 *  the recap overlay. The recap's Return-to-Menu button is the canonical
 *  exit path; it calls exitGame directly. Clearing this flag is part of
 *  exitGame so a subsequent campaign launch starts clean. */
const forfeitRequested = ref(false)

/** Combined end-of-match indicator. Precedence:
 *   1. campaign-victory locked: once the player dismissed the victory popup
 *      via "Continue Playing", they've already earned the win — any later
 *      forfeit or defeat resolves to victory so the recap doesn't downgrade
 *      them.
 *   2. forfeit (player clicked Exit during a live match) — even if a later
 *      tick would have produced victory, the forfeit framing wins.
 *   3. victory — server signaled the AND-gate passed.
 *   4. defeat — server signaled the player has no townhalls left.
 *  Returns null while the match is still in progress so the overlay
 *  stays hidden. */
const endOfMatchOutcome = computed<MatchEndOutcome | null>(() => {
  if (!hasStarted.value) return null
  if (victoryPopupDismissed.value && (forfeitRequested.value || ui.value.isDefeated)) {
    return 'victory'
  }
  if (forfeitRequested.value) return 'forfeit'
  if (isVictorious.value) return 'victory'
  if (ui.value.isDefeated) return 'defeat'
  return null
})

/** Click handler for the MatchHud's Exit button. Open the recap with the
 *  Forfeit framing rather than immediately tearing down the match — the
 *  recap's Return-to-Menu is the canonical exit. */
function requestForfeit() {
  if (!hasStarted.value) {
    // Defensive: shouldn't happen because the HUD only renders while
    // hasStarted, but if the user mashes the button during teardown we
    // should still respect the legacy direct-exit semantic.
    void exitGame()
    return
  }
  forfeitRequested.value = true
}

/** Guard so the end-of-match transition fires exactly once per match.
 *  Without this, the `endOfMatchOutcome` watcher would re-fire on every
 *  reactive flip (e.g. a brief snapshot redelivery) and try to navigate
 *  to /match-end multiple times. */
let endTransitioning = false

/** End-of-match transition: capture the recap data into the module-level
 *  `matchEndSnapshot` ref so it survives the route change, tear the
 *  running match down, then push to /match-end. The recap view reads
 *  from `matchEndSnapshot`; the campaign session ref is intentionally
 *  preserved here so MatchEnd.vue can write completed objectives
 *  against the right `{campaignId, levelId}` — MatchEnd's own dismiss
 *  handler clears it after the write. */
async function transitionToMatchEnd(outcome: MatchEndOutcome) {
  setMatchEndSnapshot({
    outcome,
    // Defensive shallow copies — the underlying arrays are reactive and
    // mutated by the network layer; the recap should see a stable
    // post-match view, not the next snapshot's diff.
    objectives: [...ui.value.objectives],
    // Prefer the roster frozen at game-over (immune to teardown clobber);
    // fall back to the live roster for any end path without a freeze.
    players: ui.value.frozenEndPlayers
      ? [...ui.value.frozenEndPlayers]
      : [...ui.value.players],
    viewerId: ui.value.player.playerId ?? '',
    campaignId: campaignSession.value?.campaignId ?? null,
    levelId: campaignSession.value?.levelId ?? null,
    levelDisplayName: campaignSession.value?.levelDisplayName,
    dominionPointsEarned: ui.value.matchDominionPointsEarned,
    matchId: currentMatchId.value,
  })

  // Tear down the running match BUT preserve the campaign session
  // (MatchEnd.vue's dismiss handler is responsible for clearing it
  // after writing completed objectives).
  await leaveStoredMatch()
  destroy()
  hasStarted.value = false
  hasPreviousSession.value = false
  localStorage.removeItem(MAP_ID_STORAGE_KEY)
  localStorage.removeItem(MATCH_ID_STORAGE_KEY)
  localStorage.removeItem(HAS_ACTIVE_SESSION_KEY)
  forfeitRequested.value = false
  campaignCompletionFired = false
  victoryPopupOpen.value = false
  victoryPopupDismissed.value = false
  setSelectedMapId('', '')

  await router.push('/match-end')
}

watch(endOfMatchOutcome, (outcome) => {
  if (!outcome || endTransitioning) return
  endTransitioning = true
  void transitionToMatchEnd(outcome)
})

// Campaign completion hook. When the match enters a victory state AND the
// current match was launched from the campaign panel, mark the level complete
// on the server. Fires at most once per match — a re-trigger of `isVictory`
// from a state edge would otherwise spam the endpoint. The server-side
// handler is idempotent so this is belt-and-braces.
let campaignCompletionFired = false

/** True when the current match defines at least one required objective.
 *  Drives the campaign-victory popup flow: in matches with required
 *  objectives, completing them all opens the popup ("Continue" or "Exit"),
 *  and the legacy auto-transition is suppressed so the player can choose
 *  to keep playing past the win condition. Matches without required
 *  objectives (Custom Game, etc.) keep the legacy wave/server-driven
 *  auto-transition unchanged. */
const hasRequiredObjectives = computed(() =>
  ui.value.objectives.some((o) => o.required),
)

/** True when every required objective is currently `completed`. Empty
 *  required-set returns false so this flag only fires for objective-driven
 *  matches. */
const allRequiredObjectivesComplete = computed(() => {
  const required = ui.value.objectives.filter((o) => o.required)
  if (required.length === 0) return false
  return required.every((o) => o.completed)
})

/** Campaign victory popup state. `victoryPopupDismissed` is sticky for the
 *  match: once the player picks "Continue Playing" we never re-show the
 *  popup, even if the completion flag bounces. */
const victoryPopupOpen = ref(false)
const victoryPopupDismissed = ref(false)

watch(allRequiredObjectivesComplete, (done) => {
  if (!done) return
  if (!hasStarted.value) return
  if (victoryPopupDismissed.value || victoryPopupOpen.value) return
  victoryPopupOpen.value = true
})

function onCampaignVictoryContinue() {
  victoryPopupOpen.value = false
  victoryPopupDismissed.value = true
}

function onCampaignVictoryExit() {
  victoryPopupOpen.value = false
  void transitionToMatchEnd('victory')
}

const isVictorious = computed(() => {
  const u = ui.value
  if (!hasStarted.value) return false
  // Matches with required objectives go through the campaign-victory popup
  // — suppress the legacy auto-transition entirely so "Continue Playing"
  // can actually keep the player in the match.
  if (hasRequiredObjectives.value) return false
  if (u.isVictory) return true
  if (u.wave.enabled && u.wave.state === 'complete' && !u.objectives.length) return true
  return false
})

/** "Match was won" for the purpose of recording campaign progress. Tracks
 *  objective completion directly so progress is logged the moment the
 *  player satisfies the level, regardless of whether they pick Continue
 *  or Exit on the popup. */
const campaignVictoryAchieved = computed(() => {
  if (!hasStarted.value) return false
  if (allRequiredObjectivesComplete.value) return true
  return isVictorious.value
})

watch(campaignVictoryAchieved, (won) => {
  if (!won || campaignCompletionFired) return
  const session = campaignSession.value
  if (!session) return
  campaignCompletionFired = true
  // Fire-and-forget. Failures are logged but not surfaced to the player —
  // the match was won regardless, and the server endpoint is idempotent so
  // a follow-up call from a future session would still record it.
  void markCampaignLevelComplete(session.levelId).catch((err) => {
    console.error('[Campaign] failed to record completion:', err)
  })
})

function markActiveSession() {
  if (hasStarted.value) {
    localStorage.setItem(HAS_ACTIVE_SESSION_KEY, 'true')
  }
}

watch(currentMatchId, (id) => {
  if (id && route.params.matchId !== id) {
    void router.replace({ path: `/match/${id}`, query: route.query })
  }
})

window.addEventListener('beforeunload', markActiveSession)

onMounted(async () => {
  // Confine the OS cursor to the game window for the duration of the
  // match. No-op outside the Tauri shell. Released in onBeforeUnmount so
  // returning to the menu (or any other route) restores normal cursor
  // movement across monitors.
  void setCursorGrab(true)

  const urlMatchId = route.params.matchId as string | undefined
  // §14R: detect Steam-proxy mode. Joiners arrive here via a host's
  // matchId that lives on the HOST's Go server; their own local server
  // returns 404 for the preflight. Skip the preflight and let the WS
  // connect (via ?proxy=steam) reach the host's hub directly. The hub
  // validates membership and will close the connection if the joiner
  // isn't supposed to be there.
  let isSteamProxyJoiner = false
  try {
    isSteamProxyJoiner = sessionStorage.getItem(STEAM_PROXY_FLAG_KEY) === '1'
  } catch {
    /* sessionStorage may be sandboxed */
  }
  console.log('[Match.onMounted]', { urlMatchId, isSteamProxyJoiner })

  if (urlMatchId) {
    // Use the unified player identity (UUID from webrts.profile.id) so the
    // /matches/<id>/status preflight asks the server about the SAME player
    // ID that NetworkClient will use on the WS connect. Reading the legacy
    // `webrts.playerId` key here was the cause of an "isParticipant: false"
    // false negative that bounced the user back to / on every Start Game.
    const playerId = getOrCreatePlayerId()

    if (isSteamProxyJoiner) {
      // Skip the local preflight. We don't have the match locally; the
      // host's hub does. NetworkClient.connect will WS-open with
      // ?proxy=steam and the hub's join_match handler will admit us (or
      // reject us cleanly, in which case we get a normal disconnect).
      const mapId = selectedMapId.value || localStorage.getItem(MAP_ID_STORAGE_KEY) || ''
      localStorage.setItem(MATCH_ID_STORAGE_KEY, urlMatchId)
      localStorage.setItem(HAS_ACTIVE_SESSION_KEY, 'true')
      if (mapId) {
        localStorage.setItem(MAP_ID_STORAGE_KEY, mapId)
        await startGame(mapId, { resume: true })
      } else {
        console.warn('[Match.onMounted] steam-proxy joiner but no mapId; falling back without preferred map')
        // The hub will tell us what map the host is on via the welcome
        // message; pass empty so the server's default catalogue entry
        // applies if NetworkClient happens to need it before welcome.
        await startGame('', { resume: true })
      }
      return
    }

    try {
      const res = await fetch(`${API_BASE}/matches/${encodeURIComponent(urlMatchId)}/status?playerId=${encodeURIComponent(playerId)}`)
      console.log('[Match.onMounted] preflight', { status: res.status, ok: res.ok })
      if (res.status === 404 || !res.ok) {
        console.warn('[Match.onMounted] preflight non-OK; kick to /')
        clearStaleSession()
        void router.push('/')
        return
      }
      const data = await res.json() as { matchId: string; mapId: string; isParticipant: boolean }
      console.log('[Match.onMounted] preflight body', data)
      if (!data.isParticipant) {
        console.warn('[Match.onMounted] not a participant; kick to /')
        clearStaleSession()
        void router.push('/')
        return
      }
      localStorage.setItem(MATCH_ID_STORAGE_KEY, data.matchId)
      localStorage.setItem(MAP_ID_STORAGE_KEY, data.mapId)
      localStorage.setItem(HAS_ACTIVE_SESSION_KEY, 'true')
      setSelectedMapId(data.mapId, '')
      await startGame(data.mapId, { resume: true })
      return
    } catch (e) {
      console.warn('[Match.onMounted] preflight threw; kick to /', e)
      clearStaleSession()
      void router.push('/')
      return
    }
  }

  if (route.query.resume === '1' && hasPreviousSession.value) {
    await returnToPreviousGame()
    return
  }

  if (hasPreviousSession.value) {
    return
  }

  const mapId = selectedMapId.value || localStorage.getItem(MAP_ID_STORAGE_KEY) || ''
  if (!mapId) {
    void router.push('/')
    return
  }

  await startGame(mapId, { resume: false })
})

onBeforeUnmount(() => {
  void setCursorGrab(false)
  window.removeEventListener('beforeunload', markActiveSession)
  destroy()
})
</script>

<style scoped>
.match-view {
  width: 100%;
  height: 100dvh;
  position: relative;
  overflow: hidden;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  background:
    radial-gradient(circle at top, rgba(36, 55, 87, 0.35), transparent 48%),
    #05080d;
}

.menu {
  position: absolute;
  top: 16px;
  left: 16px;
  z-index: 20;
  min-width: 260px;
  background: rgba(0, 0, 0, 0.75);
  color: white;
  padding: 12px;
  border-radius: 8px;
  border: 1px solid rgba(255, 255, 255, 0.12);
  backdrop-filter: blur(10px);
}

.menu-title {
  font-weight: 700;
  margin-bottom: 8px;
}

.menu-text {
  margin-bottom: 10px;
}

.menu-actions {
  display: flex;
  gap: 8px;
  margin-top: 10px;
}

.game-canvas {
  width: 100%;
  height: 100%;
  display: block;
  background: #111;
}

/* .victory-overlay / .victory-card / .victory-title / .victory-subtitle /
   .victory-button classes were removed when the end-of-match recap moved
   to its own /match-end route. Same for the .defeat-* set below. */

.disconnect-overlay {
  position: absolute;
  inset: 0;
  z-index: 60;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(5, 8, 13, 0.78);
  backdrop-filter: blur(4px);
}

.disconnect-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 16px;
  padding: 44px 52px;
  border-radius: 20px;
  background:
    radial-gradient(circle at top, rgba(80, 120, 200, 0.16), transparent 52%),
    linear-gradient(180deg, rgba(16, 22, 38, 0.98), rgba(8, 11, 20, 0.98));
  border: 1px solid rgba(100, 140, 220, 0.25);
  box-shadow:
    inset 0 1px 0 rgba(160, 190, 255, 0.1),
    0 24px 60px rgba(0, 0, 0, 0.65);
  min-width: 320px;
  text-align: center;
}

.disconnect-title {
  font-size: 22px;
  font-weight: 700;
  letter-spacing: 0.06em;
  color: #a8c4f0;
  text-transform: uppercase;
}

.disconnect-title--failed {
  color: #f0a0a0;
}

.disconnect-desc {
  font-size: 14px;
  color: #8899bb;
  line-height: 1.5;
}

.disconnect-spinner {
  width: 28px;
  height: 28px;
  border: 3px solid rgba(100, 140, 220, 0.25);
  border-top-color: #7aabee;
  border-radius: 50%;
  animation: disconnect-spin 0.8s linear infinite;
}

@keyframes disconnect-spin {
  to { transform: rotate(360deg); }
}

.disconnect-actions {
  display: flex;
  gap: 10px;
  margin-top: 4px;
}

.disconnect-button {
  padding: 10px 24px;
  border-radius: 10px;
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.05em;
  cursor: pointer;
  transition: background 0.15s, border-color 0.15s;
}

.disconnect-button--retry {
  background: linear-gradient(180deg, rgba(60, 100, 180, 0.9), rgba(35, 65, 130, 0.95));
  border: 1px solid rgba(100, 150, 240, 0.4);
  color: #ccdeff;
}

.disconnect-button--retry:hover {
  background: linear-gradient(180deg, rgba(80, 120, 200, 1), rgba(50, 85, 155, 1));
  border-color: rgba(130, 175, 255, 0.6);
}

.disconnect-button--exit {
  background: linear-gradient(180deg, rgba(50, 30, 30, 0.9), rgba(30, 18, 18, 0.95));
  border: 1px solid rgba(160, 80, 80, 0.35);
  color: #e0b8b8;
}

.disconnect-button--exit:hover {
  background: linear-gradient(180deg, rgba(80, 40, 40, 1), rgba(50, 28, 28, 1));
  border-color: rgba(200, 100, 100, 0.55);
}

/* .defeat-* rules removed alongside the move to /match-end (see also the
   .victory-* removal note above). */
</style>
