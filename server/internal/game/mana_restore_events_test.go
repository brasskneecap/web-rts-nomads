package game

// Section: mana restore event emission.
//
// Verifies that the per-tick mana restore queue captures every intentional
// mana grant (anything that routes through addUnitManaLocked) and that
// passive regen stays silent. Mirrors the pattern in damage_type_tagging_test.go.

import (
	"testing"

	"webrts/server/pkg/protocol"
)

func newManaTestState(t *testing.T) *GameState {
	t.Helper()
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x4AA1)
	s.mu.Lock()
	t.Cleanup(func() {
		// Best-effort: state.mu is acquired here for the duration of the test.
		// Nothing else holds it because tests are single-goroutine.
		s.mu.Unlock()
	})
	return s
}

func spawnSpellcaster(s *GameState, ownerID string) *Unit {
	u := &Unit{
		ID:       s.nextUnitID,
		OwnerID:  ownerID,
		UnitType: "acolyte",
		Visible:  true,
		HP:       100, MaxHP: 100,
		MaxMana: 100,
	}
	s.nextUnitID++
	s.addUnitLocked(u)
	return u
}

func TestAddUnitManaLocked_EmitsRestoreEvent(t *testing.T) {
	s := newManaTestState(t)
	u := spawnSpellcaster(s, "p1")
	u.CurrentMana = 30

	s.resetManaRestoreEventsThisTickLocked()
	gained := s.addUnitManaLocked(u, 20)
	if gained != 20 {
		t.Fatalf("addUnitManaLocked returned %d, want 20", gained)
	}
	if len(s.manaRestoreEventsThisTick) != 1 {
		t.Fatalf("expected 1 queued event, got %d", len(s.manaRestoreEventsThisTick))
	}
	evt := s.manaRestoreEventsThisTick[0]
	if evt.UnitID != u.ID {
		t.Errorf("event UnitID = %d, want %d", evt.UnitID, u.ID)
	}
	if evt.Amount != 20 {
		t.Errorf("event Amount = %d, want 20", evt.Amount)
	}
}

func TestAddUnitManaLocked_ClampsAmountInEvent(t *testing.T) {
	// When mana grant exceeds room, the event reports only what actually
	// fit — the popup must never show "+50" if only 5 mana was room.
	s := newManaTestState(t)
	u := spawnSpellcaster(s, "p1")
	u.CurrentMana = 95 // 5 room before max

	s.resetManaRestoreEventsThisTickLocked()
	gained := s.addUnitManaLocked(u, 50)
	if gained != 5 {
		t.Fatalf("clamped grant returned %d, want 5 (room=5)", gained)
	}
	if len(s.manaRestoreEventsThisTick) != 1 {
		t.Fatalf("expected 1 queued event, got %d", len(s.manaRestoreEventsThisTick))
	}
	if got := s.manaRestoreEventsThisTick[0].Amount; got != 5 {
		t.Errorf("event Amount = %d, want 5 (popup must show actual gain, not requested)", got)
	}
}

func TestAddUnitManaLocked_NoEventWhenFull(t *testing.T) {
	s := newManaTestState(t)
	u := spawnSpellcaster(s, "p1")
	u.CurrentMana = u.MaxMana

	s.resetManaRestoreEventsThisTickLocked()
	gained := s.addUnitManaLocked(u, 50)
	if gained != 0 {
		t.Errorf("full pool: gained %d, want 0", gained)
	}
	if len(s.manaRestoreEventsThisTick) != 0 {
		t.Errorf("full pool should emit no event, got %d", len(s.manaRestoreEventsThisTick))
	}
}

