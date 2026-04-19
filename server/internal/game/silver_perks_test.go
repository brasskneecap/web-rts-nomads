package game

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers shared across silver-perk tests
// ─────────────────────────────────────────────────────────────────────────────

// newSilverPerkState returns a minimal GameState with two opposing soldiers
// already in place. vanguard belongs to "p1", attacker belongs to "p2".
// Both are fully constructed (combat-capable, Visible, HP > 0).
// The caller should configure PerkIDs and any state before running assertions.
func newSilverPerkState(t *testing.T) (s *GameState, vanguard, attacker *Unit) {
	t.Helper()
	s = NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.mu.Lock()
	defer s.mu.Unlock()

	vanguard = s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	attacker = s.spawnPlayerUnitLocked("soldier", "p2", "#e74c3c", protocol.Vec2{X: 420, Y: 400})
	return s, vanguard, attacker
}

// grantPerk appends a perk ID to unit.PerkIDs without going through the
// rank-up assignment pipeline — safe for unit tests that need a specific perk
// regardless of how the unit reached Silver.
func grantPerk(unit *Unit, perkID string) {
	unit.PerkIDs = append(unit.PerkIDs, perkID)
}

// ─────────────────────────────────────────────────────────────────────────────
// 1. Last Stand
// ─────────────────────────────────────────────────────────────────────────────

// TestLastStand_ArmorBonusBelowThreshold verifies that last_stand grants its
// bonusArmor when the unit drops below the HP threshold. Effective armor
// flows into every applyArmorMitigation call site and into Retaliation's
// reflection math.
func TestLastStand_ArmorBonusBelowThreshold(t *testing.T) {
	s, vanguard, _ := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "last_stand")
	def := perkDefByID("last_stand")
	if def == nil {
		t.Fatal("last_stand perk def not found")
	}

	baseArmor := vanguard.Armor
	expectedBonus := int(def.Config["bonusArmor"])

	// Drop to 30% — below the 35% threshold.
	vanguard.MaxHP = 500
	vanguard.HP = 150

	gotBonus := s.perkBonusArmorLocked(vanguard)
	if gotBonus != expectedBonus {
		t.Errorf("perkBonusArmorLocked below threshold: got %d, want %d", gotBonus, expectedBonus)
	}

	gotEffective := s.effectiveArmorLocked(vanguard)
	if gotEffective != baseArmor+expectedBonus {
		t.Errorf("effectiveArmorLocked below threshold: got %d, want %d",
			gotEffective, baseArmor+expectedBonus)
	}
}

// TestLastStand_NoArmorBonusAboveThreshold verifies that last_stand provides
// no armor bonus while the unit is above the HP threshold.
func TestLastStand_NoArmorBonusAboveThreshold(t *testing.T) {
	s, vanguard, _ := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "last_stand")

	// At full HP — well above the 35% threshold.
	vanguard.HP = vanguard.MaxHP

	if got := s.perkBonusArmorLocked(vanguard); got != 0 {
		t.Errorf("perkBonusArmorLocked above threshold: got %d, want 0", got)
	}
	if got := s.effectiveArmorLocked(vanguard); got != vanguard.Armor {
		t.Errorf("effectiveArmorLocked above threshold: got %d, want %d (base armor)",
			got, vanguard.Armor)
	}
}

