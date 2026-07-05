package game

import (
	"log/slog"
	"math"
	"os"
	"strings"
	"time"
	"webrts/server/pkg/protocol"
)

// attackDamageDeliveryFraction is the fraction of the attack animation at
// which damage actually lands — i.e. the "hit frame" of the swing. The
// remainder of the animation plays out as follow-through during cooldown.
// 0.7 puts impact roughly where weapons connect in a typical 4-frame
// melee swing (windup → forward → impact → recover); the floating damage
// number then pops at the visually-correct moment instead of at the end
// of the animation.
const attackDamageDeliveryFraction = 0.7

// debugAttackTimingEnabled / debugAttackTimingFilter control the server-side
// swing-trace log emitted from applyDelayedAttackLocked. Pair with the
// client's `window.debugAttackTiming` flag to verify end-to-end swing-vs-
// damage timing.
//
// Enable:    DEBUG_ATTACK_TIMING=1
// Filter:    DEBUG_ATTACK_TIMING_TYPES=raider_brute,soldier (comma-separated
//            unitType list; empty = log all)
//
// The filter applies to the attacker's UnitType so it pairs naturally with
// the client's per-type filter on the same data slice.
var debugAttackTimingEnabled = os.Getenv("DEBUG_ATTACK_TIMING") == "1"
var debugAttackTimingFilter = func() map[string]bool {
	raw := os.Getenv("DEBUG_ATTACK_TIMING_TYPES")
	if raw == "" {
		return nil
	}
	out := make(map[string]bool)
	for _, name := range strings.Split(raw, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			out[name] = true
		}
	}
	return out
}()

// logAttackTiming emits one structured slog line per swing landing so the
// server's stdout can be diffed against the client's [atk-timing] logs. No-op
// when DEBUG_ATTACK_TIMING isn't set.
func (s *GameState) logAttackTiming(kind string, attacker *Unit, targetID int, targetType string, damage int) {
	if !debugAttackTimingEnabled {
		return
	}
	if debugAttackTimingFilter != nil && !debugAttackTimingFilter[attacker.UnitType] {
		return
	}
	slog.Info("[atk-timing] swing-lands",
		"kind", kind,
		"attacker_id", attacker.ID,
		"attacker_type", attacker.UnitType,
		"attacker_owner", attacker.OwnerID,
		"target_id", targetID,
		"target_type", targetType,
		"damage", damage,
		"tick", s.Tick,
		"t_ms", time.Now().UnixMilli(),
	)
}

// playersAreHostile reports whether two owner IDs should treat each other as
// enemies for combat / target acquisition purposes.
//
// Currently: real players are always allied with each other. The wave-enemy
// owner ID (enemyPlayerID, "__enemy__") and the neutral-camp owner ID
// (neutralPlayerID, "__neutral__") are both hostile to real players, and real
// players are hostile to them. Same owner is never hostile (so two units in
// the same neutral camp don't fight each other).
//
// ── Team / alliance chokepoint ───────────────────────────────────────────────
//
// Alliance is data: Player.TeamID. Same team ⇒ allied; different ⇒ hostile.
// Everyone defaults to team 0, so with no per-match team assignment this is
// bit-for-bit the old behavior (all real players allied; only the __enemy__
// PvE AI hostile). PvP/FFA later = assign different TeamIDs at match setup;
// no call site changes. Every hostility/friendship decision must route
// through these four predicates (never raw OwnerID comparisons).

// playerTeamLocked returns ownerID's TeamID, or 0 (default shared team) when
// the player is absent (defensive: a unit can briefly outlive its Player
// entry during removal — default-team for one tick is harmless). __enemy__
// is never routed here; the predicates short-circuit on it first.
func (s *GameState) playerTeamLocked(ownerID string) int {
	if p := s.Players[ownerID]; p != nil {
		return p.TeamID
	}
	return 0
}

// playersAreHostileLocked: same owner ⇒ never hostile; __enemy__ or
// __neutral__ ⇒ hostile to every player team (and vice-versa); otherwise
// hostile iff different team. At the default (all team 0) this collapses
// exactly onto the pre-team logic.
func (s *GameState) playersAreHostileLocked(a, b string) bool {
	if a == b {
		return false
	}
	// Enemy wave faction vs neutral camps: gated by the per-map toggle. Default
	// (off) makes them ignore each other; when enabled they fight. Checked before
	// the blanket enemy/neutral-hostile rules below, which still apply to every
	// other pairing (enemy vs player, neutral vs player).
	if (a == enemyPlayerID && b == neutralPlayerID) || (a == neutralPlayerID && b == enemyPlayerID) {
		return s.enemiesFightNeutralsLocked()
	}
	if a == enemyPlayerID || b == enemyPlayerID {
		return true
	}
	if a == neutralPlayerID || b == neutralPlayerID {
		return true
	}
	return s.playerTeamLocked(a) != s.playerTeamLocked(b)
}

// enemiesFightNeutralsLocked reports whether the active map enables combat
// between the __enemy__ wave faction and __neutral__ camps
// (WaveConfig.EnemiesFightNeutrals). Default false — they ignore each other.
// Only has an observable effect when the two factions coexist on the field,
// which today is continuous-wave mode.
func (s *GameState) enemiesFightNeutralsLocked() bool {
	cfg := s.MapConfig.WaveConfig
	return cfg != nil && cfg.EnemiesFightNeutrals
}

