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
      ></div>
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
  bottom: 40px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 2;
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
 */
.meta-scene__scene {
  position: relative;
  aspect-ratio: 1672 / 941;
  min-width: 100%;
  min-height: 100%;
  background-size: 100% 100%;
  background-position: center;
  background-repeat: no-repeat;
  image-rendering: pixelated;
}
</style>
