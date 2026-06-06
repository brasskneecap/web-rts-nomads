<template>
  <div class="direct-connect">
    <div class="direct-connect__layout">
      <header class="direct-connect__header">
        <ExitButton @click="router.push('/custom')" />
        <h1 class="direct-connect__title">Direct Connect</h1>
      </header>

      <!-- Active-proxy banner -->
      <UiPanel v-if="activeHost" :padding="20" class="direct-connect__active-banner">
        <div class="direct-connect__active-text">
          Currently connected to <strong>{{ activeHost }}</strong> via Direct Connect.
        </div>
        <UiButton size="sm" @click="disconnect">Disconnect &amp; Reload</UiButton>
      </UiPanel>

      <!-- Host section -->
      <UiPanel :padding="24" class="direct-connect__section">
        <div class="direct-connect__section-title">Host: Allow LAN / Internet</div>
        <div class="direct-connect__section-desc">
          Let other players on your network (or with a routable address to your
          machine) connect to a game you start.
          <strong>Anyone with this address can join &mdash; share like a Discord link.</strong>
          NAT traversal is not attempted; you are responsible for reachability.
        </div>

        <div class="direct-connect__row">
          <label class="direct-connect__toggle">
            <input type="checkbox" :checked="hostAllow" @change="onToggleAllow" />
            <span>Allow LAN/Internet connections</span>
          </label>
          <span v-if="toggleBusy" class="direct-connect__busy">Saving&hellip;</span>
          <span v-if="toggleError" class="direct-connect__error">{{ toggleError }}</span>
        </div>

        <div v-if="hostAllow" class="direct-connect__ips">
          <div class="direct-connect__ips-label">Reachable addresses on this machine:</div>
          <ul v-if="ips.length > 0" class="direct-connect__ips-list">
            <li v-for="(ip, idx) in ips" :key="ip" class="direct-connect__ip-item">
              <code>{{ ip }}:{{ port }}</code>
              <UiButton size="sm" @click="copyAddress(ip)">
                {{ copiedAddress === ip ? 'Copied!' : 'Copy' }}
              </UiButton>
              <span v-if="idx === 0" class="direct-connect__ip-hint">(suggested)</span>
            </li>
          </ul>
          <div v-else class="direct-connect__ips-empty">
            No non-loopback addresses detected. Connect to a network first or
            install Tailscale.
          </div>
          <div class="direct-connect__ips-note">
            Disabling the toggle does not end this session &mdash; close the
            window to do that. Existing connections stay live.
          </div>
        </div>
      </UiPanel>

      <!-- Joiner section -->
      <UiPanel :padding="24" class="direct-connect__section">
        <div class="direct-connect__section-title">Join a remote host</div>
        <div class="direct-connect__section-desc">
          Paste the address the host shared with you. The format is
          <code>host:port</code> &mdash; for example
          <code>192.168.1.50:8080</code> or <code>100.64.0.5:8080</code>.
        </div>

        <div class="direct-connect__row">
          <input
            v-model="hostInput"
            type="text"
            placeholder="host:port"
            class="direct-connect__input"
            :disabled="joining"
            @keyup.enter="onJoin"
          />
          <UiButton size="md" :disabled="joining || !hostInput" @click="onJoin">
            {{ joining ? 'Connecting…' : 'Connect' }}
          </UiButton>
        </div>

        <div v-if="joinError" class="direct-connect__error joiner-error">
          {{ joinError }}
        </div>
      </UiPanel>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import UiPanel from '@/components/ui/UiPanel.vue'
import UiButton from '@/components/ui/UiButton.vue'
import ExitButton from '@/components/ui/ExitButton.vue'
import * as dc from '@/services/directConnect'

const router = useRouter()

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
    // command through every composable that holds a WS reference.
    window.location.assign(window.location.pathname + '#/custom')
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
.direct-connect {
  position: relative;
  z-index: 1;
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: flex-start;
  background: radial-gradient(circle at top, rgba(36, 55, 87, 0.35), transparent 48%);
  padding: 32px;
  box-sizing: border-box;
  overflow-y: auto;
}

.direct-connect__layout {
  display: flex;
  flex-direction: column;
  gap: 20px;
  max-width: 800px;
  width: 100%;
}

.direct-connect__header {
  display: flex;
  align-items: center;
  gap: 20px;
}

.direct-connect__title {
  font-size: 24px;
  font-weight: 700;
  color: #f5ead2;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  margin: 0;
}

.direct-connect__section {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.direct-connect__section-title {
  font-size: 16px;
  font-weight: 600;
  color: #f5ead2;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.direct-connect__section-desc {
  font-size: 13px;
  color: rgba(245, 234, 210, 0.75);
  line-height: 1.5;
}

.direct-connect__section-desc code {
  background: rgba(0, 0, 0, 0.35);
  padding: 1px 6px;
  border-radius: 3px;
}

.direct-connect__row {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.direct-connect__toggle {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  color: #f5ead2;
}

.direct-connect__input {
  flex: 1;
  min-width: 220px;
  font-family: monospace;
  font-size: 14px;
  padding: 8px 12px;
  background: rgba(0, 0, 0, 0.35);
  color: #f5ead2;
  border: 1px solid rgba(245, 234, 210, 0.25);
  border-radius: 4px;
}

.direct-connect__busy {
  font-size: 12px;
  color: rgba(245, 234, 210, 0.65);
}

.direct-connect__error {
  font-size: 13px;
  color: #ff8888;
}

.direct-connect__error.joiner-error {
  margin-top: 6px;
}

.direct-connect__ips {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 10px;
}

.direct-connect__ips-label {
  font-size: 13px;
  color: rgba(245, 234, 210, 0.75);
}

.direct-connect__ips-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.direct-connect__ip-item {
  display: flex;
  align-items: center;
  gap: 10px;
}

.direct-connect__ip-item code {
  background: rgba(0, 0, 0, 0.35);
  padding: 4px 10px;
  border-radius: 3px;
  font-size: 14px;
  color: #f5ead2;
}

.direct-connect__ip-hint {
  font-size: 12px;
  color: rgba(245, 234, 210, 0.55);
}

.direct-connect__ips-empty {
  font-size: 13px;
  color: #ff8888;
  font-style: italic;
}

.direct-connect__ips-note {
  font-size: 11px;
  color: rgba(245, 234, 210, 0.55);
  margin-top: 4px;
}

.direct-connect__active-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  border: 1px solid rgba(122, 195, 122, 0.4);
  background: rgba(46, 99, 49, 0.18) !important;
}

.direct-connect__active-text {
  color: #f5ead2;
  font-size: 14px;
}
</style>
