<template>
  <!-- Full-screen modal overlay; a click on the backdrop closes the panel. -->
  <div class="uap-overlay" @click.self="emit('close')">
    <!-- Outer world-menu (black/brass) frame, same panel system as the
         custom-game war-room popup. --padding 0 so the inner frame controls
         spacing; assetVars feeds the button art to scoped CSS. -->
    <UiPanel variant="worldMenu" :padding="0" class="uap" :style="assetVars">
      <div class="uap__frame" role="dialog" :aria-label="`${unitName} advancements`">
        <!-- Title plaque (world-panel-header shield bar) mounted at the top,
             straddling the frame edge like the custom-game UI. -->
        <header class="uap__titlebar">
          <span class="uap__title">Advancements</span>
        </header>

        <div class="uap__body">
          <!-- Left profile panel (war-room-inner-panel). -->
          <UiPanel variant="warRoomInner" :padding="0" class="uap__profile">
            <div class="uap__profile-inner">
              <img class="uap__portrait" :src="portrait" :alt="unitName" />
              <div class="uap__unit-name">{{ unitName }}</div>
            </div>
          </UiPanel>

          <!-- Advancements panel (world-inner-panel / parchment) holding the tracks. -->
          <UiPanel variant="worldInner" :padding="0" class="uap__adv">
            <div class="uap__tracks">
              <div v-if="error" class="uap__error" role="alert">{{ error }}</div>

              <div
                v-for="tier in tiers"
                :key="tier.title"
                class="uap__track"
                :class="{ 'uap__track--placeholder': tier.placeholder }"
              >
                <div class="uap__track-heading">
                  <span class="uap__track-title">{{ tier.title }}</span>
                  <span class="uap__track-rule" aria-hidden="true"></span>
                </div>

                <div class="uap__nodes">
                  <template v-for="(node, idx) in tier.nodes" :key="node.id">
                    <span
                      v-if="idx > 0"
                      class="uap__connector"
                      :class="{ 'uap__connector--filled': !tier.placeholder && isAcquired(tier.nodes[idx - 1].id) }"
                      aria-hidden="true"
                    ></span>
                    <div class="uap__node-cell">
                      <!-- Fixed-height slot centers every node icon on one line
                           so seal and badge centers align regardless of the
                           badge art being taller. -->
                      <div class="uap__node-slot">
                        <button
                          type="button"
                          class="uap__node"
                          :class="[
                            node.kind === 'major' ? 'uap__node--badge' : 'uap__node--seal',
                            tier.placeholder ? 'uap__node--locked' : nodeStateClass(tier, idx),
                          ]"
                          :style="{ backgroundImage: nodeIcon(node.kind, !tier.placeholder && isAcquired(node.id)) }"
                          :disabled="tier.placeholder || isBusy || isAcquired(node.id) || !isAvailable(tier, idx) || !canAcquire(node)"
                          :aria-label="tier.placeholder ? `${tier.title} — coming soon` : `${node.name} (${nodeStateLabel(tier, idx)})`"
                          @click="!tier.placeholder && purchase(node.id)"
                        >
                          <UiTooltip
                            v-if="!tier.placeholder"
                            :title="node.name"
                            :body="tooltipBody(node)"
                          />
                        </button>
                      </div>
                      <div v-if="!tier.placeholder" class="uap__cost-row">
                        <span class="uap__cost">
                          <img :src="dominionBadgeUrl" class="uap__dp-icon" alt="Dominion Points" />{{ node.cost }}
                        </span>
                        <span v-if="node.kind === 'major'" class="uap__badge-cost">
                          <img :src="badgeEarnedUrl" class="uap__badge-cost-icon" alt="Conquest Badge" />1
                        </span>
                      </div>
                    </div>
                  </template>
                </div>
              </div>
            </div>
          </UiPanel>
        </div>

        <!-- Footer: Close button, right-aligned. -->
        <footer class="uap__footer">
          <button type="button" class="uap__close" @click="emit('close')">Close</button>
        </footer>
      </div>
    </UiPanel>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import UiPanel from '@/components/ui/UiPanel.vue'
