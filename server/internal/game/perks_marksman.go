package game

import (
	"math"
	"strconv"
)

// ═════════════════════════════════════════════════════════════════════════════
// MARKSMAN PERK MODULE
//
// All Archer → Marksman perk runtime lives in this file. Perks shipped:
//   Bronze:  eagle_spirit, hawk_spirit, vulture_spirit  (passive stat perks)
//   Silver:  split_shot, pierce, hunters_mark           (projectile / debuff)
//   Gold:    double_shot, explosive_tips, bullseye      (compound effects)
//
// Reuses existing perk hooks where possible (perkAttackSpeedBonusLocked,
// perkBonusDamageMultiplierLocked, onPerkAttackFiredLocked, etc.). Three
// new domains are introduced here that did not previously exist in the codebase:
//
//   1. Attack-range modifiers — perkAttackRangeMultiplierLocked, baked into
//      unit.AttackRange by applyRankModifiersLocked. Drives eagle_spirit,
//      bullseye. Pierce reads unit.AttackRange directly so it benefits
//      automatically.
//
//   2. Crit chance + multiplier — see perks_crit.go. Marksman perks contribute
//      via perkCritChanceBonusLocked / perkCritMultiplierBonusLocked here.
//
//   3. Hunter's Mark stacking debuff — see huntersMarkStack and its helpers
//      below. Mirrors the markStack / burnStack pattern in perks.go: per-source
//      stacks with diminishing returns and a configurable cap.
//
// ┌─────────────────────────────────────────────────────────────────────────┐
// │  PROJECTILE INTERACTION RULES (predictable, no infinite recursion)      │
// │                                                                         │
// │   primary attack (state_combat.go)                                      │
// │     ├── crit roll                                                       │
// │     ├── fire primary projectile                                         │
// │     │     └── on land: damage, on-hit perks (mark, explosive_tips)      │
// │     ├── split_shot (silver) — fires up to N additional projectiles at   │
// │     │   distinct in-range hostiles, falling back to extra hits on the   │
// │     │   primary if fewer hostiles are available. Each shot independent. │
// │     ├── double_shot (gold) — arms a deferred fire-again timer; second   │
// │     │   shot rerolls crit, can split, can pierce. Recursion guard       │
// │     │   prevents the second shot from chaining a third.                 │
// │     └── (pierce flag is read inside landProjectile — line scan along    │
// │         attack-range axis, secondary hits at reduced damage)            │
// │                                                                         │
// │   explosive_tips (gold) — runs from onPerkAttackDamageAppliedLocked     │
// │   only on the PRIMARY hit's resolution. AoE damage uses a different     │
// │   damage Kind so it never recurses, and a per-attack guard prevents     │
// │   secondary perk hits (savage_strikes-style) from triggering more       │
// │   explosions.                                                           │
// │                                                                         │
// │   hunters_mark — applied on every Marksman attack hit AND by explosion  │
// │   victims of explosive_tips. Stacks per-source (same Marksman           │
// │   refreshes), diminishing per stack, capped at maxHuntersMarkStacks.    │
// └─────────────────────────────────────────────────────────────────────────┘
// ═════════════════════════════════════════════════════════════════════════════

// ─────────────────────────────────────────────────────────────────────────────
// Hunter's Mark stacking debuff
//
// A timed crit-chance debuff stamped onto enemies hit by a Marksman with
// hunters_mark (silver) — and by explosion victims of explosive_tips (gold).
// Stacks per-source: each unique source ID contributes one stack and same-
// source re-application refreshes the stack's duration without adding a new
// one. Total crit bonus has diminishing returns: first stack = baseCritBonus,
// every additional stack = additionalStackBonus.
//
// Hunter's Mark deliberately does NOT amplify damage (that's challengers_mark /
// marker_trap territory). It only grants crit chance to attackers, so two
// Marksmen sharing a target gain crit synergy without runaway damage scaling.
// ─────────────────────────────────────────────────────────────────────────────

const (
	// maxHuntersMarkStacks is the hard ceiling on concurrent Hunter's Mark
	// stacks. Distinct from maxDebuffStacks (which gates challengers_mark /
	// burns at 2) because Hunter's Mark is a team-support debuff designed to
	// reward stacking from multiple Marksmen.
	maxHuntersMarkStacks = 3
)

