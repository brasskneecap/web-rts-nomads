package game

import "testing"

func TestAbilityProgramConstruct(t *testing.T) {
	p := AbilityProgram{
		Entry: AbilityEntryDef{Type: EntryUnit, Relations: []TargetRelation{RelSelf, RelAlly}, Range: CastRangeMatchAttackRange},
		Triggers: []AbilityTriggerDef{{
			ID:   "t_cast",
			Type: TriggerOnCastComplete,
			Actions: []AbilityActionDef{
				{ID: "a_heal", Type: ActionRestoreHealth},
			},
		}},
	}
	if p.Entry.Type != EntryUnit {
		t.Fatalf("entry type = %q", p.Entry.Type)
	}
	if got := p.Triggers[0].Actions[0].Type; got != ActionRestoreHealth {
		t.Fatalf("action type = %q", got)
	}
}