func TestPassiveRegen_DoesNotEmitRestoreEvent(t *testing.T) {
	// Passive regen mutates Unit.CurrentMana directly in
	// tickUnitManaRegenLocked, NEVER routing through addUnitManaLocked.
	// That's the load-bearing reason regen popups don't spam the screen.
	s := newManaTestState(t)
	u := spawnSpellcaster(s, "p1")
	u.CurrentMana = 0
	u.ManaRegenPerSecond = 10 // high rate so regen lands within one tick

	s.resetManaRestoreEventsThisTickLocked()
	// One full tick (dt=1s) at 10/s regen lands 10 mana — without emitting.
	s.tickUnitManaRegenLocked(u, 1.0)
	if u.CurrentMana != 10 {
		t.Fatalf("regen should add 10 mana in 1s at rate 10/s, got %d", u.CurrentMana)
	}
	if len(s.manaRestoreEventsThisTick) != 0 {
		t.Errorf("passive regen should emit no popup event, got %d entries",
			len(s.manaRestoreEventsThisTick))
	}
}

func TestSnapshotManaRestoreEvents_SerializesQueue(t *testing.T) {
	s := newManaTestState(t)
	u1 := spawnSpellcaster(s, "p1")
	u1.CurrentMana = 0
	u2 := spawnSpellcaster(s, "p1")
	u2.CurrentMana = 0

	s.resetManaRestoreEventsThisTickLocked()
	s.addUnitManaLocked(u1, 5)
	s.addUnitManaLocked(u2, 3)

	snap := s.snapshotManaRestoreEventsLocked()
	if len(snap) != 2 {
		t.Fatalf("snapshot returned %d events, want 2", len(snap))
	}
	// Order matches insertion order.
	want := []protocol.ManaRestoreEventSnapshot{
		{UnitID: u1.ID, Amount: 5},
		{UnitID: u2.ID, Amount: 3},
	}
	for i, w := range want {
		if snap[i] != w {
			t.Errorf("snap[%d] = %+v, want %+v", i, snap[i], w)
		}
	}
}

func TestSnapshotManaRestoreEvents_EmptyReturnsNil(t *testing.T) {
	s := newManaTestState(t)
	s.resetManaRestoreEventsThisTickLocked()
	if got := s.snapshotManaRestoreEventsLocked(); got != nil {
		t.Errorf("empty queue should serialise to nil for JSON omitempty, got %v", got)
	}
}

func TestRepurposedLife_EmitsManaRestorePopupsForRecipients(t *testing.T) {
	// End-to-end: Siphoner with repurposed_life kills an actively-siphoned
	// enemy. Mana restore popups should fire for the Siphoner AND every
	// nearby ally with room in their mana pool.
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xDEAD)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner := spawnSpellcaster(s, "p1")
	siphoner.X, siphoner.Y = 100, 100
	siphoner.CurrentMana = 50 // has room for the restore
	siphoner.PerkIDs = append(siphoner.PerkIDs, "repurposed_life")
	siphoner.ChannelAbilityID = "siphon_life"

	ally := spawnSpellcaster(s, "p1")
	ally.X, ally.Y = 110, 100 // well within radius
	ally.CurrentMana = 50

	victim := &Unit{
		ID: s.nextUnitID, OwnerID: enemyPlayerID, UnitType: "soldier",
		Visible: true, HP: 1, MaxHP: 100,
		X: 200, Y: 100,
	}
	s.nextUnitID++
	s.addUnitLocked(victim)
	siphoner.ChannelTargetID = victim.ID

	s.resetManaRestoreEventsThisTickLocked()
	// Kill the victim through the canonical damage path.
	s.applyUnitDamageWithSourceLocked(victim, 999, DamageSource{
		AttackerUnitID: siphoner.ID, Kind: "ability",
	})
	s.drainPendingDeathsLocked()

	// Both siphoner and ally should have a mana-restore event queued.
	gotBySiphoner := false
	gotByAlly := false
	for _, e := range s.manaRestoreEventsThisTick {
		if e.UnitID == siphoner.ID {
			gotBySiphoner = true
		}
		if e.UnitID == ally.ID {
			gotByAlly = true
		}
	}
	if !gotBySiphoner {
		t.Errorf("expected mana restore event for siphoner (id=%d). Queue: %+v",
			siphoner.ID, s.manaRestoreEventsThisTick)
	}
	if !gotByAlly {
		t.Errorf("expected mana restore event for ally (id=%d). Queue: %+v",
			ally.ID, s.manaRestoreEventsThisTick)
	}
}
