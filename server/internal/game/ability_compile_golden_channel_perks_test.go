package game

import (
	"math"
	"testing"
)

// ═════════════════════════════════════════════════════════════════════════════
// Golden equivalence for siphon_life's channel tick WITH Siphoner perks
// carried by the caster.
//
// TestAbilityCompileGolden_SiphonLife (ability_compile_golden_channel_test.go)
// already proves per-tick parity between the legacy-fixture channel path and
// the shipped schemaVersion-2 executor path for a PLAIN (no-perk) caster.
// That test cannot exercise the fold path the migration actually changed:
// the channel loop now sets ctx.damageEffectivenessMultiplier = mods.DamageMult
// and reads the applied amount back from the AUTHORED on_beam_tick/deal_damage
// action as tickDamage — for a plain caster mods.DamageMult is always the
// identity 1.0, so a double-fold or missing-fold bug in that plumbing would
// be invisible there.
//
// This file extends the same legacy-fixture-vs-executor comparison, run once
// per Siphoner perk loadout, with a caster that actually owns the perk(s) on
// BOTH legs. Two of the seven perks (soul_leech, beam_mastery) scale
// mods.DamageMult directly — those are the ones that can prove or disprove
// the fold. The other five read the post-fold tickDamage the channel loop
// hands them, so a parity break in their own math would still show up as a
// tick-by-tick divergence between legs even though DamageMult itself stays
// at 1.0 for them.
//
// Per-tick sample scope: instead of just the primary target/caster pair (the
// parent test's scope), each sample aggregates across EVERY unit in the
// scene — total damage dealt, total healing/shielding banked, caster mana
// delta, the target's Withering Beam stack count, and the live beam count.
// That is what makes a chain_siphon secondary beam, a shared_suffering echo,
// a dark_renewal shield, or a repurposed_life mana pulse show up as a
// same-tick comparison failure the instant either leg diverges from the
// other — not just an eventual different final total.
// ═════════════════════════════════════════════════════════════════════════════

// siphonWorldSample is one Update() tick's worth of scene-wide observables.
// Deliberately all-comparable-by-== (no maps/slices) so a whole run's sample
// sequence can be compared element-by-element with a plain != check.
type siphonWorldSample struct {
	totalDamageDealt   int // Σ HP lost across every unit in the scene this tick
	totalHealApplied   int // Σ HP gained across every unit this tick
	totalShieldBanked  int // Σ shield-pool value delta across every unit this tick
	casterManaDelta    int // caster mana BEFORE minus AFTER (negative = net gain, e.g. repurposed_life's kill-tick restore outrunning the tick's own mana cost)
	targetWitherStacks int // target.PerkState.WitheringBeamStacks, post-tick (absolute, not delta)
	beamCount          int // len(s.Beams), post-tick — chain_siphon adds secondary beams alongside the primary
}

// snapshotSiphonWorld captures every unit's current HP and total banked
// shield value, keyed by unit ID. Used to compute a siphonWorldSample's
// scene-wide deltas across a single Update() call. Not itself "Locked" (no
// internal locking) — matches the parent golden test's convention of reading
// GameState fields directly between Update() calls in a single-goroutine
// test, never concurrently with the tick loop.
func snapshotSiphonWorld(s *GameState) (hp, shield map[int]int) {
	hp = make(map[int]int, len(s.Units))
	shield = make(map[int]int, len(s.Units))
	for _, u := range s.Units {
		if u == nil {
			continue
		}
		hp[u.ID] = u.HP
		shield[u.ID] = totalShieldFromPoolsLocked(u)
	}
	return hp, shield
}

