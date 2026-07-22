package game

import (
	"strings"
	"testing"
)

// TestAbilityStatKinds_EveryKindIsRegistered catches a typo'd Kind on a schema
// field. A Kind the vocabulary does not know would silently never match a
// modifier — the exact "authored it, nothing happened" failure the whole
// opt-in design exists to prevent.
func TestAbilityStatKinds_EveryKindIsRegistered(t *testing.T) {
	for actionType, desc := range actionRegistry {
		for _, f := range desc.Schema.Fields {
			if f.Kind == "" {
				continue
			}
			if !isKnownAbilityStatKind(f.Kind) {
				t.Errorf("action %q field %q declares unknown kind %q — add it to abilityStatKindLabels", actionType, f.Key, f.Kind)
			}
		}
	}
}

// TestAbilityStatKinds_KindedActionsHaveAQualifier is the ROT GUARD on
// actionStatQualifier, the one hand-maintained table here. Without it, adding a
// kinded field to a new action would produce a scoped stat labelled with the raw
// action type ("apply_status_duration Duration") instead of a designer-legible
// one ("Status Duration"). Same failure mode as wiredPerkIDs: nothing breaks, the
// UI just quietly gets worse.
func TestAbilityStatKinds_KindedActionsHaveAQualifier(t *testing.T) {
	for actionType, desc := range actionRegistry {
		for _, f := range desc.Schema.Fields {
			if f.Kind == "" || !isAbilityStatGridKind(f.Kind) {
				continue
			}
			if actionStatQualifier[actionType] == "" {
				t.Errorf("action %q declares kinded field %q but has no actionStatQualifier entry — add a short label so the stat reads e.g. %q, not %q",
					actionType, f.Key, "Zone Duration", string(actionType)+" Duration")
			}
			break
		}
	}
}

// TestAbilityStatKinds_TimingFieldsAreNotKinded pins the exclusions that make
// the mechanism safe. tickInterval shares Control "duration" with the real
// duration field, so a Control-driven rule would scale it — and scaling a zone's
// tick interval silently changes its DPS rather than its lifetime. wait/seconds
// is control flow, and beam's durationMs is both a PRESENTATION value and in
// MILLISECONDS, so a flat "+2s duration" would mean "+2ms" there.
func TestAbilityStatKinds_TimingFieldsAreNotKinded(t *testing.T) {
	mustNotBeKinded := map[ActionType][]string{
		ActionCreateZone:          {"tickInterval"},
		ActionApplyStatusDuration: {"tickInterval"},
		ActionLaunchProjectile:    {"tickInterval"},
		ActionWait:                {"seconds"},
		ActionBeam:                {"durationMs", "impactDelaySeconds", "tickIntervalSeconds"},
	}
	for actionType, keys := range mustNotBeKinded {
		desc, ok := lookupActionDescriptor(actionType)
		if !ok {
			t.Fatalf("action %q is not registered", actionType)
		}
		for _, f := range desc.Schema.Fields {
			for _, key := range keys {
				if f.Key == key && f.Kind != "" {
					t.Errorf("action %q field %q must NOT carry a Kind (got %q) — see this test's doc comment", actionType, key, f.Kind)
				}
			}
		}
	}
}

// TestAbilityStatDefs_FirePitIsFullyAddressable is the acceptance case for
// scoped stats. fire_pit is the proof ability precisely because it owns TWO
// duration fields of the same kind at different depths — the zone's 10s lifetime
// and the burn status's 8s — so all three addressing levels have to be
// distinguishable on a single ability.
func TestAbilityStatDefs_FirePitIsFullyAddressable(t *testing.T) {
	defs := AbilityStatDefs()
	byID := make(map[string]AbilityStatDef, len(defs))
	for _, d := range defs {
		byID[d.ID] = d
	}

	for id, wantLabel := range map[string]string{
		"duration":                       "Duration",
		"create_zone.duration":           "Zone Duration",
		"apply_status_duration.duration": "Status Duration",
		"radius":                         "Radius",
		"create_zone.radius":             "Zone Radius",
	} {
		got, ok := byID[id]
		if !ok {
			t.Errorf("stat %q is not offered", id)
			continue
		}
		if got.Label != wantLabel {
			t.Errorf("stat %q label = %q, want %q", id, got.Label, wantLabel)
		}
	}

	// INVARIANT, not an instance: a kind with no field behind it must never be
	// offered. Asserted generically because the reachable set GROWS — `speed`
	// was unreachable until launch_projectile's projectileSpeed was surfaced and
	// kinded, at which point "Speed" and "Projectile Speed" appeared with no
	// further authoring. Pinning specific kinds here would have to be edited
	// every time that happens, which is exactly the rot the derivation avoids.
	reachable := map[string]bool{}
	for _, desc := range actionRegistry {
		for _, f := range desc.Schema.Fields {
			if f.Kind != "" {
				reachable[f.Kind] = true
			}
		}
	}
	for _, d := range defs {
		if !reachable[d.Kind] {
			t.Errorf("stat %q is offered but no registered field declares kind %q", d.ID, d.Kind)
		}
	}

	// Damage/heal are kinded for the field picker but must never become grid rows
	// — they are served by abilityPower / abilityDamage instead.
	for _, d := range defs {
		if d.Kind == abilityStatKindDamage || d.Kind == abilityStatKindHeal {
			t.Errorf("stat %q (kind %q) must not be an Ability Stats row — damage scales via abilityPower/abilityDamage", d.ID, d.Kind)
		}
	}

	// IDs must be stable and parseable: a scoped id is exactly "<action>.<kind>".
	for _, d := range defs {
		if d.Action == "" {
			continue
		}
		if want := string(d.Action) + "." + d.Kind; d.ID != want {
			t.Errorf("scoped stat id %q does not match %q", d.ID, want)
		}
		if strings.Count(d.ID, ".") != 1 {
			t.Errorf("scoped stat id %q must contain exactly one separator", d.ID)
		}
	}
}
