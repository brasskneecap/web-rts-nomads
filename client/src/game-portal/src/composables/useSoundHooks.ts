// Stubs — wire to a real audio engine later. When playHover / playClick
// actually trigger an HTMLAudioElement (or Web Audio node), pull
// `effectiveSfxVolume` from useAudioSettings and scale the playback volume
// by it so the SFX slider in Options takes effect immediately.
export function useSoundHooks() {
  return {
    playHover: () => {},
    playClick: () => {},
  }
}
