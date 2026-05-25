package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// newChannelTestState returns a fresh GameState with seed 99. Lock is NOT held
// on return; callers take s.mu themselves.
func newChannelTestState(t *testing.T) *GameState {
	t.Helper()
	return NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 99)
}

// spawnSiphoner spawns an Acolyte promoted to the Siphoner path so it has
// siphon_life in its Abilities list. Mana pool is set to maxMana at full.
// Caller holds s.mu.
func spawnSiphoner(t *testing.T, s *GameState, owner string, x, y float64, maxMana int) *Unit {
	t.Helper()
	u := s.spawnPlayerUnitLocked("acolyte", owner, "#3498db", protocol.Vec2{X: x, Y: y})
	u.MaxHP = 200
	u.HP = 200
	u.Visible = true
	u.AttackRange = 220
	u.MaxMana = maxMana
	u.CurrentMana = maxMana
	u.ManaRegenPerSecond = 0 // disable regen so mana stays predictable
	u.Abilities = []string{"siphon_life"}
	return u
}

// spawnChannelEnemy spawns a visible enemy unit at (x, y) with the given HP / MaxHP.
// Named to avoid collision with spawnEnemy in gold_perks_test.go (different signature).
// Caller holds s.mu.
func spawnChannelEnemy(t *testing.T, s *GameState, x, y float64, hp, maxHP int) *Unit {
	t.Helper()
	u := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#ff0000", protocol.Vec2{X: x, Y: y})
	u.MaxHP = maxHP
	u.HP = hp
	u.Visible = true
	return u
}

// spawnChannelAlly spawns a visible ally of owner at (x, y) with given HP / MaxHP.
// Named to avoid collision with spawnAlly in gold_perks_test.go (different signature).
// Caller holds s.mu.
func spawnChannelAlly(t *testing.T, s *GameState, owner string, x, y float64, hp, maxHP int) *Unit {
	t.Helper()
	u := s.spawnPlayerUnitLocked("soldier", owner, "#00ff00", protocol.Vec2{X: x, Y: y})
	u.MaxHP = maxHP
	u.HP = hp
	u.Visible = true
	return u
}

// getSiphonLifeDef retrieves the siphon_life AbilityDef or fails the test.
func getSiphonLifeDef(t *testing.T) AbilityDef {
	t.Helper()
	def, ok := getAbilityDef("siphon_life")
	if !ok {
		t.Fatal("siphon_life ability def not registered; check catalog/abilities/siphon_life/siphon_life.json")
	}
	return def
}

// ── Channel start / stop happy path ──────────────────────────────────────────

// TestChannel_HappyPath_DamageAndManaDecay verifies that over a channel
// interval (TickIntervalSeconds) the target takes DamagePerTick damage and
// the caster loses ManaCostPerTick mana.
func TestChannel_HappyPath_DamageAndManaDecay(t *testing.T) {
	s := newChannelTestState(t)
	def := getSiphonLifeDef(t)

	s.mu.Lock()
	siphoner := spawnSiphoner(t, s, "p1", 100, 100, 50)
	enemy := spawnChannelEnemy(t, s, 200, 100, 200, 200)
	s.mu.Unlock()

	// Start the channel manually.
	s.mu.Lock()
	ok, reason := s.beginAbilityChannelLocked(siphoner, "siphon_life", enemy)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("beginAbilityChannelLocked failed: %q", reason)
	}

	// Tick exactly one channel interval so exactly one effect tick fires.
	interval := def.TickIntervalSeconds
	s.Update(interval)

	s.mu.RLock()
	gotMana := siphoner.CurrentMana
	gotEnemyHP := enemy.HP
	channeling := siphoner.ChannelAbilityID
	s.mu.RUnlock()

	wantMana := 50 - def.ManaCostPerTick
	if gotMana != wantMana {
		t.Errorf("mana after 1 tick: got %d, want %d", gotMana, wantMana)
	}
	wantEnemyHP := 200 - def.DamagePerTick
	if gotEnemyHP != wantEnemyHP {
		t.Errorf("enemy HP after 1 tick: got %d, want %d", gotEnemyHP, wantEnemyHP)
	}
	if channeling == "" {
		t.Error("siphoner should still be channeling after one interval")
	}
}

