package game

import "testing"

func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// The real embedded bronze pool contains exactly the authored spells. Silver
// shares the same pool (cumulative Bronze∪Silver); Gold is the perk tier and
// grants no pool spell (empty).
func TestArchMageBronzePool_Contents(t *testing.T) {
	bronze := spellPoolFor("arch_mage", "bronze")
	want := []string{"fireball", "chain_lightning", "arcane_orb", "shatter", "meteor"}
	if len(bronze) != len(want) {
		t.Fatalf("bronze pool = %v; want %v", bronze, want)
	}
	for _, id := range want {
		if !containsStr(bronze, id) {
			t.Errorf("bronze pool missing %q (got %v)", id, bronze)
		}
		if _, ok := getAbilityDef(id); !ok {
			t.Errorf("bronze pool spell %q has no registered AbilityDef", id)
		}
	}
	// Silver shares the pool — it inherits the full bronze list (Bronze∪Silver).
	if silver := spellPoolFor("arch_mage", "silver"); len(silver) != len(want) {
		t.Errorf("silver pool = %v; want the shared bronze list %v", silver, want)
	}
	// Gold grants no pool spell (perk tier).
	if gold := spellPoolFor("arch_mage", "gold"); len(gold) != 0 {
		t.Errorf("gold pool = %v; want empty (Gold is the perk tier)", gold)
	}
}

// A promoted Arch Mage is assigned exactly one bronze spell, it surfaces in the
// ability snapshot, and it is castable end-to-end.
func TestArchMageBronzePool_AssignedSpellIsCastable(t *testing.T) {
	s := newProjectileTestState(t)
	s.mu.Lock()
	defer s.mu.Unlock()

	caster := spawnProjTestUnit(t, s, "p1", 100, 100)
	caster.ProgressionPath = "arch_mage"
	caster.Rank = "bronze"
	caster.Abilities = nil // start clean; recompute will build the list
	caster.AttackRange = 400
	caster.CurrentMana = 100
	caster.MaxMana = 100

	s.rollUnitPoolSpellsLocked(caster)
	s.assignUnitPathAbilitiesLocked(caster)

	pool := spellPoolFor("arch_mage", "bronze")
	assigned := ""
	for _, id := range pool {
		if containsStr(caster.Abilities, id) {
			if assigned != "" {
				t.Fatalf("more than one bronze spell assigned: %v", caster.Abilities)
			}
			assigned = id
		}
	}
	if assigned == "" {
		t.Fatalf("no bronze spell assigned; Abilities=%v", caster.Abilities)
	}

	// Surfaces in the ability snapshot with cooldown/autocast fields.
	found := false
	for _, snap := range s.abilityStatesLocked(caster) {
		if snap.ID == assigned {
			found = true
			if snap.DisplayName == "" {
				t.Errorf("assigned spell %q snapshot missing display name", assigned)
			}
		}
	}
	if !found {
		t.Errorf("assigned spell %q not in ability snapshot", assigned)
	}

	// Castable end-to-end. Point-targeted spells (arcane_orb) go through the
	// point-cast path; unit-targeted spells cast at a valid enemy.
	assignedDef, _ := getAbilityDef(assigned)
	if assignedDef.TargetsPoint {
		if ok, reason := s.beginAbilityCastAtPointLocked(caster, assigned, 250, 100); !ok {
			t.Errorf("assigned point spell %q not castable: %s", assigned, reason)
		}
	} else {
		enemy := spawnProjTestUnit(t, s, enemyPlayerID, 250, 100)
		enemy.MoveSpeed = 0
		if ok, reason := s.beginAbilityCastLocked(caster, assigned, enemy); !ok {
			t.Errorf("assigned spell %q not castable: %s", assigned, reason)
		}
	}
}
