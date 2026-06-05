<template>
  <div class="kingdom">
    <div class="kingdom__back">
      <UiButton size="sm" @click="onBack">Back</UiButton>
    </div>

    <div class="kingdom__stage">
      <div
        class="kingdom__scene"
        :style="{ backgroundImage: `url(${kingdomBgUrl})` }"
      >
        <button
          type="button"
          class="kingdom__hotspot kingdom__hotspot--town-hall"
          aria-label="War Room"
          @click="onTownHall"
        >
          <span class="kingdom__label">War Room</span>
        </button>

        <button
          type="button"
          class="kingdom__hotspot kingdom__hotspot--marketplace"
          aria-label="Marketplace"
          @click="onMarketplace"
        >
          <span class="kingdom__label">Marketplace</span>
        </button>

        <button
          type="button"
          class="kingdom__hotspot kingdom__hotspot--blacksmith"
          aria-label="Blacksmith"
          @click="onBlacksmith"
        >
          <span class="kingdom__label">Blacksmith</span>
        </button>

        <button
          type="button"
          class="kingdom__hotspot kingdom__hotspot--barracks"
          aria-label="Barracks"
          @click="onBarracks"
        >
          <span class="kingdom__label">Barracks</span>
        </button>

        <button
          type="button"
          class="kingdom__hotspot kingdom__hotspot--chapel"
          aria-label="Chapel"
          @click="onChapel"
        >
          <span class="kingdom__label">Chapel</span>
        </button>

        <button
          type="button"
          class="kingdom__hotspot kingdom__hotspot--farm"
          aria-label="Farm"
          @click="onFarm"
        >
          <span class="kingdom__label">Farm</span>
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import UiButton from '@/components/ui/UiButton.vue'
import kingdomBgUrl from '@/assets/background-images/castle-view_tier1/full-town-view_tier1.png'

const router = useRouter()

function onBack() {
  router.push('/war-room')
}

function onTownHall() {
  router.push('/war-room')
}

function onMarketplace() {
  // TODO: wire up Marketplace interaction.
}

function onBlacksmith() {
  // TODO: wire up Blacksmith interaction.
}

function onBarracks() {
  // TODO: wire up Barracks interaction.
}

function onChapel() {
  // TODO: wire up Chapel interaction.
}

function onFarm() {
  // TODO: wire up Farm interaction.
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
  top: 16px;
  left: 20px;
  z-index: 2;
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
 * Contain-style sizing: the scene preserves the background's aspect ratio
 * and shrinks to fit inside the viewport on both axes, so the full image —
 * including the top and bottom edges — is always visible. Hotspots are
 * positioned by percentage relative to the scene, so they stay locked to
 * the artwork at any window aspect ratio.
 */
.kingdom__scene {
  position: relative;
  aspect-ratio: 2064 / 1152;
  width: 100%;
  max-height: 100%;
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

.kingdom__label {
  position: absolute;
  bottom: 100%;
  left: 50%;
  transform: translateX(-50%);
  margin-bottom: 8px;
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
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
  transition: color 140ms ease, text-shadow 140ms ease, transform 140ms ease;
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

/*
 * Hotspot placements are tuned against full-town-view_tier1.png (2064x1152).
 * Percentages are relative to the scene, so they track the artwork as the
 * window resizes.
 */
.kingdom__hotspot--town-hall {
  left: 50%;
  top: 24%;
  width: 22%;
  height: 25%;
}

.kingdom__hotspot--marketplace {
  left: 50%;
  top: 74%;
  width: 18%;
  height: 26%;
}

.kingdom__hotspot--blacksmith {
  left: 84%;
  top: 74%;
  width: 20%;
  height: 34%;
}

.kingdom__hotspot--barracks {
  left: 21%;
  top: 42%;
  width: 31%;
  height: 30%;
}

.kingdom__hotspot--chapel {
  left: 76%;
  top: 36%;
  width: 29%;
  height: 42%;
}

.kingdom__hotspot--farm {
  left: 13%;
  top: 75%;
  width: 29%;
  height: 38%;
}
</style>
