package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestGroundHazard_DelaysImpactThenBurns verifies the two-phase lifecycle: no
// damage during the fall delay, a one-time impact hit at the delay, then
// periodic burn ticks for the burn duration, then removal.
func TestGroundHazard_DelaysImpactThenBurns(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	caster := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 100, Y: 100})
	caster.Visible = true
	enemy := spawnEnemy(t, s, 800, 800) // full HP=500, hostile faction
	enemyID := enemy.ID
	startHP := enemy.HP

	h := &GroundHazard{
		ID:            groundHazardIDString(1),
		Kind:          "meteor",
		OwnerUnitID:   caster.ID,
		OwnerPlayerID: caster.OwnerID,
		X:             800, Y: 800,
		ImpactDelayRemaining: 0.6,
		ImpactRadius:         130,
		ImpactDamage:         140,
		DamageType:           DamageFire,
		BurnRemaining:        4.0,
		BurnRadius:           120,
		BurnDamagePerTick:    12,
		BurnTickInterval:     0.5,
	}
	s.GroundHazards = append(s.GroundHazards, h)
	s.mu.Unlock()

	advance(s, 10) // 0.5s < impact delay: no damage yet
	s.mu.RLock()
	if s.unitsByID[enemyID].HP != startHP {
		t.Fatalf("enemy took damage before impact: HP=%d want %d", s.unitsByID[enemyID].HP, startHP)
	}
	s.mu.RUnlock()

	advance(s, 3) // cross impact threshold (~0.65s)
	s.mu.RLock()
	afterImpact := s.unitsByID[enemyID].HP
	s.mu.RUnlock()
	if afterImpact >= startHP {
		t.Fatalf("enemy should have taken impact damage: HP=%d want < %d", afterImpact, startHP)
	}

	advance(s, 90) // 4.5s — let the burn run out
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.unitsByID[enemyID].HP >= afterImpact {
		t.Errorf("enemy should have taken burn damage over time: HP=%d want < %d", s.unitsByID[enemyID].HP, afterImpact)
	}
	if len(s.GroundHazards) != 0 {
		t.Errorf("hazard should be culled after burn ends: %d remaining", len(s.GroundHazards))
	}
}

// TestGroundHazard_OwnerDiesDuringBurn_NoPanicAndBurnContinues verifies that a
// hazard whose owning unit is removed mid-burn (e.g. the caster died) neither
// panics nor stops applying burn damage. applyAbilitySplashDamageLocked's
// downstream death/XP attribution (drainPendingDeathsLocked) resolves the
// attacker via getUnitByIDLocked and nil-checks it — see
// server/internal/game/damage_pipeline.go around the AttackerUnitID branch —
// so a dangling OwnerUnitID is safe by construction, matching the Trap system.
func TestGroundHazard_OwnerDiesDuringBurn_NoPanicAndBurnContinues(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	caster := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 100, Y: 100})
	caster.Visible = true
	casterID := caster.ID
	enemy := spawnEnemy(t, s, 800, 800)
	enemyID := enemy.ID

	h := &GroundHazard{
		ID:            groundHazardIDString(1),
		Kind:          "meteor",
		OwnerUnitID:   caster.ID,
		OwnerPlayerID: caster.OwnerID,
		X:             800, Y: 800,
		ImpactDelayRemaining: 0.05,
		ImpactRadius:         130,
		ImpactDamage:         140,
		DamageType:           DamageFire,
		BurnRemaining:        4.0,
		BurnRadius:           120,
		BurnDamagePerTick:    12,
		BurnTickInterval:     0.5,
	}
	s.GroundHazards = append(s.GroundHazards, h)
	s.mu.Unlock()

	advance(s, 2) // cross the (short) impact delay so the hazard is now burning

	s.mu.Lock()
	afterImpact := s.unitsByID[enemyID].HP
	// Simulate the caster dying mid-burn: the hazard's OwnerUnitID now points
	// at a removed unit for the remainder of the burn.
	s.removeUnitLocked(casterID)
	s.mu.Unlock()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("tick panicked with owner unit removed mid-burn: %v", r)
		}
	}()

	advance(s, 90) // run out the burn with the owner gone

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.unitsByID[enemyID].HP >= afterImpact {
		t.Errorf("burn should still apply after owner unit is removed: HP=%d want < %d", s.unitsByID[enemyID].HP, afterImpact)
	}
	if len(s.GroundHazards) != 0 {
		t.Errorf("hazard should still be culled after burn ends even with owner gone: %d remaining", len(s.GroundHazards))
	}
}
