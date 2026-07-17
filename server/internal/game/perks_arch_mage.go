package game

import (
	"math"
	"sort"
)

// perks_arch_mage.go — Arch Mage (adept / arch_mage path) Gold perks.
//
// All three Gold perks hang off ONE trigger: an Arcane Missile landing a hit on
// an enemy. The trigger is wired in landProjectileLocked (projectile.go) for
// charge-fire passive bolts; this file owns the per-perk reactions. Each perk is
// independent — a unit only ever owns one Gold perk, so none may depend on
// another Gold perk existing.
//
//   arcane_feedback — restore mana to the caster on each Arcane Missile hit.
//   arcane_conduit  — let Arcane Missiles trigger the caster's item on-hit effects.
//   unstable_magic  — chance to cast a random learned spell at reduced effectiveness.
//
// These enhance the existing Arch Mage loop: cast spells → spend mana → build
// Arcane Charge → fire Arcane Missiles → Gold perk fires here.

// onArcaneMissileHitLocked dispatches the Arch Mage Gold perks when one of
// `caster`'s Arcane Missiles hits `target`. Called from landProjectileLocked at
// the moment the missile connects (target is guaranteed alive + visible there),
// so every dispatch is a successful hit. Caller holds s.mu.
func (s *GameState) onArcaneMissileHitLocked(caster, target *Unit) {
	if caster == nil || target == nil || len(caster.PerkIDs) == 0 {
		return
	}
	for _, perkID := range caster.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {

		case "arcane_feedback":
			// Restore a flat amount of mana per hit through the shared restore
			// entry point (clamps to MaxMana, emits the "+N" popup, no-ops on a
			// full/dead caster). This mana can itself feed the Arcane Charge loop
			// on the caster's next spell, tightening the core gameplay loop.
			s.addUnitManaLocked(caster, int(def.Config["manaPerHit"]))

		case "arcane_conduit":
			// Let the missile participate in the caster's item on-hit effect
			// pipeline (elemental + procs) — the same helper cleave/whirlwind
			// secondary hits use. It replays equipment effects WITHOUT re-entering
			// the attack hub, so any proc bolts stay non-recursive. Fully generic:
			// any future on-hit item works with no Arch-Mage-specific code.
			s.applyEquipmentOnHitEffectsLocked(caster, target)

		case "unstable_magic":
			// Chance (rolled on the seeded perk RNG for determinism) to unleash
			// one of the caster's learned spells at reduced effectiveness.
			if s.rngPerks.Float64() < def.Config["procChance"] {
				s.fireUnstableMagicLocked(caster, target, def.Config["effectiveness"])
			}
		}
	}
}

// fireUnstableMagicLocked casts one of the caster's currently learned pool
// spells (the bronze/silver picks recorded on PoolSpellsByRank) at `target`,
// scaled to `effectiveness` of its normal damage output. It reuses the real
// cast-resolution path rather than duplicating spell logic: the effective spell
// is built from the catalog, its mana cost is zeroed (this is a free proc, not
// the caster spending an action), its damage-bearing fields are scaled, then it
// is handed to the same resolver a normal cast uses. No cooldown or global
// cooldown is armed, so the proc never interferes with the caster's own casting.
//
// Reduced-effectiveness scaling is applied as a modifier on the EffectiveSpell,
// not by authoring separate mini-spell definitions. Caller holds s.mu.
func (s *GameState) fireUnstableMagicLocked(caster, target *Unit, effectiveness float64) {
	if caster == nil || target == nil || effectiveness <= 0 {
		return
	}
	spellID := s.randomLearnedSpellLocked(caster)
	if spellID == "" {
		return // no learned spells yet — nothing to unleash
	}
	def, ok := getAbilityDef(spellID)
	if !ok {
		return
	}
	eff := s.effectiveSpellLocked(caster, def)
	eff.ManaCost = 0 // free proc: never spends mana or builds Arcane Charge
	eff = scaleEffectiveSpellDamage(eff, effectiveness)
	if def.TargetsPoint {
		s.resolveAbilityCastAtPointLocked(caster, def, eff, target.X, target.Y)
	} else {
		// Route through the shared unit-targeted seam (branches on
		// SchemaVersion exactly like resolveAbilityCastAtPointLocked does
		// above) instead of reaching past it into the legacy-only
		// resolveAbilityCastOnTargetLocked — see resolveAbilityCastWithEffLocked's
		// doc comment for why that direct call was a silent no-op bug for a
		// v2 unit-targeted spell.
		s.resolveAbilityCastWithEffLocked(caster, def, eff, []*Unit{target})
	}
}

// randomLearnedSpellLocked returns one of the caster's learned pool spells
// (PoolSpellsByRank values) chosen uniformly from the seeded perk RNG, or ""
// when the caster has learned none. Candidates are sorted so neither map nor
// pool iteration order can drive the outcome (determinism invariant). Caller
// holds s.mu.
func (s *GameState) randomLearnedSpellLocked(caster *Unit) string {
	if caster == nil || len(caster.PoolSpellsByRank) == 0 {
		return ""
	}
	candidates := make([]string, 0, len(caster.PoolSpellsByRank))
	for _, id := range caster.PoolSpellsByRank {
		candidates = append(candidates, id)
	}
	sort.Strings(candidates)
	return candidates[int(s.rngPerks.Float64()*float64(len(candidates)))]
}

// scaleEffectiveSpellDamage returns eff with its damage-bearing magnitudes
// multiplied by factor (Unstable Magic's reduced-effectiveness cast). Only the
// direct/AoE/DoT damage fields are scaled; spatial fields (radius, duration,
// chain, pull) are left intact so the spell still behaves like itself, just
// weaker. Pure — no lock, no RNG. Values are floored at 0 by construction
// (factor is validated > 0 by the caller and the base fields are non-negative).
//
// Also stamps DamageEffectivenessMultiplier so a composable (SchemaVersion>=2)
// spell resolved through this reduced-effectiveness eff scales its
// deal_damage actions identically to how .Damage/.DamagePerSecond already
// scale the legacy path — see resolveAbilityProgramCastLocked, which honours
// the caller-supplied eff instead of re-deriving one from scratch.
func scaleEffectiveSpellDamage(eff EffectiveSpell, factor float64) EffectiveSpell {
	eff.Damage = int(math.Round(float64(eff.Damage) * factor))
	eff.DamagePerSecond *= factor
	eff.DamageEffectivenessMultiplier = eff.effectivenessMultiplier() * factor
	return eff
}
