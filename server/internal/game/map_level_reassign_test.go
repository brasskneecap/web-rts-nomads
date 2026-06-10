package game

import (
	"sort"
	"testing"

	"webrts/server/pkg/protocol"
)

// snapshotRuntimeMaps captures the package-level runtime overlay and restores
// it after the test, so a test that writes/reassigns maps doesn't leak overlay
// state into sibling tests (the embedded catalog is the baseline; tests should
// see it pristine).
func snapshotRuntimeMaps(t *testing.T) {
	t.Helper()
	runtimeMapsMu.Lock()
	saved := make(map[string]MapCatalogEntry, len(runtimeMaps))
	for k, v := range runtimeMaps {
		saved[k] = v
	}
	runtimeMapsMu.Unlock()
	t.Cleanup(func() {
		runtimeMapsMu.Lock()
		runtimeMaps = saved
		runtimeMapsMu.Unlock()
	})
}

// firstCampaignMap returns a deterministically-chosen campaign-tagged map from
// the current catalog, to use as the existing owner of a level a test will try
// to claim. Skips the test if the catalog ships no campaign maps (so this test
// file doesn't hard-depend on specific authored content).
func firstCampaignMap(t *testing.T) MapCatalogEntry {
	t.Helper()
	snap := currentMapCatalogSnapshot()
	sort.Slice(snap, func(i, j int) bool { return snap[i].ID < snap[j].ID })
	for _, e := range snap {
		if e.Map.Campaign != nil && e.Map.Campaign.LevelID != "" {
			return e
		}
	}
	t.Skip("no campaign-tagged map in catalog to test level reassignment against")
	return MapCatalogEntry{}
}

// colliding builds a new map entry that claims the same (campaignId, levelId)
// as `owner`, carrying the owner's level definition so the block validates.
func colliding(id string, owner MapCatalogEntry) MapCatalogEntry {
	src := owner.Map.Campaign
	block := &protocol.MapCampaignBlock{
		CampaignID:          src.CampaignID,
		LevelID:             src.LevelID,
		DisplayName:         src.DisplayName,
		PrerequisiteLevelID: src.PrerequisiteLevelID,
		Description:         src.Description,
		SortOrder:           src.SortOrder,
		Objectives:          src.Objectives,
	}
	return MapCatalogEntry{
		ID:   id,
		Name: id,
		Map:  protocol.MapConfig{ID: id, Name: id, Campaign: block},
	}
}

// levelOwnerMapID returns the MapID that currently backs (campaignID, levelID)
// in the synthesized campaign tree, or "" if the level isn't present.
func levelOwnerMapID(t *testing.T, campaignID, levelID string) string {
	t.Helper()
	tree := buildCampaignDefs()
	camp, ok := tree[campaignID]
	if !ok {
		return ""
	}
	count := 0
	owner := ""
	for _, lvl := range camp.Levels {
		if lvl.ID == levelID {
			count++
			owner = lvl.MapID
		}
	}
	if count > 1 {
		t.Fatalf("level %q appears %d times in campaign %q — duplicate ownership", levelID, count, campaignID)
	}
	return owner
}

