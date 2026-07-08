<template>
  <header
    class="hud"
    :style="{ '--hud-header-image': `url(${headerPanelUrl})` }"
  >
    <!-- Heraldic banner: purely decorative, overhangs the bar's left end.
         pointer-events disabled so it never blocks the battlefield beneath it. -->
    <img class="hud-banner" :src="bannerFlagUrl" alt="" draggable="false" />

    <!-- Wave indicator — only rendered when the server has wave mode enabled -->
    <section v-if="ui.wave.enabled" class="wave-panel" aria-label="Wave status">
      <div class="wave-label">
        {{ waveLabel }}
      </div>
      <div class="wave-timer">{{ waveTimerText }}</div>
    </section>

    <section class="resource-tray" aria-label="Resources">
      <article
        v-for="resource in ui.player.resources"
        :key="resource.id"
        class="resource-card"
        :title="resource.label"
        :aria-label="`${resource.label}: ${resource.amount}`"
      >
        <img
          v-if="getResourceIconUrl(resource.id)"
          :src="getResourceIconUrl(resource.id)!"
          :alt="resource.label"
          class="resource-icon"
          draggable="false"
        />
        <span
          v-else
          class="resource-gem"
          :style="{ background: `linear-gradient(180deg, ${resource.accent}, rgba(0,0,0,0.65))` }"
        ></span>
        <div class="resource-copy">
          <div class="resource-amount">{{ resource.max != null ? `${resource.amount}/${resource.max}` : resource.amount }}</div>
        </div>
      </article>
    </section>

  </header>

  <transition-group name="toast" tag="div" class="toast-stack">
    <div
      v-for="n in ui.notifications"
      :key="n.id"
      class="toast"
    >{{ n.message }}</div>
  </transition-group>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { GameUiSnapshot } from '@/game/core/GameClient'
import headerPanelUrl from '@/assets/ui/themes/updated/header-panel.png'
import bannerFlagUrl from '@/assets/ui/themes/updated/banner-flag.png'
import { getResourceIconUrl } from '@/game/rendering/resourceSprites'

const props = defineProps<{
  ui: GameUiSnapshot
}>()

function formatSeconds(s: number): string {
  const total = Math.max(0, Math.ceil(s))
  const m = Math.floor(total / 60)
  const sec = total % 60
  return `${m}:${sec.toString().padStart(2, '0')}`
}

const waveLabel = computed(() => {
  const w = props.ui.wave
  // "complete" only means the wave state machine finished — actual victory
  // is gated server-side on required objectives too. Don't say "Victory"
  // here; the real victory/defeat flow owns that word.
  if (w.state === 'complete') return 'Waves Complete'
  if (w.state === 'prep') {
    const next = w.currentWave + 1
    return `Wave ${next}`
  }
  return `Wave ${w.currentWave}`
})

const waveTimerText = computed(() => {
  const w = props.ui.wave
  if (w.state === 'prep') return `Next Wave In: ${formatSeconds(w.timer)}`
  if (w.state === 'active') {
    const timerExpired = w.waveDuration > 0 && w.timer >= w.waveDuration
    if (timerExpired) return 'Finish them!'
    return `Wave Time: ${formatSeconds(w.waveDuration - w.timer)}`
  }
  return ''
})
</script>

<style scoped>
.hud {
  position: relative;
  /* Above DebugSpawnPanel (z-index: 10) so the settings dropdown — which is
     clipped to this stacking context — pops in front of the debug panel when
     both are open. The HUD itself is along the top edge and doesn't overlap
     other UI, so elevating the whole bar is harmless. */
  z-index: 20;
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 18px;

  /* Header-bar art: 264×58 with ornamental 54px metal ends and a stretchable
     wood middle. Horizontal 3-slice — freeze the 54px ends, stretch the middle
     across whatever width the bar takes. Height is pinned to the native 58px so
     the ends never distort vertically. The 54px side borders also keep content
     (crest, resources) clear of the corner brackets, so no extra side padding. */
  height: 58px;
  box-sizing: border-box;
  background: none;
  border-style: solid;
  border-width: 0 54px;
  border-image-source: var(--hud-header-image);
  border-image-slice: 0 54 fill;
  border-image-width: 0 54px;
  border-image-repeat: stretch;
  /* No image-rendering: pixelated here — this is detailed art, not a pixel tile. */
}

