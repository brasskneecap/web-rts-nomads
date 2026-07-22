package game

import (
	"encoding/json"
	"log/slog"
	"sort"
	"strconv"

	"webrts/server/pkg/protocol"
)

// ═════════════════════════════════════════════════════════════════════════════
// ABILITY ZONE SYSTEM (Phase 3, Task 5)
//
// AbilityZone generalizes GroundHazard (ground_hazard.go, left untouched — it
// remains the legacy delayed-impact + lingering-burn primitive that Meteor
// uses today) into a composable, tick-driven spatial zone: a create_zone
// action spawns one, and every TickInterval seconds for Duration seconds it
// fires its compiled on_zone_tick trigger, which runs through the SAME
// executor as any other trigger (runProgramTriggersLocked) — this file never
// hand-rolls damage/healing/etc, it only supplies the per-tick
// RuntimeAbilityContext (ZoneCenter/EventPosition) that lets the zone's
// nested select_targets(origin: "zone_center") + deal_damage/etc actions
// resolve exactly like any other program.
//
// Server-only by design: never serialized. A zone's only player-visible
// output is (a) damage/healing/etc, which already rides the authoritative
// pipeline, and (b) a future client presentation effect (not wired in Phase
// 3 — see the TODO(phase-6) in createZoneConfig's Execute).
//
// OCCUPANCY (on_zone_enter / on_zone_exit)
//
// Every tick, tickAbilityZonesLocked also recomputes which units are
// currently inside the zone (zoneOccupantIDsLocked) and diffs that set
// against last tick's (AbilityZone.occupantIDs), firing on_zone_enter for
// units newly present and on_zone_exit for units newly absent
// (fireAbilityZoneOccupancyEventLocked). This is a distinct concern from
// on_zone_tick: a zone with TickInterval<=0 never ticks but still tracks
// occupancy every tick (the TickInterval>0 guard below only wraps the
// on_zone_tick loop).
//
// "Inside" = alive (HP>0) AND Visible, regardless of relation to the
// caster. Occupancy is deliberately NOT hostile-only (unlike
// applyTargetFiltersLocked's AoE-victim predicate, which mirrors splash
// damage on purpose): a healing zone needs to fire on_zone_enter for
// ALLIES, a damage zone for enemies, and an author can always narrow with
// select_targets{relations:[...]} or trigger Conditions inside the
// enter/exit trigger's own actions. This keeps the occupancy primitive
// general and pushes the "who does this affect" policy entirely into the
// authored trigger, matching how on_zone_tick already defers all target
// selection to its own nested select_targets action.
//
// A unit that dies or goes invisible while inside a zone drops out of the
// alive+visible set exactly like one that walked out — on_zone_exit fires
// either way, same tick. A zone whose Remaining expires this tick fires
// on_zone_exit for every unit still occupying it (in the same ascending-ID
// order as any other exit sweep) before it is dropped, so an enter-paired
// apply_status/deal_damage-style effect always gets a matching exit even
// though the unit itself never left the radius. The owner-left-match cull
// a few lines below is the one exception: it already skipped on_zone_tick
// before this change (an abrupt drop, not a graceful lifecycle end), and
// continues to skip on_zone_enter/on_zone_exit the same way for
// consistency — deliberately NOT extended to fire a final exit sweep.
// ═════════════════════════════════════════════════════════════════════════════

// zoneTickEpsilon absorbs float64 accumulation error when a zone is advanced
// in many small dt steps (e.g. 10 x 0.1s). Both the per-tick cadence
// (tickTimer) and the expiry countdown (Remaining) compare against it instead
// of a bare "<= 0", so a caller stepping in non-exact fractions (0.1 is not
// exact in binary) still gets the same tick count and expiry tick as a caller
// stepping in exact fractions (0.5, 0.25, ...).
const zoneTickEpsilon = 1e-9

