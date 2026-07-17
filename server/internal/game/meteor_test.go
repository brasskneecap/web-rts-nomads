package game

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// meteorDef returns the frozen pre-migration Meteor ability shape
// (ability_legacy_fixtures_test.go). Catalog "meteor" is schemaVersion:2 as
// of the composable-abilities migration (mechanic fields cleared, Program is
// authoritative — see TestAbilityCompileGolden_Meteor for that path's own
// coverage), so it no longer carries ImpactDelaySeconds/Radius/Burn* etc.
// directly. The tests below are specifically about the LEGACY delayed-impact
// + GroundHazard resolve path (resolveAbilityCastAtPointLocked's flat-field
// branch, spawnGroundHazardLocked), which is still live production code —
// just no longer reachable via the "meteor" id — so they exercise it through
// the frozen fixture registered under a synthetic id instead.
func meteorDef(t *testing.T) AbilityDef {
	t.Helper()
	return legacyMeteorFixture()
}

// legacyMeteorTestAbilityID registers the frozen pre-migration meteor fixture
// under a synthetic ability id so a test can drive it through the real
// beginAbilityCastAtPointLocked entry point and observe the LEGACY
// GroundHazard mechanism (s.GroundHazards) end-to-end, exactly as it behaved
// before the migration.
func legacyMeteorTestAbilityID(t *testing.T) string {
	t.Helper()
	def := legacyMeteorFixture()
	def.ID = "meteor_legacy_ground_hazard_test"
	registerRuntimeTestAbility(t, def)
	return def.ID
}

func TestMeteorDef_ParsesConfigFields(t *testing.T) {
	def := meteorDef(t)
	if !def.TargetsPoint {
		t.Error("meteor must be a point-target spell (targetsPoint:true)")
	}
	if def.ImpactDelaySeconds <= 0 {
		t.Errorf("ImpactDelaySeconds = %v; want > 0", def.ImpactDelaySeconds)
	}
	if def.BurnDurationSeconds <= 0 {
		t.Errorf("BurnDurationSeconds = %v; want > 0", def.BurnDurationSeconds)
	}
	if def.BurnTickIntervalSeconds <= 0 {
		t.Errorf("BurnTickIntervalSeconds = %v; want > 0", def.BurnTickIntervalSeconds)
	}
	if def.BurnDamagePerTick <= 0 {
		t.Errorf("BurnDamagePerTick = %v; want > 0", def.BurnDamagePerTick)
	}
	if def.BurnRadius <= 0 {
		t.Errorf("BurnRadius = %v; want > 0", def.BurnRadius)
	}
	if def.DamageAmount <= 0 || def.Radius <= 0 {
		t.Errorf("impact damage/radius must be set: DamageAmount=%v Radius=%v", def.DamageAmount, def.Radius)
	}
}

// TestMeteor_PointCastSpawnsHazardAndEffect verifies a ground cast: spends mana,
// queues the world-anchored meteor effect at the clamped point, spawns a
// GroundHazard, deals no damage until the fall delay elapses, then impacts.
func TestMeteor_PointCastSpawnsHazardAndEffect(t *testing.T) {
	def := meteorDef(t)
	abilityID := legacyMeteorTestAbilityID(t)
	s := newProjectileTestState(t)
	s.mu.Lock()
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	caster := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
	caster.Visible = true
	caster.CurrentMana = def.ManaCost + 10
	caster.Abilities = append(caster.Abilities, abilityID) // grant it for the test
	enemy := spawnEnemy(t, s, 360, 300)                    // within impact radius of the cast point
	enemyID := enemy.ID
	startHP := enemy.HP
	startMana := caster.CurrentMana

	ok, reason := s.beginAbilityCastAtPointLocked(caster, abilityID, 360, 300)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("beginAbilityCastAtPointLocked failed: %q", reason)
	}

	// Meteor has a non-zero castTime (wind-up): the hazard does not spawn at
	// initiation, only once the cast timer elapses (see tickUnitCastLocked's
	// CastIsPoint branch). Advance strictly PAST CastTime so the cast resolves,
	// but only a couple of ticks past it so we land safely BEFORE impact
	// (impact is CastTime + ImpactDelaySeconds after cast start, and
	// ImpactDelaySeconds is comfortably larger than a couple of ticks here).
	advance(s, int(def.CastTime/0.05)+2)
	s.mu.RLock()
	if len(s.GroundHazards) != 1 {
		t.Fatalf("expected 1 GroundHazard after cast completes; got %d", len(s.GroundHazards))
	}
	if caster.CurrentMana != startMana-def.ManaCost {
		t.Errorf("mana = %d; want %d (spent on resolution)", caster.CurrentMana, startMana-def.ManaCost)
	}
	if queuedEffectFor(s, def.EffectAtPoint, 0) == nil { // anchorUnitID 0 == world-anchored
		t.Error("meteor effect should have been queued at the cast point")
	}
	preImpactHP := s.unitsByID[enemyID].HP
	s.mu.RUnlock()
	if preImpactHP != startHP {
		t.Fatalf("enemy damaged before impact: HP=%d want %d", preImpactHP, startHP)
	}

	advance(s, int(def.ImpactDelaySeconds/0.05)+3)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.unitsByID[enemyID].HP >= startHP {
		t.Errorf("enemy should have taken meteor impact damage: HP=%d want < %d", s.unitsByID[enemyID].HP, startHP)
	}
}