// driveSiphonWorldSamples starts abilityID's channel on caster→target through
// the real production entry point (beginAbilityCastLocked, identical to the
// parent golden test) and drives `ticks` Update() calls at dt==tickInterval —
// exactly one channel tick fires per Update call (see channelMaxTicksPerUpdate
// and the parent test's comment on why dt==interval matters: it is what keeps
// a single before/after snapshot standing in for "this tick's" effect, and it
// is what avoids the known-and-accepted multi-tick-per-Update corpse-hit edge
// case called out in this task's brief). Returns one siphonWorldSample per
// Update() call.
//
// s must be passed with s.mu HELD (matching buildGoldenChannelScene's
// contract); this function unlocks it before ticking and leaves it unlocked
// on return.
func driveSiphonWorldSamples(t *testing.T, s *GameState, caster, target *Unit, abilityID string, interval float64, ticks int) []siphonWorldSample {
	t.Helper()
	ok, reason := s.beginAbilityCastLocked(caster, abilityID, target)
	s.mu.Unlock()
	if !ok {
		t.Fatalf("%s: beginAbilityCastLocked failed to start channel: %q", abilityID, reason)
	}

	samples := make([]siphonWorldSample, 0, ticks)
	for i := 0; i < ticks; i++ {
		beforeHP, beforeShield := snapshotSiphonWorld(s)
		beforeMana := caster.CurrentMana

		s.Update(interval)

		afterHP, afterShield := snapshotSiphonWorld(s)
		sample := siphonWorldSample{
			casterManaDelta:    beforeMana - caster.CurrentMana,
			targetWitherStacks: target.PerkState.WitheringBeamStacks,
			beamCount:          len(s.Beams),
		}
		for id, hpBefore := range beforeHP {
			hpAfter, ok := afterHP[id]
			if !ok {
				// Unit removed this tick — it died at HP<=0 during the tick
				// (drainPendingDeathsLocked runs inside Update() before this
				// snapshot); treat its post-tick HP as 0.
				hpAfter = 0
			}
			delta := hpAfter - hpBefore
			switch {
			case delta < 0:
				sample.totalDamageDealt += -delta
			case delta > 0:
				sample.totalHealApplied += delta
			}
		}
		for id, shBefore := range beforeShield {
			shAfter := afterShield[id] // missing (unit died) => 0; its pool died with it, which is correct
			sample.totalShieldBanked += shAfter - shBefore
		}
		samples = append(samples, sample)
	}
	return samples
}

// neuterIncidentalCombat zeroes a spawned unit's Damage and MoveSpeed so it
// can neither engage in nor be provoked into basic-attack combat while the
// tick loop runs. This test (unlike the existing per-mechanic Siphoner perk
// unit tests, which call the perk helpers directly and never touch
// GameState.Update) drives the REAL combat AI every tick, so an unrelated
// spawned enemy/ally sitting within another unit's AttackRange would
// otherwise trade blows autonomously — same discipline buildGoldenChannelScene
// already applies to the caster/target pair ("both with Damage=0 so no
// incidental basic-attack combat perturbs the scene").
func neuterIncidentalCombat(u *Unit) {
	if u == nil {
		return
	}
	u.Damage = 0
	u.MoveSpeed = 0
}

// swapRuntimeAbilityForTest temporarily overrides the runtime ability
// registry entry for def.ID — the same mechanism registerRuntimeTestAbility
// uses, but WITHOUT deferring the restore to t.Cleanup, so the caller can put
// the real catalog def back at a precise point MID-test rather than only at
// the very end of the (sub)test. Used by the repurposed_life case below to
// run the LEGACY leg under the real "siphon_life" id (see that case's doc
// comment for why) and then restore the real catalog def before the executor
// leg is built.
//
// A t.Cleanup safety net is still registered, so a Fatalf/panic partway
// through the legacy leg can never leave "siphon_life" shadowed for any
// later test in the package. Calling the returned restore func more than
// once (e.g. once explicitly, once via the safety net) is a safe no-op.
func swapRuntimeAbilityForTest(t *testing.T, def AbilityDef) func() {
	t.Helper()
	runtimeAbilitiesMu.Lock()
	prev, hadPrev := runtimeAbilities[def.ID]
	runtimeAbilities[def.ID] = def
	runtimeAbilitiesMu.Unlock()

	var restored bool
	restore := func() {
		if restored {
			return
		}
		restored = true
		runtimeAbilitiesMu.Lock()
		if hadPrev {
			runtimeAbilities[def.ID] = prev
		} else {
			delete(runtimeAbilities, def.ID)
		}
		runtimeAbilitiesMu.Unlock()
	}
	t.Cleanup(restore)
	return restore
}

