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
// Modeled directly on rebuildGuardianAuraCacheLocked (perks_auras.go) — same
// determinism discipline (slice-order iteration, commutative max()), same
// "rebuild once per tick, read many times" cache shape. Differs in one way:
// guardian_aura's cache is a single hand-written armor-specific struct;
// this cache is keyed by an arbitrary stat id so any perk's aura can plug in
// by authoring JSON, with no new Go type or fold-site plumbing beyond the one
// read call each stat's hook adds.
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
// Determinism: units are walked in s.Units slice order in both phases; each
// unit's OWNED perk ids are sorted before their Auras are read (mirrors
// unitPerkStatModifiersLocked's discipline — PerkIDs slice order must never
// drive a float result). The stacking rule uses max(), which is commutative,
// so no iteration order can affect the final aggregated value.
//
// KNOWN SCHEMA LIMITATION: PerkAura has no per-rank override mechanism
// (no ConfigByRank analog) — a StatModifiers Value is a single fixed number
// regardless of the emitting unit's Rank. zealous_march's catalog entry has
// no ConfigByRank overrides either, so this is behavior-neutral for the
// pilot; a future perk needing rank-scaled aura tuning would need a new
// field added to PerkAura first.
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
// rebuild; read-only for the rest of the rebuild.
type auraEmission struct {
	ownerID             string
	unitID              int
	x, y                float64
	radiusSq            float64
	targets             string // "allies" | "enemies"
	includeSelf         bool
	stat                string
	value               float64
	perAdditionalSource float64
}

// rebuildAuraStatCacheLocked rebuilds s.auraStatCache from scratch every
// tick using a two-phase algorithm:
//
// Phase 1 — Snapshot:
//
//	Walk s.Units in slice order. For each alive, visible unit, walk its OWNED
//	perk ids in perk-id-SORTED order (never PerkIDs slice order — see the
//	package doc above) and, for every PerkAura entry any owned perk defines,
//	emit one auraEmission per (aura, StatModifiers entry) pair. No writes to
//	auraStatCache in this phase.
//
// Phase 2 — Fan-out:
//
//	For each emission (slice order), walk s.Units again. A candidate unit is
//	an eligible recipient when it is alive + visible, and:
//	  - Targets == "allies": playersAreFriendlyLocked(emitter, candidate) is
//	    true, AND (candidate is not the emitter OR IncludeSelf is set).
//	  - Targets == "enemies": candidate is not the emitter (a unit is never
//	    its own enemy), AND playersAreHostileLocked(emitter, candidate).
//	Eligible candidates within radiusSq accumulate into a per-(recipient,
//	stat) working entry: track the strongest Value and the strongest
//	PerAdditionalSource seen independently, plus a source count. After all
//	emissions are folded, each working entry resolves to
//	effectiveValue = maxValue + (count-1) * maxPerAdditionalSource — this
//	exactly reproduces the legacy zealous_march formula
//	(bestBase + (count-1)*bestStack) for any "max"-stacking aura.
//
// Must be called under s.mu write lock. Called from Update(dt) alongside
// rebuildGuardianAuraCacheLocked, before combat/movement read the result.
func (s *GameState) rebuildAuraStatCacheLocked() {
	for k := range s.auraStatCache {
		delete(s.auraStatCache, k)
	}

	// Phase 1 — Snapshot every (source unit, aura, statModifier) emission.
	var emissions []auraEmission
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
			for _, aura := range def.Auras {
				if aura.Radius <= 0 {
					continue // defensive; validatePerkDef already enforces > 0
				}
				radiusSq := aura.Radius * aura.Radius
				for _, sm := range aura.StatModifiers {
					emissions = append(emissions, auraEmission{
						ownerID:             u.OwnerID,
						unitID:              u.ID,
						x:                   u.X,
						y:                   u.Y,
						radiusSq:            radiusSq,
						targets:             aura.Targets,
						includeSelf:         aura.IncludeSelf,
						stat:                sm.Stat,
						value:               sm.Value,
						perAdditionalSource: aura.PerAdditionalSource,
					})
				}
			}
		}
	}

	if len(emissions) == 0 {
		return
	}

	// Phase 2 — Fan-out. working accumulates the in-progress max-value /
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
