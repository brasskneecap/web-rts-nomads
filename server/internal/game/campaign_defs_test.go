package game

import (
	"encoding/json"
	"strings"
	"testing"
)

// expectCampaignPanic runs fn and asserts it panics with a message containing
// `msgSubstring`. Mirrors expectPanic in objective_handlers_test.go but lives
// here so the test files do not have to share helpers across compilation
// units when one is later split out.
func expectCampaignPanic(t *testing.T, msgSubstring string, fn func()) {
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

// makeCampaignWithObjectives is a small helper to build a CampaignDef with
// the right shape for validateCampaignDef. The function takes raw configs
// (any type that marshals to JSON) so each test can drive both valid and
// invalid shapes without writing literal json.RawMessage.
func makeObjectiveDef(t *testing.T, id, typeKey string, cfg any) ObjectiveDef {
	t.Helper()
	rawCfg, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal cfg: %v", err)
	}
	return ObjectiveDef{
		ID:     id,
		Type:   typeKey,
		Config: rawCfg,
	}
}

// TestValidateCampaignDef_AcceptsValidShape verifies that a campaign with a
// mix of registered objective types loads cleanly and that parseAndValidate
// has populated the typed config on every objective.
func TestValidateCampaignDef_AcceptsValidShape(t *testing.T) {
	prereq := "lvl_01"
	def := CampaignDef{
		ID:          "forest",
		DisplayName: "Forest",
		Levels: []CampaignLevelDef{
			{
				ID:          "lvl_01",
				DisplayName: "Forest 1",
				MapID:       "exploration",
				Objectives: []ObjectiveDef{
					makeObjectiveDef(t, "clear_t1", "kill_camps", killCampsConfig{CampTier: 1, Count: 3}),
					makeObjectiveDef(t, "win_waves", "survive_waves", surviveWavesConfig{WavesToSurvive: 3}),
				},
			},
			{
				ID:                  "lvl_02",
				DisplayName:         "Forest 2",
				MapID:               "exploration2",
				PrerequisiteLevelID: &prereq,
				Objectives: []ObjectiveDef{
					makeObjectiveDef(t, "elite_squad", "rank_units", rankUnitsConfig{Rank: "bronze", Count: 5}),
				},
			},
		},
	}
	got := validateCampaignDef("forest.json", def)

	if len(got.Levels) != 2 {
		t.Fatalf("expected 2 levels, got %d", len(got.Levels))
	}
	for i, lvl := range got.Levels {
		if lvl.Objectives == nil {
			t.Errorf("level %d: Objectives should be non-nil after validation", i)
		}
		for j, obj := range lvl.Objectives {
			if obj.ParsedConfig() == nil {
				t.Errorf("level %d objective %d (%s): ParsedConfig should be set after validation", i, j, obj.ID)
			}
			if obj.Scope != ObjectiveScopeTeam {
				t.Errorf("level %d objective %d (%s): default scope should be team, got %q", i, j, obj.ID, obj.Scope)
			}
		}
	}
}

// TestValidateCampaignDef_EmptyObjectivesSliceNormalised verifies the nil →
// [] normalisation so the wire format never contains `null` for an empty
// objectives slice. Spec invariant: clients can `range` over the slice
// without a nil check.
func TestValidateCampaignDef_EmptyObjectivesSliceNormalised(t *testing.T) {
	def := CampaignDef{
		ID:          "forest",
		DisplayName: "Forest",
		Levels: []CampaignLevelDef{
			{ID: "lvl_01", DisplayName: "Forest 1", MapID: "exploration"},
		},
	}
	got := validateCampaignDef("forest.json", def)

	if got.Levels[0].Objectives == nil {
		t.Error("nil Objectives slice should be normalised to empty slice")
	}
	if len(got.Levels[0].Objectives) != 0 {
		t.Errorf("Objectives should be empty, got %v", got.Levels[0].Objectives)
	}
}