// TestChannel_ManaExhaustion stops with castFailNotEnoughMana.
func TestChannel_ManaExhaustion(t *testing.T) {
	s := newChannelTestState(t)
	def := getSiphonLifeDef(t)

	// Give the siphoner exactly enough mana for one tick.
	initialMana := def.ManaCostPerTick
	s.mu.Lock()
	siphoner := spawnSiphoner(t, s, "p1", 100, 100, initialMana)
	enemy := spawnChannelEnemy(t, s, 200, 100, 500, 500)
	s.mu.Unlock()

	s.mu.Lock()
	ok, _ := s.beginAbilityChannelLocked(siphoner, "siphon_life", enemy)
	s.mu.Unlock()
	if !ok {
		t.Fatal("beginAbilityChannelLocked should succeed with exactly one tick's worth of mana")
	}

	// Advance one tick — fires the effect, mana hits 0.
	s.Update(def.TickIntervalSeconds)

	s.mu.RLock()
	manaAfterFirst := siphoner.CurrentMana
	s.mu.RUnlock()
	if manaAfterFirst != 0 {
		t.Errorf("mana after first tick: got %d, want 0", manaAfterFirst)
	}

	// Advance another tick — cannot afford; channel must stop.
	s.Update(def.TickIntervalSeconds)

	s.mu.RLock()
	chanID := siphoner.ChannelAbilityID
	reason := siphoner.LastCastFailure
	beamCount := len(s.Beams)
	s.mu.RUnlock()

	if chanID != "" {
		t.Errorf("channel should have stopped after mana exhaustion; ChannelAbilityID = %q", chanID)
	}
	if reason != castFailNotEnoughMana {
		t.Errorf("LastCastFailure = %q; want %q", reason, castFailNotEnoughMana)
	}
	if beamCount != 0 {
		t.Errorf("beam should be despawned after channel stop; got %d beams", beamCount)
	}
}

// TestChannel_TargetDeath stops with castFailTargetLost.
func TestChannel_TargetDeath(t *testing.T) {
	s := newChannelTestState(t)
	def := getSiphonLifeDef(t)

	s.mu.Lock()
	siphoner := spawnSiphoner(t, s, "p1", 100, 100, 100)
	// Enemy has exactly DamagePerTick HP so first tick kills it.
	enemy := spawnChannelEnemy(t, s, 200, 100, def.DamagePerTick, def.DamagePerTick)
	s.mu.Unlock()

	s.mu.Lock()
	ok, _ := s.beginAbilityChannelLocked(siphoner, "siphon_life", enemy)
	s.mu.Unlock()
	if !ok {
		t.Fatal("beginAbilityChannelLocked should succeed")
	}

	// First tick kills the enemy; channel should auto-stop.
	s.Update(def.TickIntervalSeconds)

	s.mu.RLock()
	chanID := siphoner.ChannelAbilityID
	reason := siphoner.LastCastFailure
	beamCount := len(s.Beams)
	s.mu.RUnlock()

	if chanID != "" {
		t.Errorf("channel should stop after target dies; ChannelAbilityID = %q", chanID)
	}
	if reason != castFailTargetLost {
		t.Errorf("LastCastFailure = %q; want %q", reason, castFailTargetLost)
	}
	if beamCount != 0 {
		t.Errorf("beam should be despawned; got %d beams", beamCount)
	}
}

