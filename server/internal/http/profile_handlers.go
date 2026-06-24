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
//
// `mm` is consulted by the dev-reset endpoint to refuse a wipe while the
// player is mid-match (profile mutations don't apply until the next match
// start, so silently allowing it would confuse the player). Tests that
// don't exercise the reset endpoint may pass nil.
func registerProfileRoutes(mux *http.ServeMux, pm *profile.Manager, mm matchInActiveChecker) {
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
			if p.DominionPoints < cost {
				writeJSONError(w, http.StatusBadRequest, "insufficient_dominion_points", "not enough dominion points to purchase this rank")
				return errAbort
			}
			p.DominionPoints -= cost
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
			p.DominionPoints += refund
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

	// DEV-ONLY: grant Dominion Points to the caller for testing the advancement
	// / upgrade purchase flow without grinding matches. Body: `{amount: int}`.
	// Negative amounts are rejected. Returns the updated profile.
	//
	// TODO: gate behind a build tag or env var (e.g. WEBRTS_DEV=1) before
	// shipping a production build. For now it's an unconditional endpoint to
	// keep iteration fast.
	mux.HandleFunc("/api/profile/dev/grant-dominion-points", func(w http.ResponseWriter, r *http.Request) {
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
			p.DominionPoints += body.Amount
			p.LifetimeDominionPoints += body.Amount
			updated = p
			return nil
		})
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "profile_error", err.Error())
			return
		}
		writeJSON(w, updated)
	})

	// DEV-ONLY: hard-reset the calling player's profile back to a fresh
	// state — zeroes DP / lifetime DP / stats / wave-upgrade caps, empties
	// owned & active upgrades, acquired advancements, completed campaign
	// levels & objectives, and re-installs the default commander.
	// CreatedAtUnix is preserved (the profile still belongs to the same
	// player); UpdatedAtUnix is bumped by WithLocked. Refused while the
	// player is in an active match for the same reason as the advancement
	// reset endpoint — profile mutations don't apply until the next match
	// start, so silently allowing it would be misleading.
	mux.HandleFunc("/api/profile/dev/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		playerID := extractPlayerID(w, r)
		if playerID == "" {
			return
		}
		if mm != nil && mm.IsPlayerInActiveMatch(playerID) {
			writeJSONError(w, http.StatusConflict, "player_in_match", "cannot reset profile while in an active match")
			return
		}
		var updated *profile.PlayerProfile
		err := pm.WithLocked(playerID, func(p *profile.PlayerProfile) error {
			p.Version = profile.CurrentVersion
			p.DominionPoints = 0
			p.LifetimeDominionPoints = 0
			p.OwnedCommanderIDs = []string{profile.DefaultCommanderID}
			p.SelectedCommanderID = profile.DefaultCommanderID
			p.Stats = profile.ProfileStats{}
			p.MaxRerolls = 0
			p.MaxUpgradeStacks = 0
			p.OwnedUpgradeRanks = map[string]int{}
			p.ActiveUpgradeIDs = []string{}
			p.AcquiredAdvancements = []profile.AcquiredAdvancement{}
			p.CompletedCampaignLevels = []string{}
			p.CompletedCampaignObjectives = map[string][]string{}
			updated = p
			return nil
		})
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "profile_error", err.Error())
			return
		}
		writeJSON(w, updated)
	})

	// Client-driven end-of-match dominion-point award. Needed because in
	// host-authoritative multiplayer the joiner's profile lives on the
	// joiner's machine — the host cannot write it. The joiner POSTs its own
	// server-reported earned total here against ITS local server. Idempotent
	// by matchId so a recap re-mount / retry cannot double-credit.
	//
	// Body: {matchId: string, amount: int}  Header: X-Player-ID
	mux.HandleFunc("/api/profile/match/award-dominion-points", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		playerID := extractPlayerID(w, r)
		if playerID == "" {
			return
		}
		var body struct {
			MatchID string `json:"matchId"`
			Amount  int    `json:"amount"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
			return
		}
		if body.MatchID == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_match_id", "matchId is required")
			return
		}
		if body.Amount < 0 {
			writeJSONError(w, http.StatusBadRequest, "invalid_amount", "amount must be >= 0")
			return
		}
		var updated *profile.PlayerProfile
		err := pm.WithLocked(playerID, func(p *profile.PlayerProfile) error {
			for _, id := range p.CreditedMatchIDs {
				if id == body.MatchID {
					updated = p // already credited — no-op
					return nil
				}
			}
			if body.Amount > 0 {
				p.DominionPoints += body.Amount
				p.LifetimeDominionPoints += body.Amount
			}
			p.CreditedMatchIDs = append(p.CreditedMatchIDs, body.MatchID)
			// Bound the ledger so it can't grow without limit; matchId reuse
			// across distinct sessions far enough apart is acceptable risk.
			const maxCredited = 50
			if len(p.CreditedMatchIDs) > maxCredited {
				p.CreditedMatchIDs = p.CreditedMatchIDs[len(p.CreditedMatchIDs)-maxCredited:]
			}
			updated = p
			return nil
		})
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "profile_error", err.Error())
			return
		}
		writeJSON(w, updated)
	})

	// Campaign progression. The campaign catalog (campaign IDs, level IDs,
	// prerequisite chains) lives on the client; the server only records which
	// level IDs the player has finished. Idempotent — re-completing a level is
	// a no-op rather than an error so the client can call this fire-and-forget
	// from the match-end hook without needing to track whether it already fired.
	//
	// Extension points: when richer per-level data is needed (best time, star
	// rating, score), replace the []string slice with a []CompletedCampaignLevel
	// struct and bump the profile schema version. The endpoint shape can stay
	// the same — just accept an optional body of metadata.
	mux.HandleFunc("/api/profile/campaign/complete-level", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		playerID := extractPlayerID(w, r)
		if playerID == "" {
			return
		}
		var body struct {
			LevelID string `json:"levelId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
			return
		}
		if body.LevelID == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_level_id", "levelId is required")
			return
		}
		var updated *profile.PlayerProfile
		err := pm.WithLocked(playerID, func(p *profile.PlayerProfile) error {
			p.CompletedCampaignLevels = addToSortedSet(p.CompletedCampaignLevels, body.LevelID)
			updated = p
			return nil
		})
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "profile_error", err.Error())
			return
		}
		writeJSON(w, updated)
	})

	// Campaign objective completion. Batched at match end by the client
	// (§15's MatchEndRecap dismiss handler) — one POST per match regardless
	// of outcome, carrying the union of objective IDs whose state.Completed
	// was true at the moment the match ended (failed objectives are not
	// written). Idempotent: re-completing the same objectives is a no-op.
	//
	// Body: {campaignId: string, levelId: string, objectiveIds: []string}
	// Header: X-Player-ID
	//
	// Storage: PlayerProfile.CompletedCampaignObjectives map[key][]string
	// where key = "<campaignId>/<levelId>". Sorted-set merge via
	// addToSortedSet for stable JSON wire format.
	mux.HandleFunc("/api/profile/campaign/complete-objectives", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		playerID := extractPlayerID(w, r)
		if playerID == "" {
			return
		}
		var body struct {
			CampaignID   string   `json:"campaignId"`
			LevelID      string   `json:"levelId"`
			ObjectiveIDs []string `json:"objectiveIds"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
			return
		}
		if body.CampaignID == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_campaign_id", "campaignId is required")
			return
		}
		if body.LevelID == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid_level_id", "levelId is required")
			return
		}
		// objectiveIds is allowed to be empty (a defeat with zero
		// completions still POSTs to keep client logic simple).
		key := body.CampaignID + "/" + body.LevelID
		var updated *profile.PlayerProfile
		err := pm.WithLocked(playerID, func(p *profile.PlayerProfile) error {
			if p.CompletedCampaignObjectives == nil {
				p.CompletedCampaignObjectives = map[string][]string{}
			}
			set := p.CompletedCampaignObjectives[key]
			for _, id := range body.ObjectiveIDs {
				if id == "" {
					continue
				}
				set = addToSortedSet(set, id)
			}
			p.CompletedCampaignObjectives[key] = set
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

	// Campaign catalog. Static data sourced from
	// server/internal/game/catalog/campaigns/*.json — drop new files there to
	// add campaigns; no code change is required for the data path. The
	// payload shape matches `Campaign` in client/src/game-portal/src/types/campaign.ts.
	mux.HandleFunc("/api/catalog/campaigns", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, map[string]any{
			"campaigns": game.ListCampaignDefs(),
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
