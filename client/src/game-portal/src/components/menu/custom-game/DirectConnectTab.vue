<template>
  <div class="cg-direct" :style="assetVars">
    <div class="cg-direct__scroll">
    <!-- Active-proxy banner -->
    <div v-if="activeHost" class="cg-direct__active-banner">
      <div class="cg-direct__active-text">
        Currently connected to <strong>{{ activeHost }}</strong> via Direct Connect.
      </div>
      <button type="button" class="cg-action cg-action--muted cg-action--sm" @click="disconnect">
        Disconnect &amp; Reload
      </button>
    </div>

    <!-- Host section -->
    <UiPanel variant="innerPanel" :padding="0" class="cg-direct__section">
      <div class="cg-direct__section-inner">
        <div class="cg-direct__section-title">Host: Allow LAN / Internet</div>
        <div class="cg-direct__section-desc">
          Let other players on your network (or with a routable address to your
          machine) connect to a game you start.
          <strong>Anyone with this address can join &mdash; share like a Discord link.</strong>
          NAT traversal is not attempted; you are responsible for reachability.
        </div>

        <div class="cg-direct__row">
          <label class="cg-direct__toggle">
            <input type="checkbox" :checked="hostAllow" @change="onToggleAllow" />
            <span>Allow LAN/Internet connections</span>
          </label>
          <span v-if="toggleBusy" class="cg-direct__busy">Saving&hellip;</span>
          <span v-if="toggleError" class="cg-direct__error">{{ toggleError }}</span>
        </div>

        <div v-if="hostAllow" class="cg-direct__ips">
          <div class="cg-direct__ips-label">Reachable addresses on this machine:</div>
          <ul v-if="ips.length > 0" class="cg-direct__ips-list">
            <li v-for="(ip, idx) in ips" :key="ip" class="cg-direct__ip-item">
              <code>{{ ip }}:{{ port }}</code>
              <button type="button" class="cg-action cg-action--sm" @click="copyAddress(ip)">
                {{ copiedAddress === ip ? 'Copied!' : 'Copy' }}
              </button>
              <span v-if="idx === 0" class="cg-direct__ip-hint">(suggested)</span>
            </li>
          </ul>
          <div v-else class="cg-direct__ips-empty">
            No non-loopback addresses detected. Connect to a network first or
            install Tailscale.
          </div>
          <div class="cg-direct__ips-note">
            Disabling the toggle does not end this session &mdash; close the
            window to do that. Existing connections stay live.
          </div>
        </div>
      </div>
    </UiPanel>

    <!-- Joiner section -->
    <UiPanel variant="innerPanel" :padding="0" class="cg-direct__section">
      <div class="cg-direct__section-inner">
        <div class="cg-direct__section-title">Join a remote host</div>
        <div class="cg-direct__section-desc">
          Paste the address the host shared with you. The format is
          <code>host:port</code> &mdash; for example
          <code>192.168.1.50:8080</code> or <code>100.64.0.5:8080</code>.
        </div>

        <div class="cg-direct__row">
          <input
            v-model="hostInput"
            type="text"
            placeholder="host:port"
            class="cg-direct__input"
            :disabled="joining"
            @keyup.enter="onJoin"
          />
          <button
            type="button"
            class="cg-action cg-action--start"
            :disabled="joining || !hostInput"
            @click="onJoin"
          >
            {{ joining ? 'Connecting…' : 'Connect' }}
          </button>
        </div>

        <div v-if="joinError" class="cg-direct__error joiner-error">
          {{ joinError }}
        </div>
      </div>
    </UiPanel>
    </div>

    <div class="cg-direct__footer">
      <BackButton @click="emit('back')" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import * as dc from '@/services/directConnect'
import UiPanel from '@/components/ui/UiPanel.vue'
import BackButton from '@/components/menu/custom-game/BackButton.vue'
import activeBtnUrl from '@/assets/ui/themes/updated/war-room/war-room-active-button.png'
import inactiveBtnUrl from '@/assets/ui/themes/updated/war-room/war-room-inactive-button.png'

// Button art exposed to scoped CSS as custom properties.
const assetVars = computed(() => ({
  '--dc-active': `url(${activeBtnUrl})`,
  '--dc-inactive': `url(${inactiveBtnUrl})`,
}))

