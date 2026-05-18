<template>
  <div class="steam-mp">
    <div class="steam-mp__layout">
      <header class="steam-mp__header">
        <UiButton size="sm" @click="router.push('/custom')">Back</UiButton>
        <h1 class="steam-mp__title">Steam Multiplayer</h1>
      </header>

      <UiPanel v-if="!player" :padding="20" class="steam-mp__signed-out">
        <div class="steam-mp__signed-out-msg">
          Steam isn't available right now. Make sure Steam is running and you're signed in,
          then relaunch the game. Direct Connect is always available as a fallback.
        </div>
        <UiButton size="sm" @click="router.push('/direct-connect')">Open Direct Connect</UiButton>
      </UiPanel>

      <template v-else>
        <UiPanel :padding="20" class="steam-mp__signed-in">
          <div class="steam-mp__signed-in-msg">
            Signed in as <strong>{{ player.personaName }}</strong>
            <span class="steam-mp__steam-id">(steam id {{ player.steamId64 }})</span>
          </div>
        </UiPanel>

        <!-- Host section -->
        <UiPanel :padding="24" class="steam-mp__section">
          <div class="steam-mp__section-title">Host with Steam friends</div>
          <div class="steam-mp__section-desc">
            Create a friends-only Steam lobby. Invited friends can join from the
            Steam overlay; up to 4 players total.
          </div>

          <div v-if="!hostLobbyId" class="steam-mp__row">
            <UiButton size="md" :disabled="hostBusy" @click="onCreate">
              {{ hostBusy ? 'Creating…' : 'Create lobby' }}
            </UiButton>
            <span v-if="hostError" class="steam-mp__error">{{ hostError }}</span>
          </div>

          <div v-else class="steam-mp__lobby-info">
            <div class="steam-mp__lobby-row">
              Lobby ID: <code>{{ hostLobbyId }}</code>
              <UiButton size="sm" @click="copyId(hostLobbyId)">
                {{ copiedId === hostLobbyId ? 'Copied!' : 'Copy' }}
              </UiButton>
            </div>
            <div class="steam-mp__lobby-row">
              <UiButton size="md" :disabled="inviteBusy" @click="onInvite">
                {{ inviteBusy ? 'Opening overlay…' : 'Invite friend (Steam overlay)' }}
              </UiButton>
            </div>
            <div v-if="inviteError" class="steam-mp__error">{{ inviteError }}</div>
            <div class="steam-mp__hint">
              Share the lobby ID with a friend so they can join by pasting it
              below — or use the Steam invite overlay above. Both paths work.
            </div>
          </div>
        </UiPanel>

        <!-- Joiner section -->
        <UiPanel :padding="24" class="steam-mp__section">
          <div class="steam-mp__section-title">Join a friend's lobby</div>
          <div class="steam-mp__section-desc">
            Paste a lobby ID a friend shared with you. (Cold-launch from a
            Steam invite is a future enhancement; for now use copy-paste.)
          </div>

          <div class="steam-mp__row">
            <input
              v-model="joinInput"
              type="text"
              placeholder="lobby SteamID64"
              class="steam-mp__input"
              :disabled="joinBusy"
              @keyup.enter="onJoin"
            />
            <UiButton size="md" :disabled="joinBusy || !joinInput" @click="onJoin">
              {{ joinBusy ? 'Joining…' : 'Join' }}
            </UiButton>
          </div>

          <div v-if="joinError" class="steam-mp__error">{{ joinError }}</div>
          <div v-if="joinedLobbyId" class="steam-mp__joined">
            ✓ Joined lobby <code>{{ joinedLobbyId }}</code>
          </div>
        </UiPanel>

        <UiPanel :padding="16" class="steam-mp__note-panel">
          <div class="steam-mp__note">
            <strong>Note:</strong> creating or joining a lobby coordinates
            players through Steam, but actual gameplay over Steam's
            networking is a separate piece (Step 4 — Steam Networking
            Sockets). Until that lands, the lobby is a coordination handle
            only; to play a match you'll still need to use Direct Connect.
          </div>
        </UiPanel>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import UiPanel from '@/components/ui/UiPanel.vue'
import UiButton from '@/components/ui/UiButton.vue'
import {
  getSteamPlayer,
  openLobby,
  joinLobby,
  inviteFriend,
  type LocalSteamPlayer,
} from '@/services/desktopBridge'

const router = useRouter()

const player = ref<LocalSteamPlayer | null>(null)

const hostBusy = ref(false)
const hostError = ref('')
const hostLobbyId = ref<string | null>(null)

