<template>
  <MenuChrome v-if="showMenuChrome" />
  <RouterView />
  <StartSplash v-if="!splashDismissed" @dismiss="onSplashDismiss" />
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useRoute } from 'vue-router'
import MenuChrome from '@/components/menu/MenuChrome.vue'
import StartSplash from '@/components/menu/StartSplash.vue'
import { startMenuMusic, stopMenuMusic } from '@/composables/useMenuAudio'

const route = useRoute()
const splashDismissed = ref(false)

const showMenuChrome = computed(() => !route.meta.hideMenuChrome)

// Music plays across the menu, war-room, kingdom and meta views. It is only
// silenced once an actual match starts (routes flagged `silenceMusic`), so it
// persists through the chrome-less war-room/kingdom/meta scenes.
const shouldPlayMusic = computed(() => !route.meta.silenceMusic)

function onSplashDismiss() {
  splashDismissed.value = true
  if (shouldPlayMusic.value) startMenuMusic()
}

watch(shouldPlayMusic, (playing) => {
  if (!splashDismissed.value) return
  if (playing) startMenuMusic()
  else stopMenuMusic()
})
</script>
