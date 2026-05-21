import { watch } from 'vue'
import ironWarchantUrl from '@/assets/audio/music/Iron Warchant.mp3'
import { useAudioSettings } from '@/composables/useAudioSettings'

const { effectiveMusicVolume } = useAudioSettings()

let audio: HTMLAudioElement | null = null
let gestureCleanup: (() => void) | null = null

function ensureAudio(): HTMLAudioElement {
  if (audio) return audio
  audio = new Audio(ironWarchantUrl)
  audio.loop = true
  audio.volume = effectiveMusicVolume.value
  return audio
}

// Live-update the audio element whenever the effective music volume changes
// (master or music slider moves). Subscribed once at module load so the
// options panel can preview changes in real time without restarting playback.
watch(effectiveMusicVolume, (v) => {
  if (audio) audio.volume = v
})

function attachGestureFallback(el: HTMLAudioElement) {
  if (gestureCleanup) return
  const onGesture = () => {
    void el.play().catch(() => {})
    cleanup()
  }
  const cleanup = () => {
    window.removeEventListener('pointerdown', onGesture)
    window.removeEventListener('keydown', onGesture)
    gestureCleanup = null
  }
  window.addEventListener('pointerdown', onGesture, { once: true })
  window.addEventListener('keydown', onGesture, { once: true })
  gestureCleanup = cleanup
}

export function startMenuMusic() {
  const el = ensureAudio()
  if (!el.paused) return
  attachGestureFallback(el)
  el.play()
    .then(() => {
      gestureCleanup?.()
    })
    .catch(() => {
      // Autoplay blocked — gesture fallback (already attached) will retry.
    })
}

export function stopMenuMusic() {
  gestureCleanup?.()
  if (!audio) return
  audio.pause()
  audio.currentTime = 0
}