// `back` closes the Custom Game popup (mirrors the Start Game tab's Back).
const emit = defineEmits<{
  (e: 'back'): void
}>()

// Active-proxy banner state — present iff sessionStorage already has a token.
const activeHost = ref<string | null>(dc.activeProxyHost())

// Host-toggle state.
const hostAllow = ref(false)
const toggleBusy = ref(false)
const toggleError = ref('')

// Reachable IPs + the port we listen on (page-origin port).
const ips = ref<string[]>([])
const port = ref<string>(window.location.port || '8080')

// Joiner-side state.
const hostInput = ref('')
const joining = ref(false)
const joinError = ref('')

// IP copy-feedback.
const copiedAddress = ref<string | null>(null)

async function refreshToggle() {
  try {
    const s = await dc.getToggleState()
    hostAllow.value = s.allow
    if (s.allow) await refreshIps()
  } catch (e) {
    toggleError.value = `Could not read toggle state: ${(e as Error).message}`
  }
}

async function refreshIps() {
  try {
    const result = await dc.getReachableIps()
    ips.value = result.ips
  } catch (e) {
    ips.value = []
    toggleError.value = `Could not read IPs: ${(e as Error).message}`
  }
}

async function onToggleAllow(ev: Event) {
  const next = (ev.target as HTMLInputElement).checked
  toggleBusy.value = true
  toggleError.value = ''
  try {
    const s = await dc.setToggleState(next)
    hostAllow.value = s.allow
    if (s.allow) await refreshIps()
  } catch (e) {
    toggleError.value = `Could not save: ${(e as Error).message}`
  } finally {
    toggleBusy.value = false
  }
}

async function copyAddress(ip: string) {
  const address = `${ip}:${port.value}`
  try {
    await navigator.clipboard.writeText(address)
    copiedAddress.value = ip
    setTimeout(() => {
      if (copiedAddress.value === ip) copiedAddress.value = null
    }, 1500)
  } catch {
    // Clipboard access may be denied; users can still select+copy manually.
  }
}

async function onJoin() {
  const hostPort = hostInput.value.trim()
  if (!hostPort) return
  joining.value = true
  joinError.value = ''
  try {
    await dc.joinHost(hostPort)
    // Hard reload so NetworkClient reinitialises and picks up the proxy
    // token on its first WS open. Simpler than threading a "reconnect"
    // command through every composable that holds a WS reference. Land back
    // on the war-room with the Custom Game / Direct Connect tab open.
    window.location.assign(window.location.pathname + '#/war-room?tab=custom&sub=direct')
    window.location.reload()
  } catch (e) {
    joinError.value = dc.dialErrorMessage(e as dc.JoinError)
  } finally {
    joining.value = false
  }
}

function disconnect() {
  dc.clearProxy()
  window.location.reload()
}

onMounted(() => {
  void refreshToggle()
})
</script>

<style scoped>
.cg-direct {
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  min-height: 0;
  color: #e9dbb8;
}

/* Scroll region holds the banner + sections so the footer stays pinned to the
   bottom (and long content scrolls) rather than pushing the footer down. */
.cg-direct__scroll {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 16);
}

/* Footer with the Back button (bottom-left) that closes the popup. Matches the
   Start Game / Find Game footer geometry so the Back button stays put. */
.cg-direct__footer {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  padding-top: calc(var(--s) * 12);
}

/* Each section sits on an inner-panel; content padding is on the inner div. */
.cg-direct__section {
  display: flex;
  flex-direction: column;
}

.cg-direct__section-inner {
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 10);
  padding: calc(var(--s) * 14) calc(var(--s) * 16);
}

.cg-direct__section-title {
  font-family: var(--font-title);
  font-size: calc(var(--s) * 16);
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: #e7c88a;
}

.cg-direct__section-desc {
  font-size: calc(var(--s) * 13);
  color: rgba(233, 219, 184, 0.85);
  line-height: 1.5;
}

.cg-direct__section-desc code,
.cg-direct__ip-item code {
  background: rgba(0, 0, 0, 0.4);
  border: 1px solid rgba(198, 158, 90, 0.3);
  color: #e6d3a3;
  padding: calc(var(--s) * 1) calc(var(--s) * 6);
  border-radius: calc(var(--s) * 3);
  font-size: calc(var(--s) * 13);
}

