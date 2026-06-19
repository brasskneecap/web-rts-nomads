package game

import (
	"encoding/json"
	"strings"
	"testing"

	"webrts/server/pkg/protocol"
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

// makeMapCampaignObjective builds one wire-shape objective for a campaign
// block. Accepts any cfg that marshals to JSON.
func makeMapCampaignObjective(t *testing.T, id, typeKey string, cfg any) protocol.MapCampaignObjective {
	t.Helper()
	rawCfg, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal cfg: %v", err)
	}
	return protocol.MapCampaignObjective{
		ID:     id,
		Type:   typeKey,
		Config: rawCfg,
	}
}

// TestConvertMapCampaignBlock_AcceptsValidShape verifies that a map's
// campaign block with a mix of registered objective types converts cleanly
// and that parseAndValidate has populated the typed config on every
// objective.
func TestConvertMapCampaignBlock_AcceptsValidShape(t *testing.T) {
	block := protocol.MapCampaignBlock{
		CampaignID:  "forest",
		LevelID:     "lvl_01",
		DisplayName: "Forest 1",
		Objectives: []protocol.MapCampaignObjective{
			makeMapCampaignObjective(t, "clear_t1", "kill_camps", killCampsConfig{CampTier: 1, Count: 3}),
			makeMapCampaignObjective(t, "win_waves", "survive_waves", surviveWavesConfig{WavesToSurvive: 3}),
		},
	}

	got := convertMapCampaignBlockToLevel("exploration", block)

	if got.ID != "lvl_01" || got.MapID != "exploration" {
		t.Fatalf("unexpected level identity: id=%s mapId=%s", got.ID, got.MapID)
	}
	if len(got.Objectives) != 2 {
		t.Fatalf("expected 2 objectives, got %d", len(got.Objectives))
	}
	for j, obj := range got.Objectives {
		if obj.ParsedConfig() == nil {
			t.Errorf("objective %d (%s): ParsedConfig should be set after validation", j, obj.ID)
		}
		if obj.Scope != ObjectiveScopeTeam {
			t.Errorf("objective %d (%s): default scope should be team, got %q", j, obj.ID, obj.Scope)
		}
	}
}

// TestConvertMapCampaignBlock_RejectsDuplicateObjectiveID covers the
// per-level duplicate check. A single level cannot have two objectives with
// the same id — the in-match snapshot keys on id.
func TestConvertMapCampaignBlock_RejectsDuplicateObjectiveID(t *testing.T) {
	block := protocol.MapCampaignBlock{
		CampaignID:  "forest",
		LevelID:     "lvl_01",
		DisplayName: "Forest 1",
		Objectives: []protocol.MapCampaignObjective{
			makeMapCampaignObjective(t, "the_objective", "survive_waves", surviveWavesConfig{WavesToSurvive: 3}),
			makeMapCampaignObjective(t, "the_objective", "kill_camps", killCampsConfig{Count: 1}),
		},
	}
	expectCampaignPanic(t, `duplicate objective id "the_objective"`, func() {
		_ = convertMapCampaignBlockToLevel("exploration", block)
	})
}

// TestConvertMapCampaignBlock_BubblesUpObjectiveValidatorPanic verifies that
// the per-objective handler panic (from kill_camps requiring count > 0)
// propagates up cleanly with the file + level + objective context attached.
func TestConvertMapCampaignBlock_BubblesUpObjectiveValidatorPanic(t *testing.T) {
	block := protocol.MapCampaignBlock{
		CampaignID:  "forest",
		LevelID:     "lvl_01",
		DisplayName: "Forest 1",
		Objectives: []protocol.MapCampaignObjective{
			makeMapCampaignObjective(t, "bad_obj", "kill_camps", killCampsConfig{Count: 0}),
		},
	}
	expectCampaignPanic(t, `level lvl_01: objective bad_obj: kill_camps requires count > 0`, func() {
		_ = convertMapCampaignBlockToLevel("exploration", block)
	})
}

