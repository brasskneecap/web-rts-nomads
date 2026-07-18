package game

import (
	"sort"

	"webrts/server/pkg/protocol"
)

// resolveOriginLocked maps a TargetOrigin to the concrete world position a
// TargetQueryDef's radius filter searches around. Caller holds s.mu.
//
// OriginProjectilePos resolves to ctx.ProjectilePosition — real for a firing
// built by tickArcaneOrbProjectileLocked (projectile.go), which binds it to
// the bolt's current world position every tick; zero-value {0,0} for any
// other context, since nothing else sets it (no existing compiled/authored
// program references this origin outside arcane_orb's on_projectile_tick
// firing, so this has no effect on any other caller).
//
// OriginStatusOwner / OriginSummonedUnit are still not wired (no status/summon
// runtime context exists yet) and fall through to the caster-position
// default; see the TODO(phase-3b) below.
func (s *GameState) resolveOriginLocked(ctx *RuntimeAbilityContext, origin TargetOrigin, ref *ContextRef) protocol.Vec2 {
	casterPos := func() protocol.Vec2 {
		if u := s.getUnitByIDLocked(ctx.CasterID); u != nil {
			return protocol.Vec2{X: u.X, Y: u.Y}
		}
		return protocol.Vec2{}
	}

	switch origin {
	case OriginCaster:
		return casterPos()
	case OriginCastPoint:
		return ctx.CastPoint
	case OriginImpactPosition:
		return ctx.ImpactPosition
	case OriginCurrentEventPos:
		return ctx.EventPosition
	case OriginZoneCenter:
		return ctx.ZoneCenter
	case OriginInitialTarget, OriginInitialTargetPos:
		if u := s.getUnitByIDLocked(ctx.InitialTarget); u != nil {
			return protocol.Vec2{X: u.X, Y: u.Y}
		}
		return protocol.Vec2{}
	case OriginNamedContextValue:
		if ref == nil || ctx.Named == nil {
			return casterPos()
		}
		v, ok := ctx.Named[ref.Key]
		if !ok {
			return casterPos()
		}
		switch v.Kind {
		case ctxPosition:
			return v.Position
		case ctxUnitID:
			if u := s.getUnitByIDLocked(v.UnitID); u != nil {
				return protocol.Vec2{X: u.X, Y: u.Y}
			}
		case ctxUnitSet:
			// A named unit set resolves to its FIRST member's position — the
			// "cursor" idiom a loop chain uses (the current target being arced
			// from). Empty set / dead unit falls back to the caster.
			if len(v.UnitIDs) > 0 {
				if u := s.getUnitByIDLocked(v.UnitIDs[0]); u != nil {
					return protocol.Vec2{X: u.X, Y: u.Y}
				}
			}
		}
		return casterPos()
	case OriginProjectilePos:
		return ctx.ProjectilePosition
	case OriginStatusOwner, OriginSummonedUnit:
		// TODO(phase-3b): no status/summon runtime context is threaded into
		// RuntimeAbilityContext yet. Fall back to caster pos.
		return casterPos()
	default:
		return casterPos()
	}
}

// originUnitForSpawnLocked returns the unit whose sprite a spawned beam should
// visually lift its endpoint from for the given SpawnOrigin, or 0 when the
// origin is a pure world position with no single owning unit. It mirrors
// resolveOriginLocked's origin→position mapping but yields the UNIT id (for the
// client's origin-lift sprite lookup, Beam.CasterUnitID) rather than the
// position. This is what makes a chain_lightning bounce read as CONTINUOUS: a
// bounce hop's launch_beam uses SpawnOrigin=current_event_position, so its
// visual origin must lift from the PREVIOUS VICTIM's chest — exactly where the
// incoming bolt terminated — not from the original caster. Matches legacy's
// spawnMomentaryDamageBeamLocked(cursor.ID) which set CasterUnitID to the
// previous victim, never the caster.
//
// Caller holds s.mu (read or write).
func (s *GameState) originUnitForSpawnLocked(ctx *RuntimeAbilityContext, origin TargetOrigin, ref *ContextRef) int {
	switch origin {
	case OriginCurrentEventPos:
		return ctx.CurrentEventUnitID
	case OriginInitialTarget, OriginInitialTargetPos:
		return ctx.InitialTarget
	case OriginNamedContextValue:
		// A named "cursor" set/id: lift the beam from that unit (the current
		// chain target being arced from), matching resolveOriginLocked's
		// named-context position. 0 when unresolvable ⇒ client default anchor.
		if ref != nil && ctx.Named != nil {
			if v, ok := ctx.Named[ref.Key]; ok {
				switch v.Kind {
				case ctxUnitID:
					return v.UnitID
				case ctxUnitSet:
					if len(v.UnitIDs) > 0 {
						return v.UnitIDs[0]
					}
				}
			}
		}
		return 0
	case OriginCastPoint, OriginImpactPosition, OriginZoneCenter, OriginProjectilePos:
		// Pure world positions with no single owning unit — the client falls
		// back to its default beam anchor offset when the unit id is 0/absent.
		return 0
	default:
		// OriginCaster and every caster-fallback case in resolveOriginLocked
		// (empty origin, status/summon-owner not-yet-threaded).
		return ctx.CasterID
	}
}

