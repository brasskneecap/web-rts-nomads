package game

// Section: Siphoner Bronze perk unit tests.
//
// Covers all four Bronze perks: soul_leech, withering_beam, lingering_hex,
// mark_of_weakness. Mirrors the shape of cleric_bronze_perks_test.go.
//
// Expected values derive from the perk catalog Config maps — never hardcoded —
// so a tuning change in JSON requires only a baseline update, not test edits.

import (
	"math"
	"testing"

	"webrts/server/pkg/protocol"
)

// newSiphonerBronzeState returns a GameState with:
//   - siphoner: an Acolyte owned by "p1" promoted along the Siphoner path to
//     Bronze. Visible, full HP, large AttackRange so range never gates tests.
//     MaxMana/CurrentMana set generously.
//   - enemy: a hostile wave-enemy soldier at (600,400) for affliction tests.
//
// Lock is NOT held on return.
func newSiphonerBronzeState(t *testing.T) (s *GameState, siphoner, enemy *Unit) {
	t.Helper()
	s = NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 0xA51)
	s.mu.Lock()
	defer s.mu.Unlock()

	siphoner = s.spawnPlayerUnitLocked("acolyte", "p1", "#9b59b6", protocol.Vec2{X: 400, Y: 400})
	siphoner.Visible = true
	siphoner.HP = siphoner.MaxHP
	siphoner.AttackRange = 1000
	siphoner.MaxMana = 200
	siphoner.CurrentMana = 200
	siphoner.ProgressionPath = "siphoner"
	siphoner.Rank = unitRankBronze
	s.assignUnitPathAbilitiesLocked(siphoner)

	// Spawn a hostile target by hand at the requested position — the wave
	// helpers route through spawn points / objective metadata which isn't
	// what we want for a focused affliction test.
	enemy = &Unit{
		ID:       s.nextUnitID,
		OwnerID:  enemyPlayerID,
		UnitType: "soldier",
		Visible:  true,
		X:        600, Y: 400,
		HP: 200, MaxHP: 200, Armor: 10,
		AttackRange: 100,
		AttackSpeed: 1.0,
		Damage:      10,
		MoveSpeed:   50,
		Color:       "#aa0000",
	}
	s.nextUnitID++
	// Register via the canonical helper so s.unitsByID is kept in sync —
	// otherwise getUnitByIDLocked (used by the affliction anchor selector)
	// can't find this unit by id, even though it's in s.Units.
	s.addUnitLocked(enemy)

	return s, siphoner, enemy
}

// withSiphonerPerk attaches a perk to the siphoner. Siphoner Bronze perks
// don't grant player-castable abilities (the affliction perks fire
// autonomously via tickUnitPerkStateLocked), so no Abilities re-derive is
// required — just append the perk id. Caller holds s.mu.
func withSiphonerPerk(_ *GameState, siphoner *Unit, perkID string) {
	siphoner.PerkIDs = append(siphoner.PerkIDs, perkID)
}

// ─────────────────────────────────────────────────────────────────────────────
// soul_leech — damage / heal multipliers in the channel tick
// ─────────────────────────────────────────────────────────────────────────────

