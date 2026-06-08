// Shared helper that creates a local lobby and kicks off the paired Steam
// matchmaking lobby in the background. Used by both CustomGame's "Create
// Lobby" flow and Campaign's "Play level" flow so they stay in sync:
// whoever calls this gets a local lobby id immediately, can navigate to
// /lobby/:id, and the Steam invite button lights up reactively when the
// background openLobby resolves.
//
// The Steam lobby creation is intentionally NOT awaited — LobbyCreated_t
// latency is 1–2s and blocking nav that long makes the button feel broken.
// See CreateGame.vue's history comments for the full justification.

import type { Lobby } from '@/game/network/protocol'
import { useLobbies } from '@/composables/useLobbies'
import { getSteamPlayer, openLobby } from '@/services/desktopBridge'
import { STEAM_LOBBY_ID_KEY } from '@/game/network/NetworkClient'
import {
  beginSteamLobbyPairing,
  completeSteamLobbyPairing,
} from '@/state/steamLobbyState'

export interface CreateMultiplayerLobbyArgs {
  mapId: string
  hostPlayerId: string
  /** Optional campaign-level identifier. When set, the server installs the
   *  authored objectives on the GameState at match start. The campaign flow
   *  (useCampaign.startCampaignLevel / openCampaignLobby) reads this from
   *  the active campaignSession; Custom Game omits it. */
  campaignLevelId?: string
}

/** Create a local lobby and start the paired Steam lobby in the background.
 *  Returns the local Lobby immediately; the Steam pairing resolves reactively
 *  via `steamLobbyPairing` (see Lobby.vue's Invite Friend button). */
export async function createMultiplayerLobby(
  args: CreateMultiplayerLobbyArgs,
): Promise<Lobby> {
  const { createLobby } = useLobbies()

  // Step 1: create the local lobby. Fast (~50ms HTTP POST).
  const created = await createLobby({
    mapId: args.mapId,
    hostPlayerId: args.hostPlayerId,
    campaignLevelId: args.campaignLevelId,
  })

  // Step 2: seed pairing state as "pending" and kick off the Steam lobby
  // create in the background. The caller is free to navigate to /lobby/:id
  // immediately — Lobby.vue computes off `steamLobbyPairing` and updates
  // the Invite button when the background promise resolves.
  beginSteamLobbyPairing(created.id)
  try {
    sessionStorage.removeItem(STEAM_LOBBY_ID_KEY)
  } catch {
    /* sessionStorage may be sandboxed; non-fatal */
  }
  void runBackgroundSteamLobbyCreate(created.id, args.mapId)

  return created
}

/** Runs openLobby off the click-handler critical path. On success writes
 *  both sessionStorage (so a /lobby reload still finds the Steam lobby id)
 *  and the reactive pairing state (so Lobby.vue's Invite button becomes
 *  live without needing a remount). On failure logs and clears the pending
 *  state so the UI stops showing "Setting up Steam invite…". */
async function runBackgroundSteamLobbyCreate(
  localLobbyId: string,
  mapId: string,
): Promise<void> {
  try {
    const steamPlayer = await getSteamPlayer()
    if (!steamPlayer) {
      // Steam unavailable (browser dev loop or packaged build without Steam
      // running). Invite button never appears; lobby still works locally.
      completeSteamLobbyPairing(localLobbyId, null)
      return
    }
    const handle = await openLobby({
      maxPlayers: 4,
      mapId,
      localLobbyId,
      hostPersona: steamPlayer.personaName,
    })
    const steamLobbyId = handle?.lobbyId ?? null
    if (steamLobbyId) {
      try {
        sessionStorage.setItem(STEAM_LOBBY_ID_KEY, steamLobbyId)
      } catch {
        /* sessionStorage may be sandboxed; non-fatal */
      }
    }
    completeSteamLobbyPairing(localLobbyId, steamLobbyId)
  } catch (err) {
    console.error('[createMultiplayerLobby] background openLobby failed:', err)
    completeSteamLobbyPairing(localLobbyId, null)
  }
}