// AbilityZone is the composable, tick-driven spatial zone spawned by a
// create_zone action. It generalizes GroundHazard (which stays as the legacy
// delayed-impact primitive): a zone fires its compiled on_zone_tick trigger
// every TickInterval for Duration seconds. Server-only, never serialized;
// damage rides the authoritative pipeline via the nested actions' seams.
type AbilityZone struct {
	ID            string
	AbilityID     string
	CasterID      int
	OwnerPlayerID string
	Center        protocol.Vec2
	Radius        float64
	Remaining     float64
	TickInterval  float64
	tickTimer     float64 // counts down to the next on_zone_tick fire (runtime-only)
	// Sprite / SpriteScale make this zone VISIBLE — see createZoneConfig.Sprite.
	// Empty Sprite (the default) keeps the zone server-only, so nothing about an
	// existing zone's behavior or wire footprint changes.
	Sprite      string
	SpriteScale float64
	Triggers    []AbilityTriggerDef // compiled on_zone_tick / on_zone_enter / on_zone_exit trigger(s)
	// occupantIDs is the sorted-ascending, deduped set of unit IDs that were
	// inside Radius as of the end of the previous tickAbilityZonesLocked call
	// (nil before the zone's first tick). Stored as an ID slice — never
	// *Unit (AI_RULES) — and never a map, so nothing about it can leak
	// map-iteration order into which order on_zone_enter/on_zone_exit fire:
	// both this and this tick's freshly computed occupant set
	// (zoneOccupantIDsLocked) are always ascending-ID-sorted, and
	// diffSortedUnitIDs merges two sorted slices, so its "entered"/"exited"
	// outputs come out ascending-ID-sorted for free. Runtime-only, not
	// serialized (mirrors tickTimer).
	occupantIDs []int
	// consumed marks a zone that ended itself mid-execution via the
	// consume_zone action (the one-shot zone shape: fire once, vanish). Checked
	// alongside the expiry test so a consumed zone is culled THIS tick and
	// still gets its paired on_zone_exit sweep. Runtime-only.
	consumed bool
}

func abilityZoneIDString(id int) string {
	return "zone-" + strconv.Itoa(id)
}

// visibleZoneSnapshotsLocked returns wire snapshots for every zone that opted
// into visibility (AbilityZone.Sprite non-empty). Zones without a Sprite —
// which is every zone shipped before this existed — return nothing, so the
// wire is unchanged for them.
//
// Visible zones ride the SAME snapshot array (and therefore the same client
// renderer) as traps. That is deliberate: a trap IS a visible zone, and the
// four legacy traps are being re-authored onto create_zone. Sharing the array
// now means the client render path is already the zone render path, so the
// trap migration removes a producer rather than needing a second renderer.
// Once s.Traps is gone the field is just "persistent ground entities" and can
// be renamed. See docs/design/ability_perk_interaction.md §8.
//
// Callers that filter by fog-of-war (snapshotForPlayerLocked) apply the same
// owner/visibility test to these that they apply to traps — every field that
// test needs (OwnerID/X/Y) is present on the returned snapshots.
//
// Caller holds s.mu.
func (s *GameState) visibleZoneSnapshotsLocked() []protocol.TrapSnapshot {
	var out []protocol.TrapSnapshot
	for _, z := range s.AbilityZones {
		if z == nil || z.Sprite == "" {
			continue
		}
		out = append(out, protocol.TrapSnapshot{
			ID:               z.ID,
			OwnerID:          z.OwnerPlayerID,
			X:                z.Center.X,
			Y:                z.Center.Y,
			Radius:           z.Radius,
			ScaleMultiplier:  z.SpriteScale,
			Type:             z.Sprite,
			RemainingSeconds: z.Remaining,
		})
	}
	return out
}