// TestConvertMapCampaignBlock_RejectsUnknownObjectiveType verifies that an
// objective referencing a non-registered type fails the load. The error
// message must surface the file path so an author can fix it.
func TestConvertMapCampaignBlock_RejectsUnknownObjectiveType(t *testing.T) {
	block := protocol.MapCampaignBlock{
		CampaignID:  "forest",
		LevelID:     "lvl_01",
		DisplayName: "Forest 1",
		Objectives: []protocol.MapCampaignObjective{
			{ID: "fly", Type: "fly_to_moon", Config: json.RawMessage(`{}`)},
		},
	}
	expectCampaignPanic(t, "unknown type fly_to_moon", func() {
		_ = convertMapCampaignBlockToLevel("exploration", block)
	})
}

// TestSortAndValidateLevels_PrereqValidation verifies that prereq resolution
// (now part of post-discovery sorting) still rejects dangling references.
func TestSortAndValidateLevels_PrereqValidation(t *testing.T) {
	bogus := "missing_level"
	levels := []CampaignLevelDef{
		{
			ID:                  "lvl_01",
			DisplayName:         "Forest 1",
			MapID:               "exploration",
			PrerequisiteLevelID: &bogus,
		},
	}
	expectCampaignPanic(t, `references unknown prerequisite "missing_level"`, func() {
		_ = sortAndValidateLevels("forest", levels)
	})
}

// TestSortAndValidateLevels_RejectsSelfPrereq guards against a level listing
// itself as its own prerequisite — would otherwise lock the level out forever.
func TestSortAndValidateLevels_RejectsSelfPrereq(t *testing.T) {
	self := "lvl_01"
	levels := []CampaignLevelDef{
		{ID: "lvl_01", DisplayName: "Forest 1", MapID: "exploration", PrerequisiteLevelID: &self},
	}
	expectCampaignPanic(t, `lists itself as a prerequisite`, func() {
		_ = sortAndValidateLevels("forest", levels)
	})
}

// TestSortAndValidateLevels_RejectsDuplicateLevelID guards against two maps
// claiming the same level id within one campaign. Author error; would cause
// the in-match snapshot to be ambiguous.
func TestSortAndValidateLevels_RejectsDuplicateLevelID(t *testing.T) {
	levels := []CampaignLevelDef{
		{ID: "lvl_01", DisplayName: "Forest 1", MapID: "explorationA"},
		{ID: "lvl_01", DisplayName: "Forest 1 (dup)", MapID: "explorationB"},
	}
	expectCampaignPanic(t, `duplicate level id "lvl_01"`, func() {
		_ = sortAndValidateLevels("forest", levels)
	})
}

// TestValidateMapCampaignBlockForSave_RecoversFromPanic verifies that the
// save-path validator returns errors (not panics) so the HTTP layer can
// surface a 400 instead of taking the server down.
func TestValidateMapCampaignBlockForSave_RecoversFromPanic(t *testing.T) {
	block := &protocol.MapCampaignBlock{
		CampaignID:  "forest",
		LevelID:     "lvl_01",
		DisplayName: "Forest 1",
		Objectives: []protocol.MapCampaignObjective{
			makeMapCampaignObjective(t, "bad", "kill_camps", killCampsConfig{Count: 0}),
		},
	}
	err := ValidateMapCampaignBlockForSave("exploration", block)
	if err == nil {
		t.Fatal("expected error for invalid objective config; got nil")
	}
	if !strings.Contains(err.Error(), "kill_camps requires count > 0") {
		t.Errorf("expected panic message in error, got %q", err.Error())
	}
}

// TestValidateMapCampaignBlockForSave_RejectsOrphanedCampaignID guards against
// an editor save naming a campaign id that has no header file. Discovery would
// panic on this; the save path returns an error instead.
func TestValidateMapCampaignBlockForSave_RejectsOrphanedCampaignID(t *testing.T) {
	block := &protocol.MapCampaignBlock{
		CampaignID:  "this_does_not_exist",
		LevelID:     "lvl_01",
		DisplayName: "Lonely",
	}
	err := ValidateMapCampaignBlockForSave("exploration", block)
	if err == nil {
		t.Fatal("expected error for unknown campaignId; got nil")
	}
	if !strings.Contains(err.Error(), "this_does_not_exist") {
		t.Errorf("expected campaign id in error, got %q", err.Error())
	}
}

