package game

import "testing"

// Splash damage (resolveAttackHitLocked → applySplashDamageLocked) skips
// same-team units (no friendly fire on allies) and still hits cross-team
// units. This exercises the friendly-fire contract through the real combat
// pipeline; the chokepoint (playersAreHostileLocked) is what every AoE/
// splash/whirlwind/cleave/pierce loop already routes through (P0), so this
// representative case proves the whole class.
//
// The projectile attacker-dead pierce branch (projectile.go) uses
// playersAreFriendlyLocked for the same purpose; that predicate's full truth
// table (incl. __enemy__) is covered by TestTeamPredicates_*.
func TestTeam_NoFriendlyFireOnAllies(t *testing.T) {
	mk := func(t *testing.T, allyTeam int) (allyHP, enemyHP, primaryHP int) {
		s := newProjectileTestState(t)
		s.mu.Lock()
		defer s.mu.Unlock()

		attacker := teamCombatUnit(t, s, "p1", 100, 100)
		attacker.SplashRadius = 140 // AoE on every other hostile near the primary

		// Primary target: a hostile (team 1) unit the attacker strikes.
		primary := teamCombatUnit(t, s, "enemy", 400, 400)
		// A different-owner unit adjacent to the primary, whose team we vary.
		ally := teamCombatUnit(t, s, "p2", 420, 400)
		// A definite cross-team unit adjacent to the primary (control).
		foe := teamCombatUnit(t, s, "enemy2", 380, 400)

		setTeam(s, "p1", 0)
		setTeam(s, "enemy", 1)
		setTeam(s, "enemy2", 1)
		setTeam(s, "p2", allyTeam) // 0 = attacker's team (spared), 1 = hostile (hit)

		var dead []int
		s.resolveAttackHitLocked(attacker, primary, 50, &dead)

		return ally.HP, foe.HP, primary.HP
	}

	t.Run("same team ally is spared by splash", func(t *testing.T) {
		allyHP, foeHP, primaryHP := mk(t, 0)
		if allyHP != 500 {
			t.Errorf("same-team unit took splash damage (HP=%d); friendly fire must be off for allies", allyHP)
		}
		if foeHP == 500 {
			t.Error("cross-team control unit should have taken splash damage")
		}
		if primaryHP == 500 {
			t.Error("primary target should have taken the direct hit")
		}
	})

	t.Run("cross team unit IS hit by splash", func(t *testing.T) {
		allyHP, _, _ := mk(t, 1) // p2 now on the enemy team
		if allyHP == 500 {
			t.Error("a cross-team unit near the primary must take splash damage")
		}
	})
}
