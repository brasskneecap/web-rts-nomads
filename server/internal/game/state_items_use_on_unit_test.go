package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// ─── Consumable use (drag onto a single unit card) ────────────────────────────

// TestUseItemOnUnit_HealFullAmountConsumesOneStack: dragging a heal potion onto
// one unit heals it by the FULL (unsplit) amount and consumes exactly one stack
// of a multi-stack entry.
func TestUseItemOnUnit_HealFullAmountConsumesOneStack(t *testing.T) {
	s, playerID := newItemTestState(t)
	def := itemDef(t, "potion_common_heal")
	amount := def.Consumable.Amount

	unit := spawnCombatUnitAt(t, s, playerID, 800, 800)
	s.mu.Lock()
	unit.HP = 1
	s.mu.Unlock()

	// Two copies stack onto one entry; using one must leave one behind.
	iid := addVaultConsumable(t, s, playerID, "potion_common_heal")
	_ = addVaultConsumable(t, s, playerID, "potion_common_heal")

	s.UseItemOnUnit(playerID, iid, unit.ID)

	s.mu.RLock()
	defer s.mu.RUnlock()
	want := 1 + amount
	if want > unit.MaxHP {
		want = unit.MaxHP
	}
	if unit.HP != want {
		t.Errorf("want HP %d (full unsplit heal), got %d", want, unit.HP)
	}
	if got := vaultItemCountLocked(s.Players[playerID], "potion_common_heal"); got != 1 {
		t.Errorf("exactly one stack should be consumed; remaining count=%d", got)
	}
}

// TestUseItemOnUnit_GrantXPConsumesOnEligibleUnit: an XP potion dropped on a
// combat unit (XP-eligible) applies and is consumed. Consumption is the proof
// the effect ran — the handler only spends the item after a successful apply.
func TestUseItemOnUnit_GrantXPConsumesOnEligibleUnit(t *testing.T) {
	s, playerID := newItemTestState(t)
	unit := spawnCombatUnitAt(t, s, playerID, 800, 800)

	iid := addVaultConsumable(t, s, playerID, "experience_potion")
	s.UseItemOnUnit(playerID, iid, unit.ID)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if got := vaultItemCountLocked(s.Players[playerID], "experience_potion"); got != 0 {
		t.Errorf("XP potion should be consumed on an eligible unit; count=%d", got)
	}
}

// TestUseItemOnUnit_IneligibleUnitIsNoop: an XP potion dropped on a worker
// (XP-ineligible, same rule as the AoE path) neither applies nor is consumed.
func TestUseItemOnUnit_IneligibleUnitIsNoop(t *testing.T) {
	s, playerID := newItemTestState(t)

	s.mu.Lock()
	worker := s.spawnPlayerUnitLocked("worker", playerID, s.Players[playerID].Color, protocol.Vec2{X: 800, Y: 800})
	s.mu.Unlock()
	if worker == nil {
		t.Fatal("failed to spawn worker")
	}

	iid := addVaultConsumable(t, s, playerID, "experience_potion")
	s.UseItemOnUnit(playerID, iid, worker.ID)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if worker.XP != 0 {
		t.Errorf("worker cannot gain XP; got XP=%d", worker.XP)
	}
	if got := vaultItemCountLocked(s.Players[playerID], "experience_potion"); got != 1 {
		t.Errorf("ineligible target must NOT consume the potion; count=%d", got)
	}
}

// TestUseItemOnUnit_ForeignUnitIsNoop: a consumable can only be applied to the
// player's own units — targeting another player's unit is a silent no-op that
// does not consume the item.
func TestUseItemOnUnit_ForeignUnitIsNoop(t *testing.T) {
	s, playerID := newItemTestState(t)

	s.mu.Lock()
	s.ensureEnemyPlayerLocked()
	foreign := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, enemyPlayerColor, protocol.Vec2{X: 800, Y: 800})
	s.mu.Unlock()
	if foreign == nil {
		t.Fatal("failed to spawn foreign unit")
	}
	s.mu.Lock()
	foreign.HP = 1
	s.mu.Unlock()

	iid := addVaultConsumable(t, s, playerID, "potion_common_heal")
	s.UseItemOnUnit(playerID, iid, foreign.ID)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if foreign.HP != 1 {
		t.Errorf("foreign unit must not be healed; HP=%d", foreign.HP)
	}
	if got := vaultItemCountLocked(s.Players[playerID], "potion_common_heal"); got != 1 {
		t.Errorf("targeting a foreign unit must NOT consume the potion; count=%d", got)
	}
}
