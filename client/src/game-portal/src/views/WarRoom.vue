<template>
  <div class="war-room">
    <div class="war-room__back">
      <UiButton size="sm" @click="onBack">Back</UiButton>
    </div>

    <div class="war-room__stage">
      <div
        class="war-room__scene"
        :style="{ backgroundImage: `url(${warRoomBgUrl})` }"
      >
        <div class="war-room__hotspots">
          <button
            type="button"
            class="war-room__hotspot war-room__hotspot--upgrades"
            :class="{ 'war-room__hotspot--selected': isSelected('upgrades') }"
            :style="{ backgroundImage: `url(${upgradesUrl})` }"
            aria-label="Upgrades"
            @click="router.push('/profile')"
          >
            <span class="war-room__label">Upgrades</span>
          </button>

          <button
            type="button"
            class="war-room__hotspot war-room__hotspot--campaign"
            :class="{ 'war-room__hotspot--selected': isSelected('campaign') }"
            :style="{ backgroundImage: `url(${campaignUrl})` }"
            aria-label="Campaign"
            @click="onCampaign"
          >
            <span class="war-room__label">Campaign</span>
          </button>

          <button
            type="button"
            class="war-room__hotspot war-room__hotspot--custom"
            :class="{ 'war-room__hotspot--selected': isSelected('custom') }"
            :style="{ backgroundImage: `url(${customGameUrl})` }"
            aria-label="Custom Game"
            @click="router.push('/custom')"
          >
            <span class="war-room__label">Custom Game</span>
          </button>

          <button
            type="button"
            class="war-room__hotspot war-room__hotspot--astrolab"
            :class="{ 'war-room__hotspot--selected': isSelected('advancements') }"
            :style="{ backgroundImage: `url(${astrolabUrl})` }"
            aria-label="Advancements"
            @click="selectTab('advancements')"
          >
            <span class="war-room__label">Advancements</span>
          </button>
        </div>

        <div class="war-room__page">
          <Advancements v-if="activeTab === 'advancements'" />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import UiButton from '@/components/ui/UiButton.vue'
import Advancements from '@/views/Advancements.vue'
import warRoomBgUrl from '@/assets/background-images/war_room_bg.png'
import campaignUrl from '@/assets/ui/buttons/war_room/campaign.png'
import customGameUrl from '@/assets/ui/buttons/war_room/custom_game.png'
import astrolabUrl from '@/assets/ui/buttons/war_room/astrolab.png'
import upgradesUrl from '@/assets/ui/buttons/war_room/upgrades.png'

const router = useRouter()

// In-room tab state. The hotspots act as a tab bar: selecting one renders its
// content inline in the parchment slot instead of pushing a nested route, so
// Back always returns to the main menu rather than a /war-room/* sub-route.
// `null` means no tab open (bare room with just the hotspots showing).
const activeTab = ref<string | null>(null)

function isSelected(tab: string): boolean {
  return activeTab.value === tab
}

// Toggle the tab: clicking the active hotspot again closes it back to the
// bare room.
function selectTab(tab: string) {
  activeTab.value = activeTab.value === tab ? null : tab
}

function onBack() {
  router.push('/')
}

function onCampaign() {
  // Campaign is not implemented yet (disabled on the main menu).
}
</script>

<style scoped>
.war-room {
  position: relative;
  z-index: 1;
  width: 100%;
  height: 100%;
  overflow: hidden;
  background-color: #05080d;
}

.war-room__back {
  position: absolute;
  top: 16px;
  left: 20px;
  z-index: 2;
}

.war-room__stage {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
}

/*
 * Cover-style sizing: the scene preserves the background's aspect ratio
 * and grows until it covers the viewport on both axes. Overflow is clipped
 * by the stage. This lets us position hotspots in percentages and have
 * them stay locked to the artwork regardless of window aspect ratio.
 *
 * `--scene-min-width` is a hard floor: once the viewport gets narrower than
 * this, the scene stops shrinking and the stage crops it symmetrically on
 * the left/right (and top/bottom if needed) instead — so the active items
 * never shrink past a usable size. The locked aspect-ratio keeps the floor
 * proportional, so the artwork never distorts. Raise this number to crop
 * sooner / keep items larger; lower it to allow more shrinkage.
 */
.war-room__scene {
  --scene-min-width: 1280px;
  position: relative;
  aspect-ratio: 1162 / 830;
  min-width: max(100%, var(--scene-min-width));
  min-height: 100%;
  background-size: 100% 100%;
  background-position: center;
  background-repeat: no-repeat;
  image-rendering: pixelated;
}

