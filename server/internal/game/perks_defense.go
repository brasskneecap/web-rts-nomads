package game

import "math"

// ═════════════════════════════════════════════════════════════════════════════
// SHIELD / HEAL / BUFF HELPERS
//
// These helpers centralize the unit-side state transitions that perks drive.
// Damage intake, heal application, and the list of "active buffs" advertised
// to the client all live here so the integration points from state.go and
// perks.go are one-liners.
//
// EXTENSION POINTS:
//   • applyUnitDamageLocked    — add new damage-intake reducers (armor-
//                                 like, reflective, etc.) before or after
//                                 the shield pool.
//   • healUnitLocked           — add new overheal routings (e.g. future
//                                 gold perks that convert overheal into
//                                 something other than shield).
//   • unitMaxShieldLocked      — aggregate max-shield from multiple perks
//                                 here if future perks also contribute.
//   • activeBuffIconsLocked    — return extra buff icon ids when new timed
//                                 or conditional states are added. Each id
//                                 must match an entry in action-icons.json.
//   • activeDebuffIconsLocked  — return extra debuff icon ids (raw icon ids,
//                                 not perk ids) for new negative status effects.
// ═════════════════════════════════════════════════════════════════════════════

// applyUnitDamageWithSourceLocked is the canonical damage entry point.
// It runs the full damage pipeline (redirect → mark amplification → flat
// reduction → shield → HP) AND, if the target hits HP<=0, enqueues a
// pendingDeath with full kill attribution. Drained at end of tick by
// drainPendingDeathsLocked.
//
// Pass DamageSource{} (anonymous) only from legacy call sites that do their
// own kill bookkeeping — the drain will then handle removal only, not XP
// credit, and the existing manual bookkeeping at those call sites is preserved.
//
// Returns the damage that landed on HP (after all mitigation).
//
// Damage intake order:
//  1. Caller computes post-armor damage (applyArmorMitigation).
//  2. pain_share redirect — nearby Vanguard absorbs a portion; src propagated.
//  3. Challenger's Mark amplification.
//  4. perkFlatDamageReductionLocked (reinforced_armor).
//  5. Shield pool.
//  6. HP.
//  7. enqueueDeathLocked if HP <= 0.
func (s *GameState) applyUnitDamageWithSourceLocked(target *Unit, damage int, src DamageSource) int {
	if target == nil || damage <= 0 {
		return 0
	}
	// Step 1: Divine Intervention invulnerability window (gold cleric). When
	// a recently-saved unit has InvulnerabilityRemaining > 0, the entire hit
	// is ignored — no mitigation, no shared pain, no shield drain, no mark
	// consumed. This is true invulnerability rather than a damage-instance
	// absorb (which is Divine Aegis below), because the design intent is
	// that a freshly-revived unit gets a brief moment of safety to escape
	// the burst that just killed them. Checking BEFORE pain_share so an
	// invuln unit's redirect target also takes nothing.
	if s.consumeInvulnerabilityLocked(target) {
		return 0
	}
	// Preserve the pre-mitigation input for Shared Pain redistribution.
	origDamage := damage
	// Step 2: pain_share redirect — propagate attribution so if the absorbing
	// Vanguard dies, the kill credits the original attacker.
	damage = s.perkRedirectIncomingDamageLocked(target, damage, src)
	if damage == 0 {
		// Even at 0 landed damage, the intended hit should still fan out via
		// Shared Pain — the attack "hit" the marked enemy, it just got fully
		// redirected. Keep the semantic consistent with the other early-exit
		// path below.
		s.perkShareDamageToMarkedLocked(target, origDamage, src)
		return 0
	}
	// Step 2b: Divine Aegis (silver cleric) — if the target currently holds an
	// unconsumed protection charge, the entire damage instance is negated and
	// the charge is consumed. The consume helper clears the field in-place so
	// no on-damage perk can re-trigger the same charge during this call stack
	// (the design constraint forbids recursive prevention). Shared Pain still
	// fires on the pre-mitigation amount so the attack's "hit" semantics are
	// preserved for downstream marked-enemy fan-out — consistent with the
	// pain_share / shield-full-absorb branches above and below.
	if s.consumeDivineAegisLocked(target) {
		s.perkShareDamageToMarkedLocked(target, origDamage, src)
		return 0
	}
	// Step 3: Mark amplification.
	if totalMult := target.PerkState.totalMarkMultiplier(); totalMult > 0 {
		damage = maxInt(damage, int(math.Round(float64(damage)*(1.0+totalMult))))
	}
	// Step 3b: Sanctuary aura mitigation (projectile-only). max-wins, no-stack.
	// Applied after mark amplification and before flat reduction so sanctuary
	// reduces on top of any mark bonus — consistent with the design intent that
	// sanctuary protects its zone from incoming fire.
	if sanctuaryMult := s.perkRangedDamageMultiplierFromAurasLocked(target, src); sanctuaryMult < 1.0 {
		damage = maxInt(0, int(math.Round(float64(damage)*sanctuaryMult)))
		if damage == 0 {
			s.perkShareDamageToMarkedLocked(target, origDamage, src)
			return 0
		}
	}
	// Step 4: Flat per-hit reduction.
	if reduction := s.perkFlatDamageReductionLocked(target); reduction > 0 {
		damage = maxInt(0, damage-reduction)
		if damage == 0 {
			return 0
		}
	}
	// Step 4b: Amplify Damage (Siphoner silver) — flat damage-taken multiplier
	// applied to every incoming damage instance while the affliction is
	// active. Applied AFTER flat reduction (so the multiplier scales whatever
	// survived armor / reinforced_armor / sanctuary) and BEFORE shield
	// consumption (so amplified damage drains shields faster too). Composes
	// multiplicatively with the mark amplification above — the two systems
	// are intentionally independent so a victim carrying both gets the
	// combined effect.
	if mult := amplifyDamageTakenMultiplierLocked(target); mult > 0 {
		damage = maxInt(0, int(math.Round(float64(damage)*(1.0+mult))))
		if damage == 0 {
			s.perkShareDamageToMarkedLocked(target, origDamage, src)
			return 0
		}
	}
	// Steps 5 & 6: Source-specific shield pools, then legacy shield, then HP.
	// Pools drain oldest-first (slice order) so the consumption order is
	// predictable for debugging. Each pool consumes up to its CurrentValue;
	// the remainder cascades to the next pool, then to the legacy single-
	// pool Unit.Shield (blood_engine), then to HP.
	damage = s.drainShieldPoolsLocked(target, damage)
	if damage == 0 {
		s.perkShareDamageToMarkedLocked(target, origDamage, src)
		return 0
	}
	if target.Shield > 0 {
		if target.Shield >= damage {
			target.Shield -= damage
			// Shared Pain fires even when the shield fully absorbed the hit.
			s.perkShareDamageToMarkedLocked(target, origDamage, src)
			return 0
		}
		damage -= target.Shield
		target.Shield = 0
	}
	prevHP := target.HP
	target.HP -= damage
	// Clamp to 0 so HP is never stored as negative.
	if target.HP < 0 {
		target.HP = 0
	}
	// Damage-type color hint: tag the major (floating-up) popup the client
	// will derive from this HP-diff with the damage type's color. Emitted
	// ONLY at this path (HP actually loses health) — full-mitigation /
	// shield-only-absorb / invuln / aegis paths above already returned
	// without recording, which is correct because no popup will appear
	// for them. damageTypeColorVariant returns "" for damage types we
	// don't paint (physical / arcane today), in which case
	// recordDamageTypeHintLocked no-ops and the popup keeps the default
	// white/red. Callers that render their own separate popup for this
	// instance set SuppressTypeHint so the main number isn't mis-tinted.
	if !src.SuppressTypeHint {
		s.recordDamageTypeHintLocked(target, damage, damageTypeColorVariant(src.DamageType))
	}
	// Overkill: damage exceeded what was on HP. The client derives its floating
	// damage numbers from HP-diffs, which clamp to prevHP for the killing blow.
	// Record the pre-clamp value so the client can show the real damage instead
	// of the capped "5 / 5" amount. Exact kills (damage == prevHP) are skipped —
	// the client's HP-delta is already correct in that case.
	if prevHP > 0 && damage > prevHP {
		s.recordLethalDamageLocked(target, damage)
	}
	// Shared Pain: redistribute a fraction of the pre-mitigation damage to
	// other marked enemies. Propagate attribution so indirect kills credit the
	// original attacker.
	s.perkShareDamageToMarkedLocked(target, origDamage, src)
	// Step 6b: Divine Intervention (gold cleric). When the unit's HP would
	// hit 0, scan nearby allied clerics for one off cooldown that can revive
	// the target. A successful save restores HP and stamps an invulnerability
	// window, then we return WITHOUT enqueueing the death. The damage value
	// is still reported (so the floating "-N" reads correctly) — only the
	// death is averted.
	if target.HP <= 0 && s.tryDivineInterventionLocked(target) {
		return damage
	}
	// Step 7: Enqueue death so drainPendingDeathsLocked handles cleanup and XP.
	s.enqueueDeathLocked(target, src)
	return damage
}

