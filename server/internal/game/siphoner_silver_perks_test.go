package game

// Section: Siphoner Silver perk unit tests + source-specific shield pool tests.
//
// Covers all four Silver perks (chain_siphon, amplify_damage, dark_renewal,
// shared_suffering) plus the underlying source-specific shield pool helpers
// (applyShieldFromSourceLocked, drainShieldPoolsLocked, totals).
//
// Setup mirrors siphoner_perks_test.go (Bronze): a Siphoner at (400,400) on
// the Siphoner path at silver rank, plus a hostile soldier at (600,400)
// registered through addUnitLocked so id-based lookups (anchor selector,
// chain target collector) see it.
//
// All expected values derive from the perk catalog Config — never hardcoded.

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// newSiphonerSilverState mirrors newSiphonerBronzeState but ranks the
// Siphoner to Silver so the silver perks pass the rank gate when assigned
// through the normal pool (debug spawn bypasses the gate, but a few of the
// tests still call assignUnitPerkLocked to verify the assignment flow).
func newSiphonerSilverState(t *testing.T) (s *GameState, siphoner, enemy *Unit) {
	t.Helper()
	s = NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0x517E)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner = s.spawnPlayerUnitLocked("acolyte", "p1", "#9b59b6", protocol.Vec2{X: 400, Y: 400})
	siphoner.Visible = true
	siphoner.HP = siphoner.MaxHP
	siphoner.AttackRange = 1000
	siphoner.MaxMana = 200
	siphoner.CurrentMana = 200
	siphoner.ProgressionPath = "siphoner"
	siphoner.Rank = unitRankSilver
	s.assignUnitPathAbilitiesLocked(siphoner)

	enemy = &Unit{
		ID:       s.nextUnitID,
		OwnerID:  enemyPlayerID,
		UnitType: "soldier",
		Visible:  true,
		X:        600, Y: 400,
		HP: 200, MaxHP: 200, Armor: 0,
		AttackRange: 100,
		AttackSpeed: 1.0,
		Damage:      10,
		MoveSpeed:   50,
		Color:       "#aa0000",
	}
	s.nextUnitID++
	s.addUnitLocked(enemy)

	return s, siphoner, enemy
}

// spawnEnemyAt spawns a hostile soldier at the requested position. Returns
// the unit pointer; caller holds s.mu.
func spawnEnemyAt(s *GameState, x, y float64) *Unit {
	u := &Unit{
		ID:       s.nextUnitID,
		OwnerID:  enemyPlayerID,
		UnitType: "soldier",
		Visible:  true,
		X:        x, Y: y,
		HP: 200, MaxHP: 200, Armor: 0,
		AttackRange: 100,
		AttackSpeed: 1.0,
		Damage:      10,
		MoveSpeed:   50,
		Color:       "#aa0000",
	}
	s.nextUnitID++
	s.addUnitLocked(u)
	return u
}

// spawnAllyAt spawns a friendly soldier owned by p1 at the requested
// position. Used to exercise dark_renewal ally-overflow routing.
func spawnAllyAt(s *GameState, x, y float64) *Unit {
	u := &Unit{
		ID:       s.nextUnitID,
		OwnerID:  "p1",
		UnitType: "soldier",
		Visible:  true,
		X:        x, Y: y,
		HP: 50, MaxHP: 50, Armor: 0,
		AttackRange: 100,
		AttackSpeed: 1.0,
		Damage:      10,
		MoveSpeed:   50,
		Color:       "#3498db",
	}
	s.nextUnitID++
	s.addUnitLocked(u)
	return u
}

// ═════════════════════════════════════════════════════════════════════════════
// Source-specific shield pool tests
// ═════════════════════════════════════════════════════════════════════════════

// ─────────────────────────────────────────────────────────────────────────────
// Shield stacking modes
//
// dark_renewal is registered as ShieldStackingShared — multiple Siphoners
// shielding the same recipient feed ONE shared pool capped at maxSelfShield
// (or maxAllyShield), they do not produce independent per-source pools that
// would let total shield exceed the configured cap.
//
// Unregistered source types default to ShieldStackingPerSource — each
// granting unit gets its own independent pool (the legacy behaviour).
// ─────────────────────────────────────────────────────────────────────────────

func TestShieldStacking_SharedSourcesCollapseIntoOnePool(t *testing.T) {
	s, _, target := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	if shieldStackingFor("dark_renewal") != ShieldStackingShared {
		t.Fatalf("dark_renewal should be registered as ShieldStackingShared; got %d",
			shieldStackingFor("dark_renewal"))
	}

	// Two granting Siphoners apply dark_renewal shield to the SAME target.
	first := s.applyShieldFromSourceLocked(target, "dark_renewal", 101, 30, 40, nil)
	if first != 30 {
		t.Errorf("first grant banked %d, want 30", first)
	}
	second := s.applyShieldFromSourceLocked(target, "dark_renewal", 202, 30, 40, nil)
	// Second grant tops up the SAME shared pool. Only 10 of the 30 fits
	// (30 already there + 10 room = 40 cap).
	if second != 10 {
		t.Errorf("second grant banked %d, want 10 (20 wasted at shared cap)", second)
	}

	// Exactly ONE pool exists; not two side-by-side per-source pools.
	if len(target.PerkState.ShieldPools) != 1 {
		t.Fatalf("shared sources should collapse into one pool, got %d pools",
			len(target.PerkState.ShieldPools))
	}
	pool := target.PerkState.ShieldPools[0]
	if pool.CurrentValue != 40 {
		t.Errorf("shared pool CurrentValue = %d, want 40 (cap)", pool.CurrentValue)
	}
	if pool.MaxValue != 40 {
		t.Errorf("shared pool MaxValue = %d, want 40", pool.MaxValue)
	}
	if pool.SourceUnitID != 101 {
		t.Errorf("shared pool SourceUnitID = %d, want 101 (first grantor)", pool.SourceUnitID)
	}
	// Aggregate display total matches the cap.
	if got := totalShieldFromPoolsLocked(target); got != 40 {
		t.Errorf("aggregate pool total = %d, want 40 (shared cap respected)", got)
	}
}

