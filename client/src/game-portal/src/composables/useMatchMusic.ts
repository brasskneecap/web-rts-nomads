import { watch } from 'vue'
import { useAudioSettings } from '@/composables/useAudioSettings'

// In-match background music. Mirrors the structure of useMenuAudio, but instead
// of a single looping track it cycles through every mp3 in the match/ folder.
//
// Drop an mp3 into assets/audio/music/match/ and it joins the rotation with no
// code change — discovery is handled by import.meta.glob at build time. The
// menu loop (Iron Warchant.mp3) lives in the parent folder and is not included.

const { effectiveMusicVolume } = useAudioSettings()

const trackModules = import.meta.glob('@/assets/audio/music/match/*.mp3', {
  eager: true,
}) as Record<string, { default: string }>
// Sort keys for a stable base order before shuffling — keeps behaviour
// reproducible across builds independent of glob iteration order.
const trackUrls = Object.keys(trackModules)
  .sort()
  .map((key) => trackModules[key].default)

let audio: HTMLAudioElement | null = null
let gestureCleanup: (() => void) | null = null
let queue: string[] = []
let lastPlayed: string | null = null

// Math.random is fine here: this is client-side presentation, not server
// simulation, so the determinism invariant does not apply.
function shuffle(items: string[]): string[] {
  const out = items.slice()
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1))
    ;[out[i], out[j]] = [out[j], out[i]]
  }
  return out
}

// Refill the play queue with a fresh shuffle. If the first track would replay
// the one that just finished, swap it with the second so nothing plays
// back-to-back (only meaningful with 2+ tracks).
function refillQueue() {
  queue = shuffle(trackUrls)
  if (queue.length > 1 && queue[0] === lastPlayed) {
    ;[queue[0], queue[1]] = [queue[1], queue[0]]
  }
}

function nextTrackUrl(): string | null {
  if (trackUrls.length === 0) return null
  if (queue.length === 0) refillQueue()
  const url = queue.shift() as string
  lastPlayed = url
  return url
}

function ensureAudio(): HTMLAudioElement {
  if (audio) return audio
  audio = new Audio()
  audio.loop = false
  audio.volume = effectiveMusicVolume.value
  // Advance to the next shuffled track when the current one finishes.
  audio.addEventListener('ended', () => {
    advance()
  })
  return audio
}

// Live-update the audio element whenever the effective music volume changes
// (master or music slider moves), matching the menu-audio behaviour.
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

function play(el: HTMLAudioElement) {
  attachGestureFallback(el)
  el.play()
    .then(() => {
      gestureCleanup?.()
    })
    .catch(() => {
      // Autoplay blocked — gesture fallback (already attached) will retry.
    })
}

// Load the next shuffled track and start it.
function advance() {
  if (trackUrls.length === 0) return
  const el = ensureAudio()
  const url = nextTrackUrl()
  if (!url) return
  el.src = url
  play(el)
}

export function startMatchMusic() {
  if (trackUrls.length === 0) return
  const el = ensureAudio()
  if (!el.paused) return
  advance()
}

export function stopMatchMusic() {
  gestureCleanup?.()
  if (!audio) return
  audio.pause()
  audio.currentTime = 0
}