// playersAreFriendlyLocked reports allies (same team, self included). This is
// NOT !hostile: the __enemy__ AI and __neutral__ camp mobs are never allies —
// not even to themselves — so heals / ally-scoring / friendly-fire skips
// never treat enemy or neutral units as friendly.
func (s *GameState) playersAreFriendlyLocked(a, b string) bool {
	if a == enemyPlayerID || b == enemyPlayerID {
		return false
	}
	if a == neutralPlayerID || b == neutralPlayerID {
		return false
	}
	return s.playerTeamLocked(a) == s.playerTeamLocked(b)
}

// unitsHostileLocked / unitsFriendlyLocked are the unit-level forms for
// within-tick call sites holding resolved *Unit working values. They encode
// only the alliance relation — callers keep their own nil/HP/Visible guards.
func (s *GameState) unitsHostileLocked(a, b *Unit) bool {
	if a == nil || b == nil || a.ID == b.ID {
		return false
	}
	return s.playersAreHostileLocked(a.OwnerID, b.OwnerID)
}

func (s *GameState) unitsFriendlyLocked(a, b *Unit) bool {
	if a == nil || b == nil {
		return false
	}
	return s.playersAreFriendlyLocked(a.OwnerID, b.OwnerID)
}

// combatTargetIsValidLocked is the single source of truth for "is this unit
// still a valid attack target?". Called from both tickUnitCombatLocked and
// shouldDropCurrentTargetLocked so the two paths agree on the predicate set.
// target may be nil (unit was removed).
func (s *GameState) combatTargetIsValidLocked(unit, target *Unit) bool {
	if target == nil || !target.Visible || target.HP <= 0 || !s.playersAreHostileLocked(target.OwnerID, unit.OwnerID) {
		return false
	}
	if !s.targetRevealedToOwnerLocked(unit, target) {
		return false
	}
	return unitCanTargetPlane(unit, target)
}

// targetRevealedToOwnerLocked reports whether `target` is currently visible to
// `unit`'s owner through the fog of war. A unit must only engage enemies its
// owner can actually see — fog was previously enforced only in the snapshot
// layer, so combat AI could acquire and chase enemies the player could not see.
//
// Owners without an FOW grid — the __enemy__ AI and __neutral__ camp mobs —
// have no fog and always "see" the target, preserving their omniscient
// acquisition (intentional asymmetry: only real players are fog-limited).
// Shared team vision is already baked into each player's grid by
// recomputeFOWLocked, so checking the owner's own grid covers allied vision
// too. Caller holds s.mu.
func (s *GameState) targetRevealedToOwnerLocked(unit, target *Unit) bool {
	if unit == nil || target == nil {
		return false
	}
	fow, ok := s.FOW[unit.OwnerID]
	if !ok {
		return true // no fog grid (enemy AI / neutral) — full sight
	}
	return fow.isClearAtWorld(target.X, target.Y, s.MapConfig.CellSize)
}

// unitCanTargetPlane reports whether `unit`'s TargetableTypes include the
// plane (ground/flyer) that `target` belongs to. Empty TargetableTypes is
// treated as "ground only" defensively — spawnUnitFromDefLocked always
// populates the slice, so a missing value indicates a malformed unit and we
// fail closed rather than letting a melee soldier acquire a flyer.
func unitCanTargetPlane(unit, target *Unit) bool {
	if unit == nil || target == nil {
		return false
	}
	requiredClass := TargetClassGround
	if target.Flyer {
		requiredClass = TargetClassFlyer
	}
	for _, t := range unit.TargetableTypes {
		if t == requiredClass {
			return true
		}
	}
	return false
}

func (s *GameState) AttackWithUnits(playerID string, unitIDs []int, targetUnitID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer profileStart("cmd.AttackWithUnits")()

	target := s.getUnitByIDLocked(targetUnitID)
	if target == nil || !target.Visible || !s.playersAreHostileLocked(target.OwnerID, playerID) {
		return
	}

	blocked := s.getBlockedCellsLocked()
	orderID := s.nextMovementOrderIDLocked()

	// Two-pass: collect valid attackers and stamp the shared OrderID on all
	// of them up front. buildPathingObstaclesLocked excludes same-OrderID
	// peers, so without the pre-pass the first attacker's path would treat
	// its later peers as obstacles and detour around them.
	groupUnits := make([]*Unit, 0, len(unitIDs))
	for _, unitID := range unitIDs {
		unit := s.getUnitByIDLocked(unitID)
		if unit == nil || unit.OwnerID != playerID || !unitHasCapability(unit.UnitType, "attack") {
			continue
		}
		// A melee ground unit cannot be ordered to attack a flyer (and vice
		// versa). Filtering here means the order is silently dropped per
		// unit, matching how the existing capability check already filters
		// non-combatants out of a group attack.
		if !unitCanTargetPlane(unit, target) {
			continue
		}
		groupUnits = append(groupUnits, unit)
	}
	for _, unit := range groupUnits {
		s.resetUnitMovementLocked(unit, orderID)
	}

	// Share the sub-cell blocked map across the group — see MoveUnits.
	groundSubBlocked, flyerSubBlocked := s.buildGroupSubBlockedLocked(groupUnits, blocked)

	// Order / anchor / target assignment first, pathing second. Splitting the
	// two passes lets assignAttackGroupPathsLocked run one full A* for the
	// leader and reuse the result for every follower — see its docstring.
	for _, unit := range groupUnits {
		unit.AttackTargetID = targetUnitID
		unit.Attacking = false
		unit.Status = "Moving To Attack"
		unit.Order = OrderState{Type: OrderAttackTarget, DestX: target.X, DestY: target.Y}
		// Anchor on the target, not the unit's current position. The leash
		// check is centered on the anchor; using unit.X/Y would fail for any
		// long-distance attack command and the AI would drop the target on
		// the next tick. Target-centered anchor mirrors MoveUnits/AttackMoveUnits.
		unit.CombatAnchorX = target.X
		unit.CombatAnchorY = target.Y
	}

	// Leader-follower attack pathing — shortest-range unit leads, others
	// truncate its path at their own range. Skips units already in range.
	s.assignAttackGroupPathsLocked(groupUnits, target, blocked, groundSubBlocked, flyerSubBlocked)
}

