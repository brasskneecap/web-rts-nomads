package game

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Every shipped ability must generate non-empty prose that names its damage
// school and surfaces its headline magnitude. Expectations are derived from the
// loaded def (never hardcoded numbers) so balance changes to the catalog can't
// break these tests — they assert the generator's structural invariants, not
// specific tuning values.
func TestDescribeAbility_CoversEveryCatalogAbility(t *testing.T) {
	defs := ListAbilityDefs()
	if len(defs) == 0 {
		t.Fatal("no abilities loaded from catalog")
	}
	for _, def := range defs {
		desc := describeAbility(def)
		if strings.TrimSpace(desc) == "" {
			t.Errorf("%s: generated description is empty", def.ID)
			continue
		}
		if !strings.HasSuffix(strings.TrimSpace(desc), ".") {
			t.Errorf("%s: description not sentence-terminated: %q", def.ID, desc)
		}

		// The headline magnitude of the ability's primary effect must appear.
		switch {
		case def.HealAmount > 0:
			assertContains(t, def.ID, desc, fmt.Sprint(def.HealAmount))
		case def.DamageAmount > 0:
			assertContains(t, def.ID, desc, fmt.Sprint(def.DamageAmount))
			assertContains(t, def.ID, desc, string(def.DamageType.OrPhysical()))
		case def.DamagePerSecond > 0:
			assertContains(t, def.ID, desc, string(def.DamageType.OrPhysical()))
		case def.DamagePerTick > 0: // channel
			assertContains(t, def.ID, desc, fmt.Sprint(def.DamagePerTick))
		case def.SummonUnitType != "":
			assertContains(t, def.ID, desc, humanizeID(def.SummonUnitType))
		case def.IsChargeFirePassive():
			assertContains(t, def.ID, desc, fmt.Sprint(def.DamagePerMissile))
		}
	}
}

// The author override wins verbatim over the generated text.
func TestEffectiveDescription_OverrideWins(t *testing.T) {
	def, ok := getAbilityDef("heal")
	if !ok {
		t.Fatal("heal ability not found")
	}
	generated := def.EffectiveDescription()
	if generated == "" {
		t.Fatal("expected generated description for heal")
	}

	def.Description = "Custom authored text."
	if got := def.EffectiveDescription(); got != "Custom authored text." {
		t.Errorf("override not honored: got %q", got)
	}

	// Whitespace-only override falls back to generated.
	def.Description = "   "
	if got := def.EffectiveDescription(); got != generated {
		t.Errorf("blank override should fall back to generated: got %q want %q", got, generated)
	}
}

// Mechanic-family composition invariants: each specialized field contributes a
// recognizable clause. Values are read from the def so the assertions track the
// catalog.
func TestDescribeAbility_MechanicClauses(t *testing.T) {
	cases := []struct {
		id       string
		mustHave func(def AbilityDef) []string
	}{
		{"siphon_life", func(def AbilityDef) []string {
			return []string{"drains", "beam", fmt.Sprint(def.DamagePerTick)}
		}},
		{"meteor", func(def AbilityDef) []string {
			// Burn crater clause with its per-tick number.
			return []string{"burning", fmt.Sprint(def.BurnDamagePerTick)}
		}},
		{"shatter", func(def AbilityDef) []string {
			pct := int((1 - def.SlowMultiplier) * 100)
			return []string{"slow", fmt.Sprintf("%d%%", pct)}
		}},
		{"chain_lightning", func(def AbilityDef) []string {
			return []string{"arcs", fmt.Sprint(def.ChainCount)}
		}},
		{"raise_skeleton", func(def AbilityDef) []string {
			return []string{"Summons", fmt.Sprint(def.SummonCount)}
		}},
		{"greater_heal", func(def AbilityDef) []string {
			return []string{"restores", fmt.Sprint(def.TargetCount)}
		}},
		{"arcane_orb", func(def AbilityDef) []string {
			return []string{"per second", "pulling"}
		}},
	}
	for _, tc := range cases {
		def, ok := getAbilityDef(tc.id)
		if !ok {
			t.Errorf("%s: ability not found", tc.id)
			continue
		}
		// Every id in this table (including siphon_life, the last ability
		// migrated) is schemaVersion:2 as of the composable-abilities
		// migration: their flat mechanic fields (BurnDamagePerTick,
		// SlowMultiplier, SummonCount, TargetCount, ChainCount,
		// DamagePerSecond/PullStrength, DamagePerTick, ...) are cleared, so
		// mustHave's expected values must be recovered from the compiled
		// Program instead — see abilityMechanicsShadow.
		def = abilityMechanicsShadow(def)
		desc := describeAbility(def)
		for _, want := range tc.mustHave(def) {
			assertContains(t, tc.id, desc, want)
		}
	}
}

