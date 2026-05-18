package main

// Steam-lobby event handlers (§14.2 / §14.3). When the Rust shell pushes
// `lobby_hosted` or `lobby_joined` notifications, this wiring installs the
// correct peer handler and fires the matching IPC bootstrap call against
// the shell's steam_net worker.
//
// Two modes:
//
//   lobby_hosted — this server is the Steam-host. Install a peer handler
//                  that registers each incoming Steam peer with the WS
//                  hub (so joiners become regular hub clients). Fire
//                  OpenListener so the Rust shell starts accepting
//                  Networking-Sockets connections on the agreed virtual
//                  port (§12 NOMADS_VIRTUAL_PORT = 27).
//
//   lobby_joined — this server is the Steam-joiner. Install a peer handler
//                  that parks each new Steam transport in the Hub's
//                  SteamSessionStore (NOT in the hub directly). The
//                  joiner's SPA will pick it up by reconnecting with
//                  `?proxy=steam` (see ws.Hub.HandleWS). Then fire
//                  ConnectTo(hostSteamId, NOMADS_VIRTUAL_PORT).
//
// LobbyManager mirroring (§14.2 second clause) is deferred — the SPA's
// Steam Multiplayer view doesn't use the lobby browser, so no LobbyManager
// entry is required for the play-through. When §14.5/§14.6 lands the
// mirroring will hook in here.

import (
	"encoding/json"
	"log"
	"strconv"

	"webrts/server/internal/steam"
	"webrts/server/internal/transportbridge"
	"webrts/server/internal/ws"
)

// steamVirtualPort matches steam_net.rs NOMADS_VIRTUAL_PORT. Both sides
// must agree on this constant. Hard-coded here (not env-driven) because
// it's part of the wire contract, not a deployment knob.
const steamVirtualPort = 27

// wireSteamLobbyHandlers registers IPC event handlers for the two Steam
// lobby state-change notifications. Call once at server startup; handlers
// stay installed for the bridge's lifetime.
func wireSteamLobbyHandlers(bridge *steam.IPCBridge, hub *ws.Hub) {
	// Event handlers run on the IPCBridge's reader goroutine and MUST NOT
	// block (any synchronous bridge.call inside would deadlock waiting for
	// a response only the same reader can deliver). We do the param parse
	// + handler install inline, then fire the actual IPC bootstrap call
	// from a fresh goroutine.

	bridge.OnEvent("lobby_hosted", func(params json.RawMessage) {
		var p struct {
			LobbyID       string `json:"lobbyId"`
			HostSteamID64 string `json:"hostSteamId64"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			log.Printf("steam: lobby_hosted: bad params: %v", err)
			return
		}
		log.Printf("steam: hosting lobby=%s as steamID=%s", p.LobbyID, p.HostSteamID64)
		installHostPeerHandler(bridge, hub)
		go func() {
			if err := bridge.OpenListener(steamVirtualPort); err != nil {
				log.Printf("steam: OpenListener(%d) failed: %v", steamVirtualPort, err)
			}
		}()
	})

	bridge.OnEvent("lobby_joined", func(params json.RawMessage) {
		var p struct {
			LobbyID       string `json:"lobbyId"`
			HostSteamID64 string `json:"hostSteamId64"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			log.Printf("steam: lobby_joined: bad params: %v", err)
			return
		}
		hostID, err := strconv.ParseUint(p.HostSteamID64, 10, 64)
		if err != nil {
			log.Printf("steam: lobby_joined: bad hostSteamId64 %q: %v", p.HostSteamID64, err)
			return
		}
		log.Printf("steam: joining lobby=%s host=%d", p.LobbyID, hostID)
		installJoinerPeerHandler(bridge, hub.SteamSessions())
		go func() {
			peerID, err := bridge.ConnectTo(hostID, steamVirtualPort)
			if err != nil {
				log.Printf("steam: ConnectTo(%d) failed: %v", hostID, err)
				return
			}
			log.Printf("steam: ConnectTo returned peerID=%d; waiting for new_peer_transport", peerID)
		}()
	})
}

// installHostPeerHandler: every new Steam peer becomes a regular hub
// client. Identical behaviour to the original default wiring in main.go
// before §14 — peers route through the standard readLoop and game-protocol
// stack.
func installHostPeerHandler(bridge *steam.IPCBridge, hub *ws.Hub) {
	bridge.SetPeerHandler(func(peerID, steamID uint64, role steam.PeerRole) steam.PeerSink {
		log.Printf("steam: new peer transport peerID=%d steamID=%d role=%s",
			peerID, steamID, role)
		t := ws.NewSteamTransport(peerID, steamID, bridge)
		hub.RegisterTransport(t)
		return t
	})
}

// installJoinerPeerHandler: the new Steam transport is parked in the
// SteamSessionStore for the joiner's local SPA to claim by reconnecting
// with `?proxy=steam`. Importantly the transport is NOT registered with
// the local hub — the joiner-as-proxy model means the joiner's server
// does no simulation; it just forwards bytes to the host through this
// transport.
func installJoinerPeerHandler(bridge *steam.IPCBridge, store *transportbridge.SteamSessionStore) {
	bridge.SetPeerHandler(func(peerID, steamID uint64, role steam.PeerRole) steam.PeerSink {
		log.Printf("steam: parked upstream peer transport peerID=%d steamID=%d role=%s",
			peerID, steamID, role)
		t := ws.NewSteamTransport(peerID, steamID, bridge)
		store.Set(t)
		return t
	})
}
