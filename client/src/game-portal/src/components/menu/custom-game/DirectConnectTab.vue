<template>
  <div class="cg-direct">
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
    <section class="cg-direct__section">
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
    </section>

    <!-- Joiner section -->
    <section class="cg-direct__section">
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
    </section>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import * as dc from '@/services/directConnect'

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
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 16);
  min-height: 0;
  color: #3a1f0a;
}

.cg-direct__section {
  display: flex;
  flex-direction: column;
  gap: calc(var(--s) * 10);
  border: 1px solid rgba(58, 31, 10, 0.25);
  border-radius: calc(var(--s) * 4);
  background: rgba(245, 234, 210, 0.4);
  padding: calc(var(--s) * 16);
}

.cg-direct__section-title {
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: calc(var(--s) * 16);
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.cg-direct__section-desc {
  font-size: calc(var(--s) * 13);
  color: rgba(58, 31, 10, 0.85);
  line-height: 1.5;
}

.cg-direct__section-desc code,
.cg-direct__ip-item code {
  background: rgba(58, 31, 10, 0.12);
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
  color: #3a1f0a;
}

.cg-direct__input {
  flex: 1;
  min-width: calc(var(--s) * 220);
  font-family: monospace;
  font-size: calc(var(--s) * 14);
  padding: calc(var(--s) * 8) calc(var(--s) * 12);
  background: rgba(255, 250, 236, 0.85);
  color: #3a1f0a;
  border: 1px solid rgba(58, 31, 10, 0.4);
  border-radius: calc(var(--s) * 4);
}

.cg-direct__busy {
  font-size: calc(var(--s) * 12);
  color: rgba(58, 31, 10, 0.65);
}

.cg-direct__error {
  font-size: calc(var(--s) * 13);
  color: #7a1a1a;
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
  color: rgba(58, 31, 10, 0.75);
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
  color: #2a1505;
}

.cg-direct__ip-hint {
  font-size: calc(var(--s) * 12);
  color: rgba(58, 31, 10, 0.55);
}

.cg-direct__ips-empty {
  font-size: calc(var(--s) * 13);
  color: #7a1a1a;
  font-style: italic;
}

.cg-direct__ips-note {
  font-size: calc(var(--s) * 11);
  color: rgba(58, 31, 10, 0.55);
  margin-top: calc(var(--s) * 4);
}

.cg-direct__active-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: calc(var(--s) * 16);
  border: 1px solid rgba(80, 120, 60, 0.55);
  background: rgba(180, 200, 140, 0.35);
  border-radius: calc(var(--s) * 4);
  padding: calc(var(--s) * 14) calc(var(--s) * 16);
}

.cg-direct__active-text {
  color: #2d4a16;
  font-size: calc(var(--s) * 14);
}

/* Shared parchment action button — mirrors the Campaign panel's action
   buttons so the whole Custom Game panel reads as one surface. */
.cg-action {
  font-family: 'Cinzel', 'Trajan Pro', 'Times New Roman', serif;
  font-size: calc(var(--s) * 13);
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  padding: calc(var(--s) * 8) calc(var(--s) * 18);
  border-radius: calc(var(--s) * 4);
  border: 1px solid rgba(58, 31, 10, 0.55);
  color: #2a1505;
  background: linear-gradient(180deg, #c0a98a 0%, #8a7350 100%);
}

.cg-action--start {
  background: linear-gradient(180deg, #d8b06a 0%, #a87a36 100%);
}

.cg-action--muted {
  background: linear-gradient(180deg, #c0a98a 0%, #8a7350 100%);
}

.cg-action--sm {
  font-size: calc(var(--s) * 11);
  padding: calc(var(--s) * 5) calc(var(--s) * 12);
}

.cg-action:disabled {
  background: rgba(180, 160, 110, 0.4);
  color: rgba(58, 31, 10, 0.45);
  /* `cursor: not-allowed` is the system semantic for "forbidden action" — the
     project rule (CLAUDE.md → AI_RULES.md) allows it on disabled states. */
  cursor: not-allowed;
}
</style>