// TestMeteor_TimedPointCast verifies castTime > 0 on a point spell: the caster
// is locked casting, mana is spent only on completion, and the hazard is spawned
// when the cast timer elapses (not at initiation).
func TestMeteor_TimedPointCast(t *testing.T) {
	def := meteorDef(t)
	abilityID := legacyMeteorTestAbilityID(t)
	if def.CastTime <= 0 {
		t.Skip("meteor castTime is 0; timed-point-cast path not exercised")
	}
	s := newProjectileTestState(t)
	s.mu.Lock()
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	caster := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
	caster.Visible = true
	caster.CurrentMana = def.ManaCost + 10
	caster.Abilities = append(caster.Abilities, abilityID)
	startMana := caster.CurrentMana
	ok, reason := s.beginAbilityCastAtPointLocked(caster, abilityID, 360, 300)
	casting := caster.CastAbilityID
	manaAtStart := caster.CurrentMana
	s.mu.Unlock()
	if !ok {
		t.Fatalf("cast failed: %q", reason)
	}
	if casting != abilityID {
		t.Errorf("caster should be locked casting meteor mid-cast; CastAbilityID=%q", casting)
	}
	if manaAtStart != startMana {
		t.Errorf("mana must not be spent at cast start: %d want %d", manaAtStart, startMana)
	}

	// Before the cast time elapses: no hazard yet.
	advance(s, int(def.CastTime/0.05)-1)
	s.mu.RLock()
	mid := len(s.GroundHazards)
	s.mu.RUnlock()
	if mid != 0 {
		t.Fatalf("hazard spawned before cast completed: %d", mid)
	}

	// After cast completes: hazard spawned, mana spent, cast cleared.
	advance(s, 3)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.GroundHazards) != 1 {
		t.Errorf("expected hazard after cast completes; got %d", len(s.GroundHazards))
	}
	if caster.CurrentMana != startMana-def.ManaCost {
		t.Errorf("mana = %d; want %d (spent on completion)", caster.CurrentMana, startMana-def.ManaCost)
	}
	if caster.CastAbilityID != "" {
		t.Errorf("cast should be cleared; CastAbilityID=%q", caster.CastAbilityID)
	}
}

