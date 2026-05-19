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
	if et.Mode != experienceModeClassic {
		t.Errorf("default experience.mode = %q, want %q", et.Mode, experienceModeClassic)
	}
	if et.SplitDefaultXP != 10 {
		t.Errorf("default experience.splitDefaultXP = %d, want 10", et.SplitDefaultXP)
	}
	if et.SplitEligibilityRadius != 500 {
		t.Errorf("default experience.splitEligibilityRadius = %v, want 500", et.SplitEligibilityRadius)
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
	// Default tuning = classic. awardUnitDeathXPLocked(dead, killer) must equal
	// the legacy pair: killer gets the kill bonus, contributors get damage XP.
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
