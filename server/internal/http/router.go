// server/internal/http/router.go
package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"webrts/server/internal/game"
	"webrts/server/internal/ws"
)

func NewRouter(hub *ws.Hub, corsOrigin string) http.Handler {
	mux := http.NewServeMux()

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
			"units": game.ListUnitDefs(),
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

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"matchId":       match.ID,
			"mapId":         match.MapID,
			"isParticipant": match.HasPlayer(playerID),
		})
	})

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
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "invalid JSON body", http.StatusBadRequest)
				return
			}
			if body.MapID == "" || body.HostPlayerID == "" {
				http.Error(w, "mapId and hostPlayerId are required", http.StatusBadRequest)
				return
			}
			lobby, err := lm.Create(body.MapID, body.HostPlayerID)
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

func withCORS(next http.Handler, allowedOrigin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
