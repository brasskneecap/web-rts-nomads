package httpserver

import (
	"encoding/json"
	"net/http"
	"regexp"

	"webrts/server/internal/game"
	"webrts/server/internal/profile"
)

var uuidRe = regexp.MustCompile(`^[0-9a-f-]{36}$`)

// writeJSON writes v as JSON with status 200. Ignores encoding errors (the
// response is already committed).
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONError writes a JSON error body: {"error": code, "message": msg}.
func writeJSONError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "message": msg})
}

// extractPlayerID reads the X-Player-ID header. Returns "" and writes a 400
// error when the header is absent or not a valid UUID.
func extractPlayerID(w http.ResponseWriter, r *http.Request) string {
	id := r.Header.Get("X-Player-ID")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "missing_player_id", "X-Player-ID header is required")
		return ""
	}
	if !uuidRe.MatchString(id) {
		writeJSONError(w, http.StatusBadRequest, "invalid_player_id", "X-Player-ID must be a lowercase UUID (36 chars, hex + dashes)")
		return ""
	}
	return id
}

// registerProfileRoutes wires all profile and catalog routes onto mux.
func registerProfileRoutes(mux *http.ServeMux, pm *profile.Manager) {
	mux.HandleFunc("/api/profile", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		playerID := extractPlayerID(w, r)
		if playerID == "" {
			return
		}
		p, err := pm.GetOrCreate(playerID, profile.DefaultCommanderID, []string{"iron_discipline", "all_out_assault", "enemy_empowered"})
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "profile_error", err.Error())
			return
		}
		writeJSON(w, map[string]any{
			"profile":    p,
			"buffCatalog": game.ListPlayerBuffDefs(),
		})
	})

	mux.HandleFunc("/api/profile/loadout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		playerID := extractPlayerID(w, r)
		if playerID == "" {
			return
		}
		var body struct {
			EquippedBuffIDs []string `json:"equippedBuffIds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
			return
		}

		tuning := game.ExportedGameplayTuning()
		if len(body.EquippedBuffIDs) > tuning.BuffSlots.MaxActive {
			writeJSONError(w, http.StatusBadRequest, "too_many_buffs",
				"cannot equip more than the maximum number of buff slots")
			return
		}

		// Check for duplicates.
		seen := make(map[string]bool, len(body.EquippedBuffIDs))
		for _, id := range body.EquippedBuffIDs {
			if seen[id] {
				writeJSONError(w, http.StatusBadRequest, "buff_duplicate", "duplicate buff id: "+id)
				return
			}
			seen[id] = true
		}

		var updated *profile.PlayerProfile
		err := pm.WithLocked(playerID, func(p *profile.PlayerProfile) error {
			unlockedSet := make(map[string]bool, len(p.UnlockedBuffIDs))
			for _, id := range p.UnlockedBuffIDs {
				unlockedSet[id] = true
			}
			for _, id := range body.EquippedBuffIDs {
				if game.PlayerBuffDefByID(id) == nil {
					writeJSONError(w, http.StatusBadRequest, "unknown_buff", "unknown buff id: "+id)
					return errAbort
				}
				if !unlockedSet[id] {
					writeJSONError(w, http.StatusBadRequest, "buff_not_unlocked", "buff not unlocked: "+id)
					return errAbort
				}
			}
			p.EquippedBuffIDs = append([]string(nil), body.EquippedBuffIDs...)
			updated = p
			return nil
		})
		if err != nil {
			if _, ok := err.(errAbortType); ok {
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "profile_error", err.Error())
			return
		}
		writeJSON(w, updated)
	})

	mux.HandleFunc("/api/profile/unlock-buff", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		playerID := extractPlayerID(w, r)
		if playerID == "" {
			return
		}
		var body struct {
			BuffID string `json:"buffId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
			return
		}

		def := game.PlayerBuffDefByID(body.BuffID)
		if def == nil {
			writeJSONError(w, http.StatusBadRequest, "unknown_buff", "unknown buff id: "+body.BuffID)
			return
		}

		var updated *profile.PlayerProfile
		err := pm.WithLocked(playerID, func(p *profile.PlayerProfile) error {
			for _, id := range p.UnlockedBuffIDs {
				if id == body.BuffID {
					writeJSONError(w, http.StatusBadRequest, "already_unlocked", "buff already unlocked: "+body.BuffID)
					return errAbort
				}
			}
			if p.LegendPoints < def.UnlockCost {
				writeJSONError(w, http.StatusBadRequest, "insufficient_legend_points",
					"not enough legend points to unlock this buff")
				return errAbort
			}
			p.LegendPoints -= def.UnlockCost
			p.UnlockedBuffIDs = append(p.UnlockedBuffIDs, body.BuffID)
			updated = p
			return nil
		})
		if err != nil {
			if _, ok := err.(errAbortType); ok {
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "profile_error", err.Error())
			return
		}
		writeJSON(w, updated)
	})

	mux.HandleFunc("/api/catalog/player-buffs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"buffs": game.ListPlayerBuffDefs(),
		})
	})

	mux.HandleFunc("/api/catalog/tuning", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, game.ExportedGameplayTuning())
	})

	mux.HandleFunc("/api/catalog/neutral-groups", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"tiers": game.ListNeutralGroupsForCatalog(),
		})
	})
}

// errAbortType is the type of errAbort so callers can use errors.Is.
type errAbortType struct{}

func (errAbortType) Error() string { return "response already written" }

// errAbort is a sentinel used inside WithLocked callbacks to signal that the
// HTTP response has already been written and no further action is needed.
var errAbort error = errAbortType{}
