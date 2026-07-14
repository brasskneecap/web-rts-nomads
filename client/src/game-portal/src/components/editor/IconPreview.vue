<template>
  <!-- The def's art in the same brass frame the vault and shop use. Frameless
       on purpose: it sits inside whatever card the editor puts it in, rather
       than bringing a panel of its own. Units / abilities / effects will drop a
       sprite preview into the same slot. -->
  <div class="ed-icon" :style="{ '--icon-frame': `url(${iconFrameUrl})`, '--icon-size': `${size}px` }">
    <img v-if="src" :src="src" class="ed-icon__img" alt="" />
  </div>
</template>

<script setup lang="ts">
import iconFrameUrl from '@/assets/ui/themes/updated/icon_container.png'

withDefaults(defineProps<{
  /** Source url of the icon. Empty renders an empty frame. */
  src?: string
  /** Outer frame size in px. */
  size?: number
}>(), {
  size: 128,
})
</script>

<style scoped>
.ed-icon {
  width: var(--icon-size);
  height: var(--icon-size);
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--icon-frame) center / 100% 100% no-repeat;
  image-rendering: pixelated;
}

.ed-icon__img {
  width: 68%;
  height: 68%;
  object-fit: contain;
  image-rendering: pixelated;
}
</style>