// spawnAbilityZoneLocked assigns z's id, arms its tick cadence, and appends it
// to s.AbilityZones. The first tick fires IMMEDIATELY (tickTimer armed at 0,
// not TickInterval), matching GroundHazard's "impact, then burn ticks" pacing
// exactly: legacy resets burnTickTimer to 0 the instant impact fires
// (ground_hazard.go's tickGroundHazardsLocked), so its first burn tick lands
// on the very same tick as impact, then repeats every BurnTickInterval. Zones
// created via create_zone reuse the identical zoneTickEpsilon-guarded loop in
// tickAbilityZonesLocked to fire that first due tick (and, if TickInterval is
// small enough relative to a caller's dt to make more than one tick due at
// once, however many are due) on the very first tickAbilityZonesLocked call
// that sees this zone — see tickAbilityZonesLocked's doc comment for why that
// call can be one simulation dt after this function runs rather than the same
// tick (zone creation via create_zone happens inside tickAbilityMarkersLocked,
// which runs AFTER tickAbilityZonesLocked in Update's ordering), which is a
// harmless sub-tick timing shift, not a missed or extra tick.
//
// This tickTimer=0 arming is also what reproduces GroundHazard's
// accumulator-overshoot behavior: resetting to 0 instead of TickInterval
// means a zone whose Duration divides its TickInterval evenly fires ONE MORE
// total tick over its life than a naive floor(Duration/TickInterval) would
// predict (see TestAbilityCompileGolden_Meteor's wantBurnTicks+1 assertion),
// exactly mirroring GroundHazard's own extra tick.
//
// Defensive: a zone with TickInterval <= 0 is spawned (so it still expires
// normally and Duration-only zones with no tick behavior aren't silently
// dropped) but never enters the tick-firing loop in tickAbilityZonesLocked —
// see the TickInterval > 0 guard there. The action's own Validate already
// rejects TickInterval <= 0, so this only guards a program that reaches
// runtime some other way (e.g. authored directly, bypassing validation).
//
// Caller holds s.mu.
func (s *GameState) spawnAbilityZoneLocked(z *AbilityZone) {
	if z == nil {
		return
	}
	if z.TickInterval <= 0 {
		slog.Warn("ability zone spawned with non-positive tickInterval; it will never tick",
			"abilityId", z.AbilityID, "casterId", z.CasterID)
	} else {
		z.tickTimer = 0
	}
	z.ID = abilityZoneIDString(s.nextAbilityZoneID)
	s.nextAbilityZoneID++
	s.AbilityZones = append(s.AbilityZones, z)
}

// tickAbilityZonesLocked advances every zone by dt: recomputes occupancy and
// fires on_zone_enter/on_zone_exit for the diff (every tick, regardless of
// TickInterval — see the OCCUPANCY section of this file's doc comment), fires
// as many due on_zone_tick triggers as TickInterval cadence demands
// (epsilon-robust, see zoneTickEpsilon), then counts Remaining down and culls
// zones whose life has ended (firing a final on_zone_exit sweep for any
// still-occupying units first) or whose owning player has left the match.
//
// With no zones spawned (s.AbilityZones nil/empty — true for every existing
// test and every match until a zone-spawning ability ships) this is a no-op.
//
// Snapshot-and-reset s.AbilityZones (active/kept), NOT the
// `kept := s.AbilityZones[:0]` in-place-compaction idiom
// tickGroundHazardsLocked uses: on_zone_tick/on_zone_enter/on_zone_exit
// actions run through the SAME executor as any other trigger, so any of them
// can contain a nested create_zone action (spawnAbilityZoneLocked appends to
// s.AbilityZones). Appending to the FIELD while this loop still holds the
// field's ORIGINAL slice header would either silently write past `kept`'s
// growing-but-always-behind write cursor (never observed, because the final
// `s.AbilityZones = kept` overwrites the field's length back down) or, if the
// append needed to grow the backing array, replace the field with a
// reallocated array this loop never sees — either way the new zone is lost.
// Mirrors tickProjectilesLocked's identical snapshot fix for the identical
// hazard (equipment on-hit procs appending to s.Projectiles mid-loop).
//
// Must run under s.mu, AFTER combat/trap/projectile ticks and BEFORE
// drainPendingDeaths (same slot as tickGroundHazardsLocked, immediately
// after it).
func (s *GameState) tickAbilityZonesLocked(dt float64) {
	if len(s.AbilityZones) == 0 {
		return
	}
	active := s.AbilityZones
	s.AbilityZones = nil
	kept := active[:0]

	for _, z := range active {
		// Drop if the owning player has left the match (mirrors
		// tickGroundHazardsLocked / tickTrapsLocked). Deliberately skips
		// occupancy/tick processing entirely — see the file doc's OCCUPANCY
		// section for why this does not also fire a final on_zone_exit sweep.
		if _, ok := s.Players[z.OwnerPlayerID]; !ok {
			continue
		}

		occupants := s.zoneOccupantIDsLocked(z)
		entered, exited := diffSortedUnitIDs(z.occupantIDs, occupants)
		for _, id := range entered {
			s.fireAbilityZoneOccupancyEventLocked(z, id, TriggerOnZoneEnter)
		}
		for _, id := range exited {
			s.fireAbilityZoneOccupancyEventLocked(z, id, TriggerOnZoneExit)
		}
		z.occupantIDs = occupants

		if z.TickInterval > 0 {
			z.tickTimer -= dt
			for z.tickTimer <= zoneTickEpsilon {
				z.tickTimer += z.TickInterval
				s.fireAbilityZoneTickLocked(z)
			}
		}

		z.Remaining -= dt
		// A zone that consumed itself (consume_zone) is culled this tick no
		// matter how much life it had left — that is the whole point of the
		// one-shot shape. It still runs the exit sweep below, so an
		// enter-paired effect is never left dangling.
		if !z.consumed && z.Remaining > zoneTickEpsilon {
			kept = append(kept, z)
			continue
		}

		// Expiring (or consumed) this tick: fire on_zone_exit for every unit still inside
		// so an enter-paired effect always gets its matching exit (see file
		// doc). z.occupantIDs is already ascending-ID-sorted.
		for _, id := range z.occupantIDs {
			s.fireAbilityZoneOccupancyEventLocked(z, id, TriggerOnZoneExit)
		}
	}
	s.AbilityZones = append(kept, s.AbilityZones...)
}