// TestChannel_OutOfRange stops with castFailTargetLost when the target moves
// out of cast range. We simulate this by directly moving the enemy.
func TestChannel_OutOfRange(t *testing.T) {
	s := newChannelTestState(t)
	def := getSiphonLifeDef(t)

	s.mu.Lock()
	siphoner := spawnSiphoner(t, s, "p1", 100, 100, 100)
	enemy := spawnChannelEnemy(t, s, 200, 100, 500, 500)
	s.mu.Unlock()

	s.mu.Lock()
	ok, _ := s.beginAbilityChannelLocked(siphoner, "siphon_life", enemy)
	s.mu.Unlock()
	if !ok {
		t.Fatal("beginAbilityChannelLocked should succeed")
	}

	// Move enemy far outside cast range.
	s.mu.Lock()
	castRange := float64(def.CastRange)
	enemy.X = siphoner.X + castRange + 100
	s.mu.Unlock()

	// One tick — target is out of range, channel must stop.
	s.Update(def.TickIntervalSeconds)

	s.mu.RLock()
	chanID := siphoner.ChannelAbilityID
	reason := siphoner.LastCastFailure
	s.mu.RUnlock()

	if chanID != "" {
		t.Errorf("channel should stop when target is out of range; ChannelAbilityID = %q", chanID)
	}
	if reason != castFailTargetLost {
		t.Errorf("LastCastFailure = %q; want %q", reason, castFailTargetLost)
	}
}

// TestChannel_MoveOrderCancels verifies that issuing a move order stops the channel.
func TestChannel_MoveOrderCancels(t *testing.T) {
	s := newChannelTestState(t)

	s.mu.Lock()
	siphoner := spawnSiphoner(t, s, "p1", 100, 100, 100)
	enemy := spawnChannelEnemy(t, s, 200, 100, 500, 500)
	s.mu.Unlock()

	s.mu.Lock()
	ok, _ := s.beginAbilityChannelLocked(siphoner, "siphon_life", enemy)
	s.mu.Unlock()
	if !ok {
		t.Fatal("beginAbilityChannelLocked should succeed")
	}

	// Issue a move order via resetUnitMovementLocked (the path all orders go through).
	s.mu.Lock()
	orderID := s.nextMovementOrderIDLocked()
	s.resetUnitMovementLocked(siphoner, orderID)
	s.mu.Unlock()

	s.mu.RLock()
	chanID := siphoner.ChannelAbilityID
	reason := siphoner.LastCastFailure
	beamCount := len(s.Beams)
	s.mu.RUnlock()

	if chanID != "" {
		t.Errorf("channel should stop after move order; ChannelAbilityID = %q", chanID)
	}
	if reason != channelInterruptedOrder {
		t.Errorf("LastCastFailure = %q; want %q", reason, channelInterruptedOrder)
	}
	if beamCount != 0 {
		t.Errorf("beam should be despawned after order cancel; got %d beams", beamCount)
	}
}

// TestChannel_StunCancels verifies that a stun stops the channel.
func TestChannel_StunCancels(t *testing.T) {
	s := newChannelTestState(t)
	def := getSiphonLifeDef(t)

	s.mu.Lock()
	siphoner := spawnSiphoner(t, s, "p1", 100, 100, 100)
	enemy := spawnChannelEnemy(t, s, 200, 100, 500, 500)
	s.mu.Unlock()

	s.mu.Lock()
	ok, _ := s.beginAbilityChannelLocked(siphoner, "siphon_life", enemy)
	siphoner.StunnedRemaining = 2.0 // stun applied mid-channel
	s.mu.Unlock()
	if !ok {
		t.Fatal("beginAbilityChannelLocked should succeed")
	}

	s.Update(def.TickIntervalSeconds)

	s.mu.RLock()
	chanID := siphoner.ChannelAbilityID
	reason := siphoner.LastCastFailure
	s.mu.RUnlock()

	if chanID != "" {
		t.Errorf("channel should stop when stunned; ChannelAbilityID = %q", chanID)
	}
	if reason != channelInterruptedStun {
		t.Errorf("LastCastFailure = %q; want %q", reason, channelInterruptedStun)
	}
}

