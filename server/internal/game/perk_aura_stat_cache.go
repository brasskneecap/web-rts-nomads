package game

import "sort"

// ═════════════════════════════════════════════════════════════════════════════
// GENERIC PERK AURA STAT CACHE
//
// PerkDef.Auras (perk_defs.go) is the typed, validated, registry-backed aura
// vocabulary that lets a designer author "emit these stat changes to nearby
// allies/enemies" with zero Go — the aura sibling of PerkStatModifier, which
// only affects the OWNING unit. This file is the runtime engine: it rebuilds
// a per-tick cache mapping (recipient unit, stat) → the aggregated
// contribution every covering aura emitter grants that stat, and exposes a
// single O(1) reader (unitAuraStatContributionLocked) for each stat's own
// fold site to consume.
//
// Pilots: zealous_march, mana_conduit, sanctuary (single-emitter, no
// synergy). guardian_aura (perks_defense.go's effectiveArmorLocked) is the
// first to use the EMITTER-side companion-synergy phase (Phase 2 below,
// SynergyRadiusPerCompanion / PerkStatModifier.PerCompanion) and the
// "sameOwner" Targets scope — it replaced its own hand-written cache
// (formerly guardianAuraCache / rebuildGuardianAuraCacheLocked in
// perks_auras.go, deleted) with this generic engine. Same determinism
// discipline throughout (slice-order iteration, commutative max()), same
// "rebuild once per tick, read many times" cache shape guardian_aura's
// bespoke cache pioneered.
//
// ⚠️ ORDERING TRAP — read this before wiring a new stat's fold site to this
// cache. unitAuraStatContributionLocked hands back a raw (value, sources)
// pair; it does NOT decide WHERE in a stat's arithmetic that value lands.
// Each fold site must place it at the SAME position the stat's pre-migration
// bespoke code occupied. For zealous_march (perks_movement.go), the legacy
// helper (perkMoveSpeedBonusFromClericAurasLocked, now deleted) added its
// result straight into the additive "bonus" pool BEFORE
// perkMult = 1.0 + bonus is computed. Folding it instead through the generic
// (base + add) × mul zone/stat-modifier pipeline would silently change how it
// composes with momentum's bonus (same additive pool) — see
// perk_aura_migration_test.go's ordering-guard test, and
// TestHawkSpirit_WithZoneAura_Damage (perk_stat_migration_test.go) for the
// identical trap hit during the damage-stat migration.
//
// Determinism: units are walked in s.Units slice order in every phase; each
// unit's OWNED perk ids are sorted before their Auras are read (mirrors
// unitPerkStatModifiersLocked's discipline — PerkIDs slice order must never
// drive a float result); aura index order (a fixed authored JSON array
// order) drives per-unit aura iteration. The companion-counting scan
// (Phase 2) walks the same deterministically-built instances slice twice
// with no map involved. The stacking rule uses max(), which is commutative,
// so no iteration order can affect the final aggregated value.
//
// KNOWN SCHEMA LIMITATION: PerkAura has no per-rank override mechanism
// (no ConfigByRank analog) — a StatModifiers Value (and
// SynergyRadiusPerCompanion / PerCompanion) is a single fixed number
// regardless of the emitting unit's Rank. zealous_march's and
// guardian_aura's catalog entries carry no ConfigByRank overrides either, so
// this is behavior-neutral for every aura shipped today; a future perk
// needing rank-scaled aura tuning would need a new field added to PerkAura
// first.
// ═════════════════════════════════════════════════════════════════════════════

// auraStatContribution is the resolved contribution ONE stat receives from
// every aura covering a recipient this tick. Sources is exposed so HUD code
// can answer "is any aura covering me" without re-scanning (see
// hasZealousMarchAuraLocked, perks_cleric.go).
type auraStatContribution struct {
	Value   float64
	Sources int
}

// auraStatCacheKey is the flat composite key auraStatCache is keyed by. Only
// ever Get/Set/Delete'd by unit ID + stat id — never iterated for an
// outcome — so map iteration order cannot affect any float result (same
// discipline as objectiveUnreachableUntil).
type auraStatCacheKey struct {
	UnitID int
	Stat   string
}

// auraEmission is a Phase-1 snapshot entry: one PerkAura's StatModifiers
// entry for one stat, from one source (emitting) unit. Built once per
// rebuild; read-only for the rest of the rebuild. radiusSq and value are the
// EFFECTIVE (post-companion-synergy) numbers — see auraInstance / the
// companion-counting sub-phase below — so Phase 3 (fan-out) never needs to
// know whether synergy was involved.
type auraEmission struct {
	ownerID             string
	unitID              int
	x, y                float64
	radiusSq            float64
	targets             string // "allies" | "enemies" | "sameOwner"
	includeSelf         bool
	stat                string
	value               float64
	perAdditionalSource float64
}