// siphonerPerkGoldenCase describes one perk loadout to drive through the
// legacy-vs-executor siphon_life channel comparison.
type siphonerPerkGoldenCase struct {
	name      string
	perkIDs   []string
	rank      string
	startMana int
	ticks     int

	// legacyUsesRealID routes the legacy leg through swapRuntimeAbilityForTest
	// instead of the usual scratch id (see runSiphonerPerkGoldenCase). Needed
	// ONLY for perk hooks that gate on the literal ability-id string
	// "siphon_life" rather than on perk ownership + channel shape — today
	// that is exactly repurposed_life's clearChannelStateLocked hook. Every
	// other case leaves this false and uses the standard scratch-id
	// technique (registerRuntimeTestAbility, shared with the parent golden
	// test), which is safe because none of those perks' hooks read the
	// ability id.
	legacyUsesRealID bool

	// setup adds any extra units/geometry the perk needs to actually fire
	// (a chain_siphon bounce target, shared_suffering neighbors, an ally for
	// repurposed_life/dark_renewal to affect). Called once per leg with that
	// leg's OWN caster/target, while s.mu is held. Returns the unit whose
	// FINAL state (post-run) proves the perk's signature effect actually
	// happened — nil when the caster/target pair itself already carries
	// that signal.
	setup func(t *testing.T, s *GameState, caster, target *Unit) (tracked *Unit)

	// afterScene overrides caster/target stats identically on both legs,
	// AFTER buildGoldenChannelScene's own defaults (half-HP caster, 500-HP
	// target) have been applied. Receives legacyDef so HP thresholds can be
	// derived from the ability's own DamagePerTick rather than hardcoded.
	afterScene func(legacyDef AbilityDef, caster, target *Unit)

	// checkFold additionally asserts the FIRST tick's total scene damage
	// equals round(DamagePerTick * mods.DamageMult), where mods is computed
	// from the LEGACY leg's own siphonLifeChannelModifiersForCasterLocked
	// BEFORE any tick runs — i.e. derived from the same leg being asserted
	// against, never a hardcoded literal. Both legs must land on that exact
	// number. Skipped for chain_siphon/shared_suffering, whose setup adds
	// extra enemies that also take damage on tick 0 (breaking the
	// "tick-0 total == primary-only damage" assumption this check relies on
	// — their fold correctness is still fully covered by the tick-by-tick
	// sequence-equality assertion below, just not by this extra derived
	// check).
	checkFold bool

	// verify runs once after the full parity comparison, with s.mu held on
	// the LEGACY leg, and must observe the perk's signature effect having
	// actually fired — guarding against a vacuous pass where both legs
	// simply did nothing.
	verify func(t *testing.T, caster, target, tracked *Unit, trackedStartHP, trackedStartMana int)
}