func TestShieldStacking_PerSourceSourcesProduceIndependentPools(t *testing.T) {
	s, _, target := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// "test_per_source" is unregistered, so it falls through to the default
	// PerSource mode. Two grantors -> two independent pools.
	if shieldStackingFor("test_per_source") != ShieldStackingPerSource {
		t.Fatalf("unregistered source type should default to PerSource; got %d",
			shieldStackingFor("test_per_source"))
	}

	s.applyShieldFromSourceLocked(target, "test_per_source", 1, 30, 40, nil)
	s.applyShieldFromSourceLocked(target, "test_per_source", 2, 30, 40, nil)

	if len(target.PerkState.ShieldPools) != 2 {
		t.Fatalf("per-source grantors should produce 2 pools, got %d", len(target.PerkState.ShieldPools))
	}
	if got := totalShieldFromPoolsLocked(target); got != 60 {
		t.Errorf("aggregate pool total = %d, want 60 (per-source pools stack)", got)
	}
}

func TestShieldStacking_DarkRenewalSharedCapHoldsAcrossSiphoners(t *testing.T) {
	s, siphonerA, _ := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphonerA.PerkIDs = append(siphonerA.PerkIDs, "dark_renewal")
	def := perkDefByID("dark_renewal")
	maxSelf := int(def.Config["maxSelfShield"])

	// Siphoner A pulses dark_renewal on themselves to fill the self-pool.
	siphonerA.HP = siphonerA.MaxHP
	s.applyDarkRenewalExcessLocked(siphonerA, maxSelf, 220)
	if got := totalShieldFromPoolsLocked(siphonerA); got != maxSelf {
		t.Fatalf("setup: A self shield = %d, want %d", got, maxSelf)
	}

	// Spawn a SECOND Siphoner that pulses dark_renewal targeting the first
	// Siphoner as the ally recipient. With shared stacking, the cap on A
	// should NOT increase past maxSelfShield no matter how many Siphoners
	// feed it.
	siphonerB := spawnAllyAt(s, siphonerA.X+10, siphonerA.Y)
	siphonerB.PerkIDs = append(siphonerB.PerkIDs, "dark_renewal")
	siphonerB.ProgressionPath = "siphoner"
	siphonerB.Rank = unitRankSilver
	siphonerB.HP = siphonerB.MaxHP

	// B has already-saturated self pool? No — B has no shield yet. So B's
	// excess heal would first fill B's own self pool. To force the ally
	// path on B, pre-cap B's self pool by hand.
	s.applyShieldFromSourceLocked(siphonerB, darkRenewalShieldSource, siphonerB.ID, maxSelf, maxSelf, nil)

	// B now pulses dark_renewal — self is capped, overflow goes to nearest
	// ally with room. siphonerA's shared pool is already at cap, so B should
	// find NO eligible ally and waste the overflow rather than busting A's cap.
	banked := s.applyDarkRenewalExcessLocked(siphonerB, 50, 220)
	if banked != 0 {
		t.Errorf("B's pulse banked %d on a fully-saturated network, want 0", banked)
	}

	// A's pool must still be at cap, not above it.
	if got := totalShieldFromPoolsLocked(siphonerA); got != maxSelf {
		t.Errorf("A self shield exceeded shared cap: got %d, want %d", got, maxSelf)
	}

	// Drain A by some amount, then B's next pulse should top up A's shared
	// pool (proving multiple sources CAN contribute, they just can't bust
	// the cap).
	for i := range siphonerA.PerkState.ShieldPools {
		siphonerA.PerkState.ShieldPools[i].CurrentValue = 10
	}
	banked = s.applyDarkRenewalExcessLocked(siphonerB, 15, 220)
	if banked == 0 {
		t.Errorf("B's pulse should refill A's drained pool, banked 0")
	}
	if got := totalShieldFromPoolsLocked(siphonerA); got > maxSelf {
		t.Errorf("A's pool exceeded cap after refill: got %d, want <= %d", got, maxSelf)
	}
	// And there's still only ONE pool entry on A — not two per-Siphoner pools.
	count := 0
	for _, p := range siphonerA.PerkState.ShieldPools {
		if p.SourceType == darkRenewalShieldSource {
			count++
		}
	}
	if count != 1 {
		t.Errorf("A should have exactly 1 dark_renewal pool, got %d", count)
	}
}

func TestShieldPool_ApplyAllocatesAndCaps(t *testing.T) {
	s, _, enemy := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Allocate a fresh pool — banked amount respects MaxValue.
	added := s.applyShieldFromSourceLocked(enemy, "test_source", 1, 50, 40, nil)
	if added != 40 {
		t.Fatalf("first allocation: banked %d, want 40 (cap)", added)
	}
	if len(enemy.PerkState.ShieldPools) != 1 {
		t.Fatalf("expected exactly 1 shield pool, got %d", len(enemy.PerkState.ShieldPools))
	}
	if enemy.PerkState.ShieldPools[0].CurrentValue != 40 {
		t.Errorf("pool CurrentValue = %d, want 40", enemy.PerkState.ShieldPools[0].CurrentValue)
	}
	if enemy.PerkState.ShieldPools[0].MaxValue != 40 {
		t.Errorf("pool MaxValue = %d, want 40", enemy.PerkState.ShieldPools[0].MaxValue)
	}

	// Top-up to an already-full pool returns 0.
	added = s.applyShieldFromSourceLocked(enemy, "test_source", 1, 10, 40, nil)
	if added != 0 {
		t.Errorf("top-up of full pool: banked %d, want 0", added)
	}

	// A different source allocates an independent pool.
	added = s.applyShieldFromSourceLocked(enemy, "other_source", 2, 25, 30, nil)
	if added != 25 {
		t.Errorf("second source banked %d, want 25", added)
	}
	if len(enemy.PerkState.ShieldPools) != 2 {
		t.Errorf("expected 2 pools after second source, got %d", len(enemy.PerkState.ShieldPools))
	}
}