func assertContains(t *testing.T, id, haystack, needle string) {
	t.Helper()
	if !strings.Contains(strings.ToLower(haystack), strings.ToLower(needle)) {
		t.Errorf("%s: description %q missing %q", id, haystack, needle)
	}
}

// legacyFixtureByID maps each of the five composable-abilities-migration ids
// to its frozen pre-migration shape (ability_legacy_fixtures_test.go). The
// live catalog defs for these ids are schemaVersion:2 now (Program is the
// sole authority, mechanic fields cleared), so ConvertLegacyAbility(id) would
// error on them directly ("already composable") — tests that need to exercise
// the conversion ITSELF (not just its already-shipped result) register the
// frozen fixture under a scratch id instead.
var legacyFixtureByID = map[string]func() AbilityDef{
	"heal":            legacyHealFixture,
	"greater_heal":    legacyGreaterHealFixture,
	"shatter":         legacyShatterFixture,
	"raise_skeleton":  legacyRaiseSkeletonFixture,
	"meteor":          legacyMeteorFixture,
	"arcane_bolt":     legacyArcaneBoltFixture,
	"fireball":        legacyFireballFixture,
	"chain_lightning": legacyChainLightningFixture,
	"arcane_orb":      legacyArcaneOrbFixture,
	"arcane_missiles": legacyArcaneMissilesFixture,
}

// registerFrozenFixtureForConvert registers id's frozen pre-migration fixture
// under a scratch id (so ConvertLegacyAbility, which looks up by string id
// via getAbilityDef, has a genuinely legacy def to operate on) and returns
// (frozen def, scratch id).
func registerFrozenFixtureForConvert(t *testing.T, id string) (AbilityDef, string) {
	t.Helper()
	build, ok := legacyFixtureByID[id]
	if !ok {
		t.Fatalf("no frozen legacy fixture registered for %q", id)
	}
	def := build()
	scratchID := id + "_pre_migration_describe_test"
	def.ID = scratchID
	registerRuntimeTestAbility(t, def)
	return def, scratchID
}

// TestDescribeAbility_ProgramEquivalence is the acceptance test for the
// legacy->composable migration: converting an ability to schemaVersion 2 (via
// ConvertLegacyAbility, which clears every legacy mechanic field per its own
// contract) must NOT change its generated tooltip prose one character. Each id
// here was migrated by the composable-abilities plan (see
// ability_legacy_fixtures_test.go) and is exercised via its frozen
// pre-migration shape, registered under a scratch id, since the live catalog
// def is schemaVersion:2 already and ConvertLegacyAbility refuses to
// re-convert it.
func TestDescribeAbility_ProgramEquivalence(t *testing.T) {
	ids := []string{"heal", "greater_heal", "shatter", "raise_skeleton", "meteor", "arcane_bolt", "fireball", "chain_lightning", "arcane_orb", "arcane_missiles"}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			legacy, scratchID := registerFrozenFixtureForConvert(t, id)
			want := describeAbility(legacy)
			if strings.TrimSpace(want) == "" {
				t.Fatalf("%s: legacy description is empty (test setup problem, not a program problem)", id)
			}

			converted, _, err := ConvertLegacyAbility(scratchID)
			if err != nil {
				t.Fatalf("%s: ConvertLegacyAbility: %v", id, err)
			}
			if converted.SchemaVersion < 2 || converted.Program == nil {
				t.Fatalf("%s: conversion did not produce a schemaVersion 2 program", id)
			}

			got := describeAbility(converted)
			if got != want {
				t.Errorf("%s: description changed by conversion\n  legacy:    %q\n  converted: %q", id, want, got)
			}

			// Cross-check against the REAL shipped catalog def too: the
			// migration's whole point is that what's live in production reads
			// identically to what this test just proved the compiler produces.
			shipped, ok := getAbilityDef(id)
			if !ok {
				t.Fatalf("%s: live catalog ability not found", id)
			}
			live := describeAbility(shipped)
			if id == "chain_lightning" {
				// chain_lightning has been REBALANCED away from legacy
				// (author-tuned loop: more bounces, percent falloff), so its live
				// prose legitimately differs from the frozen legacy baseline. The
				// compiler-equivalence check above (converting the frozen fixture)
				// still holds; here we only sanity-check the live prose is present
				// and still describes a chain.
				if strings.TrimSpace(live) == "" {
					t.Errorf("%s: live description is empty", id)
				}
				if !strings.Contains(live, "arcs to") {
					t.Errorf("%s: live description no longer mentions chaining: %q", id, live)
				}
			} else if live != want {
				t.Errorf("%s: LIVE catalog description differs from the frozen-fixture/compiler baseline\n  baseline: %q\n  live:     %q", id, want, live)
			}
		})
	}
}

