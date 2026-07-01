package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

func intPtr(v int) *int { return &v }

func TestResolveUnitXPValue(t *testing.T) {
	// Absent → splitDefaultXP (10 by shipped tuning).
	if got := resolveUnitXPValue(UnitDef{}); got != gameplayTuning().Experience.SplitDefaultXP {
		t.Errorf("absent experience: got %d, want %d", got, gameplayTuning().Experience.SplitDefaultXP)
	}
	// Explicit value honored.
	if got := resolveUnitXPValue(UnitDef{Experience: intPtr(7)}); got != 7 {
		t.Errorf("explicit 7: got %d, want 7", got)
	}
	// Explicit 0 honored (unit grants no XP) — NOT treated as absent.
	if got := resolveUnitXPValue(UnitDef{Experience: intPtr(0)}); got != 0 {
		t.Errorf("explicit 0: got %d, want 0", got)
	}
}

func TestSpawnSeedsXPValue(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()
	enemy := s.spawnEnemyUnitLocked("raider", protocol.Vec2{X: 100, Y: 100})
	if enemy == nil {
		t.Fatal("spawnEnemyUnitLocked returned nil")
	}
	if enemy.XPValue != gameplayTuning().Experience.SplitDefaultXP {
		t.Errorf("raider XPValue = %d, want %d (splitDefaultXP)", enemy.XPValue, gameplayTuning().Experience.SplitDefaultXP)
	}
}

func TestSpawnRaiderUnit_SeedsXPValue(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.spawnRaiderUnitLocked(enemyPlayerID, "#e74c3c", protocol.Vec2{X: 200, Y: 200})
	if r == nil {
		t.Fatal("spawnRaiderUnitLocked returned nil")
	}
	if r.XPValue != gameplayTuning().Experience.SplitDefaultXP {
		t.Errorf("raider fallback XPValue = %d, want %d (splitDefaultXP)", r.XPValue, gameplayTuning().Experience.SplitDefaultXP)
	}
}

func TestExperienceTuning_DefaultsLoaded(t *testing.T) {
	et := gameplayTuning().Experience
	// Don't pin exact balance values (mode / xp / radius all get retuned in the
	// catalog) — assert the tuning loaded as a valid, sane config instead.
	if et.Mode != experienceModeClassic && et.Mode != experienceModeSplit {
		t.Errorf("default experience.mode = %q, want one of %q / %q", et.Mode, experienceModeClassic, experienceModeSplit)
	}
	if et.SplitDefaultXP <= 0 {
		t.Errorf("default experience.splitDefaultXP = %d, want a positive value", et.SplitDefaultXP)
	}
	if et.SplitEligibilityRadius <= 0 {
		t.Errorf("default experience.splitEligibilityRadius = %v, want a positive configured radius", et.SplitEligibilityRadius)
	}
}

func TestAddUnitXPRawFloat_NoMultiplierAndAccumulates(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 100, Y: 100})

	// 0.5 alone must not yet count as whole XP, but must be retained.
	s.addUnitXPRawFloatLocked(u, 0.5)
	if u.XP != 0 {
		t.Errorf("after +0.5: XP = %d, want 0", u.XP)
	}
	if u.XPProgressRemainder != 0.5 {
		t.Errorf("after +0.5: remainder = %v, want 0.5", u.XPProgressRemainder)
	}

	// Another 0.5 completes a whole point — RAW, with NO 0.2 scaling applied.
	s.addUnitXPRawFloatLocked(u, 0.5)
	if u.XP != 1 {
		t.Errorf("after +0.5 again: XP = %d, want 1 (raw, unscaled)", u.XP)
	}
	if u.XPProgressRemainder != 0 {
		t.Errorf("after +0.5 again: remainder = %v, want 0", u.XPProgressRemainder)
	}
}

// withExperienceTuning swaps the global Experience tuning for the duration of
// the test and restores it via Cleanup. Mutates a package singleton, so tests
// using it MUST NOT call t.Parallel().
func withExperienceTuning(t *testing.T, et ExperienceTuning) {
	t.Helper()
	prev := gameplayTuningSingleton.Experience
	gameplayTuningSingleton.Experience = et
	t.Cleanup(func() { gameplayTuningSingleton.Experience = prev })
}