// TestValidateCampaignDef_RejectsDuplicateObjectiveID covers the
// per-level duplicate check. The same objective id is fine across two
// different levels (different scope), but a single level cannot have two
// objectives with the same id — the in-match snapshot keys on id.
func TestValidateCampaignDef_RejectsDuplicateObjectiveID(t *testing.T) {
	def := CampaignDef{
		ID:          "forest",
		DisplayName: "Forest",
		Levels: []CampaignLevelDef{
			{
				ID:          "lvl_01",
				DisplayName: "Forest 1",
				MapID:       "exploration",
				Objectives: []ObjectiveDef{
					makeObjectiveDef(t, "the_objective", "survive_waves", surviveWavesConfig{WavesToSurvive: 3}),
					makeObjectiveDef(t, "the_objective", "kill_camps", killCampsConfig{Count: 1}),
				},
			},
		},
	}
	expectCampaignPanic(t, `duplicate objective id "the_objective"`, func() {
		validateCampaignDef("forest.json", def)
	})
}

// TestValidateCampaignDef_BubblesUpObjectiveValidatorPanic verifies that the
// per-objective handler panic (from kill_camps requiring count > 0) propagates
// up cleanly with the file + level + objective context attached. The
// dispatcher does not swallow registry panics.
func TestValidateCampaignDef_BubblesUpObjectiveValidatorPanic(t *testing.T) {
	def := CampaignDef{
		ID:          "forest",
		DisplayName: "Forest",
		Levels: []CampaignLevelDef{
			{
				ID:          "lvl_01",
				DisplayName: "Forest 1",
				MapID:       "exploration",
				Objectives: []ObjectiveDef{
					makeObjectiveDef(t, "bad_obj", "kill_camps", killCampsConfig{Count: 0}),
				},
			},
		},
	}
	expectCampaignPanic(t, `level lvl_01: objective bad_obj: kill_camps requires count > 0`, func() {
		validateCampaignDef("forest.json", def)
	})
}

// TestValidateCampaignDef_RejectsUnknownObjectiveType verifies that an
// objective referencing a non-registered type fails the load. The error
// message must surface the file path so an author can fix it.
func TestValidateCampaignDef_RejectsUnknownObjectiveType(t *testing.T) {
	def := CampaignDef{
		ID:          "forest",
		DisplayName: "Forest",
		Levels: []CampaignLevelDef{
			{
				ID:          "lvl_01",
				DisplayName: "Forest 1",
				MapID:       "exploration",
				Objectives: []ObjectiveDef{
					{
						ID:     "fly",
						Type:   "fly_to_moon",
						Config: json.RawMessage(`{}`),
					},
				},
			},
		},
	}
	expectCampaignPanic(t, "unknown type fly_to_moon", func() {
		validateCampaignDef("forest.json", def)
	})
}

// TestValidateCampaignDef_PrereqValidationStillWorks regression test: the
// section-5 refactor moved validation into validateCampaignDef. Verify that
// prereq resolution (originally inline in loadCampaignDefs) still rejects
// dangling references.
func TestValidateCampaignDef_PrereqValidationStillWorks(t *testing.T) {
	bogus := "missing_level"
	def := CampaignDef{
		ID:          "forest",
		DisplayName: "Forest",
		Levels: []CampaignLevelDef{
			{
				ID:                  "lvl_01",
				DisplayName:         "Forest 1",
				MapID:               "exploration",
				PrerequisiteLevelID: &bogus,
			},
		},
	}
	expectCampaignPanic(t, `references unknown prerequisite "missing_level"`, func() {
		validateCampaignDef("forest.json", def)
	})
}

// TestValidateCampaignDef_RealForestCatalogStillLoads is a smoke test that
// the existing embedded forest.json + swamp.json catalog still validates
// after Section 5's refactor. If either file regresses (e.g. someone adds an
// invalid objective shape during the Section 6 migration), this test fires.
func TestValidateCampaignDef_RealForestCatalogStillLoads(t *testing.T) {
	defs := ListCampaignDefs()
	if len(defs) == 0 {
		t.Fatal("expected at least one campaign def in the embedded catalog")
	}
	var sawForest bool
	for _, d := range defs {
		if d.ID == "forest" {
			sawForest = true
			break
		}
	}
	if !sawForest {
		t.Errorf("expected Forest in catalog; got %v", defNames(defs))
	}
}

func defNames(defs []CampaignDef) []string {
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.ID)
	}
	return out
}