// ── Healing distribution ──────────────────────────────────────────────────────

// TestChannel_HealSelf verifies that when the Siphoner is injured, healing
// routes to the Siphoner.
func TestChannel_HealSelf(t *testing.T) {
	s := newChannelTestState(t)
	def := getSiphonLifeDef(t)

	s.mu.Lock()
	siphoner := spawnSiphoner(t, s, "p1", 100, 100, 100)
	siphoner.HP = siphoner.MaxHP / 2 // injured
	enemy := spawnChannelEnemy(t, s, 200, 100, 500, 500)
	s.mu.Unlock()

	s.mu.Lock()
	ok, _ := s.beginAbilityChannelLocked(siphoner, "siphon_life", enemy)
	s.mu.Unlock()
	if !ok {
		t.Fatal("beginAbilityChannelLocked should succeed")
	}

	startHP := siphoner.HP
	s.Update(def.TickIntervalSeconds)

	s.mu.RLock()
	gotHP := siphoner.HP
	s.mu.RUnlock()

	healAmount := int(float64(def.DamagePerTick) * def.HealingMultiplier)
	wantHP := startHP + healAmount
	if wantHP > siphoner.MaxHP {
		wantHP = siphoner.MaxHP
	}
	if gotHP != wantHP {
		t.Errorf("siphoner HP after self-heal: got %d, want %d", gotHP, wantHP)
	}
}

// TestChannel_HealAlly verifies that when the Siphoner is at full HP, healing
// routes to the lowest-HP-percent ally within allyHealRadius.
func TestChannel_HealAlly(t *testing.T) {
	s := newChannelTestState(t)
	def := getSiphonLifeDef(t)

	s.mu.Lock()
	siphoner := spawnSiphoner(t, s, "p1", 100, 100, 100)
	// Siphoner at full HP — no self-heal path.
	ally := spawnChannelAlly(t, s, "p1", 150, 100, 50, 200) // 25% HP, within allyHealRadius
	enemy := spawnChannelEnemy(t, s, 200, 100, 500, 500)
	s.mu.Unlock()

	s.mu.Lock()
	ok, _ := s.beginAbilityChannelLocked(siphoner, "siphon_life", enemy)
	s.mu.Unlock()
	if !ok {
		t.Fatal("beginAbilityChannelLocked should succeed")
	}

	allyStartHP := ally.HP
	s.Update(def.TickIntervalSeconds)

	s.mu.RLock()
	allyHP := ally.HP
	s.mu.RUnlock()

	healAmount := int(float64(def.DamagePerTick) * def.HealingMultiplier)
	wantAllyHP := allyStartHP + healAmount
	if wantAllyHP > ally.MaxHP {
		wantAllyHP = ally.MaxHP
	}
	if allyHP != wantAllyHP {
		t.Errorf("ally HP after siphon heal: got %d, want %d", allyHP, wantAllyHP)
	}
}

// TestChannel_NoAllyInRadius verifies that when no injured ally is within
// allyHealRadius, healing is wasted (no error, channel continues).
func TestChannel_NoAllyInRadius(t *testing.T) {
	s := newChannelTestState(t)
	def := getSiphonLifeDef(t)

	s.mu.Lock()
	siphoner := spawnSiphoner(t, s, "p1", 100, 100, 100)
	// Siphoner at full HP.
	// Injured ally placed well outside allyHealRadius.
	allyHealRadius := def.AllyHealRadius
	ally := spawnChannelAlly(t, s, "p1", 100+allyHealRadius+200, 100, 50, 200)
	enemy := spawnChannelEnemy(t, s, 200, 100, 500, 500)
	s.mu.Unlock()

	s.mu.Lock()
	ok, _ := s.beginAbilityChannelLocked(siphoner, "siphon_life", enemy)
	s.mu.Unlock()
	if !ok {
		t.Fatal("beginAbilityChannelLocked should succeed")
	}

	allyStartHP := ally.HP
	s.Update(def.TickIntervalSeconds)

	s.mu.RLock()
	allyHP := ally.HP
	chanID := siphoner.ChannelAbilityID
	s.mu.RUnlock()

	if allyHP != allyStartHP {
		t.Errorf("ally outside radius should not be healed: HP changed from %d to %d", allyStartHP, allyHP)
	}
	if chanID == "" {
		t.Error("channel should continue (no error) when healing is wasted")
	}
}

