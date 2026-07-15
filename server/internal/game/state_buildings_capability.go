package game

import "webrts/server/pkg/protocol"

// playerOwnsBuiltCapabilityLocked reports whether playerID owns at least one
// fully-built (not under-construction), visible building whose capability list
// includes capability. Generalizes playerHasMarketplaceLocked. Must be called
// under s.mu.
func (s *GameState) playerOwnsBuiltCapabilityLocked(playerID, capability string) bool {
	return s.findBuiltCapabilityLocked(playerID, capability, nil) != nil
}

// playerOwnsCraftingBuildingForLocked reports whether playerID owns a built
// crafting building that will make itemID.
//
// A crafting building bound to a list makes only what is ON that list — that is
// what lets a Dwarven Forge be weapons-only. A crafting building with NO list
// makes anything (the pre-list behavior, and still the default). The list only
// ever NARROWS: it never grants a recipe the player has not learned, which is
// checked separately.
//
// Must be called under s.mu.
func (s *GameState) playerOwnsCraftingBuildingForLocked(playerID, itemID string) bool {
	match := func(b *protocol.BuildingTile) bool {
		list, bound := listForBuilding(b)
		if !bound {
			return true // unbound: makes anything
		}
		return containsString(list.Items, itemID)
	}
	return s.findBuiltCapabilityLocked(playerID, "crafting", match) != nil
}

// findBuiltCapabilityLocked returns the first fully-built, visible building the
// player owns that has `capability` and satisfies `match` (nil match = any).
// Must be called under s.mu.
func (s *GameState) findBuiltCapabilityLocked(playerID, capability string, match func(*protocol.BuildingTile) bool) *protocol.BuildingTile {
	for i := range s.MapConfig.Buildings {
		b := &s.MapConfig.Buildings[i]
		if !b.Visible {
			continue
		}
		if b.OwnerID == nil || *b.OwnerID != playerID {
			continue
		}
		if getMetadataBool(b.Metadata, "underConstruction") {
			continue
		}
		for _, c := range b.Capabilities {
			if c != capability {
				continue
			}
			if match == nil || match(b) {
				return b
			}
		}
	}
	return nil
}