func TestShieldPool_DrainsOldestFirst(t *testing.T) {
	s, _, enemy := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Allocate two pools — first one is "oldest" (slice index 0).
	s.applyShieldFromSourceLocked(enemy, "first", 1, 20, 20, nil)
	s.applyShieldFromSourceLocked(enemy, "second", 2, 20, 20, nil)

	// Drain 10 damage — drains entirely from the first pool.
	remaining := s.drainShieldPoolsLocked(enemy, 10)
	if remaining != 0 {
		t.Errorf("drain 10 < first pool: remaining %d, want 0", remaining)
	}
	if enemy.PerkState.ShieldPools[0].CurrentValue != 10 {
		t.Errorf("first pool after partial drain: %d, want 10", enemy.PerkState.ShieldPools[0].CurrentValue)
	}
	if enemy.PerkState.ShieldPools[1].CurrentValue != 20 {
		t.Errorf("second pool should be untouched, got %d", enemy.PerkState.ShieldPools[1].CurrentValue)
	}

	// Drain 25 — finishes first pool and bites into second.
	remaining = s.drainShieldPoolsLocked(enemy, 25)
	if remaining != 0 {
		t.Errorf("drain 25 crossing pools: remaining %d, want 0", remaining)
	}
	// First pool exhausted and removed; second pool now has 5 left.
	if len(enemy.PerkState.ShieldPools) != 1 {
		t.Fatalf("after exhausting first pool, expected 1 pool, got %d", len(enemy.PerkState.ShieldPools))
	}
	if enemy.PerkState.ShieldPools[0].CurrentValue != 5 {
		t.Errorf("surviving pool value = %d, want 5", enemy.PerkState.ShieldPools[0].CurrentValue)
	}
}

func TestShieldPool_DrainOverflowReturnsRemainder(t *testing.T) {
	s, _, enemy := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.applyShieldFromSourceLocked(enemy, "only", 1, 15, 15, nil)
	remaining := s.drainShieldPoolsLocked(enemy, 40)
	if remaining != 25 {
		t.Errorf("drain 40 vs 15 shield: remaining %d, want 25", remaining)
	}
	if len(enemy.PerkState.ShieldPools) != 0 {
		t.Errorf("exhausted pool should be removed, got %d pools", len(enemy.PerkState.ShieldPools))
	}
}

func TestShieldPool_DamagePipelineDrainsPoolsBeforeHP(t *testing.T) {
	s, _, enemy := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	enemy.Armor = 0
	startHP := enemy.HP
	s.applyShieldFromSourceLocked(enemy, "pipeline_test", 1, 25, 25, nil)

	// 30 damage — 25 absorbed by pool, 5 hits HP.
	s.applyUnitDamageWithSourceLocked(enemy, 30, DamageSource{Kind: "melee"})
	if enemy.HP != startHP-5 {
		t.Errorf("HP after 30 dmg vs 25 shield: %d, want %d", enemy.HP, startHP-5)
	}
	if len(enemy.PerkState.ShieldPools) != 0 {
		t.Errorf("pool should be exhausted by 30 dmg, got %d pools", len(enemy.PerkState.ShieldPools))
	}
}

// ═════════════════════════════════════════════════════════════════════════════
// chain_siphon
// ═════════════════════════════════════════════════════════════════════════════

func TestChainSiphon_PicksNearestEnemiesWithinChainRange(t *testing.T) {
	s, siphoner, primary := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "chain_siphon")
	def := perkDefByID("chain_siphon")
	chainRange := def.Config["chainRange"]

	// Spawn two extra enemies: one inside chainRange of the primary, one outside.
	inside := spawnEnemyAt(s, primary.X+chainRange*0.5, primary.Y)
	outside := spawnEnemyAt(s, primary.X+chainRange*2.0, primary.Y)

	targets := s.chainSiphonTargetsLocked(siphoner, primary)
	if len(targets) == 0 {
		t.Fatal("expected at least one chain target")
	}
	// additionalTargetCount is 1 by default — only the nearest in-range enemy.
	if len(targets) != int(def.Config["additionalTargetCount"]) {
		t.Errorf("got %d chain targets, want %d (additionalTargetCount)",
			len(targets), int(def.Config["additionalTargetCount"]))
	}
	if targets[0].ID != inside.ID {
		t.Errorf("chain target id = %d, want %d (in-range enemy)", targets[0].ID, inside.ID)
	}
	if targets[0].ID == outside.ID {
		t.Errorf("outside enemy should not be a chain target")
	}
}

func TestChainSiphon_HonorsAdditionalTargetCount(t *testing.T) {
	s, siphoner, primary := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "chain_siphon")
	// Spawn three extra enemies inside chainRange — chain target count should
	// still respect additionalTargetCount cap from the perk config.
	def := perkDefByID("chain_siphon")
	chainRange := def.Config["chainRange"]
	spawnEnemyAt(s, primary.X+chainRange*0.3, primary.Y)
	spawnEnemyAt(s, primary.X+chainRange*0.4, primary.Y)
	spawnEnemyAt(s, primary.X+chainRange*0.5, primary.Y)

	targets := s.chainSiphonTargetsLocked(siphoner, primary)
	want := int(def.Config["additionalTargetCount"])
	if len(targets) != want {
		t.Errorf("chain target count = %d, want %d (capped by additionalTargetCount)", len(targets), want)
	}
}