// TestLastStand_TauntFires_OnThresholdEntry verifies the one-shot AoE taunt
// fires when the unit first crosses below the HP threshold.
func TestLastStand_TauntFires_OnThresholdEntry(t *testing.T) {
	s, vanguard, attacker := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "last_stand")
	def := perkDefByID("last_stand")

	// Place the attacker within taunt radius.
	attacker.X = vanguard.X + def.Config["tauntRadius"]*0.5
	attacker.Y = vanguard.Y

	// Unit starts above threshold — trigger should not have fired yet.
	if vanguard.PerkState.LastStandTriggered {
		t.Fatal("LastStandTriggered should be false before threshold crossing")
	}

	// Drop HP to 30% — below the 35% threshold. Tick perk state.
	vanguard.HP = int(math.Floor(float64(vanguard.MaxHP) * 0.30))
	s.tickUnitPerkStateLocked(vanguard, 0.05)

	if !vanguard.PerkState.LastStandTriggered {
		t.Fatal("LastStandTriggered should be true after crossing below threshold")
	}
	if attacker.TauntedByUnitID != vanguard.ID {
		t.Errorf("attacker should be taunted by vanguard (ID=%d), got TauntedByUnitID=%d",
			vanguard.ID, attacker.TauntedByUnitID)
	}
	if attacker.TauntRemaining <= 0 {
		t.Errorf("attacker.TauntRemaining should be > 0 after taunt, got %f", attacker.TauntRemaining)
	}
}

// TestLastStand_TauntFiredOnlyOnce verifies the one-shot taunt doesn't
// re-fire on subsequent ticks while the unit remains below threshold.
func TestLastStand_TauntFiredOnlyOnce(t *testing.T) {
	s, vanguard, attacker := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "last_stand")
	def := perkDefByID("last_stand")
	attacker.X = vanguard.X + def.Config["tauntRadius"]*0.5
	attacker.Y = vanguard.Y

	vanguard.HP = int(math.Floor(float64(vanguard.MaxHP) * 0.30))
	// First tick: taunt fires.
	s.tickUnitPerkStateLocked(vanguard, 0.05)
	tauntAfterFirstTick := attacker.TauntRemaining

	// Manually expire the taunt duration so we can detect a second application.
	attacker.TauntRemaining = 0
	attacker.TauntedByUnitID = 0

	// Second tick: unit still below threshold — taunt must NOT re-fire.
	s.tickUnitPerkStateLocked(vanguard, 0.05)
	if attacker.TauntedByUnitID != 0 {
		t.Errorf("taunt should not re-fire while unit stays below threshold; first TauntRemaining was %.2f", tauntAfterFirstTick)
	}
}

// TestLastStand_TriggerResets_WhenHealedAboveThreshold verifies the trigger
// flag resets when HP rises above the threshold, allowing the taunt to re-fire.
func TestLastStand_TriggerResets_WhenHealedAboveThreshold(t *testing.T) {
	s, vanguard, attacker := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "last_stand")
	def := perkDefByID("last_stand")
	attacker.X = vanguard.X + def.Config["tauntRadius"]*0.5
	attacker.Y = vanguard.Y

	// First dip below threshold.
	vanguard.HP = int(math.Floor(float64(vanguard.MaxHP) * 0.30))
	s.tickUnitPerkStateLocked(vanguard, 0.05)
	if !vanguard.PerkState.LastStandTriggered {
		t.Fatal("expected LastStandTriggered=true after first dip")
	}

	// Heal above threshold.
	vanguard.HP = vanguard.MaxHP
	s.tickUnitPerkStateLocked(vanguard, 0.05)
	if vanguard.PerkState.LastStandTriggered {
		t.Fatal("expected LastStandTriggered=false after healing above threshold")
	}

	// Dip below again — taunt should re-fire.
	attacker.TauntRemaining = 0
	attacker.TauntedByUnitID = 0
	vanguard.HP = int(math.Floor(float64(vanguard.MaxHP) * 0.30))
	s.tickUnitPerkStateLocked(vanguard, 0.05)
	if attacker.TauntedByUnitID != vanguard.ID {
		t.Error("expected taunt to re-fire after healing and dipping below threshold again")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 2. Punishing Guard
// ─────────────────────────────────────────────────────────────────────────────

// TestPunishingGuard_WeakenedStampedOnAttacker verifies that when a Vanguard
// with punishing_guard takes damage, the attacker's WeakenedRemaining is set.
func TestPunishingGuard_WeakenedStampedOnAttacker(t *testing.T) {
	s, vanguard, attacker := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "punishing_guard")
	def := perkDefByID("punishing_guard")

	if attacker.PerkState.WeakenedRemaining > 0 {
		t.Fatal("attacker should not be weakened before any hit")
	}

	s.onPerkDamageTakenLocked(vanguard, attacker, 10)

	if attacker.PerkState.WeakenedRemaining != def.Config["durationSeconds"] {
		t.Errorf("expected WeakenedRemaining=%.1f, got %.1f",
			def.Config["durationSeconds"], attacker.PerkState.WeakenedRemaining)
	}
	if attacker.PerkState.WeakenedMultiplier != def.Config["weakenedMultiplier"] {
		t.Errorf("expected WeakenedMultiplier=%.2f, got %.2f",
			def.Config["weakenedMultiplier"], attacker.PerkState.WeakenedMultiplier)
	}
}

