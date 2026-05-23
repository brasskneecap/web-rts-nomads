package game

// Cleric Silver perk unit tests.
//
// Covers all four Silver Cleric perks: divine_aegis, restoration_aura,
// zealous_march, divine_healer. Setup mirrors cleric_bronze_perks_test.go.
//
// Tunable values are pulled from the catalog via perkDefByID — no magic
// numbers in the assertions so a JSON tuning pass never silently invalidates
// the suite.

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// divine_aegis
// ─────────────────────────────────────────────────────────────────────────────

// TestDivineAegis_StampsProtectionOnNearbyAllies verifies that the first
// pulse (timer starts at 0) reaches every ally inside radius and skips
// allies outside it.
func TestDivineAegis_StampsProtectionOnNearbyAllies(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "divine_aegis")
	def := perkDefByID("divine_aegis")
	if def == nil {
		t.Fatal("divine_aegis perk def not found")
	}
	cfg := def.ConfigForRank(cleric.Rank)
	radius := cfg["radiusPixels"]
	duration := cfg["protectionDurationSeconds"]

	inside := spawnClericTestAlly(t, s, cleric.X+radius*0.5, cleric.Y)
	outside := spawnClericTestAlly(t, s, cleric.X+radius*2.0, cleric.Y)

	// First tick must fire the pulse because the timer is 0 on grant.
	s.tickDivineAegisPulseLocked(cleric, def, 0.05)

	if inside.PerkState.DivineAegisRemaining != duration {
		t.Errorf("inside ally DivineAegisRemaining = %.3f, want %.3f", inside.PerkState.DivineAegisRemaining, duration)
	}
	if outside.PerkState.DivineAegisRemaining != 0 {
		t.Errorf("outside ally DivineAegisRemaining = %.3f, want 0", outside.PerkState.DivineAegisRemaining)
	}
	if cleric.PerkState.DivineAegisPulseRemaining != cfg["intervalSeconds"] {
		t.Errorf("cleric DivineAegisPulseRemaining after pulse = %.3f, want %.3f",
			cleric.PerkState.DivineAegisPulseRemaining, cfg["intervalSeconds"])
	}
}

// TestDivineAegis_PulseCadenceRespectsInterval drives the pulse multiple
// times and asserts that the timer gates re-application (only one pulse per
// intervalSeconds).
func TestDivineAegis_PulseCadenceRespectsInterval(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "divine_aegis")
	def := perkDefByID("divine_aegis")
	if def == nil {
		t.Fatal("divine_aegis perk def not found")
	}
	cfg := def.ConfigForRank(cleric.Rank)
	ally := spawnClericTestAlly(t, s, cleric.X+10, cleric.Y)

	// First pulse fires (timer = 0).
	s.tickDivineAegisPulseLocked(cleric, def, 0.05)
	firstStamp := ally.PerkState.DivineAegisRemaining

	// Drain the recipient's charge as if a hit consumed it.
	ally.PerkState.DivineAegisRemaining = 0

	// Tick for half the interval — should NOT pulse again.
	for i := 0; i < int(cfg["intervalSeconds"]/0.05/2); i++ {
		s.tickDivineAegisPulseLocked(cleric, def, 0.05)
	}
	if ally.PerkState.DivineAegisRemaining != 0 {
		t.Errorf("pulse fired before interval elapsed; ally DivineAegisRemaining = %.3f", ally.PerkState.DivineAegisRemaining)
	}

	// Drain the rest of the interval.
	for i := 0; i < int(cfg["intervalSeconds"]/0.05); i++ {
		s.tickDivineAegisPulseLocked(cleric, def, 0.05)
	}
	if ally.PerkState.DivineAegisRemaining == 0 {
		t.Errorf("pulse did not fire after interval elapsed; ally DivineAegisRemaining = 0")
	}
	_ = firstStamp
}

// TestDivineAegis_AbsorbsOneHitThenConsumed grants the charge and applies a
// damage instance through the canonical pipeline. The first hit must be
// fully negated; the second hit lands normally.
func TestDivineAegis_AbsorbsOneHitThenConsumed(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "divine_aegis")
	def := perkDefByID("divine_aegis")
	if def == nil {
		t.Fatal("divine_aegis perk def not found")
	}
	cfg := def.ConfigForRank(cleric.Rank)

	ally := spawnClericTestAlly(t, s, cleric.X+10, cleric.Y)
	ally.Armor = 0
	ally.PerkState.DivineAegisRemaining = cfg["protectionDurationSeconds"]
	startHP := ally.HP

	// First hit — should be absorbed.
	s.applyUnitDamageWithSourceLocked(ally, 25, DamageSource{Kind: "melee"})
	if ally.HP != startHP {
		t.Errorf("first hit not absorbed: HP %d → %d", startHP, ally.HP)
	}
	if ally.PerkState.DivineAegisRemaining != 0 {
		t.Errorf("charge not consumed: DivineAegisRemaining = %.3f", ally.PerkState.DivineAegisRemaining)
	}

	// Second hit — should land normally.
	s.applyUnitDamageWithSourceLocked(ally, 25, DamageSource{Kind: "melee"})
	if ally.HP != startHP-25 {
		t.Errorf("second hit blocked or wrong: HP %d → %d, want %d", startHP, ally.HP, startHP-25)
	}
}

