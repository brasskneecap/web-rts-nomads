package game

// AbilityModifierSet is the composed (multiplied) scalar modifiers a caster's
// perks apply to ONE ability. All fields default to 1.0 (no-op).
type AbilityModifierSet struct {
	DamageMult   float64
	HealMult     float64
	ManaCostMult float64
	RangeMult    float64
	CooldownMult float64
}

func identityAbilityModifierSet() AbilityModifierSet {
	return AbilityModifierSet{
		DamageMult: 1, HealMult: 1, ManaCostMult: 1, RangeMult: 1, CooldownMult: 1,
	}
}

// abilityScalarModifiersForCasterLocked composes every owned perk's
// AbilityModifiers entry that targets abilityID, multiplicatively. A modifier
// field <= 0 is treated as unset (identity) — matching the "if m > 0 { mult
// *= m }" convention used by the bespoke Siphon Life aggregator this
// generalizes. Safe on a nil caster / empty abilityID (returns identity).
// Caller holds s.mu (read or write).
func (s *GameState) abilityScalarModifiersForCasterLocked(caster *Unit, abilityID string) AbilityModifierSet {
	set := identityAbilityModifierSet()
	if caster == nil || abilityID == "" {
		return set
	}
	for _, perkID := range caster.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		for i := range def.AbilityModifiers {
			m := def.AbilityModifiers[i]
			if m.Target != abilityID {
				continue
			}
			if m.DamageMult > 0 {
				set.DamageMult *= m.DamageMult
			}
			if m.HealMult > 0 {
				set.HealMult *= m.HealMult
			}
			if m.ManaCostMult > 0 {
				set.ManaCostMult *= m.ManaCostMult
			}
			if m.RangeMult > 0 {
				set.RangeMult *= m.RangeMult
			}
			if m.CooldownMult > 0 {
				set.CooldownMult *= m.CooldownMult
			}
		}
	}
	return set
}
