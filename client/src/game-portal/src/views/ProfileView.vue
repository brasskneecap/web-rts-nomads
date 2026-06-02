<template>
  <div class="profile-view">
    <div class="profile-view__layout">
      <div class="profile-view__back-row">
        <UiButton size="sm" @click="router.back()">Back</UiButton>
      </div>

      <MenuPanel class="profile-view__panel">
        <header class="profile-view__header">
          <h1 class="profile-view__title">Player Profile</h1>
        </header>

        <div class="profile-view__tabs" role="tablist" aria-label="Profile sections">
          <UiButton
            size="md"
            :selected="activeTab === 'profile'"
            role="tab"
            :aria-selected="activeTab === 'profile'"
            @click="activeTab = 'profile'"
          >
            Profile
          </UiButton>
          <UiButton
            size="md"
            :selected="activeTab === 'upgrades'"
            role="tab"
            :aria-selected="activeTab === 'upgrades'"
            @click="activeTab = 'upgrades'"
          >
            Upgrades
          </UiButton>
        </div>

        <!-- Profile tab ─────────────────────────────────────────────────── -->
        <div v-if="activeTab === 'profile'" class="profile-view__tab-panel" role="tabpanel">
          <div v-if="isLoading && !profile" class="profile-view__loading" aria-live="polite">
            Loading profile...
          </div>

          <div v-else-if="error && !profile" class="profile-view__error" role="alert">
            {{ error }}
            <UiButton size="sm" @click="initialize">Retry</UiButton>
          </div>

          <div v-else-if="profile" class="profile-view__body">
            <div class="profile-view__left">
              <section class="profile-card" aria-label="Legend Points">
                <div class="profile-card__label">Legend Points</div>
                <div class="profile-card__value profile-card__value--legend">
                  {{ profile.legendPoints.toLocaleString() }}
                </div>
                <div class="profile-card__sub">
                  {{ profile.lifetimeLegendPoints.toLocaleString() }} lifetime
                </div>
                <button
                  type="button"
                  class="profile-card__dev-grant"
                  :disabled="isGrantingLP"
                  title="DEV: grant +50 Legend Points to this profile"
                  @click="grantDevLegendPoints"
                >+50 LP (dev)</button>
              </section>

              <section class="profile-card" aria-label="Commander">
                <div class="profile-card__label">Commander</div>
                <div class="profile-card__value profile-card__value--commander">
                  {{ profile.selectedCommanderId || 'Default' }}
                </div>
              </section>

              <section class="profile-card" aria-label="Match Statistics">
                <div class="profile-card__label">Statistics</div>
                <dl class="profile-stats">
                  <div class="profile-stats__row">
                    <dt>Matches Played</dt>
                    <dd>{{ profile.stats.matchesPlayed }}</dd>
                  </div>
                  <div class="profile-stats__row">
                    <dt>Matches Won</dt>
                    <dd>{{ profile.stats.matchesWon }}</dd>
                  </div>
                  <div class="profile-stats__row">
                    <dt>Matches Lost</dt>
                    <dd>{{ profile.stats.matchesLost }}</dd>
                  </div>
                  <div class="profile-stats__row">
                    <dt>Enemies Killed</dt>
                    <dd>{{ profile.stats.enemiesKilled.toLocaleString() }}</dd>
                  </div>
                  <div class="profile-stats__row">
                    <dt>Objectives Done</dt>
                    <dd>{{ profile.stats.objectivesDone }}</dd>
                  </div>
                  <div class="profile-stats__row">
                    <dt>Win Rate</dt>
                    <dd>{{ winRate }}</dd>
                  </div>
                </dl>
              </section>
            </div>

            <div class="profile-view__right">
              <section class="profile-card" aria-label="Active Upgrade Loadout">
                <ProfileUpgradeLoadoutPicker @switch-tab="activeTab = $event as Tab" />
              </section>
            </div>
          </div>
        </div>

        <!-- Upgrades tab ────────────────────────────────────────────────── -->
        <div v-else-if="activeTab === 'upgrades'" class="profile-view__tab-panel" role="tabpanel">
          <ProfileUpgradesPanel />
        </div>
      </MenuPanel>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useProfile } from '@/composables/useProfile'
import { devGrantLegendPoints } from '@/services/profileApi'
import ProfileUpgradeLoadoutPicker from '@/components/profile/ProfileUpgradeLoadoutPicker.vue'
import ProfileUpgradesPanel from '@/components/profile/ProfileUpgradesPanel.vue'
import MenuPanel from '@/components/menu/MenuPanel.vue'
import UiButton from '@/components/ui/UiButton.vue'

const router = useRouter()
const { profile, isLoading, error, initialize, refresh } = useProfile()

// DEV-only affordance: grants +50 LP to this profile via the dev endpoint and
// re-fetches so the displayed balance updates.
const isGrantingLP = ref(false)
async function grantDevLegendPoints() {
  if (isGrantingLP.value) return
  isGrantingLP.value = true
  try {
    await devGrantLegendPoints(50)
    await refresh()
  } finally {
    isGrantingLP.value = false
  }
}