// anyCampaignMapEntry returns the (mapID, campaignID, levelID) of the first
// catalog map that owns a campaign level, so the cross-map save tests below
// don't hardcode which map owns which levelId (that moves around as the map
// catalog is edited).
func anyCampaignMapEntry(t *testing.T) (mapID, campaignID, levelID string) {
	t.Helper()
	for _, entry := range currentMapCatalogSnapshot() {
		if c := entry.Map.Campaign; c != nil && c.LevelID != "" {
			return entry.ID, c.CampaignID, c.LevelID
		}
	}
	t.Skip("no campaign map entries in catalog")
	return "", "", ""
}

// TestValidateMapCampaignBlockForSave_RejectsDuplicateLevelIDAcrossMaps is
// the regression guard for the editor footgun: an author saves a NEW map and
// types a levelId that another map in the same campaign already claims. The
// save must fail so the next /api/catalog/campaigns request doesn't panic
// the server in discovery's duplicate-id check.
//
// Discovers a real (mapID, campaignID, levelID) from the catalog, then saves a
// DIFFERENT map id with that levelId — which must be rejected and the error
// must name the conflicting level + the owning map.
func TestValidateMapCampaignBlockForSave_RejectsDuplicateLevelIDAcrossMaps(t *testing.T) {
	ownerMapID, campaignID, levelID := anyCampaignMapEntry(t)
	block := &protocol.MapCampaignBlock{
		CampaignID:  campaignID,
		LevelID:     levelID,
		DisplayName: "Stolen Slot",
		Objectives: []protocol.MapCampaignObjective{
			makeMapCampaignObjective(t, "win_waves", "survive_waves", surviveWavesConfig{WavesToSurvive: 3}),
		},
	}
	err := ValidateMapCampaignBlockForSave(ownerMapID+"_alt", block)
	if err == nil {
		t.Fatal("expected error for duplicate levelId across maps; got nil")
	}
	if !strings.Contains(err.Error(), levelID) || !strings.Contains(err.Error(), ownerMapID) {
		t.Errorf("expected error to name the conflicting level %q + owner map %q, got %q", levelID, ownerMapID, err.Error())
	}
}

// TestValidateMapCampaignBlockForSave_AllowsSameMapResave covers the
// re-edit path: saving the same map id with the same levelId must succeed.
// Without the entry.ID == mapID skip in the cross-map loop, every in-place
// edit would falsely trip the duplicate guard against its own previous save.
func TestValidateMapCampaignBlockForSave_AllowsSameMapResave(t *testing.T) {
	ownerMapID, campaignID, levelID := anyCampaignMapEntry(t)
	block := &protocol.MapCampaignBlock{
		CampaignID:  campaignID,
		LevelID:     levelID,
		DisplayName: "Edited",
		Objectives: []protocol.MapCampaignObjective{
			makeMapCampaignObjective(t, "win_waves", "survive_waves", surviveWavesConfig{WavesToSurvive: 5}),
		},
	}
	if err := ValidateMapCampaignBlockForSave(ownerMapID, block); err != nil {
		t.Fatalf("expected re-save of same map to succeed; got %v", err)
	}
}

// TestValidateMapCampaignBlockForSave_AllowsSameLevelIDAcrossCampaigns
// verifies that campaigns are independent ID namespaces. Reusing "forest_01"
// inside the Swamp campaign should succeed — the duplicate guard only fires
// when both campaignId AND levelId match.
func TestValidateMapCampaignBlockForSave_AllowsSameLevelIDAcrossCampaigns(t *testing.T) {
	block := &protocol.MapCampaignBlock{
		CampaignID:  "swamp",
		LevelID:     "forest_01",
		DisplayName: "Swamp 1",
		Objectives: []protocol.MapCampaignObjective{
			makeMapCampaignObjective(t, "win_waves", "survive_waves", surviveWavesConfig{WavesToSurvive: 3}),
		},
	}
	if err := ValidateMapCampaignBlockForSave("swamp_alt_map", block); err != nil {
		t.Fatalf("expected cross-campaign id reuse to succeed; got %v", err)
	}
}

// TestListCampaignDefs_RealForestCatalogStillLoads is a smoke test that the
// existing embedded forest.json + swamp.json catalog still validates after
// the map-editor-authors-campaign-maps refactor. After migration, Forest's
// levels come from exploration*.json's Campaign blocks.
func TestListCampaignDefs_RealForestCatalogStillLoads(t *testing.T) {
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