.cg-direct__row {
  display: flex;
  align-items: center;
  gap: calc(var(--s) * 12);
  flex-wrap: wrap;
}

.cg-direct__toggle {
  display: flex;
  align-items: center;
  gap: calc(var(--s) * 8);
  color: #e9dbb8;
}

.cg-direct__input {
  flex: 1;
  min-width: calc(var(--s) * 220);
  font-family: monospace;
  font-size: calc(var(--s) * 14);
  padding: calc(var(--s) * 8) calc(var(--s) * 12);
  background: rgba(0, 0, 0, 0.4);
  color: #f0e2c0;
  border: 1px solid rgba(198, 158, 90, 0.45);
  border-radius: calc(var(--s) * 4);
}

.cg-direct__input::placeholder {
  color: rgba(233, 219, 184, 0.4);
}

.cg-direct__busy {
  font-size: calc(var(--s) * 12);
  color: rgba(233, 219, 184, 0.65);
}

.cg-direct__error {
  font-size: calc(var(--s) * 13);
  color: #e88a6a;
}

.cg-direct__error.joiner-error {
  margin-top: calc(var(--s) * 6);
}

.cg-direct__ips {
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 8);
  margin-top: calc(var(--s) * 6);
}

.cg-direct__ips-label {
  font-size: calc(var(--s) * 13);
  color: #c7a768;
}

.cg-direct__ips-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 6);
}

.cg-direct__ip-item {
  display: flex;
  align-items: center;
  gap: calc(var(--s) * 10);
}

.cg-direct__ip-item code {
  font-size: calc(var(--s) * 14);
  color: #f0e2c0;
}

.cg-direct__ip-hint {
  font-size: calc(var(--s) * 12);
  color: rgba(233, 219, 184, 0.55);
}

.cg-direct__ips-empty {
  font-size: calc(var(--s) * 13);
  color: #e88a6a;
  font-style: italic;
}

.cg-direct__ips-note {
  font-size: calc(var(--s) * 11);
  color: rgba(233, 219, 184, 0.55);
  margin-top: calc(var(--s) * 4);
}

.cg-direct__active-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: calc(var(--s) * 16);
  border: 1px solid rgba(120, 160, 90, 0.55);
  background: rgba(40, 60, 30, 0.55);
  border-radius: calc(var(--s) * 4);
  padding: calc(var(--s) * 14) calc(var(--s) * 16);
}

.cg-direct__active-text {
  color: #bcd89a;
  font-size: calc(var(--s) * 14);
}

/* War-room button art — active blue for the primary Connect action, dark for
   utility buttons. Matches the Start Game footer buttons. */
.cg-action {
  font-family: var(--font-title);
  font-size: calc(var(--s) * 13);
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #f4e3b6;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.7);
  padding: calc(var(--s) * 6) calc(var(--s) * 18);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: none;
  border: calc(var(--s) * 15) solid transparent;
  border-image-source: var(--dc-inactive);
  border-image-slice: 14 fill;
  border-image-width: calc(var(--s) * 15);
  border-image-repeat: stretch;
  image-rendering: pixelated;
  transition:
    filter 120ms ease,
    transform 80ms ease;
}

.cg-action--start {
  border-image-source: var(--dc-active);
}

.cg-action--muted {
  border-image-source: var(--dc-inactive);
}

.cg-action--sm {
  font-size: calc(var(--s) * 11);
  border-width: calc(var(--s) * 12);
  border-image-width: calc(var(--s) * 12);
  padding: calc(var(--s) * 3) calc(var(--s) * 10);
}

.cg-action:hover:not(:disabled) {
  filter: brightness(1.12);
}

.cg-action:active:not(:disabled) {
  filter: brightness(0.9);
  transform: translateY(1px);
}

.cg-action:disabled {
  color: rgba(244, 227, 182, 0.4);
  /* `cursor: not-allowed` is the system semantic for "forbidden action" — the
     project rule (CLAUDE.md → AI_RULES.md) allows it on disabled states. */
  cursor: not-allowed;
  filter: grayscale(0.4) brightness(0.8);
}
</style>