// candidatePoolIDsLocked gathers the raw (unfiltered) unit-id candidate pool
// for a TargetQueryDef's Source. Caller holds s.mu.
//
// SrcCurrentEvent resolves ctx.CurrentEventUnitID, the unit bound by whichever
// producer fired the current trigger (today: on_zone_enter/on_zone_exit via
// fireAbilityZoneOccupancyEventLocked in ability_zone.go). 0 (no producer has
// bound a unit) yields an empty pool rather than an error, matching every
// other "nothing to resolve" case in this file.
//
// SrcSourceObject is not wired yet (no non-unit source-object runtime context
// exists); see the TODO(phase-3b) below.
func (s *GameState) candidatePoolIDsLocked(ctx *RuntimeAbilityContext, q TargetQueryDef) []int {
	switch q.Source {
	case SrcAllInScene:
		ids := make([]int, 0, len(s.Units))
		for _, u := range s.Units {
			if u == nil {
				continue
			}
			ids = append(ids, u.ID)
		}
		return ids
	case SrcPrevActionTargets:
		ids := make([]int, len(ctx.Selected))
		copy(ids, ctx.Selected)
		return ids
	case SrcNamedContext:
		if q.OriginRef == nil || ctx.Named == nil {
			return nil
		}
		v, ok := ctx.Named[q.OriginRef.Key]
		if !ok {
			return nil
		}
		switch v.Kind {
		case ctxUnitSet:
			ids := make([]int, len(v.UnitIDs))
			copy(ids, v.UnitIDs)
			return ids
		case ctxUnitID:
			return []int{v.UnitID}
		default:
			return nil
		}
	case SrcCaster:
		return []int{ctx.CasterID}
	case SrcInitialTarget:
		if ctx.InitialTarget == 0 {
			return nil
		}
		return []int{ctx.InitialTarget}
	case SrcCurrentEvent:
		if ctx.CurrentEventUnitID == 0 {
			return nil
		}
		return []int{ctx.CurrentEventUnitID}
	case SrcSourceObject:
		// TODO(phase-3b): no source-object runtime context is threaded into
		// RuntimeAbilityContext yet.
		return nil
	default:
		return nil
	}
}

// relationMatchesLocked reports whether candidate unit u satisfies ANY of the
// requested relations relative to caster. An empty rels slice means "no
// relation filter" (always matches). Caller holds s.mu.
//
// RelNeutral is not wired yet (no neutral-vs-caster classification exists
// alongside the ally/enemy alliance predicates); see the TODO(phase-3b)
// below — it never matches in Phase 3.
func (s *GameState) relationMatchesLocked(caster, u *Unit, rels []TargetRelation) bool {
	if len(rels) == 0 {
		return true
	}
	self := u.ID == caster.ID
	ally := !self && s.unitsFriendlyLocked(caster, u)
	enemy := s.unitsHostileLocked(caster, u)
	for _, r := range rels {
		switch r {
		case RelSelf:
			if self {
				return true
			}
		case RelAlly:
			if ally {
				return true
			}
		case RelEnemy:
			if enemy {
				return true
			}
		case RelNeutral:
			// TODO(phase-3b): neutral relation classification.
		}
	}
	return false
}

// resolveTargetQueryLocked resolves a TargetQueryDef to a deterministic,
// ordered list of unit IDs by gathering the Source candidate pool and
// delegating filter/order/cap logic to applyTargetFiltersLocked. Caller
// holds s.mu.
//
// q.MinCount, q.Filters, and q.RequireLineOfSight are not enforced in Phase
// 3 (see the TODOs in applyTargetFiltersLocked below); they are validated
// fields on the wire but have no runtime effect yet.
func (s *GameState) resolveTargetQueryLocked(ctx *RuntimeAbilityContext, q TargetQueryDef) []int {
	caster := s.getUnitByIDLocked(ctx.CasterID)
	if caster == nil {
		return nil
	}

	poolIDs := s.candidatePoolIDsLocked(ctx, q)
	poolSeen := make(map[int]struct{}, len(poolIDs)) // dedups the raw candidate pool only
	candidates := make([]*Unit, 0, len(poolIDs))
	for _, id := range poolIDs {
		if _, dup := poolSeen[id]; dup {
			continue
		}
		poolSeen[id] = struct{}{}
		if u := s.getUnitByIDLocked(id); u != nil {
			candidates = append(candidates, u)
		}
	}

	return s.applyTargetFiltersLocked(ctx, caster, candidates, q)
}

