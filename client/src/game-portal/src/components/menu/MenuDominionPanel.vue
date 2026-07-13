<template>
  <div class="menu-dominion" role="status" aria-live="polite">
    <div
      class="menu-dominion__item"
      tabindex="0"
      aria-label="Dominion Points"
      @mouseenter="clampTip"
      @focus="clampTip"
    >
      <img :src="dominionBadgeUrl" class="menu-dominion__dp-icon" alt="" aria-hidden="true" />
      <span class="menu-dominion__value">{{ formattedPoints }}</span>
      <span class="menu-dominion__tip" role="tooltip">Dominion Points</span>
    </div>

    <div class="menu-dominion__divider" aria-hidden="true"></div>

    <div
      class="menu-dominion__item"
      tabindex="0"
      aria-label="Conquest Badges"
      @mouseenter="clampTip"
      @focus="clampTip"
    >
      <img :src="badgeEarnedUrl" class="menu-dominion__badge-icon" alt="" aria-hidden="true" />
      <span class="menu-dominion__value menu-dominion__value--badges">{{ formattedBadges }}</span>
      <span class="menu-dominion__tip" role="tooltip">Conquest Badges</span>
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

// The panel is pinned to the right edge, so a tooltip centered under its item
// can overhang the viewport (the Conquest Badges one always does). Measure on
// hover/focus and slide it back inside. The shift is stored per-tooltip as
// --tip-shift and reset to 0 before measuring, so repeated hovers don't drift.
const VIEWPORT_MARGIN = 8

function clampTip(event: MouseEvent | FocusEvent) {
  const item = event.currentTarget as HTMLElement | null
  const tip = item?.querySelector<HTMLElement>('.menu-dominion__tip')
  if (!tip) return

  tip.style.setProperty('--tip-shift', '0px')
  const rect = tip.getBoundingClientRect()

  let shift = 0
  const overflowRight = rect.right - (window.innerWidth - VIEWPORT_MARGIN)
  if (overflowRight > 0) shift = -overflowRight
  else if (rect.left < VIEWPORT_MARGIN) shift = VIEWPORT_MARGIN - rect.left

  tip.style.setProperty('--tip-shift', `${Math.round(shift)}px`)
}
</script>

<style scoped>
/* Persistent top-right readout for the out-of-game menus. Mirrors the
   parchment-on-tabletop aesthetic of the in-game Objectives panel
   (MatchObjectivesPanel.vue): warm sepia fill, soft gold border, Cinzel
   header. The panel itself is pointer-events: none so it never intercepts
   clicks on menu buttons beneath it — only the two currency items opt back
   in, so their hover tooltips work. Sits above the MenuChrome background
   but below modals and the start splash. */
.menu-dominion {
  position: fixed;
  top: 12px;
  right: 12px;
  z-index: 40;
  display: flex;
  align-items: center;
  gap: 12px;
  pointer-events: none;
  font-family: var(--font-title);
  color: #f4d27a;
  background: rgba(28, 18, 8, 0.78);
  border: 1px solid rgba(212, 168, 71, 0.45);
  border-radius: 4px;
  padding: 8px 12px;
  box-shadow:
    0 2px 6px rgba(0, 0, 0, 0.55),
    0 0 0 1px rgba(0, 0, 0, 0.25) inset;
}

.menu-dominion__item {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  pointer-events: auto;
}

.menu-dominion__divider {
  width: 1px;
  align-self: stretch;
  background: rgba(212, 168, 71, 0.25);
}

.menu-dominion__value {
  font-size: 22px;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  line-height: 1;
  color: #f4d27a;
}

.menu-dominion__value--badges {
  font-size: 18px;
}

.menu-dominion__dp-icon {
  height: 20px;
  width: auto;
  object-fit: contain;
}

.menu-dominion__badge-icon {
  height: 30px;
  width: auto;
  object-fit: contain;
}

/* Hover/focus tooltip naming the currency, replacing the old always-on
   headers. Anchored below its item and centered on it. */
.menu-dominion__tip {
  --tip-shift: 0px;
  position: absolute;
  top: calc(100% + 8px);
  left: 50%;
  /* Centered under the item, then nudged by --tip-shift (set in clampTip) when
     that would push it past the viewport edge. */
  transform: translateX(calc(-50% + var(--tip-shift)));
  z-index: 1;
  white-space: nowrap;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: #d7bb84;
  background: rgba(18, 12, 5, 0.95);
  border: 1px solid rgba(212, 168, 71, 0.45);
  border-radius: 3px;
  padding: 4px 8px;
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.55);
  opacity: 0;
  visibility: hidden;
  transition: opacity 120ms ease;
}

.menu-dominion__item:hover .menu-dominion__tip,
.menu-dominion__item:focus-visible .menu-dominion__tip {
  opacity: 1;
  visibility: visible;
}
</style>