// applyUnitDamageLocked is the legacy wrapper around applyUnitDamageWithSourceLocked
// with an anonymous DamageSource. Call sites that have not been migrated to pass
// attribution should use this; the drain still catches HP=0 units they miss
// (defensive safety net) but does not award XP for them — those call sites
// continue to do their own kill bookkeeping as before.
//
// Damage intake order:
//   1. Caller computes post-armor damage (applyArmorMitigation). Armor
//      mitigation accounts for all flat and percent armor bonuses from perks
//      (last_stand, interlock, brace, guardian_aura, banners)
//      via effectiveArmorLocked. This means armor already reduces damage before
//      we enter this function.
//   2. pain_share redirect — nearby allied Vanguard absorbs a portion of raw damage.
//   3. Challenger's Mark amplification — amplifies after armor reduction and
//      after the redirect, so the mark bonus applies to whatever survives both
//      of those stages. NOTE: mark is therefore relatively stronger against
//      already-armored targets than it was under the old percentage-DR system.
//      This is intentional — see design approval in commit history.
//   4. perkFlatDamageReductionLocked (reinforced_armor) — per-hit flat reduction.
//   5. Shield pool absorbs what remains.
//   6. HP takes what the shield didn't absorb.
//
// Called from every unit-damage intake site:
//   - state.go primary attack
//   - state.go building-on-unit attack
//   - perks.go savage_strikes bonus hit
//   - perks.go applyCleaveHitLocked
//   - perks.go applyWhirlwindHitLocked
//
// A damage intake that bypasses this helper will bypass flat reduction and
// shield — avoid it.
func (s *GameState) applyUnitDamageLocked(target *Unit, damage int) int {
	return s.applyUnitDamageWithSourceLocked(target, damage, DamageSource{})
}

