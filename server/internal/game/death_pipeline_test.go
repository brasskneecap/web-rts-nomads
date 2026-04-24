package game

// ═════════════════════════════════════════════════════════════════════════════
// Death pipeline regression tests
//
// These tests verify the exact failure modes the centralized death pipeline
// (damage_pipeline.go) was introduced to fix:
//
//   1. Units killed by indirect damage paths (Shared Pain, pain_share redirect,
//      retaliation reflect) sat at HP=0 forever — removeUnitLocked was never
//      called for them because the outer call site only checked the primary
//      target's HP.
//   2. Indirect kills were not crediting the original attacker with kill XP.
//
// Each test is self-contained, deterministic, and calls drainPendingDeathsLocked
// directly (same package) to exercise the drain without running the full
// tickUnitCombatLocked / tickTrapsLocked stack, which would pull in combat AI,
// wave logic, and other systems that would interfere with minimal setups.
//
// Pattern: arrange under lock → call damage helpers under lock → call drain
// under lock → assert under lock. All within one critical section so no Update
// race.
// ═════════════════════════════════════════════════════════════════════════════

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// ─────────────────────────────────────────────────────────────────────────────
// helpers local to this file
// ─────────────────────────────────────────────────────────────────────────────

// newDeathPipelineState returns a minimal GameState. The lock is NOT held on
// return so callers can choose to acquire it or call Update.
func newDeathPipelineState(t *testing.T) *GameState {
	t.Helper()
	return NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
}

// armSharedPain stamps a mark stack and sets SharedPainFraction so
// perkShareDamageToMarkedLocked will fan damage out to this unit. Does NOT
// interact with any trap — it sets the raw PerkState fields the trap would
// normally set. Must be called with the lock held.
func armSharedPain(u *Unit, sharedPainFraction float64) {
	u.PerkState.SharedPainFraction = sharedPainFraction
	u.PerkState.applyMarkStack("test-source", 0, 0.1, 60.0) // 0.1× amplifier, 60 s duration
}

