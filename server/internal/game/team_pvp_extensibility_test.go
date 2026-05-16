package game

import "testing"

// The bake-in guarantee: making two players hostile is PURELY a data change
// (different Player.TeamID) — nothing else. This single test runs each of the
// five team-scoped subsystems twice, identical except for p2's team, and
// asserts every one behaves *allied* when same-team and *hostile* when
// cross-team. If any subsystem ignored TeamID, exactly one of these flips
// would fail.
//
// Each check returns true when the observed behavior is the ALLIED outcome.
func TestTeam_PvP_AllSubsystemsFollowTeamData(t *testing.T) {
	// 1. Combat auto-targeting: allied ⇒ they never fight.
	combatAllied := func(t *testing.T, p2Team int) bool {
		s := newProjectileTestState(t)
		s.mu.Lock()
		a := teamCombatUnit(t, s, "p1", 400, 400)
		b := teamCombatUnit(t, s, "p2", 460, 400)
		setTeam(s, "p1", 0)
		setTeam(s, "p2", p2Team)
		aID, bID := a.ID, b.ID
		s.mu.Unlock()
		tickN(s, 60)
		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.unitsByID[aID].HP == 500 && s.unitsByID[bID].HP == 500
	}

	// 2. Ability/heal targeting: allied ⇒ heal can target the other.
	abilityAllied := func(t *testing.T, p2Team int) bool {
		s := newProjectileTestState(t)
		s.mu.Lock()
		defer s.mu.Unlock()
		caster := spawnProjTestUnit(t, s, "p1", 100, 100)
		caster.AttackRange = 300
		mate := spawnProjTestUnit(t, s, "p2", 140, 100)
		mate.HP = mate.MaxHP - 20
		setTeam(s, "p1", 0)
		setTeam(s, "p2", p2Team)
		heal, _ := getAbilityDef("heal")
		return s.canAbilityTargetUnitLocked(heal, caster, mate)
	}

	// 3. Friendly-fire: allied ⇒ p2 is spared by splash.
	friendlyFireAllied := func(t *testing.T, p2Team int) bool {
		s := newProjectileTestState(t)
		s.mu.Lock()
		defer s.mu.Unlock()
		attacker := teamCombatUnit(t, s, "p1", 100, 100)
		attacker.SplashRadius = 140
		primary := teamCombatUnit(t, s, enemyPlayerID, 400, 400) // always hostile
		bystander := teamCombatUnit(t, s, "p2", 420, 400)
		setTeam(s, "p1", 0)
		setTeam(s, "p2", p2Team)
		var dead []int
		s.resolveAttackHitLocked(attacker, primary, 50, &dead)
		return bystander.HP == 500 // unhurt ⇒ treated as ally (no friendly fire)
	}

	// 4. FOW: allied ⇒ p1 sees through p2's vision.
	fowAllied := func(t *testing.T, p2Team int) bool {
		s := newProjectileTestState(t)
		s.mu.Lock()
		defer s.mu.Unlock()
		cell := s.MapConfig.CellSize
		cols, rows := s.MapConfig.GridCols, s.MapConfig.GridRows
		y := cell * float64(rows/2)
		p1x := cell * float64(cols/4)
		p2x := cell * float64(cols*3/4)
		vision := cell * 1.5
		mk := func(id int, owner string, x float64) *Unit {
			return &Unit{ID: id, OwnerID: owner, X: x, Y: y, HP: 100, Visible: true, VisionRange: vision, Flyer: true}
		}
		s.Units = append(s.Units, mk(1, "p1", p1x), mk(2, "p2", p2x))
		s.Players["p1"] = &Player{ID: "p1", TeamID: 0}
		s.Players["p2"] = &Player{ID: "p2", TeamID: p2Team}
		s.FOW = map[string]*PlayerFOW{
			"p1": newPlayerFOW(cols, rows),
			"p2": newPlayerFOW(cols, rows),
		}
		s.recomputeFOWLocked()
		return fowCellClear(s.FOW["p1"], int(p2x/cell), int(y/cell))
	}

	// 5. Team defeat: allied ⇒ losing p1's only townhall does NOT defeat p1
	// (p2's townhall carries the shared team).
	defeatAllied := func(t *testing.T, p2Team int) bool {
		s := newProjectileTestState(t)
		s.mu.Lock()
		defer s.mu.Unlock()
		addTownhall(s, "th1", "p1")
		addTownhall(s, "th2", "p2")
		setTeam(s, "p1", 0)
		setTeam(s, "p2", p2Team)
		s.checkPlayerLossLocked() // register both
		destroyTownhall(s, "th1")
		s.checkPlayerLossLocked()
		return !s.lostPlayerIDs["p1"] // p1 survives ⇒ carried by allied p2
	}

	checks := []struct {
		name string
		fn   func(*testing.T, int) bool
	}{
		{"combat", combatAllied},
		{"ability", abilityAllied},
		{"friendly-fire", friendlyFireAllied},
		{"fow", fowAllied},
		{"defeat", defeatAllied},
	}

	// Same team (0): every subsystem must show the ALLIED outcome.
	for _, c := range checks {
		if !c.fn(t, 0) {
			t.Errorf("[%s] same team: expected ALLIED behavior, got hostile", c.name)
		}
	}
	// Flip ONLY p2's TeamID → every subsystem must show the HOSTILE outcome.
	// (Identical setup; the team int is the sole difference — proves the
	// whole feature is data-driven with no per-subsystem wiring.)
	for _, c := range checks {
		if c.fn(t, 1) {
			t.Errorf("[%s] different team: expected HOSTILE behavior, got allied", c.name)
		}
	}
}
