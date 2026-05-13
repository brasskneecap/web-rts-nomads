<template>
  <div class="profile-view">
    <div class="profile-view__layout">
      <header class="profile-view__header">
        <button class="profile-view__back" type="button" @click="router.back()">Back</button>
        <h1 class="profile-view__title">Player Profile</h1>
      </header>

      <div v-if="isLoading && !profile" class="profile-view__loading" aria-live="polite">
        Loading profile...
      </div>

      <div v-else-if="error && !profile" class="profile-view__error" role="alert">
        {{ error }}
        <button type="button" class="profile-view__retry" @click="initialize">Retry</button>
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
          </section>

          <section class="profile-card" aria-label="Commander">
            <div class="profile-card__label">Commander</div>
            <div class="profile-card__value">
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
          <section class="profile-card" aria-label="Buff Loadout">
            <div class="profile-card__label">Buff Loadout</div>
            <BuffLoadoutPicker />
          </section>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useProfile } from '@/composables/useProfile'
import BuffLoadoutPicker from '@/components/profile/BuffLoadoutPicker.vue'

const router = useRouter()
const { profile, isLoading, error, initialize } = useProfile()

onMounted(() => { void initialize() })

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
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: flex-start;
  background: radial-gradient(circle at top, rgba(36, 55, 87, 0.35), transparent 48%);
  padding: 32px;
  box-sizing: border-box;
  overflow-y: auto;
}

.profile-view__layout {
  display: flex;
  flex-direction: column;
  gap: 24px;
  width: 100%;
  max-width: 980px;
}

.profile-view__header {
  display: flex;
  align-items: center;
  gap: 20px;
}

.profile-view__back {
  padding: 8px 18px;
  border-radius: 8px;
  border: 1px solid rgba(200, 164, 106, 0.28);
  background: linear-gradient(180deg, rgba(60, 40, 18, 0.85), rgba(30, 18, 8, 0.92));
  color: #f5ead2;
  font-size: 13px;
  font-weight: 700;
  cursor: pointer;
  letter-spacing: 0.05em;
  transition: background 0.1s, border-color 0.1s;
}

.profile-view__back:hover {
  background: linear-gradient(180deg, rgba(90, 60, 25, 0.9), rgba(50, 30, 12, 0.95));
  border-color: rgba(220, 180, 100, 0.5);
}

.profile-view__back:focus-visible {
  outline: 2px solid rgba(247, 216, 142, 0.9);
  outline-offset: 2px;
}

.profile-view__title {
  font-size: 24px;
  font-weight: 700;
  color: #f5ead2;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  margin: 0;
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
  padding: 32px 0;
}

.profile-view__retry {
  padding: 6px 16px;
  border-radius: 6px;
  border: 1px solid rgba(200, 80, 80, 0.4);
  background: linear-gradient(180deg, rgba(80, 20, 20, 0.85), rgba(40, 10, 10, 0.9));
  color: #f5d8d8;
  font-size: 12px;
  font-weight: 700;
  cursor: pointer;
}

.profile-view__body {
  display: grid;
  grid-template-columns: 280px 1fr;
  gap: 20px;
  align-items: start;
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

.profile-card {
  padding: 16px;
  border-radius: 10px;
  border: 1px solid rgba(200, 164, 106, 0.2);
  background: linear-gradient(180deg, rgba(20, 13, 6, 0.9), rgba(10, 7, 3, 0.95));
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

.profile-card__sub {
  margin-top: 4px;
  font-size: 11px;
  color: #8a7a5a;
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