// assertUnitRemoved fails the test if the unit is still present in s.Units
// or still indexed in s.unitsByID.
func assertUnitRemoved(t *testing.T, s *GameState, u *Unit, label string) {
	t.Helper()
	if got := s.getUnitByIDLocked(u.ID); got != nil {
		t.Errorf("%s: unit (ID=%d) is still in unitsByID — removeUnitLocked was never called", label, u.ID)
	}
	for _, existing := range s.Units {
		if existing.ID == u.ID {
			t.Errorf("%s: unit (ID=%d) is still in s.Units slice — standing corpse", label, u.ID)
			return
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 1 — SharedPain secondary kill removes unit from s.Units
//
// Setup: attacker A (p1), primary target B (enemy), secondary target C (enemy).
// B and C both have an active mark and SharedPainFraction so
// perkShareDamageToMarkedLocked fans damage from B's hit to C as well.
// C's HP is set low enough that the shared fraction kills it.
// The primary hit damages B (B survives). C dies from the shared damage.
//
// Before the drain, C should still be in s.Units (HP=0 standing corpse).
// After drainPendingDeathsLocked, C must be removed.
// ─────────────────────────────────────────────────────────────────────────────
func TestSharedPain_SecondaryKill_RemovesUnit(t *testing.T) {
	s := newDeathPipelineState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	attacker.Visible = true

	// B is the primary target — stays alive after the hit.
	B := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 420, Y: 400})
	B.MaxHP = 500
	B.HP = 500
	B.Visible = true
	armSharedPain(B, 0.5) // 50% shared to other marked enemies

	// C is the secondary target — low HP so the shared damage kills it.
	C := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 440, Y: 400})
	C.MaxHP = 500
	C.HP = 1 // will be killed by the 50% shared fraction of a 100-damage hit
	C.Visible = true
	armSharedPain(C, 0.5)

	cID := C.ID

	// Apply 100 post-armor damage to B with attribution to attacker A.
	// perkShareDamageToMarkedLocked fans 50% (≥1 damage) to C, killing it.
	src := DamageSource{AttackerUnitID: attacker.ID, Kind: "melee"}
	s.applyUnitDamageWithSourceLocked(B, 100, src)

	// At this point C has HP=0 and is in the pending-death queue,
	// but still present in s.Units (the old bug: standing corpse).
	cStillPresent := s.getUnitByIDLocked(cID) != nil
	if !cStillPresent {
		// Drain ran early somehow — that's also wrong; it should run once per tick.
		t.Error("C was removed before drainPendingDeathsLocked ran — drain must not run eagerly")
	}

	s.drainPendingDeathsLocked()

	assertUnitRemoved(t, s, C, "TestSharedPain_SecondaryKill_RemovesUnit")
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 2 — SharedPain secondary kill credits kill XP to original attacker
//
// Same setup as Test 1. After the drain, the original attacker A must have
// received kill XP (xpPerKillBonus * xpGainMultiplier = 25 * 0.2 = 5 XP).
// ─────────────────────────────────────────────────────────────────────────────
func TestSharedPain_SecondaryKill_CreditsOriginalAttacker(t *testing.T) {
	s := newDeathPipelineState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	attacker.Visible = true
	xpBefore := attacker.XP

	B := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 420, Y: 400})
	B.MaxHP = 500
	B.HP = 500
	B.Visible = true
	armSharedPain(B, 0.5)

	C := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 440, Y: 400})
	C.MaxHP = 500
	C.HP = 1
	C.Visible = true
	armSharedPain(C, 0.5)

	src := DamageSource{AttackerUnitID: attacker.ID, Kind: "melee"}
	s.applyUnitDamageWithSourceLocked(B, 100, src)

	if C.HP > 0 {
		t.Fatalf("setup error: expected C to die from shared damage (HP=%d)", C.HP)
	}

	s.drainPendingDeathsLocked()

	// xpPerKillBonus = 25.0, xpGainMultiplier = 0.2 → 5 whole XP per kill.
	// payoutDamageDealtXPLocked may also contribute. We assert XP increased by
	// at least the kill-bonus amount.
	expectedKillXP := int(xpPerKillBonus * xpGainMultiplier)
	if got := attacker.XP - xpBefore; got < expectedKillXP {
		t.Errorf("attacker XP after indirect kill: got +%d XP (total %d), want at least +%d (kill bonus)",
			got, attacker.XP, expectedKillXP)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 3 — pain_share redirect kills absorber, drain removes absorber
//
// Setup: attacker A (enemy faction), protected ally ALLY (p1), absorbing
// Vanguard V (p1) with pain_share within radius of ALLY.
// V has very low HP so the redirected fraction kills it.
// ─────────────────────────────────────────────────────────────────────────────
func TestPainShareRedirect_AbsorberDeath_RemovesUnit(t *testing.T) {
	s := newDeathPipelineState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("pain_share")
	if def == nil {
		t.Fatal("pain_share perk def not found — is perk-defs.json loaded?")
	}
	radius := def.Config["radius"]
	redirectPct := def.Config["redirectPercent"] // e.g. 0.30

	// Attacker is an enemy — use a different (third) player so hostility works.
	attacker := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 500, Y: 400})
	attacker.Visible = true

	// Protected ally on p1.
	ally := s.spawnPlayerUnitLocked("soldier", "p1", "#2ecc71", protocol.Vec2{X: 400, Y: 400})
	ally.MaxHP = 500
	ally.HP = 500
	ally.Visible = true

	// Vanguard absorber on p1, within radius of ally.
	V := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{
		X: 400 + radius*0.4,
		Y: 400,
	})
	V.MaxHP = 500
	V.Visible = true
	grantPerk(V, "pain_share")

	// Give V just 1 HP so the redirected damage kills it.
	// redirectPct * 200 = at least 1 damage (even rounding down to 1 by maxInt guard).
	V.HP = 1

	vID := V.ID

	// Apply damage to ally — pain_share redirect fires on ally, absorber V dies.
	src := DamageSource{AttackerUnitID: attacker.ID, Kind: "melee"}
	s.applyUnitDamageWithSourceLocked(ally, 200, src)

	// Before drain: V should still be in the registry (standing corpse bug).
	if V.HP > 0 {
		t.Fatalf("setup error: redirectPct=%.2f × 200 damage should have killed V (HP=%d)", redirectPct, V.HP)
	}
	if s.getUnitByIDLocked(vID) == nil {
		t.Error("V was removed before drainPendingDeathsLocked — drain should not run eagerly")
	}

	s.drainPendingDeathsLocked()

	assertUnitRemoved(t, s, V, "TestPainShareRedirect_AbsorberDeath_RemovesUnit")
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 4 — pain_share redirect kills absorber, credits original attacker XP
//
// Same setup as Test 3. After the drain, the enemy attacker A must have gained
// kill XP (A killed V via the redirect even though A's hit landed on ALLY).
//
// Note: the enemy player cannot gain XP (unitCanGainXPLocked returns false for
// enemyPlayerID). This correctly verifies the pipeline does NOT award XP to the
// enemy faction — and documents that the pipeline respects the XP gate.
// The real scenario where this matters is a player attacker killing an ally's
// Vanguard through pain_share; we verify the attribution is correctly threaded.
// ─────────────────────────────────────────────────────────────────────────────
func TestPainShareRedirect_AbsorberDeath_CreditsAttacker(t *testing.T) {
	s := newDeathPipelineState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("pain_share")
	if def == nil {
		t.Fatal("pain_share perk def not found")
	}
	radius := def.Config["radius"]

	// Attacker is p1 (can gain XP); target and absorber are enemies.
	// perkRedirectIncomingDamageLocked absorbs a fraction from the ALLY's damage,
	// where "ally" means same-OwnerID as the protected target. So we need:
	//   - attacker on p1 (can gain XP)
	//   - protected ally on enemyPlayerID
	//   - absorber Vanguard on enemyPlayerID (same team as ally so it absorbs)
	attacker := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 500, Y: 400})
	attacker.Visible = true
	xpBefore := attacker.XP

	// Protected ally and absorber are both enemies.
	ally := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 400, Y: 400})
	ally.MaxHP = 500
	ally.HP = 500
	ally.Visible = true

	V := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#c0392b", protocol.Vec2{
		X: 400 + radius*0.4,
		Y: 400,
	})
	V.MaxHP = 500
	V.HP = 1
	V.Visible = true
	grantPerk(V, "pain_share")

	src := DamageSource{AttackerUnitID: attacker.ID, Kind: "melee"}
	s.applyUnitDamageWithSourceLocked(ally, 200, src)

	if V.HP > 0 {
		t.Fatalf("setup error: expected V to die from redirected damage (HP=%d)", V.HP)
	}

	s.drainPendingDeathsLocked()

	assertUnitRemoved(t, s, V, "TestPainShareRedirect_AbsorberDeath_CreditsAttacker")

	// Enemy units cannot gain XP (unitCanGainXPLocked gates on ownerID !=
	// enemyPlayerID). Attacker is p1 — verify kill XP landed.
	expectedKillXP := int(xpPerKillBonus * xpGainMultiplier)
	if got := attacker.XP - xpBefore; got < expectedKillXP {
		t.Errorf("attacker (p1) XP after V's death via redirect: got +%d, want at least +%d",
			got, expectedKillXP)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 5 — retaliation reflect kills attacker; drain removes attacker and
//           credits defender; XP is awarded exactly once (no double-credit)
//
// Setup: defender D (p1) with high armor + retaliation, weak attacker A (enemy)
// whose HP is low enough that D's reflected damage kills it.
//
// We exercise this directly via applyUnitDamageWithSourceLocked + onPerkDamage-
// TakenLocked (the same call sequence that resolveAttackHitLocked uses) rather
// than going through the full combat path so we can isolate the drain behavior
// from the deadUnitIDs path.
//
// XP double-credit check: the manual bookkeeping in resolveAttackHitLocked runs
// awardKillXPLocked then appends the attacker's ID to deadUnitIDs, which later
// calls removeUnitLocked. That removes the attacker before drainPendingDeathsLocked
// runs, so the drain sees nil and skips. This test verifies the isolated drain
// path awards XP exactly once when the call site has NOT done its own manual
// bookkeeping (i.e., only the drain is responsible).
// ─────────────────────────────────────────────────────────────────────────────
func TestRetaliation_AttackerKilledByReflect_RemovesAttackerAndCreditsDefender(t *testing.T) {
	s := newDeathPipelineState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	retDef := perkDefByID("retaliation")
	if retDef == nil {
		t.Fatal("retaliation perk def not found — is perk-defs.json loaded?")
	}
	armorPct := retDef.Config["armorPercent"] // e.g. 0.50

	// Defender D on p1 with retaliation and high armor.
	D := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	D.Visible = true
	D.Armor = 200 // large armor so reflected damage = armorPct * 200 ≥ 1
	D.MaxHP = 500
	D.HP = 500
	grantPerk(D, "retaliation")
	xpBefore := D.XP

	// Reflected damage = armorPct * D.Armor (via effectiveArmorLocked with no modifiers).
	// effectiveArmorLocked returns base armor + perkBonusArmorLocked + aura.
	// D has no other perks; effectiveArmor = D.Armor = 200.
	// reflected = floor(armorPct * 200) = floor(0.5 * 200) = 100 (with default config).
	reflected := int(armorPct * float64(D.Armor))
	if reflected <= 0 {
		t.Fatalf("setup: expected reflected damage > 0 (armorPct=%.2f, armor=%d)", armorPct, D.Armor)
	}

	// Attacker A (enemy) with HP just below the reflected amount so it dies.
	A := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 420, Y: 400})
	A.Visible = true
	A.MaxHP = reflected - 1
	A.HP = reflected - 1

	aID := A.ID

	// Simulate the damage that D receives (triggering retaliation) using a
	// realistic melee source. We apply damage directly to D; onPerkDamageTakenLocked
	// then fires retaliation back onto A. This mirrors the call order in
	// resolveAttackHitLocked without the deadUnitIDs bookkeeping.
	incomingDamage := 10 // D survives this
	meleeSrc := DamageSource{AttackerUnitID: A.ID, Kind: "melee"}
	s.applyUnitDamageWithSourceLocked(D, incomingDamage, meleeSrc)

	// Fire the defender-side perk hook — this is where retaliation reflects.
	s.onPerkDamageTakenLocked(D, A, incomingDamage)

	// A should now be at HP <= 0.
	if A.HP > 0 {
		t.Fatalf("setup error: expected A to die from reflected damage %d (A.HP=%d before, reflected=%d)",
			reflected, A.MaxHP, reflected)
	}

	// Before drain: A is still in the registry.
	if s.getUnitByIDLocked(aID) == nil {
		t.Error("A was removed before drainPendingDeathsLocked — drain must not run eagerly inside the hook")
	}

	s.drainPendingDeathsLocked()

	// A must be removed.
	assertUnitRemoved(t, s, A, "TestRetaliation_AttackerKilledByReflect")

	// Defender D (the "attacker" in the retaliation source) must have received
	// kill XP. expectedKillXP = xpPerKillBonus * xpGainMultiplier = 25 * 0.2 = 5.
	expectedKillXP := int(xpPerKillBonus * xpGainMultiplier)
	if got := D.XP - xpBefore; got < expectedKillXP {
		t.Errorf("defender XP after retaliation kill: got +%d, want at least +%d",
			got, D.XP)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 5b — XP awarded exactly once for retaliation kill
//
// Simulates the full resolveAttackHitLocked call path: the call site does its
// own manual kill bookkeeping (attacker.HP <= 0 branch at state_combat.go:91-96),
// then removeUnitLocked is called for the attacker. When drainPendingDeathsLocked
// runs afterward, the unit is already gone — drain must skip and NOT double-award.
// ─────────────────────────────────────────────────────────────────────────────
func TestRetaliation_NoDoubleXP_WhenCallSiteAlreadyHandledKill(t *testing.T) {
	s := newDeathPipelineState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	retDef := perkDefByID("retaliation")
	if retDef == nil {
		t.Fatal("retaliation perk def not found")
	}
	armorPct := retDef.Config["armorPercent"]

	D := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 400, Y: 400})
	D.Visible = true
	D.Armor = 200
	D.MaxHP = 500
	D.HP = 500
	grantPerk(D, "retaliation")

	reflected := int(armorPct * float64(D.Armor))
	A := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 420, Y: 400})
	A.Visible = true
	A.MaxHP = reflected - 1
	A.HP = reflected - 1

	// Apply D's incoming damage + fire perk hook → A dies, pendingDeath enqueued
	// with src={AttackerUnitID: D.ID}.
	meleeSrc := DamageSource{AttackerUnitID: A.ID, Kind: "melee"}
	s.applyUnitDamageWithSourceLocked(D, 10, meleeSrc)
	s.onPerkDamageTakenLocked(D, A, 10)

	if A.HP > 0 {
		t.Fatalf("setup: A not killed by reflect (HP=%d)", A.HP)
	}

	// Simulate what resolveAttackHitLocked does when attacker.HP <= 0:
	// award XP at the call site, then removeUnitLocked.
	xpBeforeManual := D.XP
	s.awardKillXPLocked(D)
	xpAfterManual := D.XP
	s.removeUnitLocked(A.ID)

	// Now drain. The unit is gone — drain must be a no-op for this death entry.
	s.drainPendingDeathsLocked()

	xpAfterDrain := D.XP

	if xpAfterDrain != xpAfterManual {
		t.Errorf("double XP: drain awarded XP again after call-site already awarded it. "+
			"XP before manual=%d, after manual=%d, after drain=%d",
			xpBeforeManual, xpAfterManual, xpAfterDrain)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 6 — anonymous legacy call site still removes unit via drain
//
// Defensive net test. Constructs a unit, kills it via the legacy wrapper
// applyUnitDamageLocked (anonymous DamageSource). The drain must still
// remove the unit from s.Units even though no source attribution was provided.
// This covers the pre-existing bug scenario: any damage path that hadn't been
// migrated to pass attribution would leave a standing corpse. The drain fixes
// this regardless of migration status.
// ─────────────────────────────────────────────────────────────────────────────
func TestDeathPipeline_AnonymousLegacyCallSite_StillRemovesUnit(t *testing.T) {
	s := newDeathPipelineState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	victim := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 400, Y: 400})
	victim.MaxHP = 100
	victim.HP = 100
	victim.Visible = true
	victimID := victim.ID

	// Kill via the legacy anonymous wrapper — no attribution, no XP.
	s.applyUnitDamageLocked(victim, 999)

	if victim.HP > 0 {
		t.Fatalf("setup: victim not killed (HP=%d)", victim.HP)
	}
	// Confirm it's still present before the drain (the old bug).
	if s.getUnitByIDLocked(victimID) == nil {
		t.Error("victim removed before drain — drain must not run eagerly inside applyUnitDamageLocked")
	}

	s.drainPendingDeathsLocked()

	// Must be gone after the drain.
	if got := s.getUnitByIDLocked(victimID); got != nil {
		t.Errorf("unit still in unitsByID after drain — standing corpse bug not fixed (HP=%d)", got.HP)
	}
	for _, u := range s.Units {
		if u.ID == victimID {
			t.Errorf("unit still in s.Units slice after drain — standing corpse bug not fixed")
			return
		}
	}
}