// resolveAttackHitLocked applies damage to target and runs every on-hit
// reaction (perk procs, XP payouts, kill tracking, retaliation). Returns true
// when attacker died from reflected damage — callers should skip any further
// per-attacker work and move on.
//
// Does NOT touch attacker.AttackCooldown: that's already been committed by
// applyDelayedAttackLocked at windup-end (the moment this function is called),
// and the kill-reset path in removeUnitLocked can clear it later if the
// target dies from this hit. Layering another cooldown write in here would
// stomp those decisions.
func (s *GameState) resolveAttackHitLocked(attacker, target *Unit, damage int, deadUnitIDs *[]int) bool {
	// Tag the damage with the attacker's declared school (set at spawn from
	// unit def, defaults to "" = physical). Carries through to the client's
	// colored-popup system so a fire-typed mage's basic attack pops orange,
	// a shadow-typed necromancer's attack pops dark purple, etc. Untyped
	// units (most soldiers, raiders) keep the default white/red popup.
	s.applyUnitDamageWithSourceLocked(target, damage, DamageSource{AttackerUnitID: attacker.ID, Kind: "melee", DamageType: attacker.AttackDamageType})
	s.onUnitDamagedLocked(attacker, target, damage)
	s.onPerkDamageTakenLocked(target, attacker, damage)

	// Base-stat splash (raider_brute, etc.): damage every OTHER hostile
	// within SplashRadius of the primary target. Direct damage call only —
	// no on-attack perk hooks so it can't recurse (a splash hit shouldn't
	// chain hunters_mark, savage_strikes, more splashes, etc.).
	if attacker.SplashRadius > 0 && damage > 0 {
		s.applySplashDamageLocked(attacker, target, damage, deadUnitIDs)
	}

	if attacker.HP <= 0 {
		s.awardUnitDeathXPLocked(attacker, target)
		s.awardSoldierTankKillXPLocked(attacker.ID)
		s.onPerkKillLocked(target)
		*deadUnitIDs = append(*deadUnitIDs, attacker.ID)
		return true
	}

	s.recordSoldierTankContributionLocked(attacker, target, damage)
	s.recordDamageDealtLocked(attacker, target, damage)
	s.trackBattleDamageLocked(battleSourceFromUnit(attacker), target, damage)
	s.onPerkAttackFiredLocked(attacker, target, damage, deadUnitIDs)
	s.onPerkAttackDamageAppliedLocked(attacker, target, damage)
	s.applyEquipmentOnHitEffectsLocked(attacker, target)

	if target.HP <= 0 {
		target.HP = 0
		s.awardUnitDeathXPLocked(target, attacker)
		s.awardSoldierTankKillXPLocked(target.ID)
		s.onPerkKillLocked(attacker)
		s.trackBattleKillLocked(battleSourceFromUnit(attacker), target)
		// DP drop rolls on melee kills. The dual-death pipeline removes the
		// unit synchronously here (via deadUnitIDs → removeUnitLocked), so by
		// the time drainPendingDeathsLocked iterates the pending queue the
		// target is already gone and its rollDominionPointDropLocked branch is
		// skipped. Mirror the call site in drainPendingDeathsLocked so kills
		// landed via this legacy direct-remove path still roll for drops.
		s.rollDominionPointDropLocked(attacker.OwnerID, target)
		// Neutral-camp kill attribution: this legacy direct-remove path removes
		// the unit synchronously (deadUnitIDs → removeUnitLocked) before
		// drainPendingDeathsLocked runs, so the drain's markCampKillerLocked at
		// the camp's 0-units hook is skipped (unit already gone). Mirror it here
		// — same reason rollDominionPointDropLocked is mirrored above — so a
		// camp cleared by real combat records who landed the final blow. Without
		// this the kill_camps objective is never credited: LastKillerWasPlayer
		// stays false and onUnitRemovedFromCampLocked's gate denies the clear.
		if target.NeutralCampID != "" {
			s.markCampKillerLocked(target.NeutralCampID, attacker.OwnerID)
		}
		*deadUnitIDs = append(*deadUnitIDs, target.ID)
		// Legacy markObjectiveKillLocked(target.ObjectiveID) call removed in
		// §9 of campaign-objectives-and-metrics; kill counters now feed the
		// new objective evaluator via Player.Metrics.
	}
	return false
}

