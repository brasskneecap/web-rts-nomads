package game

// playerOwnsBuiltCapabilityLocked reports whether playerID owns at least one
// fully-built (not under-construction), visible building whose capability list
// includes capability. Generalizes playerHasMarketplaceLocked. Must be called
// under s.mu.
func (s *GameState) playerOwnsBuiltCapabilityLocked(playerID, capability string) bool {
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
			if c == capability {
				return true
			}
		}
	}
	return false
}
