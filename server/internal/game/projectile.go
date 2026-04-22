package game

import (
	"fmt"
	"math"
)

// defaultProjectileSpeed is the world-space travel speed in pixels/second.
// Archer AttackRange is ~200–300px, so a max-range shot lands in ~0.4–0.6s.
const defaultProjectileSpeed = 500.0

// minProjectileFlightSeconds guarantees a visible arc even when attacker and
// target are essentially on top of each other.
const minProjectileFlightSeconds = 0.05

// Projectile is an in-flight ranged shot. It homes on its target's current
// position each tick and lands damage (+ on-hit perk triggers) when
// RemainingSeconds hits zero. At fire time only the damage snapshot, the
// attacker's cooldown, and the archer combat gate are committed — the full
// damage pipeline is deferred to landProjectileLocked.
type Projectile struct {
	ID            string
	OwnerUnitID   int
	OwnerPlayerID string

	TargetUnitID int

	OriginX, OriginY float64
	// TargetX/Y is refreshed each tick from the target unit so the client can
	// render a homing arc that doesn't get outrun by moving targets.
	TargetX, TargetY float64

	TotalSeconds     float64
	RemainingSeconds float64

	// Damage is the armor-mitigated final damage snapshotted at fire time.
	Damage int

	// Variant is the client-side sprite key — defaults to attacker.UnitType.
	// Perks may override it at fire time for alternate shot visuals.
	Variant string
}

func (s *GameState) fireProjectileLocked(attacker, target *Unit, damage int) {
	dx := target.X - attacker.X
	dy := target.Y - attacker.Y
	travelTime := math.Sqrt(dx*dx+dy*dy) / defaultProjectileSpeed
	if travelTime < minProjectileFlightSeconds {
		travelTime = minProjectileFlightSeconds
	}

	id := fmt.Sprintf("proj_%d", s.nextProjectileID)
	s.nextProjectileID++

	s.Projectiles = append(s.Projectiles, &Projectile{
		ID:               id,
		OwnerUnitID:      attacker.ID,
		OwnerPlayerID:    attacker.OwnerID,
		TargetUnitID:     target.ID,
		OriginX:          attacker.X,
		OriginY:          attacker.Y,
		TargetX:          target.X,
		TargetY:          target.Y,
		TotalSeconds:     travelTime,
		RemainingSeconds: travelTime,
		Damage:           damage,
		Variant:          attacker.UnitType,
	})
}

// tickProjectilesLocked advances in-flight projectiles and lands the ones that
// hit zero this tick. Must run after tickUnitCombatLocked so shots fired this
// tick wait a full dt before decaying.
func (s *GameState) tickProjectilesLocked(dt float64) {
	if len(s.Projectiles) == 0 {
		return
	}

	var deadUnitIDs []int
	kept := s.Projectiles[:0]

	for _, proj := range s.Projectiles {
		target := s.getUnitByIDLocked(proj.TargetUnitID)
		// Drop silently if the target is gone — no retarget, no wasted hit.
		if target == nil || target.HP <= 0 || !target.Visible {
			continue
		}

		proj.TargetX = target.X
		proj.TargetY = target.Y

		proj.RemainingSeconds -= dt
		if proj.RemainingSeconds > 0 {
			kept = append(kept, proj)
			continue
		}

		s.landProjectileLocked(proj, target, &deadUnitIDs)
	}
	s.Projectiles = kept

	for _, id := range deadUnitIDs {
		s.removeUnitLocked(id)
	}
}

func (s *GameState) landProjectileLocked(proj *Projectile, target *Unit, deadUnitIDs *[]int) {
	attacker := s.getUnitByIDLocked(proj.OwnerUnitID)
	if attacker == nil {
		// Attacker died between fire and land — damage still lands (the arrow
		// was already in flight), but attacker-side perks are skipped.
		s.applyUnitDamageLocked(target, proj.Damage)
		if target.HP <= 0 {
			target.HP = 0
			*deadUnitIDs = append(*deadUnitIDs, target.ID)
		}
		return
	}
	s.resolveAttackHitLocked(attacker, target, proj.Damage, deadUnitIDs)
}

// cullProjectilesLocked drops any in-flight projectiles matching shouldDrop.
// Used by player/unit removal paths to clear stale references.
func (s *GameState) cullProjectilesLocked(shouldDrop func(*Projectile) bool) {
	if len(s.Projectiles) == 0 {
		return
	}
	kept := s.Projectiles[:0]
	for _, proj := range s.Projectiles {
		if shouldDrop(proj) {
			continue
		}
		kept = append(kept, proj)
	}
	s.Projectiles = kept
}