import UiTooltip from '@/components/ui/UiTooltip.vue'
import { useAdvancements } from '@/composables/useAdvancements'
import type { UnitAdvancementNode } from '@/types/profile'
import buttonUrl from '@/assets/ui/themes/updated/war-room/war-room-inactive-button.png'
import buttonActiveUrl from '@/assets/ui/themes/updated/war-room/war-room-active-button.png'
import headerUrl from '@/assets/ui/themes/updated/world-panel-header.png'
import sealEarnedUrl from '@/assets/ui/themes/updated/advancements/seal-earned.png'
import sealUnearnedUrl from '@/assets/ui/themes/updated/advancements/seal-unearned.png'
import badgeEarnedUrl from '@/assets/ui/themes/updated/advancements/badge-earned.png'
import badgeUnearnedUrl from '@/assets/ui/themes/updated/advancements/badge-unearned.png'
import dominionBadgeUrl from '@/assets/ui/themes/updated/dominion-badge.png'

const props = defineProps<{
  unitType: string
  unitName: string
  portrait: string
}>()

const emit = defineEmits<{ close: [] }>()

const {
  catalog,
  conquestBadges,
  isBusy,
  error,
  isAcquired,
  canAcquire,
  purchase,
} = useAdvancements()

// Button art exposed to scoped CSS as custom properties — scoped styles can't
// import assets directly. Mirrors CustomGame.vue's assetVars wiring.
const assetVars = computed(() => ({
  '--uap-btn': `url(${buttonUrl})`,
  '--uap-btn-active': `url(${buttonActiveUrl})`,
  '--uap-header': `url(${headerUrl})`,
}))

// A tier is one horizontal track row. Row 0 is the unit's real, purchasable
// advancement track; rows 1 & 2 are placeholders for tiers that ship later —
// they mirror the real track's node shapes but render greyed and inert.
interface Tier {
  title: string
  placeholder: boolean
  nodes: UnitAdvancementNode[]
}

const TIER_TITLES = ['Recruit Training', 'Veteran Doctrine', 'Elite Regiment'] as const

// Default node-shape pattern for placeholder rows when no real track exists yet
// (6 seals with a badge at the 4th and 8th slot), matching the soldier layout.
const DEFAULT_KINDS: UnitAdvancementNode['kind'][] = [
  'minor', 'minor', 'minor', 'major', 'minor', 'minor', 'minor', 'major',
]

const realTrack = computed(() =>
  catalog.value.find((t) => t.unitType === props.unitType && t.nodes.length > 0),
)

const tiers = computed<Tier[]>(() => {
  const kinds = realTrack.value?.nodes.map((n) => n.kind) ?? DEFAULT_KINDS
  return TIER_TITLES.map((title, ti) => {
    if (ti === 0 && realTrack.value) {
      return { title, placeholder: false, nodes: realTrack.value.nodes }
    }
    return {
      title,
      placeholder: true,
      nodes: kinds.map((kind, i) => ({
        id: `${title}-placeholder-${i}`,
        kind,
        name: '',
        description: '',
        cost: 0,
        effects: [],
      })),
    }
  })
})

// A node is buyable only once every earlier node on its track is acquired.
function isAvailable(tier: Tier, idx: number): boolean {
  if (tier.placeholder) return false
  if (idx === 0) return true
  return isAcquired(tier.nodes[idx - 1].id)
}

function nodeIcon(kind: UnitAdvancementNode['kind'], acquired: boolean): string {
  if (kind === 'major') {
    return `url(${acquired ? badgeEarnedUrl : badgeUnearnedUrl})`
  }
  return `url(${acquired ? sealEarnedUrl : sealUnearnedUrl})`
}

function nodeStateClass(tier: Tier, idx: number): string {
  const node = tier.nodes[idx]
  if (isAcquired(node.id)) return 'uap__node--acquired'
  if (isAvailable(tier, idx)) {
    return canAcquire(node) ? 'uap__node--available' : 'uap__node--unaffordable'
  }
  return 'uap__node--locked'
}