// auraInstance is a Phase-1 snapshot entry: one PerkAura owned by one source
// (emitting) unit — the unit-level granularity companion-counting operates
// at, one level up from auraEmission (which is per StatModifiers entry).
// perkID + auraIdx together identify "the same aura" for companion-counting
// purposes: two different units owning the SAME perk's SAME aura entry are
// companions of each other; a unit owning a DIFFERENT aura (even on the same
// perk, or an aura with the same Radius by coincidence) is never a
// companion. Built once per rebuild; read-only for the rest of the rebuild.
type auraInstance struct {
	ownerID string
	unitID  int
	x, y    float64
	perkID  string
	auraIdx int
	aura    *PerkAura // points into the perk registry's immutable PerkDef.Auras slice
}

// auraDeclaresSynergy reports whether `aura` opts into the EMITTER-side
// companion-synergy phase at all (SynergyRadiusPerCompanion on the aura, or
// PerCompanion on any of its StatModifiers). Every aura migrated before
// guardian_aura (zealous_march, mana_conduit, sanctuary) declares none of
// these — this predicate lets rebuildAuraStatCacheLocked skip the O(n²)
// companion scan entirely for them, so guardian_aura's addition costs
// nothing for the auras that don't use it.
func auraDeclaresSynergy(aura *PerkAura) bool {
	if aura.SynergyRadiusPerCompanion != 0 {
		return true
	}
	for _, sm := range aura.StatModifiers {
		if sm.PerCompanion != 0 {
			return true
		}
	}
	return false
}

