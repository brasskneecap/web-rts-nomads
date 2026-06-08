package game

import (
	"encoding/json"
	"strings"
	"testing"
)

// loadObjective is a test helper that exercises the same load path the catalog
// loader will use in §5: marshal a typed config, build an ObjectiveDef, run
// parseAndValidateObjectiveDef, and initialise its ObjectiveState. Returns
// the def + state ready to feed to EvaluateObjective.
func loadObjective(t *testing.T, id, typeKey, scope string, required bool, cfg any) (ObjectiveDef, ObjectiveState) {
	t.Helper()
	rawCfg, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal cfg: %v", err)
	}
	def := parseAndValidateObjectiveDef("test.json", "test_level", ObjectiveDef{
		ID:       id,
		Type:     typeKey,
		Scope:    ObjectiveScope(scope),
		Required: required,
		Config:   rawCfg,
	})
	return def, NewObjectiveState(def)
}

// expectPanic runs fn and verifies it panics with a message containing the
// given substring. Useful for validation tests where every panic includes the
// file + level + objective id in the message.
func expectPanic(t *testing.T, msgSubstring string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic containing %q, got none", msgSubstring)
		}
		got, _ := r.(string)
		if !strings.Contains(got, msgSubstring) {
			t.Fatalf("expected panic containing %q, got %q", msgSubstring, got)
		}
	}()
	fn()
}

// =============================================================================
// kill_camps
// =============================================================================

func TestKillCampsHandler_AnyTier(t *testing.T) {
	def, state := loadObjective(t, "obj", "kill_camps", "team", false, killCampsConfig{Count: 3})

	metrics := NewMatchMetrics()
	EvaluateObjective(nil, def, &metrics, &state)
	if state.Completed {
		t.Fatal("should not complete with zero camps killed")
	}

	// 1 of tier 1, 2 of tier 2 — sum is 3, completes (any-tier).
	metrics.RecordCampKilled(1)
	metrics.RecordCampKilled(2)
	metrics.RecordCampKilled(2)
	EvaluateObjective(nil, def, &metrics, &state)
	if !state.Completed {
		t.Fatalf("any-tier kill_camps should complete at total >= 3, got %d", state.Current)
	}
	if state.Current != 3 {
		t.Errorf("Current: want 3, got %d", state.Current)
	}
}

func TestKillCampsHandler_TierFiltered(t *testing.T) {
	def, state := loadObjective(t, "obj", "kill_camps", "team", false, killCampsConfig{CampTier: 1, Count: 3})

	metrics := NewMatchMetrics()
	metrics.RecordCampKilled(1)
	metrics.RecordCampKilled(1)
	metrics.RecordCampKilled(2)
	metrics.RecordCampKilled(2)
	EvaluateObjective(nil, def, &metrics, &state)
	if state.Completed {
		t.Fatalf("tier-1 kill_camps should not complete with only 2 tier-1, got %d", state.Current)
	}
	metrics.RecordCampKilled(1)
	EvaluateObjective(nil, def, &metrics, &state)
	if !state.Completed {
		t.Fatalf("tier-1 kill_camps should complete at 3 tier-1, got %d", state.Current)
	}
}

func TestKillCampsHandler_ValidateRejectsCountZero(t *testing.T) {
	expectPanic(t, "kill_camps requires count > 0", func() {
		loadObjective(t, "obj", "kill_camps", "team", false, killCampsConfig{Count: 0})
	})
}

// =============================================================================
// build_buildings
// =============================================================================

func TestBuildBuildingsHandler_CompletesAtThreshold(t *testing.T) {
	def, state := loadObjective(t, "obj", "build_buildings", "player", false, buildBuildingsConfig{BuildingType: "barracks", Count: 2})

	metrics := NewMatchMetrics()
	metrics.RecordBuildingBuilt("barracks")
	EvaluateObjective(nil, def, &metrics, &state)
	if state.Completed {
		t.Fatalf("should not complete at 1/2, got %d", state.Current)
	}
	metrics.RecordBuildingBuilt("barracks")
	EvaluateObjective(nil, def, &metrics, &state)
	if !state.Completed {
		t.Fatalf("should complete at 2/2, got %d", state.Current)
	}
}