// zoneOccupantIDsLocked returns the ascending-ID-sorted, deduped set of unit
// IDs currently inside z's radius: alive (HP>0) AND Visible, regardless of
// relation to the caster (see this file's OCCUPANCY doc for the "who counts
// as inside" rationale).
//
// Walks s.Units — a slice, iterated in its existing deterministic order —
// rather than any map, so the returned set never depends on map iteration
// order; the explicit sort.Ints below additionally guarantees the result is
// always in the ascending order on_zone_enter/on_zone_exit must fire in,
// independent of whatever order s.Units happens to hold units in. Caller
// holds s.mu.
func (s *GameState) zoneOccupantIDsLocked(z *AbilityZone) []int {
	radSq := z.Radius * z.Radius
	var ids []int
	for _, u := range s.Units {
		if u == nil || u.HP <= 0 || !u.Visible {
			continue
		}
		if distanceSquared(z.Center.X, z.Center.Y, u.X, u.Y) > radSq {
			continue
		}
		ids = append(ids, u.ID)
	}
	sort.Ints(ids)
	return ids
}

// diffSortedUnitIDs merges two ascending-sorted, deduped unit-ID slices
// (prev = last tick's occupants, cur = this tick's) and returns the IDs
// present in cur but not prev (entered) and present in prev but not cur
// (exited). Because both inputs are pre-sorted, this is a linear merge, not a
// sort — and both outputs come out ascending-ID-sorted for free, which is the
// stable firing order on_zone_enter/on_zone_exit require. Pure function, no
// lock needed.
func diffSortedUnitIDs(prev, cur []int) (entered, exited []int) {
	i, j := 0, 0
	for i < len(prev) && j < len(cur) {
		switch {
		case prev[i] == cur[j]:
			i++
			j++
		case prev[i] < cur[j]:
			exited = append(exited, prev[i])
			i++
		default:
			entered = append(entered, cur[j])
			j++
		}
	}
	exited = append(exited, prev[i:]...)
	entered = append(entered, cur[j:]...)
	return entered, exited
}

// zoneNamedBindings is the context binding set every zone-driven execution
// starts from. It exposes the zone's OWN live geometry to its nested actions so
// they never have to restate it: a select_targets inside an on_zone_tick can
// say radiusRef: "zone_radius" instead of hard-coding a number that would
// silently disagree with the zone once a perk or item widened it.
//
// Caller holds s.mu.
func zoneNamedBindings(z *AbilityZone) map[string]ContextValue {
	return map[string]ContextValue{
		"zone_radius": {Kind: ctxScalar, Scalar: z.Radius},
	}
}