// applySplashDamageLocked is the base-stat splash payload for units with
// SplashRadius > 0 (raider_brute, etc.). Hits every hostile within
// attacker.SplashRadius of the primary target's position EXCEPT the primary
// target itself (already hit). Uses applyUnitDamageWithSourceLocked
// directly — bypassing the on-attack perk hooks — so it can't recurse and
// doesn't accidentally trigger savage_strikes / hunters_mark / explosive_tips
// chains on each splashed victim. Awards XP and tracks kills like any other
// trap-style AoE.
//
// Must be called under s.mu write lock.
func (s *GameState) applySplashDamageLocked(attacker, primaryTarget *Unit, damage int, deadUnitIDs *[]int) {
	radSq := attacker.SplashRadius * attacker.SplashRadius
	for _, u := range s.Units {
		if u == nil || u == primaryTarget || u == attacker {
			continue
		}
		if u.HP <= 0 || !u.Visible {
			continue
		}
		if !s.playersAreHostileLocked(u.OwnerID, attacker.OwnerID) {
			continue
		}
		// Respect targetability — a ground-only splasher can't bleed into flyers.
		if !unitCanTargetPlane(attacker, u) {
			continue
		}
		dx := u.X - primaryTarget.X
		dy := u.Y - primaryTarget.Y
		if dx*dx+dy*dy > radSq {
			continue
		}
		// Base-stat splash inherits the attacker's damage school — a fire-
		// typed splasher's AoE reads as fire, etc. Defaults to physical for
		// untyped attackers (raider_brute today).
		s.applyUnitDamageWithSourceLocked(u, damage, DamageSource{AttackerUnitID: attacker.ID, Kind: "splash", DamageType: attacker.AttackDamageType})
		s.recordDamageDealtLocked(attacker, u, damage)
		s.trackBattleDamageLocked(battleSourceFromUnit(attacker), u, damage)
		if u.HP <= 0 {
			s.awardUnitDeathXPLocked(u, attacker)
			s.awardSoldierTankKillXPLocked(u.ID)
			s.trackBattleKillLocked(battleSourceFromUnit(attacker), u)
			// DP roll on splash kills. Same legacy direct-remove path as the
			// melee branch — see comment in resolveAttackHitLocked.
			s.rollDominionPointDropLocked(attacker.OwnerID, u)
			*deadUnitIDs = append(*deadUnitIDs, u.ID)
		}
	}
}

// applyEquipmentOnHitElementalLocked applies the attacker's aggregated
// per-element on-hit damage as SEPARATE typed damage instances against the
// primary target, distinct from the physical hit that resolveAttackHitLocked
// already landed. Iterates DamageTypes() (sorted) rather than ranging the map
// directly so the order of damage events is deterministic. No-op when the
// attacker has no elemental bonus. Must be called under s.mu.
//
// Each element renders as its OWN colored side-popup (like trap DoT / splash),
// not as a tint on the main number: the instance suppresses the auto damage-
// type hint and instead records a minor damage event for the landed amount, so
// the client peels it off the combined HP-diff and floats it out to the side.
func (s *GameState) applyEquipmentOnHitElementalLocked(attacker, target *Unit) {
	if attacker == nil || target == nil || len(attacker.EquipmentBonus.OnHitElemental) == 0 {
		return
	}
	for _, dt := range DamageTypes() {
		amt := attacker.EquipmentBonus.OnHitElemental[dt]
		if amt <= 0 {
			continue
		}
		landed := s.applyUnitDamageWithSourceLocked(target, amt, DamageSource{
			AttackerUnitID:   attacker.ID,
			Kind:             "item-elemental",
			DamageType:       dt,
			SuppressTypeHint: true,
		})
		// Show the element as a separate side-falling popup, colored by its
		// variant (fire/orange, cold/light-blue, electric/purple, …).
		s.recordMinorDamageHitLocked(target, landed, damageTypeColorVariant(dt))
	}
}

// applyEquipmentOnHitEffectsLocked runs an attacker's EQUIPMENT on-hit effects
// (flat elemental instances + rolled procs) against a single target. It is
// deliberately scoped to equipment effects and does NOT re-run the on-ATTACK
// perk hub (savage_strikes, cleaving_rage, whirlwind_core, …).
//
// This is the seam that lets hits which are not the primary swing still trigger
// on-hit gear: cleaving_rage's secondary and every whirlwind_core sweep hit call
// it so a Berserker's lightning_sword (or any proc/elemental item) fires per hit
// — matching Marksman split-shot, whose arrows each resolve a full hit. Routing
// those hits through resolveAttackHitLocked instead would re-enter the attack
// hub and recursively spawn more cleaves/whirlwinds, so only the equipment
// effects are replayed here. Procs themselves apply damage with SkipOnHitEffects
// semantics, so this cannot recurse.
//
// Caller holds s.mu write lock.
func (s *GameState) applyEquipmentOnHitEffectsLocked(attacker, target *Unit) {
	s.applyEquipmentOnHitElementalLocked(attacker, target)
	s.rollEquipmentProcsLocked(attacker, target)
}

// rollEquipmentProcsLocked rolls each of the attacker's equipped on-hit procs
// against the seeded perk RNG and fires an elemental bolt for each success at
// the primary target. Must be called under s.mu. Determinism: rngPerks is the
// shared seeded stream; OnHitProcs order is fixed by equip order.
func (s *GameState) rollEquipmentProcsLocked(attacker, target *Unit) {
	if attacker == nil || target == nil || len(attacker.EquipmentBonus.OnHitProcs) == 0 {
		return
	}
	for _, proc := range attacker.EquipmentBonus.OnHitProcs {
		if proc.Chance <= 0 || proc.Damage <= 0 {
			continue
		}
		if s.rngPerks.Float64() < proc.Chance {
			// Route by the emitted effect's declared kind: a beam-kind def zaps
			// the target instantly (damage applied here), a projectile-kind def
			// (the default, incl. unknown ids) fires a flying bolt that lands
			// later. The chance roll above is identical either way so
			// determinism is unaffected by the branch.
			if def, ok := getProjectileDef(proc.ProjectileID); ok && def.IsBeam() {
				s.fireOnHitProcBeamLocked(attacker, target, proc, def)
			} else {
				s.fireOnHitProcProjectileLocked(attacker, target, proc)
			}
		}
	}
}

