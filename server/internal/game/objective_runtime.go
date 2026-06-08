package game

import (
	"log"

	"webrts/server/pkg/protocol"
)

// SetCampaignLevelLocked installs the objectives authored on the named
// CampaignLevelDef onto this GameState. Called once per match, from
// Match.SetCampaignLevel, before any meaningful ticks have run.
//
// Tolerates:
//   - empty levelID  → no objectives installed (Custom Game / find-game).
//   - unknown levelID → logs a warning and leaves objectives empty (stale
//     client id should not block match start). Objective evaluation will
//     simply be a no-op for this match.
//
// Idempotent: re-calling with the same id rebuilds the slice from the
// current catalog. Production code calls this exactly once.
//
// Takes the lock (not "Locked" suffixed despite the name — the suffix is
// retained because every other state-mutating method on GameState follows
// the pattern, and the match-start caller treats this as part of the
// pre-loop setup. See AI_RULES.md for the *Locked convention.)
func (s *GameState) SetCampaignLevelLocked(levelID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.CampaignLevelID = levelID
	s.Objectives = nil

	if levelID == "" {
		return
	}

	def, ok := lookupCampaignLevelByID(levelID)
	if !ok {
		log.Printf("objective_runtime: campaign level %q not found in catalog; match runs without objectives", levelID)
		return
	}

	s.Objectives = make([]objectiveRuntime, 0, len(def.Objectives))
	for _, objDef := range def.Objectives {
		s.Objectives = append(s.Objectives, newObjectiveRuntime(objDef))
	}
}

// lookupCampaignLevelByID searches the loaded campaign catalog for a level
// whose ID matches. Recomputes the level tree from the current map catalog
// snapshot so editor saves are visible without a restart.
//
// Linear scan — campaigns and levels are both small (single digits) so a
// map index would be premature.
func lookupCampaignLevelByID(levelID string) (CampaignLevelDef, bool) {
	for _, c := range buildCampaignDefs() {
		for _, lvl := range c.Levels {
			if lvl.ID == levelID {
				return lvl, true
			}
		}
	}
	return CampaignLevelDef{}, false
}

// evaluateObjectivesLocked runs one tick of objective evaluation. Called from
// GameState.Update after all metric-bumping subsystems have run and before
// the snapshot/victory check.
//
// For team-scope objectives, builds a single team-aggregated MatchMetrics
// view and updates `runtime.TeamState`. For player-scope objectives,
// iterates every non-AI player and updates their entry in
// `runtime.PlayerStates`. The dispatcher (`EvaluateObjective`) enforces
// the sticky completion/failure invariant, so handlers may assume the
// state passed in is in-progress.
//
// No-op when `s.Objectives` is empty (Custom Game / find-game matches).
// Caller must hold s.mu (per `*Locked` convention).
func (s *GameState) evaluateObjectivesLocked() {
	if len(s.Objectives) == 0 {
		return
	}

	teamMetrics := s.computeTeamMetricsLocked()

	for i := range s.Objectives {
		runtime := &s.Objectives[i]
		switch runtime.Def.Scope {
		case ObjectiveScopeTeam:
			EvaluateObjective(s, runtime.Def, &teamMetrics, &runtime.TeamState)
		case ObjectiveScopePlayer:
			for playerID, player := range s.Players {
				if playerID == enemyPlayerID || playerID == neutralPlayerID {
					continue
				}
				state := runtime.ensurePlayerState(playerID)
				EvaluateObjective(s, runtime.Def, &player.Metrics, state)
				runtime.storePlayerState(playerID, *state)
			}
		}
	}
}

