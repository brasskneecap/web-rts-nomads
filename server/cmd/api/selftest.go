package main

// Pre-§14 smoke-test path for the Steam Networking Sockets transport bridge.
// Active only when NOMADS_SELFTEST is set (the Tauri shell sets it from the
// --steam-net-selftest argv flag). Two modes:
//
//   NOMADS_SELFTEST=host                       — call bridge.OpenListener(27)
//   NOMADS_SELFTEST=connect:<steamid64>        — call bridge.ConnectTo(<id>, 27)
//
// In either mode the default hub-registration peer handler is replaced with
// a ping-and-print one: each new peer transport gets a goroutine that sends
// "PING peer=N seq=K ts=…" every 2 s and logs everything it receives. This
// lets a two-machine smoke test verify the full IPC → steam_net → IPC path
// without the §14 UI being in place. Once §14 lands this file becomes dead
// code and can be deleted in the cleanup commit.

import (
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	"webrts/server/internal/steam"
	"webrts/server/internal/ws"
)

const selftestVirtualPort = 27

// installSteamNetSelftest installs the selftest peer handler on bridge and
// fires the matching IPC bootstrap call. Returns nil when the mode string
// is recognised and the bootstrap succeeded; an error otherwise. Errors
// from the bootstrap call are logged and propagated so the operator sees a
// loud failure in <ts>-server.log rather than a silent hang.
func installSteamNetSelftest(bridge *steam.IPCBridge, mode string) error {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return fmt.Errorf("empty NOMADS_SELFTEST value")
	}
	bridge.SetPeerHandler(func(peerID, steamID uint64, role steam.PeerRole) steam.PeerSink {
		log.Printf("[SELFTEST] peer transport up: peerID=%d steamID=%d role=%s",
			peerID, steamID, role)
		t := ws.NewSteamTransport(peerID, steamID, bridge)
		go selftestPingLoop(t, peerID)
		go selftestReadLoop(t, peerID)
		return t
	})

	switch {
	case mode == "host":
		log.Printf("[SELFTEST] host mode: opening listener on virtual port %d", selftestVirtualPort)
		if err := bridge.OpenListener(selftestVirtualPort); err != nil {
			return fmt.Errorf("OpenListener: %w", err)
		}
		log.Printf("[SELFTEST] listener open; waiting for joiner. Share your SteamID with them.")
		return nil

	case strings.HasPrefix(mode, "connect:"):
		raw := strings.TrimPrefix(mode, "connect:")
		steamID, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return fmt.Errorf("bad SteamID in %q: %w", mode, err)
		}
		log.Printf("[SELFTEST] joiner mode: connecting to steamID=%d on virtual port %d",
			steamID, selftestVirtualPort)
		peerID, err := bridge.ConnectTo(steamID, selftestVirtualPort)
		if err != nil {
			return fmt.Errorf("ConnectTo(%d): %w", steamID, err)
		}
		log.Printf("[SELFTEST] ConnectTo returned peerID=%d; awaiting new_peer_transport event", peerID)
		return nil

	default:
		return fmt.Errorf("unrecognised NOMADS_SELFTEST mode %q (want 'host' or 'connect:<steamid>')", mode)
	}
}

// selftestPingLoop fires a "PING …" message every 2s until the transport
// returns an error from WriteMessage (peer left, bridge closed, etc.).
func selftestPingLoop(t ws.Transport, peerID uint64) {
	seq := 0
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		seq++
		msg := fmt.Sprintf("PING peer=%d seq=%d ts=%d", peerID, seq, time.Now().Unix())
		if err := t.WriteMessage([]byte(msg)); err != nil {
			log.Printf("[SELFTEST] peer=%d write error after %d pings: %v", peerID, seq, err)
			return
		}
		log.Printf("[SELFTEST] → peer=%d sent: %s", peerID, msg)
	}
}

// selftestReadLoop logs every inbound message verbatim until the transport
// surfaces io.EOF (Disconnect from the bridge) or any other error.
func selftestReadLoop(t ws.Transport, peerID uint64) {
	for {
		data, err := t.ReadMessage()
		if err != nil {
			if err == io.EOF {
				log.Printf("[SELFTEST] peer=%d closed (EOF)", peerID)
			} else {
				log.Printf("[SELFTEST] peer=%d read error: %v", peerID, err)
			}
			return
		}
		log.Printf("[SELFTEST] ← peer=%d received: %s", peerID, data)
	}
}