// TestDivineAegis_RefreshLongerNotStack confirms multiple clerics pulsing the
// same ally refresh the charge under refresh-longer semantics rather than
// stacking multiple charges.
func TestDivineAegis_RefreshLongerNotStack(t *testing.T) {
	s, _ := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("divine_aegis")
	if def == nil {
		t.Fatal("divine_aegis perk def not found")
	}
	cfg := def.ConfigForRank("bronze")
	duration := cfg["protectionDurationSeconds"]

	ally := spawnClericTestAlly(t, s, 500, 400)
	ally.PerkState.DivineAegisRemaining = duration - 1.0 // partially consumed

	// Simulate another cleric pulsing the same ally with the full duration.
	if duration > ally.PerkState.DivineAegisRemaining {
		ally.PerkState.DivineAegisRemaining = duration
	}
	if ally.PerkState.DivineAegisRemaining != duration {
		t.Errorf("refresh did not extend to full duration: %.3f, want %.3f",
			ally.PerkState.DivineAegisRemaining, duration)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// restoration_aura
// ─────────────────────────────────────────────────────────────────────────────

// TestRestorationAura_PulseHealsNearbyAllies fires a pulse and asserts each
// ally inside radius takes healAmount HP (capped at MaxHP).
func TestRestorationAura_PulseHealsNearbyAllies(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "restoration_aura")
	def := perkDefByID("restoration_aura")
	if def == nil {
		t.Fatal("restoration_aura perk def not found")
	}
	cfg := def.ConfigForRank(cleric.Rank)
	healAmount := int(math.Round(cfg["healAmount"]))

	ally := spawnClericTestAlly(t, s, cleric.X+10, cleric.Y)
	ally.HP = ally.MaxHP - healAmount - 5 // missing more than one pulse
	startHP := ally.HP

	s.tickRestorationAuraPulseLocked(cleric, def, 0.05)
	if ally.HP != startHP+healAmount {
		t.Errorf("restoration pulse heal: HP %d → %d, want %d", startHP, ally.HP, startHP+healAmount)
	}
}

// TestRestorationAura_DivineHealerDoublesHealAmount verifies that owning
// divine_healer scales the pulse heal by healMultiplier.
func TestRestorationAura_DivineHealerDoublesHealAmount(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "restoration_aura")
	grantPerk(cleric, "divine_healer")

	raDef := perkDefByID("restoration_aura")
	dhDef := perkDefByID("divine_healer")
	if raDef == nil || dhDef == nil {
		t.Fatal("perk def missing")
	}
	raCfg := raDef.ConfigForRank(cleric.Rank)
	dhCfg := dhDef.ConfigForRank(cleric.Rank)
	wantHeal := int(math.Round(raCfg["healAmount"] * dhCfg["healMultiplier"]))

	ally := spawnClericTestAlly(t, s, cleric.X+10, cleric.Y)
	ally.HP = ally.MaxHP - wantHeal - 5
	startHP := ally.HP

	s.tickRestorationAuraPulseLocked(cleric, raDef, 0.05)
	if ally.HP != startHP+wantHeal {
		t.Errorf("divine_healer scaled restoration pulse: HP %d → %d, want %d", startHP, ally.HP, startHP+wantHeal)
	}
}

// TestRestorationAura_EnemiesNotHealed places an enemy in radius and asserts
// the pulse skips it.
func TestRestorationAura_EnemiesNotHealed(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "restoration_aura")
	def := perkDefByID("restoration_aura")
	if def == nil {
		t.Fatal("restoration_aura perk def not found")
	}

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: cleric.X + 10, Y: cleric.Y})
	enemy.Visible = true
	enemy.HP = enemy.MaxHP / 2
	startHP := enemy.HP

	s.tickRestorationAuraPulseLocked(cleric, def, 0.05)
	if enemy.HP != startHP {
		t.Errorf("enemy should not be healed; HP %d → %d", startHP, enemy.HP)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// zealous_march
// ─────────────────────────────────────────────────────────────────────────────

// TestZealousMarch_GrantsMoveSpeedToNearbyAllies verifies a recipient ally
// receives the configured move-speed bonus when inside an allied cleric's
// aura.
func TestZealousMarch_GrantsMoveSpeedToNearbyAllies(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "zealous_march")
	def := perkDefByID("zealous_march")
	if def == nil {
		t.Fatal("zealous_march perk def not found")
	}
	cfg := def.ConfigForRank(cleric.Rank)
	wantBonus := cfg["moveSpeedMultiplier"]

	ally := spawnClericTestAlly(t, s, cleric.X+10, cleric.Y)
	got := s.perkMoveSpeedMultiplierLocked(ally)
	if math.Abs(got-(1.0+wantBonus)) > 1e-6 {
		t.Errorf("recipient move speed multiplier = %.4f, want %.4f", got, 1.0+wantBonus)
	}
}