func TestAbilityCompileGolden_SiphonLife_Perks(t *testing.T) {
	legacyDef := legacySiphonLifeFixture()
	legacyDef.ID = "siphon_life_legacy_golden_perks_test"
	registerRuntimeTestAbility(t, legacyDef)
	catalogDef := requireMigratedV2(t, "siphon_life")
	if catalogDef.CastRange != legacyDef.CastRange {
		t.Fatalf("fixture drifted from catalog: CastRange legacy=%v catalog=%v", legacyDef.CastRange, catalogDef.CastRange)
	}

	interval := legacyDef.TickIntervalSeconds
	const startMana = 5000 // generous — these cases test perk fold parity, not mana exhaustion (already covered by the parent golden test)
	const ticks = 20

	cases := []siphonerPerkGoldenCase{
		// ── soul_leech (Bronze) — THE key fold test: scales DamageMult/HealMult. ──
		{
			name:      "soul_leech",
			perkIDs:   []string{"soul_leech"},
			rank:      unitRankBronze,
			startMana: startMana,
			ticks:     ticks,
			checkFold: true,
			verify: func(t *testing.T, caster, target, tracked *Unit, trackedStartHP, trackedStartMana int) {
				def := perkDefByID("soul_leech")
				if def == nil {
					t.Fatal("soul_leech perk def missing")
				}
				if def.Config["damageMultiplier"] == 1.0 {
					t.Fatal("soul_leech damageMultiplier config drifted to a no-op 1.0 — this case can't prove anything about the fold")
				}
			},
		},
		// ── beam_mastery (Gold) — scales DamageMult/HealMult/ManaCostMult/RangeMult. ──
		{
			name:      "beam_mastery",
			perkIDs:   []string{"beam_mastery"},
			rank:      unitRankGold,
			startMana: startMana,
			ticks:     ticks,
			checkFold: true,
			verify: func(t *testing.T, caster, target, tracked *Unit, trackedStartHP, trackedStartMana int) {
				def := perkDefByID("beam_mastery")
				if def == nil {
					t.Fatal("beam_mastery perk def missing")
				}
				if def.Config["damageMultiplier"] == 1.0 && def.Config["manaCostMultiplier"] == 1.0 {
					t.Fatal("beam_mastery damage/mana multiplier config drifted to no-ops — this case can't prove anything")
				}
			},
		},
		// ── soul_leech + beam_mastery combined — both scale DamageMult; proves
		// the two compose (multiplicatively) through the fold exactly once,
		// not per-perk. ──
		{
			name:      "soul_leech+beam_mastery",
			perkIDs:   []string{"soul_leech", "beam_mastery"},
			rank:      unitRankGold,
			startMana: startMana,
			ticks:     ticks,
			checkFold: true,
			verify: func(t *testing.T, caster, target, tracked *Unit, trackedStartHP, trackedStartMana int) {
				// Nothing extra beyond the fold check itself — covered by checkFold.
			},
		},
		// ── chain_siphon (Silver) — secondary beams off the primary target,
		// damage scaled off the primary tickDamage. ──
		{
			name:      "chain_siphon",
			perkIDs:   []string{"chain_siphon"},
			rank:      unitRankSilver,
			startMana: startMana,
			ticks:     ticks,
			setup: func(t *testing.T, s *GameState, caster, target *Unit) *Unit {
				chainRange := perkDefByID("chain_siphon").Config["chainRange"]
				tracked := spawnEnemyAt(s, target.X+chainRange*0.5, target.Y)
				neuterIncidentalCombat(tracked)
				return tracked
			},
			verify: func(t *testing.T, caster, target, tracked *Unit, trackedStartHP, trackedStartMana int) {
				if tracked.HP >= trackedStartHP {
					t.Errorf("chain_siphon: chain target took no damage over the run (HP %d -> %d)", trackedStartHP, tracked.HP)
				}
			},
		},
		// ── shared_suffering (Silver) — echoes a fraction of tickDamage to
		// nearby enemies. ──
		{
			name:      "shared_suffering",
			perkIDs:   []string{"shared_suffering"},
			rank:      unitRankSilver,
			startMana: startMana,
			ticks:     ticks,
			setup: func(t *testing.T, s *GameState, caster, target *Unit) *Unit {
				radius := perkDefByID("shared_suffering").Config["radius"]
				neighbor := spawnEnemyAt(s, target.X+radius*0.5, target.Y+10) // second neighbor, not tracked — just extra echo-recipient coverage
				neuterIncidentalCombat(neighbor)
				tracked := spawnEnemyAt(s, target.X+radius*0.4, target.Y)
				neuterIncidentalCombat(tracked)
				return tracked
			},
			verify: func(t *testing.T, caster, target, tracked *Unit, trackedStartHP, trackedStartMana int) {
				if tracked.HP >= trackedStartHP {
					t.Errorf("shared_suffering: echo neighbor took no damage over the run (HP %d -> %d)", trackedStartHP, tracked.HP)
				}
			},
		},
		// ── withering_beam (Bronze) — stamps stacks on the target over
		// continuous channel contact. ──
		{
			name:      "withering_beam",
			perkIDs:   []string{"withering_beam"},
			rank:      unitRankBronze,
			startMana: startMana,
			ticks:     ticks,
			checkFold: true, // DamageMult stays at the identity 1.0 for this perk; still a valid (if less interesting) derived check
			verify: func(t *testing.T, caster, target, tracked *Unit, trackedStartHP, trackedStartMana int) {
				if target.PerkState.WitheringBeamStacks <= 0 {
					t.Errorf("withering_beam: target has 0 stacks after %d ticks of continuous contact", ticks)
				}
			},
		},
		// ── repurposed_life (Gold) — restores mana to nearby allies (incl.
		// the Siphoner) when the channel target dies to this Siphoner's own
		// tick. Target HP is set to exactly one tick's damage so the kill
		// (and the resulting mana pulse) lands deterministically on tick 1,
		// derived from legacyDef.DamagePerTick rather than hardcoded. ──
		//
		// legacyUsesRealID: true — see swapRuntimeAbilityForTest and its use
		// in runSiphonerPerkGoldenCase below. repurposed_life's mana-restore
		// fires from clearChannelStateLocked (ability_channel.go), gated on
		// the LITERAL string `unit.ChannelAbilityID == "siphon_life"` — code
		// this migration never touched (the migration only changed the
		// per-tick DAMAGE computation). Run under the usual scratch id, that
		// literal-string gate simply can't match on the legacy leg, which
		// would produce a "divergence" that is actually just a harness
		// artifact (comparing two legs that intentionally carry different
		// ability ids), not a migration regression. Routing the legacy leg
		// through the real "siphon_life" id here (matching what the
		// executor leg already uses) makes the comparison faithful: both
		// legs see identical ChannelAbilityID gating, and the restore is
		// proven to fire identically on both — which it does.
		//
		// PRE-EXISTING CODE SMELL (not introduced or fixed by this
		// migration — flagged here as a known follow-up): THREE call sites
		// identify "is this a Siphon Life channel" by an exact ability-id
		// string comparison instead of a semantic property (perk ownership +
		// channel shape): clearChannelStateLocked (ability_channel.go:626,
		// exercised by this case), the identical pattern in
		// onSiphonVictimDeathLocked (perks_siphoner.go:1528, the
		// "ally-lands-the-killing-blow" path — not exercised by this specific
		// scenario, which goes through the caster's-own-tick-kill path
		// instead), and channelRangeMultiplierForCasterLocked
		// (ability_channel.go:567, beam_mastery's RangeMult scaling — not
		// exercised by the beam_mastery case above, which never tests a
		// range-boundary scenario). All three are invisible in live gameplay
		// today (there is exactly one shipped ability with this shape) but
		// would silently break if siphon_life were ever renamed/cloned, or —
		// as demonstrated here — under this exact golden-test isolation
		// technique.
		{
			name:             "repurposed_life",
			perkIDs:          []string{"repurposed_life"},
			rank:             unitRankGold,
			startMana:        startMana,
			ticks:            ticks,
			legacyUsesRealID: true,
			afterScene: func(legacyDef AbilityDef, caster, target *Unit) {
				target.HP = legacyDef.DamagePerTick // dies on tick 1 (DamageMult is the identity 1.0 for this perk)
				target.MaxHP = target.HP
			},
			setup: func(t *testing.T, s *GameState, caster, target *Unit) *Unit {
				radius := perkDefByID("repurposed_life").Config["radius"]
				ally := spawnAllyAt(s, caster.X+radius*0.3, caster.Y)
				ally.MaxMana = 1000
				ally.CurrentMana = 0
				neuterIncidentalCombat(ally)
				return ally
			},
			checkFold: true,
			verify: func(t *testing.T, caster, target, tracked *Unit, trackedStartHP, trackedStartMana int) {
				def := perkDefByID("repurposed_life")
				want := int(def.Config["manaRestoreAmount"])
				if got := tracked.CurrentMana - trackedStartMana; got != want {
					t.Errorf("repurposed_life: ally mana restored = %d, want exactly %d (manaRestoreAmount, one pulse on the kill tick)", got, want)
				}
				if target.HP > 0 {
					t.Errorf("repurposed_life: target should have died on tick 1, still has %d HP", target.HP)
				}
			},
		},
		// ── dark_renewal (Silver) — routes heal overflow into a shield
		// cascade. Caster is forced to full HP (overriding the scene's
		// default half-HP self-heal setup) so every tick's heal is pure
		// overflow. ──
		{
			name:      "dark_renewal",
			perkIDs:   []string{"dark_renewal"},
			rank:      unitRankSilver,
			startMana: startMana,
			ticks:     ticks,
			afterScene: func(legacyDef AbilityDef, caster, target *Unit) {
				caster.HP = caster.MaxHP // full HP: distributeSiphonHealLocked's self-heal path is a no-op, forcing 100% overflow into the dark_renewal cascade
			},
			setup: func(t *testing.T, s *GameState, caster, target *Unit) *Unit {
				allyRadius := perkDefByID("dark_renewal").Config["allyRadius"]
				ally := spawnAllyAt(s, caster.X+allyRadius*0.3, caster.Y)
				neuterIncidentalCombat(ally)
				return ally
			},
			checkFold: true,
			verify: func(t *testing.T, caster, target, tracked *Unit, trackedStartHP, trackedStartMana int) {
				total := totalShieldFromPoolsLocked(caster) + totalShieldFromPoolsLocked(tracked)
				if total <= 0 {
					t.Errorf("dark_renewal: no shield banked anywhere (self=%d ally=%d) after %d ticks of overflow heal",
						totalShieldFromPoolsLocked(caster), totalShieldFromPoolsLocked(tracked), ticks)
				}
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			runSiphonerPerkGoldenCase(t, legacyDef, tc, interval)
		})
	}
}