// healUnitLocked adds `amount` HP to a unit, clamped to MaxHP. If the unit has
// blood_engine (gold berserker), any excess beyond MaxHP becomes shield up to
// the perk's configured cap. Safe to call with non-positive amounts.
//
// ADD NEW OVERHEAL ROUTINGS HERE (e.g. future perks that convert overheal
// into something other than shield).
func (s *GameState) healUnitLocked(unit *Unit, amount int) {
	if unit == nil || amount <= 0 || unit.HP <= 0 {
		return
	}
	// Mark of Weakness (Siphoner bronze) scales every incoming heal applied
	// to this unit. The check is cheap (one PerkState read) and a no-op when
	// the debuff isn't active. Applied here — at the canonical heal entry —
	// so EVERY heal path (passive regen, blood_sustain, applyClericHeal-
	// Locked overflow, future sources) is consistently throttled without
	// requiring each call site to remember.
	if mult := markOfWeaknessHealingReceivedMultiplierLocked(unit); mult < 1.0 {
		amount = int(math.Round(float64(amount) * mult))
		if amount <= 0 {
			return
		}
	}
	missing := unit.MaxHP - unit.HP
	if amount <= missing {
		unit.HP += amount
		return
	}
	unit.HP = unit.MaxHP
	overheal := amount - missing
	maxShield := s.unitMaxShieldLocked(unit)
	if maxShield <= 0 || overheal <= 0 {
		return
	}
	unit.Shield = minInt(maxShield, unit.Shield+overheal)
}

// unitMaxShieldLocked returns the unit's current shield capacity from the
// LEGACY single-pool path (Unit.Shield). Today only blood_engine contributes
// here; source-specific shield perks (dark_renewal, future cleric barrier
// perks) carry their own per-source caps on UnitPerkState.ShieldPools and
// surface them via totalMaxShieldFromPoolsLocked instead.
//
// Use unitTotalMaxShieldLocked when you need the aggregate (legacy + pools)
// for HUD / snapshot purposes.
func (s *GameState) unitMaxShieldLocked(unit *Unit) int {
	if unit == nil || len(unit.PerkIDs) == 0 {
		return 0
	}
	total := 0
	for _, perkID := range unit.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {
		case "blood_engine":
			total += int(def.Config["maxShield"])
		// ── add cases for new shield-granting perks below this line ─────────
		}
	}
	return total
}