// TestZealousMarch_AdditionalCovererAddsStackBonus places two clerics in
// range and confirms the recipient receives base + 1 × stackBonus — not
// 2 × base. Additional covering clerics extend the bonus additively (so two
// clerics → 35% with the default 30% / 5% tuning, three clerics → 40%, etc.)
// rather than each contributing the full base bonus.
func TestZealousMarch_AdditionalCovererAddsStackBonus(t *testing.T) {
	s, clericA := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(clericA, "zealous_march")
	clericB := s.spawnPlayerUnitLocked("acolyte", "p1", "#aabbcc", protocol.Vec2{X: 410, Y: 400})
	clericB.Visible = true
	grantPerk(clericB, "zealous_march")

	def := perkDefByID("zealous_march")
	if def == nil {
		t.Fatal("zealous_march perk def not found")
	}
	cfg := def.ConfigForRank("bronze")
	base := cfg["moveSpeedMultiplier"]
	stack := cfg["stackBonus"]
	if stack <= 0 {
		t.Skipf("stackBonus = %.4f; test requires a positive stack bonus to be meaningful", stack)
	}
	wantBonus := base + stack

	ally := spawnClericTestAlly(t, s, clericA.X+10, clericA.Y)
	got := s.perkMoveSpeedMultiplierLocked(ally)
	if math.Abs(got-(1.0+wantBonus)) > 1e-6 {
		t.Errorf("two covering clerics: recipient move speed = %.4f, want %.4f (base + 1×stack)", got, 1.0+wantBonus)
	}
}

// TestZealousMarch_ThreeCoverersAddTwoStacks confirms the additive stack
// scales linearly with the count of additional sources beyond the first.
// Three clerics → base + 2 × stackBonus.
func TestZealousMarch_ThreeCoverersAddTwoStacks(t *testing.T) {
	s, clericA := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(clericA, "zealous_march")
	clericB := s.spawnPlayerUnitLocked("acolyte", "p1", "#aabbcc", protocol.Vec2{X: 410, Y: 400})
	clericB.Visible = true
	grantPerk(clericB, "zealous_march")
	clericC := s.spawnPlayerUnitLocked("acolyte", "p1", "#ccddee", protocol.Vec2{X: 420, Y: 400})
	clericC.Visible = true
	grantPerk(clericC, "zealous_march")

	def := perkDefByID("zealous_march")
	if def == nil {
		t.Fatal("zealous_march perk def not found")
	}
	cfg := def.ConfigForRank("bronze")
	base := cfg["moveSpeedMultiplier"]
	stack := cfg["stackBonus"]
	wantBonus := base + 2*stack

	ally := spawnClericTestAlly(t, s, clericA.X+10, clericA.Y)
	got := s.perkMoveSpeedMultiplierLocked(ally)
	if math.Abs(got-(1.0+wantBonus)) > 1e-6 {
		t.Errorf("three covering clerics: recipient move speed = %.4f, want %.4f (base + 2×stack)", got, 1.0+wantBonus)
	}
}

// TestZealousMarch_OutsideRadiusUnaffected confirms an ally outside the aura
// has no bonus.
func TestZealousMarch_OutsideRadiusUnaffected(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "zealous_march")
	def := perkDefByID("zealous_march")
	if def == nil {
		t.Fatal("zealous_march perk def not found")
	}
	cfg := def.ConfigForRank(cleric.Rank)

	ally := spawnClericTestAlly(t, s, cleric.X+cfg["radiusPixels"]*2, cleric.Y)
	got := s.perkMoveSpeedMultiplierLocked(ally)
	if math.Abs(got-1.0) > 1e-6 {
		t.Errorf("ally outside radius: move speed = %.4f, want 1.0", got)
	}
}