// runSiphonerPerkGoldenCase builds a legacy leg and an executor leg for the
// same perk loadout, drives both through the identical channel-tick
// sequence, and asserts the per-tick siphonWorldSample sequences are
// IDENTICAL — proving the authored on_beam_tick/deal_damage path folds
// mods.DamageMult (and every downstream perk hook that reads the resulting
// tickDamage) exactly the same way the legacy inline computation does, for
// this specific perk loadout.
func runSiphonerPerkGoldenCase(t *testing.T, legacyDef AbilityDef, tc siphonerPerkGoldenCase, interval float64) {
	t.Helper()

	// legacyAbilityID is normally legacyDef's own scratch id (registered once
	// for the whole test, shared with every OTHER case here — safe because
	// none of those perks' hooks read the ability id). tc.legacyUsesRealID
	// swaps the frozen legacy fixture into the REAL "siphon_life" registry
	// slot for the duration of the legacy leg only, restoring it before the
	// executor leg is built — see swapRuntimeAbilityForTest and the
	// repurposed_life case's doc comment for why this is necessary for that
	// one perk.
	legacyAbilityID := legacyDef.ID
	var restoreRuntimeAbility func()
	if tc.legacyUsesRealID {
		realIDLegacyDef := legacyDef
		realIDLegacyDef.ID = "siphon_life"
		restoreRuntimeAbility = swapRuntimeAbilityForTest(t, realIDLegacyDef)
		legacyAbilityID = "siphon_life"
	}

	// ── Legacy leg ──────────────────────────────────────────────────────────
	sLegacy, casterL, targetL := buildGoldenChannelScene(t, legacyAbilityID, tc.startMana)
	casterL.Rank = tc.rank
	casterL.PerkIDs = append(casterL.PerkIDs, tc.perkIDs...)
	if tc.afterScene != nil {
		tc.afterScene(legacyDef, casterL, targetL)
	}
	var trackedL *Unit
	if tc.setup != nil {
		trackedL = tc.setup(t, sLegacy, casterL, targetL)
	}
	trackedStartHPL, trackedStartManaL := 0, 0
	if trackedL != nil {
		trackedStartHPL, trackedStartManaL = trackedL.HP, trackedL.CurrentMana
	}

	// Fold-once evidence: derive the expected FIRST-tick primary damage from
	// the legacy leg's OWN modifier aggregation, before any tick has run.
	// Never a hardcoded literal — read straight off this leg's perk config.
	var wantFirstTickDamage int
	if tc.checkFold {
		// sLegacy.mu is already held here (buildGoldenChannelScene returns
		// with the lock held, and tc.afterScene/tc.setup ran above without
		// releasing it) — do NOT re-lock; sync.Mutex is not reentrant and
		// doing so deadlocks the test.
		mods := sLegacy.siphonLifeChannelModifiersForCasterLocked(casterL)
		wantFirstTickDamage = int(math.Round(float64(legacyDef.DamagePerTick) * mods.DamageMult))
		if wantFirstTickDamage <= 0 {
			t.Fatalf("%s: fold-once setup produced a non-positive expected first-tick damage (%d)", tc.name, wantFirstTickDamage)
		}
	}

	legacySamples := driveSiphonWorldSamples(t, sLegacy, casterL, targetL, legacyAbilityID, interval, tc.ticks)

	// Restore the real "siphon_life" catalog def BEFORE building the executor
	// leg — it must never see the swapped-in legacy fixture. No-op when
	// tc.legacyUsesRealID is false (restoreRuntimeAbility is nil then).
	if restoreRuntimeAbility != nil {
		restoreRuntimeAbility()
	}

	// ── Executor leg — identical setup sequence against the real catalog id. ──
	sExec, casterE, targetE := buildGoldenChannelScene(t, "siphon_life", tc.startMana)
	casterE.Rank = tc.rank
	casterE.PerkIDs = append(casterE.PerkIDs, tc.perkIDs...)
	if tc.afterScene != nil {
		tc.afterScene(legacyDef, casterE, targetE)
	}
	var trackedE *Unit
	if tc.setup != nil {
		trackedE = tc.setup(t, sExec, casterE, targetE)
	}
	trackedStartHPE, trackedStartManaE := 0, 0
	if trackedE != nil {
		trackedStartHPE, trackedStartManaE = trackedE.HP, trackedE.CurrentMana
	}

	execSamples := driveSiphonWorldSamples(t, sExec, casterE, targetE, "siphon_life", interval, tc.ticks)

	// ── Tick-by-tick parity: no aggregate can hide a divergence on any one
	// tick — this is the actual golden-equivalence proof for this loadout. ──
	if len(legacySamples) != len(execSamples) {
		t.Fatalf("%s: tick-count mismatch: legacy=%d exec=%d", tc.name, len(legacySamples), len(execSamples))
	}
	for i := range legacySamples {
		if legacySamples[i] != execSamples[i] {
			t.Errorf("%s: tick %d mismatch: legacy=%+v exec=%+v", tc.name, i, legacySamples[i], execSamples[i])
		}
	}

	if tc.checkFold {
		if legacySamples[0].totalDamageDealt != wantFirstTickDamage {
			t.Fatalf("%s: fold-once check: legacy leg's own tick-0 damage = %d, want %d (= round(DamagePerTick=%d * DamageMult), computed from this SAME leg before any tick ran)",
				tc.name, legacySamples[0].totalDamageDealt, wantFirstTickDamage, legacyDef.DamagePerTick)
		}
		if execSamples[0].totalDamageDealt != wantFirstTickDamage {
			t.Errorf("%s: fold-once check: executor leg's tick-0 damage = %d, want %d (must match the legacy leg's own round(DamagePerTick*DamageMult) — a mismatch means the authored deal_damage path folded ctx.damageEffectivenessMultiplier zero or two times)",
				tc.name, execSamples[0].totalDamageDealt, wantFirstTickDamage)
		}
	}

	// ── Non-vacuous sanity: the perk's signature effect actually happened,
	// on both legs, using each leg's OWN tracked unit / final state. ──
	if tc.verify != nil {
		tc.verify(t, casterL, targetL, trackedL, trackedStartHPL, trackedStartManaL)
		tc.verify(t, casterE, targetE, trackedE, trackedStartHPE, trackedStartManaE)
	}

	// ── Full-scene structural equivalence (unit-for-unit), mirroring the
	// parent golden test. Valid here because both legs run through the
	// IDENTICAL deterministic setup sequence (same seed, same spawn-call
	// order), so unit IDs line up between legs. ──
	sLegacy.mu.Lock()
	sExec.mu.Lock()
	assertScenesEquivalent(t, sLegacy, sExec, tc.name)
	sExec.mu.Unlock()
	sLegacy.mu.Unlock()
}
