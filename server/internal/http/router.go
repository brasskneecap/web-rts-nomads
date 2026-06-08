// server/internal/http/router.go
package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sort"
	"strings"

	"webrts/server/internal/game"
	"webrts/server/internal/profile"
	"webrts/server/internal/transportbridge"
	"webrts/server/internal/ws"
)

// NewRouter wires the server's HTTP and WebSocket handlers. When spaHandler is
// non-nil, it is mounted at "/" as a catch-all so the embedded SPA is served
// for any path that no other route matches; routes registered above keep their
// existing precedence by virtue of http.ServeMux's longest-prefix matching.
// The no-tag build passes nil here and the server stays API-only.
func NewRouter(hub *ws.Hub, corsOrigin string, profileManager *profile.Manager, spaHandler http.Handler) http.Handler {
	mux := http.NewServeMux()

	registerProfileRoutes(mux, profileManager)

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
		})
	})

	mux.HandleFunc("/catalog/buildings", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"buildings": game.ListBuildingDefs(),
		})
	})

	mux.HandleFunc("/catalog/obstacles", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"obstacles": game.ListObstacleDefs(),
		})
	})

	mux.HandleFunc("/catalog/units", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"units":       game.ListUnitDefs(),
			"paths":       game.ListPathBounds(),
			"pathsByUnit": game.ListPathsByUnitType(),
		})
	})

	mux.HandleFunc("/catalog/perks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"perks": game.ListPerkDefs(),
		})
	})

	mux.HandleFunc("/catalog/items", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": game.ListItemDefs(),
		})
	})


	mux.HandleFunc("/catalog/action-icons", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"icons": game.ListActionIconDefs(),
		})
	})

	mux.HandleFunc("/maps", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var entry game.MapCatalogEntry
			if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
				http.Error(w, "invalid JSON body", http.StatusBadRequest)
				return
			}
			if entry.ID == "" {
				http.Error(w, "map id is required", http.StatusBadRequest)
				return
			}
			if err := game.SaveMapCatalogEntry(entry); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": entry.ID, "status": "saved"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(game.ListMapCatalogSummaries())
	})

	mux.HandleFunc("/maps/", func(w http.ResponseWriter, r *http.Request) {
		mapID := strings.TrimPrefix(r.URL.Path, "/maps/")
		if mapID == "" {
			http.NotFound(w, r)
			return
		}

		entry, ok := game.GetMapCatalogEntryByID(mapID)
		if !ok {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(entry)
	})

	mux.HandleFunc("/matches/", func(w http.ResponseWriter, r *http.Request) {
		// Expect exactly: /matches/{matchID}/status
		trimmed := strings.TrimPrefix(r.URL.Path, "/matches/")
		matchID, suffix, ok := strings.Cut(trimmed, "/")
		if !ok || matchID == "" || suffix != "status" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		playerID := r.URL.Query().Get("playerId")
		if playerID == "" {
			http.Error(w, "playerId required", http.StatusBadRequest)
			return
		}

		match, ok := hub.GetMatch(matchID)
		if !ok {
			http.NotFound(w, r)
			return
		}

		// Participant check: a player is a participant if they're already in
		// the match's state (reconnect path) OR if a lobby started this match
		// and the player is on that lobby's roster (first-join path — the WS
		// join_match handler is the sole entry point that adds the player to
		// the match state, so HasPlayer would be false before the WS opens).
		isParticipant := match.HasPlayer(playerID)
		if !isParticipant {
			if lobbyMgr := hub.GetLobbyManager(); lobbyMgr != nil {
				if l := lobbyMgr.FindByMatchID(matchID); l != nil {
					for _, pid := range l.Players {
						if pid == playerID {
							isParticipant = true
							break
						}
					}
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"matchId":       match.ID,
			"mapId":         match.MapID,
			"isParticipant": isParticipant,
		})
	})

	registerAdvancementRoutes(mux, profileManager, hub.GetMatchManager())

	lm := hub.GetLobbyManager()
	mm := hub.GetMatchManager()

	mux.HandleFunc("/lobbies", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"lobbies": lm.List()})

		case http.MethodPost:
			var body struct {
				MapID        string `json:"mapId"`
				HostPlayerID string `json:"hostPlayerId"`
				// CampaignLevelID is optional. When set, the lobby is
				// campaign-launched and the engine installs the level's
				// objectives at match-start. Custom Game lobbies omit it.
				CampaignLevelID string `json:"campaignLevelId,omitempty"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid JSON body", http.StatusBadRequest)
				return
			}
			if body.MapID == "" || body.HostPlayerID == "" {
				http.Error(w, "mapId and hostPlayerId are required", http.StatusBadRequest)
				return
			}
			lobby, err := lm.Create(body.MapID, body.HostPlayerID, body.CampaignLevelID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"lobby": lobby})

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/lobbies/", func(w http.ResponseWriter, r *http.Request) {
		trimmed := strings.TrimPrefix(r.URL.Path, "/lobbies/")
		lobbyID, action, hasAction := strings.Cut(trimmed, "/")
		if lobbyID == "" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		if !hasAction {
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			lobby, ok := lm.Get(lobbyID)
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"lobby": lobby})
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		switch action {
		case "join":
			var body struct {
				PlayerID string `json:"playerId"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid JSON body", http.StatusBadRequest)
				return
			}
			if body.PlayerID == "" {
				http.Error(w, "playerId is required", http.StatusBadRequest)
				return
			}
			lobby, err := lm.Join(lobbyID, body.PlayerID)
			if err != nil {
				lobbyHTTPError(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"lobby": lobby})

		case "leave":
			var body struct {
				PlayerID string `json:"playerId"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid JSON body", http.StatusBadRequest)
				return
			}
			if body.PlayerID == "" {
				http.Error(w, "playerId is required", http.StatusBadRequest)
				return
			}
			lobby, err := lm.Leave(lobbyID, body.PlayerID)
			if err != nil {
				lobbyHTTPError(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"lobby": lobby})

		case "start":
			var body struct {
				PlayerID string `json:"playerId"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid JSON body", http.StatusBadRequest)
				return
			}
			if body.PlayerID == "" {
				http.Error(w, "playerId is required", http.StatusBadRequest)
				return
			}
			lobby, err := lm.Start(lobbyID, body.PlayerID, mm)
			if err != nil {
				lobbyHTTPError(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"lobby": lobby})

		default:
			http.Error(w, "unknown action", http.StatusNotFound)
		}
	})

	mux.HandleFunc("/ws", hub.HandleWS)

	// §13 task 13.1: Direct connect toggle. POST {allow:bool} to flip;
	// GET to read. Lives under /api/ to keep top-level uncluttered.
	mux.HandleFunc("/api/direct-connect", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"allow": hub.AllowNonLoopback()})
		case http.MethodPost:
			var body struct {
				Allow bool `json:"allow"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid JSON body", http.StatusBadRequest)
				return
			}
			hub.SetAllowNonLoopback(body.Allow)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"allow": hub.AllowNonLoopback()})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// §13 task 13.3 + 13.9: list the host's reachable non-loopback IPv4
	// addresses with the documented sort order (Tailscale CGNAT first,
	// RFC1918 private next, everything else last). SPA host UI calls this
	// when the user toggles "Allow LAN/Internet connections" on.
	mux.HandleFunc("/api/direct-connect/ips", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ips := enumerateReachableIPv4s()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ips": ips})
	})

	// §11.5 + §13.4: joiner-as-proxy join. SPA posts the host's address;
	// server dials the host's WS (5s timeout via transportbridge), stashes
	// the conn under a token, returns the token. SPA then reconnects its
	// own WS as ?proxy=<token> and the hub wires the two bytes-for-bytes.
	mux.HandleFunc("/api/direct-connect/join", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			HostPort string `json:"hostPort"`
			UseTLS   bool   `json:"useTls,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
			return
		}
		if body.HostPort == "" {
			http.Error(w, `{"error":"hostPort is required"}`, http.StatusBadRequest)
			return
		}
		hostConn, err := transportbridge.ConnectToHost(context.Background(), body.HostPort, body.UseTLS)
		if err != nil {
			// Surface the DialError kind so the SPA can pick a sensible UI string.
			kind := transportbridge.DialErrOther
			var de *transportbridge.DialError
			if errors.As(err, &de) {
				kind = de.Kind
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": err.Error(),
				"kind":  string(kind),
			})
			return
		}
		token := hub.DirectSessions().Put(hostConn.Conn)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":  token,
			"hostPort": body.HostPort,
		})
	})

	if spaHandler != nil {
		mux.Handle("/", spaHandler)
	}

	return withCORS(mux, corsOrigin)
}