// huntersMarkStack is one stack of the Hunter's Mark debuff. SourceID keys
// the stack — same source refreshes, distinct sources stack up to the cap.
// SourceID for unit-applied marks is "hunter-<unitID>"; for explosion-applied
// marks it is "hunter-explode-<unitID>" so a Marksman's bow shot and their
// own explosion mark count as separate stacks (matches user expectation that
// explosive_tips synergizes with hunters_mark to build stacks faster).
type huntersMarkStack struct {
	SourceID    string
	OwnerUnitID int
	Remaining   float64
}

// applyHuntersMarkStack applies or refreshes a Hunter's Mark stack.
// maxStacks lets each calling perk override the default cap if a future
// tuning pass needs a per-perk cap. Returns true when the stack landed.
func (ps *UnitPerkState) applyHuntersMarkStack(sourceID string, ownerUnitID int, duration float64, maxStacks int) bool {
	if duration <= 0 || sourceID == "" {
		return false
	}
	if maxStacks <= 0 {
		maxStacks = maxHuntersMarkStacks
	}
	for i := range ps.HuntersMarkStacks {
		if ps.HuntersMarkStacks[i].SourceID == sourceID {
			if duration > ps.HuntersMarkStacks[i].Remaining {
				ps.HuntersMarkStacks[i].Remaining = duration
			}
			ps.HuntersMarkStacks[i].OwnerUnitID = ownerUnitID
			return true
		}
	}
	if len(ps.HuntersMarkStacks) >= maxStacks {
		return false
	}
	ps.HuntersMarkStacks = append(ps.HuntersMarkStacks, huntersMarkStack{
		SourceID:    sourceID,
		OwnerUnitID: ownerUnitID,
		Remaining:   duration,
	})
	return true
}

// huntersMarkCount returns the number of active Hunter's Mark stacks on this
// unit. Read by attackers' crit-chance calculation.
func (ps *UnitPerkState) huntersMarkCount() int {
	return len(ps.HuntersMarkStacks)
}

// maxHuntersMarkRemaining returns the longest remaining duration across the
// active stacks — used for HUD icon timer display (longest survivor, mirroring
// markStack's max duration semantics).
func (ps *UnitPerkState) maxHuntersMarkRemaining() float64 {
	best := 0.0
	for _, s := range ps.HuntersMarkStacks {
		if s.Remaining > best {
			best = s.Remaining
		}
	}
	return best
}

// decayHuntersMarkStacks reduces every stack's Remaining by dt, dropping
// expired stacks in-place. Mirrors decayMarkStacks. Called from the cross-unit
// decay loop in state.go Update() because the debuff lives on units that may
// not own any Marksman perk themselves.
func (ps *UnitPerkState) decayHuntersMarkStacks(dt float64) {
	if len(ps.HuntersMarkStacks) == 0 {
		return
	}
	kept := ps.HuntersMarkStacks[:0]
	for _, s := range ps.HuntersMarkStacks {
		s.Remaining = math.Max(0, s.Remaining-dt)
		if s.Remaining > 0 {
			kept = append(kept, s)
		}
	}
	ps.HuntersMarkStacks = kept
}

// huntersMarkUnitSourceID is the SourceID for a Marksman's bow-shot mark.
// Same Marksman re-applying refreshes the stack rather than adding another.
func huntersMarkUnitSourceID(unitID int) string {
	return "hunter-unit-" + strconv.Itoa(unitID)
}

// huntersMarkExplosionSourceID is the SourceID for explosive_tips explosion
// marks. Distinct from the bow-shot source so a single Marksman with both
// hunters_mark + explosive_tips can land two stacks on the same target (one
// from the arrow, one from the splash) — matches the user-stated design that
// "Double Shot can apply an additional mark stack" / "explosion-applied
// marks respect the same stacking rules".
func huntersMarkExplosionSourceID(unitID int) string {
	return "hunter-explode-" + strconv.Itoa(unitID)
}

// ─────────────────────────────────────────────────────────────────────────────
// Attack-range hook — Marksman exclusive
// ─────────────────────────────────────────────────────────────────────────────

// perkAttackRangeMultiplierLocked returns the total fractional bonus to
// attack range from this unit's perks. Stacks additively: e.g. eagle_spirit
// (+0.20) + bullseye (+1.00) = +1.20 multiplier => 2.2× base range.
//
// Called from applyRankModifiersLocked so unit.AttackRange is recomputed on
// every rank-up after perks are assigned. Anything reading unit.AttackRange
// (combat acquisition, projectile fire, pierce length, retreat leash, HUD)
// gets the perk-adjusted value with zero additional plumbing.
//
// ADD NEW ATTACK-RANGE-MODIFYING PERKS HERE.
func (s *GameState) perkAttackRangeMultiplierLocked(unit *Unit) float64 {
	if unit == nil || len(unit.PerkIDs) == 0 {
		return 0
	}
	total := 0.0
	for _, perkID := range unit.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {
		case "eagle_spirit":
			total += def.Config["attackRangeBonus"]
		case "bullseye":
			total += def.Config["attackRangeBonus"]
		}
	}
	return total
}

