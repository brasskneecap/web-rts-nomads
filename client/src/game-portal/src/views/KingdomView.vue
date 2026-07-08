<template>
  <div class="kingdom">
    <div class="kingdom__back">
      <ExitButton aria-label="Back to War Room" @click="onBack" />
    </div>

    <div class="kingdom__stage">
      <div
        class="kingdom__scene"
        :style="{ backgroundImage: `url(${kingdomBgUrl})` }"
      >
        <button
          v-for="b in BUILDINGS"
          :key="b.id"
          type="button"
          class="kingdom__hotspot"
          :style="hotspotStyle(b)"
          :aria-label="b.label"
          @click="onSelect(b)"
        >
          <!-- Orb & Beam: a soft halo orb under the label with a beam rising to it. -->
          <span class="fx fx-beam"></span>
          <span class="fx fx-orb"></span>

          <span class="kingdom__label">{{ b.label }}</span>
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import ExitButton from '@/components/ui/ExitButton.vue'
import kingdomBgUrl from '@/assets/background-images/castle-view_tier1/full-town-view_tier1.png'

const router = useRouter()

/* ----------------------------------------------------------------
 * Buildings: tuned against full-town-view_tier1.png (2064x1152).
 * x/y are center positions, w/h are size — all percentages of the scene.
 * ---------------------------------------------------------------- */
interface Building {
  id: string
  label: string
  x: number
  y: number
  w: number
  h: number
  route?: string
}

const BUILDINGS: ReadonlyArray<Building> = [
  { id: 'townHall', label: 'War Room', x: 51.3, y: 16.5, w: 22, h: 22, route: '/war-room' },
  { id: 'barracks', label: 'Barracks', x: 13, y: 37, w: 31, h: 29, route: '/kingdom/barracks' },
  { id: 'chapel', label: 'Chapel', x: 80.3, y: 36, w: 22, h: 34, route: '/kingdom/chapel' },
  { id: 'farm', label: 'Farm', x: 18.5, y: 69, w: 20, h: 30, route: '/kingdom/farm' },
  { id: 'marketplace', label: 'Marketplace', x: 51, y: 51, w: 28, h: 26, route: '/kingdom/marketplace' },
  { id: 'blacksmith', label: 'Blacksmith', x: 80.2, y: 71, w: 20, h: 34, route: '/kingdom/blacksmith' },
]

function hotspotStyle(b: Building) {
  return { left: `${b.x}%`, top: `${b.y}%`, width: `${b.w}%`, height: `${b.h}%` }
}

function onSelect(b: Building) {
  if (b.route) {
    router.push(b.route)
  }
}

function onBack() {
  router.push('/war-room')
}
</script>

<style scoped>
.kingdom {
  position: relative;
  z-index: 1;
  width: 100%;
  height: 100%;
  overflow: hidden;
  background-color: #05080d;
}

.kingdom__back {
  position: absolute;
  top: 50px;
  left: 50px;
  z-index: 2;
}

/* Larger exit icon (2x the base) pinned to the top-left, matching meta views. */
.kingdom__back :deep(.exit-button) {
  width: 112px;
  height: 112px;
}

.kingdom__stage {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
}

/*
 * Cover-style sizing: the scene preserves the background's aspect ratio
 * and grows until it covers the viewport on both axes — no letterbox bars.
 * Any overflow is clipped by the stage. Hotspots are positioned by
 * percentage relative to the scene, so they stay locked to the artwork at
 * any window aspect ratio.
 */
.kingdom__scene {
  position: relative;
  aspect-ratio: 2064 / 1152;
  min-width: 100%;
  min-height: 100%;
  background-size: 100% 100%;
  background-position: center;
  background-repeat: no-repeat;
  image-rendering: pixelated;
}

.kingdom__hotspot {
  position: absolute;
  transform: translate(-50%, -50%);
  padding: 0;
  border: 0;
  background-color: transparent;
}

.kingdom__hotspot:focus-visible {
  outline: none;
}

/* Shared base for the orb/beam layers. */
.fx {
  position: absolute;
  left: 50%;
  pointer-events: none;
  opacity: 0;
  z-index: 0;
}

/*
 * Orb — a soft, edgeless halo of golden light centered under the label.
 * Anchored at (--orb-x, --orb-y) within the hotspot box; bottom + translateY(50%)
 * puts the orb CENTER on the anchor line. The glow uses drop-shadow (follows the
 * shape), and the hover pulse animates it.
 */