// ─────────────────────────────────────────────────────────────────────────────
// Source-specific shield pools
//
// A unit may carry multiple independent shield pools at once (e.g.
// dark_renewal + a future cleric barrier). The damage pipeline drains pools
// oldest-first (slice order) so the consumption order is predictable and
// easy to debug.
//
// Stacking modes:
//   - ShieldStackingPerSource (default): pools are keyed by
//     (SourceType, SourceUnitID). Two granting units of the same perk give
//     the recipient TWO independent pools, each respecting its own cap.
//     Example use case: a future "shield stamp" attack where multiple
//     stamps from different units should stack.
//   - ShieldStackingShared: pools are keyed by SourceType ALONE. All
//     granting units of the same perk feed ONE shared pool that respects a
//     single cap across them all. This is the "buff doesn't double-up"
//     model — e.g. dark_renewal: two Siphoners shielding the same ally
//     still cap the ally's dark_renewal shield at maxSelfShield, they just
//     fill it faster between them.
//
// Register a new shield source's stacking mode in shieldStackingModes
// below; unregistered source types default to PerSource (the zero value).
//
// Design notes:
//   - Pools live on UnitPerkState.ShieldPools so they ride along with the
//     rest of per-unit perk state and decay/cleanup all happen through the
//     same machinery (no new lifecycle to manage).
//   - Pools do NOT decay by default; dark_renewal pools persist until
//     depleted. Future pools with timed expiration can add an expiry field
//     here and decay alongside the other cross-unit timers in state.go.
//   - applyShieldFromSourceLocked returns the amount actually banked so
//     callers can attribute waste (e.g. dark_renewal overflow → ally).
// ─────────────────────────────────────────────────────────────────────────────

// ShieldStacking declares how multiple granting units of the same shield
// SourceType combine on one recipient.
type ShieldStacking int

const (
	// ShieldStackingPerSource (zero value): each granting unit gets its own
	// pool. Two grantors of the same perk on the same recipient = two pools,
	// each with its own MaxValue cap. Total shield = sum of both pools.
	ShieldStackingPerSource ShieldStacking = 0
	// ShieldStackingShared: all grantors of this SourceType feed ONE pool on
	// the recipient. The cap is shared across every grantor — total shield
	// cannot exceed MaxValue regardless of how many sources are stacking.
	// SourceUnitID on the pool is "first grantor wins" — refreshes from
	// other units top up the same bucket without changing the pool identity.
	ShieldStackingShared ShieldStacking = 1
)

// shieldStackingModes registers the stacking behaviour of each shield
// SourceType the game knows about. Unregistered types fall through to the
// zero value (ShieldStackingPerSource), which is the safe default — a new
// shield perk added without an entry here gets independent per-source
// pools, which never violates any cap.
//
// ADD NEW SHIELD-GRANTING PERKS HERE when their stacking behaviour is not
// the per-source default. Keep this map small and obvious — it is the
// canonical "which shield buffs don't stack" list.
var shieldStackingModes = map[string]ShieldStacking{
	"dark_renewal": ShieldStackingShared,
}

// shieldStackingFor returns the stacking mode for a SourceType, defaulting
// to ShieldStackingPerSource when the type is unregistered. Cheap O(1)
// map read; safe on the hot path.
func shieldStackingFor(sourceType string) ShieldStacking {
	return shieldStackingModes[sourceType]
}

// applyShieldFromSourceLocked tops up (or creates) a source-specific shield
// pool on `unit`. Returns the amount actually added — anything beyond the
// pool's MaxValue is wasted and reported back so the caller can route it
// elsewhere (e.g. dark_renewal overflow → ally pool).
//
// Keying depends on the SourceType's registered stacking mode:
//   - PerSource (default): keyed by (SourceType, SourceUnitID). Two
//     grantors of the same perk get two pools, each respecting its own cap.
//   - Shared: keyed by SourceType only. All grantors feed one shared pool
//     capped at MaxValue. The pool's SourceUnitID is "first grantor wins";
//     subsequent top-ups from other units don't change pool identity.
//
// Safe to call with amount <= 0 (no-op) or maxValue <= 0 (no-op).
//
// Caller holds s.mu write lock.
func (s *GameState) applyShieldFromSourceLocked(unit *Unit, sourceType string, sourceUnitID, amount, maxValue int, tags []string) int {
	if unit == nil || amount <= 0 || maxValue <= 0 || sourceType == "" {
		return 0
	}
	shared := shieldStackingFor(sourceType) == ShieldStackingShared
	// Top up an existing matching pool. For PerSource we require both keys to
	// match; for Shared the SourceType alone is the identity.
	for i := range unit.PerkState.ShieldPools {
		p := &unit.PerkState.ShieldPools[i]
		if p.SourceType != sourceType {
			continue
		}
		if !shared && p.SourceUnitID != sourceUnitID {
			continue
		}
		// Cap raised? Honor the larger of the two (re-tuning the perk
		// later doesn't shrink already-allocated pools).
		if maxValue > p.MaxValue {
			p.MaxValue = maxValue
		}
		room := p.MaxValue - p.CurrentValue
		if room <= 0 {
			return 0
		}
		add := amount
		if add > room {
			add = room
		}
		p.CurrentValue += add
		return add
	}
	// Allocate a fresh pool.
	add := amount
	if add > maxValue {
		add = maxValue
	}
	unit.PerkState.ShieldPools = append(unit.PerkState.ShieldPools, ShieldPool{
		SourceType:   sourceType,
		SourceUnitID: sourceUnitID,
		CurrentValue: add,
		MaxValue:     maxValue,
		Tags:         tags,
	})
	return add
}