// fireAbilityZoneTickLocked builds the per-tick RuntimeAbilityContext and runs
// the zone's compiled on_zone_tick trigger(s) through the shared executor.
// Caller holds s.mu.
func (s *GameState) fireAbilityZoneTickLocked(z *AbilityZone) {
	ctx := &RuntimeAbilityContext{
		CasterID:      z.CasterID,
		AbilityID:     z.AbilityID,
		OwnerUnitID:   z.CasterID,
		ZoneCenter:    z.Center,
		EventPosition: z.Center,
		CurrentZoneID: z.ID,
		currentZone:   z,
		Named:         zoneNamedBindings(z),
		Trace:         s.previewTrace,
		now:           s.previewClock,
	}
	s.runProgramTriggersLocked(ctx, z.Triggers, TriggerOnTick)
}

// fireAbilityZoneOccupancyEventLocked builds the RuntimeAbilityContext for a
// single on_zone_enter/on_zone_exit fire and runs z's compiled trigger(s) of
// type ttype through the shared executor. Mirrors fireAbilityZoneTickLocked's
// context shape, additionally binding CurrentEventUnitID = unitID (so
// select_targets{source: "current_event"} resolves to exactly the unit that
// crossed the boundary — the SrcCurrentEvent case in candidatePoolIDsLocked,
// ability_exec_targeting.go) and EventPosition to that unit's current world
// position (so origin: "current_event_position" anchors on it too), falling
// back to the zone's own center if the unit can no longer be resolved (e.g.
// removed from s.Units by some unrelated system in the very tick its exit
// fires). unitID is stored, never a *Unit, per AI_RULES; resolution happens
// at point of use inside the executor exactly like every other ID-bound
// context field. Caller holds s.mu.
func (s *GameState) fireAbilityZoneOccupancyEventLocked(z *AbilityZone, unitID int, ttype TriggerType) {
	pos := z.Center
	if u := s.getUnitByIDLocked(unitID); u != nil {
		pos = protocol.Vec2{X: u.X, Y: u.Y}
	}
	ctx := &RuntimeAbilityContext{
		CasterID:           z.CasterID,
		AbilityID:          z.AbilityID,
		OwnerUnitID:        z.CasterID,
		ZoneCenter:         z.Center,
		EventPosition:      pos,
		CurrentEventUnitID: unitID,
		CurrentZoneID:      z.ID,
		currentZone:        z,
		Named:              zoneNamedBindings(z),
		Trace:              s.previewTrace,
		now:                s.previewClock,
	}
	s.runProgramTriggersLocked(ctx, z.Triggers, ttype)
}

// ─────────────────────────────────────────────────────────────────────────────
// create_zone action
// ─────────────────────────────────────────────────────────────────────────────

// createZoneConfig is the decoded config for the create_zone action.
type createZoneConfig struct {
	Name         string      `json:"name"`
	PositionRef  *ContextRef `json:"position"`
	Radius       float64     `json:"radius"`
	Duration     float64     `json:"duration"`
	TickInterval float64     `json:"tickInterval"`
	OwnerRef     *ContextRef `json:"owner"`
	Presentation string      `json:"presentation"`
	// PresentationScale sizes the Presentation effect (0/absent -> 1x, matching
	// playEffectAtPointForDurationLocked). For a compiled meteor this carries
	// the legacy def.EffectScale so the crater matches the legacy GroundHazard
	// path's sizing.
	PresentationScale float64 `json:"scale,omitempty"`
	// Sprite opts this zone into being VISIBLE. AbilityZones are server-only by
	// default (see this file's doc comment) — their only player-visible output
	// is the damage/healing they cause plus a transient Presentation effect. A
	// zone that names a Sprite is instead serialized every tick as a persistent
	// ground entity the client renders for the zone's whole life, the same way
	// a trap is drawn.
	//
	// The value is an object sprite-set id (client
	// assets/objects/<id>/sprites.json), e.g. "fire_pit". Empty = invisible,
	// which stays the default so every existing zone (meteor's burn crater) is
	// byte-for-byte unchanged.
	//
	// This is deliberately generic, NOT a trap feature: any ability wanting a
	// persistent visible area (a healing circle, a hazard, a ward) opts in the
	// same way. See docs/design/ability_perk_interaction.md §8.
	Sprite string `json:"sprite,omitempty"`
	// SpriteScale is an extra render-scale factor on top of the sprite set's
	// own base scale. 0/absent = 1x. Only meaningful alongside Sprite.
	SpriteScale float64             `json:"spriteScale,omitempty"`
	Triggers    []AbilityTriggerDef `json:"triggers"`
}

