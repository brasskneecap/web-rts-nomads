package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"webrts/server/internal/game"
	"webrts/server/pkg/protocol"

	"github.com/gorilla/websocket"
)

const (
	heartbeatInterval = 30 * time.Second
	heartbeatTimeout  = 75 * time.Second
)

type Hub struct {
	upgrader websocket.Upgrader
	manager  *game.MatchManager
	quit     chan struct{}
}

func NewHub(manager *game.MatchManager) *Hub {
	h := &Hub{
		manager: manager,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		quit: make(chan struct{}),
	}

	go h.heartbeatLoop()

	return h
}

// Close signals the heartbeat goroutine to stop. Call during graceful shutdown.
func (h *Hub) Close() {
	close(h.quit)
}

func (h *Hub) GetMatch(matchID string) (*game.Match, bool) {
	return h.manager.GetMatch(matchID)
}

func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("upgrade error:", err)
		return
	}

	client := NewClient(conn)
	go h.readLoop(client)
}

func (h *Hub) readLoop(client *Client) {
	defer h.cleanupClient(client, true)

	for {
		_, data, err := client.Conn.ReadMessage()
		if err != nil {
			log.Println("read error:", err)
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
			match.State.EnsurePlayer(msg.PlayerID)

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