// TestMeteor_ImpactQueuesLingeringCraterEffect verifies the burning-crater VFX
// is queued only after the fall/impact animation ENDS (not at impact), so it
// doesn't double up with the meteor's own crater frames, and lives at the world
// point. The crater appears at castTime + meteor-effect duration.
func TestMeteor_ImpactQueuesLingeringCraterEffect(t *testing.T) {
	def := meteorDef(t)
	abilityID := legacyMeteorTestAbilityID(t)
	if def.BurnEffectAtPoint == "" {
		t.Skip("meteor has no burnEffectAtPoint configured")
	}
	fallFx, ok := getEffectDef(def.EffectAtPoint)
	if !ok {
		t.Fatalf("meteor EffectAtPoint %q not registered", def.EffectAtPoint)
	}
	// The test only makes sense if impact precedes the animation end (otherwise
	// there's no window in which the crater is deliberately withheld).
	if def.ImpactDelaySeconds >= fallFx.Duration {
		t.Fatalf("test assumes impact precedes animation end; impactDelay=%v animDur=%v",
			def.ImpactDelaySeconds, fallFx.Duration)
	}

	s := newProjectileTestState(t)
	s.mu.Lock()
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	caster := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
	caster.Visible = true
	caster.CurrentMana = def.ManaCost + 10
	caster.Abilities = append(caster.Abilities, abilityID)
	ok, reason := s.beginAbilityCastAtPointLocked(caster, abilityID, 360, 300)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("cast failed: %q", reason)
	}

	// Past impact but BEFORE the fall/impact animation ends: crater withheld.
	// Impact = castTime+impactDelay; animation ends = castTime+fallFx.Duration.
	midpoint := def.CastTime + def.ImpactDelaySeconds + (fallFx.Duration-def.ImpactDelaySeconds)/2
	advance(s, int(midpoint/0.05))
	s.mu.RLock()
	craterMid := queuedEffectFor(s, def.BurnEffectAtPoint, 0)
	s.mu.RUnlock()
	if craterMid != nil {
		t.Error("burning crater should NOT be queued until the meteor animation ends")
	}

	// Past the animation end: crater queued at the world point (anchor 0).
	advance(s, int((fallFx.Duration+0.3)/0.05))
	s.mu.RLock()
	defer s.mu.RUnlock()
	if queuedEffectFor(s, def.BurnEffectAtPoint, 0) == nil {
		t.Errorf("burning crater %q should be queued after the meteor animation ends", def.BurnEffectAtPoint)
	}
}

// TestArchMageSpells_AutoCastOnByDefault guards the design rule that every
// castable Arch Mage pool spell auto-casts by default — players shouldn't need
// micro-management to start (they can still toggle it off). Passives are exempt.
func TestArchMageSpells_AutoCastOnByDefault(t *testing.T) {
	// "silver" resolves to the full shared pool (Bronze ∪ Silver); Gold grants
	// no pool spell so it can't be used to enumerate the pool.
	pool := spellPoolFor("arch_mage", "silver")
	if len(pool) == 0 {
		t.Fatal("arch_mage pool is empty")
	}
	for _, id := range pool {
		def, ok := getAbilityDef(id)
		if !ok {
			t.Errorf("pool spell %q has no AbilityDef", id)
			continue
		}
		if def.IsPassive() {
			continue // passives are driven by their own system, not auto-cast
		}
		if !def.SupportsAutoCast || !def.DefaultAutoCast {
			t.Errorf("%q: want auto-cast on by default; got supportsAutoCast=%v defaultAutoCast=%v",
				id, def.SupportsAutoCast, def.DefaultAutoCast)
		}
	}
}

// TestArchMage_SilverAutoCastOutranksBronze verifies the auto-cast priority: an
// Arch Mage with two ready pool spells (one earned at Bronze, one at Silver)
// targeting the same enemy scores the SILVER spell strictly higher, so the
// selection loop casts it first. Both spells share a category and selector, so
// without the rank priority they would tie.
func TestArchMage_SilverAutoCastOutranksBronze(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	caster := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
	caster.Visible = true
	// Two offensive pool spells learned at different ranks (same target/selector).
	caster.PoolSpellsByRank = map[string]string{"bronze": "fireball", "silver": "shatter"}
	enemy := spawnEnemy(t, s, 360, 300)

	bronzeDef, ok := getAbilityDef("fireball")
	if !ok {
		t.Fatal("fireball def missing")
	}
	silverDef, ok := getAbilityDef("shatter")
	if !ok {
		t.Fatal("shatter def missing")
	}
	bronzeScore := s.scoreAutoCastCandidateLocked(caster, bronzeDef, enemy)
	silverScore := s.scoreAutoCastCandidateLocked(caster, silverDef, enemy)
	if silverScore <= bronzeScore {
		t.Errorf("silver spell must outrank bronze in auto-cast: silver(shatter)=%v bronze(fireball)=%v",
			silverScore, bronzeScore)
	}
}