// TestPunishingGuard_ReducesOutgoingDamage verifies that a weakened attacker
// deals less damage via perkOutgoingDamageDebuffMultiplierLocked.
func TestPunishingGuard_ReducesOutgoingDamage(t *testing.T) {
	s, vanguard, attacker := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "punishing_guard")
	def := perkDefByID("punishing_guard")

	// Stamp the weakened debuff on the attacker.
	s.onPerkDamageTakenLocked(vanguard, attacker, 10)

	debuffMult := s.perkOutgoingDamageDebuffMultiplierLocked(attacker)
	if math.Abs(debuffMult-def.Config["weakenedMultiplier"]) > 0.001 {
		t.Errorf("expected debuff multiplier %.2f, got %.2f", def.Config["weakenedMultiplier"], debuffMult)
	}
}

// TestPunishingGuard_DebuffExpires verifies that the weakened debuff clears
// after durationSeconds elapses. We tick the cross-unit decay directly rather
// than going through s.Update() to avoid combat between the two enemy units.
func TestPunishingGuard_DebuffExpires(t *testing.T) {
	s, vanguard, attacker := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "punishing_guard")
	def := perkDefByID("punishing_guard")
	s.onPerkDamageTakenLocked(vanguard, attacker, 10)

	if attacker.PerkState.WeakenedRemaining <= 0 {
		t.Fatal("expected WeakenedRemaining > 0 after hit")
	}

	// Manually run the cross-unit decay the same way Update() does it.
	duration := def.Config["durationSeconds"]
	dt := 0.05
	elapsed := 0.0
	for elapsed < duration+dt {
		if attacker.PerkState.WeakenedRemaining > 0 {
			attacker.PerkState.WeakenedRemaining = math.Max(0, attacker.PerkState.WeakenedRemaining-dt)
			if attacker.PerkState.WeakenedRemaining == 0 {
				attacker.PerkState.WeakenedMultiplier = 0
			}
		}
		elapsed += dt
	}

	if attacker.PerkState.WeakenedRemaining != 0 {
		t.Errorf("WeakenedRemaining should be 0 after duration, got %.3f", attacker.PerkState.WeakenedRemaining)
	}
	if attacker.PerkState.WeakenedMultiplier != 0 {
		t.Errorf("WeakenedMultiplier should be 0 after duration, got %.3f", attacker.PerkState.WeakenedMultiplier)
	}
	if s.perkOutgoingDamageDebuffMultiplierLocked(attacker) != 0 {
		t.Error("debuff multiplier should return 0 after expiry")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 3. Brace
// ─────────────────────────────────────────────────────────────────────────────

// TestBrace_NoArmorBonus_BelowEnemyThreshold verifies no armor bonus when
// there is only one enemy nearby (threshold is 2).
func TestBrace_NoArmorBonus_BelowEnemyThreshold(t *testing.T) {
	s, vanguard, attacker := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "brace")
	def := perkDefByID("brace")

	// Place attacker within radius.
	attacker.X = vanguard.X + def.Config["radius"]*0.5
	attacker.Y = vanguard.Y

	// With only 1 enemy nearby (threshold=2), no armor bonus.
	got := s.perkBonusArmorLocked(vanguard)
	if got != 0 {
		t.Errorf("brace with 1 enemy: expected 0 bonus armor, got %d", got)
	}
}