// rebuildAuraStatCacheLocked rebuilds s.auraStatCache from scratch every
// tick using a three-phase algorithm:
//
// Phase 1 — Snapshot:
//
//	Walk s.Units in slice order. For each alive, visible unit, walk its OWNED
//	perk ids in perk-id-SORTED order (never PerkIDs slice order — see the
//	package doc above) and, for every PerkAura entry any owned perk defines
//	(aura index order), record one auraInstance. No writes to auraStatCache
//	in this phase.
//
// Phase 2 — Companion synergy (EMITTER-side, opt-in):
//
//	For each instance whose aura declares synergy (auraDeclaresSynergy), count
//	OTHER instances of the SAME aura (same perkID + auraIdx) with the SAME
//	ownerID whose position is within THIS instance's BASE Radius (dist² ≤
//	Radius², never the companion-inflated effective radius — this is the
//	critical rule that prevents recursive radius inflation, mirroring
//	guardian_aura's pre-migration algorithm exactly). The resulting count
//	scales this instance's effective radius (Radius +
//	count×SynergyRadiusPerCompanion) and each of its StatModifiers' effective
//	value (Value + count×PerCompanion). Instances whose aura does not declare
//	synergy skip this scan outright: effective radius/value equal the raw
//	Radius/Value (companions implicitly 0), byte-identical to pre-guardian_aura
//	behavior. Emissions (one per StatModifiers entry) are built from these
//	effective numbers.
//
// Phase 3 — Fan-out:
//
//	For each emission (slice order), walk s.Units again. A candidate unit is
//	an eligible recipient when it is alive + visible, and:
//	  - Targets == "allies": playersAreFriendlyLocked(emitter, candidate) is
//	    true (same TEAM — may span multiple owners on a shared team), AND
//	    (candidate is not the emitter OR IncludeSelf is set).
//	  - Targets == "sameOwner": candidate.OwnerID == emitter.OwnerID EXACTLY
//	    (strictly narrower than "allies" — never crosses owners even on the
//	    same team), AND (candidate is not the emitter OR IncludeSelf is set).
//	  - Targets == "enemies": candidate is not the emitter (a unit is never
//	    its own enemy), AND playersAreHostileLocked(emitter, candidate).
//	Eligible candidates within radiusSq accumulate into a per-(recipient,
//	stat) working entry: track the strongest Value and the strongest
//	PerAdditionalSource seen independently, plus a source count. After all
//	emissions are folded, each working entry resolves to
//	effectiveValue = maxValue + (count-1) * maxPerAdditionalSource — this
//	exactly reproduces the legacy zealous_march formula
//	(bestBase + (count-1)*bestStack) for any "max"-stacking aura, and
//	collapses to a strict per-dimension max (guardian_aura's legacy rule)
//	when PerAdditionalSource is 0, as guardian_aura's catalog entry sets it.
//
// Determinism: Phase 1/2 walk s.Units in slice order (outer) and perk-id-
// sorted / aura-index order (inner) for both instances and emissions; the
// companion scan in Phase 2 walks the same deterministically-built
// `instances` slice twice (i, j) with no map involved. Phase 3's per-
// (recipient, stat) accumulation uses only max() (commutative) and a plain
// running sum for count, so no iteration order can affect the final
// aggregated float value — same guarantee the legacy guardianAuraCache
// algorithm made, now generalized.
//
// Must be called under s.mu write lock. Called from Update(dt) before
// combat/movement read the result.
func (s *GameState) rebuildAuraStatCacheLocked() {
	for k := range s.auraStatCache {
		delete(s.auraStatCache, k)
	}

	// Phase 1 — Snapshot every (source unit, aura) instance.
	var instances []auraInstance
	for _, u := range s.Units {
		if u == nil || u.HP <= 0 || !u.Visible || len(u.PerkIDs) == 0 {
			continue
		}
		ids := append([]string(nil), u.PerkIDs...)
		sort.Strings(ids)
		for _, perkID := range ids {
			def := perkDefByID(perkID)
			if def == nil || len(def.Auras) == 0 {
				continue
			}
			for auraIdx := range def.Auras {
				aura := &def.Auras[auraIdx]
				if aura.Radius <= 0 {
					continue // defensive; validatePerkDef already enforces > 0
				}
				instances = append(instances, auraInstance{
					ownerID: u.OwnerID,
					unitID:  u.ID,
					x:       u.X,
					y:       u.Y,
					perkID:  perkID,
					auraIdx: auraIdx,
					aura:    aura,
				})
			}
		}
	}

	if len(instances) == 0 {
		return
	}

	// Phase 2 — Companion synergy (opt-in) + emission build. For instances
	// whose aura does not declare synergy, companions is implicitly 0 and
	// effRadius/effValue equal the raw Radius/Value — no scan performed.
	var emissions []auraEmission
	for i := range instances {
		inst := &instances[i]
		aura := inst.aura

		companions := 0
		if auraDeclaresSynergy(aura) {
			baseRSq := aura.Radius * aura.Radius
			for j := range instances {
				if j == i {
					continue
				}
				other := &instances[j]
				if other.perkID != inst.perkID || other.auraIdx != inst.auraIdx {
					continue // not the same aura — not a companion
				}
				if other.ownerID != inst.ownerID {
					continue
				}
				dx := other.x - inst.x
				dy := other.y - inst.y
				if dx*dx+dy*dy <= baseRSq {
					companions++
				}
			}
		}

		effRadius := aura.Radius + float64(companions)*aura.SynergyRadiusPerCompanion
		radiusSq := effRadius * effRadius
		for _, sm := range aura.StatModifiers {
			effValue := sm.Value + float64(companions)*sm.PerCompanion
			emissions = append(emissions, auraEmission{
				ownerID:             inst.ownerID,
				unitID:              inst.unitID,
				x:                   inst.x,
				y:                   inst.y,
				radiusSq:            radiusSq,
				targets:             aura.Targets,
				includeSelf:         aura.IncludeSelf,
				stat:                sm.Stat,
				value:               effValue,
				perAdditionalSource: aura.PerAdditionalSource,
			})
		}
	}

	if len(emissions) == 0 {
		return
	}

	// Phase 3 — Fan-out. working accumulates the in-progress max-value /
	// max-perAdditionalSource / source-count triple for each (recipient,
	// stat) pair across every emission that covers it.
	type accum struct {
		maxValue    float64
		maxPerExtra float64
		count       int
	}
	working := make(map[auraStatCacheKey]*accum)

	for _, e := range emissions {
		for _, c := range s.Units {
			if c == nil || c.HP <= 0 || !c.Visible {
				continue
			}
			isSelf := c.ID == e.unitID
			switch e.targets {
			case "allies":
				if !s.playersAreFriendlyLocked(e.ownerID, c.OwnerID) {
					continue
				}
				if isSelf && !e.includeSelf {
					continue
				}
			case "sameOwner":
				// Strictly narrower than "allies": same OWNER, not merely
				// same team. See PerkAura.Targets' doc comment for why this
				// exists (guardian_aura's legacy algorithm compared OwnerID
				// directly, never playersAreFriendlyLocked).
				if c.OwnerID != e.ownerID {
					continue
				}
				if isSelf && !e.includeSelf {
					continue
				}
			case "enemies":
				if isSelf {
					continue // a unit is never its own enemy
				}
				if !s.playersAreHostileLocked(e.ownerID, c.OwnerID) {
					continue
				}
			default:
				continue // unknown Targets; validatePerkDef should have caught this
			}
			dx := c.X - e.x
			dy := c.Y - e.y
			if dx*dx+dy*dy > e.radiusSq {
				continue
			}
			key := auraStatCacheKey{UnitID: c.ID, Stat: e.stat}
			a, ok := working[key]
			if !ok {
				a = &accum{}
				working[key] = a
			}
			a.count++
			if e.value > a.maxValue {
				a.maxValue = e.value
			}
			if e.perAdditionalSource > a.maxPerExtra {
				a.maxPerExtra = e.perAdditionalSource
			}
		}
	}

	for key, a := range working {
		if a.count == 0 {
			continue
		}
		s.auraStatCache[key] = auraStatContribution{
			Value:   a.maxValue + float64(a.count-1)*a.maxPerExtra,
			Sources: a.count,
		}
	}
}

// unitAuraStatContributionLocked returns the aggregated aura contribution to
// `stat` covering `unit` this tick, and the number of distinct aura sources
// covering it. (0, 0) means no covering aura for that stat this tick. Safe
// to call with a nil unit.
//
// Reads the cache built by rebuildAuraStatCacheLocked this tick — does NOT
// itself scan s.Units, so it's O(1) at every call site. Caller holds s.mu
// (read or write).
func (s *GameState) unitAuraStatContributionLocked(unit *Unit, stat string) (value float64, sources int) {
	if unit == nil {
		return 0, 0
	}
	c := s.auraStatCache[auraStatCacheKey{UnitID: unit.ID, Stat: stat}]
	return c.Value, c.Sources
}