// drainShieldPoolsLocked drains `damage` from the unit's source-specific
// shield pools, oldest-first (slice order). Returns the damage REMAINING
// after pools are exhausted, which the damage pipeline then routes to
// Unit.Shield (legacy blood_engine pool) and finally HP.
//
// Empty pools (CurrentValue == 0) are filtered out in-place so the slice
// shrinks cleanly as pools are exhausted. Pools with non-zero current
// value are preserved so future top-ups (e.g. another dark_renewal pulse)
// can re-fill them.
//
// Caller holds s.mu write lock.
func (s *GameState) drainShieldPoolsLocked(unit *Unit, damage int) int {
	if unit == nil || damage <= 0 || len(unit.PerkState.ShieldPools) == 0 {
		return damage
	}
	kept := unit.PerkState.ShieldPools[:0]
	for _, p := range unit.PerkState.ShieldPools {
		if damage <= 0 || p.CurrentValue <= 0 {
			// Either we've absorbed everything (preserve remaining pools) or
			// this pool was already empty — drop it.
			if p.CurrentValue > 0 {
				kept = append(kept, p)
			}
			continue
		}
		if p.CurrentValue >= damage {
			p.CurrentValue -= damage
			damage = 0
			kept = append(kept, p)
			continue
		}
		damage -= p.CurrentValue
		p.CurrentValue = 0
		// Drop this exhausted pool.
	}
	unit.PerkState.ShieldPools = kept
	return damage
}

// totalShieldFromPoolsLocked returns the sum of CurrentValue across every
// active source-specific shield pool on the unit. Used by the snapshot path
// to ship "displayed shield" to the client (combined with Unit.Shield for
// the legacy blood_engine pool).
//
// Caller holds s.mu (read or write).
func totalShieldFromPoolsLocked(unit *Unit) int {
	if unit == nil {
		return 0
	}
	total := 0
	for i := range unit.PerkState.ShieldPools {
		total += unit.PerkState.ShieldPools[i].CurrentValue
	}
	return total
}

// totalMaxShieldFromPoolsLocked is the analogue of totalShieldFromPools-
// Locked for MaxValue. Sum of every pool's per-source cap.
//
// Caller holds s.mu (read or write).
func totalMaxShieldFromPoolsLocked(unit *Unit) int {
	if unit == nil {
		return 0
	}
	total := 0
	for i := range unit.PerkState.ShieldPools {
		total += unit.PerkState.ShieldPools[i].MaxValue
	}
	return total
}

// unitTotalShieldLocked returns the aggregate "displayed shield" for a unit:
// the sum of every source-specific shield pool plus the legacy single-pool
// Unit.Shield (blood_engine). Used by the snapshot path; HUD shows this as
// the unit's "Shield: X / Y" line.
//
// Caller holds s.mu (read or write).
func (s *GameState) unitTotalShieldLocked(unit *Unit) int {
	if unit == nil {
		return 0
	}
	return unit.Shield + totalShieldFromPoolsLocked(unit)
}