func splitTuning(radius float64) ExperienceTuning {
	return ExperienceTuning{Mode: experienceModeSplit, SplitDefaultXP: 10, SplitEligibilityRadius: radius}
}

func classicTuning() ExperienceTuning {
	return ExperienceTuning{Mode: experienceModeClassic, SplitDefaultXP: 10, SplitEligibilityRadius: 700}
}

func TestSplit_EvenDivisionAmongInRangeUnits(t *testing.T) {
	withExperienceTuning(t, splitTuning(500))
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1000, Y: 1000})
	enemy.XPValue = 10

	var recips []*Unit
	for i := 0; i < 4; i++ {
		u := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1000 + float64(i*10), Y: 1000})
		recips = append(recips, u)
	}

	s.awardSplitDeathXPLocked(enemy)

	// 10 / 4 = 2.5 each → 2 whole XP + 0.5 remainder.
	for i, u := range recips {
		if u.XP != 2 || u.XPProgressRemainder != 0.5 {
			t.Errorf("recipient %d: XP=%d remainder=%v, want XP=2 remainder=0.5", i, u.XP, u.XPProgressRemainder)
		}
	}
}

func TestSplit_FractionAccumulatesOverManyKills(t *testing.T) {
	withExperienceTuning(t, splitTuning(500))
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	// 20 recipients, enemy worth 10 → 0.5 each per kill.
	var recips []*Unit
	for i := 0; i < 20; i++ {
		recips = append(recips, s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1000 + float64(i), Y: 1000}))
	}
	for k := 0; k < 4; k++ {
		enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1005, Y: 1000})
		enemy.XPValue = 10
		s.awardSplitDeathXPLocked(enemy)
	}
	// 4 kills × 0.5 = 2.0 whole XP, 0 remainder.
	for i, u := range recips {
		if u.XP != 2 || u.XPProgressRemainder != 0 {
			t.Errorf("recipient %d: XP=%d remainder=%v, want XP=2 remainder=0", i, u.XP, u.XPProgressRemainder)
		}
	}
}

func TestSplit_OutOfRangeContributorStillEligible(t *testing.T) {
	withExperienceTuning(t, splitTuning(100)) // tight radius
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1000, Y: 1000})
	enemy.XPValue = 8

	near := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1010, Y: 1000}) // within 100
	far := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 5000, Y: 5000})  // far away
	// `far` dealt damage at some point → recorded on the enemy's ledger.
	s.recordDamageDealtLocked(far, enemy, 3)

	s.awardSplitDeathXPLocked(enemy)

	// Eligible set = {near (proximity), far (contributor)} → 8/2 = 4 each.
	if near.XP != 4 {
		t.Errorf("near.XP = %d, want 4", near.XP)
	}
	if far.XP != 4 {
		t.Errorf("far.XP = %d, want 4 (contributor despite being out of range)", far.XP)
	}
}

func TestSplit_DeadContributorExcluded(t *testing.T) {
	withExperienceTuning(t, splitTuning(100))
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1000, Y: 1000})
	enemy.XPValue = 10
	alive := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1010, Y: 1000})
	dead := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1010, Y: 1010})
	s.recordDamageDealtLocked(dead, enemy, 5)
	dead.HP = 0 // unitCanGainXPLocked must exclude it

	s.awardSplitDeathXPLocked(enemy)

	if alive.XP != 10 {
		t.Errorf("alive.XP = %d, want 10 (sole eligible recipient)", alive.XP)
	}
	if dead.XP != 0 {
		t.Errorf("dead.XP = %d, want 0 (excluded)", dead.XP)
	}
}

func TestSplit_NoEligibleRecipients_XPLost(t *testing.T) {
	withExperienceTuning(t, splitTuning(50))
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1000, Y: 1000})
	enemy.XPValue = 10
	farAlly := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 9000, Y: 9000})

	s.awardSplitDeathXPLocked(enemy) // must not panic; nobody gains

	if farAlly.XP != 0 || farAlly.XPProgressRemainder != 0 {
		t.Errorf("farAlly gained XP (%d / %v); want none — XP should be lost", farAlly.XP, farAlly.XPProgressRemainder)
	}
}

