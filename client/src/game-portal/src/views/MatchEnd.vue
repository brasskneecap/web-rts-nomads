<template>
  <div class="match-end-view">
    <MatchEndRecap
      v-if="snapshot"
      :outcome="snapshot.outcome"
      :objectives="snapshot.objectives"
      :players="snapshot.players"
      :viewer-id="snapshot.viewerId"
      :level-display-name="snapshot.levelDisplayName"
      @close="onClose"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import MatchEndRecap from '@/components/match/MatchEndRecap.vue'
import { matchEndSnapshot, clearMatchEndSnapshot, matchEndDpPersisted } from '@/state/matchEndState'
import { campaignSession, clearCampaignSession } from '@/state/campaignSession'
import { markCampaignObjectivesComplete, awardMatchDominionPoints, isRemoteProxyClient } from '@/services/profileApi'
import { useProfile } from '@/composables/useProfile'

const router = useRouter()
const { refresh: refreshProfile } = useProfile()

const snapshot = computed(() => matchEndSnapshot.value)

onMounted(() => {
  const snap = snapshot.value
  if (!snap) {
    // Cold mount with no snapshot — bounce home so the user isn't stranded.
    void router.replace('/')
    return
  }

  // Remote joiner only: the host already committed the host player's DP
  // server-side, and could only write the joiner's DP to the host's disk —
  // so the joiner persists its own earned total to ITS local profile here.
  // Single-player / host take no action (server-side commit is authoritative).
  // Idempotent on the server by matchId; the local guard prevents a re-mount
  // from firing a second request.
  if (
    !matchEndDpPersisted.value &&
    isRemoteProxyClient() &&
    snap.matchId &&
    snap.dominionPointsEarned > 0
  ) {
    matchEndDpPersisted.value = true
    void awardMatchDominionPoints(snap.matchId, snap.dominionPointsEarned)
      .then(() => refreshProfile())
      .catch((err) => console.error('[MatchEnd] failed to persist dominion points:', err))
  }
})

/** Canonical post-recap exit. Writes the batch of completed objectives
 *  to the profile when this was a campaign match, then clears the
 *  module-level state so a future match starts clean.
 *
 *  The write is awaited because the user is staring at a button waiting
 *  for navigation — a small delay is fine, and we'd rather not lose the
 *  write to a fast tab close. Errors are logged but never block the
 *  navigation; the server endpoint is idempotent so a follow-up call
 *  from a future session would still record any missed completions. */
async function onClose() {
  const session = campaignSession.value
  const snap = snapshot.value
  if (session && snap) {
    const completedObjectives = snap.objectives
      .filter((o) => o.completed && !o.failed)
      .map((o) => ({ id: o.id, rewardDominionPoints: o.rewardDominionPoints ?? 0, rewardConquestBadges: o.rewardConquestBadges ?? 0 }))
    try {
      await markCampaignObjectivesComplete(
        session.campaignId,
        session.levelId,
        completedObjectives,
      )
      // Refresh the profile so the Campaign panel's level-select rows AND the
      // Dominion Point balance pick up the first-completion reward before the
      // user navigates back.
      await refreshProfile()
    } catch (err) {
      console.error('[Campaign] failed to record completed objectives:', err)
    }
  }
  // Capture the session presence BEFORE we clear it, so we can still
  // decide the destination after the campaign-session ref is gone.
  const destination = session ? '/war-room' : '/'
  clearCampaignSession()
  clearMatchEndSnapshot()
  void router.push(destination)
}
</script>

<style scoped>
.match-end-view {
  position: relative;
  width: 100%;
  min-height: 100dvh;
  overflow-y: auto;
  background:
    radial-gradient(circle at top, rgba(36, 55, 87, 0.35), transparent 48%),
    #05080d;
}
</style>