// unitTotalMaxShieldLocked is the aggregate cap: legacy maxShield (from
// unitMaxShieldLocked) plus the sum of every source-specific pool cap.
// Used by the snapshot path so the HUD's "Shield: X / Y" displays the
// combined ceiling rather than just the blood_engine cap.
//
// Caller holds s.mu (read or write).
func (s *GameState) unitTotalMaxShieldLocked(unit *Unit) int {
	if unit == nil {
		return 0
	}
	return s.unitMaxShieldLocked(unit) + totalMaxShieldFromPoolsLocked(unit)
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 11 — percent armor bonus (self-perk, fractional)
//
// perkArmorPercentBonusLocked returns the total fractional armor bonus from
// this unit's own perks (e.g. 0.20 = +20% of base armor). Used in
// effectiveArmorLocked. Percents stack additively.
//
// Currently empty — guardian_aura's percent bonus flows through the aura cache
// via perkArmorPercentBonusFromAurasLocked. This hook exists for symmetry and
// as the future home for any self-perk percent-armor sources.
//
// ADD NEW SELF-PERK PERCENT-ARMOR SOURCES HERE.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) perkArmorPercentBonusLocked(unit *Unit) float64 {
	if unit == nil {
		return 0
	}
	total := 0.0
	// No self-perk percent-armor sources yet.
	// ── add cases for new self-perk percent-armor perks below this line ──────
	_ = total
	return 0
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 12 — outgoing damage debuff multiplier (attacker-side penalty)
//
// perkOutgoingDamageDebuffMultiplierLocked returns the fractional outgoing
// damage penalty currently on the unit (e.g. 0.30 = deal 30% less damage).
// Applied in tickUnitCombatLocked to the raw damage before armor mitigation.
//
// The debuff (WeakenedRemaining / WeakenedMultiplier) is stamped onto the
// attacker by Punishing Guard when the Vanguard takes a hit. It decays in the
// main Update loop (cross-unit, same pattern as TauntRemaining) regardless of
// whether the weakened unit itself owns any perks.
//
// Returns 0 when no debuff is active.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) perkOutgoingDamageDebuffMultiplierLocked(unit *Unit) float64 {
	if unit == nil {
		return 0
	}
	total := 0.0
	if unit.PerkState.WeakenedRemaining > 0 {
		total += unit.PerkState.WeakenedMultiplier
	}
	// Withering Beam (Siphoner bronze) stacks an additional outgoing-damage
	// debuff on top of Weakened. Both feed the same consumer in
	// tickUnitCombatLocked (raw_damage *= 1 - debuff). Cap the total at 1.0
	// so the multiplier can never produce negative damage even if both
	// sources hit at full strength simultaneously (which is intentional —
	// stacking debuffs feel valuable — just bounded).
	total += witheringBeamDamageDebuffMultiplierLocked(unit)
	if total > 1.0 {
		total = 1.0
	}
	return total
}

// ═════════════════════════════════════════════════════════════════════════════
// VANGUARD PERK HOOKS
//
// These three functions implement the defender-side perk effects introduced
// for the Vanguard path. They are called from the damage pipeline and the
// rank-modifier application path.
//
// EXTENSION POINTS — adding more perks later:
//   • More Bronze Vanguard perks  → add entries to perk-defs.json under
//                                   units.soldier.paths.vanguard.bronze
//                                   and add cases to the relevant hook(s) below.
//   • Silver/Gold Vanguard perks  → add entries under vanguard.silver / .gold
//                                   in perk-defs.json, then add cases here as
//                                   needed. Same hooks apply.
//   • Perks for other unit types  → add the unit type under units.<type>.paths
//                                   in perk-defs.json and add cases here.
// ═════════════════════════════════════════════════════════════════════════════