func TestBuildBuildingsHandler_ValidateRejectsUnknownType(t *testing.T) {
	expectPanic(t, "not in the building catalog", func() {
		loadObjective(t, "obj", "build_buildings", "team", false, buildBuildingsConfig{BuildingType: "wizard_tower", Count: 1})
	})
}

func TestBuildBuildingsHandler_ValidateRejectsEmptyType(t *testing.T) {
	expectPanic(t, "non-empty buildingType", func() {
		loadObjective(t, "obj", "build_buildings", "team", false, buildBuildingsConfig{BuildingType: "", Count: 1})
	})
}

// =============================================================================
// collect_resource
// =============================================================================

func TestCollectResourceHandler_GoldThreshold(t *testing.T) {
	def, state := loadObjective(t, "obj", "collect_resource", "team", false, collectResourceConfig{Resource: "gold", Amount: 500})

	metrics := NewMatchMetrics()
	metrics.RecordGoldEarned(400)
	EvaluateObjective(nil, def, &metrics, &state)
	if state.Completed {
		t.Fatalf("400 < 500 should not complete")
	}
	metrics.RecordGoldEarned(100)
	EvaluateObjective(nil, def, &metrics, &state)
	if !state.Completed {
		t.Fatalf("500 should complete, got Current=%d", state.Current)
	}
}

func TestCollectResourceHandler_WoodIgnoresGoldDeposits(t *testing.T) {
	def, state := loadObjective(t, "obj", "collect_resource", "team", false, collectResourceConfig{Resource: "wood", Amount: 100})

	metrics := NewMatchMetrics()
	metrics.RecordGoldEarned(9999)
	EvaluateObjective(nil, def, &metrics, &state)
	if state.Completed || state.Current != 0 {
		t.Fatalf("wood objective should ignore gold; got Completed=%v Current=%d", state.Completed, state.Current)
	}
	metrics.RecordWoodEarned(100)
	EvaluateObjective(nil, def, &metrics, &state)
	if !state.Completed {
		t.Fatalf("wood at threshold should complete")
	}
}

func TestCollectResourceHandler_ValidateRejectsUnknownResource(t *testing.T) {
	expectPanic(t, `must be "gold" or "wood"`, func() {
		loadObjective(t, "obj", "collect_resource", "team", false, collectResourceConfig{Resource: "crystal", Amount: 10})
	})
}

// =============================================================================
// kill_camps_before_wave — the only objective with a failure outcome.
// =============================================================================

func TestKillCampsBeforeWaveHandler_CompleteBeforeDeadline(t *testing.T) {
	def, state := loadObjective(t, "obj", "kill_camps_before_wave", "team", false,
		killCampsBeforeWaveConfig{Count: 3, BeforeWave: 5})

	metrics := NewMatchMetrics()
	metrics.RecordCampKilled(1)
	metrics.RecordCampKilled(1)
	metrics.RecordCampKilled(1)

	s := &GameState{}
	s.WaveManager.CurrentWave = 3
	s.WaveManager.State = "active"
	EvaluateObjective(s, def, &metrics, &state)

	if !state.Completed {
		t.Fatalf("should complete before deadline wave")
	}
	if state.Failed {
		t.Fatalf("completed objective should not also be failed")
	}

	// Deadline arrives later — completion sticks; the active-wave check
	// must not flip Failed.
	s.WaveManager.CurrentWave = 5
	EvaluateObjective(s, def, &metrics, &state)
	if state.Failed {
		t.Fatalf("completed objective should not be retroactively failed by deadline wave")
	}
}

