package ws

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"webrts/server/internal/game"
	"webrts/server/internal/transportbridge"
	"webrts/server/pkg/protocol"

	"github.com/gorilla/websocket"
)

const (
	heartbeatInterval = 30 * time.Second
	heartbeatTimeout  = 75 * time.Second

	// originRejectLogThrottle keeps the rejection log from flooding when a
	// misbehaving caller hammers the WS endpoint with a bad Origin. One line
	// per origin per minute is plenty for diagnostics.
	originRejectLogThrottle = time.Minute
)

type Hub struct {
	upgrader     websocket.Upgrader
	manager      *game.MatchManager
	lobbyManager *game.LobbyManager
	quit         chan struct{}

	// allowNonLoopback gates whether non-loopback peers can complete the WS
	// upgrade. Toggled at runtime by the SPA via SetAllowNonLoopback (§13
	// task 13.1). The listener itself is bound to all interfaces — this
	// accept-time gate (design D11) is the only mechanism. Default off.
	allowNonLoopback atomic.Bool

	// version is the server's compiled version. Used by the hello-message
	// version-mismatch check (§17 task 17.1). Set by main.go from the same
	// `var version` that populates NOMADS_READY.
	version string

	// directSessions stores host-side WS connections dialled by
	// /api/direct-connect/join, keyed by token, until the joiner's SPA
	// reconnects with ?proxy=<token> (§11.5 + §13.4 joiner-as-proxy model).
	directSessions *transportbridge.SessionStore

	// steamSessions parks the joiner's upstream Steam Sockets transport
	// awaiting an SPA reconnect with ?proxy=steam (§14.3). Single-slot —
	// a Steam joiner has at most one upstream peer at a time.
	steamSessions *transportbridge.SteamSessionStore

	originRejectMu      sync.Mutex
	originRejectLastLog map[string]time.Time
}

// CloseCodeVersionMismatch is the WebSocket close code used when the SPA's
// hello version doesn't match the server's compiled version. The SPA's onclose
// handler maps this to the "Build mismatch — please restart" modal.
// 4000-4999 is the application-defined range per RFC 6455.
const CloseCodeVersionMismatch = 4000

func NewHub(manager *game.MatchManager, lobbyManager *game.LobbyManager) *Hub {
	h := &Hub{
		manager:             manager,
		lobbyManager:        lobbyManager,
		quit:                make(chan struct{}),
		originRejectLastLog: make(map[string]time.Time),
		directSessions:      transportbridge.NewSessionStore(),
		steamSessions:       transportbridge.NewSteamSessionStore(),
	}
	h.upgrader = websocket.Upgrader{
		CheckOrigin: h.checkOrigin,
	}

	go h.heartbeatLoop()
	go h.reapStaleSessionsLoop()

	return h
}

// DirectSessions exposes the session store so the HTTP router's
// /api/direct-connect/join handler can register dialled host conns.
// Returns the same instance for the Hub's lifetime.
func (h *Hub) DirectSessions() *transportbridge.SessionStore { return h.directSessions }

// SteamSessions exposes the single-slot Steam-upstream parking lot so
// main.go's lobby_joined handler can Set the steamTransport before the
// joiner SPA reconnects with `?proxy=steam`. Returns the same instance
// for the Hub's lifetime.
func (h *Hub) SteamSessions() *transportbridge.SteamSessionStore { return h.steamSessions }

func (h *Hub) reapStaleSessionsLoop() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-h.quit:
			return
		case t := <-ticker.C:
			if n := h.directSessions.ReapStale(t); n > 0 {
				log.Printf("transportbridge: reaped %d stale direct-connect session(s)", n)
			}
			if h.steamSessions.ReapStale(t) {
				log.Printf("transportbridge: reaped stale steam-upstream session")
			}
		}
	}
}

// SetAllowNonLoopback toggles the runtime gate that lets non-loopback peers
// complete the WS upgrade. Toggling off does NOT disconnect existing peers
// (per §13 task 13.10); it only affects subsequent accept-time decisions.
func (h *Hub) SetAllowNonLoopback(allow bool) {
	h.allowNonLoopback.Store(allow)
}

// SetVersion records the server's compiled version for the §17.1 hello
// handshake. Empty version disables the check (used in tests).
func (h *Hub) SetVersion(v string) { h.version = v }

// Version returns the recorded server version.
func (h *Hub) Version() string { return h.version }