// TestBrace_ArmorBonus_AtEnemyThreshold verifies the armor bonus activates
// when there are exactly enemyThreshold enemies nearby, and that end-to-end
// damage is reduced compared to having no brace.
func TestBrace_ArmorBonus_AtEnemyThreshold(t *testing.T) {
	s, vanguard, attacker := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "brace")
	def := perkDefByID("brace")

	// Place first attacker within radius.
	attacker.X = vanguard.X + def.Config["radius"]*0.5
	attacker.Y = vanguard.Y

	// Spawn a second enemy within radius.
	enemy2 := s.spawnPlayerUnitLocked("soldier", "p2", "#e74c3c", protocol.Vec2{
		X: vanguard.X - def.Config["radius"]*0.5,
		Y: vanguard.Y,
	})
	enemy2.Visible = true

	// Verify the bonus armor is applied.
	wantBonus := int(def.Config["bonusArmor"])
	gotBonus := s.perkBonusArmorLocked(vanguard)
	if gotBonus != wantBonus {
		t.Errorf("brace bonus armor: got %d, want %d", gotBonus, wantBonus)
	}

	// Verify effective armor includes the bonus.
	wantEffective := vanguard.Armor + wantBonus
	gotEffective := s.effectiveArmorLocked(vanguard)
	if gotEffective != wantEffective {
		t.Errorf("brace effectiveArmor: got %d, want %d", gotEffective, wantEffective)
	}
}

// TestBrace_NoArmorBonus_EnemyOutsideRadius verifies that enemies outside the
// radius do not count toward the threshold.
func TestBrace_NoArmorBonus_EnemyOutsideRadius(t *testing.T) {
	s, vanguard, attacker := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "brace")
	def := perkDefByID("brace")

	// Place both enemies outside the radius.
	attacker.X = vanguard.X + def.Config["radius"]*2
	attacker.Y = vanguard.Y
	enemy2 := s.spawnPlayerUnitLocked("soldier", "p2", "#e74c3c", protocol.Vec2{
		X: vanguard.X - def.Config["radius"]*2,
		Y: vanguard.Y,
	})

	got := s.perkBonusArmorLocked(vanguard)
	if got != 0 {
		t.Errorf("brace: enemies outside radius should not trigger armor bonus; got %d", got)
	}
	_ = enemy2
}

// ─────────────────────────────────────────────────────────────────────────────
// 4. Bulwark
// ─────────────────────────────────────────────────────────────────────────────

// TestBulwark_ShieldGranted_AfterStationaryThreshold verifies the Vanguard
// receives its shield once it has held position for the required duration.
func TestBulwark_ShieldGranted_AfterStationaryThreshold(t *testing.T) {
	s, vanguard, _ := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "bulwark")
	def := perkDefByID("bulwark")

	vanguard.Moving = false
	vanguard.Shield = 0

	threshold := def.Config["stationaryThresholdSeconds"]

	// Tick for exactly the threshold — shield should be granted.
	elapsed := 0.0
	dt := 0.05
	for elapsed < threshold {
		s.tickUnitPerkStateLocked(vanguard, dt)
		elapsed += dt
	}

	if vanguard.Shield != int(def.Config["maxShield"]) {
		t.Errorf("expected shield=%d after stationary threshold, got %d",
			int(def.Config["maxShield"]), vanguard.Shield)
	}
}