// ─────────────────────────────────────────────────────────────────────────────
// Marksman fire-time dispatch — runs from fireProjectileLocked
//
// FIRE-time effects (visible-projectile spawning) live here so split arrows
// and double-shot timers spawn at the moment the bow fires, not when the
// primary projectile lands. That makes split shot show 3 simultaneous arrows
// in flight, double-shot fire its second arrow shortly after the first leaves
// the bow, etc.
//
// Hunter's Mark stamping is HIT-time (see onMarksmanDamageAppliedLocked) —
// the mark "brands" the target physically when struck.
//
// Recursion guard (MarksmanFireInProgress) prevents the split arrows / second
// double-shot arrow from re-entering this dispatch and chaining into infinite
// shots. Set true on entry, cleared on return.
// ─────────────────────────────────────────────────────────────────────────────

func (s *GameState) onMarksmanProjectileFiredLocked(attacker, primaryTarget *Unit, primaryDamage int) {
	if attacker == nil || primaryTarget == nil || len(attacker.PerkIDs) == 0 {
		return
	}
	if attacker.PerkState.MarksmanFireInProgress {
		return
	}

	// Resolve the perk set up front; this loop runs every attack so cache the
	// def lookups in locals.
	var hasSplitShot, hasDoubleShot bool
	var splitShotExtraTargets int
	var splitShotDamageMult float64
	var splitShotFallbackOnSelf bool
	var doubleShotDelay, doubleShotProcChance float64
	for _, perkID := range attacker.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {
		case "split_shot":
			hasSplitShot = true
			splitShotExtraTargets = int(def.Config["extraShots"])
			splitShotDamageMult = def.Config["secondaryDamageMultiplier"]
			splitShotFallbackOnSelf = def.Config["fallbackOnPrimary"] > 0
		case "double_shot":
			hasDoubleShot = true
			doubleShotDelay = def.Config["delaySeconds"]
			// procChance defaults to 1.0 (always-fire) when missing so old
			// configs without the field don't silently break — but the
			// shipped JSON sets it to 0.25.
			doubleShotProcChance = def.Config["procChance"]
			if doubleShotProcChance <= 0 {
				doubleShotProcChance = 1.0
			}
		}
	}

	if !hasSplitShot && !hasDoubleShot {
		return
	}

	// Guard re-entry from fireProjectileLocked spawned by split / double.
	attacker.PerkState.MarksmanFireInProgress = true
	defer func() { attacker.PerkState.MarksmanFireInProgress = false }()

	// 1. Split Shot — fire up to N extra shots at distinct in-range hostiles.
	if hasSplitShot && splitShotExtraTargets > 0 && splitShotDamageMult > 0 {
		s.fireSplitShotsLocked(attacker, primaryTarget, primaryDamage, splitShotExtraTargets, splitShotDamageMult, splitShotFallbackOnSelf, nil)
	}

	// 2. Double Shot — RNG-proc the deferred second-shot timer. Skipped if
	// the primary target already died this tick (no point queuing a miss),
	// already arming a deferred shot (recursion guard), or the proc roll
	// fails. procChance lives on the perk JSON; clamp <=0 to 1.0 above so
	// missing-field configs default to always-fire.
	if hasDoubleShot && !attacker.PerkState.DoubleShotInProgress && primaryTarget.HP > 0 {
		if doubleShotProcChance >= 1.0 || s.rngPerks.Float64() < doubleShotProcChance {
			attacker.PerkState.DoubleShotPendingSeconds = math.Max(doubleShotDelay, 0.05)
			attacker.PerkState.DoubleShotPendingTargetID = primaryTarget.ID
		}
	}
}