// AllowNonLoopback reports the current toggle state. Used by the SPA's
// lobby UI to drive the toggle's checked state on render.
func (h *Hub) AllowNonLoopback() bool { return h.allowNonLoopback.Load() }

// checkOrigin implements the §13 task 13.11 Origin policy. Replaces the
// previous always-true CheckOrigin. Accepts:
//   - No Origin header (non-browser clients incl. transportbridge)
//   - Origin host is a loopback host (127.0.0.1, localhost, [::1] — any port)
// Rejects everything else with a single rate-limited log line per bad host.
// The check is UNCONDITIONAL (does not consult AllowNonLoopback) because
// the rule is correct in both toggle states and keeps the upgrade-time guard
// simple. Non-loopback peer admission lives in checkRemoteAddrAllowed.
func (h *Hub) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		h.logOriginReject(origin, "malformed Origin")
		return false
	}
	host := u.Hostname()
	if isLoopbackHost(host) {
		return true
	}
	h.logOriginReject(origin, "non-loopback Origin")
	return false
}

func isLoopbackHost(host string) bool {
	switch strings.ToLower(host) {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

func (h *Hub) logOriginReject(origin, reason string) {
	h.originRejectMu.Lock()
	defer h.originRejectMu.Unlock()
	now := time.Now()
	if last, ok := h.originRejectLastLog[origin]; ok && now.Sub(last) < originRejectLogThrottle {
		return
	}
	h.originRejectLastLog[origin] = now
	log.Printf("ws: %s — rejected (origin=%q)", reason, origin)
}

// checkRemoteAddrAllowed enforces the AllowNonLoopback gate at WS upgrade
// time (§13 task 13.1, design D11). Loopback peers always pass; non-loopback
// peers pass only when the toggle is on. Returns true if upgrade should
// proceed; false (with a logged reason) if it should be rejected.
func (h *Hub) checkRemoteAddrAllowed(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	if h.allowNonLoopback.Load() {
		return true
	}
	log.Printf("ws: rejecting non-loopback peer %s (AllowNonLoopback=false)", r.RemoteAddr)
	return false
}

// Close signals the heartbeat goroutine to stop and drops any parked
// proxy sessions. Call during graceful shutdown.
func (h *Hub) Close() {
	close(h.quit)
	h.steamSessions.Close()
}

func (h *Hub) GetMatch(matchID string) (*game.Match, bool) {
	return h.manager.GetMatch(matchID)
}

func (h *Hub) GetLobbyManager() *game.LobbyManager {
	return h.lobbyManager
}

func (h *Hub) GetMatchManager() *game.MatchManager {
	return h.manager
}

func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	if !h.checkRemoteAddrAllowed(r) {
		http.Error(w, "non-loopback connections are not allowed; host must enable LAN/Internet access", http.StatusForbidden)
		return
	}

	// §11.5 / §13.4 / §14.3: proxy-mode SPAs include ?proxy=<token>.
	//   token == "steam"   — Steam Sockets joiner-as-proxy (§14.3); pull the
	//                        parked upstream from steamSessions
	//   token == "<hex>"   — Direct-connect joiner-as-proxy (§11.5); pull from
	//                        directSessions by the hex token returned from
	//                        /api/direct-connect/join
	// Look up BEFORE the upgrade so we can return 502 with a JSON error body
	// if the session is missing/expired.
	if token := r.URL.Query().Get("proxy"); token != "" {
		if token == "steam" {
			upstream, ok := h.steamSessions.Take()
			if !ok {
				http.Error(w, `{"error":"no steam upstream parked"}`, http.StatusBadGateway)
				return
			}
			spaConn, err := h.upgrader.Upgrade(w, r, nil)
			if err != nil {
				_ = upstream.Close()
				log.Println("steam proxy upgrade error:", err)
				return
			}
			log.Printf("steam-sockets proxy active: spa=%s -> steam upstream",
				spaConn.RemoteAddr())
			transportbridge.ProxyStreams(transportbridge.NewWSConnStream(spaConn), upstream)
			log.Printf("steam-sockets proxy ended: spa=%s", spaConn.RemoteAddr())
			return
		}
		hostConn, ok := h.directSessions.Take(token)
		if !ok {
			http.Error(w, `{"error":"unknown or expired proxy token"}`, http.StatusBadGateway)
			return
		}
		spaConn, err := h.upgrader.Upgrade(w, r, nil)
		if err != nil {
			// We already took the host conn; clean it up so it doesn't leak.
			_ = hostConn.Close()
			log.Println("proxy upgrade error:", err)
			return
		}
		log.Printf("direct-connect proxy active: spa=%s -> host (token=%s…)",
			spaConn.RemoteAddr(), token[:8])
		transportbridge.Proxy(spaConn, hostConn)
		log.Printf("direct-connect proxy ended: spa=%s", spaConn.RemoteAddr())
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("upgrade error:", err)
		return
	}

	transport := newWebSocketTransport(conn, conn.RemoteAddr().String())
	client := NewClient(transport)
	log.Printf("peer connected: %s", client.PeerIdentity())
	go h.readLoop(client)
}

// RegisterTransport attaches an externally-constructed Transport (Steam
// Sockets in Phase 2, the direct-connect transportbridge in §13) as a
// hub-managed client. The peer is treated identically to a WebSocket client
// from this point forward — no transport-specific branches downstream.
func (h *Hub) RegisterTransport(transport Transport) *Client {
	client := NewClient(transport)
	log.Printf("peer connected: %s", client.PeerIdentity())
	go h.readLoop(client)
	return client
}

func (h *Hub) readLoop(client *Client) {
	defer h.cleanupClient(client, true)

	for {
		data, err := client.Read()
		if err != nil {
			log.Printf("read error from %s: %v", client.PeerIdentity(), err)
			return
		}

		var base protocol.ClientMessage
		if err := json.Unmarshal(data, &base); err != nil {
			_ = client.WriteJSON(protocol.ErrorMessage{
				Type:    "error",
				Message: "invalid message",
			})
			continue
		}

		switch base.Type {
		case "hello":
			// §17 task 17.1: version-mismatch close code. Only enforced when
			// the server's version is set (empty = test mode); both values
			// equal to "dev" or "unknown" match by construction so the
			// dev/CI workflows never trip this gate (per the
			// embedded-spa-serving spec D23).
			var msg protocol.HelloMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "invalid hello payload",
				})
				continue
			}
			if h.version != "" && msg.Version != "" && msg.Version != h.version {
				log.Printf("ws: version mismatch (server=%q client=%q) — closing %s",
					h.version, msg.Version, client.PeerIdentity())
				// Try to send a close frame with the documented code; ignore
				// errors because we're closing anyway.
				if wt, ok := client.Transport().(*websocketTransport); ok {
					msg := websocket.FormatCloseMessage(CloseCodeVersionMismatch,
						"build mismatch — please restart")
					_ = wt.conn.WriteControl(websocket.CloseMessage, msg,
						time.Now().Add(writeDeadline))
				}
				return
			}

		case "join_match":
			var msg protocol.JoinMatchMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "invalid join_match payload",
				})
				continue
			}

			mapID := msg.MapID
			if mapID == "" {
				mapID = game.DefaultMapID()
			}

			var match *game.Match
			if msg.MatchID != "" {
				if existing, ok := h.manager.GetMatch(msg.MatchID); ok {
					match = existing
					// Cancel any pending removal — this is a reconnect.
					reconnect := match.CancelPlayerRemoval(msg.PlayerID)
					if reconnect {
						log.Printf("reconnect: player=%s match=%s\n", msg.PlayerID, match.ID)
					}
				}
			}
			if match == nil {
				match = h.manager.FindOrCreateMatch(mapID)
			}

			client.SetPlayerID(msg.PlayerID)
			client.SetMatchID(match.ID)
			client.TouchPong()

			match.AddClient(client)
			log.Printf("join_match: player=%s equippedBuffIDs=%v\n", msg.PlayerID, msg.EquippedBuffIDs)
			match.State.EnsurePlayer(msg.PlayerID, msg.EquippedBuffIDs...)

			welcome := protocol.WelcomeMessage{
				Type:     "welcome",
				PlayerID: msg.PlayerID,
				MatchID:  match.ID,
				Map:      match.State.GetMapConfig(),
			}
			if err := client.WriteJSON(welcome); err != nil {
				log.Println("failed to send welcome:", err)
				return
			}

			snapshot := match.State.Snapshot()
			snapshot.MatchID = match.ID
			if err := client.WriteJSON(snapshot); err != nil {
				log.Println("failed to send snapshot:", err)
				return
			}

		case "leave_match":
			var msg protocol.LeaveMatchMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "invalid leave_match payload",
				})
				continue
			}

			match, ok := h.manager.GetMatch(msg.MatchID)
			if !ok {
				continue
			}

			match.RemovePlayer(msg.PlayerID)
			match.RemoveClient(client)
			if match.ClientCount() == 0 {
				h.manager.DeleteMatch(match.ID)
			} else {
				match.BroadcastSnapshot()
			}

			if client.MatchID() == msg.MatchID {
				client.SetMatchID("")
			}
			if client.PlayerID() == msg.PlayerID {
				client.SetPlayerID("")
			}

		case "move_command":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "must join a match before sending commands",
				})
				continue
			}

			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "match not found",
				})
				continue
			}

			var msg protocol.MoveCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "invalid move_command payload",
				})
				continue
			}

			// The 20 Hz tick loop is the sole broadcast path; per-command
			// broadcasts are redundant and amplify bandwidth.
			match.State.MoveUnits(client.PlayerID(), msg.UnitIDs, msg.Destination)

		case "gather_command":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "must join a match before sending commands",
				})
				continue
			}

			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "match not found",
				})
				continue
			}

			var msg protocol.GatherCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "invalid gather_command payload",
				})
				continue
			}

			match.State.GatherWithUnits(client.PlayerID(), msg.UnitIDs, msg.TargetID)

		case "deposit_command":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "must join a match before sending commands",
				})
				continue
			}

			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "match not found",
				})
				continue
			}

			var msg protocol.DepositCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "invalid deposit_command payload",
				})
				continue
			}

			match.State.DepositWithUnits(client.PlayerID(), msg.UnitIDs, msg.BuildingID)

		case "train_unit_command":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "must join a match before sending commands"})
				continue
			}

			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "match not found"})
				continue
			}

			var msg protocol.TrainUnitCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "invalid train_unit_command payload"})
				continue
			}

			if !match.State.CanAffordUnit(client.PlayerID(), msg.UnitType) {
				_ = client.WriteJSON(protocol.NotificationMessage{Type: "notification", Message: "Not enough resources"})
				continue
			}
			match.State.TrainUnit(client.PlayerID(), msg.BuildingID, msg.UnitType)

		case "cancel_training_command":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "must join a match before sending commands",
				})
				continue
			}

			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "match not found",
				})
				continue
			}

			var msg protocol.CancelTrainingCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "invalid cancel_training_command payload",
				})
				continue
			}

			match.State.CancelTrainingAt(client.PlayerID(), msg.BuildingID, msg.QueueIndex)

		case "set_building_spawn_point_command":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "must join a match before sending commands",
				})
				continue
			}

			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "match not found",
				})
				continue
			}

			var msg protocol.SetBuildingSpawnPointCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "invalid set_building_spawn_point_command payload",
				})
				continue
			}

			match.State.SetBuildingSpawnPoint(client.PlayerID(), msg.BuildingID, msg.Point)

		case "build_building_command":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "must join a match before sending commands"})
				continue
			}

			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "match not found"})
				continue
			}

			var msg protocol.BuildBuildingCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "invalid build_building_command payload"})
				continue
			}

			if !match.State.CanAffordBuilding(client.PlayerID(), msg.BuildingType) {
				_ = client.WriteJSON(protocol.NotificationMessage{Type: "notification", Message: "Not enough resources"})
				continue
			}
			match.State.BuildBuilding(client.PlayerID(), msg.BuildingType, msg.UnitIDs, msg.GridX, msg.GridY)

		case "attack_command":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "must join a match before sending commands",
				})
				continue
			}

			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "match not found",
				})
				continue
			}

			var msg protocol.AttackCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "invalid attack_command payload",
				})
				continue
			}

			match.State.AttackWithUnits(client.PlayerID(), msg.UnitIDs, msg.TargetUnitID)

		case "cast_ability_command":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "must join a match before sending commands"})
				continue
			}
			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "match not found"})
				continue
			}
			var msg protocol.CastAbilityCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "invalid cast_ability_command payload"})
				continue
			}
			if ok, reason := match.State.RequestAbilityCast(client.PlayerID(), msg.CasterUnitID, msg.AbilityID, msg.TargetUnitID); !ok {
				_ = client.WriteJSON(protocol.NotificationMessage{Type: "notification", Message: reason})
			}

		case "cast_commander_ability":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "must join a match before sending commands"})
				continue
			}
			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "match not found"})
				continue
			}
			var msg protocol.CastCommanderAbilityCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "invalid cast_commander_ability payload"})
				continue
			}
			if ok, reason := match.State.RequestCastCommanderAbility(client.PlayerID(), msg.AbilityID, msg.X, msg.Y); !ok {
				_ = client.WriteJSON(protocol.NotificationMessage{Type: "notification", Message: reason})
			}

		case "set_focus_target_command":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "must join a match before sending commands"})
				continue
			}
			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "match not found"})
				continue
			}
			var msg protocol.SetFocusTargetCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "invalid set_focus_target_command payload"})
				continue
			}
			if ok2, reason := match.State.RequestSetFocusTarget(client.PlayerID(), msg.CasterUnitID, msg.TargetUnitID); !ok2 {
				_ = client.WriteJSON(protocol.NotificationMessage{Type: "notification", Message: reason})
			}

		case "toggle_autocast_command":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "must join a match before sending commands"})
				continue
			}
			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "match not found"})
				continue
			}
			var msg protocol.ToggleAutoCastCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "invalid toggle_autocast_command payload"})
				continue
			}
			// Silent no-op when invalid (not owned / not an auto-cast ability)
			// per spec — no notification.
			match.State.ToggleAutoCast(client.PlayerID(), msg.UnitID, msg.AbilityID)

		case "attack_move_command":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "must join a match before sending commands",
				})
				continue
			}

			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "match not found",
				})
				continue
			}

			var msg protocol.AttackMoveCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "invalid attack_move_command payload",
				})
				continue
			}

			match.State.AttackMoveUnits(client.PlayerID(), msg.UnitIDs, msg.Destination)

		case "set_stance_command":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "must join a match before sending commands",
				})
				continue
			}

			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "match not found",
				})
				continue
			}

			var msg protocol.SetStanceCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "invalid set_stance_command payload",
				})
				continue
			}

			match.State.SetUnitStance(client.PlayerID(), msg.UnitIDs, msg.Stance)

		case "patrol_command":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "must join a match before sending commands",
				})
				continue
			}

			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "match not found",
				})
				continue
			}

			var msg protocol.PatrolCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "invalid patrol_command payload",
				})
				continue
			}

			match.State.PatrolUnits(client.PlayerID(), msg.UnitIDs, msg.Destination)

		case "repair_command":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "must join a match before sending commands",
				})
				continue
			}

			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "match not found",
				})
				continue
			}

			var msg protocol.RepairCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "invalid repair_command payload",
				})
				continue
			}

			match.State.RepairBuilding(client.PlayerID(), msg.UnitIDs, msg.BuildingID)

		case "kick_builders_command":
			if client.MatchID() == "" {
				continue
			}
			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				continue
			}
			var msg protocol.KickBuildersCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "invalid kick_builders_command payload",
				})
				continue
			}
			match.State.KickBuildersFromBuilding(client.PlayerID(), msg.BuildingID)

		case "demolish_building_command":
			if client.MatchID() == "" {
				continue
			}
			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				continue
			}
			var msg protocol.DemolishBuildingCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "invalid demolish_building_command payload",
				})
				continue
			}
			match.State.DemolishBuilding(client.PlayerID(), msg.BuildingID)

		case "purchase_upgrade":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "must join a match before sending commands"})
				continue
			}
			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "match not found"})
				continue
			}
			var msg protocol.PurchaseUpgradeCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "invalid purchase_upgrade payload"})
				continue
			}
			match.State.PurchaseUpgrade(client.PlayerID(), msg.Track)

		case "upgrade_townhall":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "must join a match before sending commands"})
				continue
			}
			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "match not found"})
				continue
			}
			var msg protocol.UpgradeTownHallCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "invalid upgrade_townhall payload"})
				continue
			}
			match.State.UpgradeTownHall(client.PlayerID(), msg.BuildingID)

		case "purchase_item":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "must join a match before sending commands"})
				continue
			}
			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "match not found"})
				continue
			}
			var msg protocol.PurchaseItemCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "invalid purchase_item payload"})
				continue
			}
			match.State.PurchaseItem(client.PlayerID(), msg.BuildingID, msg.ItemID)

		case "equip_item":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "must join a match before sending commands"})
				continue
			}
			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "match not found"})
				continue
			}
			var msg protocol.EquipItemCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "invalid equip_item payload"})
				continue
			}
			match.State.EquipItem(client.PlayerID(), msg.UnitID, msg.SlotIndex, msg.InstanceID)

		case "unequip_item":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "must join a match before sending commands"})
				continue
			}
			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "match not found"})
				continue
			}
			var msg protocol.UnequipItemCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "invalid unequip_item payload"})
				continue
			}
			match.State.UnequipItem(client.PlayerID(), msg.UnitID, msg.SlotIndex)

		case "wave_upgrade_choice":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "must join a match before sending commands"})
				continue
			}
			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "match not found"})
				continue
			}
			var msg protocol.WaveUpgradeChoiceMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "invalid wave_upgrade_choice payload"})
				continue
			}
			match.State.HandleWaveUpgradeChoice(client.PlayerID(), msg.UpgradeID, msg.TargetUnitID)

		case "wave_upgrade_reroll":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "must join a match before sending commands"})
				continue
			}
			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "match not found"})
				continue
			}
			match.State.HandleWaveUpgradeReroll(client.PlayerID())

		case "use_consumable":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "must join a match before sending commands"})
				continue
			}
			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "match not found"})
				continue
			}
			var msg protocol.UseConsumableCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "invalid use_consumable payload"})
				continue
			}
			match.State.UseConsumable(client.PlayerID(), msg.UnitID, msg.SlotIndex)

		case "transfer_item":
			if client.MatchID() == "" {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "must join a match before sending commands"})
				continue
			}
			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "match not found"})
				continue
			}
			var msg protocol.TransferItemCommandMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{Type: "error", Message: "invalid transfer_item payload"})
				continue
			}
			match.State.TransferItem(client.PlayerID(), msg.FromUnitID, msg.FromSlotIdx, msg.ToUnitID, msg.ToSlotIdx)

		case "debug_spawn_unit":
			// Dev-only: spawn an arbitrary enemy unit with a chosen perk
			// loadout. Gated on the map's debug.debugSpawn flag; on
			// production maps the command is silently ignored (logged only)
			// so a malicious client cannot exploit this on live gameplay.
			if client.MatchID() == "" {
				continue
			}
			match, ok := h.manager.GetMatch(client.MatchID())
			if !ok {
				continue
			}
			var msg protocol.DebugSpawnUnitMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				_ = client.WriteJSON(protocol.ErrorMessage{
					Type:    "error",
					Message: "invalid debug_spawn_unit payload",
				})
				continue
			}
			if !match.State.DebugSpawnEnabled() {
				log.Printf("debug_spawn_unit rejected: map does not have debug.debugSpawn enabled (match=%s player=%s)",
					match.ID, client.PlayerID())
				continue
			}
			if !match.State.DebugSpawnUnit(msg, client.PlayerID()) {
				_ = client.WriteJSON(protocol.NotificationMessage{
					Type:    "notification",
					Message: "Debug spawn failed (unknown unit type?)",
				})
			}

		case "pong":
			client.TouchPong()

		default:
			_ = client.WriteJSON(protocol.ErrorMessage{
				Type:    "error",
				Message: "unknown message type",
			})
		}
	}
}