// TestBulwark_ShieldNotGranted_WhileMoving verifies no shield while moving.
func TestBulwark_ShieldNotGranted_WhileMoving(t *testing.T) {
	s, vanguard, _ := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "bulwark")
	def := perkDefByID("bulwark")

	vanguard.Moving = true
	vanguard.Shield = 0

	threshold := def.Config["stationaryThresholdSeconds"]
	elapsed := 0.0
	for elapsed < threshold+0.5 {
		s.tickUnitPerkStateLocked(vanguard, 0.05)
		elapsed += 0.05
	}

	if vanguard.Shield != 0 {
		t.Errorf("moving unit should not have bulwark shield, got %d", vanguard.Shield)
	}
}

// TestBulwark_StationaryTimerResets_OnMove verifies that moving resets the
// stationary timer so the shield regen restarts from zero on next stop.
func TestBulwark_StationaryTimerResets_OnMove(t *testing.T) {
	s, vanguard, _ := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "bulwark")
	def := perkDefByID("bulwark")

	vanguard.Moving = false
	// Accumulate some stationary time but not enough to grant shield.
	vanguard.PerkState.StationarySeconds = def.Config["stationaryThresholdSeconds"] - 0.5
	s.tickUnitPerkStateLocked(vanguard, 0.05)

	// Now the unit moves.
	vanguard.Moving = true
	s.tickUnitPerkStateLocked(vanguard, 0.05)

	if vanguard.PerkState.StationarySeconds != 0 {
		t.Errorf("StationarySeconds should reset to 0 on movement, got %.2f",
			vanguard.PerkState.StationarySeconds)
	}
}

// TestBulwark_ShieldClears_OnMove verifies that existing shield drops to zero
// the moment the Vanguard breaks formation. Bulwark is a planted-play reward —
// movement forfeits the protection.
func TestBulwark_ShieldClears_OnMove(t *testing.T) {
	s, vanguard, _ := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "bulwark")
	def := perkDefByID("bulwark")

	// Grant shield by crossing the stationary threshold.
	vanguard.Moving = false
	vanguard.PerkState.StationarySeconds = def.Config["stationaryThresholdSeconds"]
	s.tickUnitPerkStateLocked(vanguard, 0.05)

	if vanguard.Shield <= 0 {
		t.Fatal("expected shield to be granted before movement test")
	}

	// Unit moves — shield should drop to zero immediately.
	vanguard.Moving = true
	s.tickUnitPerkStateLocked(vanguard, 0.05)

	if vanguard.Shield != 0 {
		t.Errorf("shield should clear on movement, got %d", vanguard.Shield)
	}
}

// TestBulwark_ShieldDoesNotRefill_AfterDamage verifies that once the shield
// has been granted, damage chips it down and it stays reduced — it does not
// auto-regenerate every tick while the unit remains stationary. The shield
// only re-arms after the unit moves and re-plants for the full threshold.
func TestBulwark_ShieldDoesNotRefill_AfterDamage(t *testing.T) {
	s, vanguard, _ := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "bulwark")
	def := perkDefByID("bulwark")
	maxShield := int(def.Config["maxShield"])

	// Plant and accumulate enough stationary time to be granted the shield.
	vanguard.Moving = false
	vanguard.PerkState.StationarySeconds = def.Config["stationaryThresholdSeconds"]
	s.tickUnitPerkStateLocked(vanguard, 0.05)
	if vanguard.Shield != maxShield {
		t.Fatalf("expected shield=%d after grant, got %d", maxShield, vanguard.Shield)
	}

	// Damage the shield — simulate a 10-damage hit absorbed by the shield.
	vanguard.Shield -= 10
	expected := maxShield - 10

	// Tick repeatedly while still stationary. Shield must NOT refill.
	for i := 0; i < 100; i++ {
		s.tickUnitPerkStateLocked(vanguard, 0.05)
		if vanguard.Shield != expected {
			t.Fatalf("shield auto-refilled while stationary: tick %d, got %d, want %d",
				i, vanguard.Shield, expected)
		}
	}
}