// onMarksmanDamageAppliedLocked runs from onPerkAttackDamageAppliedLocked,
// AFTER the primary hit's damage has landed on the target. Hit-time effects
// (Hunter's Mark stamping, explosive_tips AoE) live here so they take effect
// only when an arrow actually connects — players can dodge them by killing
// the archer before the arrow lands.
func (s *GameState) onMarksmanDamageAppliedLocked(attacker, target *Unit, damage int) {
	if attacker == nil || target == nil || damage <= 0 || len(attacker.PerkIDs) == 0 {
		return
	}
	// Recursion guard: explosion damage and pierce secondary hits MUST NOT
	// trigger another explosion.
	if attacker.PerkState.ExplosiveTipsActive {
		return
	}
	for _, perkID := range attacker.PerkIDs {
		def := perkDefByID(perkID)
		if def == nil {
			continue
		}
		switch perkID {
		case "hunters_mark":
			// Stamp Hunter's Mark on the target the arrow hit. Same Marksman
			// re-attacking refreshes this stack rather than adding another;
			// distinct Marksmen build separate stacks up to maxHuntersMarkStacks.
			if target.HP > 0 {
				target.PerkState.applyHuntersMarkStack(
					huntersMarkUnitSourceID(attacker.ID),
					attacker.ID,
					def.Config["durationSeconds"],
					maxHuntersMarkStacks,
				)
			}
		case "explosive_tips":
			s.fireExplosiveTipsLocked(attacker, target, def)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Split Shot — fires N additional shots at in-range hostiles
//
// Each extra shot rolls its own crit chance independently and routes through
// the same projectile pipeline as the primary. Damage is scaled by
// secondaryDamageMultiplier. When fewer than N extras are in range, the
// missing slots fall back to extra hits on the primary target (configurable).
//
// Recursion: split-shot does NOT call onMarksmanAttackFiredLocked again, so
// there is no risk of split → split chains. Each extra projectile is fired
// directly via fireProjectileLocked (and pierces if the attacker has the
// pierce perk, since pierce is checked at land-time off the attacker's perks).
// ─────────────────────────────────────────────────────────────────────────────

func (s *GameState) fireSplitShotsLocked(attacker, primaryTarget *Unit, primaryRawDamage int, extraTargets int, damageMultiplier float64, fallbackOnPrimary bool, deadUnitIDs *[]int) {
	if attacker == nil || primaryTarget == nil || extraTargets <= 0 {
		return
	}
	rangeSq := attacker.AttackRange * attacker.AttackRange

	// Find up to extraTargets distinct hostiles in range, sorted by distance
	// to attacker. Excludes primary target so split shots actually splash.
	type splitCandidate struct {
		unit   *Unit
		distSq float64
	}
	var candidates []splitCandidate
	for _, candidate := range s.Units {
		if candidate == nil || candidate == primaryTarget || candidate.ID == attacker.ID {
			continue
		}
		if candidate.HP <= 0 || !candidate.Visible {
			continue
		}
		if !playersAreHostile(candidate.OwnerID, attacker.OwnerID) {
			continue
		}
		dx := candidate.X - attacker.X
		dy := candidate.Y - attacker.Y
		distSq := dx*dx + dy*dy
		if distSq > rangeSq {
			continue
		}
		candidates = append(candidates, splitCandidate{unit: candidate, distSq: distSq})
	}
	// Insertion-sort by distance — list is small (<= 6 typical), so O(n²) is cheap.
	for i := 1; i < len(candidates); i++ {
		for j := i; j > 0 && candidates[j].distSq < candidates[j-1].distSq; j-- {
			candidates[j], candidates[j-1] = candidates[j-1], candidates[j]
		}
	}

	maxCandidates := extraTargets
	if maxCandidates > len(candidates) {
		maxCandidates = len(candidates)
	}

	// Compute scaled damage per shot. Re-rolls crit independently per shot —
	// rationale: split shot is meant to feel like multiple arrows, each with
	// its own physical "fortune". Without independent rolls, all extras
	// crit/non-crit together which feels deterministic.
	fireExtra := func(target *Unit) {
		base := float64(primaryRawDamage) * damageMultiplier
		critMult := 1.0
		isCrit := false
		if rolled := s.rollCritDamage(attacker, target); rolled > 1.0 {
			critMult = rolled
			isCrit = true
		}
		raw := base * critMult * (1.0 - s.perkOutgoingDamageDebuffMultiplierLocked(attacker))
		armorAdj := applyArmorMitigation(int(math.Round(raw)), s.effectiveArmorLocked(target))
		if armorAdj <= 0 {
			return
		}
		// Route through fireProjectileLocked so pierce-on-land still triggers
		// for split-shot arrows, mirroring primary projectiles. Tag the
		// freshly-spawned projectile with the crit flag so its land-time
		// damage application queues a critEvent for the client.
		projsBefore := len(s.Projectiles)
		s.fireProjectileLocked(attacker, target, armorAdj)
		if isCrit && len(s.Projectiles) > projsBefore {
			s.Projectiles[projsBefore].IsCrit = true
		}
	}

	for i := 0; i < maxCandidates; i++ {
		fireExtra(candidates[i].unit)
	}
	if fallbackOnPrimary {
		// Fewer extras than slots — fill the rest with extra shots at the primary,
		// at the same reduced damage. Clamp to primary still alive.
		shortfall := extraTargets - maxCandidates
		for i := 0; i < shortfall && primaryTarget.HP > 0; i++ {
			fireExtra(primaryTarget)
		}
	}
	_ = deadUnitIDs // future hook if instant-resolve ever replaces fireProjectileLocked
}

// ─────────────────────────────────────────────────────────────────────────────
// Pierce — implemented as a fixed-line projectile in projectile.go
//
// firePierceProjectileLocked spawns a Projectile with Pierce=true; the
// per-tick traversal lives in tickPierceProjectileLocked (projectile.go) and
// damages enemies as the arrow physically crosses their position. There is
// no land-time helper here — the arrow doesn't "land", it just runs out of
// path or hits its max-hits cap.
// ─────────────────────────────────────────────────────────────────────────────

// ─────────────────────────────────────────────────────────────────────────────
// Double Shot — schedules a deferred second shot
//
// Tick decay lives in tickUnitPerkStateLocked (perks.go). When the timer
// expires and the target is still valid, fireDeferredDoubleShotLocked fires
// a fresh attack as if the unit just attacked. Recursion guard
// (DoubleShotInProgress) prevents the second shot from arming a third.
// ─────────────────────────────────────────────────────────────────────────────

func (s *GameState) fireDeferredDoubleShotLocked(attacker *Unit) {
	if attacker == nil || attacker.HP <= 0 || !attacker.Visible {
		return
	}
	target := s.getUnitByIDLocked(attacker.PerkState.DoubleShotPendingTargetID)
	// Standard target validation — same predicate set as combatTargetIsValidLocked.
	if target == nil || target.HP <= 0 || !target.Visible || !playersAreHostile(target.OwnerID, attacker.OwnerID) {
		return
	}
	// Range gate: out-of-range targets just drop the second shot rather than
	// extending the timer. Keeps double-shot self-contained per attack cycle.
	dx := target.X - attacker.X
	dy := target.Y - attacker.Y
	if dx*dx+dy*dy > attacker.AttackRange*attacker.AttackRange {
		return
	}

	// Compute a fresh damage roll for the second shot. Independent crit roll —
	// each shot has its own fortune.
	bonus := s.perkBonusDamageMultiplierLocked(attacker, target)
	raw := float64(attacker.Damage) * (1.0 + bonus)
	isCrit := false
	if rolled := s.rollCritDamage(attacker, target); rolled > 1.0 {
		raw *= rolled
		isCrit = true
	}
	raw *= (1.0 - s.perkOutgoingDamageDebuffMultiplierLocked(attacker))
	dmg := applyArmorMitigation(int(math.Round(raw)), s.effectiveArmorLocked(target))
	if dmg <= 0 {
		return
	}

	// Arm DoubleShotInProgress so the spawned projectile's fire-time
	// dispatch (split shot, hypothetical future on-fire effects) does NOT
	// arm yet another double-shot timer. fireProjectileLocked also handles
	// split-shot integration through its own fire-time dispatch, so we don't
	// need to call fireSplitShotsLocked manually — the second arrow takes
	// the same code path as the primary, including pierce vs. homing routing.
	attacker.PerkState.DoubleShotInProgress = true
	defer func() { attacker.PerkState.DoubleShotInProgress = false }()

	// Tag the primary 2nd-shot projectile (the FIRST one this dispatch
	// appends) with DoubleShotSecond so the client can render the combined
	// yellow damage number after both arrows land. Splits / pierce extras
	// from this dispatch are appended after and are intentionally NOT tagged
	// — they read as their own independent hits.
	projsBefore := len(s.Projectiles)
	s.fireProjectileLocked(attacker, target, dmg)
	if len(s.Projectiles) > projsBefore {
		s.Projectiles[projsBefore].DoubleShotSecond = true
		if isCrit {
			s.Projectiles[projsBefore].IsCrit = true
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Explosive Tips — small AoE on every hit
//
// Fires from onMarksmanDamageAppliedLocked (i.e. AFTER the primary hit's
// damage has landed). Damages enemies in a configurable radius around the
// target's position. Marks each victim with a Hunter's Mark stack (using the
// "explode" source so it can co-exist with bow-shot marks from the same
// Marksman). The AoE itself uses Kind: "explosive_tips" so it never registers
// for re-explosion, AND a per-attacker recursion guard (ExplosiveTipsActive)
// makes sure secondary hits inside the AoE can't trigger more AoEs.
// ─────────────────────────────────────────────────────────────────────────────

func (s *GameState) fireExplosiveTipsLocked(attacker, primaryTarget *Unit, def *PerkDef) {
	if def == nil || primaryTarget == nil {
		return
	}
	radius := def.Config["explosionRadius"]
	if radius <= 0 {
		return
	}
	radiusSq := radius * radius
	// Damage = attacker damage × multiplier. Kept small relative to the bow
	// shot so explosive_tips is splash, not the main payload.
	damageMult := def.Config["damageMultiplier"]
	if damageMult <= 0 {
		damageMult = 0.5
	}

	// Hunter's Mark on explosion victims is gated on the attacker also
	// owning the silver hunters_mark perk. Without it, explosive_tips is
	// pure splash damage — picking up Hunter's Mark unlocks the mark-on-
	// blast synergy, which is the explicit reward for taking both perks.
	// Duration always tracks the hunters_mark perk's authored value so
	// arrow-mark and explosion-mark stacks share the same tick-down clock.
	var hmDuration float64
	if containsString(attacker.PerkIDs, "hunters_mark") {
		if hmDef := perkDefByID("hunters_mark"); hmDef != nil {
			hmDuration = hmDef.Config["durationSeconds"]
		}
	}

	// Queue the visual VFX up front so the client renders the boom even when
	// no enemy is in radius (the arrow still hit something — the splash is
	// just empty). Visual is independent of damage.
	s.queueExplosionLocked(attacker, primaryTarget.X, primaryTarget.Y, radius, "explosive_tips")

	// Recursion guard armed only while explosion damage is being applied.
	attacker.PerkState.ExplosiveTipsActive = true
	defer func() { attacker.PerkState.ExplosiveTipsActive = false }()

	var deadUnitIDs []int
	for _, candidate := range s.Units {
		if candidate == nil {
			continue
		}
		if candidate.HP <= 0 || !candidate.Visible {
			continue
		}
		if !playersAreHostile(candidate.OwnerID, attacker.OwnerID) {
			continue
		}
		dx := candidate.X - primaryTarget.X
		dy := candidate.Y - primaryTarget.Y
		if dx*dx+dy*dy > radiusSq {
			continue
		}
		// Per-victim crit roll — each blast victim rolls independently so a
		// high-crit Marksman lands red-circle visuals on individual victims
		// rather than all-or-nothing across the AoE. Hunter's Mark on the
		// victim feeds into the crit chance via the standard pathway, so a
		// pre-marked target is more likely to crit-explode for free.
		raw := float64(attacker.Damage) * damageMult
		isVictimCrit := false
		if rolled := s.rollCritDamage(attacker, candidate); rolled > 1.0 {
			raw *= rolled
			isVictimCrit = true
		}
		armorAdj := applyArmorMitigation(int(math.Round(raw)), s.effectiveArmorLocked(candidate))
		if armorAdj > 0 {
			s.applyUnitDamageWithSourceLocked(candidate, armorAdj, DamageSource{AttackerUnitID: attacker.ID, Kind: "explosive_tips"})
			s.recordDamageDealtLocked(attacker, candidate, armorAdj)
			s.trackBattleDamageLocked(battleSourceFromUnit(attacker), candidate, armorAdj)
			if isVictimCrit {
				s.recordCritHitLocked(candidate, armorAdj)
			}
			if candidate.HP <= 0 {
				candidate.HP = 0
				s.awardKillXPLocked(attacker)
				s.payoutDamageDealtXPLocked(candidate)
				s.awardSoldierTankKillXPLocked(candidate.ID)
				s.onPerkKillLocked(attacker)
				s.trackBattleKillLocked(battleSourceFromUnit(attacker), candidate)
				deadUnitIDs = append(deadUnitIDs, candidate.ID)
				continue
			}
		}
		// Apply Hunter's Mark stack on every survivor in the explosion.
		if hmDuration > 0 {
			candidate.PerkState.applyHuntersMarkStack(
				huntersMarkExplosionSourceID(attacker.ID),
				attacker.ID,
				hmDuration,
				maxHuntersMarkStacks,
			)
		}
	}
	for _, id := range deadUnitIDs {
		s.removeUnitLocked(id)
	}
}