func TestKillCampsBeforeWaveHandler_FailsAtDeadline(t *testing.T) {
	def, state := loadObjective(t, "obj", "kill_camps_before_wave", "team", false,
		killCampsBeforeWaveConfig{Count: 3, BeforeWave: 5})

	metrics := NewMatchMetrics()
	metrics.RecordCampKilled(1)
	metrics.RecordCampKilled(1)
	// Only 2 of 3 cleared when the deadline begins.

	s := &GameState{}
	s.WaveManager.CurrentWave = 5
	s.WaveManager.State = "active"
	EvaluateObjective(s, def, &metrics, &state)

	if !state.Failed {
		t.Fatalf("should have failed at deadline; state=%+v", state)
	}
	if state.Completed {
		t.Fatalf("failed objective should not also be completed")
	}

	// Later kills do NOT unfailed.
	metrics.RecordCampKilled(1)
	EvaluateObjective(s, def, &metrics, &state)
	if state.Completed {
		t.Fatalf("failed objective must stay failed even when threshold met after deadline")
	}
}

func TestKillCampsBeforeWaveHandler_PrepPhaseDoesNotFail(t *testing.T) {
	// The wave's "prep" phase is the warm-up before a wave starts. We only
	// fail when the wave is actively running, so a player who finishes the
	// camps during the deadline wave's prep should still complete.
	def, state := loadObjective(t, "obj", "kill_camps_before_wave", "team", false,
		killCampsBeforeWaveConfig{Count: 3, BeforeWave: 5})

	metrics := NewMatchMetrics()
	metrics.RecordCampKilled(1)
	metrics.RecordCampKilled(1)

	s := &GameState{}
	s.WaveManager.CurrentWave = 5
	s.WaveManager.State = "prep"
	EvaluateObjective(s, def, &metrics, &state)
	if state.Failed {
		t.Fatalf("prep phase should not fail the objective; only \"active\" wave does")
	}
}

// =============================================================================
// rank_units
// =============================================================================

func TestRankUnitsHandler_CompletesAtThreshold(t *testing.T) {
	def, state := loadObjective(t, "obj", "rank_units", "player", false, rankUnitsConfig{Rank: "bronze", Count: 5})

	metrics := NewMatchMetrics()
	metrics.UnitsByRank["bronze"] = 5
	EvaluateObjective(nil, def, &metrics, &state)
	if !state.Completed {
		t.Fatalf("rank_units bronze>=5 should complete; got %d", state.Current)
	}
}

func TestRankUnitsHandler_StickyOnceComplete(t *testing.T) {
	def, state := loadObjective(t, "obj", "rank_units", "player", false, rankUnitsConfig{Rank: "bronze", Count: 5})

	metrics := NewMatchMetrics()
	metrics.UnitsByRank["bronze"] = 5
	EvaluateObjective(nil, def, &metrics, &state)
	if !state.Completed {
		t.Fatalf("precondition: should complete")
	}
	// All units die.
	metrics.UnitsByRank["bronze"] = 0
	EvaluateObjective(nil, def, &metrics, &state)
	if !state.Completed {
		t.Fatal("rank_units must stay completed when units later die")
	}
	// Current is intentionally NOT decremented after completion — it freezes
	// at the value that triggered completion. The dispatcher short-circuits
	// further evaluation on Completed.
	if state.Current != 5 {
		t.Errorf("Current should freeze at completion: want 5, got %d", state.Current)
	}
}

func TestRankUnitsHandler_ValidateRejectsBase(t *testing.T) {
	expectPanic(t, `rank_units rank must be "bronze", "silver", or "gold"`, func() {
		loadObjective(t, "obj", "rank_units", "team", false, rankUnitsConfig{Rank: "base", Count: 1})
	})
}

// =============================================================================
// survive_waves
// =============================================================================