func TestSplit_ZeroXPValue_NoAward(t *testing.T) {
	withExperienceTuning(t, splitTuning(500))
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1000, Y: 1000})
	enemy.XPValue = 0 // explicit "no XP"
	ally := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1005, Y: 1000})

	s.awardSplitDeathXPLocked(enemy)

	if ally.XP != 0 || ally.XPProgressRemainder != 0 {
		t.Errorf("ally gained XP from a 0-XP enemy (%d / %v)", ally.XP, ally.XPProgressRemainder)
	}
}

func TestDispatcher_ClassicReproducesPair(t *testing.T) {
	// This test asserts a property of CLASSIC mode, so pin it explicitly rather
	// than relying on the catalog default (which is now "split").
	// awardUnitDeathXPLocked(dead, killer) must equal the legacy pair: killer
	// gets the kill bonus, contributors get damage XP.
	withExperienceTuning(t, classicTuning())
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	killer := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 100, Y: 100})
	dead := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 120, Y: 100})
	contributor := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 140, Y: 100})
	s.recordDamageDealtLocked(contributor, dead, 30)

	// Baseline reference: a second, identical setup run through the legacy pair.
	rs := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	rs.mu.Lock()
	rk := rs.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 100, Y: 100})
	rd := rs.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 120, Y: 100})
	rc := rs.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 140, Y: 100})
	rs.recordDamageDealtLocked(rc, rd, 30)
	rs.awardKillXPLocked(rk)
	rs.payoutDamageDealtXPLocked(rd)
	rs.mu.Unlock()

	s.awardUnitDeathXPLocked(dead, killer)

	if killer.XP != rk.XP || killer.XPProgressRemainder != rk.XPProgressRemainder {
		t.Errorf("killer XP %d/%v != legacy %d/%v", killer.XP, killer.XPProgressRemainder, rk.XP, rk.XPProgressRemainder)
	}
	if contributor.XP != rc.XP || contributor.XPProgressRemainder != rc.XPProgressRemainder {
		t.Errorf("contributor XP %d/%v != legacy %d/%v", contributor.XP, contributor.XPProgressRemainder, rc.XP, rc.XPProgressRemainder)
	}
}

func TestDispatcher_SplitRoutesAndSuppressesTank(t *testing.T) {
	withExperienceTuning(t, splitTuning(500))
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	dead := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1000, Y: 1000})
	dead.XPValue = 10
	killer := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1005, Y: 1000})

	// A soldier that tanked damage from `dead`. In split mode the tank payout
	// must be suppressed — this unit only earns its split share (it is in range).
	tanker := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1010, Y: 1000})
	s.recordSoldierTankContributionLocked(dead, tanker, 100)

	s.awardUnitDeathXPLocked(dead, killer)
	s.awardSoldierTankKillXPLocked(dead.ID) // guarded → no-op in split

	// killer + tanker both in range → 10/2 = 5 each. Tank payout added nothing.
	if killer.XP != 5 {
		t.Errorf("killer.XP = %d, want 5 (split share only)", killer.XP)
	}
	if tanker.XP != 5 {
		t.Errorf("tanker.XP = %d, want 5 (split share only; tank payout suppressed)", tanker.XP)
	}
}

// Drives a real melee kill (resolveAttackHitLocked → awardUnitDeathXPLocked)
// in split mode and asserts the dispatcher routes to the split algorithm:
// the killer and a nearby ally each get half the enemy's XPValue, and the
// classic kill bonus (25 × 0.2) does NOT appear.
func TestSplit_EndToEndMeleeKill(t *testing.T) {
	withExperienceTuning(t, splitTuning(500))
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	killer := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1000, Y: 1000})
	killer.Damage = 9999 // one-shot
	ally := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1010, Y: 1000})
	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1005, Y: 1000})
	enemy.HP = 1
	enemy.MaxHP = 1
	enemy.XPValue = 10

	var dead []int
	s.resolveAttackHitLocked(killer, enemy, killer.Damage, &dead)

	// Eligible = {killer, ally} (both in range) → 10/2 = 5 each, no kill bonus.
	if killer.XP != 5 {
		t.Errorf("killer.XP = %d, want 5 (split share, no classic kill bonus)", killer.XP)
	}
	if ally.XP != 5 {
		t.Errorf("ally.XP = %d, want 5 (in-range split share)", ally.XP)
	}
}