func (createZoneConfig) actionConfig() {}

// resolveContextPositionLocked maps a position ContextRef (create_zone's
// "position", play_presentation's "position", etc — the single source of
// truth for every action that needs a world position out of a ContextRef) to
// a concrete world position. It recognizes both the legacy camelCase keys
// ("castPoint", "impactPosition", "zoneCenter", "eventPosition") and the
// program model's canonical snake_case origin strings (OriginCastPoint,
// OriginImpactPosition, OriginZoneCenter, etc — see ability_program.go), plus
// "caster" and a Named position lookup. ref == nil or an unrecognized/empty
// key returns fallback, which callers set to whatever "no position given"
// should mean for them: create_zone's Execute passes the caster's current
// position (self-centered zone is the expected default for a zone spell),
// play_presentation's Execute passes ctx.CastPoint. Caller holds s.mu.
func (s *GameState) resolveContextPositionLocked(ctx *RuntimeAbilityContext, ref *ContextRef, fallback protocol.Vec2) protocol.Vec2 {
	if ref == nil {
		return fallback
	}
	switch ref.Key {
	case "caster":
		if u := s.getUnitByIDLocked(ctx.CasterID); u != nil {
			return protocol.Vec2{X: u.X, Y: u.Y}
		}
		return protocol.Vec2{}
	case "castPoint", "cast_point":
		return ctx.CastPoint
	case "impactPosition", "impact_position":
		return ctx.ImpactPosition
	case "zoneCenter", "zone_center":
		return ctx.ZoneCenter
	case "eventPosition", "current_event_position":
		return ctx.EventPosition
	case "initialTarget", "initial_target", "initial_target_position":
		// The unit this cast was aimed at. Lets a zone be centered on its
		// target rather than the caster — how a thrown/placed area lands on the
		// enemy it was aimed at. Falls back when the target is gone by the time
		// the zone spawns (died mid-cast), which keeps the zone at the caller's
		// sensible default rather than at the world origin.
		if u := s.getUnitByIDLocked(ctx.InitialTarget); u != nil {
			return protocol.Vec2{X: u.X, Y: u.Y}
		}
		return fallback
	}
	if ctx.Named != nil {
		if v, ok := ctx.Named[ref.Key]; ok && v.Kind == ctxPosition {
			return v.Position
		}
	}
	return fallback
}

// consumeZoneConfig is the (empty) config for consume_zone. The action needs no
// tuning: it always ends the zone the current execution is running inside.
type consumeZoneConfig struct{}

func (consumeZoneConfig) actionConfig() {}