.fx-orb {
  --orb-size: 34px;
  --orb-soft: 5px;
  --orb-glow: rgba(212, 168, 71, 0.5);
  left: var(--orb-x, 50%);
  bottom: calc(100% - var(--orb-y, 90%));
  width: var(--orb-size);
  height: var(--orb-size);
  border-radius: 50%;
  background: radial-gradient(
    circle at 50% 45%,
    rgba(255, 243, 214, 0.95),
    rgba(212, 168, 71, 0.55) 45%,
    rgba(212, 168, 71, 0) 75%
  );
  mix-blend-mode: screen;
  filter: drop-shadow(0 0 6px var(--orb-glow)) blur(var(--orb-soft));
  transform: translate(-50%, 50%) scale(0);
  transition:
    opacity 160ms ease,
    transform 260ms cubic-bezier(0.34, 1.56, 0.64, 1); /* bounce-in */
}

.kingdom__hotspot:hover .fx-orb,
.kingdom__hotspot:focus-visible .fx-orb {
  opacity: 1;
  transform: translate(-50%, 50%) scale(1);
  animation: fx-orb-pulse 1.6s ease-in-out infinite;
}

@keyframes fx-orb-pulse {
  0%,
  100% {
    filter: drop-shadow(0 0 6px var(--orb-glow)) blur(var(--orb-soft));
  }
  50% {
    filter: drop-shadow(0 0 13px var(--orb-glow)) blur(var(--orb-soft));
  }
}

/*
 * Beam — rises straight up from the orb center to the label. Its bottom edge
 * sits on the anchor line; --beam-h is how far up it reaches (% of hotspot).
 */
.fx-beam {
  --beam-w: 16px;
  left: var(--orb-x, 50%);
  bottom: calc(100% - var(--orb-y, 90%));
  width: var(--beam-w);
  height: var(--beam-h, 105%);
  background: linear-gradient(
    to top,
    rgba(212, 168, 71, 0.85),
    rgba(212, 168, 71, 0.35) 50%,
    rgba(245, 235, 200, 0) 100%
  );
  /* 16px wide at bottom -> ~10px at top: inset (16-10)/2 / 16 = 18.75%. */
  clip-path: polygon(0% 100%, 100% 100%, 81.25% 0%, 18.75% 0%);
  filter: blur(3px);
  mix-blend-mode: screen;
  transform: translateX(-50%) scaleY(0);
  transform-origin: bottom center;
  /* 150ms delay so the orb appears first, then the beam grows up. */
  transition:
    opacity 200ms ease 150ms,
    transform 320ms cubic-bezier(0.22, 1, 0.36, 1) 150ms;
}

.kingdom__hotspot:hover .fx-beam,
.kingdom__hotspot:focus-visible .fx-beam {
  opacity: 1;
  transform: translateX(-50%) scaleY(1);
  animation: fx-beam-shimmer 1.8s ease-in-out infinite 0.4s;
}

@keyframes fx-beam-shimmer {
  0%,
  100% {
    filter: blur(3px) brightness(1);
  }
  50% {
    filter: blur(3px) brightness(1.3);
  }
}

/* Building label */
.kingdom__label {
  position: absolute;
  bottom: 100%;
  left: 50%;
  z-index: 1;
  transform: translateX(-50%);
  margin-bottom: 8px;
  font-family: var(--font-title);
  font-size: clamp(12px, 1.2vw, 20px);
  font-weight: 700;
  letter-spacing: 0.06em;
  white-space: nowrap;
  color: #f4d27a;
  text-shadow:
    0 0 6px rgba(0, 0, 0, 0.9),
    0 1px 2px rgba(0, 0, 0, 0.9),
    0 0 10px rgba(255, 200, 100, 0.25);
  pointer-events: none;
  transition:
    color 140ms ease,
    text-shadow 140ms ease,
    transform 140ms ease;
}

.kingdom__hotspot:hover .kingdom__label,
.kingdom__hotspot:focus-visible .kingdom__label {
  transform: translateX(-50%) translateY(-2px);
  color: #ffe9a8;
  text-shadow:
    0 0 6px rgba(0, 0, 0, 0.95),
    0 1px 2px rgba(0, 0, 0, 0.95),
    0 0 12px rgba(255, 220, 140, 0.95),
    0 0 24px rgba(255, 200, 100, 0.7);
}
</style>
