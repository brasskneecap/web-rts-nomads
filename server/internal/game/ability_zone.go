package game

import (
	"encoding/json"
	"log/slog"
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
	tickTimer     float64             // counts down to the next on_zone_tick fire (runtime-only)
	Triggers      []AbilityTriggerDef // compiled on_zone_tick trigger(s)
}

func abilityZoneIDString(id int) string {
	return "zone-" + strconv.Itoa(id)
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

// tickAbilityZonesLocked advances every zone by dt: fires as many due
// on_zone_tick triggers as TickInterval cadence demands (epsilon-robust, see
// zoneTickEpsilon), then counts Remaining down and culls zones whose life has
// ended or whose owning player has left the match. Filter-into-front-of-slice
// (like tickGroundHazardsLocked) to avoid steady-state allocation.
//
// With no zones spawned (s.AbilityZones nil/empty — true for every existing
// test and every match until a zone-spawning ability ships) this is a no-op.
//
// Must run under s.mu, AFTER combat/trap/projectile ticks and BEFORE
// drainPendingDeaths (same slot as tickGroundHazardsLocked, immediately
// after it).
func (s *GameState) tickAbilityZonesLocked(dt float64) {
	if len(s.AbilityZones) == 0 {
		return
	}
	kept := s.AbilityZones[:0]
	for _, z := range s.AbilityZones {
		// Drop if the owning player has left the match (mirrors
		// tickGroundHazardsLocked / tickTrapsLocked).
		if _, ok := s.Players[z.OwnerPlayerID]; !ok {
			continue
		}

		if z.TickInterval > 0 {
			z.tickTimer -= dt
			for z.tickTimer <= zoneTickEpsilon {
				z.tickTimer += z.TickInterval
				s.fireAbilityZoneTickLocked(z)
			}
		}

		z.Remaining -= dt
		if z.Remaining > zoneTickEpsilon {
			kept = append(kept, z)
		}
	}
	s.AbilityZones = kept
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
		Named:         map[string]ContextValue{},
		Trace:         s.previewTrace,
		now:           s.previewClock,
	}
	s.runProgramTriggersLocked(ctx, z.Triggers, TriggerOnZoneTick)
}

// ─────────────────────────────────────────────────────────────────────────────
// create_zone action
// ─────────────────────────────────────────────────────────────────────────────

// createZoneConfig is the decoded config for the create_zone action.
type createZoneConfig struct {
	Name         string              `json:"name"`
	PositionRef  *ContextRef         `json:"position"`
	Radius       float64             `json:"radius"`
	Duration     float64             `json:"duration"`
	TickInterval float64             `json:"tickInterval"`
	OwnerRef     *ContextRef         `json:"owner"`
	Presentation string              `json:"presentation"`
	// PresentationScale sizes the Presentation effect (0/absent -> 1x, matching
	// playEffectAtPointForDurationLocked). For a compiled meteor this carries
	// the legacy def.EffectScale so the crater matches the legacy GroundHazard
	// path's sizing.
	PresentationScale float64             `json:"scale,omitempty"`
	Triggers          []AbilityTriggerDef `json:"triggers"`
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
	}
	if ctx.Named != nil {
		if v, ok := ctx.Named[ref.Key]; ok && v.Kind == ctxPosition {
			return v.Position
		}
	}
	return fallback
}

func init() {
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
			{Key: "radius", Label: "Radius", Control: "number", Section: "Targeting"},
			{Key: "duration", Label: "Duration", Control: "duration", Section: "Timing"},
			{Key: "tickInterval", Label: "Tick Interval", Control: "duration", Section: "Timing"},
			{Key: "owner", Label: "Owner", Control: "context_ref", Section: "Advanced"},
			{Key: "presentation", Label: "Presentation", Control: "asset", Section: "Presentation"},
			{Key: "scale", Label: "Presentation Scale", Control: "number", Section: "Presentation"},
			{Key: "triggers", Label: "Triggers", Control: "nested_triggers", Section: "Advanced"},
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