// TestAbilityGlobalCooldown_BlocksSecondCast verifies the 1s global cooldown:
// initiating any ability arms it, a second ability is blocked while it is
// active, and casting is allowed again once it elapses. Uses two distinct
// instant point spells so only the GCD (not a per-ability cooldown) gates the
// second cast.
func TestAbilityGlobalCooldown_BlocksSecondCast(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	caster := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
	caster.Visible = true
	caster.CurrentMana = 1000
	caster.Abilities = append(caster.Abilities, "shatter", "arcane_orb")

	ok, reason := s.beginAbilityCastAtPointLocked(caster, "shatter", 360, 300)
	if !ok {
		t.Fatalf("first cast failed: %q", reason)
	}
	if caster.GlobalCooldownRemaining <= 0 {
		t.Fatal("global cooldown should be armed after a cast")
	}
	// Second, different ability immediately: blocked by the GCD alone.
	ok, reason = s.beginAbilityCastAtPointLocked(caster, "arcane_orb", 360, 300)
	if ok || reason != castFailGlobalCooldown {
		t.Errorf("second cast during GCD = (%v, %q); want (false, %q)", ok, reason, castFailGlobalCooldown)
	}
	s.mu.Unlock()

	// Advance past the GCD; a cast is allowed again.
	advance(s, int(abilityGlobalCooldownSeconds/0.05)+1)
	s.mu.Lock()
	defer s.mu.Unlock()
	if caster.GlobalCooldownRemaining > 0 {
		t.Errorf("GCD should have elapsed; remaining=%v", caster.GlobalCooldownRemaining)
	}
	if ok, reason = s.beginAbilityCastAtPointLocked(caster, "arcane_orb", 360, 300); !ok {
		t.Errorf("cast after GCD elapsed should succeed; got %q", reason)
	}
}

// TestAbilityGlobalCooldown_ShownInAbilitySnapshot verifies the GCD surfaces as
// a cooldown wipe on OTHER abilities (so the action bar shows the ~1s
// unavailability) even when those abilities have no cooldown of their own.
func TestAbilityGlobalCooldown_ShownInAbilitySnapshot(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	caster := s.spawnPlayerUnitLocked("acolyte", "p1", "#3498db", protocol.Vec2{X: 300, Y: 300})
	caster.Visible = true
	caster.CurrentMana = 1000
	caster.Abilities = append(caster.Abilities, "shatter", "arcane_orb")

	if ok, reason := s.beginAbilityCastAtPointLocked(caster, "shatter", 360, 300); !ok {
		t.Fatalf("cast failed: %q", reason)
	}
	var orb *protocol.AbilitySnapshot
	for i, snap := range s.abilityStatesLocked(caster) {
		if snap.ID == "arcane_orb" {
			orb = &s.abilityStatesLocked(caster)[i]
			break
		}
	}
	if orb == nil {
		t.Fatal("arcane_orb snapshot missing")
	}
	// arcane_orb has no cooldown of its own yet, so its wipe is driven purely by
	// the GCD: remaining > 0 and total == the GCD length.
	if orb.CooldownRemaining <= 0 {
		t.Errorf("GCD should surface as arcane_orb cooldownRemaining; got %v", orb.CooldownRemaining)
	}
	if orb.CooldownTotal != abilityGlobalCooldownSeconds {
		t.Errorf("GCD wipe total = %v; want %v (GCD length)", orb.CooldownTotal, abilityGlobalCooldownSeconds)
	}
}

// TestArchMage_MaxMPMultiplierScalesManaPool verifies the arch_mage path's
// per-rank maxMPMultiplier actually scales the caster's MaxMana. Expected value
// is derived from the catalog (unit def MaxMana × the path multiplier), never
// hardcoded, so tuning either JSON can't silently break this.
func TestArchMage_MaxMPMultiplierScalesManaPool(t *testing.T) {
	def, ok := getUnitDef("adept")
	if !ok || def.MaxMana <= 0 {
		t.Fatalf("adept def missing or has no mana pool: ok=%v maxMana=%d", ok, def.MaxMana)
	}
	mult := pathModifierFor("arch_mage", "bronze").MaxMPMultiplier
	if mult <= 1.0 {
		t.Fatalf("precondition: arch_mage bronze maxMPMultiplier should be > 1 to be meaningful; got %v", mult)
	}

	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Players["p1"] = &Player{ID: "p1", Resources: map[string]int{}}
	mage := s.spawnPlayerUnitLocked("adept", "p1", "#3498db", protocol.Vec2{X: 100, Y: 100})
	mage.ProgressionPath = "arch_mage"
	mage.Rank = "bronze"
	s.applyRankModifiersLocked(mage, false)

	want := int(math.Round(float64(def.MaxMana) * mult))
	if mage.MaxMana != want {
		t.Errorf("bronze arch_mage MaxMana = %d; want %d (def %d × maxMPMultiplier %.2f)",
			mage.MaxMana, want, def.MaxMana, mult)
	}
}