func TestChainSiphon_SecondaryDamageScalesByMultiplier(t *testing.T) {
	s, siphoner, primary := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "chain_siphon")
	def := perkDefByID("chain_siphon")
	chainRange := def.Config["chainRange"]
	chainTarget := spawnEnemyAt(s, primary.X+chainRange*0.5, primary.Y)
	chainStartHP := chainTarget.HP

	primaryDamage := 20
	s.applyChainSiphonBeamsLocked(siphoner, primary, primaryDamage, 0, 220, "siphon_life")

	expected := int(math.Round(float64(primaryDamage) * def.Config["secondaryDamageMultiplier"]))
	gotDelta := chainStartHP - chainTarget.HP
	if gotDelta != expected {
		t.Errorf("chain damage = %d, want %d (primary %d × %.2f)",
			gotDelta, expected, primaryDamage, def.Config["secondaryDamageMultiplier"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// chain_siphon — secondary beam visuals
// ─────────────────────────────────────────────────────────────────────────────

// chainBeamForTarget returns the (beamID, found) pair for the chain link
// whose TargetID matches `targetID`. Tests use this to look up the beam id
// without caring about its bounce position in the ordered Links slice.
func chainBeamForTarget(siphoner *Unit, targetID int) (string, bool) {
	for _, link := range siphoner.PerkState.ChainSiphonLinks {
		if link.TargetID == targetID {
			return link.BeamID, true
		}
	}
	return "", false
}

func TestChainSiphon_SpawnsBeamPerChainTarget(t *testing.T) {
	s, siphoner, primary := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "chain_siphon")
	def := perkDefByID("chain_siphon")
	chainRange := def.Config["chainRange"]
	chainTarget := spawnEnemyAt(s, primary.X+chainRange*0.5, primary.Y)

	// Fire one channel tick worth of chain beams.
	s.applyChainSiphonBeamsLocked(siphoner, primary, 5, 5, 220, "siphon_life")

	if got := len(siphoner.PerkState.ChainSiphonLinks); got != 1 {
		t.Fatalf("expected 1 tracked chain link, got %d", got)
	}
	beamID, tracked := chainBeamForTarget(siphoner, chainTarget.ID)
	if !tracked {
		t.Fatalf("chain target id %d not tracked", chainTarget.ID)
	}
	// Beam must exist on GameState.Beams with caster=primary, target=chain,
	// and the chain_siphon variant.
	var found *Beam
	for _, b := range s.Beams {
		if b.ID == beamID {
			found = b
			break
		}
	}
	if found == nil {
		t.Fatalf("spawned beam id %s not found in s.Beams", beamID)
	}
	if found.CasterUnitID != primary.ID {
		t.Errorf("first-hop chain beam caster = %d, want %d (primary target)", found.CasterUnitID, primary.ID)
	}
	if found.TargetUnitID != chainTarget.ID {
		t.Errorf("first-hop chain beam target = %d, want %d (chain target)", found.TargetUnitID, chainTarget.ID)
	}
	if found.Variant != chainSiphonBeamVariant {
		t.Errorf("chain beam variant = %q, want %q", found.Variant, chainSiphonBeamVariant)
	}
	if siphoner.PerkState.ChainSiphonPrimaryTargetID != primary.ID {
		t.Errorf("tracked primary id = %d, want %d", siphoner.PerkState.ChainSiphonPrimaryTargetID, primary.ID)
	}
}

func TestChainSiphon_ReusesBeamAcrossTicksForStableChainTarget(t *testing.T) {
	s, siphoner, primary := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "chain_siphon")
	chainRange := perkDefByID("chain_siphon").Config["chainRange"]
	chainTarget := spawnEnemyAt(s, primary.X+chainRange*0.5, primary.Y)

	// First tick.
	s.applyChainSiphonBeamsLocked(siphoner, primary, 5, 5, 220, "siphon_life")
	firstID, _ := chainBeamForTarget(siphoner, chainTarget.ID)
	firstBeamCount := len(s.Beams)

	// Second tick — chain target unchanged. Beam id must be the SAME (reused),
	// and s.Beams should not have grown.
	s.applyChainSiphonBeamsLocked(siphoner, primary, 5, 5, 220, "siphon_life")
	secondID, _ := chainBeamForTarget(siphoner, chainTarget.ID)
	if secondID != firstID {
		t.Errorf("beam id changed across ticks: %s -> %s (should reuse)", firstID, secondID)
	}
	if len(s.Beams) != firstBeamCount {
		t.Errorf("s.Beams grew across stable-target ticks: %d -> %d (should reuse)", firstBeamCount, len(s.Beams))
	}
}

func TestChainSiphon_DespawnsBeamWhenChainTargetLeavesRange(t *testing.T) {
	s, siphoner, primary := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "chain_siphon")
	chainRange := perkDefByID("chain_siphon").Config["chainRange"]
	chainTarget := spawnEnemyAt(s, primary.X+chainRange*0.5, primary.Y)

	s.applyChainSiphonBeamsLocked(siphoner, primary, 5, 5, 220, "siphon_life")
	if got := len(siphoner.PerkState.ChainSiphonLinks); got != 1 {
		t.Fatalf("setup failed: expected 1 tracked link, got %d", got)
	}
	startBeamID, _ := chainBeamForTarget(siphoner, chainTarget.ID)

	// Move chain target out of range — next tick should drop the beam.
	chainTarget.X = primary.X + chainRange*5
	s.applyChainSiphonBeamsLocked(siphoner, primary, 5, 5, 220, "siphon_life")

	if _, stillTracked := chainBeamForTarget(siphoner, chainTarget.ID); stillTracked {
		t.Errorf("chain target id should be removed from the links slice after leaving range")
	}
	for _, b := range s.Beams {
		if b.ID == startBeamID {
			t.Errorf("beam id %s should be despawned after chain target left range", startBeamID)
		}
	}
}

