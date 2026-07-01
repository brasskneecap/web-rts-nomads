package httpserver

import (
	"encoding/json"
	"net/http"
	"sort"

	"webrts/server/internal/game"
	"webrts/server/internal/profile"
)

// registerAdvancementRoutes wires the advancement purchase and catalog routes
// onto mux. Requires the match manager so the purchase handler can enforce the
// "not in active match" invariant.
func registerAdvancementRoutes(mux *http.ServeMux, pm *profile.Manager, mm matchInActiveChecker) {
	// POST /api/profile/advancements/purchase
	mux.HandleFunc("/api/profile/advancements/purchase", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		playerID := extractPlayerID(w, r)
		if playerID == "" {
			return
		}

		var body struct {
			AdvancementID string `json:"advancementId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
			return
		}
		if body.AdvancementID == "" {
			writeJSONError(w, http.StatusBadRequest, "missing_advancement_id", "advancementId is required")
			return
		}

		node, ok := game.GetAdvancementDef(body.AdvancementID)
		if !ok {
			writeJSONError(w, http.StatusBadRequest, "unknown_advancement", "unknown advancement id: "+body.AdvancementID)
			return
		}

		// Reject purchase if the player is currently in an active match. Profile
		// changes during a match would not take effect until the next match start,
		// which is confusing; block at the API level instead.
		if mm != nil && mm.IsPlayerInActiveMatch(playerID) {
			writeJSONError(w, http.StatusConflict, "player_in_match", "cannot purchase advancements while in an active match")
			return
		}

		// Resolve the prerequisite node ID (empty string means no prerequisite).
		prereqID := game.GetAdvancementPrerequisiteID(body.AdvancementID)

		type purchaseResponse struct {
			DominionPoints       int                           `json:"dominionPoints"`
			ConquestBadges       int                           `json:"conquestBadges"`
			AcquiredAdvancements []profile.AcquiredAdvancement `json:"acquiredAdvancements"`
		}

		var resp purchaseResponse
		err := pm.WithLocked(playerID, func(p *profile.PlayerProfile) error {
			// Idempotency: already acquired → reject.
			for _, aa := range p.AcquiredAdvancements {
				if aa.ID == body.AdvancementID {
					writeJSONError(w, http.StatusBadRequest, "already_acquired", "advancement is already acquired")
					return errAbort
				}
			}

			// Prerequisite gate: node N requires node N-1 to be acquired first.
			if prereqID != "" {
				prereqOwned := false
				for _, aa := range p.AcquiredAdvancements {
					if aa.ID == prereqID {
						prereqOwned = true
						break
					}
				}
				if !prereqOwned {
					writeJSONError(w, http.StatusBadRequest, "prerequisite_not_acquired", "prerequisite advancement must be acquired first: "+prereqID)
					return errAbort
				}
			}

			if p.DominionPoints < node.Cost {
				writeJSONError(w, http.StatusBadRequest, "insufficient_dominion_points", "not enough dominion points to purchase this advancement")
				return errAbort
			}

			// Major nodes also require 1 Conquest Badge. Check after DP-sufficiency
			// so both deficits are reported in a consistent order.
			badgesPaid := 0
			if node.Kind == "major" {
				if p.ConquestBadges < 1 {
					writeJSONError(w, http.StatusBadRequest, "insufficient_conquest_badges", "a Conquest Badge is required to purchase a major advancement")
					return errAbort
				}
				badgesPaid = 1
			}

			p.DominionPoints -= node.Cost
			p.ConquestBadges -= badgesPaid
			p.AcquiredAdvancements = append(p.AcquiredAdvancements, profile.AcquiredAdvancement{
				ID:         node.ID,
				CostPaid:   node.Cost,
				BadgesPaid: badgesPaid,
			})
			// Keep the list sorted by ID for deterministic iteration.
			sort.Slice(p.AcquiredAdvancements, func(i, j int) bool {
				return p.AcquiredAdvancements[i].ID < p.AcquiredAdvancements[j].ID
			})
			resp = purchaseResponse{
				DominionPoints:       p.DominionPoints,
				ConquestBadges:       p.ConquestBadges,
				AcquiredAdvancements: p.AcquiredAdvancements,
			}
			return nil
		})
		if err != nil {
			if _, ok := err.(errAbortType); ok {
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "profile_error", err.Error())
			return
		}
		writeJSON(w, resp)
	})

	// POST /api/profile/advancements/reset
	//
	// Refunds every acquired advancement's paid cost back to Dominion Points and
	// clears the acquired list, returning the player to a clean slate. Intended
	// as a dev/testing affordance for A/B-comparing unit behavior with and
	// without advancements; the refund means the player can immediately re-buy.
	mux.HandleFunc("/api/profile/advancements/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		playerID := extractPlayerID(w, r)
		if playerID == "" {
			return
		}

		// Same guard as purchase: profile changes during a match would not take
		// effect until the next match start, so block at the API level.
		if mm != nil && mm.IsPlayerInActiveMatch(playerID) {
			writeJSONError(w, http.StatusConflict, "player_in_match", "cannot reset advancements while in an active match")
			return
		}

		type resetResponse struct {
			DominionPoints       int                           `json:"dominionPoints"`
			ConquestBadges       int                           `json:"conquestBadges"`
			AcquiredAdvancements []profile.AcquiredAdvancement `json:"acquiredAdvancements"`
		}

		var resp resetResponse
		err := pm.WithLocked(playerID, func(p *profile.PlayerProfile) error {
			for _, aa := range p.AcquiredAdvancements {
				p.DominionPoints += aa.CostPaid
				p.ConquestBadges += aa.BadgesPaid
			}
			// Empty (non-nil) slice so the JSON serializes as [] rather than null.
			p.AcquiredAdvancements = []profile.AcquiredAdvancement{}
			resp = resetResponse{
				DominionPoints:       p.DominionPoints,
				ConquestBadges:       p.ConquestBadges,
				AcquiredAdvancements: p.AcquiredAdvancements,
			}
			return nil
		})
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "profile_error", err.Error())
			return
		}
		writeJSON(w, resp)
	})
}

// matchInActiveChecker is the minimal interface needed from *game.MatchManager
// for the purchase guard. Declared here so the handler package does not import
// the concrete game.MatchManager type at package scope (avoiding a hard coupling
// and making the handler easier to test with a stub).
type matchInActiveChecker interface {
	IsPlayerInActiveMatch(playerID string) bool
}

// refundStaleAdvancementCosts checks the player's AcquiredAdvancements for
// any entry where the catalog cost has changed (decreased) or the advancement
// no longer exists in the catalog, and issues a proportional Dominion Point
// refund. Returns true when the profile was modified.
//
// This is the "refund-on-cost-change" migration. Call it inside a
// pm.WithLocked callback so changes are persisted atomically.
func refundStaleAdvancementCosts(p *profile.PlayerProfile) bool {
	modified := false
	kept := p.AcquiredAdvancements[:0]
	for _, aa := range p.AcquiredAdvancements {
		node, exists := game.GetAdvancementDef(aa.ID)
		if !exists {
			// Advancement removed from catalog — full refund of cost paid and
			// any badges consumed at purchase time.
			p.DominionPoints += aa.CostPaid
			p.ConquestBadges += aa.BadgesPaid
			modified = true
			continue // drop from list
		}
		if node.Cost < aa.CostPaid {
			// Cost decreased — refund the delta.
			delta := aa.CostPaid - node.Cost
			p.DominionPoints += delta
			aa.CostPaid = node.Cost
			modified = true
		}
		kept = append(kept, aa)
	}
	p.AcquiredAdvancements = kept
	return modified
}