function nodeStateLabel(tier: Tier, idx: number): string {
  const node = tier.nodes[idx]
  if (isAcquired(node.id)) return 'acquired'
  if (isAvailable(tier, idx)) {
    if (canAcquire(node)) return 'available'
    return node.kind === 'major' && conquestBadges.value < 1
      ? 'requires a Conquest Badge'
      : 'not enough Dominion Points'
  }
  return 'locked'
}

function tooltipBody(node: UnitAdvancementNode): string {
  const lines: string[] = []
  if (node.description) lines.push(node.description)
  if (node.kind === 'major') lines.push('Requires: 1 Conquest Badge')
  return lines.join('\n')
}
</script>

<style scoped>
/* Full-screen dimmed overlay centering the modal. */
.uap-overlay {
  position: absolute;
  inset: 0;
  z-index: 10;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(6, 8, 13, 0.62);
  backdrop-filter: blur(2px);
}

/* Outer world-menu frame (UiPanel paints the border-image + fill). */
.uap {
  position: relative;
  width: min(1400px, 94vw);
  aspect-ratio: 1780 / 1160;
  max-height: 94vh;
  box-sizing: border-box;
  display: flex;
}

/* Inner frame stacks title / body / footer, with the panel's own padding. */
.uap__frame {
  position: relative;
  flex: 1 1 auto;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  /* No top padding: the titlebar is the first child and lifts itself above the
     frame's top edge with a negative margin (mirrors custom-game). */
  padding: 0 clamp(18px, 2.4vw, 34px) clamp(12px, 1.6vw, 22px);
  color: #e9dbb8;
}

/* --- Title plaque (world-panel-header shield bar) --- */
/* Aspect-ratio locked to the source art so the shield never distorts; centered
   and lifted with a negative margin so ~30% straddles the frame's top edge,
   matching the custom-game titlebar. */
.uap__titlebar {
  position: relative;
  z-index: 2;
  flex: 0 0 auto;
  align-self: center;
  width: min(96%, clamp(440px, 54vw, 760px));
  aspect-ratio: 740 / 140;
  /* Lift roughly the top half of the header above the panel's top edge so it
     hangs over the frame like a mounted sign (as in the custom-game UI). */
  margin-top: calc(-1 * clamp(46px, 5.2vw, 82px));
  margin-bottom: clamp(6px, 0.8vw, 14px);
  background: var(--uap-header) center / 100% 100% no-repeat;
  image-rendering: pixelated;
  display: flex;
  align-items: center;
  justify-content: center;
}

.uap__title {
  font-family: var(--font-title);
  font-size: clamp(15px, 1.95vw, 27px);
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #e7c88a;
  text-shadow:
    0 1px 2px rgba(0, 0, 0, 0.85),
    0 0 12px rgba(212, 168, 71, 0.3);
  white-space: nowrap;
  /* Centered in the bar, then nudged right (clear of the shield) and down onto
     the wood plank — same recipe as the custom-game titlebar. The smaller font
     keeps this longer title clear of the shield when centered. */
  transform: translate(25px, 6%);
}

/* --- Body: sidebar + parchment --- */
.uap__body {
  flex: 1 1 auto;
  display: flex;
  align-items: stretch;
  gap: clamp(12px, 1.6vw, 26px);
  min-height: 0;
  margin-top: clamp(10px, 1.2vw, 18px);
}

/* Left profile panel (UiPanel warRoomInner paints the dark-wood fill). Wraps
   its content (portrait + name) and sits at the top rather than stretching to
   the full body height. */
.uap__profile {
  flex: 0 0 clamp(150px, 16%, 230px);
  align-self: flex-start;
  display: flex;
}

.uap__profile-inner {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: flex-start;
  gap: clamp(8px, 1vw, 16px);
  padding: clamp(12px, 1.4vw, 22px) clamp(8px, 1vw, 16px);
}

/* Portrait sits directly at the top of the profile panel (no sub-frame). */
.uap__portrait {
  flex: 0 0 auto;
  width: clamp(96px, 10vw, 150px);
  aspect-ratio: 1 / 1;
  object-fit: cover;
  object-position: center top;
  image-rendering: pixelated;
}