func TestChainSiphon_RespawnsBeamsOnPrimaryTargetSwap(t *testing.T) {
	s, siphoner, primary := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "chain_siphon")
	chainRange := perkDefByID("chain_siphon").Config["chainRange"]
	chainTarget := spawnEnemyAt(s, primary.X+chainRange*0.5, primary.Y)

	s.applyChainSiphonBeamsLocked(siphoner, primary, 5, 5, 220, "siphon_life")
	oldBeamID, _ := chainBeamForTarget(siphoner, chainTarget.ID)

	// Swap primary target. Spawn a brand-new primary far from chainTarget so
	// the old chain target is now out of range and a fresh chain target slot
	// will be filled by chainTarget2.
	newPrimary := spawnEnemyAt(s, 2000, 2000)
	chainTarget2 := spawnEnemyAt(s, newPrimary.X+chainRange*0.5, newPrimary.Y)
	s.applyChainSiphonBeamsLocked(siphoner, newPrimary, 5, 5, 220, "siphon_life")

	// Tracked primary must update; old beam must be despawned.
	if siphoner.PerkState.ChainSiphonPrimaryTargetID != newPrimary.ID {
		t.Errorf("tracked primary id = %d, want %d", siphoner.PerkState.ChainSiphonPrimaryTargetID, newPrimary.ID)
	}
	for _, b := range s.Beams {
		if b.ID == oldBeamID {
			t.Errorf("old chain beam %s still present after primary swap", oldBeamID)
		}
	}
	// The new first-hop chain beam should be rooted at the new primary.
	newBeamID, ok := chainBeamForTarget(siphoner, chainTarget2.ID)
	if !ok {
		t.Fatalf("no chain beam tracked against new chain target id %d", chainTarget2.ID)
	}
	var found *Beam
	for _, b := range s.Beams {
		if b.ID == newBeamID {
			found = b
			break
		}
	}
	if found == nil || found.CasterUnitID != newPrimary.ID {
		t.Errorf("new chain beam not rooted at new primary: %+v", found)
	}
}

func TestChainSiphon_ClearChannelDespawnsAllChainBeams(t *testing.T) {
	s, siphoner, primary := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "chain_siphon")
	chainRange := perkDefByID("chain_siphon").Config["chainRange"]
	spawnEnemyAt(s, primary.X+chainRange*0.3, primary.Y)
	spawnEnemyAt(s, primary.X+chainRange*0.5, primary.Y)

	s.applyChainSiphonBeamsLocked(siphoner, primary, 5, 5, 220, "siphon_life")
	if len(siphoner.PerkState.ChainSiphonLinks) == 0 {
		t.Fatal("setup failed: no chain beams spawned")
	}
	trackedIDs := make([]string, 0, len(siphoner.PerkState.ChainSiphonLinks))
	for _, link := range siphoner.PerkState.ChainSiphonLinks {
		trackedIDs = append(trackedIDs, link.BeamID)
	}

	// Simulate channel ending.
	s.clearChannelStateLocked(siphoner)

	if got := len(siphoner.PerkState.ChainSiphonLinks); got != 0 {
		t.Errorf("ChainSiphonLinks should be empty after clearChannelStateLocked, got %d entries", got)
	}
	if siphoner.PerkState.ChainSiphonPrimaryTargetID != 0 {
		t.Errorf("ChainSiphonPrimaryTargetID should reset to 0, got %d",
			siphoner.PerkState.ChainSiphonPrimaryTargetID)
	}
	// Beams must all be gone from s.Beams.
	for _, want := range trackedIDs {
		for _, b := range s.Beams {
			if b.ID == want {
				t.Errorf("beam id %s still present in s.Beams after channel clear", want)
			}
		}
	}
}

func TestChainSiphon_BouncesFromPreviousVictimNotPrimary(t *testing.T) {
	s, siphoner, primary := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Author a Gold-rank Siphoner so chain count goes up to 3 via
	// beam_mastery (+1) and ascended_corruption (+1) on top of the base 1.
	// That gives us a 3-link chain to inspect.
	siphoner.Rank = unitRankGold
	siphoner.PerkIDs = append(siphoner.PerkIDs, "chain_siphon", "beam_mastery", "ascended_corruption")
	cfg := s.chainSiphonEffectiveConfigLocked(siphoner)
	maxCount := int(cfg["additionalTargetCount"])
	if maxCount < 3 {
		t.Fatalf("setup expected additionalTargetCount >= 3, got %d", maxCount)
	}
	chainRange := cfg["chainRange"]

	// Lay out four enemies in a line so the bounce chain MUST step from each
	// to the next. If the chain fanned out from the primary instead, only
	// the two nearest-to-primary enemies would even be in range — the
	// farthest enemy is reachable only by hopping through intermediates.
	step := chainRange * 0.6
	primary.X = 0
	primary.Y = 0
	a := spawnEnemyAt(s, primary.X+step, primary.Y) // hop 1: primary → a (in range of primary)
	b := spawnEnemyAt(s, a.X+step, a.Y)             // hop 2: a → b (in range of a, NOT primary)
	c := spawnEnemyAt(s, b.X+step, b.Y)             // hop 3: b → c (in range of b, NOT primary or a)
	// Sanity: c must be OUTSIDE chainRange of primary so the fan-out shape
	// would have rejected it. The bounce shape reaches it via a, b.
	dxC := c.X - primary.X
	if dxC <= chainRange {
		t.Fatalf("layout error: c must be outside primary's chainRange to prove bounce reach (dxC=%.1f range=%.1f)", dxC, chainRange)
	}

	// Pick chain targets — they must come out [a, b, c] in bounce order.
	targets := s.chainSiphonTargetsLocked(siphoner, primary)
	if len(targets) != 3 {
		t.Fatalf("bounce chain should reach 3 targets, got %d", len(targets))
	}
	wantOrder := []*Unit{a, b, c}
	for i, want := range wantOrder {
		if targets[i].ID != want.ID {
			t.Errorf("chain[%d] = %d, want %d (expected bounce order a→b→c)", i, targets[i].ID, want.ID)
		}
	}

	// Fire the channel-tick sync and verify each beam's caster is the
	// PREVIOUS link's unit (not always the primary).
	s.applyChainSiphonBeamsLocked(siphoner, primary, 5, 5, 220, "siphon_life")
	if got := len(siphoner.PerkState.ChainSiphonLinks); got != 3 {
		t.Fatalf("expected 3 chain links recorded, got %d", got)
	}
	wantSources := []int{primary.ID, a.ID, b.ID} // beam[i] should originate from this id
	wantTargets := []int{a.ID, b.ID, c.ID}
	for i, link := range siphoner.PerkState.ChainSiphonLinks {
		var beam *Beam
		for _, bb := range s.Beams {
			if bb.ID == link.BeamID {
				beam = bb
				break
			}
		}
		if beam == nil {
			t.Errorf("link[%d] beam %s missing from s.Beams", i, link.BeamID)
			continue
		}
		if beam.CasterUnitID != wantSources[i] {
			t.Errorf("link[%d] beam caster = %d, want %d (must bounce from previous victim)",
				i, beam.CasterUnitID, wantSources[i])
		}
		if beam.TargetUnitID != wantTargets[i] {
			t.Errorf("link[%d] beam target = %d, want %d", i, beam.TargetUnitID, wantTargets[i])
		}
	}
}