// ── Beam lifecycle ────────────────────────────────────────────────────────────

// TestChannel_BeamSpawnedOnStart verifies that a Beam entity is created when
// the channel starts.
func TestChannel_BeamSpawnedOnStart(t *testing.T) {
	s := newChannelTestState(t)

	s.mu.Lock()
	siphoner := spawnSiphoner(t, s, "p1", 100, 100, 100)
	enemy := spawnChannelEnemy(t, s, 200, 100, 500, 500)
	s.mu.Unlock()

	s.mu.Lock()
	ok, _ := s.beginAbilityChannelLocked(siphoner, "siphon_life", enemy)
	beamCount := len(s.Beams)
	var beamCasterID, beamTargetID int
	if beamCount > 0 {
		beamCasterID = s.Beams[0].CasterUnitID
		beamTargetID = s.Beams[0].TargetUnitID
	}
	s.mu.Unlock()

	if !ok {
		t.Fatal("beginAbilityChannelLocked should succeed")
	}
	if beamCount != 1 {
		t.Fatalf("expected 1 beam after channel start, got %d", beamCount)
	}
	if beamCasterID != siphoner.ID {
		t.Errorf("beam CasterUnitID = %d; want %d", beamCasterID, siphoner.ID)
	}
	if beamTargetID != enemy.ID {
		t.Errorf("beam TargetUnitID = %d; want %d", beamTargetID, enemy.ID)
	}
}

// TestChannel_BeamRemovedOnStop verifies that the Beam is despawned when the
// channel stops (mana exhaustion path used here for simplicity).
func TestChannel_BeamRemovedOnStop(t *testing.T) {
	s := newChannelTestState(t)
	def := getSiphonLifeDef(t)

	// Exactly one tick of mana.
	s.mu.Lock()
	siphoner := spawnSiphoner(t, s, "p1", 100, 100, def.ManaCostPerTick)
	enemy := spawnChannelEnemy(t, s, 200, 100, 500, 500)
	s.mu.Unlock()

	s.mu.Lock()
	ok, _ := s.beginAbilityChannelLocked(siphoner, "siphon_life", enemy)
	s.mu.Unlock()
	if !ok {
		t.Fatal("beginAbilityChannelLocked should succeed")
	}

	// Two intervals: first fires (mana → 0), second stops channel.
	s.Update(def.TickIntervalSeconds)
	s.Update(def.TickIntervalSeconds)

	s.mu.RLock()
	beamCount := len(s.Beams)
	s.mu.RUnlock()

	if beamCount != 0 {
		t.Errorf("beam should be removed after channel stop; got %d beams", beamCount)
	}
}

// ── Auto-cast precondition ────────────────────────────────────────────────────

// TestAutocast_ChannelDoesNotStartWithFullHealthTeam verifies that the siphon_life
// auto-cast does NOT start when neither the Siphoner nor any ally is injured,
// even when a valid enemy is in range.
func TestAutocast_ChannelDoesNotStartWithFullHealthTeam(t *testing.T) {
	s := newChannelTestState(t)

	s.mu.Lock()
	siphoner := spawnSiphoner(t, s, "p1", 100, 100, 100)
	// Siphoner at full HP (set by spawnSiphoner).
	// Ally at full HP.
	_ = spawnChannelAlly(t, s, "p1", 150, 100, 200, 200)
	// Enemy in range.
	_ = spawnChannelEnemy(t, s, 200, 100, 500, 500)
	// Enable auto-cast.
	if siphoner.AutoCastEnabled == nil {
		siphoner.AutoCastEnabled = make(map[string]bool)
	}
	siphoner.AutoCastEnabled["siphon_life"] = true
	s.mu.Unlock()

	// Run several ticks — auto-cast must not start the channel.
	for i := 0; i < 10; i++ {
		s.Update(0.05)
	}

	s.mu.RLock()
	chanID := siphoner.ChannelAbilityID
	s.mu.RUnlock()

	if chanID != "" {
		t.Errorf("channel should NOT start when whole team is at full HP; ChannelAbilityID = %q", chanID)
	}
}

