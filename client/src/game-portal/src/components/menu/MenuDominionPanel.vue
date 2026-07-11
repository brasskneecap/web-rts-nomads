<template>
  <div class="menu-dominion" role="status" aria-live="polite">
    <div class="menu-dominion__header">Dominion Points</div>
    <div class="menu-dominion__value">
      <img :src="dominionBadgeUrl" class="menu-dominion__dp-icon" alt="" aria-hidden="true" />{{ formattedPoints }}
    </div>
    <div class="menu-dominion__header menu-dominion__header--badges">Conquest Badges</div>
    <div class="menu-dominion__value menu-dominion__value--badges">
      <img :src="badgeEarnedUrl" class="menu-dominion__badge-icon" alt="" aria-hidden="true" />{{ formattedBadges }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useProfile } from '@/composables/useProfile'
import badgeEarnedUrl from '@/assets/ui/themes/updated/advancements/badge-earned.png'
import dominionBadgeUrl from '@/assets/ui/themes/updated/dominion-badge.png'

// Reads the app-wide profile singleton (initialized at startup in main.ts).
// The same reactive ref is mutated by the advancement / profile-upgrade
// purchase flows, so the displayed total live-updates when Dominion Points
// or Conquest Badges are earned or spent in the menus — no extra wiring needed.
const { profile } = useProfile()

const formattedPoints = computed(() =>
  (profile.value?.dominionPoints ?? 0).toLocaleString(),
)

const formattedBadges = computed(() =>
  (profile.value?.conquestBadges ?? 0).toLocaleString(),
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
  font-family: var(--font-title);
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

.menu-dominion__header--badges {
  margin-top: 10px;
  padding-top: 8px;
  border-top: 1px solid rgba(212, 168, 71, 0.25);
}

.menu-dominion__value {
  /* Equal-width flex rows with left-aligned content: the block stays centered
     in the panel (parent text-align: center), while both icons share the same
     left edge so the star and shield line up. */
  display: inline-flex;
  align-items: center;
  justify-content: flex-start;
  gap: 8px;
  min-width: 96px;
  text-align: left;
  font-size: 22px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  line-height: 1;
  color: #f4d27a;
}

.menu-dominion__dp-icon {
  height: 20px;
  width: auto;
  object-fit: contain;
}

.menu-dominion__value--badges {
  font-size: 18px;
}

.menu-dominion__badge-icon {
  height: 30px;
  width: auto;
  object-fit: contain;
}
</style>
