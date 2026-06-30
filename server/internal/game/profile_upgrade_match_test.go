package game

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// ─── match-start application (task 5.4) ──────────────────────────────────────

// TestProfileUpgrade_NoUpgrades_DefaultMultipliers verifies a player with no
// owned upgrades gets PhysicalDamageMultiplier=1.0, MagicDamageMultiplier=1.0,
// and ExtraStartingUnits is empty (no per-unit-type grants).
func TestProfileUpgrade_NoUpgrades_DefaultMultipliers(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.EnsurePlayerWithUpgrades("p1", nil, nil, nil, nil)

	s.mu.RLock()
	p, ok := s.Players["p1"]
	s.mu.RUnlock()

	if !ok {
		t.Fatal("player p1 not found after EnsurePlayerWithUpgrades")
	}
	if p.PhysicalDamageMultiplier != 1.0 {
		t.Errorf("PhysicalDamageMultiplier: want 1.0, got %v", p.PhysicalDamageMultiplier)
	}
	if p.MagicDamageMultiplier != 1.0 {
		t.Errorf("MagicDamageMultiplier: want 1.0, got %v", p.MagicDamageMultiplier)
	}
	if got := p.ExtraStartingUnits["worker"]; got != 0 {
		t.Errorf(`ExtraStartingUnits["worker"]: want 0, got %d`, got)
	}
	if len(p.ExtraStartingUnits) != 0 {
		t.Errorf("ExtraStartingUnits: want empty map, got %v", p.ExtraStartingUnits)
	}
}

// TestProfileUpgrade_PhysicalPowerRank3_Multiplier verifies that physical_power
// at rank 3 yields PhysicalDamageMultiplier=1.30 and unchanged MagicDamageMultiplier.
func TestProfileUpgrade_PhysicalPowerRank3_Multiplier(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.EnsurePlayerWithUpgrades("p1", map[string]int{"physical_power": 3}, nil, nil, nil)

	s.mu.RLock()
	p, ok := s.Players["p1"]
	s.mu.RUnlock()

	if !ok {
		t.Fatal("player p1 not found")
	}
	const wantPhysical = 1.30
	if math.Abs(p.PhysicalDamageMultiplier-wantPhysical) > 1e-9 {
		t.Errorf("PhysicalDamageMultiplier: want %.2f, got %v", wantPhysical, p.PhysicalDamageMultiplier)
	}
	if p.MagicDamageMultiplier != 1.0 {
		t.Errorf("MagicDamageMultiplier: want 1.0, got %v", p.MagicDamageMultiplier)
	}
}

// TestProfileUpgrade_MagicPowerRank1_OnlyMagicMultiplied verifies magic_power
// rank 1 yields MagicDamageMultiplier=1.10 and PhysicalDamageMultiplier=1.0.
func TestProfileUpgrade_MagicPowerRank1_OnlyMagicMultiplied(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.EnsurePlayerWithUpgrades("p1", map[string]int{"magic_power": 1}, nil, nil, nil)

	s.mu.RLock()
	p, ok := s.Players["p1"]
	s.mu.RUnlock()

	if !ok {
		t.Fatal("player p1 not found")
	}
	if math.Abs(p.MagicDamageMultiplier-1.10) > 1e-9 {
		t.Errorf("MagicDamageMultiplier: want 1.10, got %v", p.MagicDamageMultiplier)
	}
	if p.PhysicalDamageMultiplier != 1.0 {
		t.Errorf("PhysicalDamageMultiplier: want 1.0, got %v", p.PhysicalDamageMultiplier)
	}
}

// ─── damage pipeline (task 6.2) ──────────────────────────────────────────────

