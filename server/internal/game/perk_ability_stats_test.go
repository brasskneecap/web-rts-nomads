package game

import (
	"math"
	"testing"
)

// Every authored perk's abilityStats must name a REAL stat and, where it names
// an ability, a real ability. Neither check can run at perk load — perk defs are
// built by a package-level var initializer, before any init() has populated the
// action registry or the ability catalog (see validatePerkAbilityStats). So this
// is the guard, and it fails CI rather than at boot.
//
// The failure it exists to catch is silent: a row addressing "raduis" or a
// renamed ability contributes nothing at all and looks perfectly correct in the
// editor.
func TestCatalog_PerkAbilityStatsResolve(t *testing.T) {
	for _, def := range ListPerkDefs() {
		if len(def.AbilityStats) == 0 {
			continue
		}
		if err := perkAbilityStatsResolve("perk \""+def.ID+"\"", def.AbilityStats); err != nil {
			t.Error(err)
		}
	}
}

// registerScopePerk puts a throwaway perk in the runtime overlay for the length
// of the test, so contributions are exercised through the real perkDefByID
// lookup the fold uses rather than a hand-built source list.
func registerScopePerk(t *testing.T, rows []PerkAbilityStat) {
	t.Helper()
	runtimePerksMu.Lock()
	runtimePerks["zz_scope_perk"] = PerkDef{ID: "zz_scope_perk", AbilityStats: rows}
	runtimePerksMu.Unlock()
	rebuildPerkRegistry()
	t.Cleanup(func() {
		runtimePerksMu.Lock()
		delete(runtimePerks, "zz_scope_perk")
		runtimePerksMu.Unlock()
		rebuildPerkRegistry()
	})
}

// A perk row that names an ability applies to THAT ability and nothing else; a
// row that names none applies to everything the unit can cast. That distinction
// is the whole reason the ability id is threaded down to the fold.
func TestPerkAbilityStats_AbilityScoping(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	radiusFor := func(abilityID string) float64 {
		raw := s.applyAbilityStatsToConfigLocked(caster, abilityID, ActionCreateZone,
			mustJSON(map[string]any{"radius": 100.0}))
		v, _ := decodedConfig(t, raw)["radius"].(float64)
		return v
	}
	near := func(got, want float64) bool { return math.Abs(got-want) < 1e-9 }

	// A throwaway perk in the runtime overlay, so the scoping is exercised
	// through the real perkDefByID lookup the fold uses.
	withPerk := func(rows []PerkAbilityStat, body func()) {
		registerScopePerk(t, rows)
		caster.PerkIDs = []string{"zz_scope_perk"}
		body()
		caster.PerkIDs = nil
	}

	withPerk([]PerkAbilityStat{{Ability: "fire_pit", Stat: "radius", Pct: 0.5}}, func() {
		if got := radiusFor("fire_pit"); !near(got, 150) {
			t.Errorf("named ability radius = %v, want 150", got)
		}
		if got := radiusFor("caltrops"); !near(got, 100) {
			t.Errorf("a DIFFERENT ability got %v, want 100 — an ability-scoped row leaked", got)
		}
	})

	withPerk([]PerkAbilityStat{{Stat: "radius", Pct: 0.5}}, func() {
		if got := radiusFor("fire_pit"); !near(got, 150) {
			t.Errorf("unscoped row on fire_pit = %v, want 150", got)
		}
		if got := radiusFor("caltrops"); !near(got, 150) {
			t.Errorf("unscoped row on caltrops = %v, want 150 — it should reach every ability", got)
		}
	})

	// Both rows on one perk: the global one always applies, the named one adds
	// on top only for its own ability. Two separate perks would compose the same
	// way, so one perk must not behave differently.
	withPerk([]PerkAbilityStat{
		{Stat: "radius", Pct: 0.5},
		{Ability: "fire_pit", Stat: "radius", Pct: 0.5},
	}, func() {
		if got := radiusFor("fire_pit"); !near(got, 200) {
			t.Errorf("both rows on fire_pit = %v, want 200 (0.5 + 0.5 on a base of 100)", got)
		}
		if got := radiusFor("caltrops"); !near(got, 150) {
			t.Errorf("both rows on caltrops = %v, want 150 (the global row only)", got)
		}
	})
}

// A perk carrying no ability stats must not change what any ability does — the
// early bail has to stay correct now that perks are a source.
func TestPerkAbilityStats_NoPerkIsANoOp(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	in := mustJSON(map[string]any{"radius": 100.0})
	if got := s.applyAbilityStatsToConfigLocked(caster, "fire_pit", ActionCreateZone, in); string(got) != string(in) {
		t.Errorf("config changed for a caster with no ability stats: %s", got)
	}
}

// INFLICTED-STAT addressing: a row names the unit stat an ability applies, and
// finds it wherever it lives in the program without knowing the action's id.
//
// This is the form "Add Ability Stat -> Vulnerable +0.15" produces. Its
// advantage over the precise {action, field} form is durability: renaming
// marker_trap's "vulnerable" action silently unhooks an abilityFields row
// pointing at it, and cannot unhook this.
func TestPerkAbilityStats_InflictedStatFindsTheChangeStat(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	value := func(statID string, base float64) float64 {
		raw := s.applyAbilityStatsToConfigLocked(caster, "marker_trap", ActionChangeStat,
			mustJSON(map[string]any{"stat": statID, "op": "add", "value": base}))
		v, _ := decodedConfig(t, raw)["value"].(float64)
		return v
	}
	near := func(got, want float64) bool { return math.Abs(got-want) < 1e-9 }

	registerScopePerk(t, []PerkAbilityStat{
		{Stat: statDamageTaken, Flat: 0.15},
		{Stat: "moveSpeed", Flat: -0.15},
	})
	caster.PerkIDs = []string{"zz_scope_perk"}

	// Positive strengthens a debuff…
	if got := value(statDamageTaken, 0.2); !near(got, 0.35) {
		t.Errorf("damageTaken value = %v, want 0.35", got)
	}
	// …and negative strengthens an inverse-sense one. A slow multiplier of 0.35
	// means "slowed to 35% speed", so 0.20 is STRONGER. This is why the family
	// is flat-only: no percentage reads the same way at both sites.
	if got := value("moveSpeed", 0.35); !near(got, 0.2) {
		t.Errorf("moveSpeed value = %v, want 0.2 (a stronger slow)", got)
	}
	// A stat the perk says nothing about is untouched.
	if got := value("attackSpeed", 1.0); !near(got, 1.0) {
		t.Errorf("attackSpeed value = %v, want 1.0 unchanged", got)
	}
}

// The reporting read (tooltips, the trap stat panel) must agree with what the
// executor applies. It did not, once: EffectiveAbilityFieldLocked folded kinded
// ability stats but not inflicted ones, so every amplified slow and
// vulnerability was under-reported.
func TestPerkAbilityStats_InflictedStatReachesTheReportingRead(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 0, 0)
	before, ok := s.EffectiveAbilityFieldLocked(caster, "marker_trap", "vulnerable", "value")
	if !ok {
		t.Fatal("marker_trap vulnerable.value not readable")
	}

	registerScopePerk(t, []PerkAbilityStat{{Stat: statDamageTaken, Flat: 0.15}})
	caster.PerkIDs = []string{"zz_scope_perk"}

	after, _ := s.EffectiveAbilityFieldLocked(caster, "marker_trap", "vulnerable", "value")
	if math.Abs((after-before)-0.15) > 1e-9 {
		t.Errorf("reported vulnerability went %v -> %v, want +0.15 — the tooltip disagrees with the executor", before, after)
	}
}