// TestBulwark_ShieldReArms_AfterMoveAndReplant verifies that after the unit
// moves (clearing the shield and granted flag) and then plants again for the
// full threshold, the shield is granted a second time.
func TestBulwark_ShieldReArms_AfterMoveAndReplant(t *testing.T) {
	s, vanguard, _ := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "bulwark")
	def := perkDefByID("bulwark")
	maxShield := int(def.Config["maxShield"])
	threshold := def.Config["stationaryThresholdSeconds"]

	// First grant.
	vanguard.Moving = false
	vanguard.PerkState.StationarySeconds = threshold
	s.tickUnitPerkStateLocked(vanguard, 0.05)
	if vanguard.Shield != maxShield {
		t.Fatalf("expected first shield grant, got %d", vanguard.Shield)
	}

	// Move — shield drops, flag clears.
	vanguard.Moving = true
	s.tickUnitPerkStateLocked(vanguard, 0.05)
	if vanguard.Shield != 0 || vanguard.PerkState.BulwarkShieldGranted {
		t.Fatalf("after move: shield=%d granted=%v", vanguard.Shield, vanguard.PerkState.BulwarkShieldGranted)
	}

	// Re-plant and accumulate the threshold again.
	vanguard.Moving = false
	elapsed := 0.0
	dt := 0.05
	for elapsed < threshold {
		s.tickUnitPerkStateLocked(vanguard, dt)
		elapsed += dt
	}
	if vanguard.Shield != maxShield {
		t.Errorf("expected shield to re-arm to %d after replant, got %d", maxShield, vanguard.Shield)
	}
}

// TestBulwark_ContributesToUnitMaxShield verifies bulwark is counted by
// unitMaxShieldLocked so healUnitLocked can route overheal into shield.
func TestBulwark_ContributesToUnitMaxShield(t *testing.T) {
	s, vanguard, _ := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "bulwark")
	def := perkDefByID("bulwark")

	maxShield := s.unitMaxShieldLocked(vanguard)
	if maxShield != int(def.Config["maxShield"]) {
		t.Errorf("unitMaxShieldLocked: expected %d, got %d", int(def.Config["maxShield"]), maxShield)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 5. Challenger's Mark
// ─────────────────────────────────────────────────────────────────────────────

// TestChallengersmark_MarkApplied_OnAttack verifies that when a Vanguard with
// challengers_mark fires an attack, the target has MarkedRemaining set.
func TestChallengersmark_MarkApplied_OnAttack(t *testing.T) {
	s, vanguard, target := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "challengers_mark")
	def := perkDefByID("challengers_mark")

	if target.PerkState.MarkedRemaining > 0 {
		t.Fatal("target should not be marked before any attack")
	}

	var dead []int
	s.onPerkAttackFiredLocked(vanguard, target, 10, &dead)

	if target.PerkState.MarkedRemaining != def.Config["durationSeconds"] {
		t.Errorf("MarkedRemaining: expected %.1f, got %.1f",
			def.Config["durationSeconds"], target.PerkState.MarkedRemaining)
	}
	if target.PerkState.MarkedMultiplier != def.Config["bonusMultiplier"] {
		t.Errorf("MarkedMultiplier: expected %.2f, got %.2f",
			def.Config["bonusMultiplier"], target.PerkState.MarkedMultiplier)
	}
}

