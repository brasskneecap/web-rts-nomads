<template>
  <MenuChrome v-if="showMenuChrome" />
  <RouterView />
  <MenuDominionPanel v-if="showDominionPanel" />
  <StartSplash v-if="!splashDismissed" @dismiss="onSplashDismiss" />
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useRoute } from 'vue-router'
import MenuChrome from '@/components/menu/MenuChrome.vue'
import MenuDominionPanel from '@/components/menu/MenuDominionPanel.vue'
import StartSplash from '@/components/menu/StartSplash.vue'
import { startMenuMusic, stopMenuMusic } from '@/composables/useMenuAudio'
import { startMatchMusic, stopMatchMusic } from '@/composables/useMatchMusic'

const route = useRoute()
const splashDismissed = ref(false)

const showMenuChrome = computed(() => !route.meta.hideMenuChrome)

// Persistent Dominion Points readout shows on every out-of-game screen. It is
// hidden only once an actual match (or the match-end recap) is active — those
// routes are flagged `silenceMusic`, the same flag that marks "in a match".
const showDominionPanel = computed(() => !route.meta.silenceMusic)

// Music plays across the menu, war-room, kingdom and meta views. It is only
// silenced once an actual match starts (routes flagged `silenceMusic`), so it
// persists through the chrome-less war-room/kingdom/meta scenes.
const shouldPlayMusic = computed(() => !route.meta.silenceMusic)

function onSplashDismiss() {
  splashDismissed.value = true
  if (shouldPlayMusic.value) startMenuMusic()
  else startMatchMusic()
}

// Menu music plays everywhere outside a match; in-match music cycles through
// the match/ folder while a match is active. `playing` is true outside a match
// (shouldPlayMusic === !silenceMusic), so the two engines hand off here.
watch(shouldPlayMusic, (playing) => {
  if (!splashDismissed.value) return
  if (playing) {
    stopMatchMusic()
    startMenuMusic()
  } else {
    stopMenuMusic()
    startMatchMusic()
  }
})
</script>