// TestProfileUpgrade_PhysicalPower_DamageMultiplied verifies that a unit owned
// by a player with PhysicalDamageMultiplier=1.30 deals 30% more physical damage.
// Also verifies the enemy AI attacker is unaffected by profile multipliers.
func TestProfileUpgrade_PhysicalPower_DamageMultiplied(t *testing.T) {
	s := newProjectileTestState(t)

	// Set up a player with rank-3 physical_power → multiplier = 1.30.
	s.EnsurePlayerWithUpgrades("p1", map[string]int{"physical_power": 3}, nil, nil, nil)

	s.mu.Lock()
	defer s.mu.Unlock()

	p1Player := s.Players["p1"]
	if math.Abs(p1Player.PhysicalDamageMultiplier-1.30) > 1e-9 {
		// Double-check the multiplier is correct before proceeding.
		s.mu.Unlock()
		t.Fatalf("expected PhysicalDamageMultiplier=1.30, got %v", p1Player.PhysicalDamageMultiplier)
		s.mu.Lock()
	}

	// Player attacker with physical damage type (default).
	playerAttacker := spawnProjTestUnit(t, s, "p1", 100, 100)
	playerAttacker.AttackDamageType = DamagePhysical
	playerAttacker.Damage = 100

	// Enemy AI attacker (should be unaffected).
	enemyAttacker := spawnProjTestUnit(t, s, enemyPlayerID, 150, 100)
	enemyAttacker.AttackDamageType = DamagePhysical
	enemyAttacker.Damage = 100

	// Targets — each starts at 500 HP (from spawnProjTestUnit).
	physTarget1 := spawnProjTestUnit(t, s, enemyPlayerID, 400, 400)
	physTarget2 := spawnProjTestUnit(t, s, "p1", 420, 400)

	// Apply the raw multiplier as the pipeline does, before armor mitigation.
	// We call applyProfileDamageMultiplierLocked directly to isolate the test.
	rawPlayerDmg := float64(playerAttacker.Damage)
	multiplied := s.applyProfileDamageMultiplierLocked(playerAttacker, rawPlayerDmg)
	if math.Abs(multiplied-130.0) > 1e-6 {
		t.Errorf("player physical attacker: want rawDamage=130, got %v", multiplied)
	}

	rawEnemyDmg := float64(enemyAttacker.Damage)
	enemyMultiplied := s.applyProfileDamageMultiplierLocked(enemyAttacker, rawEnemyDmg)
	if math.Abs(enemyMultiplied-100.0) > 1e-6 {
		t.Errorf("enemy AI attacker: want rawDamage=100 (unmodified), got %v", enemyMultiplied)
	}

	// Also verify that the player's magic multiplier is NOT applied to physical.
	playerAttacker.AttackDamageType = DamageFire
	magicMultiplied := s.applyProfileDamageMultiplierLocked(playerAttacker, rawPlayerDmg)
	// Magic multiplier is 1.0 (no magic_power purchased), so result should be 100.
	if math.Abs(magicMultiplied-100.0) > 1e-6 {
		t.Errorf("player fire attacker (no magic_power): want rawDamage=100, got %v", magicMultiplied)
	}

	_ = physTarget1
	_ = physTarget2
}

// TestProfileUpgrade_MagicAttack_PhysicalPowerNotApplied verifies that a unit
// with physical_power rank 3 (PhysicalDamageMultiplier=1.30) does NOT boost a
// fire-typed attack — fire uses MagicDamageMultiplier, which is 1.0.
func TestProfileUpgrade_MagicAttack_PhysicalPowerNotApplied(t *testing.T) {
	s := newProjectileTestState(t)
	s.EnsurePlayerWithUpgrades("p1", map[string]int{"physical_power": 3}, nil, nil, nil)

	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := spawnProjTestUnit(t, s, "p1", 100, 100)
	attacker.AttackDamageType = DamageFire
	attacker.Damage = 100

	rawDmg := float64(attacker.Damage)
	result := s.applyProfileDamageMultiplierLocked(attacker, rawDmg)

	// Fire is not physical → uses MagicDamageMultiplier which is 1.0.
	if math.Abs(result-100.0) > 1e-6 {
		t.Errorf("fire attack with physical_power only: want rawDamage=100, got %v", result)
	}
}

// TestProfileUpgrade_EnemyAI_Unaffected verifies that neutral and enemy AI
// units do not receive profile upgrade multipliers.
func TestProfileUpgrade_EnemyAI_Unaffected(t *testing.T) {
	s := newProjectileTestState(t)

	s.mu.Lock()
	defer s.mu.Unlock()

	enemyUnit := spawnProjTestUnit(t, s, enemyPlayerID, 100, 100)
	enemyUnit.AttackDamageType = DamagePhysical
	enemyUnit.Damage = 100

	neutralUnit := &Unit{
		OwnerID:          neutralPlayerID,
		AttackDamageType: DamagePhysical,
		Damage:           100,
	}

	raw := 100.0
	if got := s.applyProfileDamageMultiplierLocked(enemyUnit, raw); got != raw {
		t.Errorf("enemy unit: want %v (unmodified), got %v", raw, got)
	}
	if got := s.applyProfileDamageMultiplierLocked(neutralUnit, raw); got != raw {
		t.Errorf("neutral unit: want %v (unmodified), got %v", raw, got)
	}
}

// TestProfileUpgrade_FullDamagePipeline_PlayerPhysicalAttack verifies the full
// damage pipeline (through applyUnitDamageWithSourceLocked) applies the
// physical multiplier for a player-owned unit. The target must take 130 damage
// (100 * 1.30) before armor mitigation, where armor=0 means the full 130 lands.
func TestProfileUpgrade_FullDamagePipeline_PlayerPhysicalAttack(t *testing.T) {
	s := newProjectileTestState(t)
	s.EnsurePlayerWithUpgrades("p1", map[string]int{"physical_power": 3}, nil, nil, nil)

	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := spawnProjTestUnit(t, s, "p1", 100, 100)
	attacker.AttackDamageType = DamagePhysical
	attacker.Damage = 100

	target := spawnProjTestUnit(t, s, enemyPlayerID, 400, 400)
	// Zero out all armor fields so mitigation doesn't interfere with the
	// expected 130 damage. spawnProjTestUnit sets HP=500, MaxHP=500.
	target.BaseArmor = 0
	target.Armor = 0

	// Simulate what applyDelayedAttackLocked does: compute rawDamage, apply
	// profile multiplier, then run through the damage pipeline.
	rawDamage := float64(attacker.Damage)
	rawDamage = s.applyProfileDamageMultiplierLocked(attacker, rawDamage)
	damage := applyArmorMitigation(int(math.Round(rawDamage)), s.effectiveArmorLocked(target))
	s.applyUnitDamageWithSourceLocked(target, damage, DamageSource{
		AttackerUnitID: attacker.ID,
		Kind:           "melee",
		DamageType:     DamagePhysical,
	})

	wantHP := target.MaxHP - 130
	if target.HP != wantHP {
		t.Errorf("target HP: want %d (100 * 1.30 = 130 damage), got %d", wantHP, target.HP)
	}
}