func TestSoulLeech_DamageAndHealMultipliers(t *testing.T) {
	s, siphoner, _ := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Baseline: no perk → (1.0, 1.0).
	dMult, hMult := s.siphonLifeModifiersForCasterLocked(siphoner)
	if dMult != 1.0 || hMult != 1.0 {
		t.Fatalf("baseline multipliers expected (1.0,1.0), got (%.3f,%.3f)", dMult, hMult)
	}

	// With soul_leech: multipliers reflect the perk config.
	withSiphonerPerk(s, siphoner, "soul_leech")
	def := perkDefByID("soul_leech")
	if def == nil {
		t.Fatal("soul_leech perk def missing")
	}
	wantD := def.Config["damageMultiplier"]
	wantH := def.Config["healingMultiplier"]
	dMult, hMult = s.siphonLifeModifiersForCasterLocked(siphoner)
	if math.Abs(dMult-wantD) > 1e-9 || math.Abs(hMult-wantH) > 1e-9 {
		t.Errorf("soul_leech multipliers: got (%.3f,%.3f), want (%.3f,%.3f)", dMult, hMult, wantD, wantH)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// withering_beam — continuous-siphon stack accumulator
// ─────────────────────────────────────────────────────────────────────────────

func TestWitheringBeam_StackAccumulatesEverySecond(t *testing.T) {
	s, siphoner, enemy := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	withSiphonerPerk(s, siphoner, "withering_beam")
	def := perkDefByID("withering_beam")
	secondsPerStack := def.Config["secondsPerStack"]
	maxStacks := int(def.Config["maxStacks"])
	reductionPerStack := def.Config["damageReductionPerStack"]

	// Simulate Siphon Life tick cadence: 0.25s per tick. After every full
	// `secondsPerStack` of contact the helper should add one stack until
	// the cap is reached.
	tickDt := 0.25
	totalTime := 0.0
	for totalTime < secondsPerStack*float64(maxStacks)+0.5 { // plenty of headroom
		s.tickWitheringBeamChannelLocked(siphoner, enemy, tickDt)
		totalTime += tickDt
	}
	if got := enemy.PerkState.WitheringBeamStacks; got != maxStacks {
		t.Errorf("stacks: got %d, want max %d", got, maxStacks)
	}
	if got := enemy.PerkState.WitheringBeamReductionPerStack; math.Abs(got-reductionPerStack) > 1e-9 {
		t.Errorf("per-stack reduction: got %.3f, want %.3f", got, reductionPerStack)
	}
	if enemy.PerkState.WitheringBeamRemaining <= 0 {
		t.Errorf("Remaining timer should be armed while channel is in contact")
	}
}

func TestWitheringBeam_TargetSwapResetsAccumulator(t *testing.T) {
	s, siphoner, enemy := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	withSiphonerPerk(s, siphoner, "withering_beam")

	// Half a stack's worth of contact on the first target.
	def := perkDefByID("withering_beam")
	half := def.Config["secondsPerStack"] / 2
	s.tickWitheringBeamChannelLocked(siphoner, enemy, half)
	if siphoner.PerkState.WitheringBeamChannelAccum <= 0 {
		t.Fatal("caster accumulator should be > 0 after partial contact")
	}

	// Swap to a different enemy: accumulator must reset, tracking-target updates.
	enemy2 := &Unit{
		ID:      s.nextUnitID,
		OwnerID: enemyPlayerID,
		Visible: true,
		HP:      100, MaxHP: 100,
		X: 620, Y: 400,
	}
	s.nextUnitID++
	s.addUnitLocked(enemy2)
	s.tickWitheringBeamChannelLocked(siphoner, enemy2, 0)
	if siphoner.PerkState.WitheringBeamChannelTargetID != enemy2.ID {
		t.Errorf("tracking-target should update on swap, got %d want %d",
			siphoner.PerkState.WitheringBeamChannelTargetID, enemy2.ID)
	}
	if siphoner.PerkState.WitheringBeamChannelAccum != 0 {
		t.Errorf("accumulator should reset on target swap, got %f", siphoner.PerkState.WitheringBeamChannelAccum)
	}
}

func TestWitheringBeam_DebuffMultiplierFolds(t *testing.T) {
	s, _, enemy := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// Manually stamp 2 stacks of Withering Beam on the enemy.
	enemy.PerkState.WitheringBeamStacks = 2
	enemy.PerkState.WitheringBeamReductionPerStack = 0.10
	enemy.PerkState.WitheringBeamRemaining = 2.0

	got := s.perkOutgoingDamageDebuffMultiplierLocked(enemy)
	want := 0.20
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("outgoing damage debuff: got %.3f, want %.3f", got, want)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// lingering_hex — AoE stamps move + attack-speed slow
// ─────────────────────────────────────────────────────────────────────────────

func TestLingeringHex_AoEStampsHexOnEnemies(t *testing.T) {
	s, siphoner, anchor := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	withSiphonerPerk(s, siphoner, "lingering_hex")

	// Place a second enemy just inside the radius and a third just outside.
	def := perkDefByID("lingering_hex")
	radius := def.Config["radius"]
	inside := &Unit{
		ID:      s.nextUnitID,
		OwnerID: enemyPlayerID,
		Visible: true,
		HP:      200, MaxHP: 200,
		X: anchor.X + radius - 5, Y: anchor.Y,
	}
	s.nextUnitID++
	outside := &Unit{
		ID:      s.nextUnitID,
		OwnerID: enemyPlayerID,
		Visible: true,
		HP:      200, MaxHP: 200,
		X: anchor.X + radius + 50, Y: anchor.Y,
	}
	s.nextUnitID++
	s.addUnitLocked(inside)
	s.addUnitLocked(outside)

	s.applyLingeringHexAoELocked(siphoner, anchor)

	if anchor.PerkState.LingeringHexRemaining <= 0 {
		t.Errorf("anchor should be hexed")
	}
	if inside.PerkState.LingeringHexRemaining <= 0 {
		t.Errorf("in-radius enemy should be hexed")
	}
	if outside.PerkState.LingeringHexRemaining > 0 {
		t.Errorf("out-of-radius enemy should NOT be hexed")
	}

	// Stat hooks must surface the configured multipliers.
	wantMove := def.Config["moveSpeedMultiplier"]
	wantAtk := def.Config["attackSpeedMultiplier"]
	if got := lingeringHexMoveSpeedFactorLocked(anchor); math.Abs(got-wantMove) > 1e-9 {
		t.Errorf("hex move factor: got %.3f, want %.3f", got, wantMove)
	}
	if got := lingeringHexAttackSpeedFactorLocked(anchor); math.Abs(got-wantAtk) > 1e-9 {
		t.Errorf("hex attack factor: got %.3f, want %.3f", got, wantAtk)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// mark_of_weakness — AoE stamps armor + healing-received debuff
// ─────────────────────────────────────────────────────────────────────────────

func TestMarkOfWeakness_ArmorReductionAndHealingDebuff(t *testing.T) {
	s, siphoner, anchor := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	withSiphonerPerk(s, siphoner, "mark_of_weakness")

	def := perkDefByID("mark_of_weakness")
	armorReduction := int(math.Round(def.Config["armorReduction"]))
	healMult := def.Config["healingReceivedMultiplier"]

	// Snapshot armor BEFORE the mark lands.
	armorBefore := s.effectiveArmorLocked(anchor)

	s.applyMarkOfWeaknessAoELocked(siphoner, anchor)

	if anchor.PerkState.MarkOfWeaknessRemaining <= 0 {
		t.Fatal("anchor should be marked")
	}

	armorAfter := s.effectiveArmorLocked(anchor)
	wantArmorAfter := armorBefore - armorReduction
	if wantArmorAfter < 0 {
		wantArmorAfter = 0
	}
	if armorAfter != wantArmorAfter {
		t.Errorf("effective armor: got %d, want %d (was %d, reduction %d)",
			armorAfter, wantArmorAfter, armorBefore, armorReduction)
	}

	// Healing-received: 100 hp restored to a wounded marked unit becomes
	// round(100 * healMult). The damage path is integer; we drop HP first.
	anchor.HP = 1
	anchor.MaxHP = 1000
	s.healUnitLocked(anchor, 100)
	wantGain := int(math.Round(100 * healMult))
	if got := anchor.HP - 1; got != wantGain {
		t.Errorf("healing landed: got +%d HP, want +%d (mult %.2f)", got, wantGain, healMult)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Autonomous-fire tests — Lingering Hex / Mark of Weakness pulse on a per-
// unit cooldown. They are NOT player-castable abilities; the tick handler
// in tickUnitPerkStateLocked drives them, mirroring the trapper trap-
// placement pattern.
// ─────────────────────────────────────────────────────────────────────────────

func TestLingeringHex_AutonomousFireOnCooldown(t *testing.T) {
	s, siphoner, enemy := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	withSiphonerPerk(s, siphoner, "lingering_hex")
	def := perkDefByID("lingering_hex")
	cooldown := def.Config["cooldownSeconds"]

	// First tick with cooldown==0 and enemy in range should fire immediately.
	s.tickLingeringHexPerkLocked(siphoner, def, 0)
	if enemy.PerkState.LingeringHexRemaining <= 0 {
		t.Fatalf("hex should land on first tick with cooldown==0; got Remaining=%.2f", enemy.PerkState.LingeringHexRemaining)
	}
	if siphoner.PerkState.LingeringHexCooldownRemaining <= 0 {
		t.Errorf("cooldown should arm after fire; got %.2f", siphoner.PerkState.LingeringHexCooldownRemaining)
	}
	manaBefore := siphoner.CurrentMana

	// While cooldown > 0, subsequent ticks must NOT re-fire.
	prevRemaining := enemy.PerkState.LingeringHexRemaining
	s.tickLingeringHexPerkLocked(siphoner, def, 0.1)
	if enemy.PerkState.LingeringHexRemaining > prevRemaining {
		t.Errorf("hex should NOT refresh while cooldown is ticking")
	}
	if siphoner.CurrentMana != manaBefore {
		t.Errorf("mana should not be spent while cooldown is active")
	}

	// Advance time past the cooldown — the next tick should re-fire.
	s.tickLingeringHexPerkLocked(siphoner, def, cooldown+0.01)
	if siphoner.PerkState.LingeringHexCooldownRemaining <= 0 && enemy.PerkState.LingeringHexRemaining < def.Config["durationSeconds"] {
		t.Errorf("re-fire should re-stamp the affliction at full duration; got %.2f want %.2f",
			enemy.PerkState.LingeringHexRemaining, def.Config["durationSeconds"])
	}
}

func TestLingeringHex_HoldsFireWithoutTarget(t *testing.T) {
	s, siphoner, enemy := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	withSiphonerPerk(s, siphoner, "lingering_hex")
	def := perkDefByID("lingering_hex")

	// Park the enemy way out of cast range so no anchor exists.
	enemy.X = siphoner.X + 9999

	s.tickLingeringHexPerkLocked(siphoner, def, 0)
	if siphoner.PerkState.LingeringHexCooldownRemaining > 0 {
		t.Errorf("perk should NOT consume cooldown when no anchor in range")
	}
	if enemy.PerkState.LingeringHexRemaining > 0 {
		t.Errorf("no hex should land while target is out of range")
	}
}

func TestLingeringHex_HoldsFireWithoutMana(t *testing.T) {
	s, siphoner, _ := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	withSiphonerPerk(s, siphoner, "lingering_hex")
	def := perkDefByID("lingering_hex")
	siphoner.CurrentMana = 0

	s.tickLingeringHexPerkLocked(siphoner, def, 0)
	if siphoner.PerkState.LingeringHexCooldownRemaining > 0 {
		t.Errorf("perk should NOT consume cooldown when mana insufficient")
	}
}

func TestMarkOfWeakness_AutonomousFireOnCooldown(t *testing.T) {
	s, siphoner, enemy := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	withSiphonerPerk(s, siphoner, "mark_of_weakness")
	def := perkDefByID("mark_of_weakness")
	cooldown := def.Config["cooldownSeconds"]
	manaCost := int(def.Config["manaCost"])
	manaBefore := siphoner.CurrentMana

	s.tickMarkOfWeaknessPerkLocked(siphoner, def, 0)
	if enemy.PerkState.MarkOfWeaknessRemaining <= 0 {
		t.Fatalf("mark should land on first tick; got Remaining=%.2f", enemy.PerkState.MarkOfWeaknessRemaining)
	}
	if siphoner.CurrentMana != manaBefore-manaCost {
		t.Errorf("mana spent: got %d, want %d", siphoner.CurrentMana, manaBefore-manaCost)
	}
	if siphoner.PerkState.MarkOfWeaknessCooldownRemaining <= 0 {
		t.Errorf("cooldown should arm after fire")
	}

	// During cooldown — no refire.
	prev := enemy.PerkState.MarkOfWeaknessRemaining
	s.tickMarkOfWeaknessPerkLocked(siphoner, def, 0.1)
	if enemy.PerkState.MarkOfWeaknessRemaining > prev {
		t.Errorf("mark should NOT refresh while cooldown is ticking")
	}

	// Past cooldown — refire.
	s.tickMarkOfWeaknessPerkLocked(siphoner, def, cooldown+0.01)
	if enemy.PerkState.MarkOfWeaknessRemaining < def.Config["durationSeconds"] {
		t.Errorf("re-fire should re-stamp at full duration; got %.2f", enemy.PerkState.MarkOfWeaknessRemaining)
	}
}

func TestSiphonerAfflictionAnchor_PrefersChannelTarget(t *testing.T) {
	s, siphoner, enemy := newSiphonerBronzeState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	// A second enemy that is closer than the channel target.
	closer := &Unit{
		ID:      s.nextUnitID,
		OwnerID: enemyPlayerID,
		Visible: true,
		HP:      100, MaxHP: 100,
		X: siphoner.X + 50, Y: siphoner.Y,
	}
	s.nextUnitID++
	s.addUnitLocked(closer)

	// Without a channel: nearest enemy in range wins.
	got := s.siphonerAfflictionAnchorLocked(siphoner, 500)
	if got != closer {
		t.Errorf("no-channel: want nearest hostile, got id=%d", idOrZero(got))
	}

	// Pretend we are siphoning the FAR enemy: that anchor must win.
	siphoner.ChannelTargetID = enemy.ID
	got = s.siphonerAfflictionAnchorLocked(siphoner, 500)
	if got != enemy {
		t.Errorf("channeling: want channel target id=%d, got id=%d", enemy.ID, idOrZero(got))
	}
}

func idOrZero(u *Unit) int {
	if u == nil {
		return 0
	}
	return u.ID
}
