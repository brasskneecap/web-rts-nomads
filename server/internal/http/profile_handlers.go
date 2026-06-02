package httpserver

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sort"

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

		// Load (or create) the profile, running the advancement cost-change
		// refund migration atomically inside WithLocked so any refunds are
		// persisted before we return the profile to the caller.
		var p *profile.PlayerProfile
		migrationErr := pm.WithLocked(playerID, func(prof *profile.PlayerProfile) error {
			if refundStaleAdvancementCosts(prof) {
				// Profile was modified; WithLocked will persist it.
			}
			p = prof
			return nil
		})
		if migrationErr != nil {
			writeJSONError(w, http.StatusInternalServerError, "profile_error", migrationErr.Error())
			return
		}

		writeJSON(w, map[string]any{
			"profile":               p,
			"profileUpgradeCatalog": game.ListProfileUpgradeDefs(),
			"advancementCatalog":    game.ListUnitAdvancementTracks(),
		})
	})

	mux.HandleFunc("/api/profile/upgrades/purchase", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		playerID := extractPlayerID(w, r)
		if playerID == "" {
			return
		}
		var body struct {
			UpgradeID string `json:"upgradeId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
			return
		}
		def, ok := game.GetProfileUpgradeDef(body.UpgradeID)
		if !ok {
			writeJSONError(w, http.StatusBadRequest, "unknown_upgrade", "unknown upgrade id: "+body.UpgradeID)
			return
		}
		var updated *profile.PlayerProfile
		err := pm.WithLocked(playerID, func(p *profile.PlayerProfile) error {
			currentRank := p.OwnedUpgradeRanks[body.UpgradeID]
			if currentRank >= def.MaxRanks {
				writeJSONError(w, http.StatusBadRequest, "max_rank_reached", "upgrade is already at maximum rank")
				return errAbort
			}
			cost := def.CostPerRank[currentRank]
			if p.LegendPoints < cost {
				writeJSONError(w, http.StatusBadRequest, "insufficient_legend_points", "not enough legend points to purchase this rank")
				return errAbort
			}
			p.LegendPoints -= cost
			p.OwnedUpgradeRanks[body.UpgradeID] = currentRank + 1
			// Auto-activate on first rank purchase.
			if currentRank == 0 {
				p.ActiveUpgradeIDs = addToSortedSet(p.ActiveUpgradeIDs, body.UpgradeID)
			}
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

	mux.HandleFunc("/api/profile/upgrades/refund", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		playerID := extractPlayerID(w, r)
		if playerID == "" {
			return
		}
		var body struct {
			UpgradeID string `json:"upgradeId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
			return
		}
		def, ok := game.GetProfileUpgradeDef(body.UpgradeID)
		if !ok {
			writeJSONError(w, http.StatusBadRequest, "unknown_upgrade", "unknown upgrade id: "+body.UpgradeID)
			return
		}
		var updated *profile.PlayerProfile
		err := pm.WithLocked(playerID, func(p *profile.PlayerProfile) error {
			currentRank := p.OwnedUpgradeRanks[body.UpgradeID]
			if currentRank <= 0 {
				writeJSONError(w, http.StatusBadRequest, "not_owned", "upgrade is not owned at any rank")
				return errAbort
			}
			refund := def.CostPerRank[currentRank-1]
			p.LegendPoints += refund
			p.OwnedUpgradeRanks[body.UpgradeID] = currentRank - 1
			// Auto-deactivate when fully refunded.
			if currentRank-1 == 0 {
				p.ActiveUpgradeIDs = removeFromSortedSet(p.ActiveUpgradeIDs, body.UpgradeID)
			}
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

	mux.HandleFunc("/api/profile/upgrades/toggle", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		playerID := extractPlayerID(w, r)
		if playerID == "" {
			return
		}
		var body struct {
			UpgradeID string `json:"upgradeId"`
			Active    bool   `json:"active"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
			return
		}
		if _, ok := game.GetProfileUpgradeDef(body.UpgradeID); !ok {
			writeJSONError(w, http.StatusBadRequest, "unknown_upgrade", "unknown upgrade id: "+body.UpgradeID)
			return
		}
		var updated *profile.PlayerProfile
		err := pm.WithLocked(playerID, func(p *profile.PlayerProfile) error {
			if p.OwnedUpgradeRanks[body.UpgradeID] <= 0 {
				writeJSONError(w, http.StatusBadRequest, "not_owned", "upgrade is not owned")
				return errAbort
			}
			if body.Active {
				p.ActiveUpgradeIDs = addToSortedSet(p.ActiveUpgradeIDs, body.UpgradeID)
			} else {
				p.ActiveUpgradeIDs = removeFromSortedSet(p.ActiveUpgradeIDs, body.UpgradeID)
			}
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

	// DEV-ONLY: grant Legend Points to the caller for testing the advancement
	// / upgrade purchase flow without grinding matches. Body: `{amount: int}`.
	// Negative amounts are rejected. Returns the updated profile.
	//
	// TODO: gate behind a build tag or env var (e.g. WEBRTS_DEV=1) before
	// shipping a production build. For now it's an unconditional endpoint to
	// keep iteration fast.
	mux.HandleFunc("/api/profile/dev/grant-legend-points", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		playerID := extractPlayerID(w, r)
		if playerID == "" {
			return
		}
		var body struct {
			Amount int `json:"amount"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
			return
		}
		if body.Amount <= 0 {
			writeJSONError(w, http.StatusBadRequest, "invalid_amount", "amount must be > 0")
			return
		}
		var updated *profile.PlayerProfile
		err := pm.WithLocked(playerID, func(p *profile.PlayerProfile) error {
			p.LegendPoints += body.Amount
			p.LifetimeLegendPoints += body.Amount
			updated = p
			return nil
		})
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "profile_error", err.Error())
			return
		}
		writeJSON(w, updated)
	})

	mux.HandleFunc("/api/catalog/profile-upgrades", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"upgrades": game.ListProfileUpgradeDefs(),
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

// addToSortedSet appends id to the sorted slice if not already present.
func addToSortedSet(s []string, id string) []string {
	for _, existing := range s {
		if existing == id {
			return s
		}
	}
	s = append(s, id)
	sort.Strings(s)
	return s
}

// removeFromSortedSet removes id from the sorted slice if present.
func removeFromSortedSet(s []string, id string) []string {
	out := s[:0]
	for _, existing := range s {
		if existing != id {
			out = append(out, existing)
		}
	}
	return out
}

// errAbortType is the type of errAbort so callers can use errors.Is.
type errAbortType struct{}

func (errAbortType) Error() string { return "response already written" }

// errAbort is a sentinel used inside WithLocked callbacks to signal that the
// HTTP response has already been written and no further action is needed.
var errAbort error = errAbortType{}
