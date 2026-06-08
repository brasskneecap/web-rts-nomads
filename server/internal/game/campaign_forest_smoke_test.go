package game

import "testing"

// TestForestCampaignFromMapsSmoke is the integration-level guard for
// map-editor-authors-campaign-maps: it verifies that Forest's three levels
// flow from the per-map `Campaign` blocks in exploration*.json into the
// CampaignDef tree in the right order and with their objectives attached.
// Catches regressions in catalog discovery, map-file inlining of campaign
// data, the validation pipeline, and the sortOrder ordering rule.
func TestForestCampaignFromMapsSmoke(t *testing.T) {
	for _, d := range ListCampaignDefs() {
		if d.ID != "forest" {
			continue
		}
		if len(d.Levels) != 3 {
			t.Fatalf("expected forest to have 3 levels, got %d", len(d.Levels))
		}
		want := []string{"forest_01", "forest_02", "forest_03"}
		for i, lvl := range d.Levels {
			if lvl.ID != want[i] {
				t.Errorf("level[%d]: want id=%s got %s", i, want[i], lvl.ID)
			}
			if len(lvl.Objectives) == 0 {
				t.Errorf("level %s: expected objectives, got none", lvl.ID)
			}
		}
		return
	}
	t.Fatal("forest campaign not in catalog")
}