// ─────────────────────────────────────────────────────────────────────────────
// Hook 8 — on damage received (defender-side reactions)
//
// onPerkDamageTakenLocked is called after a unit takes damage from an attacker.
// `damage` is the post-armor value that was passed into the damage pipeline
// (i.e. what the attacker intended after armor, before flat reduction or shield).
//
// Called from:
//   - state.go tickUnitCombatLocked     — primary attack
//   - perks.go savage_strikes bonus hit — secondary hit
//   - perks.go applyCleaveHitLocked     — cleave secondary
//   - perks.go applyWhirlwindHitLocked  — whirlwind AoE
//
// ADD NEW DEFENDER-SIDE PERK REACTIONS HERE.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) onPerkDamageTakenLocked(target, attacker *Unit, damage int) {
	if target == nil || attacker == nil || damage <= 0 || len(target.PerkIDs) == 0 {
		return
	}
	// Skip reactions if the unit is already dead this tick.
	if target.HP <= 0 {
		return
	}

	for _, perkID := range target.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}

		switch perkID {

		case "retaliation":
			// Reflect damage equal to (armorPercent × this unit's armor) back to the
			// attacker on each hit. Higher-armor Vanguards punish attackers more.
			//
			// Guard: RetaliationActive prevents recursive reflection if the attacker
			// also has retaliation. The reflected hit goes through applyUnitDamageLocked
			// only — no XP, threat, or further perk hooks — keeping the chain flat.
			//
			// Tuning point: armorPercent in perk-defs.json → retaliation.config.
			if target.PerkState.RetaliationActive {
				continue // already inside a reflection; do not chain
			}
			if attacker.HP <= 0 || !s.playersAreHostileLocked(attacker.OwnerID, target.OwnerID) {
				continue
			}
			// Use effective armor so conditional armor perks (last_stand) boost
			// reflected damage — a low-HP Vanguard with Retaliation punishes
			// attackers harder, which is the intended synergy.
			reflected := maxInt(0, int(math.Round(float64(s.effectiveArmorLocked(target))*def.Config["armorPercent"])))
			if reflected <= 0 {
				continue
			}
			// Set guard before the call so any path that re-enters this function
			// for this unit is a no-op.
			target.PerkState.RetaliationActive = true
			// Route through the attributed helper so if the attacker dies from
			// reflected damage, the drain handles kill bookkeeping (XP to target,
			// trackBattleKillLocked) and removeUnitLocked. The manual
			// trackBattleKillLocked below is replaced by the drain.
			s.applyUnitDamageWithSourceLocked(attacker, reflected, DamageSource{
				AttackerUnitID: target.ID,
				Kind:           "retaliation",
			})
			target.PerkState.RetaliationActive = false
			// Debug: reflected damage counts under the defender's unit bucket.
			s.trackBattleDamageLocked(battleSourceFromUnit(target), attacker, reflected)

		case "punishing_guard":
			// Stamp a weakened debuff on the attacker: they deal reduced outgoing
			// damage for durationSeconds. Refreshes on every hit so persistent
			// attackers remain debuffed.
			// The debuff lives on the attacker's PerkState and decays in Update().
			if attacker.HP > 0 {
				attacker.PerkState.WeakenedMultiplier = def.Config["weakenedMultiplier"]
				attacker.PerkState.WeakenedRemaining = def.Config["durationSeconds"]
			}

		// ── add cases for new defender-side reactions below this line ────────
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 9 — flat per-hit damage reduction query (defender-side)
//
// perkFlatDamageReductionLocked returns the total flat damage reduction the
// target gets from its perks, applied per hit after armor mitigation and before
// the shield pool. Returns 0 for units with no relevant perk.
//
// Called from applyUnitDamageLocked — covers all damage sources automatically.
//
// ADD NEW FLAT-DAMAGE-REDUCTION PERKS HERE.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) perkFlatDamageReductionLocked(target *Unit) int {
	if target == nil || len(target.PerkIDs) == 0 {
		return 0
	}
	total := 0
	for _, perkID := range target.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {
		case "reinforced_armor":
			// Tuning point: flatReduction in perk-defs.json → reinforced_armor.config.
			total += int(def.Config["flatReduction"])
		// ── add cases for new flat-reduction perks below this line ───────────
		}
	}
	return total
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 10b — bonus armor (defender-side stat modifier, conditional or passive)
//
// perkBonusArmorLocked returns the total flat armor bonus the unit currently
// has from its perks. Stacked additively on top of unit.Armor via
// effectiveArmorLocked.
//
// Unlike perkFlatMaxHPBonusLocked this is NOT baked into unit.Armor via
// applyRankModifiersLocked — the bonus can be conditional (last_stand fires
// only below an HP threshold) and needs to react live. Reading effective armor
// through the helper means the bonus automatically flows into:
//   - every applyArmorMitigation call (primary combat, savage_strikes,
//     cleave, whirlwind)
//   - retaliation reflection (synergy: more armor → more reflected damage)
//   - UnitSnapshot.Armor for HUD display
//
// Handles: last_stand (active only below hpThresholdPercent).
// ADD NEW FLAT-ARMOR PERKS HERE.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) perkBonusArmorLocked(unit *Unit) int {
	if unit == nil || len(unit.PerkIDs) == 0 {
		return 0
	}
	total := 0
	for _, perkID := range unit.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {
		case "last_stand":
			// Bonus active only during the timed window opened by an HP dip
			// below threshold (see last_stand tick in perks.go). Reads the
			// decaying LastStandRemaining timer directly so the bonus
			// disappears cleanly when the window expires — independent of
			// current HP, so heals during the window keep the armor up.
			if unit.PerkState.LastStandRemaining > 0 {
				total += int(def.Config["bonusArmor"])
			}

		case "interlock":
			// Prefer the per-tick predicate cache (recomputePerkPredicate-
			// CacheLocked) so the snapshot path doesn't pay another O(N) scan
			// for an answer the cache already computed. Fall back to a live
			// scan when the cache is stale (helpers called outside Update,
			// e.g. direct-call tests).
			active := unit.PerkState.InterlockActive
			if s.perkPredicateCacheTick != s.Tick {
				active = s.hasAllyInRangeLocked(unit, def.Config["radius"])
			}
			if active {
				total += int(def.Config["bonusArmor"])
			}

		case "brace":
			// Prefer the per-tick predicate cache — see interlock above.
			active := unit.PerkState.BraceActive
			if s.perkPredicateCacheTick != s.Tick {
				threshold := int(def.Config["enemyThreshold"])
				active = s.countEnemiesInRangeLocked(unit, def.Config["radius"], threshold) >= threshold
			}
			if active {
				total += int(def.Config["bonusArmor"])
			}

		// ── add cases for new flat-armor perks below this line ───────────────
		}
	}
	return total
}