func (h *Hub) heartbeatLoop() {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-h.quit:
			return

		case <-ticker.C:
			matches := h.manager.ListMatches()

			for _, match := range matches {
				clients := match.ListClients()

				for _, rawClient := range clients {
					client, ok := rawClient.(*Client)
					if !ok {
						continue
					}

					if time.Since(client.LastPong()) > heartbeatTimeout {
						log.Printf("heartbeat timeout for player=%s match=%s\n", client.PlayerID(), client.MatchID())
						h.cleanupClient(client, false)
						continue
					}

					// Send a WebSocket-level ping frame. The client's pong handler
					// will call TouchPong and extend the read deadline.
					if err := client.WritePing(); err != nil {
						log.Printf("ping failed for player=%s match=%s: %v\n", client.PlayerID(), client.MatchID(), err)
						h.cleanupClient(client, false)
					}
				}
			}
		}
	}
}

func (h *Hub) cleanupClient(client *Client, closeConn bool) {
	matchID := client.MatchID()
	playerID := client.PlayerID()

	if matchID != "" {
		if match, ok := h.manager.GetMatch(matchID); ok {
			if playerID != "" {
				// Schedule removal after a grace window so transient drops
				// (tab sleep, flaky radio, etc.) don't destroy the player's
				// in-match state. The timer calls RemovePlayer and then
				// triggers a match-deletion check if the match is empty.
				match.SchedulePlayerRemoval(playerID, game.PlayerRemovalGrace, h.manager)
			}
			match.RemoveClient(client)
			// Delete only when no active clients AND no pending removals remain.
			if match.ClientCount() == 0 && match.PendingCleanupCount() == 0 {
				h.manager.DeleteMatch(match.ID)
			} else {
				match.BroadcastSnapshot()
			}
		}
	}

	client.SetMatchID("")
	client.SetPlayerID("")

	if closeConn {
		client.Close()
	}
}