type Tab = 'profile' | 'upgrades'
const activeTab = ref<Tab>('profile')

onMounted(async () => {
  // initialize() is a one-shot — it primes the buff catalog + tuning on the
  // very first visit. refresh() always re-fetches the profile so mid-match
  // server-side mutations (e.g. immediate LP commits) appear here without a
  // page reload.
  await initialize()
  void refresh()
})

const winRate = computed(() => {
  if (!profile.value || profile.value.stats.matchesPlayed === 0) return '—'
  const rate = profile.value.stats.matchesWon / profile.value.stats.matchesPlayed
  return `${Math.round(rate * 100)}%`
})
</script>

<style scoped>
.profile-view {
  position: relative;
  z-index: 1;
  width: 100%;
  height: 100%;
  overflow: hidden;
}

.profile-view__layout {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 16px;
  padding: 32px;
  height: 100%;
  box-sizing: border-box;
}

.profile-view__back-row {
  width: 100%;
  max-width: 980px;
  display: flex;
  justify-content: flex-start;
}

.profile-view__panel {
  width: 100%;
  max-width: 980px;
  flex: 1 1 auto;
  min-height: 0;
  overflow: hidden;
}

.profile-view__header {
  padding-bottom: 12px;
  margin-bottom: 4px;
  border-bottom: 1px solid rgba(212, 168, 79, 0.35);
}

.profile-view__title {
  margin: 0;
  font-size: 24px;
  font-weight: 700;
  color: #f5ead2;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.profile-view__tabs {
  display: flex;
  gap: 8px;
  padding-bottom: 12px;
}

.profile-view__tab-panel {
  display: flex;
  flex-direction: column;
  gap: 14px;
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: rgba(210, 176, 113, 0.4) transparent;
  /* Inset right padding so the scrollbar doesn't crowd the content. */
  padding-right: 6px;
}

.profile-view__tab-panel::-webkit-scrollbar {
  width: 8px;
}

.profile-view__tab-panel::-webkit-scrollbar-thumb {
  background: rgba(210, 176, 113, 0.4);
  border-radius: 4px;
}

.profile-view__tab-panel::-webkit-scrollbar-thumb:hover {
  background: rgba(210, 176, 113, 0.65);
}

.profile-view__loading {
  color: #a09070;
  font-size: 14px;
  padding: 32px 0;
  text-align: center;
}

.profile-view__error {
  display: flex;
  align-items: center;
  gap: 16px;
  color: #f07070;
  font-size: 14px;
  padding: 16px 0;
}

.profile-view__body {
  display: grid;
  grid-template-columns: 280px 1fr;
  gap: 20px;
  align-items: stretch;
}

.profile-view__left {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.profile-view__right {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.profile-view__right > .profile-card {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.profile-card {
  padding: 16px;
  border-radius: 10px;
  border: 1px solid rgba(200, 164, 106, 0.2);
  background: linear-gradient(180deg, rgba(20, 13, 6, 0.9), rgba(10, 7, 3, 0.95));
}

.profile-card--placeholder {
  text-align: center;
  padding: 40px 24px;
}

.profile-card__label {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: #d7bb84;
  margin-bottom: 8px;
}

.profile-card__value {
  font-size: 22px;
  font-weight: 700;
  color: #f5ead2;
}

.profile-card__value--legend {
  font-size: 32px;
  color: #f7d88e;
}

.profile-card__value--commander {
  font-size: 14px;
  font-weight: 600;
  overflow-wrap: anywhere;
  line-height: 1.3;
}

.profile-card__sub {
  margin-top: 4px;
  font-size: 11px;
  color: #8a7a5a;
}

/* DEV-only "+50 LP" button — visually distinct (dashed border, muted tint)
   so it doesn't look like a real player-facing button. Remove or gate
   behind an env var before shipping. */
.profile-card__dev-grant {
  margin-top: 10px;
  padding: 4px 10px;
  border: 1px dashed #c68c44;
  border-radius: 3px;
  background-color: rgba(58, 31, 10, 0.18);
  color: #d4b87a;
  font-family: inherit;
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.profile-card__dev-grant:hover:not(:disabled) {
  background-color: rgba(198, 140, 68, 0.28);
  color: #fff2d6;
}

.profile-card__dev-grant:disabled {
  opacity: 0.5;
}

.profile-stats {
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.profile-stats__row {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  gap: 8px;
}

.profile-stats__row dt {
  font-size: 12px;
  color: #a09070;
}

.profile-stats__row dd {
  font-size: 13px;
  font-weight: 700;
  color: #f5ead2;
  margin: 0;
}

@media (max-width: 700px) {
  .profile-view__body {
    grid-template-columns: 1fr;
  }
}
</style>
