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

function onSplashDismiss() {
  splashDismissed.value = true
  if (showMenuChrome.value) startMenuMusic()
}

watch(showMenuChrome, (visible) => {
  if (!splashDismissed.value) return
  if (visible) startMenuMusic()
  else stopMenuMusic()
})
</script>
