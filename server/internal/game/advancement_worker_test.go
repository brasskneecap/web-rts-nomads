package game

import "testing"

// workerAdvancementIDs is the full worker track, in purchase order. Kept in one
// place so the tests below acquire the whole track without pinning node order
// assumptions into each test body.
var workerAdvancementIDs = []string{
	"worker_extra_1",
	"worker_movespeed_1",
	"worker_goldcost_1",
	"worker_woodgather_1",
	"worker_movespeed_2",
	"worker_goldcost_2",
	"worker_extra_2",
	"worker_goldgather_1",
}

// TestWorkerAdvancements_FullTrackEffectsApplied acquires every worker node and
// verifies the effective worker def and ExtraStartingUnits reflect the sum of
// the node effects. Per the "no hardcoded tunables" rule, every expected value
// is derived from the catalog (base worker def + node effect amounts read back
// via GetAdvancementDef), never pinned as a literal.
func TestWorkerAdvancements_FullTrackEffectsApplied(t *testing.T) {
	base, ok := getUnitDef("worker")
	if !ok {
		t.Skip("worker not in unit catalog, skip")
	}

	// Derive expectations from the catalog node definitions.
	wantMoveSpeed := base.MoveSpeed
	wantGoldGather := base.GoldGatherAmount
	wantWoodGather := base.WoodGatherAmount
	wantGoldCost := base.ResourceCost["gold"]
	wantExtraWorkers := 0

	for _, id := range workerAdvancementIDs {
		node, found := GetAdvancementDef(id)
		if !found {
			t.Fatalf("worker advancement %q missing from catalog", id)
		}
		if node.UnitType != "worker" {
			t.Errorf("node %q unitType: want %q, got %q", id, "worker", node.UnitType)
		}
		for _, eff := range node.Effects {
			switch eff.Kind {
			case "unitStatAdd":
				switch eff.Stat {
				case "moveSpeed":
					wantMoveSpeed += float64(eff.Amount)
				case "goldGatherAmount":
					wantGoldGather += eff.Amount
				case "woodGatherAmount":
					wantWoodGather += eff.Amount
				case "goldCost":
					wantGoldCost += eff.Amount
				default:
					t.Errorf("node %q unexpected unitStatAdd stat %q", id, eff.Stat)
				}
			case "unitExtraStartingUnit":
				wantExtraWorkers += eff.Amount
			default:
				t.Errorf("node %q unexpected effect kind %q", id, eff.Kind)
			}
		}
	}

	player := &Player{AcquiredAdvancements: append([]string(nil), workerAdvancementIDs...)}
	applyAdvancementsToEffectiveDefsLocked(player)

	eff, found := player.EffectiveUnitDefs["worker"]
	if !found {
		t.Fatal("EffectiveUnitDefs: worker entry not created after applying advancements")
	}
	if eff.MoveSpeed != wantMoveSpeed {
		t.Errorf("effective worker moveSpeed: want %v, got %v", wantMoveSpeed, eff.MoveSpeed)
	}
	if eff.GoldGatherAmount != wantGoldGather {
		t.Errorf("effective worker goldGatherAmount: want %d, got %d", wantGoldGather, eff.GoldGatherAmount)
	}
	if eff.WoodGatherAmount != wantWoodGather {
		t.Errorf("effective worker woodGatherAmount: want %d, got %d", wantWoodGather, eff.WoodGatherAmount)
	}
	if eff.ResourceCost["gold"] != wantGoldCost {
		t.Errorf("effective worker gold cost: want %d, got %d", wantGoldCost, eff.ResourceCost["gold"])
	}
	if got := player.ExtraStartingUnits["worker"]; got != wantExtraWorkers {
		t.Errorf(`ExtraStartingUnits["worker"]: want %d, got %d`, wantExtraWorkers, got)
	}
}

// TestWorkerAdvancements_GatherUsesEffectiveDef verifies that the per-haul
// gather amount reflects the owner's woodGatherAmount/goldGatherAmount
// advancements (resolved through EffectiveUnitDefs), not just the raw catalog
// def. Regression guard for gatherAmountForUnitResourceLocked.
func TestWorkerAdvancements_GatherUsesEffectiveDef(t *testing.T) {
	base, ok := getUnitDef("worker")
	if !ok {
		t.Skip("worker not in unit catalog, skip")
	}
	woodNode, _ := GetAdvancementDef("worker_woodgather_1")
	goldNode, _ := GetAdvancementDef("worker_goldgather_1")
	wantWood := base.WoodGatherAmount + woodNode.Effects[0].Amount
	wantGold := base.GoldGatherAmount + goldNode.Effects[0].Amount

	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.EnsurePlayerWithUpgrades("p1", nil, nil, []string{
		"worker_woodgather_1", "worker_goldgather_1",
	}, nil)
	// A baseline player without the advancements should gather the catalog amounts.
	s.EnsurePlayer("baseline")

	s.mu.RLock()
	defer s.mu.RUnlock()

	if got := s.gatherAmountForUnitResourceLocked("worker", "p1", "wood"); got != wantWood {
		t.Errorf("upgraded worker wood gather: want %d, got %d", wantWood, got)
	}
	if got := s.gatherAmountForUnitResourceLocked("worker", "p1", "gold"); got != wantGold {
		t.Errorf("upgraded worker gold gather: want %d, got %d", wantGold, got)
	}
	if got := s.gatherAmountForUnitResourceLocked("worker", "baseline", "wood"); got != base.WoodGatherAmount {
		t.Errorf("baseline worker wood gather: want catalog %d, got %d", base.WoodGatherAmount, got)
	}
}

// TestWorkerAdvancements_GoldCostDoesNotMutateCatalog verifies the goldCost
// effect's map-copy guard: applying it must not mutate the shared catalog
// UnitDef's ResourceCost map (the working def is only a shallow struct copy, so
// the map is shared by reference until the applier replaces it).
func TestWorkerAdvancements_GoldCostDoesNotMutateCatalog(t *testing.T) {
	base, ok := getUnitDef("worker")
	if !ok {
		t.Skip("worker not in unit catalog, skip")
	}
	catalogGoldBefore := base.ResourceCost["gold"]

	player := &Player{AcquiredAdvancements: []string{"worker_goldcost_1"}}
	applyAdvancementsToEffectiveDefsLocked(player)

	// Re-read the catalog def: its gold cost must be unchanged.
	after, _ := getUnitDef("worker")
	if after.ResourceCost["gold"] != catalogGoldBefore {
		t.Errorf("catalog worker gold cost was mutated: want %d, got %d (map-copy guard failed)",
			catalogGoldBefore, after.ResourceCost["gold"])
	}

	// And the effective def should reflect the reduction.
	node, _ := GetAdvancementDef("worker_goldcost_1")
	wantEffectiveGold := catalogGoldBefore + node.Effects[0].Amount
	if eff := player.EffectiveUnitDefs["worker"]; eff.ResourceCost["gold"] != wantEffectiveGold {
		t.Errorf("effective worker gold cost: want %d, got %d", wantEffectiveGold, eff.ResourceCost["gold"])
	}
}