const inviteBusy = ref(false)
const inviteError = ref('')

const joinInput = ref('')
const joinBusy = ref(false)
const joinError = ref('')
const joinedLobbyId = ref<string | null>(null)

const copiedId = ref<string | null>(null)

onMounted(async () => {
  try {
    player.value = await getSteamPlayer()
  } catch {
    player.value = null
  }
})

async function onCreate() {
  hostBusy.value = true
  hostError.value = ''
  try {
    const handle = await openLobby({ maxPlayers: 4 })
    if (handle) hostLobbyId.value = handle.lobbyId
  } catch (e) {
    hostError.value = describe(e)
  } finally {
    hostBusy.value = false
  }
}

async function onInvite() {
  if (!hostLobbyId.value) return
  inviteBusy.value = true
  inviteError.value = ''
  try {
    await inviteFriend(hostLobbyId.value)
  } catch (e) {
    inviteError.value = describe(e)
  } finally {
    inviteBusy.value = false
  }
}

async function onJoin() {
  const id = joinInput.value.trim()
  if (!id) return
  joinBusy.value = true
  joinError.value = ''
  joinedLobbyId.value = null
  try {
    const handle = await joinLobby(id)
    if (handle) joinedLobbyId.value = handle.lobbyId
  } catch (e) {
    joinError.value = describe(e)
  } finally {
    joinBusy.value = false
  }
}

async function copyId(id: string) {
  try {
    await navigator.clipboard.writeText(id)
    copiedId.value = id
    setTimeout(() => {
      if (copiedId.value === id) copiedId.value = null
    }, 1500)
  } catch {
    // clipboard may be denied; user can manually select+copy
  }
}

function describe(e: unknown): string {
  if (typeof e === 'string') return e
  if (e instanceof Error) return e.message
  return JSON.stringify(e)
}
</script>

<style scoped>
.steam-mp {
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

.steam-mp__layout {
  display: flex;
  flex-direction: column;
  gap: 20px;
  max-width: 800px;
  width: 100%;
}

.steam-mp__header {
  display: flex;
  align-items: center;
  gap: 20px;
}

.steam-mp__title {
  font-size: 24px;
  font-weight: 700;
  color: #f5ead2;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  margin: 0;
}

.steam-mp__section,
.steam-mp__signed-in,
.steam-mp__signed-out,
.steam-mp__note-panel {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.steam-mp__signed-in {
  border: 1px solid rgba(122, 195, 122, 0.4);
  background: rgba(46, 99, 49, 0.18) !important;
}

.steam-mp__signed-out {
  border: 1px solid rgba(245, 234, 210, 0.25);
}

.steam-mp__signed-in-msg,
.steam-mp__signed-out-msg {
  color: #f5ead2;
  font-size: 14px;
}

.steam-mp__steam-id {
  font-size: 11px;
  opacity: 0.55;
  font-family: monospace;
  margin-left: 8px;
}

.steam-mp__section-title {
  font-size: 16px;
  font-weight: 600;
  color: #f5ead2;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.steam-mp__section-desc,
.steam-mp__hint {
  font-size: 13px;
  color: rgba(245, 234, 210, 0.75);
  line-height: 1.5;
}

.steam-mp__hint {
  margin-top: 6px;
  font-style: italic;
}

.steam-mp__row {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.steam-mp__lobby-info {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.steam-mp__lobby-row {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.steam-mp__lobby-row code {
  background: rgba(0, 0, 0, 0.35);
  padding: 4px 10px;
  border-radius: 3px;
  font-size: 13px;
  color: #f5ead2;
  font-family: monospace;
}

.steam-mp__input {
  flex: 1;
  min-width: 220px;
  font-family: monospace;
  font-size: 13px;
  padding: 8px 12px;
  background: rgba(0, 0, 0, 0.35);
  color: #f5ead2;
  border: 1px solid rgba(245, 234, 210, 0.25);
  border-radius: 4px;
}

.steam-mp__error {
  font-size: 13px;
  color: #ff8888;
}

.steam-mp__joined {
  margin-top: 6px;
  color: #7ac37a;
  font-size: 14px;
}

.steam-mp__joined code {
  background: rgba(0, 0, 0, 0.35);
  padding: 2px 8px;
  border-radius: 3px;
  font-family: monospace;
}

.steam-mp__note-panel {
  border: 1px solid rgba(245, 234, 210, 0.15);
  background: rgba(0, 0, 0, 0.2) !important;
}

.steam-mp__note {
  font-size: 12px;
  color: rgba(245, 234, 210, 0.7);
  line-height: 1.5;
}
</style>