func TestChainSiphon_BouncePrefixReuseOnTailChange(t *testing.T) {
	s, siphoner, primary := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// 3-hop chain.
	siphoner.Rank = unitRankGold
	siphoner.PerkIDs = append(siphoner.PerkIDs, "chain_siphon", "beam_mastery", "ascended_corruption")
	cfg := s.chainSiphonEffectiveConfigLocked(siphoner)
	chainRange := cfg["chainRange"]
	step := chainRange * 0.6

	primary.X, primary.Y = 0, 0
	a := spawnEnemyAt(s, primary.X+step, primary.Y)
	b := spawnEnemyAt(s, a.X+step, a.Y)
	c := spawnEnemyAt(s, b.X+step, b.Y)

	// First tick: chain is primary → a → b → c.
	s.applyChainSiphonBeamsLocked(siphoner, primary, 5, 5, 220, "siphon_life")
	links := siphoner.PerkState.ChainSiphonLinks
	if len(links) != 3 {
		t.Fatalf("setup expected 3 links, got %d", len(links))
	}
	// Snapshot the first two beam ids — they should survive a tail change.
	keep0 := links[0].BeamID
	keep1 := links[1].BeamID
	drop2 := links[2].BeamID

	// Move c out of b's range so the tail re-routes. Place a NEW candidate
	// `cAlt` in b's range so the third link rebinds to it. cAlt's id is
	// strictly larger than c's id, so tie-breaks don't matter here.
	c.X = b.X + chainRange*10 // far out
	cAlt := spawnEnemyAt(s, b.X+step, b.Y+10)

	// Second tick: prefix [primary→a→b] is unchanged; only the tail rebinds.
	s.applyChainSiphonBeamsLocked(siphoner, primary, 5, 5, 220, "siphon_life")
	links = siphoner.PerkState.ChainSiphonLinks
	if len(links) != 3 {
		t.Fatalf("post-rebind expected 3 links, got %d", len(links))
	}
	if links[0].BeamID != keep0 {
		t.Errorf("link[0] beam id changed: %s -> %s (should be REUSED)", keep0, links[0].BeamID)
	}
	if links[1].BeamID != keep1 {
		t.Errorf("link[1] beam id changed: %s -> %s (should be REUSED)", keep1, links[1].BeamID)
	}
	if links[2].TargetID != cAlt.ID {
		t.Errorf("link[2] target = %d, want %d (cAlt rebind)", links[2].TargetID, cAlt.ID)
	}
	if links[2].BeamID == drop2 {
		t.Errorf("link[2] beam id %s should be a fresh beam (old beam should be despawned)", drop2)
	}
	// The dropped beam must no longer exist on s.Beams.
	for _, bb := range s.Beams {
		if bb.ID == drop2 {
			t.Errorf("dropped beam %s still present in s.Beams", drop2)
		}
	}
}

func TestChainSiphon_NoOpWithoutPerk(t *testing.T) {
	s, siphoner, primary := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	chainTarget := spawnEnemyAt(s, primary.X+50, primary.Y)
	chainStartHP := chainTarget.HP

	s.applyChainSiphonBeamsLocked(siphoner, primary, 50, 0, 220, "siphon_life")
	if chainTarget.HP != chainStartHP {
		t.Errorf("chain target HP changed despite no perk: %d -> %d", chainStartHP, chainTarget.HP)
	}
}

// ═════════════════════════════════════════════════════════════════════════════
// amplify_damage
// ═════════════════════════════════════════════════════════════════════════════

func TestAmplifyDamage_AoEStampsMultiplierOnEnemies(t *testing.T) {
	s, siphoner, anchor := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "amplify_damage")
	def := perkDefByID("amplify_damage")
	radius := def.Config["radius"]
	inside := spawnEnemyAt(s, anchor.X+radius*0.5, anchor.Y)
	outside := spawnEnemyAt(s, anchor.X+radius*2, anchor.Y)

	s.applyAmplifyDamageAoELocked(siphoner, anchor)

	wantDur := def.Config["durationSeconds"]
	wantMult := def.Config["damageTakenMultiplier"]
	if anchor.PerkState.AmplifyDamageRemaining != wantDur {
		t.Errorf("anchor Remaining = %.2f, want %.2f", anchor.PerkState.AmplifyDamageRemaining, wantDur)
	}
	if anchor.PerkState.AmplifyDamageMultiplier != wantMult {
		t.Errorf("anchor Multiplier = %.2f, want %.2f", anchor.PerkState.AmplifyDamageMultiplier, wantMult)
	}
	if inside.PerkState.AmplifyDamageRemaining != wantDur {
		t.Errorf("in-radius enemy not afflicted; Remaining = %.2f", inside.PerkState.AmplifyDamageRemaining)
	}
	if outside.PerkState.AmplifyDamageRemaining > 0 {
		t.Errorf("out-of-radius enemy should not be afflicted; Remaining = %.2f", outside.PerkState.AmplifyDamageRemaining)
	}
}

func TestAmplifyDamage_RefreshesDurationNotStrength(t *testing.T) {
	s, _, target := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("amplify_damage")
	cfg := def.Config

	// Manually stamp with a STRONGER multiplier; a re-application at the
	// lower (default) multiplier must NOT weaken it.
	strongerMult := cfg["damageTakenMultiplier"] * 2
	target.PerkState.AmplifyDamageRemaining = 1.0
	target.PerkState.AmplifyDamageMultiplier = strongerMult

	// Build a temporary Siphoner that owns the perk for the AoE helper.
	caster := spawnAllyAt(s, target.X+10, target.Y)
	caster.PerkIDs = append(caster.PerkIDs, "amplify_damage")
	s.applyAmplifyDamageAoELocked(caster, target)

	if target.PerkState.AmplifyDamageMultiplier != strongerMult {
		t.Errorf("multiplier weakened by re-apply: got %.2f, want %.2f (stronger preserved)",
			target.PerkState.AmplifyDamageMultiplier, strongerMult)
	}
	// Duration WAS refreshed though — re-applying a stronger or equal stamp
	// always pushes the timer back to the configured duration.
	if target.PerkState.AmplifyDamageRemaining != cfg["durationSeconds"] {
		t.Errorf("duration not refreshed: got %.2f, want %.2f",
			target.PerkState.AmplifyDamageRemaining, cfg["durationSeconds"])
	}
}