// TestEffectiveDescription_ProgramAbility exercises the two callers that must
// keep working for a v2 ability (per the ability-editor "reset to generated"
// affordance): EffectiveDescription falls back to the program-derived prose
// when no override is set and still lets an author override win verbatim;
// GeneratedDescription always ignores the override. Uses the frozen
// pre-migration "shatter" fixture (registered under a scratch id) since the
// live catalog "shatter" is already schemaVersion:2 and can't be re-converted
// — see registerFrozenFixtureForConvert.
func TestEffectiveDescription_ProgramAbility(t *testing.T) {
	_, scratchID := registerFrozenFixtureForConvert(t, "shatter")
	converted, _, err := ConvertLegacyAbility(scratchID)
	if err != nil {
		t.Fatalf("ConvertLegacyAbility: %v", err)
	}

	generated := describeAbility(converted)
	if strings.TrimSpace(generated) == "" {
		t.Fatal("expected non-empty generated description for converted (v2) ability")
	}
	if got := converted.EffectiveDescription(); got != generated {
		t.Errorf("EffectiveDescription (no override) = %q, want %q", got, generated)
	}

	converted.Description = "Custom authored text."
	if got := converted.EffectiveDescription(); got != "Custom authored text." {
		t.Errorf("override not honored on v2 ability: got %q", got)
	}
	if got := converted.GeneratedDescription(); got != generated {
		t.Errorf("GeneratedDescription must ignore override: got %q want %q", got, generated)
	}
}

// ═════════════════════════════════════════════════════════════════════════
// VISIBLE ZONE PROSE
// ═════════════════════════════════════════════════════════════════════════

// THE regression this section exists for. A zone ability used to describe only
// its own placement — "Places a marker zone (115 radius). Lasts 12s." — because
// the generator looked for exactly one shape (an on_tick deal_damage, outside
// any conditional). Three of the four Trapper traps do not have that shape, so
// three of four tooltips said nothing about what the trap actually does.
//
// The "has an effect at all" walk here is deliberately INDEPENDENT of
// scanZoneActions: it looks for the action types directly, so it stays a real
// check on the generator rather than a restatement of it.
func TestDescribeAbility_VisibleZoneDescribesWhatItDoes(t *testing.T) {
	checked := 0
	for _, def := range ListAbilityDefs() {
		cfg, ok := findVisibleZoneConfig(def.Program)
		if !ok || !zoneHasAnyEffect(cfg) {
			continue
		}
		checked++
		desc := describeAbility(def)
		if !strings.Contains(desc, " that ") && !strings.Contains(desc, "When an enemy enters") {
			t.Errorf("%s: zone has effects but the description only places it: %q", def.ID, desc)
		}
	}
	if checked == 0 {
		t.Fatal("no visible-zone abilities found; this test is no longer checking anything")
	}
}

// zoneHasAnyEffect reports whether cfg's triggers contain any action carrying a
// player-visible magnitude, at any depth (including inside conditional
// branches). Independent of the describe code by design — see the caller.
func zoneHasAnyEffect(cfg createZoneConfig) bool {
	var walk func([]AbilityActionDef) bool
	walk = func(actions []AbilityActionDef) bool {
		for _, act := range actions {
			switch act.Type {
			case ActionDealDamage, ActionApplyStatusDuration, ActionApplyStatus, ActionConsumeZone:
				return true
			case ActionConditional:
				var cc conditionalConfig
				decodeActionConfig(act.Config, &cc)
				if walk(cc.Then) || walk(cc.Else) {
					return true
				}
			}
			for _, child := range act.Children {
				if walk(child.Actions) {
					return true
				}
			}
		}
		return false
	}
	for _, trg := range cfg.Triggers {
		if walk(trg.Actions) {
			return true
		}
	}
	return false
}