// TestChallengersmark_BonusDamage_FromAnySource verifies that a marked target
// takes amplified damage from a DIFFERENT attacker (not the marking Vanguard),
// confirming the "all sources" behaviour.
func TestChallengersmark_BonusDamage_FromAnySource(t *testing.T) {
	s, vanguard, target := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "challengers_mark")
	def := perkDefByID("challengers_mark")

	// Stamp the mark via the attack hook.
	var dead []int
	s.onPerkAttackFiredLocked(vanguard, target, 10, &dead)

	// Now any attacker hits the target — test applyUnitDamageLocked directly.
	// The mark amplification is the first step in that function.
	// Use 20 raw damage: 20 * (1 + 0.15) = 23. Both fit within MaxHP headroom.
	const rawDamage = 20
	target.MaxHP = 500 // ensure plenty of headroom so HP doesn't run out
	target.HP = target.MaxHP
	hpBefore := target.HP
	s.applyUnitDamageLocked(target, rawDamage)
	hpAfter := target.HP

	actualDamage := hpBefore - hpAfter
	expectedDamage := int(math.Round(float64(rawDamage) * (1.0 + def.Config["bonusMultiplier"])))

	// Allow ±1 for rounding.
	if diff := actualDamage - expectedDamage; diff > 1 || diff < -1 {
		t.Errorf("challengers_mark: bonus damage from other source: got %d, want ~%d (bonus=%.0f%%)",
			actualDamage, expectedDamage, def.Config["bonusMultiplier"]*100)
	}
}

// TestChallengersmark_NoBonusDamage_WhenExpired verifies that once the mark
// expires, the target takes normal (unamplified) damage. We tick decay directly
// to avoid combat between the two enemy units during a full Update() loop.
func TestChallengersmark_NoBonusDamage_WhenExpired(t *testing.T) {
	s, vanguard, target := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "challengers_mark")
	def := perkDefByID("challengers_mark")
	var dead []int
	s.onPerkAttackFiredLocked(vanguard, target, 10, &dead)

	if target.PerkState.MarkedRemaining <= 0 {
		t.Fatal("expected MarkedRemaining > 0 after attack")
	}

	// Manually run the cross-unit decay the same way Update() does it.
	duration := def.Config["durationSeconds"]
	dt := 0.05
	elapsed := 0.0
	for elapsed < duration+dt {
		if target.PerkState.MarkedRemaining > 0 {
			target.PerkState.MarkedRemaining = math.Max(0, target.PerkState.MarkedRemaining-dt)
			if target.PerkState.MarkedRemaining == 0 {
				target.PerkState.MarkedMultiplier = 0
			}
		}
		elapsed += dt
	}

	if target.PerkState.MarkedRemaining != 0 {
		t.Errorf("MarkedRemaining should be 0 after duration, got %.3f", target.PerkState.MarkedRemaining)
	}

	// Apply 20 raw damage — should take exactly 20 (no mark amplification).
	target.MaxHP = 500
	target.HP = target.MaxHP
	const rawDamage = 20
	hpBefore := target.HP
	s.applyUnitDamageLocked(target, rawDamage)
	hpAfter := target.HP
	actualDamage := hpBefore - hpAfter
	if actualDamage != rawDamage {
		t.Errorf("expired mark: expected full damage %d, got %d", rawDamage, actualDamage)
	}
}