// applyDelayedAttackLocked resolves a unit's swing at the moment its windup
// expires. Re-validates the target — if it became invalid during the windup
// (HP<=0, invisible, or no longer hostile) the swing whiffs harmlessly,
// which in practice is rare because the kill-on-target-death path in
// removeUnitLocked / destroyBuildingLocked already cancels the windup
// before this function runs. Distance is intentionally NOT re-checked
// here: once a swing has committed it lands regardless of where the target
// moved during the animation (matches the standard RTS "committed swing"
// contract). Must be called under s.mu while AttackWindupRemaining has just
// reached 0.
func (s *GameState) applyDelayedAttackLocked(unit *Unit, deadUnitIDs *[]int, destroyedBuildingIDs *[]string) {
	// Effective speed at fire time — slow / haste landing during the windup
	// window is reflected in the post-swing idle gap, not in the already-
	// committed animation length. Cooldown spans both the animation's
	// follow-through (post-impact frames) and the idle gap, totalling
	// (cycle − pre-impact windup) so the next swing fires at the right
	// moment to keep the overall cadence at 1/effectiveSpeed.
	effectiveSpeed := math.Max(0.1, unit.AttackSpeed+s.perkAttackSpeedBonusLocked(unit))
	effectiveSpeed = math.Max(0.1, effectiveSpeed*slowFactorLocked(unit)*lingeringHexAttackSpeedFactorLocked(unit))
	cycleSeconds := 1.0 / effectiveSpeed
	animDur := math.Min(1.0, cycleSeconds)
	preImpact := animDur * attackDamageDeliveryFraction
	unit.AttackCooldown = math.Max(0, cycleSeconds-preImpact)
	if unit.UnitType == "archer" {
		unit.PerkState.LastCombatSeconds = 1.5
	}

	// ── Unit-vs-unit ─────────────────────────────────────────────────────
	// Resolve against the target the swing was COMMITTED to at windup start
	// (AttackWindupTargetID), NOT the live AttackTargetID. A mid-swing retarget
	// — camp aggro broadcast, a player re-issuing AttackWithUnits, taunt, etc. —
	// changes AttackTargetID but must not redirect an already-committed swing
	// onto a different (possibly out-of-range) enemy. The live retarget applies
	// on the next swing. Falls back to AttackTargetID only if no committed id was
	// recorded (defensive: pre-existing swings across a hot reload).
	committedTargetID := unit.AttackWindupTargetID
	// Defensive fallback for a unit swing whose committed id was never recorded
	// (a swing already in flight across a hot reload, or a test that hand-sets
	// AttackWindupRemaining). Gated on "no building swing in flight" so it can
	// NEVER route a building swing into the unit branch — a mid-swing player
	// retarget from a building to an out-of-range unit must not connect.
	if committedTargetID == 0 && unit.AttackBuildingTargetID == "" {
		committedTargetID = unit.AttackTargetID
	}
	if committedTargetID != 0 {
		target := s.getUnitByIDLocked(committedTargetID)
		if !s.combatTargetIsValidLocked(unit, target) {
			return // target gone / dead / allied — whiff
		}
		// No distance check at fire time: once a swing is committed (windup
		// began while target was in range), it lands. Re-checking distance
		// here would whiff visibly-connecting melee swings whenever the
		// target stepped just outside AttackRange during the 1s animation —
		// see the parallel "committed swing" semantic that RTS players
		// expect. Ranged units fire a projectile that homes onto the
		// current target position regardless of distance at fire time.
		profile := resolveCombatProfile(unit)
		// Pierce defers the crit roll to per-victim rolls inside
		// tickPierceProjectileLocked so each enemy along the line rolls
		// independent fortune (and a red-circle visual on a hit). The
		// projectile carries the pre-crit damage in that case.
		isPierce := !profile.Melee && containsString(unit.PerkIDs, "pierce")
		rawDamage := float64(unit.Damage) * (1.0 + s.perkBonusDamageMultiplierLocked(unit, target))
		// Zone-aura damage: a flat add and a multiplier from the attacker owner's
		// controlled zones, folded as (existing + add) × mul before crit/armor.
		// Covers both melee and ranged (this is pre-branch). No active aura ⇒
		// (0, 1) identity. Ability damage is a separate system, out of v1 scope.
		if dmgAdd, dmgMul := s.playerStatModifierLocked(unit.OwnerID, statDamage); dmgAdd != 0 || dmgMul != 1 {
			rawDamage = (rawDamage + dmgAdd) * dmgMul
		}
		critMult := 1.0
		isCrit := false
		if !isPierce {
			critMult = s.rollCritDamage(unit, target)
			isCrit = critMult > 1.0
		}
		rawDamage *= critMult
		rawDamage *= (1.0 - s.perkOutgoingDamageDebuffMultiplierLocked(unit))
		// Profile damage multiplier (physical_power / magic_power) is baked
		// into unit.BaseDamage at spawn time via applyPlayerUpgradesAtSpawnLocked,
		// so rawDamage already reflects it here — no runtime multiply needed.
		damage := applyArmorMitigation(int(math.Round(rawDamage)), s.effectiveArmorLocked(target))

		if !profile.Melee {
			// Tag the primary projectile with the crit flag so its land-time
			// damage application queues a critEvent. Splits / pierces appended
			// inside fireProjectileLocked stay un-flagged unless their own
			// per-victim roll succeeds later.
			projsBefore := len(s.Projectiles)
			s.fireProjectileLocked(unit, target, damage)
			if isCrit && len(s.Projectiles) > projsBefore {
				s.Projectiles[projsBefore].IsCrit = true
			}
			s.logAttackTiming("projectile-fire", unit, target.ID, target.UnitType, damage)
			return
		}
		s.logAttackTiming("melee-land", unit, target.ID, target.UnitType, damage)
		if s.resolveAttackHitLocked(unit, target, damage, deadUnitIDs) {
			return
		}
		// Melee landed — record the crit now if it was one.
		if isCrit {
			s.recordCritHitLocked(target, damage)
		}
		return
	}

	// ── Unit-vs-building ─────────────────────────────────────────────────
	if unit.AttackBuildingTargetID != "" {
		building := s.getBuildingByIDLocked(unit.AttackBuildingTargetID)
		if building == nil {
			return
		}
		hp, _, hpOk := getBuildingHP(building)
		if !hpOk || hp <= 0 {
			return
		}
		// No at-fire distance check (see unit-vs-unit rationale above) —
		// a building can't dodge, so this only matters when the attacker
		// itself was knocked back during the windup. Committing to the
		// swing is the right behaviour either way.
		damage := unit.Damage
		newHP := hp - float64(damage)
		building.Metadata["hp"] = newHP
		s.onBuildingDamagedLocked(unit, building, damage)
		s.recordDamageDealtBuildingLocked(unit, building.ID, damage)
		s.logAttackTiming("melee-building-land", unit, 0, "building:"+building.BuildingType, damage)
		if newHP <= 0 {
			building.Metadata["hp"] = 0.0
			s.payoutBuildingDamageDealtXPLocked(building.ID)
			*destroyedBuildingIDs = append(*destroyedBuildingIDs, building.ID)
		}
		return
	}
}

