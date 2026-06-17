<template>
  <div class="menu-dominion" role="status" aria-live="polite">
    <div class="menu-dominion__header">Dominion Points</div>
    <div class="menu-dominion__value">{{ formattedPoints }}</div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useProfile } from '@/composables/useProfile'

// Reads the app-wide profile singleton (initialized at startup in main.ts).
// The same reactive ref is mutated by the advancement / profile-upgrade
// purchase flows, so the displayed total live-updates when Dominion Points
// are earned or spent in the menus — no extra wiring needed.
const { profile } = useProfile()

const formattedPoints = computed(() =>
  (profile.value?.dominionPoints ?? 0).toLocaleString(),
)
</script>

<style scoped>
/* Persistent top-right readout for the out-of-game menus. Mirrors the
   parchment-on-tabletop aesthetic of the in-game Objectives panel
   (MatchObjectivesPanel.vue): warm sepia fill, soft gold border, Cinzel
   header. Purely decorative — pointer-events: none so it never intercepts
   clicks on menu buttons beneath it. Sits above the MenuChrome background
   but below modals and the start splash. */
.menu-dominion {
  position: fixed;
  top: 12px;
  right: 12px;
  z-index: 40;
  min-width: 150px;
  pointer-events: none;
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  color: #f4d27a;
  background: rgba(28, 18, 8, 0.78);
  border: 1px solid rgba(212, 168, 71, 0.45);
  border-radius: 4px;
  padding: 10px 12px;
  box-shadow:
    0 2px 6px rgba(0, 0, 0, 0.55),
    0 0 0 1px rgba(0, 0, 0, 0.25) inset;
  text-align: center;
}

.menu-dominion__header {
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: #d7bb84;
  margin-bottom: 6px;
}

.menu-dominion__value {
  font-size: 22px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  line-height: 1;
  color: #f4d27a;
}
</style>