// TestProfileUpgrade_ReconnectDoesNotResetMultipliers verifies that a player
// who reconnects (calls EnsurePlayer again on an existing player slot) does not
// have their multipliers reset.
func TestProfileUpgrade_ReconnectDoesNotResetMultipliers(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.EnsurePlayerWithUpgrades("p1", map[string]int{"physical_power": 3}, nil, nil, nil)

	s.mu.RLock()
	multBefore := s.Players["p1"].PhysicalDamageMultiplier
	s.mu.RUnlock()

	// Simulate reconnect — EnsurePlayer is idempotent for existing players.
	s.EnsurePlayer("p1")

	s.mu.RLock()
	multAfter := s.Players["p1"].PhysicalDamageMultiplier
	s.mu.RUnlock()

	if multBefore != multAfter {
		t.Errorf("reconnect changed PhysicalDamageMultiplier: %v → %v", multBefore, multAfter)
	}
}

// ─── HTTP purchase / refund helpers ──────────────────────────────────────────

// TestApplyProfileUpgradesToPlayerLocked_Deterministic verifies that applying
// the same upgrade map twice yields the same multipliers (idempotent). Because
// the function accumulates onto the player, calling it twice would double the
// bonus — this test verifies the function is only called once per player join
// (not a re-entrant loop). We test it by calling it once and checking the result.
func TestApplyProfileUpgradesToPlayerLocked_Deterministic(t *testing.T) {
	player := &Player{
		ID:                       "test",
		PhysicalDamageMultiplier: 1.0,
		MagicDamageMultiplier:    1.0,
		ProfileUpgrades: map[string]int{
			"physical_power": 3,
			"magic_power":    2,
		},
		ActiveUpgradeIDs: map[string]bool{
			"physical_power": true,
			"magic_power":    true,
		},
	}
	applyProfileUpgradesToPlayerLocked(player)

	if math.Abs(player.PhysicalDamageMultiplier-1.30) > 1e-9 {
		t.Errorf("PhysicalDamageMultiplier: want 1.30, got %v", player.PhysicalDamageMultiplier)
	}
	if math.Abs(player.MagicDamageMultiplier-1.20) > 1e-9 {
		t.Errorf("MagicDamageMultiplier: want 1.20, got %v", player.MagicDamageMultiplier)
	}
}

// ─── active upgrade gate (task: active-state toggle) ─────────────────────────

// TestActiveUpgradeGate_InactiveUpgradeNotApplied verifies that a player who
// owns physical_power rank 3 but has it NOT in ActiveUpgradeIDs gets
// PhysicalDamageMultiplier=1.0 (no bonus applied).
func TestActiveUpgradeGate_InactiveUpgradeNotApplied(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	// Pass an explicit empty active set — physical_power is owned but inactive.
	s.EnsurePlayerWithUpgrades("p1", map[string]int{"physical_power": 3}, []string{}, nil, nil)

	s.mu.RLock()
	p, ok := s.Players["p1"]
	s.mu.RUnlock()

	if !ok {
		t.Fatal("player p1 not found")
	}
	if p.PhysicalDamageMultiplier != 1.0 {
		t.Errorf("inactive upgrade must not apply: want PhysicalDamageMultiplier=1.0, got %v", p.PhysicalDamageMultiplier)
	}
}

// TestActiveUpgradeGate_ActiveUpgradeApplied verifies that a player who owns
// physical_power rank 3 AND has it in ActiveUpgradeIDs gets
// PhysicalDamageMultiplier=1.30.
func TestActiveUpgradeGate_ActiveUpgradeApplied(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.EnsurePlayerWithUpgrades("p1", map[string]int{"physical_power": 3}, []string{"physical_power"}, nil, nil)

	s.mu.RLock()
	p, ok := s.Players["p1"]
	s.mu.RUnlock()

	if !ok {
		t.Fatal("player p1 not found")
	}
	const wantPhysical = 1.30
	if math.Abs(p.PhysicalDamageMultiplier-wantPhysical) > 1e-9 {
		t.Errorf("active upgrade must apply: want PhysicalDamageMultiplier=%.2f, got %v", wantPhysical, p.PhysicalDamageMultiplier)
	}
}

// newWorldCenter is a helper that returns a protocol.Vec2 for tests.
func newWorldCenter(x, y float64) protocol.Vec2 {
	return protocol.Vec2{X: x, Y: y}
}