func TestSaveMap_ConflictRejectedWithoutReassign(t *testing.T) {
	snapshotRuntimeMaps(t)
	t.Setenv("MAP_CATALOG_DIR", t.TempDir())

	owner := firstCampaignMap(t)
	block := owner.Map.Campaign
	entry := colliding("test_reassign_reject_map", owner)

	reassignedFrom, conflict, err := SaveMapCatalogEntryWithOptions(entry, SaveMapOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reassignedFrom != "" {
		t.Fatalf("reassignedFrom = %q, want empty", reassignedFrom)
	}
	if conflict == nil {
		t.Fatal("expected a level conflict, got nil")
	}
	if conflict.OwnerMapID != owner.ID || conflict.CampaignID != block.CampaignID || conflict.LevelID != block.LevelID {
		t.Fatalf("conflict = %+v, want owner=%s campaign=%s level=%s",
			conflict, owner.ID, block.CampaignID, block.LevelID)
	}
	// Nothing should have been written: the new map is absent and the level
	// still resolves to the original owner.
	if _, ok := GetMapCatalogEntryByID(entry.ID); ok {
		t.Fatal("rejected save still wrote the new map to the overlay")
	}
	if got := levelOwnerMapID(t, block.CampaignID, block.LevelID); got != owner.ID {
		t.Fatalf("level owner = %q after rejected save, want unchanged %q", got, owner.ID)
	}
}

func TestSaveMap_ReassignTransfersLevel(t *testing.T) {
	snapshotRuntimeMaps(t)
	t.Setenv("MAP_CATALOG_DIR", t.TempDir())

	owner := firstCampaignMap(t)
	block := owner.Map.Campaign
	entry := colliding("test_reassign_target_map", owner)

	reassignedFrom, conflict, err := SaveMapCatalogEntryWithOptions(entry, SaveMapOptions{ReassignLevel: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conflict != nil {
		t.Fatalf("expected no conflict on reassign, got %+v", conflict)
	}
	if reassignedFrom != owner.ID {
		t.Fatalf("reassignedFrom = %q, want %q", reassignedFrom, owner.ID)
	}

	// The old owner's campaign block is cleared...
	clearedOwner, ok := GetMapCatalogEntryByID(owner.ID)
	if !ok {
		t.Fatalf("old owner %q vanished after reassign", owner.ID)
	}
	if clearedOwner.Map.Campaign != nil {
		t.Fatalf("old owner %q still has a campaign block after reassign", owner.ID)
	}
	// ...and the level now resolves to the new map, exactly once (no duplicate,
	// which would otherwise panic buildCampaignDefs via levelOwnerMapID).
	if got := levelOwnerMapID(t, block.CampaignID, block.LevelID); got != entry.ID {
		t.Fatalf("level owner = %q after reassign, want %q", got, entry.ID)
	}
}

func TestSaveMap_NoConflictWritesNormally(t *testing.T) {
	snapshotRuntimeMaps(t)
	t.Setenv("MAP_CATALOG_DIR", t.TempDir())

	owner := firstCampaignMap(t)
	// Same campaign, brand-new level id => no conflict.
	entry := colliding("test_reassign_newlevel_map", owner)
	entry.Map.Campaign.LevelID = "test_reassign_unique_level"
	entry.Map.Campaign.PrerequisiteLevelID = nil

	reassignedFrom, conflict, err := SaveMapCatalogEntryWithOptions(entry, SaveMapOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conflict != nil {
		t.Fatalf("unexpected conflict for a fresh level id: %+v", conflict)
	}
	if reassignedFrom != "" {
		t.Fatalf("reassignedFrom = %q, want empty (no reassign)", reassignedFrom)
	}
	if _, ok := GetMapCatalogEntryByID(entry.ID); !ok {
		t.Fatal("non-conflicting campaign map was not written to the overlay")
	}
}

func TestSaveMapCatalogEntry_BackwardCompatRejectsConflict(t *testing.T) {
	snapshotRuntimeMaps(t)
	t.Setenv("MAP_CATALOG_DIR", t.TempDir())

	owner := firstCampaignMap(t)
	entry := colliding("test_reassign_compat_map", owner)

	err := SaveMapCatalogEntry(entry)
	if err == nil {
		t.Fatal("legacy SaveMapCatalogEntry should still reject a level conflict")
	}
	if !IsMapSaveValidationError(err) {
		t.Fatalf("conflict error should be classified as a validation error, got %v", err)
	}
	if _, ok := GetMapCatalogEntryByID(entry.ID); ok {
		t.Fatal("rejected legacy save still wrote the map")
	}
}

func TestSaveMap_ValidationErrorIsTyped(t *testing.T) {
	snapshotRuntimeMaps(t)
	t.Setenv("MAP_CATALOG_DIR", t.TempDir())

	entry := MapCatalogEntry{
		ID:   "test_reassign_badcampaign_map",
		Name: "bad",
		Map: protocol.MapConfig{
			ID: "test_reassign_badcampaign_map",
			Campaign: &protocol.MapCampaignBlock{
				CampaignID:  "no_such_campaign_xyz",
				LevelID:     "lvl",
				DisplayName: "Lvl",
			},
		},
	}

	_, _, err := SaveMapCatalogEntryWithOptions(entry, SaveMapOptions{})
	if err == nil {
		t.Fatal("expected a validation error for an unknown campaign id")
	}
	if !IsMapSaveValidationError(err) {
		t.Fatalf("unknown-campaign error should be a validation error, got %v", err)
	}
}