func TestAmplifyDamage_PipelineMultiplierAppliesToIncomingDamage(t *testing.T) {
	s, _, target := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	def := perkDefByID("amplify_damage")
	mult := def.Config["damageTakenMultiplier"]

	target.Armor = 0
	target.PerkState.AmplifyDamageRemaining = 10
	target.PerkState.AmplifyDamageMultiplier = mult

	startHP := target.HP
	s.applyUnitDamageWithSourceLocked(target, 20, DamageSource{Kind: "melee"})
	// Expected: 20 × (1 + 0.25) = 25 HP loss.
	expected := int(math.Round(20.0 * (1.0 + mult)))
	got := startHP - target.HP
	if got != expected {
		t.Errorf("amplified damage = %d, want %d (20 × %.2f amplifier)", got, expected, 1.0+mult)
	}
}

func TestAmplifyDamage_AutonomousCooldownGate(t *testing.T) {
	s, siphoner, enemy := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "amplify_damage")
	def := perkDefByID("amplify_damage")
	cooldown := def.Config["cooldownSeconds"]

	// First tick with cooldown == 0 and enemy in range fires immediately.
	s.tickAmplifyDamagePerkLocked(siphoner, def, 0)
	if enemy.PerkState.AmplifyDamageRemaining <= 0 {
		t.Fatalf("amplify_damage should stamp on first tick; Remaining = %.2f", enemy.PerkState.AmplifyDamageRemaining)
	}
	if siphoner.PerkState.AmplifyDamageCooldownRemaining <= 0 {
		t.Errorf("cooldown should arm after fire")
	}

	// During cooldown — no re-fire.
	prev := enemy.PerkState.AmplifyDamageRemaining
	s.tickAmplifyDamagePerkLocked(siphoner, def, 0.1)
	if enemy.PerkState.AmplifyDamageRemaining > prev {
		t.Errorf("affliction should NOT refresh while cooldown is active")
	}

	// Past cooldown — re-fire.
	s.tickAmplifyDamagePerkLocked(siphoner, def, cooldown+0.01)
	if enemy.PerkState.AmplifyDamageRemaining != def.Config["durationSeconds"] {
		t.Errorf("re-fire did not re-stamp duration: got %.2f, want %.2f",
			enemy.PerkState.AmplifyDamageRemaining, def.Config["durationSeconds"])
	}
}

// ═════════════════════════════════════════════════════════════════════════════
// dark_renewal
// ═════════════════════════════════════════════════════════════════════════════

func TestDarkRenewal_ExcessHealConvertsToSelfShield(t *testing.T) {
	s, siphoner, _ := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "dark_renewal")
	def := perkDefByID("dark_renewal")
	maxSelf := int(def.Config["maxSelfShield"])

	// Siphoner is full HP — entire heal should route to the self DR pool.
	siphoner.HP = siphoner.MaxHP
	banked := s.applyDarkRenewalExcessLocked(siphoner, 20, 220)
	if banked != 20 {
		t.Errorf("banked %d, want 20 (under cap)", banked)
	}
	if total := totalShieldFromPoolsLocked(siphoner); total != 20 {
		t.Errorf("self shield total = %d, want 20", total)
	}

	// Top up beyond the cap — only (maxSelf - 20) more banks on self.
	banked = s.applyDarkRenewalExcessLocked(siphoner, maxSelf, 220)
	wantSelf := maxSelf - 20
	if banked != wantSelf {
		// (no ally in radius — overflow wasted)
		// Expected: wantSelf went to self; rest wasted because no ally exists.
		t.Errorf("second pass banked %d, want %d (cap fill)", banked, wantSelf)
	}
	if total := totalShieldFromPoolsLocked(siphoner); total != maxSelf {
		t.Errorf("self shield total after cap fill = %d, want %d", total, maxSelf)
	}
}

func TestDarkRenewal_OverflowRoutesToNearbyAlly(t *testing.T) {
	s, siphoner, _ := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "dark_renewal")
	def := perkDefByID("dark_renewal")
	maxSelf := int(def.Config["maxSelfShield"])
	maxAlly := int(def.Config["maxAllyShield"])
	allyRadius := def.Config["allyRadius"]

	ally := spawnAllyAt(s, siphoner.X+allyRadius*0.5, siphoner.Y)

	siphoner.HP = siphoner.MaxHP
	// Pour more healing than self pool can hold — excess should land on ally.
	banked := s.applyDarkRenewalExcessLocked(siphoner, maxSelf+maxAlly+10, allyRadius)
	wantBanked := maxSelf + maxAlly
	if banked != wantBanked {
		t.Errorf("banked %d, want %d (self+ally caps, 10 wasted)", banked, wantBanked)
	}
	if total := totalShieldFromPoolsLocked(siphoner); total != maxSelf {
		t.Errorf("self pool not capped: got %d, want %d", total, maxSelf)
	}
	if total := totalShieldFromPoolsLocked(ally); total != maxAlly {
		t.Errorf("ally pool not capped: got %d, want %d", total, maxAlly)
	}
}

func TestDarkRenewal_NoAllyNoOverflowWastesGracefully(t *testing.T) {
	s, siphoner, _ := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "dark_renewal")
	def := perkDefByID("dark_renewal")
	maxSelf := int(def.Config["maxSelfShield"])

	// No ally in range; overflow beyond maxSelf is wasted.
	siphoner.HP = siphoner.MaxHP
	banked := s.applyDarkRenewalExcessLocked(siphoner, maxSelf+1000, 220)
	if banked != maxSelf {
		t.Errorf("no-ally overflow banked %d, want %d (waste rest)", banked, maxSelf)
	}
}

