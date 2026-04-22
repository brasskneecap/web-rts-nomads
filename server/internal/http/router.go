// server/internal/http/router.go
package httpserver

import (
	"encoding/json"
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

	mux.HandleFunc("/ws", hub.HandleWS)

	return withCORS(mux, corsOrigin)
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