.hud-banner {
  position: absolute;
  top: -8px;
  left: 4px;
  width: 84px;
  height: auto;
  z-index: 2;
  /* Decorative overhang — must not intercept clicks meant for the battlefield. */
  pointer-events: none;
  filter: drop-shadow(0 5px 7px rgba(0, 0, 0, 0.5));
}

.resource-tray {
  flex: 0 1 420px;
  min-width: 0;
  display: flex;
  justify-content: flex-end;
  flex-wrap: nowrap;
  gap: 10px;
}

.resource-card {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 10px;
  min-width: 120px;
  padding: 4px 12px;
  border-radius: 4px;
  background: #000;
  border: 1px solid rgba(200, 164, 106, 0.28);
  box-shadow: 0 5px 12px rgba(0, 0, 0, 0.18);
}

.resource-gem {
  width: 12px;
  height: 12px;
  border-radius: 999px;
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.28),
    0 0 0 2px rgba(27, 16, 10, 0.35);
}

/* PNG-based resource icon used when assets/resources/<id>.png exists.
   Sized slightly larger than the gem to read clearly. */
.resource-icon {
  width: 20px;
  height: 20px;
  flex: 0 0 20px;
  object-fit: contain;
  image-rendering: pixelated;
}

.resource-copy {
  display: flex;
  align-items: baseline;
  gap: 8px;
  /* Push the amount text to the right edge of the card while the icon
     stays left-aligned via the card's flex flow. */
  margin-left: auto;
  text-align: right;
}

.resource-label {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #d7bb84;
}

.resource-amount {
  font-size: 18px;
  font-weight: 700;
  color: #fff2d6;
}

.wave-panel {
  flex: 1 1 220px;
  min-width: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 1px;
}

.wave-label {
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: #d7bb84;
  white-space: nowrap;
  line-height: 1.1;
}

.wave-timer {
  font-size: 11px;
  font-weight: 600;
  color: #cbb893;
  line-height: 1.1;
  letter-spacing: 0.06em;
  white-space: nowrap;
}

.hud-command {
  flex: 1 1 320px;
  min-width: 0;
  text-align: center;
}

.hud-copy {
  margin-top: 2px;
  font-size: 13px;
  line-height: 1.35;
  color: #cbb893;
  text-align: center;
}

@media (max-width: 900px) {
  .hud {
    gap: 10px;
  }

  .resource-tray {
    flex: 1 1 0;
    min-width: 0;
    justify-content: flex-end;
    overflow-x: auto;
    scrollbar-width: none;
  }

  .resource-tray::-webkit-scrollbar {
    display: none;
  }
}

@media (max-width: 600px) {
  .hud {
    gap: 8px;
  }

  .resource-card {
    min-width: 0;
    padding: 5px 9px;
    gap: 6px;
  }

  .resource-label {
    font-size: 10px;
  }

  .resource-amount {
    font-size: 14px;
  }
}

.toast-stack {
  position: fixed;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  z-index: 100;
  pointer-events: none;
}

.toast {
  background: rgba(20, 10, 10, 0.88);
  color: #f0c070;
  border: 1px solid rgba(200, 80, 60, 0.5);
  border-radius: 6px;
  padding: 7px 16px;
  font-size: 13px;
  font-weight: 600;
  backdrop-filter: blur(6px);
  white-space: nowrap;
}

.toast-enter-active {
  transition: opacity 0.15s ease, transform 0.15s ease;
}
.toast-leave-active {
  transition: opacity 0.3s ease, transform 0.3s ease;
}
.toast-enter-from {
  opacity: 0;
  transform: translateY(-6px);
}
.toast-leave-to {
  opacity: 0;
  transform: translateY(-6px);
}
</style>