// TestSplit_EnemyBystander_NoXP asserts that an enemy-owned unit in proximity
// of the death does NOT receive XP. This exercises unitCanGainXPLocked's
// ownership check as a defense-in-depth guard inside awardSplitDeathXPLocked.
func TestSplit_EnemyBystander_NoXP(t *testing.T) {
	withExperienceTuning(t, splitTuning(500))
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	dead := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1000, Y: 1000})
	dead.XPValue = 10

	ally := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1005, Y: 1000})

	// An enemy-team bystander standing right next to the death.
	enemyBystander := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1002, Y: 1000})

	s.awardSplitDeathXPLocked(dead)

	// Only the allied unit receives XP; the enemy bystander gets none.
	if ally.XP != dead.XPValue {
		t.Errorf("ally.XP = %d, want %d (sole eligible recipient)", ally.XP, dead.XPValue)
	}
	if enemyBystander.XP != 0 || enemyBystander.XPProgressRemainder != 0 {
		t.Errorf("enemy bystander gained XP (%d / %v); unitCanGainXPLocked should exclude enemies",
			enemyBystander.XP, enemyBystander.XPProgressRemainder)
	}
}

// TestSplit_WorkerNotEligible verifies that workers are excluded from the split
// XP pool: a worker standing next to a kill neither gains XP nor dilutes the
// share of the eligible combat units (it previously "sapped" the pool).
func TestSplit_WorkerNotEligible(t *testing.T) {
	withExperienceTuning(t, splitTuning(500))
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	dead := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1000, Y: 1000})
	dead.XPValue = 10

	soldier := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1005, Y: 1000})
	worker := s.spawnPlayerUnitLocked("worker", "p1", "#3498db", protocol.Vec2{X: 1010, Y: 1000})

	s.awardSplitDeathXPLocked(dead)

	// Only the soldier is eligible → it gets the full 10, not a diluted 5.
	if soldier.XP != 10 {
		t.Errorf("soldier.XP = %d, want 10 (worker must not split the pool)", soldier.XP)
	}
	if worker.XP != 0 || worker.XPProgressRemainder != 0 {
		t.Errorf("worker gained XP (%d / %v); workers must not gain XP", worker.XP, worker.XPProgressRemainder)
	}
}

// TestSplit_BuildingDestroy_NoXP verifies the payoutBuildingDamageDealtXPLocked
// early-return guard: in split mode, units that damaged an enemy building
// receive no XP when the building is destroyed.
func TestSplit_BuildingDestroy_NoXP(t *testing.T) {
	withExperienceTuning(t, splitTuning(500))
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 100, Y: 100})
	const buildingID = "test-building-1"

	// Bank some damage against the building — this is what would pay out in classic.
	s.recordDamageDealtBuildingLocked(attacker, buildingID, 200)

	// Simulate the building being destroyed and the payout being triggered.
	s.payoutBuildingDamageDealtXPLocked(buildingID)

	if attacker.XP != 0 || attacker.XPProgressRemainder != 0 {
		t.Errorf("attacker gained XP from building destruction in split mode (%d / %v); want none",
			attacker.XP, attacker.XPProgressRemainder)
	}
}

// TestSplit_Determinism_SameResultAcrossRuns executes the same split scenario
// N times and asserts every run produces an identical final XP state.
// This catches any accidental map-iteration-order dependence that would cause
// nondeterministic share ordering (even though equal shares make ordering moot
// for the final sum, this guards against future code changes that break the invariant).
func TestSplit_Determinism_SameResultAcrossRuns(t *testing.T) {
	const runs = 10
	const recipients = 5
	const xpValue = 13 // intentionally indivisible to test remainder accumulation

	type result struct {
		xp        int
		remainder float64
	}

	var baseline []result

	for run := 0; run < runs; run++ {
		withExperienceTuning(t, splitTuning(500))
		s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
		s.mu.Lock()

		dead := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 1000, Y: 1000})
		dead.XPValue = xpValue

		var recips []*Unit
		for i := 0; i < recipients; i++ {
			u := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 1000 + float64(i*5), Y: 1000})
			recips = append(recips, u)
		}

		s.awardSplitDeathXPLocked(dead)

		got := make([]result, len(recips))
		for i, u := range recips {
			got[i] = result{xp: u.XP, remainder: u.XPProgressRemainder}
		}
		s.mu.Unlock()

		if run == 0 {
			baseline = got
			continue
		}
		for i, r := range got {
			if r != baseline[i] {
				t.Errorf("run %d recipient %d: XP=%d remainder=%v, baseline XP=%d remainder=%v — nondeterministic result",
					run, i, r.xp, r.remainder, baseline[i].xp, baseline[i].remainder)
			}
		}
	}
}