func (s *GameState) tickUnitCombatLocked(dt float64, blocked map[gridPoint]bool) {
	var deadUnitIDs []int
	var destroyedBuildingIDs []string

	for _, unit := range s.Units {
		// Cast lock: a unit mid-cast is animation-locked. Pin "Casting" and
		// skip combat dispatch so the per-tick status writer below can never
		// clobber it with "Attacking"/"Idle", and the unit cannot attack while
		// casting. Mirrors the AttackWindup early-continue just below. The
		// timed cast lifecycle (duration, mana, interrupt) is layered on this
		// by the ability system.
		if unit.Casting {
			// Channeled abilities (Siphon Life) ride the same Casting lock but
			// expose a distinct status so the client can pin a held sprite
			// frame instead of looping the casting cycle.
			if unit.ChannelAbilityID != "" {
				unit.Status = unitStatusChanneling
			} else {
				unit.Status = unitStatusCasting
			}
			continue
		}

		// Mid-swing windup: decay toward 0; on completion resolve damage.
		// Runs ahead of target/branch dispatch so the swing is the single
		// authoritative animation phase for the unit. Paused while stunned
		// (the stun's CC effect freezes the wind-up alongside movement).
		// Status / Attacking are pinned true here so the client keeps
		// playing the attack animation across the whole windup, including
		// the tick that fires damage. The cancel-on-death paths in
		// removeUnitLocked / destroyBuildingLocked zero AttackWindupRemaining
		// when the target dies, so this block simply doesn't fire on the
		// next tick — no whiff visual, the unit drops cleanly to Idle.
		if unit.AttackWindupRemaining > 0 {
			if unit.StunnedRemaining == 0 {
				unit.AttackWindupRemaining = math.Max(0, unit.AttackWindupRemaining-dt)
				if unit.AttackWindupRemaining == 0 {
					s.applyDelayedAttackLocked(unit, &deadUnitIDs, &destroyedBuildingIDs)
				}
			}
			unit.Attacking = true
			unit.Status = "Attacking"
			continue
		}

		// Handle unit-vs-unit combat
		if unit.AttackTargetID != 0 {
			target := s.getUnitByIDLocked(unit.AttackTargetID)
			if !s.combatTargetIsValidLocked(unit, target) {
				unit.AttackTargetID = 0
				unit.Attacking = false
				unit.ActionFacingDX = 0
				unit.ActionFacingDY = 0
				if unit.Order.Type == OrderAttackTarget {
					unit.Order = OrderState{Type: OrderIdle}
				}
				unit.Status = "Idle"
			} else {
				dx := target.X - unit.X
				dy := target.Y - unit.Y
				dist := math.Sqrt(dx*dx + dy*dy)

				if dist <= unit.AttackRange {
					unit.Moving = false
					unit.Path = nil
					unit.Attacking = true
					unit.Status = "Attacking"
					// Persist the unit→target delta so the snapshot can ship an
					// authoritative facing direction. Recomputed every tick the
					// unit is in-range and firing — the target it actually shoots
					// is the source of truth, not the client's local "nearest
					// enemy" guess.
					unit.ActionFacingDX = dx
					unit.ActionFacingDY = dy

					// Stun: cooldown still decays so the unit doesn't bank a free
					// attack on un-stun, but the unit must not fire. AttackTargetID
					// is intentionally left intact so combat resumes immediately.
					if unit.StunnedRemaining > 0 {
						if unit.AttackCooldown > 0 {
							unit.AttackCooldown = math.Max(0, unit.AttackCooldown-dt)
						}
					} else {
						// Decay cooldown FIRST, then check if ready to fire. The
						// previous "check, then decay" order added a 1-tick (dt
						// = 50ms) delay to every cycle because a cooldown that
						// expired mid-tick had to wait until the next tick to
						// trigger the next windup. Over many swings that drift
						// accumulates and the damage popup falls progressively
						// later than the animation's hit frame.
						if unit.AttackCooldown > 0 {
							unit.AttackCooldown = math.Max(0, unit.AttackCooldown-dt)
						}
						if unit.AttackCooldown <= 0 {
							// Begin windup. Damage / projectile lands when the windup
							// reaches 0 (see applyDelayedAttackLocked). The windup is
							// the *pre-impact* portion of the animation; damage sits
							// at attackDamageDeliveryFraction of the swing so the
							// floating number coincides with the visible hit frame.
							effectiveSpeed := math.Max(0.1, unit.AttackSpeed+s.perkAttackSpeedBonusLocked(unit))
							effectiveSpeed = math.Max(0.1, effectiveSpeed*slowFactorLocked(unit)*lingeringHexAttackSpeedFactorLocked(unit))
							animDur := math.Min(1.0, 1.0/effectiveSpeed)
							unit.AttackWindupRemaining = animDur * attackDamageDeliveryFraction
							// Commit this swing to the target it started against. A
							// later mid-swing retarget of AttackTargetID won't redirect
							// the damage (see applyDelayedAttackLocked).
							unit.AttackWindupTargetID = unit.AttackTargetID
						}
					}
				} else {
					unit.Attacking = false
					// Out of range — clear the per-tick attack facing so the
					// client falls back to movement-direction inference while
					// the unit chases.
					unit.ActionFacingDX = 0
					unit.ActionFacingDY = 0
					// Hold units never move to engage. If the target walked out of
					// attack range, drop it and stay put rather than giving chase.
					// Guards are exempt: they actively chase intruders within
					// GuardLeashRange of their anchor (see Gate C in
					// applyCombatTargetLocked). shouldDropCurrentTargetLocked drops
					// the target if it leaves the leash.
					if unit.Order.Type == OrderHold && !unit.GuardMode {
						s.clearCombatTargetLocked(unit)
						continue
					}
					unit.Status = "Moving To Attack"
					profile := resolveCombatProfile(unit)
					// Refresh approach unconditionally — refreshUnitAttackApproachLocked
					// either lays a fresh A* path (cheap thanks to the in-range
					// short-circuit when the destination is already valid) or, on A*
					// failure, drops the unit into drift mode. Drift is single-step
					// straight-line movement (no per-tick A*) so there is no longer a
					// per-tick A* storm to throttle here. AttackDrifting=true with an
					// empty Path is the unit's "I'm advancing toward target without a
					// route" state; the per-unit movement loop in state.go handles it.
					s.refreshUnitAttackApproachLocked(unit, target, profile, blocked, !unit.Moving)
				}
			}
			continue
		}

		// Handle unit-vs-building combat
		if unit.AttackBuildingTargetID != "" {
			building := s.getBuildingByIDLocked(unit.AttackBuildingTargetID)
			if building == nil {
				unit.AttackBuildingTargetID = ""
				unit.Attacking = false
				unit.ActionFacingDX = 0
				unit.ActionFacingDY = 0
				unit.Status = "Idle"
				continue
			}
			hp, _, hpOk := getBuildingHP(building)
			if !hpOk || hp <= 0 {
				unit.AttackBuildingTargetID = ""
				unit.Attacking = false
				unit.ActionFacingDX = 0
				unit.ActionFacingDY = 0
				unit.Status = "Idle"
			} else {
				dist := s.distanceToBuilding(unit.X, unit.Y, building)

				if dist <= unit.AttackRange {
					unit.Moving = false
					unit.Path = nil
					unit.Attacking = true
					unit.Status = "Attacking"
					center := s.buildingCenterLocked(building)
					unit.ActionFacingDX = center.X - unit.X
					unit.ActionFacingDY = center.Y - unit.Y

					// Stun: cooldown still decays, but the unit must not fire.
					// AttackBuildingTargetID is left intact so combat resumes on un-stun.
					if unit.StunnedRemaining > 0 {
						if unit.AttackCooldown > 0 {
							unit.AttackCooldown = math.Max(0, unit.AttackCooldown-dt)
						}
					} else {
						// Decay cooldown FIRST, then check if ready to fire — see
						// the unit-vs-unit branch above for the drift rationale.
						if unit.AttackCooldown > 0 {
							unit.AttackCooldown = math.Max(0, unit.AttackCooldown-dt)
						}
						if unit.AttackCooldown <= 0 {
							buildingAttackSpeed := math.Max(0.1, unit.AttackSpeed*slowFactorLocked(unit)*lingeringHexAttackSpeedFactorLocked(unit))
							animDur := math.Min(1.0, 1.0/buildingAttackSpeed)
							unit.AttackWindupRemaining = animDur * attackDamageDeliveryFraction
							// This swing targets a building — clear any committed unit
							// target so applyDelayedAttackLocked resolves the building
							// branch and a stale unit id can't hijack it.
							unit.AttackWindupTargetID = 0
						}
					}
				} else {
					unit.Attacking = false
					unit.ActionFacingDX = 0
					unit.ActionFacingDY = 0
					// Hold units never move to engage buildings either. Guards are
					// exempt — same exception as the unit-vs-unit branch above and
					// applyCombatTargetLocked: GuardMode painted enemies are spawned
					// with OrderHold but are expected to chase within GuardLeashRange.
					if unit.Order.Type == OrderHold && !unit.GuardMode {
						unit.AttackBuildingTargetID = ""
						unit.Status = "Idle"
						continue
					}
					unit.Status = "Moving To Attack"
					if !unit.Moving {
						// Throttle forced repathing for building targets, mirroring the
						// unit-vs-unit branch above. Without this, a unit whose A* keeps
						// failing (target enclosed, dense crowd, etc.) runs full sub-cell
						// A* (~65k cells) every tick, dominating the tick budget on maps
						// with multiple stuck attackers.
						//
						// On each failure escalate via applyBuildingUnreachableEscalation-
						// Locked: shouldDropCurrentTargetLocked has no unreachable-memo
						// check for buildings (it only drops on destruction), so without
						// active escalation here a unit committed to an unreachable
						// building loops forever at the throttle cadence. The escalation
						// drops the target via clearCombatTargetLocked on strike 3, which
						// breaks the loop and lets the unit fall back to an objective.
						if s.Tick >= unit.NextApproachRepathTick {
							buildingID := unit.AttackBuildingTargetID
							s.assignUnitPath(unit, protocol.Vec2{X: unit.TargetX, Y: unit.TargetY}, blocked, nil)
							if !unit.Moving {
								unit.NextApproachRepathTick = s.Tick + approachRepathCooldownTicks
								if buildingID != "" {
									s.applyBuildingUnreachableEscalationLocked(unit, buildingID, blocked)
								}
							}
						}
					} else {
						unit.NextApproachRepathTick = 0
					}
				}
			}
			continue
		}

		if unit.AttackCooldown > 0 {
			unit.AttackCooldown = math.Max(0, unit.AttackCooldown-dt)
		}
	}

	for _, id := range deadUnitIDs {
		s.removeUnitLocked(id)
	}
	for _, id := range destroyedBuildingIDs {
		s.destroyBuildingLocked(id)
	}
}