// effectiveArmorLocked returns the unit's total effective armor including all
// flat and percent bonuses from perks and banners. Use this everywhere armor is
// read for damage mitigation, damage reflection, and HUD display so conditional
// armor perks (last_stand, brace, guardian_aura) propagate consistently.
//
// Formula:
//
//	effectiveArmor = floor(unit.Armor × (1 + percentBonus)) + flatBonus
//
// Where:
//   - flatBonus    = perkBonusArmorLocked + perkBonusArmorFromBannersLocked +
//                    perkBonusArmorFromAurasLocked + perkBonusArmorFromBuffsLocked
//   - percentBonus = perkArmorPercentBonusLocked + perkArmorPercentBonusFromAurasLocked
//
// Percent bonuses stack additively: two sources of +20% = +40% of base armor.
// This means high-armor units benefit more from percent bonuses, which is the
// intended design (guardian_aura scales with the unit's invested armor stat).
// Buff sources (e.g. bolstering_prayer) are flat — never scaled by percent
// bonuses, so the +50 armor on a healed ally is +50 regardless of percent
// armor modifiers on that ally.
func (s *GameState) effectiveArmorLocked(unit *Unit) int {
	if unit == nil {
		return 0
	}
	flatBonus := s.perkBonusArmorLocked(unit) +
		s.perkBonusArmorFromBannersLocked(unit) +
		s.perkBonusArmorFromAurasLocked(unit) +
		s.perkBonusArmorFromBuffsLocked(unit)
	percentBonus := s.perkArmorPercentBonusLocked(unit) +
		s.perkArmorPercentBonusFromAurasLocked(unit)
	// Mark of Weakness (Siphoner bronze) — flat armor reduction applied
	// after all positive bonuses so it always lands at face value rather
	// than being scaled by percent armor sources. Clamp at 0 so a stacked
	// debuff against a low-armor unit doesn't produce negative armor (the
	// damage pipeline treats negative armor as "no mitigation", but a
	// clearly-clamped zero is easier to reason about in the HUD).
	core := float64(unit.Armor)*(1.0+percentBonus) + float64(flatBonus)
	// Zone-aura armor: a flat add and a multiplier from the owner's controlled
	// zones, folded as (existing + add) × mul. No active aura ⇒ (0, 1) identity,
	// so this is exactly the prior result. Reuses this single armor formula —
	// no second percent path (AI_RULES: avoid duplicate stat calculation).
	auraAdd, auraMul := s.playerStatModifierLocked(unit.OwnerID, statArmor)
	core = (core + auraAdd) * auraMul
	result := int(math.Floor(core))
	result -= markOfWeaknessArmorReductionLocked(unit)
	if result < 0 {
		result = 0
	}
	return result
}

// perkBonusArmorFromBuffsLocked returns the flat-armor contribution from
// cross-unit *buffs* currently active on `unit`. Buffs differ from perks /
// banners / auras in that the buffed unit need not own the originating perk
// — the buff is stamped on PerkState by another unit's cast (e.g. a Cleric
// with bolstering_prayer healing this unit). This helper aggregates every
// such source and is summed into the flat-armor bonus by effectiveArmorLocked.
//
// Currently surfaces:
//   - bolstering_prayer (cleric bronze): BolsteringPrayerArmor while
//     BolsteringPrayerRemaining > 0; rounded to int.
//
// ADD NEW FLAT-ARMOR BUFF SOURCES HERE.
//
// Caller holds s.mu.
func (s *GameState) perkBonusArmorFromBuffsLocked(unit *Unit) int {
	if unit == nil {
		return 0
	}
	total := 0
	if unit.PerkState.BolsteringPrayerRemaining > 0 && unit.PerkState.BolsteringPrayerArmor > 0 {
		total += int(math.Round(unit.PerkState.BolsteringPrayerArmor))
	}
	return total
}

// ─────────────────────────────────────────────────────────────────────────────
// Hook 10 — flat max HP bonus query (passive stat modifier)
//
// perkFlatMaxHPBonusLocked returns the total flat max HP bonus granted by the
// unit's perks. Applied additively on top of rank × path multipliers inside
// applyRankModifiersLocked (progression.go) so it is always included when stats
// are recalculated. Returns 0 for units with no relevant perk.
//
// Called from progression.go applyRankModifiersLocked.
//
// ADD NEW FLAT-MAX-HP PERKS HERE.
// ─────────────────────────────────────────────────────────────────────────────
func (s *GameState) perkFlatMaxHPBonusLocked(unit *Unit) int {
	if unit == nil || len(unit.PerkIDs) == 0 {
		return 0
	}
	total := 0
	for _, perkID := range unit.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {
		case "hold_the_line":
			// Tuning point: bonusMaxHP in perk-defs.json → hold_the_line.config.
			total += int(def.Config["bonusMaxHP"])
		// ── add cases for new flat max HP perks below this line ──────────────
		}
	}
	return total
}
