package game

import (
	"sort"
	"testing"
)

// wantPathPools is the authoritative "mirror today's pools" mapping. Phase A
// makes perksByRank produce exactly these sets; Phase B makes them the SOLE
// source. Keeping this table in the test (not derived from the JSON) is
// intentional: it is the human-authored contract the catalog must satisfy, so
// a stray edit to a path file fails here instead of silently changing rolls.
var wantPathPools = map[string]map[string][]string{
	"cleric": {
		"bronze": {"battle_prayer", "bolstering_prayer", "mana_conduit", "sanctuary"},
		"silver": {"divine_aegis", "divine_healer", "restoration_aura", "zealous_march"},
		"gold":   {"beacon_of_life", "divine_intervention", "divine_judgement"},
	},
	"siphoner": {
		"bronze": {"lingering_hex", "mark_of_weakness", "soul_leech", "withering_beam"},
		"silver": {"amplify_damage", "chain_siphon", "dark_renewal", "shared_suffering"},
		"gold":   {"ascended_corruption", "beam_mastery", "repurposed_life"},
	},
	"arch_mage": {
		"gold": {"arcane_conduit", "arcane_feedback", "unstable_magic"},
	},
	"marksman": {
		"bronze": {"eagle_spirit", "hawk_spirit", "vulture_spirit"},
		"silver": {"hunters_mark", "pierce", "split_shot"},
		"gold":   {"bullseye", "double_shot", "explosive_tips"},
	},
	"trapper": {
		"bronze": {"caltrops", "explosive_trap", "fire_pit", "marker_trap"},
		"silver": {"amplified_effects", "barbed_field", "explosive_chain", "exposed_weakness", "extended_setup", "lasting_flames", "rapid_deployment", "wider_nets"},
		"gold":   {"ascendant_infusion", "increased_deployment", "overload_protocol"},
	},
	"berserker": {
		"bronze": {"bloodlust", "cleaving_rage", "frenzy_core", "relentless", "savage_strikes"},
		"silver": {"blood_sustain", "executioner", "momentum"},
		"gold":   {"berserk_state", "blood_engine", "whirlwind_core"},
	},
	"vanguard": {
		"bronze": {"hold_the_line", "interlock", "reinforced_armor", "retaliation", "shield_bash"},
		"silver": {"brace", "challengers_mark", "last_stand", "punishing_guard"},
		"gold":   {"guardian_aura", "pain_share", "rallying_banner"},
	},
}

// pathUnitType maps each path to the unit type a probe Unit needs so
// eligiblePerksForUnitAtRank resolves the right pool.
var pathUnitType = map[string]string{
	"cleric": "acolyte", "siphoner": "acolyte", "arch_mage": "adept",
	"marksman": "archer", "trapper": "archer", "berserker": "soldier", "vanguard": "soldier",
}

func poolIDsAtRank(path, rank string) []string {
	u := &Unit{UnitType: pathUnitType[path], ProgressionPath: path}
	defs := eligiblePerksForUnitAtRank(u, rank)
	ids := make([]string, 0, len(defs))
	for _, d := range defs {
		ids = append(ids, d.ID)
	}
	sort.Strings(ids)
	return ids
}

func TestPathPoolsMatchMirror(t *testing.T) {
	for path, byRank := range wantPathPools {
		for rank, want := range byRank {
			sort.Strings(want)
			got := poolIDsAtRank(path, rank)
			if len(got) != len(want) {
				t.Errorf("%s/%s: got %v, want %v", path, rank, got, want)
				continue
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("%s/%s: got %v, want %v", path, rank, got, want)
					break
				}
			}
		}
	}
}