// buildVictorySnapshotForViewerLocked produces the per-viewer Victory
// payload for the snapshot. Returns nil when the match has no objectives
// installed (Custom Game / find-game), keeping the wire compact.
//
// Per-viewer because player-scope objectives carry the viewer's own state:
// two players in the same lobby see the same team-scope progress but their
// own personal numbers for player-scope objectives. Team-scope progress
// reads from TeamState; player-scope reads from PlayerStates[viewerID] or
// a freshly-initialised state if the viewer has never been evaluated
// (e.g. snapshot built before the first tick after the player joined).
//
// Pass viewerID="" for the broadcast/join snapshot path. Team-scope
// progress is still correct; player-scope shows initial state.
//
// Caller must hold s.mu (RLock or Lock).
func (s *GameState) buildVictorySnapshotForViewerLocked(viewerID string) *protocol.VictorySnapshot {
	if len(s.Objectives) == 0 {
		return nil
	}

	objectives := make([]protocol.ObjectiveSnapshot, 0, len(s.Objectives))
	for i := range s.Objectives {
		runtime := &s.Objectives[i]
		var state ObjectiveState
		switch runtime.Def.Scope {
		case ObjectiveScopeTeam:
			state = runtime.TeamState
		case ObjectiveScopePlayer:
			if ps, ok := runtime.PlayerStates[viewerID]; ok {
				state = ps
			} else {
				// Viewer has never been evaluated (or viewerID empty for
				// broadcast snapshot). Show initial state — handler's
				// Required is populated, Current is zero.
				state = NewObjectiveState(runtime.Def)
			}
		}
		objectives = append(objectives, protocol.ObjectiveSnapshot{
			ID:            runtime.Def.ID,
			Type:          runtime.Def.Type,
			Description:   runtime.Def.Description,
			Scope:         string(runtime.Def.Scope),
			Required:      runtime.Def.Required,
			Current:       state.Current,
			RequiredCount: state.Required,
			Completed:     state.Completed,
			Failed:        state.Failed,
		})
	}

	return &protocol.VictorySnapshot{
		Achieved:   s.victoryAchieved,
		Objectives: objectives,
	}
}

// toMetricsSnapshot is the value-type converter from game.MatchMetrics to
// protocol.MatchMetricsSnapshot. Defensive copies are NOT made — the
// snapshot is consumed and serialised by the broadcast hot path before any
// further tick mutates the maps. If snapshot consumers ever need to retain
// the value past the broadcast boundary, this should copy the maps.
func toMetricsSnapshot(m MatchMetrics) protocol.MatchMetricsSnapshot {
	return protocol.MatchMetricsSnapshot{
		TotalGoldEarned:          m.TotalGoldEarned,
		TotalWoodEarned:          m.TotalWoodEarned,
		TotalEnemiesKilled:       m.TotalEnemiesKilled,
		BuildingsBuilt:           m.BuildingsBuilt,
		BuildingsBuiltByType:     m.BuildingsBuiltByType,
		NeutralCampsKilled:       m.NeutralCampsKilled,
		NeutralCampsKilledByTier: m.NeutralCampsKilledByTier,
		UnitsTrained:             m.UnitsTrained,
		UnitsTrainedByType:       m.UnitsTrainedByType,
		UnitsByRank:              m.UnitsByRank,
		WavesCleared:             m.WavesCleared,
	}
}

// computeTeamMetricsLocked aggregates per-player metrics into a single
// team-wide MatchMetrics value. Aggregation rule per field:
//
//   - SUM: TotalGoldEarned, TotalWoodEarned, TotalEnemiesKilled,
//          BuildingsBuilt, BuildingsBuiltByType, UnitsTrained,
//          UnitsTrainedByType, UnitsByRank. Each player contributes only
//          their own (no double-counting).
//   - MAX: NeutralCampsKilled, NeutralCampsKilledByTier, WavesCleared.
//          The event hook credits every non-AI player on team-wide events
//          (see recordCampClearedMetricLocked / recordWaveClearedMetricLocked),
//          so each player's value already equals the team's. Taking the max
//          (rather than summing) yields the correct team count.
//
// Skips AI player entries (enemyPlayerID, neutralPlayerID).
//
// Caller must hold s.mu.
func (s *GameState) computeTeamMetricsLocked() MatchMetrics {
	team := NewMatchMetrics()
	for id, p := range s.Players {
		if id == enemyPlayerID || id == neutralPlayerID {
			continue
		}
		m := p.Metrics

		team.TotalGoldEarned += m.TotalGoldEarned
		team.TotalWoodEarned += m.TotalWoodEarned
		team.TotalEnemiesKilled += m.TotalEnemiesKilled
		team.BuildingsBuilt += m.BuildingsBuilt
		team.UnitsTrained += m.UnitsTrained
		for k, v := range m.BuildingsBuiltByType {
			team.BuildingsBuiltByType[k] += v
		}
		for k, v := range m.UnitsTrainedByType {
			team.UnitsTrainedByType[k] += v
		}
		for k, v := range m.UnitsByRank {
			team.UnitsByRank[k] += v
		}

		if m.NeutralCampsKilled > team.NeutralCampsKilled {
			team.NeutralCampsKilled = m.NeutralCampsKilled
		}
		for k, v := range m.NeutralCampsKilledByTier {
			if v > team.NeutralCampsKilledByTier[k] {
				team.NeutralCampsKilledByTier[k] = v
			}
		}
		if m.WavesCleared > team.WavesCleared {
			team.WavesCleared = m.WavesCleared
		}
	}
	return team
}
