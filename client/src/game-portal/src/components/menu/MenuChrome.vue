<template>
  <div
    class="menu-chrome"
    :style="{ backgroundImage: `url(${mainMenuBackgroundUrl})` }"
    aria-hidden="true"
  ></div>
</template>

<script setup lang="ts">
import mainMenuBackgroundUrl from '@/assets/background-images/main-menu.png'
</script>

<style scoped>
.menu-chrome {
  position: fixed;
  inset: 0;
  z-index: 0;
  pointer-events: none;
  background-color: #05080d;
  background-size: cover;
  background-position: center;
  background-repeat: no-repeat;
}
</style>