func lobbyHTTPError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, game.ErrLobbyNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, game.ErrNotHost):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, game.ErrLobbyAlreadyStarted), errors.Is(err, game.ErrLobbyFull), errors.Is(err, game.ErrLobbyClosed):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// enumerateReachableIPv4s returns the host's non-loopback IPv4 addresses in
// the order §13 task 13.9 documents:
//   1. Tailscale CGNAT (100.64.0.0/10) — listed first because Tailscale is
//      the lowest-friction reachability path for ad-hoc playtests.
//   2. RFC1918 private (10/8, 172.16/12, 192.168/16) — typical LAN.
//   3. Everything else — public IPs, link-local, etc.
//
// Within each bucket, order is whatever the OS reports; we don't second-guess
// the interface order. Errors at the interface enumeration level fall through
// to an empty list (the SPA's UI shows "no addresses found").
func enumerateReachableIPv4s() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	type bucket int
	const (
		bucketTailscale bucket = iota
		bucketRFC1918
		bucketOther
	)
	type entry struct {
		ip     string
		bucket bucket
	}
	var entries []entry
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		v4 := ipNet.IP.To4()
		if v4 == nil || v4.IsLoopback() || v4.IsUnspecified() || v4.IsLinkLocalUnicast() {
			continue
		}
		switch {
		case isTailscaleCGNAT(v4):
			entries = append(entries, entry{v4.String(), bucketTailscale})
		case v4.IsPrivate():
			entries = append(entries, entry{v4.String(), bucketRFC1918})
		default:
			entries = append(entries, entry{v4.String(), bucketOther})
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].bucket < entries[j].bucket
	})
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.ip)
	}
	return out
}

// isTailscaleCGNAT returns true for addresses inside the CGNAT range
// (100.64.0.0/10) that Tailscale uses. CGNAT is technically a shared address
// range, but in practice the only consumer who routes traffic through it on
// a typical dev machine is Tailscale itself, so it's a reliable signal.
func isTailscaleCGNAT(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		return v4[0] == 100 && (v4[1] >= 64 && v4[1] <= 127)
	}
	return false
}

func withCORS(next http.Handler, allowedOrigin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Player-ID")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