func (s *GameState) tickBuildingCombatLocked(dt float64) {
	var deadUnitIDs []int

	for i := range s.MapConfig.Buildings {
		building := &s.MapConfig.Buildings[i]
		if !building.Visible || building.OwnerID == nil {
			continue
		}
		if building.Metadata != nil && building.Metadata["underConstruction"] == true {
			continue
		}

		def, ok := getBuildingDef(building.BuildingType)
		if !ok || def.Damage <= 0 || def.AttackRange <= 0 || def.AttackSpeed <= 0 {
			continue
		}

		if building.Metadata == nil {
			building.Metadata = map[string]interface{}{}
		}

		cooldown, _ := getMetadataFloat(building.Metadata, "attackCooldown")
		if cooldown > 0 {
			cooldown = math.Max(0, cooldown-dt)
			building.Metadata["attackCooldown"] = cooldown
		}

		// Mid-swing windup: tick down toward 0, fire on completion. The
		// target is re-acquired at fire time (rather than locked at windup
		// start) so a tower that loses its line-of-fire to one unit still
		// hits whatever's in range when the swing lands. Whiffs only when
		// the radius is empty.
		windup, _ := getMetadataFloat(building.Metadata, "attackWindupRemaining")
		if windup > 0 {
			windup = math.Max(0, windup-dt)
			building.Metadata["attackWindupRemaining"] = windup
			if windup == 0 {
				if hit := s.findNearestHostileUnitForBuildingLocked(building, *building.OwnerID, def.AttackRange); hit != nil {
					s.applyUnitDamageWithSourceLocked(hit, def.Damage, DamageSource{AttackerBuildingID: building.ID, Kind: "building"})
					s.trackBattleDamageLocked(battleSourceFromBuilding(building), hit, def.Damage)
					if hit.HP <= 0 {
						hit.HP = 0
						s.trackBattleKillLocked(battleSourceFromBuilding(building), hit)
						deadUnitIDs = append(deadUnitIDs, hit.ID)
					}
				}
				// Cooldown begins now — total cycle minus the pre-impact
				// windup just consumed. Follow-through frames play out
				// during this cooldown, then the idle gap (if any).
				cycleSeconds := 1.0 / def.AttackSpeed
				animDur := math.Min(1.0, cycleSeconds)
				preImpact := animDur * attackDamageDeliveryFraction
				building.Metadata["attackCooldown"] = math.Max(0, cycleSeconds-preImpact)
			}
			continue
		}

		target := s.findNearestHostileUnitForBuildingLocked(building, *building.OwnerID, def.AttackRange)
		if target == nil || cooldown > 0 {
			continue
		}

		// Begin windup. Damage lands at attackDamageDeliveryFraction of the
		// animation; follow-through frames play during the subsequent cooldown.
		animDur := math.Min(1.0, 1.0/def.AttackSpeed)
		building.Metadata["attackWindupRemaining"] = animDur * attackDamageDeliveryFraction
	}

	for _, id := range deadUnitIDs {
		s.removeUnitLocked(id)
	}
}