// Each clause shape a zone can produce, on synthetic configs so the numbers are
// the test's own rather than catalog tuning.
func TestDescribeZone_ClauseShapes(t *testing.T) {
	zone := func(triggers []AbilityTriggerDef, tickInterval float64) AbilityDef {
		cfg := createZoneConfig{
			Name: "Test Zone", Radius: 50, Duration: 10,
			TickInterval: tickInterval, Sprite: "x", Triggers: triggers,
		}
		return AbilityDef{
			ID: "synthetic_zone", DisplayName: "Synthetic", SchemaVersion: 2,
			Program: &AbilityProgram{Triggers: []AbilityTriggerDef{{
				Type:    TriggerOnCastComplete,
				Actions: []AbilityActionDef{{Type: ActionCreateZone, Config: rawJSON(t, cfg)}},
			}}},
		}
	}
	damage := AbilityActionDef{Type: ActionDealDamage, Config: rawJSON(t, dealDamageConfig{Amount: 7, Type: DamageFire})}
	status := func(name string, duration, tickInterval float64, nested ...AbilityActionDef) AbilityActionDef {
		return AbilityActionDef{Type: ActionApplyStatusDuration, Config: rawJSON(t, applyStatusDurationConfig{
			Name: name, Duration: duration, TickInterval: tickInterval,
			Triggers: []AbilityTriggerDef{{Type: TriggerOnActionComplete, Actions: nested}},
		})}
	}
	changeStat := func(stat, op string, v float64) AbilityActionDef {
		return AbilityActionDef{Type: ActionChangeStat, Config: rawJSON(t, changeStatConfig{Stat: stat, Op: op, Value: v})}
	}

	cases := []struct {
		name string
		def  AbilityDef
		want string
	}{{
		name: "tick damage reads as a rate",
		def:  zone([]AbilityTriggerDef{{Type: TriggerOnTick, Actions: []AbilityActionDef{damage}}}, 1),
		want: "that deals 7 fire damage per second",
	}, {
		name: "a slower tick names its interval",
		def:  zone([]AbilityTriggerDef{{Type: TriggerOnTick, Actions: []AbilityActionDef{damage}}}, 2),
		want: "that deals 7 fire damage every 2s",
	}, {
		name: "entry damage is a separate sentence, with no rate",
		def:  zone([]AbilityTriggerDef{{Type: TriggerOnZoneEnter, Actions: []AbilityActionDef{damage}}}, 1),
		want: "When an enemy enters, deals 7 fire damage.",
	}, {
		name: "a spent zone says so",
		def: zone([]AbilityTriggerDef{{Type: TriggerOnZoneEnter, Actions: []AbilityActionDef{
			damage, {Type: ActionConsumeZone},
		}}}, 1),
		want: "deals 7 fire damage, then vanishes.",
	}, {
		name: "a moveSpeed multiply reads as a slow, not as a stat delta",
		def: zone([]AbilityTriggerDef{{Type: TriggerOnTick, Actions: []AbilityActionDef{
			status("Slowed", 3, 0, changeStat(statMoveSpeed, statOpMultiply, 0.4)),
		}}}, 1),
		want: "slows enemies by 60% for 3s",
	}, {
		name: "other stat changes name the status and the delta",
		def: zone([]AbilityTriggerDef{{Type: TriggerOnZoneEnter, Actions: []AbilityActionDef{
			status("Marked", 4, 0, changeStat(statDamageTaken, statOpAdd, 0.25)),
		}}}, 1),
		want: "applies Marked (+25% Vulnerable) for 4s",
	}, {
		name: "a damaging status is a burn over its own duration",
		def: zone([]AbilityTriggerDef{{Type: TriggerOnTick, Actions: []AbilityActionDef{
			status("Burning", 8, 1, damage),
		}}}, 1),
		want: "burns for 7 fire damage per second for 8s",
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := describeAbility(tc.def); !strings.Contains(got, tc.want) {
				t.Errorf("description = %q, want it to contain %q", got, tc.want)
			}
		})
	}
}

// A has_perk branch gates a VARIANT: the default tooltip must promise the
// `else` side, which is what a caster without the perk actually gets. Reading
// `then` (as the old generator did) advertises a bonus the reader may not have.
func TestDescribeZone_HasPerkBranchDescribesTheDefaultSide(t *testing.T) {
	dmg := func(n int) AbilityActionDef {
		return AbilityActionDef{Type: ActionDealDamage, Config: rawJSON(t, dealDamageConfig{Amount: n})}
	}
	cfg := createZoneConfig{
		Name: "Test Zone", Radius: 50, Duration: 10, TickInterval: 1, Sprite: "x",
		Triggers: []AbilityTriggerDef{{Type: TriggerOnTick, Actions: []AbilityActionDef{{
			Type: ActionConditional,
			Config: rawJSON(t, conditionalConfig{
				Conditions: []AbilityConditionDef{{Op: condOpHasPerk, Right: rawJSON(t, "some_perk")}},
				Then:       []AbilityActionDef{dmg(99)},
				Else:       []AbilityActionDef{dmg(11)},
			}),
		}}}},
	}
	def := AbilityDef{
		ID: "synthetic_gated", DisplayName: "Gated", SchemaVersion: 2,
		Program: &AbilityProgram{Triggers: []AbilityTriggerDef{{
			Type:    TriggerOnCastComplete,
			Actions: []AbilityActionDef{{Type: ActionCreateZone, Config: rawJSON(t, cfg)}},
		}}},
	}

	got := describeAbility(def)
	if !strings.Contains(got, "deals 11 damage") {
		t.Errorf("description = %q, want the ungated (else) magnitude 11", got)
	}
	if strings.Contains(got, "99") {
		t.Errorf("description = %q, must not advertise the perk-gated magnitude 99", got)
	}
}

// rawJSON marshals a config struct for a synthetic action def. Distinct from
// the map-only mustJSON helper in path_ability_stats_test.go.
func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}
	return b
}