// TestAutocast_ChannelStartsWhenAllyInjured verifies that auto-cast starts
// the channel when an ally within allyHealRadius is injured.
func TestAutocast_ChannelStartsWhenAllyInjured(t *testing.T) {
	s := newChannelTestState(t)
	def := getSiphonLifeDef(t)

	s.mu.Lock()
	siphoner := spawnSiphoner(t, s, "p1", 100, 100, 100)
	// Injured ally within heal radius.
	_ = spawnChannelAlly(t, s, "p1", 150, 100, 50, 200)
	// Enemy in cast range.
	_ = spawnChannelEnemy(t, s, 200, 100, 500, 500)
	if siphoner.AutoCastEnabled == nil {
		siphoner.AutoCastEnabled = make(map[string]bool)
	}
	siphoner.AutoCastEnabled["siphon_life"] = true
	s.mu.Unlock()

	// Run a few ticks. Auto-cast should start the channel.
	for i := 0; i < 5; i++ {
		s.Update(def.TickIntervalSeconds)
	}

	s.mu.RLock()
	chanID := siphoner.ChannelAbilityID
	s.mu.RUnlock()

	if chanID == "" {
		t.Error("auto-cast should have started the channel when an ally is injured")
	}
}

// ── Determinism: tie-break by lowest unit ID ─────────────────────────────────

// TestChannel_AllyHealTieBreakByLowestID verifies that when two allies are at
// exactly equal HP%, the one with the lower unit ID receives the heal.
func TestChannel_AllyHealTieBreakByLowestID(t *testing.T) {
	s := newChannelTestState(t)
	def := getSiphonLifeDef(t)

	s.mu.Lock()
	siphoner := spawnSiphoner(t, s, "p1", 100, 100, 100)
	// Both allies at exactly 50% HP (same MaxHP so the ratio is identical).
	allyA := spawnChannelAlly(t, s, "p1", 120, 100, 100, 200)
	allyB := spawnChannelAlly(t, s, "p1", 130, 100, 100, 200)
	enemy := spawnChannelEnemy(t, s, 200, 100, 500, 500)
	s.mu.Unlock()

	// Determine which ally has the lower ID (IDs are assigned at spawn in
	// ascending order, so allyA.ID < allyB.ID, but let's not assume).
	var lowerIDAlly, higherIDAlly *Unit
	if allyA.ID < allyB.ID {
		lowerIDAlly, higherIDAlly = allyA, allyB
	} else {
		lowerIDAlly, higherIDAlly = allyB, allyA
	}

	s.mu.Lock()
	ok, _ := s.beginAbilityChannelLocked(siphoner, "siphon_life", enemy)
	s.mu.Unlock()
	if !ok {
		t.Fatal("beginAbilityChannelLocked should succeed")
	}

	startA := lowerIDAlly.HP
	startB := higherIDAlly.HP
	s.Update(def.TickIntervalSeconds)

	s.mu.RLock()
	hpLower := lowerIDAlly.HP
	hpHigher := higherIDAlly.HP
	s.mu.RUnlock()

	healAmount := int(float64(def.DamagePerTick) * def.HealingMultiplier)

	if hpLower != startA+healAmount {
		t.Errorf("lower-ID ally should receive heal; HP = %d, want %d", hpLower, startA+healAmount)
	}
	if hpHigher != startB {
		t.Errorf("higher-ID ally should NOT receive heal; HP = %d, want %d (unchanged)", hpHigher, startB)
	}
}