.uap__unit-name {
  font-family: var(--font-title);
  font-size: clamp(15px, 1.5vw, 24px);
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: #f0d69a;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.8);
  text-align: center;
}

/* Advancements panel (UiPanel worldInner paints the parchment fill). It flexes
   to fill the body; content padding lives on .uap__tracks because UiPanel
   applies its own inline padding to the root. */
.uap__adv {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
}

.uap__tracks {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  flex-direction: column;
  justify-content: flex-start;
  gap: clamp(16px, 2.2vw, 36px);
  padding: clamp(6px, 0.8vw, 12px) clamp(18px, 2.6vw, 44px) clamp(10px, 1.4vw, 22px);
}

.uap__error {
  margin-bottom: 10px;
  padding: 4px 10px;
  border-radius: 4px;
  background: rgba(180, 40, 20, 0.15);
  border: 1px solid rgba(180, 40, 20, 0.4);
  font-family: var(--font-title);
  font-size: clamp(11px, 1vw, 14px);
  color: #7a1a0a;
}

/* --- One tiered track (heading + node row) --- */
.uap__track {
  display: flex;
  flex-direction: column;
  gap: clamp(4px, 0.6vw, 10px);
}

.uap__track--placeholder {
  opacity: 0.72;
}

.uap__track-heading {
  display: flex;
  align-items: center;
  gap: clamp(8px, 1vw, 16px);
}

.uap__track-title {
  font-family: var(--font-title);
  font-size: clamp(12px, 1.15vw, 18px);
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: #4a2d12;
  white-space: nowrap;
}

.uap__track-rule {
  flex: 1 1 auto;
  height: 2px;
  background: linear-gradient(90deg, rgba(74, 45, 18, 0.55), rgba(74, 45, 18, 0.05));
  border-radius: 1px;
}

.uap__nodes {
  display: flex;
  flex-wrap: nowrap;
  align-items: flex-start;
  justify-content: center;
}

/* Thin link between adjacent nodes. Fills gold when the node to its left is
   acquired, echoing the progression line in the reference art. */
.uap__connector {
  flex: 1 1 auto;
  min-width: clamp(6px, 1.2vw, 24px);
  height: 3px;
  /* Sit on the node slot's vertical center so the line threads through the
     node icons. */
  margin-top: calc(clamp(63px, 5.7vw, 91px) / 2 - 1.5px);
  background: rgba(74, 46, 20, 0.3);
  border-radius: 2px;
}

