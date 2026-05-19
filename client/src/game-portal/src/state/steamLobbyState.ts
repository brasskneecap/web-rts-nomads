// Single-slot reactive coordination state for the Steam-paired-lobby flow.
//
// Problem this solves: CreateGame.vue navigates to /lobby/:id as soon as
// the LOCAL lobby is created (optimistic, ~50ms), but the paired Steam
// Matchmaking lobby creation takes another 1–2s (LobbyCreated_t callback
// latency). Without coordination, Lobby.vue mounts before the Steam lobby
// id exists, reads an empty sessionStorage on mount, and never re-checks —
// the Invite Friend button stays hidden forever.
//
// This module exposes a single reactive ref. CreateGame seeds it with the
// localLobbyId as "pending" before navigating, then writes the result
// (steamLobbyId or null) when the background openLobby resolves. Lobby.vue
// computes off it for live updates AND falls back to sessionStorage on
// cold mount / reload.

import { ref } from 'vue'

/** Status of the host-side Steam lobby creation tied to a specific local
 *  lobby. Single-slot — a player creates one lobby at a time. Cleared by
 *  Lobby.vue when leaving (so a subsequent local-only lobby creation
 *  doesn't see a stale Steam pairing). */
export interface SteamLobbyPairing {
  localLobbyId: string
  /** null while pending, then either the Steam lobby id (success) or
   *  null permanently (Steam unavailable / openLobby failed). */
  steamLobbyId: string | null
  /** true between "create-lobby clicked" and "openLobby callback fired". */
  pending: boolean
}

/** The pairing for the currently-active local lobby. Set by CreateGame
 *  immediately on click (with pending=true), updated when the background
 *  openLobby resolves, cleared on lobby-leave. */
export const steamLobbyPairing = ref<SteamLobbyPairing | null>(null)

/** Seed an entry as pending. Called by CreateGame after the LOCAL lobby
 *  is created but before openLobby resolves. */
export function beginSteamLobbyPairing(localLobbyId: string): void {
  steamLobbyPairing.value = { localLobbyId, steamLobbyId: null, pending: true }
}

/** Record the outcome of the background openLobby. steamLobbyId=null
 *  means Steam was unavailable or the call failed (logged; non-fatal). */
export function completeSteamLobbyPairing(
  localLobbyId: string,
  steamLobbyId: string | null,
): void {
  // Guard against a stale background completion if the user has already
  // moved on to a different lobby — don't clobber the newer pairing.
  const current = steamLobbyPairing.value
  if (current && current.localLobbyId !== localLobbyId) {
    return
  }
  steamLobbyPairing.value = { localLobbyId, steamLobbyId, pending: false }
}

/** Drop the pairing. Called by Lobby.vue on leaveAndGoBack so a fresh
 *  /create-game → /lobby cycle starts with no stale state. */
export function clearSteamLobbyPairing(): void {
  steamLobbyPairing.value = null
}