// ── Snapshot wiring ───────────────────────────────────────────────────────────

// TestChannel_SnapshotIncludesBeam verifies that an active beam appears in the
// MatchSnapshot.
func TestChannel_SnapshotIncludesBeam(t *testing.T) {
	s := newChannelTestState(t)

	s.mu.Lock()
	siphoner := spawnSiphoner(t, s, "p1", 100, 100, 100)
	enemy := spawnChannelEnemy(t, s, 200, 100, 500, 500)
	s.mu.Unlock()

	s.mu.Lock()
	ok, _ := s.beginAbilityChannelLocked(siphoner, "siphon_life", enemy)
	s.mu.Unlock()
	if !ok {
		t.Fatal("beginAbilityChannelLocked should succeed")
	}

	snap := s.Snapshot()
	if len(snap.Beams) == 0 {
		t.Fatal("MatchSnapshot.Beams should contain the active beam")
	}
	b := snap.Beams[0]
	if b.CasterUnitId != siphoner.ID {
		t.Errorf("beam CasterUnitId = %d; want %d", b.CasterUnitId, siphoner.ID)
	}
	if b.TargetUnitId != enemy.ID {
		t.Errorf("beam TargetUnitId = %d; want %d", b.TargetUnitId, enemy.ID)
	}
}

// TestChannel_SnapshotBeamAbsentAfterStop verifies that a stopped channel's
// beam is absent from the snapshot.
func TestChannel_SnapshotBeamAbsentAfterStop(t *testing.T) {
	s := newChannelTestState(t)
	def := getSiphonLifeDef(t)

	// One tick of mana only.
	s.mu.Lock()
	siphoner := spawnSiphoner(t, s, "p1", 100, 100, def.ManaCostPerTick)
	enemy := spawnChannelEnemy(t, s, 200, 100, 500, 500)
	s.mu.Unlock()

	s.mu.Lock()
	ok, _ := s.beginAbilityChannelLocked(siphoner, "siphon_life", enemy)
	s.mu.Unlock()
	if !ok {
		t.Fatal("beginAbilityChannelLocked should succeed")
	}

	// Exhaust mana: first tick fires, second tick stops channel.
	s.Update(def.TickIntervalSeconds)
	s.Update(def.TickIntervalSeconds)

	snap := s.Snapshot()
	if len(snap.Beams) != 0 {
		t.Errorf("MatchSnapshot.Beams should be empty after channel stops; got %d beams", len(snap.Beams))
	}
}

// TestChannel_AbilitySnapshotChannelingFlag verifies the Channeling field on
// AbilitySnapshot is set while the unit is channeling siphon_life.
func TestChannel_AbilitySnapshotChannelingFlag(t *testing.T) {
	s := newChannelTestState(t)

	s.mu.Lock()
	siphoner := spawnSiphoner(t, s, "p1", 100, 100, 100)
	enemy := spawnChannelEnemy(t, s, 200, 100, 500, 500)
	s.mu.Unlock()

	s.mu.Lock()
	ok, _ := s.beginAbilityChannelLocked(siphoner, "siphon_life", enemy)
	s.mu.Unlock()
	if !ok {
		t.Fatal("beginAbilityChannelLocked should succeed")
	}

	s.mu.RLock()
	snaps := s.abilityStatesLocked(siphoner)
	s.mu.RUnlock()

	var found bool
	for _, a := range snaps {
		if a.ID == "siphon_life" {
			found = true
			if !a.Channeling {
				t.Errorf("AbilitySnapshot.Channeling should be true while channeling siphon_life")
			}
		}
	}
	if !found {
		t.Error("siphon_life ability snapshot not found")
	}
}
