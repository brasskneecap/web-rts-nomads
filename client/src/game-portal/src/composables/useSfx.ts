import { useAudioSettings } from '@/composables/useAudioSettings'

// One-shot sound-effect engine for in-match situations (building clicked, and
// later unit attacks, construction, etc.). Mirrors the build-time glob pattern
// used for building sprites: drop files in the asset folders and they are
// discovered automatically — no manual registry.
//
// Two sources are globbed:
//   - assets/audio/sfx/*.{mp3,wav,ogg}      → the actual sound files
//   - assets/buildings/<type>/sounds.json   → which file plays for which event
//
// A building's sounds.json is a flat map of event name → filename, e.g.
//   { "select": "barracks_select.mp3" }
// keyed into the sfx folder by bare filename. The shape is intentionally open
// so new events (attack, build, destroy, ...) are just new keys.

const { effectiveSfxVolume } = useAudioSettings()

const sfxUrls = import.meta.glob('@/assets/audio/sfx/*.{mp3,wav,ogg}', {
  eager: true,
  query: '?url',
  import: 'default',
}) as Record<string, string>

// filename (e.g. "barracks_select.mp3") → resolved asset URL
const urlByFilename = new Map<string, string>()
for (const [path, url] of Object.entries(sfxUrls)) {
  const name = path.split('/').pop()
  if (name) urlByFilename.set(name, url)
}

type BuildingSounds = Record<string, string>

const soundConfigs = import.meta.glob('@/assets/buildings/*/sounds.json', {
  eager: true,
}) as Record<string, { default: BuildingSounds }>

// building type key (lowercased folder name) → its event→filename map
const soundsByBuilding = new Map<string, BuildingSounds>()
for (const [path, mod] of Object.entries(soundConfigs)) {
  const match = path.match(/\/buildings\/([^/]+)\/sounds\.json$/)
  if (!match) continue
  soundsByBuilding.set(match[1].toLowerCase(), mod.default)
}

// Resolves the filename a building has configured for an event (e.g. 'select'),
// or null when the building has no sounds.json or no entry for that event.
function resolveBuildingSound(buildingType: string, event: string): string | null {
  const config = soundsByBuilding.get(buildingType.toLowerCase())
  if (!config) return null
  return config[event] ?? null
}

// Plays a one-shot sound effect by bare filename at the current effective SFX
// volume. A fresh HTMLAudioElement per call lets effects overlap freely; it is
// garbage-collected once playback finishes. Unknown filenames are a no-op.
export function playSfx(filename: string) {
  const url = urlByFilename.get(filename)
  if (!url) return
  const el = new Audio(url)
  el.volume = effectiveSfxVolume.value
  // By the time any in-match SFX fires the user has already interacted with the
  // page, so autoplay is unlocked — no gesture fallback needed here.
  el.play().catch(() => {})
}

// Fire-and-forget building sound for one-shot events (future: attack, build,
// destroy, ...). Not tied to selection — use playBuildingSelectSound for the
// selection sound that must stop on deselect.
export function playBuildingSound(buildingType: string, event: string) {
  const filename = resolveBuildingSound(buildingType, event)
  if (filename) playSfx(filename)
}

// The building "select" sound lives on a single interruptible channel: only one
// can play at a time, and it is cut when the building is deselected (see
// stopBuildingSelectSound, driven by the selection snapshot) or when another
// building is selected.
let currentSelectAudio: HTMLAudioElement | null = null

// Plays the building's configured 'select' sound, replacing any select sound
// already playing. No-ops when the building has no select sound configured —
// but still stops the previous one, so selecting a silent building clears the
// prior building's sound.
export function playBuildingSelectSound(buildingType: string) {
  stopBuildingSelectSound()
  const filename = resolveBuildingSound(buildingType, 'select')
  if (!filename) return
  const url = urlByFilename.get(filename)
  if (!url) return
  const el = new Audio(url)
  el.volume = effectiveSfxVolume.value
  el.addEventListener('ended', () => {
    if (currentSelectAudio === el) currentSelectAudio = null
  })
  currentSelectAudio = el
  el.play().catch(() => {})
}

// Stops the current building-select sound, if any. Called on deselect.
export function stopBuildingSelectSound() {
  if (!currentSelectAudio) return
  currentSelectAudio.pause()
  currentSelectAudio.currentTime = 0
  currentSelectAudio = null
}