func init() {
	registerAction(ActionDescriptor{
		Type: ActionConsumeZone,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			return consumeZoneConfig{}, nil
		},
		// Nothing to validate — the action has no config. Must still be
		// non-nil: the program validator calls Validate unconditionally.
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue { return nil },
		Schema:   ActionFieldSchema{},
		// Execute ends the enclosing zone immediately — the ONE-SHOT zone shape.
		// A pressure-plate trap authors "on enter: deal damage, then consume
		// myself" instead of needing a distinct one-shot zone kind, and any
		// ability wanting a spend-once ward gets the same behavior for free.
		//
		// Marking rather than removing here is deliberate: the zone must survive
		// to the end of tickAbilityZonesLocked so its paired on_zone_exit sweep
		// still fires for everyone inside. Targets pass through unchanged — a
		// zone ending is not a target-set output.
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			if ctx.currentZone == nil {
				ctx.trace("zone_consume_skipped", ctx.currentActionPath, map[string]any{"reason": "not inside a zone"})
				return targets
			}
			ctx.currentZone.consumed = true
			ctx.trace("zone_consumed", ctx.currentActionPath, map[string]any{"zone": ctx.CurrentZoneID})
			return targets
		},
	})

	registerAction(ActionDescriptor{
		Type: ActionCreateZone,
		Decode: func(b json.RawMessage) (ActionConfig, error) {
			var c createZoneConfig
			if len(b) == 0 {
				return c, nil
			}
			err := json.Unmarshal(b, &c)
			return c, err
		},
		Validate: func(cfg ActionConfig, _ ValidationScope) []ValidationIssue {
			c := cfg.(createZoneConfig)
			var out []ValidationIssue
			if c.Radius <= 0 {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "create_zone requires radius > 0", Severity: "error"})
			}
			if c.Duration <= 0 {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "create_zone requires duration > 0", Severity: "error"})
			}
			if c.TickInterval <= 0 {
				out = append(out, ValidationIssue{Code: "empty_required_property", Message: "create_zone requires tickInterval > 0", Severity: "error"})
			}
			return out
		},
		Schema: ActionFieldSchema{Fields: []SchemaField{
			{Key: "name", Label: "Name", Control: "text", Section: "Basic"},
			{Key: "position", Label: "Position", Control: "context_ref", Section: "Targeting"},
			{Key: "radius", Label: "Radius", Control: "number", Kind: abilityStatKindRadius, Section: "Targeting"},
			{Key: "duration", Label: "Duration", Control: "duration", Kind: abilityStatKindDuration, Section: "Timing"},
			{Key: "tickInterval", Label: "Tick Interval", Control: "duration", Section: "Timing"},
			{Key: "owner", Label: "Owner", Control: "context_ref", Section: "Advanced"},
			{Key: "presentation", Label: "Presentation", Control: "asset", Section: "Presentation"},
			{Key: "scale", Label: "Presentation Scale", Control: "number", Section: "Presentation"},
			// Opt-in visibility: naming a sprite turns this zone from a
			// server-only effect area into a persistent ground entity the
			// client draws for the zone's whole life.
			{Key: "sprite", Label: "Visible Sprite", Control: "text", Section: "Presentation"},
			{Key: "spriteScale", Label: "Sprite Scale", Control: "number", Section: "Presentation"},
			// Config's on_zone_tick/on_zone_enter/on_zone_exit triggers are NOT
			// re-declared here (a "triggers" nested_triggers field used to sit
			// here) — see apply_status's identical note (ability_exec_actions.go):
			// the flow view already renders config.triggers as real, recursive
			// FlowTriggerCards under this action.
		}},
		// Execute spawns the zone; it never resolves/deals damage itself — the
		// zone's own on_zone_tick trigger (fired later by
		// tickAbilityZonesLocked) reuses the same executor + target-query as
		// any other trigger.
		Execute: func(s *GameState, ctx *RuntimeAbilityContext, cfg ActionConfig, targets []int) []int {
			c := cfg.(createZoneConfig)
			caster := s.getUnitByIDLocked(ctx.CasterID)
			if caster == nil {
				return nil
			}
			center := s.resolveContextPositionLocked(ctx, c.PositionRef, protocol.Vec2{X: caster.X, Y: caster.Y})
			z := &AbilityZone{
				AbilityID: ctx.AbilityID,
				CasterID:  ctx.CasterID,
				// TODO(phase-3b): resolve OwnerRef to a player when present; Phase 3
				// zones are always owned by the caster.
				OwnerPlayerID: caster.OwnerID,
				Center:        center,
				Radius:        c.Radius,
				Remaining:     c.Duration,
				TickInterval:  c.TickInterval,
				Sprite:        c.Sprite,
				SpriteScale:   c.SpriteScale,
				Triggers:      c.Triggers,
			}
			s.spawnAbilityZoneLocked(z)
			ctx.trace("zone_created", ctx.currentActionPath, map[string]any{"name": c.Name, "radius": c.Radius, "duration": c.Duration})
			// Render the zone's lingering VFX (e.g. meteor's "burning_crater") for
			// the zone's whole life, mirroring the legacy GroundHazard path
			// (spawnGroundHazardLocked -> playEffectAtPointForDurationLocked). Played
			// at zone-creation time — which for a compiled meteor is the impact
			// marker, i.e. when the crater should first appear. No-op for an empty /
			// unregistered presentation id (the helper fails safe).
			if c.Presentation != "" {
				s.playEffectAtPointForDurationLocked(c.Presentation, center.X, center.Y, c.PresentationScale, c.Duration)
			}
			return nil
		},
	})
}