// TestChallengersmark_MarkRefreshes_OnEachAttack verifies that the mark
// duration is refreshed (not stacked) on consecutive Vanguard attacks.
func TestChallengersmark_MarkRefreshes_OnEachAttack(t *testing.T) {
	s, vanguard, target := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "challengers_mark")
	def := perkDefByID("challengers_mark")

	var dead []int
	s.onPerkAttackFiredLocked(vanguard, target, 10, &dead)
	// Partially drain the mark duration to simulate time passing.
	target.PerkState.MarkedRemaining = 1.0

	// Attack again — duration should be reset to full.
	s.onPerkAttackFiredLocked(vanguard, target, 10, &dead)
	if target.PerkState.MarkedRemaining != def.Config["durationSeconds"] {
		t.Errorf("mark should refresh to full duration on each attack: got %.1f, want %.1f",
			target.PerkState.MarkedRemaining, def.Config["durationSeconds"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Interaction tests
// ─────────────────────────────────────────────────────────────────────────────

// TestLastStand_And_Brace_CoexistCleanly verifies that Last Stand (armor bonus)
// and Brace (armor bonus, conditional) both contribute to perkBonusArmorLocked
// without interfering with each other. Both are flat-armor sources that stack
// additively — they both flow through the same hook, reducing damage via the
// effectiveArmorLocked formula.
func TestLastStand_And_Brace_CoexistCleanly(t *testing.T) {
	s, vanguard, attacker := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "last_stand")
	grantPerk(vanguard, "brace")
	lsDef := perkDefByID("last_stand")
	braceDef := perkDefByID("brace")

	// Below Last Stand threshold AND with 2+ nearby enemies to trigger Brace.
	vanguard.MaxHP = 500
	vanguard.HP = 150 // 30% of 500, below 35% threshold
	attacker.X = vanguard.X + braceDef.Config["radius"]*0.5
	attacker.Y = vanguard.Y
	_ = s.spawnPlayerUnitLocked("soldier", "p2", "#e74c3c", protocol.Vec2{
		X: vanguard.X - braceDef.Config["radius"]*0.5,
		Y: vanguard.Y,
	})

	// Both Last Stand and Brace contribute to perkBonusArmorLocked when their
	// conditions are met (below HP threshold + 2 nearby enemies).
	wantBonus := int(lsDef.Config["bonusArmor"]) + int(braceDef.Config["bonusArmor"])
	if got := s.perkBonusArmorLocked(vanguard); got != wantBonus {
		t.Errorf("perkBonusArmorLocked with last_stand+brace: got %d, want %d (ls=%d + brace=%d)",
			got, wantBonus, int(lsDef.Config["bonusArmor"]), int(braceDef.Config["bonusArmor"]))
	}

	// Verify effectiveArmorLocked reflects combined bonus.
	wantEffective := vanguard.Armor + wantBonus
	if got := s.effectiveArmorLocked(vanguard); got != wantEffective {
		t.Errorf("effectiveArmorLocked with last_stand+brace: got %d, want %d", got, wantEffective)
	}
}

// TestLastStand_BoostsRetaliation verifies Last Stand's core synergy with
// Retaliation: Retaliation reflects (armor × armorPercent) back to the
// attacker, so Last Stand's armor bonus (while below HP threshold) amplifies
// the reflected damage.
func TestLastStand_BoostsRetaliation(t *testing.T) {
	s, vanguard, attacker := newSilverPerkState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(vanguard, "retaliation")
	grantPerk(vanguard, "last_stand")
	retDef := perkDefByID("retaliation")
	lsDef := perkDefByID("last_stand")

	// Headroom so neither unit dies during the test.
	vanguard.MaxHP = 500
	vanguard.HP = 150 // 30% — below Last Stand's 35% threshold
	attacker.MaxHP = 500
	attacker.HP = 500
	attackerHPBefore := attacker.HP

	// Trigger Retaliation by taking a hit. Damage value is irrelevant to the
	// reflected amount — the reflection is a pure function of the target's
	// effective armor.
	s.onPerkDamageTakenLocked(vanguard, attacker, 10)
	reflectedWithLastStand := attackerHPBefore - attacker.HP

	// Expected reflection uses EFFECTIVE armor (base + Last Stand bonus).
	effectiveArmor := vanguard.Armor + int(lsDef.Config["bonusArmor"])
	wantReflected := int(math.Round(float64(effectiveArmor) * retDef.Config["armorPercent"]))

	if diff := reflectedWithLastStand - wantReflected; diff > 1 || diff < -1 {
		t.Errorf("retaliation with last_stand: reflected %d, want ~%d (effective armor=%d, armorPercent=%.2f)",
			reflectedWithLastStand, wantReflected, effectiveArmor, retDef.Config["armorPercent"])
	}

	// Sanity check: reflected damage exceeds what bare armor alone would produce.
	baseReflected := int(math.Round(float64(vanguard.Armor) * retDef.Config["armorPercent"]))
	if reflectedWithLastStand <= baseReflected {
		t.Errorf("expected Last Stand to BOOST retaliation: reflected=%d, bare=%d",
			reflectedWithLastStand, baseReflected)
	}
}
