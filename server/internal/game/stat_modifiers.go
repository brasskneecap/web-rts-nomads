package game

import (
	"fmt"
	"math"
	"sort"

	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// SHARED STAT-MODIFIER VOCABULARY
//
// This file defines the single, system-agnostic stat-modifier language that
// zone auras (and, in future, campaign modifiers, equipment, and global events)
// all speak. It is deliberately LAYERED OVER the existing per-stat read sites
// (effectiveArmorLocked, perkAttackSpeedBonusLocked, applyRankModifiersLocked,
// …) rather than replacing them: a contributor emits protocol.StatModifiers,
// those are aggregated per player into a PlayerStatModifierSet, and each
// existing read site folds in ONE extra (add, mul) term resolved from that set.
//
// Stacking rule, applied per stat at the read site:
//
//	effective = (base + Σ add) × Π multiply
//
// All functions here are pure or read player state; the resolver is called from
// hot-path read sites and must stay O(1).
// ═════════════════════════════════════════════════════════════════════════════

// Stat operation sentinels (mirror of the strings authored in map JSON).
const (
	statOpAdd      = "add"
	statOpMultiply = "multiply"
)

// Canonical stat identifiers. Adding a new stat is: (1) a const here, (2) an
// entry in statRegistry, (3) one read-site wire-up where the stat is consumed.
// Nothing in the aura code or the aggregation needs to change.
const (
	// Combat — these all have existing read sites.
	statHealthRegen = "healthRegen"
	statManaRegen   = "manaRegen"
	statMoveSpeed   = "moveSpeed"
	statAttackSpeed = "attackSpeed"
	statDamage      = "damage"
	statArmor       = "armor"
	statMaxHealth   = "maxHealth"
	statMaxMana     = "maxMana"

	// Economy / workers — these get NEW read sites (gather/production/construction).
	statGoldGatherRate            = "goldGatherRate"
	statWoodGatherRate            = "woodGatherRate"
	statGatherSpeed               = "gatherSpeed"
	statWorkerMoveSpeed           = "workerMoveSpeed"
	statUnitProductionSpeed       = "unitProductionSpeed"
	statBuildingConstructionSpeed = "buildingConstructionSpeed"
)

// statDef describes a registered stat: its id, the human label the editor and
// HUD show, and whether a multiply operation is meaningful for it. AllowMultiply
// is advisory metadata for the editor/UI today (both operations are accepted by
// validation); it documents intent and drives sensible editor defaults.
type statDef struct {
	ID            string
	Label         string
	AllowMultiply bool
}

// statRegistry is the single source of truth for which stats exist. Ordered for
// deterministic iteration and stable editor/UI lists. Keep combat then economy.
var statRegistry = []statDef{
	{statHealthRegen, "Health Regen", true},
	{statManaRegen, "Mana Regen", true},
	{statMoveSpeed, "Move Speed", true},
	{statAttackSpeed, "Attack Speed", true},
	{statDamage, "Damage", true},
	{statArmor, "Armor", true},
	{statMaxHealth, "Max Health", true},
	{statMaxMana, "Max Mana", true},
	{statGoldGatherRate, "Gold Gather Rate", true},
	{statWoodGatherRate, "Wood Gather Rate", true},
	{statGatherSpeed, "Gather Speed", true},
	{statWorkerMoveSpeed, "Worker Move Speed", true},
	{statUnitProductionSpeed, "Unit Production Speed", true},
	{statBuildingConstructionSpeed, "Building Construction Speed", true},
}

// statRegistryByID is the O(1) lookup index built once at init.
var statRegistryByID = func() map[string]statDef {
	m := make(map[string]statDef, len(statRegistry))
	for _, d := range statRegistry {
		m[d.ID] = d
	}
	return m
}()

// isKnownStat reports whether id names a registered stat.
func isKnownStat(id string) bool {
	_, ok := statRegistryByID[id]
	return ok
}

// statLabel returns the display label for a stat id, falling back to the raw id.
func statLabel(id string) string {
	if d, ok := statRegistryByID[id]; ok {
		return d.Label
	}
	return id
}

// ListStatIDs returns the registered stat ids in a stable sorted order. Used by
// the editor schema endpoint / TS mirror and for deterministic enumeration.
func ListStatIDs() []string {
	ids := make([]string, 0, len(statRegistry))
	for _, d := range statRegistry {
		ids = append(ids, d.ID)
	}
	sort.Strings(ids)
	return ids
}

// validateStatModifier checks a single modifier at catalog load. ctx is a
// human-readable location (e.g. "zone north_outpost aura 0") used in the panic
// message. Returns an error; callers at load time panic on it (catalogs are
// static, so a bad entry is a build error, mirroring the zone validators).
func validateStatModifier(ctx string, m protocol.StatModifier) error {
	if !isKnownStat(m.Stat) {
		return fmt.Errorf("%s: unknown stat %q", ctx, m.Stat)
	}
	if m.Operation != statOpAdd && m.Operation != statOpMultiply {
		return fmt.Errorf("%s: invalid operation %q (want %q or %q)", ctx, m.Operation, statOpAdd, statOpMultiply)
	}
	if math.IsNaN(m.Value) || math.IsInf(m.Value, 0) {
		return fmt.Errorf("%s: non-finite value", ctx)
	}
	if m.Operation == statOpMultiply && m.Value == 0 {
		return fmt.Errorf("%s: multiply value must be non-zero", ctx)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Aggregation
// ─────────────────────────────────────────────────────────────────────────────

// statAccum is the reduced contribution for a single stat: the summed additive
// total and the product of multiplicative factors. Identity is {Add: 0, Mul: 1}.
type statAccum struct {
	Add float64
	Mul float64
}

// PlayerStatModifierSet is a player's aggregated stat modifiers, keyed by stat
// id. Absent keys resolve to the identity (0, 1). Reduced from all of the
// player's active StatModifier sources (zone auras in v1) and rebuilt on change
// — see zone_auras.go. Stored on Player; server-only (never on the wire).
type PlayerStatModifierSet map[string]statAccum

// newPlayerStatModifierSet returns an empty, non-nil set.
func newPlayerStatModifierSet() PlayerStatModifierSet {
	return PlayerStatModifierSet{}
}

// fold applies one modifier into the set per the stacking rule: add → sum,
// multiply → product. The first multiply seeds from the identity 1.0.
func (set PlayerStatModifierSet) fold(m protocol.StatModifier) {
	acc, ok := set[m.Stat]
	if !ok {
		acc = statAccum{Add: 0, Mul: 1}
	}
	switch m.Operation {
	case statOpAdd:
		acc.Add += m.Value
	case statOpMultiply:
		acc.Mul *= m.Value
	}
	set[m.Stat] = acc
}

// resolve returns (add, mul) for a stat, or the identity (0, 1) when absent.
func (set PlayerStatModifierSet) resolve(stat string) (add, mul float64) {
	if set == nil {
		return 0, 1
	}
	acc, ok := set[stat]
	if !ok {
		return 0, 1
	}
	return acc.Add, acc.Mul
}

// applyStatModifier applies a resolved (add, mul) to a base value per the
// canonical rule: effective = (base + add) × mul. Convenience for read sites.
func applyStatModifier(base, add, mul float64) float64 {
	return (base + add) * mul
}

// playerStatModifierLocked resolves the (add, mul) a player's aggregated zone
// auras contribute to a stat. O(1). Returns the identity (0, 1) for an unknown
// player or a stat with no active modifier — so a read site that calls this
// when no auras are active behaves exactly as before this system existed.
//
// Must be called under s.mu (read or write) lock.
func (s *GameState) playerStatModifierLocked(playerID, stat string) (add, mul float64) {
	player, ok := s.Players[playerID]
	if !ok || player == nil {
		return 0, 1
	}
	return player.ZoneStatModifiers.resolve(stat)
}
