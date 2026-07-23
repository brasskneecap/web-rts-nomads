package game

import (
	"fmt"
	"strings"
	"testing"
)

// requirePerkDef fetches a real catalog perk by id, failing the test if it is
// missing (a renamed/removed fixture perk should fail loudly, not silently
// skip an assertion).
func requirePerkDef(t *testing.T, id string) PerkDef {
	t.Helper()
	def := perkDefByID(id)
	if def == nil {
		t.Fatalf("perk %q not found in catalog", id)
	}
	return *def
}

// statModifierValue returns the Value of def's StatModifier for (stat, op),
// failing the test if there isn't exactly one. Lets characterization tests
// derive expectations from the SAME typed data that drives behavior, instead
// of a parallel config key — the whole point of migrating a perk onto
// StatModifiers is that there is exactly one source of truth for its numbers.
func statModifierValue(t *testing.T, def PerkDef, stat, op string) float64 {
	t.Helper()
	var matches []PerkStatModifier
	for _, m := range def.StatModifiers {
		if m.Stat == stat && m.Op == op {
			matches = append(matches, m)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("perk %q: expected exactly 1 StatModifier{Stat:%q, Op:%q}, found %d (StatModifiers=%+v)",
			def.ID, stat, op, len(matches), def.StatModifiers)
	}
	return matches[0].Value
}

// ─────────────────────────────────────────────────────────────────────────────
// Empty perk → ""
// ─────────────────────────────────────────────────────────────────────────────

func TestDescribePerk_NoDescribableData_ReturnsEmptyString(t *testing.T) {
	def := PerkDef{ID: "no_typed_data", DisplayName: "No Typed Data"}
	if got := describePerk(def); got != "" {
		t.Fatalf("expected empty description, got %q", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// StatModifiers — synthetic fixture (exact-string; literals are fine here,
// this is a made-up perk, not a catalog balance value).
// ─────────────────────────────────────────────────────────────────────────────

func TestDescribePerk_StatModifiers_AddMultiplyAndNonBaseStage(t *testing.T) {
	def := PerkDef{
		ID: "synthetic_stats",
		StatModifiers: []PerkStatModifier{
			{Stat: statMaxHp, Op: statOpAdd, Value: 90}, // stage omitted -> base, no suffix
			{Stat: statDamage, Op: statOpMultiply, Value: 1.15, Stage: statStageIntrinsic},
			{Stat: statArmor, Op: statOpMultiply, Value: 2, Stage: statStageFinal},
		},
	}
	got := describePerk(def)

	// Intrinsic sorts before base sorts before final (statStages order), not
	// authored slice order.
	want := "+15% Damage (before other bonuses), +90 Max Health, +100% Armor (applied last)."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestDescribeStatModifierClause_AddOp_FractionVsNonFraction pins the exact
// rendering split describeStatModifierClause makes for "add" ops:
// isFractionStat true -> signed percentage (critChance IS itself a 0-1
// probability, so +0.1 unambiguously means +10 percentage points); false ->
// signed bare number (attackSpeed's base varies per unit, so a raw +0.3 add
// can't be rendered as a percentage without guessing — this is the exact
// hawk_spirit bug the IsFraction field exists to prevent).
func TestDescribeStatModifierClause_AddOp_FractionVsNonFraction(t *testing.T) {
	cases := []struct {
		name string
		mod  PerkStatModifier
		want string
	}{
		{"fraction stat (critChance) renders as percent", PerkStatModifier{Stat: statCritChance, Op: statOpAdd, Value: 0.1}, "+10% Crit Chance"},
		{"non-fraction stat (attackSpeed) renders as bare number", PerkStatModifier{Stat: statAttackSpeed, Op: statOpAdd, Value: 0.3}, "+0.3 Attack Speed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := describeStatModifierClause(tc.mod, statStageBase); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDescribePerk_HawkSpirit_AttackSpeedNeverRendersAsWrongPercent is the
// direct regression test for the shipped bug: hawk_spirit's old
// tooltipTemplate claimed "+30% attack speed" for a +0.3 add on a
// per-unit-varying base (1.5 for an archer, which is actually +20%). The
// generated text must render the raw add as a bare number instead of
// guessing a percentage. "want"/"mustNotContain" are both derived from the
// catalog's own StatModifiers value, never a hardcoded balance literal.
func TestDescribePerk_HawkSpirit_AttackSpeedNeverRendersAsWrongPercent(t *testing.T) {
	def := requirePerkDef(t, "hawk_spirit")
	got := describePerk(def)

	var asValue float64
	found := false
	for _, m := range def.StatModifiers {
		if m.Stat == statAttackSpeed && m.Op == statOpAdd {
			asValue = m.Value
			found = true
		}
	}
	if !found {
		t.Fatal("hawk_spirit: no attackSpeed add StatModifier in catalog — fixture perk changed shape")
	}
	if isFractionStat(statAttackSpeed) {
		t.Fatal("attackSpeed must not be registered as a fraction stat, or this regression test proves nothing")
	}

	wantClause := signedNumber(asValue) + " Attack Speed"
	if !strings.Contains(got, wantClause) {
		t.Errorf("hawk_spirit: want bare-number attack-speed clause %q in %q", wantClause, got)
	}
	wrongPercentClause := signedPercent(asValue) + " Attack Speed"
	if strings.Contains(got, wrongPercentClause) {
		t.Errorf("hawk_spirit: generated text contains the bogus percentage %q (the shipped bug) in %q", wrongPercentClause, got)
	}
}

func TestDescribePerk_StatModifiers_NegativeValues(t *testing.T) {
	def := PerkDef{
		ID: "synthetic_negative",
		StatModifiers: []PerkStatModifier{
			{Stat: statArmor, Op: statOpAdd, Value: -5},
			{Stat: statMoveSpeed, Op: statOpMultiply, Value: 0.5},
		},
	}
	got := describePerk(def)
	want := "-5 Armor, -50% Move Speed."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// StatModifiers — real catalog perks. Expectations are DERIVED from the
// loaded def's own StatModifiers, never pinned balance literals, so a tuning
// change to these perks can't silently break this test.
// ─────────────────────────────────────────────────────────────────────────────

func TestDescribePerk_StatModifiers_RealCatalogPerks(t *testing.T) {
	for _, id := range []string{"hold_the_line", "hawk_spirit", "vulture_spirit"} {
		t.Run(id, func(t *testing.T) {
			def := requirePerkDef(t, id)
			if len(def.StatModifiers) == 0 {
				t.Fatalf("%s: fixture perk has no StatModifiers — test needs updating", id)
			}
			got := describePerk(def)

			if !strings.HasSuffix(got, ".") {
				t.Errorf("%s: not sentence-terminated: %q", id, got)
			}

			sawIntrinsic, sawFinal := false, false
			for _, m := range def.StatModifiers {
				// The stat's human label must appear.
				if !strings.Contains(got, statLabel(m.Stat)) {
					t.Errorf("%s: missing label %q in %q", id, statLabel(m.Stat), got)
				}
				// The correctly-formatted signed delta must appear, built from
				// the SAME low-level formatting helpers describePerk itself
				// uses (signedNumber/signedPercent) — this pins the derivation
				// (which stat, which op, and IsFraction rendering) not the
				// tuning literal.
				var wantDelta string
				switch {
				case m.Op == statOpMultiply:
					wantDelta = signedPercent(m.Value - 1)
				case isFractionStat(m.Stat):
					wantDelta = signedPercent(m.Value)
				default:
					wantDelta = signedNumber(m.Value)
				}
				if !strings.Contains(got, wantDelta) {
					t.Errorf("%s: missing delta %q for stat %q in %q", id, wantDelta, m.Stat, got)
				}
				switch m.Stage {
				case statStageIntrinsic:
					sawIntrinsic = true
				case statStageFinal:
					sawFinal = true
				}
			}
			if sawIntrinsic && !strings.Contains(got, "(before other bonuses)") {
				t.Errorf("%s: expected intrinsic-stage suffix in %q", id, got)
			}
			if sawFinal && !strings.Contains(got, "(applied last)") {
				t.Errorf("%s: expected final-stage suffix in %q", id, got)
			}
			if !sawFinal && strings.Contains(got, "(applied last)") {
				t.Errorf("%s: unexpected final-stage suffix in %q", id, got)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Auras — real catalog perk (zealous_march).
// ─────────────────────────────────────────────────────────────────────────────

// TestDescribePerk_Auras_Synthetic pins the exact rendering shape (sentence,
// stacking clause) against a made-up fixture — literals are fine here, this
// is not a catalog balance value.
func TestDescribePerk_Auras_Synthetic(t *testing.T) {
	def := PerkDef{
		ID: "synthetic_aura",
		Auras: []PerkAura{{
			Radius:              150,
			Targets:             "allies",
			PerAdditionalSource: 0.05,
			StatModifiers: []PerkStatModifier{
				{Stat: statMoveSpeed, Op: statOpAdd, Value: 0.3},
			},
		}},
	}
	got := describePerk(def)
	want := "Allies within 150 gain +30% Move Speed. Each additional covering source adds +5% Move Speed."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestDescribePerk_Auras_EnemiesTarget_NoPerSourceStacking confirms the
// "enemies" target label and that omitting PerAdditionalSource (zero — the
// default) renders the max-wins stacking rule instead of an "Each
// additional..." per-source clause.
func TestDescribePerk_Auras_EnemiesTarget_NoPerSourceStacking(t *testing.T) {
	def := PerkDef{
		ID: "synthetic_enemy_aura",
		Auras: []PerkAura{{
			Radius:  80,
			Targets: "enemies",
			StatModifiers: []PerkStatModifier{
				{Stat: statArmor, Op: statOpAdd, Value: -3},
			},
		}},
	}
	got := describePerk(def)
	want := "Enemies within 80 gain -3 Armor. Multiple sources do not stack; the strongest aura wins."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if strings.Contains(got, "additional") {
		t.Errorf("no PerAdditionalSource authored — should not render a per-source stacking clause: %q", got)
	}
}

// TestDescribePerk_ZealousMarch_MentionsRadiusAndStacking is the real-catalog
// regression this task's Problem 2 targets: zealous_march's tooltipTemplate
// was deleted, so describePerk is now the ONLY source of its tooltip prose.
// Expectations are derived from the perk's own typed Auras data (radius,
// stat delta, PerAdditionalSource), never a pinned balance literal, per the
// "no hardcoded tunables in tests" convention.
func TestDescribePerk_ZealousMarch_MentionsRadiusAndStacking(t *testing.T) {
	def := requirePerkDef(t, "zealous_march")
	if len(def.Auras) != 1 || len(def.Auras[0].StatModifiers) != 1 {
		t.Fatalf("zealous_march: expected exactly one aura with one stat modifier, got %+v", def.Auras)
	}
	aura := def.Auras[0]
	sm := aura.StatModifiers[0]

	got := describePerk(def)
	if strings.TrimSpace(got) == "" {
		t.Fatal("zealous_march: generated description is empty")
	}

	wantRadius := trimFloat(aura.Radius)
	if !strings.Contains(got, wantRadius) {
		t.Errorf("zealous_march: missing radius %q in %q", wantRadius, got)
	}

	wantBase := signedPercent(sm.Value)
	if !strings.Contains(got, wantBase) {
		t.Errorf("zealous_march: missing base bonus %q in %q", wantBase, got)
	}

	if aura.PerAdditionalSource <= 0 {
		t.Fatal("zealous_march: PerAdditionalSource must be > 0 for the stacking assertion below to be meaningful")
	}
	wantStack := signedPercent(aura.PerAdditionalSource)
	if !strings.Contains(got, wantStack) {
		t.Errorf("zealous_march: missing stacking bonus %q in %q", wantStack, got)
	}
	if !strings.Contains(got, "additional") {
		t.Errorf("zealous_march: expected a stacking clause (PerAdditionalSource > 0) in %q", got)
	}
}

// TestDescribePerk_ManaConduit_MentionsRadiusTargetsAndMaxWinsRule is the
// real-catalog regression this task's Part 1 targets: mana_conduit's
// tooltipTemplate is deleted once describePerk conveys everything it did —
// the amount, the radius, who it affects, and the no-stack/max-wins rule.
// Expectations are derived from the perk's own typed Auras data, never a
// pinned balance literal.
func TestDescribePerk_ManaConduit_MentionsRadiusTargetsAndMaxWinsRule(t *testing.T) {
	def := requirePerkDef(t, "mana_conduit")
	if len(def.Auras) != 1 || len(def.Auras[0].StatModifiers) != 1 {
		t.Fatalf("mana_conduit: expected exactly one aura with one stat modifier, got %+v", def.Auras)
	}
	aura := def.Auras[0]
	sm := aura.StatModifiers[0]
	if aura.PerAdditionalSource != 0 {
		t.Fatal("mana_conduit: PerAdditionalSource must be 0 for the max-wins assertion below to be meaningful")
	}

	got := describePerk(def)
	if strings.TrimSpace(got) == "" {
		t.Fatal("mana_conduit: generated description is empty")
	}

	wantRadius := trimFloat(aura.Radius)
	if !strings.Contains(got, wantRadius) {
		t.Errorf("mana_conduit: missing radius %q in %q", wantRadius, got)
	}

	wantAmount := describeAuraStatModifierClause(sm)
	if !strings.Contains(got, wantAmount) {
		t.Errorf("mana_conduit: missing amount clause %q in %q", wantAmount, got)
	}

	if !strings.Contains(got, auraTargetLabel(aura.Targets)) {
		t.Errorf("mana_conduit: missing target label %q in %q", auraTargetLabel(aura.Targets), got)
	}

	if !strings.Contains(got, "do not stack") || !strings.Contains(got, "strongest") {
		t.Errorf("mana_conduit: expected the max-wins stacking rule in %q", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AbilityModifiers — real catalog perks (beam_mastery: all four scalars;
// soul_leech: only damage + heal). Expectations derived from the loaded
// def's own AbilityModifiers, never pinned balance literals.
// ─────────────────────────────────────────────────────────────────────────────

func TestDescribePerk_AbilityModifiers_RealCatalogPerks(t *testing.T) {
	for _, id := range []string{"beam_mastery", "soul_leech"} {
		t.Run(id, func(t *testing.T) {
			def := requirePerkDef(t, id)
			if len(def.AbilityModifiers) == 0 {
				t.Fatalf("%s: fixture perk has no AbilityModifiers — test needs updating", id)
			}
			got := describePerk(def)

			for _, m := range def.AbilityModifiers {
				wantName := abilityDisplayNameOrID(m.Target)
				if !strings.Contains(got, wantName+":") {
					t.Errorf("%s: missing ability header %q in %q", id, wantName+":", got)
				}
				fields := []struct {
					mult  float64
					label string
				}{
					{m.DamageMult, "damage"},
					{m.HealMult, "healing"},
					{m.ManaCostMult, "mana cost"},
					{m.RangeMult, "range"},
				}
				for _, f := range fields {
					want := fmt.Sprintf("%s %s", signedPercent(f.mult-1), f.label)
					contains := strings.Contains(got, want)
					if f.mult == 0 {
						if contains {
							t.Errorf("%s: unset field %q should not appear as %q in %q", id, f.label, want, got)
						}
						continue
					}
					if !contains {
						t.Errorf("%s: missing clause %q in %q", id, want, got)
					}
				}
			}
		})
	}
}

// beam_mastery sets all four AbilityModifier scalars; soul_leech sets only
// two. Pin that STRUCTURAL difference (not the tuning values) so a future
// edit that accidentally adds/removes a scalar on either fixture is caught.
func TestDescribePerk_AbilityModifiers_SoulLeechOmitsUnsetScalars(t *testing.T) {
	def := requirePerkDef(t, "soul_leech")
	got := describePerk(def)
	for _, absent := range []string{"mana cost", "range"} {
		if strings.Contains(got, absent) {
			t.Errorf("soul_leech: unexpected %q in %q (ManaCostMult/RangeMult are unset)", absent, got)
		}
	}
	for _, present := range []string{"damage", "healing"} {
		if !strings.Contains(got, present) {
			t.Errorf("soul_leech: expected %q in %q", present, got)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AbilityRiders — real catalog perk (shared_suffering).
// ─────────────────────────────────────────────────────────────────────────────

func TestDescribePerk_AbilityRiders_SharedSuffering(t *testing.T) {
	def := requirePerkDef(t, "shared_suffering")
	if len(def.AbilityRiders) != 1 {
		t.Fatalf("shared_suffering: expected exactly one rider fixture, got %d — test needs updating", len(def.AbilityRiders))
	}
	rider := def.AbilityRiders[0]
	got := describePerk(def)

	wantName := abilityDisplayNameOrID(rider.Target)
	wantTrigger := riderTriggerLabel(rider.Trigger)
	wantEffect := riderEffectSummary(rider.Actions)
	if wantEffect == "" {
		t.Fatal("shared_suffering: rider fixture has no recognized effect action — test needs updating")
	}
	want := fmt.Sprintf("Adds an extra %s effect to %s's %s.", wantEffect, wantName, wantTrigger)
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GrantsAbilities — synthetic fixture (no shipped perk uses this field yet).
// ─────────────────────────────────────────────────────────────────────────────

func TestDescribePerk_GrantsAbilities_Synthetic(t *testing.T) {
	// Use a real ability id so the DisplayName-resolution branch is exercised
	// too, alongside an unknown id to exercise the humanized-fallback branch.
	def := PerkDef{
		ID:              "synthetic_grant",
		GrantsAbilities: []string{"siphon_life", "not_a_real_ability_id"},
	}
	got := describePerk(def)
	want := "Grants: Siphon Life, Not A Real Ability Id."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Determinism
// ─────────────────────────────────────────────────────────────────────────────

// TestDescribePerk_Deterministic_SameInputSameOutput guards against the
// classic Go bug this generator is most exposed to: ranging over a map
// (byStage in describePerkStatModifiers, seen in riderEffectSummary) to
// build output directly rather than only using the map as a side lookup
// while a fixed slice drives iteration order. A def with modifiers across
// every stage plus a multi-action rider is run many times; any output
// divergence would indicate map-iteration-order leakage.
func TestDescribePerk_Deterministic_SameInputSameOutput(t *testing.T) {
	def := PerkDef{
		ID: "synthetic_determinism",
		StatModifiers: []PerkStatModifier{
			{Stat: statDamage, Op: statOpMultiply, Value: 1.2, Stage: statStageFinal},
			{Stat: statArmor, Op: statOpAdd, Value: 3, Stage: statStageBase},
			{Stat: statCritChance, Op: statOpMultiply, Value: 1.5, Stage: statStageIntrinsic},
			{Stat: statMoveSpeed, Op: statOpAdd, Value: 1, Stage: statStageIntrinsic},
		},
		AbilityModifiers: []AbilityModifier{
			{Target: "siphon_life", DamageMult: 1.3, HealMult: 1.1, ManaCostMult: 0.9, RangeMult: 1.05},
		},
		AbilityRiders: []AbilityRider{
			{
				Target:  "siphon_life",
				Trigger: TriggerOnTick,
				Actions: []AbilityActionDef{
					{ID: "a", Type: ActionSelectTargets},
					{ID: "b", Type: ActionDealDamage},
					{ID: "c", Type: ActionApplyStatus},
				},
			},
		},
		GrantsAbilities: []string{"siphon_life", "arcane_bolt"},
	}

	first := describePerk(def)
	if first == "" {
		t.Fatal("expected non-empty description")
	}
	for i := 0; i < 50; i++ {
		if got := describePerk(def); got != first {
			t.Fatalf("iteration %d: got %q, want %q (non-deterministic output)", i, got, first)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Coverage sweep — every catalog perk with typed authored data must generate
// well-formed prose (non-empty, sentence-terminated) without panicking.
// ─────────────────────────────────────────────────────────────────────────────

func TestDescribePerk_CoversEveryTypedCatalogPerk(t *testing.T) {
	defs := ListPerkDefs()
	if len(defs) == 0 {
		t.Fatal("no perks loaded from catalog")
	}
	covered := 0
	for _, def := range defs {
		hasTypedData := len(def.StatModifiers) > 0 || len(def.Auras) > 0 ||
			len(def.AbilityModifiers) > 0 || len(def.AbilityRiders) > 0 || len(def.GrantsAbilities) > 0
		if !hasTypedData {
			continue
		}
		covered++
		desc := describePerk(def)
		if strings.TrimSpace(desc) == "" {
			t.Errorf("%s: generated description is empty despite typed data", def.ID)
			continue
		}
		if !strings.HasSuffix(strings.TrimSpace(desc), ".") {
			t.Errorf("%s: description not sentence-terminated: %q", def.ID, desc)
		}
		// GeneratedDescription (populated by ListPerkDefs) must match
		// describePerk called directly — same "derived, never diverges"
		// contract as Wired.
		if def.GeneratedDescription != desc {
			t.Errorf("%s: GeneratedDescription %q != describePerk(def) %q", def.ID, def.GeneratedDescription, desc)
		}
	}
	if covered == 0 {
		t.Fatal("no catalog perk has typed authored data — is the perk_stat_modifiers/ability_modifiers migration wired?")
	}
}

// The registry's own defs (perkDefsByID) must never carry a populated
// GeneratedDescription — it is presentation-only and derived exclusively on
// ListPerkDefs' returned copies, mirroring Wired's discipline.
func TestDescribePerk_RegistryDefsNeverCarryGeneratedDescription(t *testing.T) {
	for _, def := range snapshotPerkDefs() {
		if def.GeneratedDescription != "" {
			t.Errorf("%s: registry def has non-empty GeneratedDescription %q; must only be set on ListPerkDefs copies", def.ID, def.GeneratedDescription)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AbilityStats / AbilityFields
//
// These two authoring forms produced NOTHING for a long time: a perk whose only
// contribution was an ability-stat row generated an empty description, which is
// exactly how wider_nets and extended_setup ended up depending on a hand-written
// tooltipTemplate and a config block to say what they did.
// ─────────────────────────────────────────────────────────────────────────────

func TestDescribePerkAbilityStats(t *testing.T) {
	t.Run("a kinded row reads as a percentage of that shape", func(t *testing.T) {
		got := describePerkAbilityStats([]PerkAbilityStat{{Stat: "create_zone.radius", Pct: 0.5}})
		if got != "+50% Zone Radius." {
			t.Errorf("got %q, want %q", got, "+50% Zone Radius.")
		}
	})

	t.Run("naming an ability scopes the clause to it", func(t *testing.T) {
		got := describePerkAbilityStats([]PerkAbilityStat{{Ability: "fire_pit", Stat: "duration", Flat: 2}})
		if !strings.HasPrefix(got, "Fire Pit:") {
			t.Errorf("got %q, want a clause naming Fire Pit", got)
		}
	})

	// An INFLICTED row changes what the ability does TO a unit, and its sign is
	// the whole meaning — a negative Move Speed contribution is a STRONGER slow.
	// A fixed-1.0-baseline stat reads in percentage points; anything else raw.
	t.Run("an inflicted row reads as applied, with its sign intact", func(t *testing.T) {
		got := describePerkAbilityStats([]PerkAbilityStat{
			{Stat: statDamageTaken, Flat: 0.15},
			{Stat: "moveSpeed", Flat: -0.15},
		})
		want := "+15% Vulnerable applied. -0.15 Move Speed applied."
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("a row with no value contributes no clause", func(t *testing.T) {
		if got := describePerkAbilityStats([]PerkAbilityStat{{Stat: "duration"}}); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func TestDescribePerkAbilityFields(t *testing.T) {
	got := describePerkAbilityFields([]AbilityFieldModifier{
		{Target: "marker_trap", Action: "mark", Field: "duration", Op: statOpMultiply, Value: 1.35},
	})
	if got != "Marker Trap: +35% duration." {
		t.Errorf("got %q, want %q", got, "Marker Trap: +35% duration.")
	}

	// An identity contribution says nothing rather than "+0%".
	if got := describePerkAbilityFields([]AbilityFieldModifier{
		{Target: "marker_trap", Action: "mark", Field: "duration", Op: statOpMultiply, Value: 1},
	}); got != "" {
		t.Errorf("identity modifier described as %q, want empty", got)
	}
}

// "+35% Ability Damage %" reads as a typo. The trailing % earns its place in a
// PICKER, where it is what separates the stat from Ability Power at a glance.
func TestDescribePerk_DoesNotDoubleThePercentSign(t *testing.T) {
	got := describePerkStatModifiers([]PerkStatModifier{
		{Stat: statAbilityDamage, Op: statOpMultiply, Value: 1.35},
	})
	if strings.Contains(got, "% %") || strings.Contains(got, "Damage %") {
		t.Errorf("got %q; a percentage clause must not also carry the label's %% suffix", got)
	}
}

// Every perk that contributes SOMETHING must describe itself. A silent perk is
// how a hand-written tooltip becomes load-bearing.
func TestCatalog_EveryContributingPerkDescribesItself(t *testing.T) {
	for _, def := range ListPerkDefs() {
		contributes := len(def.StatModifiers) > 0 || len(def.Auras) > 0 ||
			len(def.AbilityModifiers) > 0 || len(def.AbilityRiders) > 0 ||
			len(def.GrantsAbilities) > 0 || len(def.AbilityStats) > 0 ||
			len(def.AbilityFields) > 0
		if !contributes {
			continue // a Go-wired perk describes itself in prose, not from data
		}
		if describePerk(def) == "" {
			t.Errorf("perk %q authors data contributions but generates an empty description", def.ID)
		}
	}
}