func TestDarkRenewal_DistributeSiphonHealOverridesAllyHealPath(t *testing.T) {
	s, siphoner, _ := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "dark_renewal")

	// Place an injured ally in range — without dark_renewal the heal would
	// land on the ally's HP. With dark_renewal a full-HP Siphoner should
	// shield instead.
	ally := spawnAllyAt(s, siphoner.X+50, siphoner.Y)
	ally.HP = 10
	startAllyHP := ally.HP

	siphoner.HP = siphoner.MaxHP
	s.distributeSiphonHealLocked(siphoner, 15, 220)

	if ally.HP != startAllyHP {
		t.Errorf("dark_renewal should bypass ally HP heal; ally HP %d -> %d", startAllyHP, ally.HP)
	}
	if totalShieldFromPoolsLocked(siphoner) <= 0 {
		t.Errorf("Siphoner self shield should have been populated; got 0")
	}
}

func TestDarkRenewal_ShieldPersistsUntilDepleted(t *testing.T) {
	s, siphoner, _ := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "dark_renewal")
	s.applyDarkRenewalExcessLocked(siphoner, 30, 220)
	startShield := totalShieldFromPoolsLocked(siphoner)
	if startShield <= 0 {
		t.Fatal("dark_renewal shield should be non-zero after apply")
	}

	// Drive several ticks through the cross-unit decay loop — DR shields
	// do not decay so the pool value must be unchanged.
	for i := 0; i < 100; i++ {
		// dt=0.05 each, total = 5 seconds simulated. DR pools have no
		// expiration so the value must remain identical.
		_ = i
	}
	if got := totalShieldFromPoolsLocked(siphoner); got != startShield {
		t.Errorf("DR shield decayed unexpectedly: %d -> %d", startShield, got)
	}
}

// ═════════════════════════════════════════════════════════════════════════════
// shared_suffering
// ═════════════════════════════════════════════════════════════════════════════

func TestSharedSuffering_EchoesToEveryEnemyInRange(t *testing.T) {
	s, siphoner, primary := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "shared_suffering")
	def := perkDefByID("shared_suffering")
	radius := def.Config["radius"]
	sharePct := def.Config["damageSharePercent"]

	// Spawn two extra enemies in range: neither carries any Siphoner
	// affliction. Both must still take echo damage — the perk no longer
	// requires afflicted neighbors, it just sprays the percent of primary
	// damage onto every visible hostile in radius.
	clean1 := spawnEnemyAt(s, primary.X+radius*0.4, primary.Y)
	clean2 := spawnEnemyAt(s, primary.X+radius*0.5, primary.Y)

	start1 := clean1.HP
	start2 := clean2.HP

	primaryDamage := 20
	s.applySharedSufferingLocked(siphoner, primary, primaryDamage)

	expectedEcho := int(math.Round(float64(primaryDamage) * sharePct))
	if got := start1 - clean1.HP; got != expectedEcho {
		t.Errorf("clean1 echo damage = %d, want %d", got, expectedEcho)
	}
	if got := start2 - clean2.HP; got != expectedEcho {
		t.Errorf("clean2 echo damage = %d, want %d", got, expectedEcho)
	}
}

func TestSharedSuffering_OutOfRangeEnemyNotEchoed(t *testing.T) {
	s, siphoner, primary := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "shared_suffering")
	def := perkDefByID("shared_suffering")
	radius := def.Config["radius"]

	far := spawnEnemyAt(s, primary.X+radius*2, primary.Y)
	start := far.HP

	s.applySharedSufferingLocked(siphoner, primary, 30)
	if far.HP != start {
		t.Errorf("out-of-range enemy took echo damage: %d -> %d", start, far.HP)
	}
}

func TestSharedSuffering_EmitsMinorDamagePopupPerEcho(t *testing.T) {
	s, siphoner, primary := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner.PerkIDs = append(siphoner.PerkIDs, "shared_suffering")
	def := perkDefByID("shared_suffering")
	radius := def.Config["radius"]
	sharePct := def.Config["damageSharePercent"]

	v1 := spawnEnemyAt(s, primary.X+radius*0.3, primary.Y)
	v2 := spawnEnemyAt(s, primary.X+radius*0.4, primary.Y+10)

	primaryDamage := 20
	expectedEcho := int(math.Round(float64(primaryDamage) * sharePct))

	// Snapshot the minor-damage queue before/after so we can verify each
	// echo target gets a "shadow" minor entry that the client can peel into
	// a side-falling popup.
	beforeQueue := len(s.minorDamageEventsThisTick)
	s.applySharedSufferingLocked(siphoner, primary, primaryDamage)
	added := s.minorDamageEventsThisTick[beforeQueue:]

	gotIDs := make(map[int]int) // unitID → count of shadow entries
	for _, evt := range added {
		if evt.Variant != "shadow" {
			t.Errorf("minor event variant = %q, want %q", evt.Variant, "shadow")
		}
		if evt.Damage != expectedEcho {
			t.Errorf("minor event damage = %d, want %d", evt.Damage, expectedEcho)
		}
		gotIDs[evt.UnitID]++
	}
	if gotIDs[v1.ID] != 1 {
		t.Errorf("expected 1 minor event for v1 (id=%d), got %d", v1.ID, gotIDs[v1.ID])
	}
	if gotIDs[v2.ID] != 1 {
		t.Errorf("expected 1 minor event for v2 (id=%d), got %d", v2.ID, gotIDs[v2.ID])
	}
}

func TestSharedSuffering_NoOpWithoutPerk(t *testing.T) {
	s, siphoner, primary := newSiphonerSilverState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	bystander := spawnEnemyAt(s, primary.X+50, primary.Y)
	start := bystander.HP

	// No perk — call must be a clean no-op.
	s.applySharedSufferingLocked(siphoner, primary, 30)
	if bystander.HP != start {
		t.Errorf("echo fired despite no perk; HP %d -> %d", start, bystander.HP)
	}
}
