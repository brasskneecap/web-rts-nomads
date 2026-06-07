<template>
  <div class="meta-scene">
    <div class="meta-scene__exit">
      <ExitButton aria-label="Back to Kingdom" @click="onBack" />
    </div>

    <div class="meta-scene__stage">
      <div
        class="meta-scene__scene"
        role="img"
        :aria-label="title"
        :style="{ backgroundImage: `url(${bg})` }"
      >
        <!-- Overlay slot: scene-relative so children can be positioned in
             percentages and stay locked to the cover-fit artwork. -->
        <slot />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import ExitButton from '@/components/ui/ExitButton.vue'

defineProps<{
  bg: string
  title: string
}>()

const router = useRouter()

function onBack() {
  router.push('/kingdom')
}
</script>

<style scoped>
.meta-scene {
  position: relative;
  z-index: 1;
  width: 100%;
  height: 100%;
  overflow: hidden;
  background-color: #05080d;
}

.meta-scene__exit {
  position: absolute;
  top: 50px;
  left: 50px;
  z-index: 2;
}

/* Meta views use a larger exit icon (2x the base) pinned to the top-left. */
.meta-scene__exit :deep(.exit-button) {
  width: 112px;
  height: 112px;
}

.meta-scene__stage {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
}

/*
 * Cover-style sizing matches KingdomView: the scene preserves the
 * background's aspect ratio and grows to fill both axes — no letterbox.
 * Any overflow is clipped by the stage.
 *
 * `--scene-min-width` is a hard floor: once the viewport gets narrower than
 * this, the scene stops shrinking and the stage crops it symmetrically
 * instead — so the artwork (and any overlaid panels) never shrink past a
 * usable size. Raise this number to crop sooner / keep things larger; lower
 * it to allow more shrinkage.
 */
.meta-scene__scene {
  --scene-min-width: 1500px;
  position: relative;
  aspect-ratio: 1672 / 941;
  min-width: max(100%, var(--scene-min-width));
  min-height: 100%;
  background-size: 100% 100%;
  background-position: center;
  background-repeat: no-repeat;
  image-rendering: pixelated;
}
</style>