func TestSurviveWavesHandler_CompletesAtThreshold(t *testing.T) {
	def, state := loadObjective(t, "obj", "survive_waves", "team", true, surviveWavesConfig{WavesToSurvive: 3})

	metrics := NewMatchMetrics()
	for i := 0; i < 2; i++ {
		metrics.RecordWaveCleared()
	}
	EvaluateObjective(nil, def, &metrics, &state)
	if state.Completed {
		t.Fatalf("should not complete at 2/3, got %d", state.Current)
	}
	metrics.RecordWaveCleared()
	EvaluateObjective(nil, def, &metrics, &state)
	if !state.Completed {
		t.Fatalf("should complete at 3/3, got %d", state.Current)
	}
}

func TestSurviveWavesHandler_ValidateRejectsZero(t *testing.T) {
	expectPanic(t, "survive_waves requires wavesToSurvive > 0", func() {
		loadObjective(t, "obj", "survive_waves", "team", false, surviveWavesConfig{WavesToSurvive: 0})
	})
}

// =============================================================================
// Cross-cutting: registry surface area.
// =============================================================================

// TestRegistry_ExposesAllSixHandlers pins the "shipped types" list. A future
// add or removal needs to update this test consciously.
func TestRegistry_ExposesAllSixHandlers(t *testing.T) {
	expected := []string{
		"build_buildings",
		"collect_resource",
		"kill_camps",
		"kill_camps_before_wave",
		"rank_units",
		"survive_waves",
	}
	got := ListObjectiveTypes()
	if len(got) != len(expected) {
		t.Fatalf("ListObjectiveTypes: want %d types %v, got %d types %v", len(expected), expected, len(got), got)
	}
	for i, want := range expected {
		if got[i] != want {
			t.Errorf("ListObjectiveTypes()[%d]: want %q, got %q", i, want, got[i])
		}
	}
}

func TestParseAndValidate_UnknownTypeRejected(t *testing.T) {
	expectPanic(t, "unknown type fly_to_moon", func() {
		parseAndValidateObjectiveDef("test.json", "test_level", ObjectiveDef{
			ID:     "obj",
			Type:   "fly_to_moon",
			Config: json.RawMessage(`{}`),
		})
	})
}

func TestParseAndValidate_InvalidScopeRejected(t *testing.T) {
	expectPanic(t, "invalid scope Team", func() {
		parseAndValidateObjectiveDef("test.json", "test_level", ObjectiveDef{
			ID:     "obj",
			Type:   "survive_waves",
			Scope:  "Team", // capital T — must be exact "team"
			Config: json.RawMessage(`{"wavesToSurvive":3}`),
		})
	})
}

func TestParseAndValidate_DefaultScopeIsTeam(t *testing.T) {
	def := parseAndValidateObjectiveDef("test.json", "test_level", ObjectiveDef{
		ID:     "obj",
		Type:   "survive_waves",
		Config: json.RawMessage(`{"wavesToSurvive":3}`),
	})
	if def.Scope != ObjectiveScopeTeam {
		t.Errorf("missing scope should default to team, got %q", def.Scope)
	}
}

// TestEvaluate_StickyCompletionSurvivesMetricDrop guards the dispatcher's
// short-circuit — once Completed flips true, subsequent evaluations are
// no-ops regardless of metric movement.
func TestEvaluate_StickyCompletionSurvivesMetricDrop(t *testing.T) {
	def, state := loadObjective(t, "obj", "kill_camps", "team", false, killCampsConfig{Count: 1})

	metrics := NewMatchMetrics()
	metrics.RecordCampKilled(1)
	EvaluateObjective(nil, def, &metrics, &state)
	if !state.Completed {
		t.Fatal("precondition: should complete")
	}

	// Corrupt the metric backwards (impossible in production, but the
	// invariant matters). The dispatcher's short-circuit means we don't
	// even call the handler.
	metrics.NeutralCampsKilled = 0
	EvaluateObjective(nil, def, &metrics, &state)
	if !state.Completed {
		t.Fatal("Completed must remain sticky")
	}
}