// applyTargetFiltersLocked applies a TargetQueryDef's alive/relation/
// visibility/radius/exclude-source filters, the IncludeInitialTarget forcing
// rule, ordering, and the MaxCount cap to a candidate *Unit list, returning
// the resulting unit IDs. Extracted from resolveTargetQueryLocked (Phase 3
// Task 6) so filter_targets can apply the SAME filter/order/cap logic to an
// action's incoming target set instead of the scene-wide candidate pool.
// Caller holds s.mu.
//
// q.MinCount, q.Filters, and q.RequireLineOfSight are not enforced in Phase
// 3 (see the TODOs in resolveTargetQueryLocked's former doc, preserved
// below); they are validated fields on the wire but have no runtime effect
// yet.
func (s *GameState) applyTargetFiltersLocked(ctx *RuntimeAbilityContext, caster *Unit, candidates []*Unit, q TargetQueryDef) []int {
	origin := s.resolveOriginLocked(ctx, q.Origin, q.OriginRef)

	// ExcludeRef: drop every candidate whose ID is present in the named
	// ctxUnitSet at q.ExcludeRef.Key (e.g. a chain's accumulated "already hit"
	// set). Absent key or a Kind other than ctxUnitSet is a no-op — mirrors
	// SrcNamedContext's own "wrong kind -> nothing" convention rather than
	// erroring. Built as a membership map for O(1) lookup; the candidates
	// slice itself is filtered in its existing order below, so map iteration
	// order never influences the result.
	var excludeSet map[int]struct{}
	if q.ExcludeRef != nil && q.ExcludeRef.Key != "" && ctx.Named != nil {
		if v, ok := ctx.Named[q.ExcludeRef.Key]; ok && v.Kind == ctxUnitSet {
			excludeSet = make(map[int]struct{}, len(v.UnitIDs))
			for _, id := range v.UnitIDs {
				excludeSet[id] = struct{}{}
			}
		}
	}

	// Resolve the effective radius (handles the match-attack-range sentinel).
	radiusActive := q.Radius != 0
	effRadius := q.Radius
	if effRadius < 0 {
		effRadius = CastRange(q.Radius).Resolve(caster)
	}
	radSq := effRadius * effRadius

	passesFilters := func(u *Unit) bool {
		if u == nil {
			return false
		}
		switch q.AliveState {
		case "dead":
			if u.HP > 0 {
				return false
			}
		case "any":
			// no HP filter
		default: // "" or "alive"
			if u.HP <= 0 {
				return false
			}
		}
		if !s.relationMatchesLocked(caster, u, q.Relations) {
			return false
		}
		// Enemy-visibility parity: mirror applyAbilitySplashDamageLocked's
		// AoE-victim predicate (state_combat.go) so query results equal what
		// splash damage would actually hit: hostile + HP>0 + Visible. This
		// must be evaluated PER CANDIDATE against that candidate's own
		// relation to the caster, not query-wide off q.Relations — a mixed
		// query like Relations:[RelAlly, RelEnemy] must never visibility-
		// filter an invisible ALLY just because the query also asks for
		// enemies. Only a candidate that is itself hostile to the caster is
		// subject to this check.
		if s.unitsHostileLocked(caster, u) && !u.Visible {
			return false
		}
		if radiusActive && distanceSquared(origin.X, origin.Y, u.X, u.Y) > radSq {
			return false
		}
		if q.ExcludeSource && u.ID == caster.ID {
			return false
		}
		// ExcludeCurrentEvent: drop the unit a trigger's event centers on
		// (ctx.CurrentEventUnitID) — see TargetQueryDef.ExcludeCurrentEvent's
		// doc comment (ability_program.go). 0 means no producer bound a
		// current-event unit, so nothing is excluded (matches SrcCurrentEvent's
		// own "no producer -> empty pool" convention rather than treating an
		// unset ctx field as if it names a real unit id).
		if q.ExcludeCurrentEvent && ctx.CurrentEventUnitID != 0 && u.ID == ctx.CurrentEventUnitID {
			return false
		}
		if excludeSet != nil {
			if _, dropped := excludeSet[u.ID]; dropped {
				return false
			}
		}
		return true
	}

	inResults := make(map[int]struct{}, len(candidates))
	units := make([]*Unit, 0, len(candidates))
	for _, u := range candidates {
		if u == nil {
			continue
		}
		if _, dup := inResults[u.ID]; dup {
			continue
		}
		if !passesFilters(u) {
			continue
		}
		units = append(units, u)
		inResults[u.ID] = struct{}{}
	}

	// IncludeInitialTarget: force the initial target into the result set if
	// it is valid (passes the same alive/relation checks; radius/exclude-
	// source are deliberately bypassed for the forced inclusion, matching
	// "force an out-of-radius initial target in"). Prepend, deduped by id.
	if q.IncludeInitialTarget && ctx.InitialTarget != 0 {
		if _, already := inResults[ctx.InitialTarget]; !already {
			it := s.getUnitByIDLocked(ctx.InitialTarget)
			if it != nil {
				aliveOK := true
				switch q.AliveState {
				case "dead":
					aliveOK = it.HP <= 0
				case "any":
					aliveOK = true
				default:
					aliveOK = it.HP > 0
				}
				if aliveOK && s.relationMatchesLocked(caster, it, q.Relations) {
					units = append([]*Unit{it}, units...)
					inResults[ctx.InitialTarget] = struct{}{}
				}
			}
		}
	}

	switch q.Ordering {
	case OrderClosest:
		sort.SliceStable(units, func(i, j int) bool {
			di := distanceSquared(origin.X, origin.Y, units[i].X, units[i].Y)
			dj := distanceSquared(origin.X, origin.Y, units[j].X, units[j].Y)
			if di != dj {
				return di < dj
			}
			return units[i].ID < units[j].ID
		})
	case OrderFarthest:
		sort.SliceStable(units, func(i, j int) bool {
			di := distanceSquared(origin.X, origin.Y, units[i].X, units[i].Y)
			dj := distanceSquared(origin.X, origin.Y, units[j].X, units[j].Y)
			if di != dj {
				return di > dj
			}
			return units[i].ID < units[j].ID
		})
	case OrderLowestHealth:
		sort.SliceStable(units, func(i, j int) bool {
			if units[i].HP != units[j].HP {
				return units[i].HP < units[j].HP
			}
			return units[i].ID < units[j].ID
		})
	case OrderHighestHealth:
		sort.SliceStable(units, func(i, j int) bool {
			if units[i].HP != units[j].HP {
				return units[i].HP > units[j].HP
			}
			return units[i].ID < units[j].ID
		})
	case OrderLowestHealthPct:
		sort.SliceStable(units, func(i, j int) bool {
			c := healthPctCompare(units[i], units[j])
			if c != 0 {
				return c < 0
			}
			return units[i].ID < units[j].ID
		})
	case OrderRandom:
		// Deterministic shuffle: consumes the seeded combat RNG stream
		// (s.rngCombat). Callers relying on rngCombat determinism across a
		// tick must account for this consumption, same as any other
		// rngCombat.* draw (dodge/block rolls, spell_charge target picks).
		sort.SliceStable(units, func(i, j int) bool { return units[i].ID < units[j].ID })
		s.rngCombat.Shuffle(len(units), func(i, j int) { units[i], units[j] = units[j], units[i] })
	case OrderUnitID:
		sort.SliceStable(units, func(i, j int) bool { return units[i].ID < units[j].ID })
	default:
		sort.SliceStable(units, func(i, j int) bool { return units[i].ID < units[j].ID })
	}

	if q.MaxCount > 0 && len(units) > q.MaxCount {
		units = units[:q.MaxCount]
	}

	// TODO(phase-3b): q.MinCount is not enforced (no "abort/short-circuit
	// the action if below MinCount" behavior wired yet).
	// TODO(phase-3b): q.Filters (TargetFilter) is not evaluated.
	// TODO(phase-3b): q.RequireLineOfSight is not evaluated (no LoS/vision-
	// blocking geometry query wired into the targeting resolver yet).

	ids := make([]int, len(units))
	for i, u := range units {
		ids[i] = u.ID
	}
	return ids
}

// healthPctCompare compares two units' HP/MaxHP ratios without floats, via
// integer cross-multiplication (a.HP/a.MaxHP vs b.HP/b.MaxHP). Returns <0 if
// a's percentage is lower, 0 if equal, >0 if higher. A unit with MaxHP<=0 is
// treated as 0% (sorts lowest) to avoid a division by zero.
func healthPctCompare(a, b *Unit) int {
	if a.MaxHP <= 0 && b.MaxHP <= 0 {
		return 0
	}
	if a.MaxHP <= 0 {
		return -1
	}
	if b.MaxHP <= 0 {
		return 1
	}
	lhs := int64(a.HP) * int64(b.MaxHP)
	rhs := int64(b.HP) * int64(a.MaxHP)
	switch {
	case lhs < rhs:
		return -1
	case lhs > rhs:
		return 1
	default:
		return 0
	}
}
