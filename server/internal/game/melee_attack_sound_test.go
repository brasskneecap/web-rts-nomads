package game

import (
	"webrts/server/pkg/protocol"
	"testing"
)

// TestMeleeAttackType_CatalogAndPathResolution asserts the attack-sound key
// authored on the melee unit defs and their promotion paths, and that spawn +
// promotion resolve unit.AttackType correctly. These strings are the feature's
// contract (which sound plays), not balance tunables, so pinning them is the
// point of the test.
func TestMeleeAttackType_CatalogAndPathResolution(t *testing.T) {
	cases := []struct {
		unitType string
		want     string
	}{
		{"soldier", "swing"},
		{"raider", "stab"},
		{"raider_brute", "swing"},
	}
	for _, c := range cases {
		def, ok := getUnitDef(c.unitType)
		if !ok {
			t.Fatalf("getUnitDef(%q) not found", c.unitType)
		}
		if def.AttackType != c.want {
			t.Errorf("%s def.AttackType = %q, want %q", c.unitType, def.AttackType, c.want)
		}
	}

	// Soldier promotion paths: vanguard overrides to "stab", berserker inherits
	// the soldier's "swing" (no override authored).
	if got := pathAttackTypeByPath[unitPathVanguard]; got != "stab" {
		t.Errorf("pathAttackTypeByPath[vanguard] = %q, want \"stab\"", got)
	}
	if _, has := pathAttackTypeByPath[unitPathBerserker]; has {
		t.Errorf("berserker should have no attackType override (inherits soldier's swing); got one")
	}

	s := NewGameState(protocol.MapConfig{ID: "test", Width: 100, Height: 100})
	s.mu.Lock()
	defer s.mu.Unlock()

	sol := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 100, Y: 100})
	if sol.AttackType != "swing" {
		t.Fatalf("spawned soldier AttackType = %q, want \"swing\"", sol.AttackType)
	}

	// Promote to vanguard → stab.
	sol.ProgressionPath = unitPathVanguard
	sol.Rank = unitRankBronze
	s.applyRankModifiersLocked(sol, true)
	if sol.AttackType != "stab" {
		t.Errorf("vanguard soldier AttackType = %q, want \"stab\"", sol.AttackType)
	}

	// A separate soldier promoted to berserker keeps "swing".
	ber := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 120, Y: 100})
	ber.ProgressionPath = unitPathBerserker
	ber.Rank = unitRankBronze
	s.applyRankModifiersLocked(ber, true)
	if ber.AttackType != "swing" {
		t.Errorf("berserker soldier AttackType = %q, want \"swing\" (inherited)", ber.AttackType)
	}
}

// TestMeleeAttackType_SwingEmitsEvent confirms a melee swing pushes a
// meleeAttackEvent carrying the unit's AttackType at the START of the swing
// (windup begin, driven through tickUnitCombatLocked), and that the event
// drains into the snapshot wire type.
func TestMeleeAttackType_SwingEmitsEvent(t *testing.T) {
	s := NewGameState(protocol.MapConfig{ID: "test", Width: 100, Height: 100})
	s.mu.Lock()
	defer s.mu.Unlock()

	attacker := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: 100, Y: 100})
	target := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c", protocol.Vec2{X: 110, Y: 100})

	// In-range, cooldown ready → the combat tick begins a swing (windup) and
	// emits the swing sound at that moment, before any damage lands.
	attacker.AttackTargetID = target.ID
	attacker.AttackCooldown = 0
	blocked := s.getBlockedCellsLocked()
	s.tickUnitCombatLocked(0.1, blocked)

	if attacker.AttackWindupRemaining <= 0 {
		t.Fatalf("expected the swing to have begun (AttackWindupRemaining > 0); got %.3f — no windup started", attacker.AttackWindupRemaining)
	}
	if len(s.meleeAttackEventsThisTick) != 1 {
		t.Fatalf("meleeAttackEventsThisTick len = %d, want 1 (emitted at swing start)", len(s.meleeAttackEventsThisTick))
	}
	if got := s.meleeAttackEventsThisTick[0].AttackType; got != "swing" {
		t.Errorf("emitted AttackType = %q, want \"swing\"", got)
	}
	// The event is tagged with the attacker's position so the client can
	// viewport-gate the sound.
	if ev := s.meleeAttackEventsThisTick[0]; ev.X != attacker.X || ev.Y != attacker.Y {
		t.Errorf("emitted position = (%.1f, %.1f), want attacker (%.1f, %.1f)", ev.X, ev.Y, attacker.X, attacker.Y)
	}

	wire := s.snapshotMeleeAttackEventsLocked()
	if len(wire) != 1 || wire[0].AttackType != "swing" {
		t.Errorf("snapshotMeleeAttackEventsLocked() = %+v, want one {swing}", wire)
	}
	if wire[0].X != attacker.X || wire[0].Y != attacker.Y {
		t.Errorf("wire position = (%.1f, %.1f), want attacker (%.1f, %.1f)", wire[0].X, wire[0].Y, attacker.X, attacker.Y)
	}

	s.resetMeleeAttackEventsThisTickLocked()
	if len(s.meleeAttackEventsThisTick) != 0 {
		t.Errorf("reset did not clear the queue: len = %d", len(s.meleeAttackEventsThisTick))
	}
}