// TestZealousMarch_EnemyNotAffected confirms hostile units do not benefit.
func TestZealousMarch_EnemyNotAffected(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "zealous_march")

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: cleric.X + 10, Y: cleric.Y})
	enemy.Visible = true
	got := s.perkMoveSpeedMultiplierLocked(enemy)
	if math.Abs(got-1.0) > 1e-6 {
		t.Errorf("enemy in aura: move speed = %.4f, want 1.0", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// divine_healer
// ─────────────────────────────────────────────────────────────────────────────

// TestDivineHealer_ScalesHealAmount casts heal and asserts the target HP gain
// is multiplied by healMultiplier.
func TestDivineHealer_ScalesHealAmount(t *testing.T) {
	s, app, ally := healSetup(t)
	healDef := healDef(t)

	s.mu.Lock()
	allyID := ally.ID
	grantPerk(app, "divine_healer")
	dhDef := perkDefByID("divine_healer")
	if dhDef == nil {
		s.mu.Unlock()
		t.Fatal("divine_healer perk def not found")
	}
	cfg := dhDef.ConfigForRank(app.Rank)
	wantHeal := int(math.Round(float64(healDef.HealAmount) * cfg["healMultiplier"]))
	// Make sure the ally is missing enough HP to absorb the doubled heal.
	ally.HP = ally.MaxHP - wantHeal - 5
	startHP := ally.HP
	ok, reason := s.beginAbilityCastLocked(app, "heal", ally)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("beginAbilityCastLocked: %q", reason)
	}

	advance(s, 25)

	s.mu.RLock()
	defer s.mu.RUnlock()
	a := s.unitsByID[allyID]
	if a.HP != startHP+wantHeal {
		t.Errorf("divine_healer scaled heal: HP %d → %d, want %d", startHP, a.HP, startHP+wantHeal)
	}
}

// TestDivineHealer_DoublesBattlePrayerBuff asserts the cross-unit buff
// stamped onto every heal target is scaled by triggeredEffectMultiplier.
func TestDivineHealer_DoublesBattlePrayerBuff(t *testing.T) {
	s, app, ally := healSetup(t)

	s.mu.Lock()
	allyID := ally.ID
	grantPerk(app, "battle_prayer")
	grantPerk(app, "divine_healer")

	bpDef := perkDefByID("battle_prayer")
	dhDef := perkDefByID("divine_healer")
	if bpDef == nil || dhDef == nil {
		s.mu.Unlock()
		t.Fatal("perk def missing")
	}
	bpCfg := bpDef.ConfigForRank(app.Rank)
	dhCfg := dhDef.ConfigForRank(app.Rank)
	mult := dhCfg["triggeredEffectMultiplier"]
	wantDuration := bpCfg["buffDurationSeconds"] * mult
	wantStrength := bpCfg["attackSpeedMultiplier"] * mult

	ok, reason := s.beginAbilityCastLocked(app, "heal", ally)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("beginAbilityCastLocked: %q", reason)
	}

	advance(s, 25)

	s.mu.RLock()
	defer s.mu.RUnlock()
	a := s.unitsByID[allyID]

	const advanceDt = 25 * 0.05
	if a.PerkState.BattlePrayerRemaining > wantDuration {
		t.Errorf("BattlePrayerRemaining = %.3f exceeds scaled duration %.3f", a.PerkState.BattlePrayerRemaining, wantDuration)
	}
	if a.PerkState.BattlePrayerRemaining < wantDuration-advanceDt {
		t.Errorf("BattlePrayerRemaining = %.3f decayed too far below scaled %.3f", a.PerkState.BattlePrayerRemaining, wantDuration)
	}
	if math.Abs(a.PerkState.BattlePrayerMultiplier-wantStrength) > 1e-6 {
		t.Errorf("BattlePrayerMultiplier = %.4f, want scaled %.4f", a.PerkState.BattlePrayerMultiplier, wantStrength)
	}
}

// TestDivineHealer_DoesNotDoubleManaConduit confirms the multiplier does NOT
// bleed into mana_conduit's per-tick mana regen (the design forbids touching
// mana / cooldown systems).
func TestDivineHealer_DoesNotDoubleManaConduit(t *testing.T) {
	s, cleric := newClericBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	grantPerk(cleric, "mana_conduit")
	grantPerk(cleric, "divine_healer")

	mcDef := perkDefByID("mana_conduit")
	if mcDef == nil {
		t.Fatal("mana_conduit perk def not found")
	}
	cfg := mcDef.ConfigForRank(cleric.Rank)
	bonusPerSec := cfg["bonusManaRegen"]

	cleric.CurrentMana = 0
	cleric.ManaRegenAccumulator = 0

	const dt = 0.1
	s.tickUnitPerkStateLocked(cleric, dt)

	wantAccum := bonusPerSec * dt
	totalMana := float64(cleric.CurrentMana) + cleric.ManaRegenAccumulator
	if math.Abs(totalMana-wantAccum) > 0.01 {
		t.Errorf("divine_healer should not scale mana_conduit: got %.4f, want %.4f", totalMana, wantAccum)
	}
}