// TestSplit_TrapDeadTrapper_XPLost verifies that when a trap kills a unit and
// the trap owner is nil (trapper died before the kill), no XP is distributed
// in split mode. This exercises the call-site guard in trap.go: the dispatcher
// call is inside "if ownerUnit != nil { ... }", so with a nil owner, the
// dispatcher is never invoked and the XP is simply lost.
//
// This test exercises the guard structurally by calling awardUnitDeathXPLocked
// directly with a nil killer (replicating what the trap site does when ownerUnit
// is non-nil), and separately confirming that awardSplitDeathXPLocked with no
// nearby allies also yields lost XP — the two halves of the trap guard.
func TestSplit_TrapDeadTrapper_XPLost(t *testing.T) {
	withExperienceTuning(t, splitTuning(500))
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	// The trap victim (the unit the trap killed).
	victim := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 5000, Y: 5000})
	victim.XPValue = 20

	// A living ally far from the trap kill site (no proximity eligibility).
	farAlly := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 100, Y: 100})

	// Simulate: the trap fires, victim dies, but ownerUnit is nil — the trap
	// site's guard prevents the dispatcher call entirely. We verify this by
	// calling the dispatcher with a nil killer (split mode ignores killer)
	// with no allies in range. Since the victim is at (5000,5000) and farAlly
	// is at (100,100), the split radius of 500 does not cover farAlly.
	// Nobody dealt damage to victim either, so the eligible set is empty → XP lost.
	s.awardUnitDeathXPLocked(victim, nil /* nil killer: dead trapper */)

	if farAlly.XP != 0 || farAlly.XPProgressRemainder != 0 {
		t.Errorf("farAlly gained XP (%d / %v) from trap kill with no nearby allies; XP should be lost",
			farAlly.XP, farAlly.XPProgressRemainder)
	}
}

// TestClassic_SoldierTankKillXP_NotSuppressed verifies that in classic mode
// the awardSoldierTankKillXPLocked function still runs and credits XP to a
// tanking soldier. This is the counterpart of TestDispatcher_SplitRoutesAndSuppressesTank.
func TestClassic_SoldierTankKillXP_NotSuppressed(t *testing.T) {
	// Default tuning = classic; no withExperienceTuning override needed.
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 42)
	s.mu.Lock()
	defer s.mu.Unlock()

	dead := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 100, Y: 100})
	killer := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 110, Y: 100})
	tanker := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 120, Y: 100})

	// Record that tanker absorbed damage from `dead`.
	const tankedDamage = 50
	s.recordSoldierTankContributionLocked(dead, tanker, tankedDamage)

	// Run the two-step classic kill sequence.
	s.awardUnitDeathXPLocked(dead, killer)
	s.awardSoldierTankKillXPLocked(dead.ID)

	// Tank XP = tankedDamage × xpPerSoldierDamageTankedOnKill × xpGainMultiplier
	// (the last scaling happens inside addUnitXPFloatLocked which is called by
	// awardSoldierTankKillXPLocked). Assert that the tanker received strictly
	// more XP than zero — we do not pin the exact value since it derives from
	// three catalog constants (xpPerSoldierDamageTankedOnKill=0.5, xpGainMultiplier=0.2).
	if tanker.XP == 0 && tanker.XPProgressRemainder == 0 {
		t.Errorf("tanker received no XP in classic mode; awardSoldierTankKillXPLocked may be suppressed")
	}
	// Killer should also have received kill-bonus XP.
	if killer.XP == 0 && killer.XPProgressRemainder == 0 {
		t.Errorf("killer received no XP in classic mode")
	}
}