func (s *GameState) findNearestHostileUnitForBuildingLocked(building *protocol.BuildingTile, ownerID string, attackRange float64) *Unit {
	var best *Unit
	bestDistSq := attackRange * attackRange

	for _, unit := range s.Units {
		if !unit.Visible || unit.HP <= 0 || !s.playersAreHostileLocked(unit.OwnerID, ownerID) {
			continue
		}

		dist := s.distanceToBuilding(unit.X, unit.Y, building)
		distSq := dist * dist
		if distSq > bestDistSq {
			continue
		}

		best = unit
		bestDistSq = distSq
	}

	return best
}

func (s *GameState) unitsAreInMutualMeleeLocked(a, b *Unit) bool {
	if a == nil || b == nil {
		return false
	}
	if !s.playersAreHostileLocked(a.OwnerID, b.OwnerID) {
		return false
	}
	aProfile := resolveCombatProfile(a)
	bProfile := resolveCombatProfile(b)
	if !aProfile.Melee || !bProfile.Melee {
		return false
	}
	if a.AttackTargetID != b.ID && b.AttackTargetID != a.ID {
		return false
	}
	const meleeContactPadding = 8.0
	aRange := math.Max(a.AttackRange, unitRadius+meleeContactPadding)
	bRange := math.Max(b.AttackRange, unitRadius+meleeContactPadding)
	return distanceSquared(a.X, a.Y, b.X, b.Y) <= aRange*aRange || distanceSquared(a.X, a.Y, b.X, b.Y) <= bRange*bRange
}
