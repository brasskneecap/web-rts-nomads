package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// AI-acquired unit target that becomes unreachable: the enemy must drop it,
// memo it on the unreachable cooldown, and NOT enter drift mode. (drift =
// AttackDrifting true with a straight-line TargetX/Y and no path.)
func TestUnreachableUnit_AIAcquired_DropsNotDrift(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()
	ownerID := "p1"

	// Full unit-wall partition; a lone player unit sits behind it (unreachable).
	for y := 10.0; y <= s.MapHeight-10.0; y += 20.0 {
		w := s.spawnPlayerUnitLocked("soldier", ownerID, "#3498db", protocol.Vec2{X: 1200, Y: y})
		w.Visible = true
		w.MaxHP, w.HP = 1000, 1000
		w.MoveSpeed = 0
		w.Damage = 0
		w.Capabilities = nil
		s.initializeCombatUnitLocked(w)
	}
	behind := s.spawnPlayerUnitLocked("soldier", ownerID, "#3498db", protocol.Vec2{X: 400, Y: 768})
	behind.Visible = true
	behind.MaxHP, behind.HP = 50, 50
	s.initializeCombatUnitLocked(behind)
	behindID := behind.ID

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c",
		protocol.Vec2{X: 1300, Y: 768})
	enemy.Visible = true
	enemy.MaxHP, enemy.HP = 800, 800
	enemy.MoveSpeed = 150
	s.initializeCombatUnitLocked(enemy)
	blocked := s.getBlockedCellsLocked()

	// Force the unreachable unit as the AI-acquired target.
	enemy.AttackTargetID = behindID
	target := s.unitsByID[behindID]
	s.assignAttackApproachPathLocked(enemy, target, blocked)

	if enemy.AttackDrifting {
		t.Fatal("AI-acquired unreachable unit must NOT enter drift mode")
	}
	if enemy.UnreachableUnitTargetID != behindID {
		t.Fatalf("unreachable unit must be memoed; got %d want %d",
			enemy.UnreachableUnitTargetID, behindID)
	}
	if enemy.AttackTargetID != 0 {
		t.Fatalf("unreachable target must be dropped; AttackTargetID=%d", enemy.AttackTargetID)
	}
}

// selectBestTargetLocked must skip a unit while it is on the unreachable
// cooldown, so the enemy picks a reachable alternative instead.
func TestUnreachableUnit_SkippedBySelection(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()
	ownerID := "p1"

	// NOTE: `far` is deliberately the unit PHYSICALLY CLOSEST to the enemy
	// (5px) and `near` the farther one (90px). The memoed-unreachable unit
	// must be the strictly-highest-scored pre-skip pick, so this test fails
	// iff the selection-skip is absent (true red->green guard).
	near := s.spawnPlayerUnitLocked("soldier", ownerID, "#3498db", protocol.Vec2{X: 1340, Y: 768})
	near.Visible = true
	near.MaxHP, near.HP = 50, 50
	s.initializeCombatUnitLocked(near)
	nearID := near.ID

	far := s.spawnPlayerUnitLocked("soldier", ownerID, "#3498db", protocol.Vec2{X: 1255, Y: 768})
	far.Visible = true
	far.MaxHP, far.HP = 50, 50
	s.initializeCombatUnitLocked(far)
	farID := far.ID

	enemy := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c",
		protocol.Vec2{X: 1250, Y: 768})
	enemy.Visible = true
	enemy.MaxHP, enemy.HP = 800, 800
	s.initializeCombatUnitLocked(enemy)

	// Mark `far` unreachable for a window that covers this tick.
	enemy.UnreachableUnitTargetID = farID
	enemy.UnreachableUnitUntilTick = s.Tick + 100

	profile := resolveCombatProfile(enemy)
	idx := newCombatSpatialIndex(combatSpatialBucketSize)
	for _, u := range s.Units {
		if u != nil && u.Visible && u.HP > 0 {
			idx.add(u)
		}
	}
	best := s.selectBestTargetLocked(enemy, profile, combatEvalContext{index: idx, blocked: s.getBlockedCellsLocked()})
	if best.Kind != combatTargetUnit || best.Unit == nil || best.Unit.ID != nearID {
		t.Fatalf("must skip unreachable %d and pick reachable %d; got kind=%d unit=%v",
			farID, nearID, best.Kind, best.Unit)
	}
}

// Player-issued (OrderAttackTarget) unreachable unit still uses drift mode —
// the deliberate "the player explicitly chose this fight" invariant.
func TestUnreachableUnit_PlayerIssued_StillDrifts(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()
	ownerID := "p1"

	for y := 10.0; y <= s.MapHeight-10.0; y += 20.0 {
		w := s.spawnPlayerUnitLocked("soldier", ownerID, "#3498db", protocol.Vec2{X: 1200, Y: y})
		w.Visible = true
		w.MaxHP, w.HP = 1000, 1000
		w.MoveSpeed = 0
		w.Damage = 0
		w.Capabilities = nil
		s.initializeCombatUnitLocked(w)
	}
	enemyVictim := s.spawnPlayerUnitLocked("soldier", "p2", "#9b59b6", protocol.Vec2{X: 400, Y: 768})
	enemyVictim.Visible = true
	enemyVictim.MaxHP, enemyVictim.HP = 50, 50
	s.initializeCombatUnitLocked(enemyVictim)
	victimID := enemyVictim.ID

	player := s.spawnPlayerUnitLocked("soldier", ownerID, "#3498db", protocol.Vec2{X: 1300, Y: 768})
	player.Visible = true
	player.MaxHP, player.HP = 800, 800
	player.MoveSpeed = 150
	s.initializeCombatUnitLocked(player)
	player.Order = OrderState{Type: OrderAttackTarget}
	player.AttackTargetID = victimID
	blocked := s.getBlockedCellsLocked()

	s.assignAttackApproachPathLocked(player, s.unitsByID[victimID], blocked)

	if !player.AttackDrifting {
		t.Fatal("player-issued unreachable target must still drift")
	}
	if player.UnreachableUnitTargetID != 0 {
		t.Fatalf("player-issued target must NOT be memoed/dropped; memo=%d", player.UnreachableUnitTargetID)
	}
}
