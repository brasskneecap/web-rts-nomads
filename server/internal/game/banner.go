package game

// Banner is a placeable, persistent entity created by the rallying_banner perk.
// It grants allied units within its radius bonus armor and attack speed for its
// remaining duration, regardless of whether the owner is still alive or present.
//
// Banners are created in tickUnitPerkStateLocked and decayed in tickBannersLocked.
// They are separate from units — they have no combat participation and cannot be
// targeted or destroyed by enemy actions.
type Banner struct {
	ID               int
	OwnerUnitID      int
	OwnerPlayerID    string
	X, Y             float64
	Radius           float64
	RemainingSeconds float64
	ArmorBonus       int
	AttackSpeedBonus float64
}

// tickBannersLocked advances all active banner durations by dt seconds, removing
// expired banners and banners whose owner player has left the match.
//
// Uses the filter-into-front-of-slice pattern (kept := s.Banners[:0]) to avoid
// allocations in the steady state when no banners expire. This approach also
// avoids the non-determinism of in-place splice-and-continue inside a range loop.
//
// Must be called under s.mu write lock. Called from Update(dt) after
// tickUnitCombatLocked and before the per-unit tickUnitPerkStateLocked loop.
func (s *GameState) tickBannersLocked(dt float64) {
	if len(s.Banners) == 0 {
		return
	}
	kept := s.Banners[:0]
	for _, b := range s.Banners {
		b.RemainingSeconds -= dt
		// Epsilon guard: repeated float64 subtraction of dt (e.g. 0.05 at 20Hz
		// for 160 ticks across an 8s duration) accumulates ~2e-14 residual, so
		// a strict `<= 0` check keeps the banner alive one extra tick. Treating
		// sub-nanosecond remaining time as expired is visually and mechanically
		// indistinguishable from 0.
		if b.RemainingSeconds <= 1e-9 {
			continue
		}
		// Drop if owner's player has left the match.
		if _, ok := s.Players[b.OwnerPlayerID]; !ok {
			continue
		}
		kept = append(kept, b)
	}
	s.Banners = kept
}