.war-room__hotspots {
  position: absolute;
  inset: 0;
}

.war-room__page {
  position: absolute;
  left: calc(22% + 75px);
  right: calc(22% - 75px);
  top: 46%;
  bottom: 26%;
  pointer-events: none;
  /*
   * Establish a size query container so the nested page (Advancements) can
   * size its contents in container-query units and scale 1:1 with the
   * parchment slot — which itself already tracks the cover-fit scene.
   * `container-type: size` applies size/layout/style containment but NOT
   * paint, so node tooltips can still extend above the panel bounds.
   */
  container-type: size;
}

.war-room__page :deep(> *) {
  pointer-events: auto;
}

.war-room__hotspot {
  position: absolute;
  transform: translate(-50%, -50%);
  padding: 0;
  border: 0;
  background-color: transparent;
  background-repeat: no-repeat;
  background-position: center;
  background-size: contain;
  cursor: pointer;
  image-rendering: pixelated;
  /*
   * Stacked drop-shadows simulate the contact shadow right under the
   * object plus a softer ambient shadow further out. Together they make
   * the items read as resting on the tabletop.
   */
  filter:
    drop-shadow(0 1px 1px rgba(0, 0, 0, 0.75))
    drop-shadow(0 5px 6px rgba(0, 0, 0, 0.5));
  transition: transform 120ms ease, filter 120ms ease;
}

.war-room__hotspot:hover,
.war-room__hotspot:focus-visible {
  transform: translate(-50%, -50%) scale(1.06);
  filter:
    drop-shadow(0 1px 1px rgba(0, 0, 0, 0.75))
    drop-shadow(0 7px 8px rgba(0, 0, 0, 0.55))
    drop-shadow(0 0 10px rgba(255, 220, 140, 0.7));
  outline: none;
}

.war-room__hotspot:active {
  transform: translate(-50%, -50%) scale(0.98);
}

.war-room__hotspot--selected {
  filter:
    drop-shadow(0 1px 1px rgba(0, 0, 0, 0.75))
    drop-shadow(0 5px 6px rgba(0, 0, 0, 0.5))
    drop-shadow(0 0 8px rgba(106, 178, 255, 0.95))
    drop-shadow(0 0 18px rgba(74, 143, 219, 0.7));
}

.war-room__hotspot--selected:hover,
.war-room__hotspot--selected:focus-visible {
  filter:
    drop-shadow(0 1px 1px rgba(0, 0, 0, 0.75))
    drop-shadow(0 7px 8px rgba(0, 0, 0, 0.55))
    drop-shadow(0 0 10px rgba(140, 200, 255, 1))
    drop-shadow(0 0 22px rgba(74, 143, 219, 0.8));
}

.war-room__hotspot--selected .war-room__label {
  color: #b8dcff;
  opacity: 1;
  text-shadow:
    0 0 6px rgba(0, 0, 0, 0.9),
    0 1px 2px rgba(0, 0, 0, 0.9),
    0 0 12px rgba(106, 178, 255, 0.7);
}

.war-room__label {
  position: absolute;
  bottom: 100%;
  left: 50%;
  transform: translateX(-50%);
  margin-bottom: 6px;
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: clamp(11px, 1.1vw, 18px);
  font-weight: 700;
  letter-spacing: 0.06em;
  white-space: nowrap;
  color: #f4d27a;
  text-shadow:
    0 0 6px rgba(0, 0, 0, 0.9),
    0 1px 2px rgba(0, 0, 0, 0.9),
    0 0 12px rgba(255, 200, 100, 0.35);
  pointer-events: none;
  opacity: 0.92;
  transition: opacity 120ms ease, transform 120ms ease;
}

.war-room__hotspot:hover .war-room__label,
.war-room__hotspot:focus-visible .war-room__label {
  opacity: 1;
  transform: translateX(-50%) translateY(-2px);
  color: #ffe9a8;
}

.war-room__hotspot--campaign {
  left: 31%;
  top: 43%;
  width: 7%;
  aspect-ratio: 1 / 1;
}

.war-room__hotspot--custom {
  left: 44%;
  top: 41%;
  width: 7%;
  aspect-ratio: 1 / 1;
}

.war-room__hotspot--upgrades {
  left: 58%;
  top: 42%;
  width: 8%;
  aspect-ratio: 1 / 1;
}

.war-room__hotspot--astrolab {
  left: 71%;
  top: 42%;
  width: 7%;
  aspect-ratio: 1 / 1;
}
</style>
