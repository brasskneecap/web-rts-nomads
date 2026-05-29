<!--
  DebugHud — network diagnostics overlay (Phase 1A).

  Toggled with F3 from Match.vue. Surfaces the per-snapshot timing the
  client already has on hand so we can pinpoint where a multiplayer lag
  is being introduced WITHOUT touching the wire protocol or the tick path.

  Reading the panel:
    age   = Date.now() - snap.serverNow at receive time. Includes any
            clock skew between machines, but a steady ~5000ms value is
            far above any plausible skew so the signal is unambiguous:
            the snapshot was authored that many ms before the SPA saw it.
    gap   = ms between consecutive snapshot arrivals. Server broadcasts
            at 20 Hz so the healthy steady state is ~50ms; spikes appear
            here. A consistent ~50ms with a large `age` means snapshots
            are arriving on cadence but each one is stale (buffered
            somewhere upstream). A jittery gap means jitter in transit.
    rate  = snapshots/sec received over the last 1s window. Should be
            ~20. Drops indicate dropped or delayed packets.
    buf   = interpolation buffer depth. Set by detectInitialInterpolationDelayMs
            (100ms LAN / 200ms Steam joiner). Underrun → stutter, not lag.
    bytes = wire byte length of the last snapshot frame. * 20 Hz gives
            approximate bandwidth in use; compare against the friend's
            uplink to spot capacity exhaustion.

  Phase 1B's server-side `[send-profile]` log line (WEBRTS_SEND_PROFILE=1)
  is the matching server-side view — read together they isolate whether a
  delay lives in the server write path, in transit, or downstream.
-->
<template>
  <div class="debug-hud" role="status" aria-label="Network diagnostics">
    <div class="dh-head">
      <span class="dh-title">NET</span>
      <span class="dh-transport" :class="transportClass">{{ stats.transportLabel }}</span>
    </div>
    <dl class="dh-grid">
      <dt>age</dt>
      <dd :class="ageClass">
        {{ stats.snapshotAgeMs }}ms
        <span class="dh-sub">(avg {{ Math.round(stats.snapshotAgeAvgMs) }} · max {{ stats.snapshotAgeMaxMs }})</span>
      </dd>

      <dt>gap</dt>
      <dd :class="gapClass">
        {{ stats.receiveGapMs }}ms
        <span class="dh-sub">(max {{ stats.receiveGapMaxMs }})</span>
      </dd>

      <dt>rate</dt>
      <dd :class="rateClass">{{ stats.snapshotsPerSec }}/s</dd>

      <dt>buf</dt>
      <dd>{{ stats.bufferDepth }}</dd>

      <dt>bytes</dt>
      <dd>{{ formatBytes(stats.lastSnapshotBytes) }}</dd>

      <dt>recv</dt>
      <dd>{{ stats.totalSnapshots }}</dd>
    </dl>
    <div class="dh-hint">F3 to hide</div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { NetStats } from '@/game/core/GameState'

const props = defineProps<{
  stats: NetStats
}>()

// Age thresholds: server is 50ms/tick so anything under 200ms is healthy
// for any path. 200-500ms is jittery but playable; >500ms is the lag we
// are hunting. Tuned to the Steam-joiner 200ms interp buffer so the
// "healthy steam" steady state shouldn't trip yellow.
const ageClass = computed(() => {
  const a = props.stats.snapshotAgeMs
  if (a >= 500) return 'dh-bad'
  if (a >= 200) return 'dh-warn'
  return 'dh-ok'
})

// Gap: 20 Hz cadence = 50ms. Up to 100ms is normal jitter. Over 200ms
// means at least one tick was dropped or held back; over 500ms means
// rendering is starving the interpolation buffer.
const gapClass = computed(() => {
  const g = props.stats.receiveGapMs
  if (g >= 500) return 'dh-bad'
  if (g >= 200) return 'dh-warn'
  return 'dh-ok'
})

// Rate: server broadcasts at 20 Hz. 18-22 is healthy noise. Below 15
// means we're losing snapshots, above 25 means burst delivery (e.g. a
// queue draining all at once after a stall — also a smell).
const rateClass = computed(() => {
  const r = props.stats.snapshotsPerSec
  if (r === 0) return ''
  if (r < 15 || r > 25) return 'dh-warn'
  return 'dh-ok'
})

const transportClass = computed(() =>
  props.stats.transportLabel === 'steam-proxy' ? 'dh-transport-steam' : 'dh-transport-direct',
)

function formatBytes(n: number): string {
  if (n < 1024) return `${n}B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)}KB`
  return `${(n / 1024 / 1024).toFixed(2)}MB`
}
</script>

<style scoped>
.debug-hud {
  position: fixed;
  top: 12px;
  right: 12px;
  z-index: 200;
  min-width: 220px;
  padding: 8px 10px 6px;
  background: rgba(20, 14, 8, 0.86);
  color: #f1e7d0;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  line-height: 1.35;
  border: 1px solid rgba(212, 168, 79, 0.5);
  border-radius: 4px;
  box-shadow: 0 6px 18px rgba(0, 0, 0, 0.45);
  pointer-events: none;
  user-select: none;
}

.dh-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 4px;
  padding-bottom: 4px;
  border-bottom: 1px solid rgba(212, 168, 79, 0.25);
}

.dh-title {
  letter-spacing: 0.12em;
  font-weight: 600;
  color: #d4a84f;
}

.dh-transport {
  font-size: 10px;
  padding: 1px 6px;
  border-radius: 3px;
  letter-spacing: 0.05em;
  text-transform: uppercase;
}

.dh-transport-steam {
  background: rgba(64, 130, 200, 0.25);
  color: #b6d4f0;
  border: 1px solid rgba(64, 130, 200, 0.55);
}

.dh-transport-direct {
  background: rgba(120, 154, 82, 0.22);
  color: #c9dfa8;
  border: 1px solid rgba(120, 154, 82, 0.5);
}

.dh-grid {
  display: grid;
  grid-template-columns: 44px 1fr;
  gap: 2px 10px;
  margin: 0;
}

.dh-grid dt {
  color: #8a7a55;
  text-align: right;
}

.dh-grid dd {
  margin: 0;
  font-variant-numeric: tabular-nums;
}

.dh-sub {
  margin-left: 6px;
  color: #8a7a55;
  font-size: 11px;
}

.dh-ok {
  color: #c9dfa8;
}

.dh-warn {
  color: #f0c87a;
}

.dh-bad {
  color: #f08a6f;
  font-weight: 600;
}

.dh-hint {
  margin-top: 4px;
  padding-top: 4px;
  border-top: 1px solid rgba(212, 168, 79, 0.18);
  color: #6c5e40;
  font-size: 10px;
  text-align: right;
  letter-spacing: 0.05em;
}
</style>
