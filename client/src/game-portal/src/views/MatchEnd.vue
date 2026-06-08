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
import { matchEndSnapshot, clearMatchEndSnapshot } from '@/state/matchEndState'
import { campaignSession, clearCampaignSession } from '@/state/campaignSession'
import { markCampaignObjectivesComplete } from '@/services/profileApi'
import { useProfile } from '@/composables/useProfile'

const router = useRouter()
const { refresh: refreshProfile } = useProfile()

const snapshot = computed(() => matchEndSnapshot.value)

// Cold mount with no snapshot — someone navigated to /match-end directly
// (browser back/forward, deep link, refresh after match exit). Send them
// home so they don't sit on a blank screen.
onMounted(() => {
  if (!snapshot.value) {
    void router.replace('/')
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
    const completedIDs = snap.objectives
      .filter((o) => o.completed && !o.failed)
      .map((o) => o.id)
    try {
      await markCampaignObjectivesComplete(
        session.campaignId,
        session.levelId,
        completedIDs,
      )
      // Refresh the profile so the Campaign panel's level-select rows
      // pick up the new ✓ icons before the user navigates back in.
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