.uap__connector--filled {
  background: linear-gradient(90deg, #a9781f, #e6bd5c);
  box-shadow: 0 0 4px rgba(230, 189, 92, 0.5);
}

.uap__node-cell {
  flex: 0 0 auto;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: clamp(3px, 0.4vw, 6px);
  padding: 0 clamp(2px, 0.3vw, 6px);
}

/* Uniform-height slot (= the tallest node art, i.e. a badge) that vertically
   centers whatever node icon it holds, so seal and badge centers share one
   line and the cost labels below still line up. */
.uap__node-slot {
  display: flex;
  align-items: center;
  justify-content: center;
  height: clamp(63px, 5.7vw, 91px);
}

.uap__node {
  position: relative;
  padding: 0;
  border: 0;
  background-color: transparent;
  background-repeat: no-repeat;
  background-position: center;
  background-size: contain;
  /* These seal/badge icons are detailed painted art, not pixel art. Override
     the pixelated image-rendering inherited from the UiPanel ancestor so they
     scale smoothly instead of aliasing/distorting. */
  image-rendering: auto;
  transition:
    filter 120ms ease,
    opacity 120ms ease;
}

.uap__node--seal {
  width: clamp(40px, 3.6vw, 58px);
  aspect-ratio: 83 / 85;
}

.uap__node--badge {
  width: clamp(44px, 4vw, 64px);
  aspect-ratio: 77 / 110;
}

/* Tooltip is triggered on the slot (a plain div) rather than the button, so it
   still shows for disabled/acquired nodes that don't fire :hover themselves. */
.uap__node-slot:hover .ui-tooltip,
.uap__node:focus-visible .ui-tooltip {
  opacity: 1;
  visibility: visible;
}

/* Acquired: a steady soft gold glow marks it as owned. No hover reaction — the
   button is disabled, so only its tooltip appears on hover. */
.uap__node--acquired {
  filter: drop-shadow(0 0 6px rgba(230, 179, 90, 0.55));
}

/* Next available upgrade that the player can afford: a steady gold glow.
   (Steady, not animated — animating the filter re-rasterizes the whole row
   each frame and makes the icons shimmer.) */
.uap__node--available {
  filter: drop-shadow(0 0 8px rgba(240, 196, 90, 0.85));
}

.uap__node--unaffordable {
  filter: grayscale(0.35) brightness(0.9);
  opacity: 0.9;
  cursor: not-allowed;
}

.uap__node--locked {
  filter: grayscale(0.5) brightness(0.78);
  opacity: 0.85;
  cursor: not-allowed;
}

/* Hover feedback only on the actionable (enabled) node — brighten while keeping
   the same glow radius so nothing shifts or re-rasterizes on hover. */
.uap__node:hover:not(:disabled) {
  filter: brightness(1.12) drop-shadow(0 0 8px rgba(255, 224, 150, 0.9));
}

.uap__node:active:not(:disabled) {
  filter: brightness(0.95) drop-shadow(0 0 8px rgba(255, 224, 150, 0.9));
}

/* DP cost, with the badge cost hung underneath it for major upgrades. The
   badge is absolutely positioned so it doesn't add to the cell height — that
   keeps every DP label on the same line across the row, badge dangling only
   under the majors. */
.uap__cost-row {
  position: relative;
  display: flex;
  flex-direction: column;
  /* Shrink to the (wider) DP row and left-align both rows so the DP star and
     badge shield share the same left edge. The row itself is centered under
     the node by the node-cell's align-items: center. */
  width: fit-content;
  align-items: flex-start;
  white-space: nowrap;
}

.uap__cost {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-family: var(--font-title);
  font-size: clamp(12px, 1.15vw, 18px);
  font-weight: 700;
  letter-spacing: 0.03em;
  color: #3a1f0a;
  white-space: nowrap;
}

.uap__dp-icon {
  height: clamp(15px, 1.5vw, 21px);
  width: auto;
  object-fit: contain;
}

.uap__badge-cost {
  position: absolute;
  top: 100%;
  left: 0;
  right: 0;
  margin-top: clamp(2px, 0.3vw, 5px);
  display: flex;
  align-items: center;
  justify-content: flex-start;
  gap: 5px;
  font-family: var(--font-title);
  font-size: clamp(12px, 1.15vw, 18px);
  font-weight: 700;
  color: #3a1f0a;
  white-space: nowrap;
}

.uap__badge-cost-icon {
  height: clamp(20px, 1.95vw, 27px);
  width: auto;
  object-fit: contain;
}

/* --- Footer --- */
.uap__footer {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  margin-top: clamp(10px, 1.2vw, 18px);
  padding: 0 clamp(6px, 0.8vw, 14px);
}

.uap__close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: clamp(36px, 3.4vw, 52px);
  min-width: clamp(120px, 12vw, 190px);
  padding: 0 clamp(20px, 2.4vw, 40px);
  font-family: var(--font-title);
  font-size: clamp(13px, 1.2vw, 19px);
  font-weight: 700;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: #f0d69a;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.85);
  border: 14px solid transparent;
  border-image-source: var(--uap-btn);
  border-image-slice: 14 fill;
  border-image-width: 14px;
  border-image-repeat: stretch;
  image-rendering: pixelated;
  transition: color 120ms ease, transform 120ms ease;
}

.uap__close:hover {
  color: #fff0c8;
  border-image-source: var(--uap-btn-active);
  transform: translateY(-1px);
}

.uap__close:active {
  transform: translateY(0);
}
</style>
