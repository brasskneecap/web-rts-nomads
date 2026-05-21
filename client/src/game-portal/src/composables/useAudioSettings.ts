import { ref, computed, watch } from 'vue'

// Volume settings persist to localStorage as 0..1 floats. Three independent
// channels: master, music, sfx. The "effective" volume for any sound is
// master * channel — that lets the options panel raise/lower a category in
// isolation, while master scales everything at once.
//
// Music is wired through useMenuAudio (and any future in-match music). SFX
// is not yet hooked up to a real audio engine — useSoundHooks reads its
// effective volume so the plumbing is in place for when SFX lands.

const MASTER_KEY = 'webrts.audio.masterVolume'
const MUSIC_KEY = 'webrts.audio.musicVolume'
const SFX_KEY = 'webrts.audio.sfxVolume'

function loadVolume(key: string, fallback: number): number {
  const raw = localStorage.getItem(key)
  if (raw === null) return fallback
  const n = parseFloat(raw)
  if (Number.isNaN(n)) return fallback
  return Math.max(0, Math.min(1, n))
}

const masterVolume = ref(loadVolume(MASTER_KEY, 1))
const musicVolume = ref(loadVolume(MUSIC_KEY, 0.4))
const sfxVolume = ref(loadVolume(SFX_KEY, 1))

watch(masterVolume, (v) => localStorage.setItem(MASTER_KEY, String(v)))
watch(musicVolume, (v) => localStorage.setItem(MUSIC_KEY, String(v)))
watch(sfxVolume, (v) => localStorage.setItem(SFX_KEY, String(v)))

const effectiveMusicVolume = computed(() => masterVolume.value * musicVolume.value)
const effectiveSfxVolume = computed(() => masterVolume.value * sfxVolume.value)

export function useAudioSettings() {
  return {
    masterVolume,
    musicVolume,
    sfxVolume,
    effectiveMusicVolume,
    effectiveSfxVolume,
  }
}
