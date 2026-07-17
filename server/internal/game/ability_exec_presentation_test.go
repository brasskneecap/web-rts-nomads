package game

import (
	"encoding/json"
	"testing"

	"webrts/server/pkg/protocol"
)

// ── play_presentation: at-point ─────────────────────────────────────────────

func TestPlayPresentation_AtPoint_QueuesEffect(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)

	cfg := playPresentationAtPointConfig{
		Asset:    "shatter",
		Position: ContextRef{Key: "castPoint"},
		Scale:    2,
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	tr := &AbilityExecutionTrace{}
	ctx := &RuntimeAbilityContext{
		CasterID:  caster.ID,
		CastPoint: protocol.Vec2{X: 100, Y: 200},
		Named:     map[string]ContextValue{},
		Trace:     tr,
	}
	a := &AbilityActionDef{ID: "vfx", Type: ActionPlayPresentation, Config: json.RawMessage(b)}

	before := len(s.activeEffects)
	s.executeActionLocked(ctx, a, "trig")

	added := s.activeEffects[before:]
	if len(added) != 1 {
		t.Fatalf("len(added effects) = %d; want exactly 1", len(added))
	}
	e := added[0]
	if e.Name != "shatter" {
		t.Fatalf("effect.Name = %q; want %q", e.Name, "shatter")
	}
	if e.FallbackX != 100 || e.FallbackY != 200 {
		t.Fatalf("effect position = (%v,%v); want (100,200)", e.FallbackX, e.FallbackY)
	}
	if e.SizeScale != 2 {
		t.Fatalf("effect.SizeScale = %v; want 2", e.SizeScale)
	}
	if e.AnchorUnitID != 0 {
		t.Fatalf("effect.AnchorUnitID = %d; want 0 (world-anchored)", e.AnchorUnitID)
	}
	if !traceHas(tr, "presentation_played") {
		t.Fatalf("missing presentation_played trace event: %+v", tr.Events)
	}
}

// TestPlayPresentation_AtPoint_SnakeCaseKey_ResolvesImpactPosition locks the
// Fix 1 contract: the at-point config's Position ContextRef is resolved via
// the shared resolveContextPositionLocked (ability_zone.go), which recognizes
// the program model's canonical snake_case origin strings
// (OriginImpactPosition = "impact_position", ability_program.go) in addition
// to the legacy camelCase keys. Before the fix, a hand-authored config using
// the canonical form silently fell back to ctx.CastPoint instead.
func TestPlayPresentation_AtPoint_SnakeCaseKey_ResolvesImpactPosition(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)

	cfg := playPresentationAtPointConfig{
		Asset:    "shatter",
		Position: ContextRef{Key: "impact_position"},
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	ctx := &RuntimeAbilityContext{
		CasterID:       caster.ID,
		CastPoint:      protocol.Vec2{X: 100, Y: 200},
		ImpactPosition: protocol.Vec2{X: 300, Y: 400},
		Named:          map[string]ContextValue{},
		Trace:          &AbilityExecutionTrace{},
	}
	a := &AbilityActionDef{ID: "vfx", Type: ActionPlayPresentation, Config: json.RawMessage(b)}

	before := len(s.activeEffects)
	s.executeActionLocked(ctx, a, "trig")

	added := s.activeEffects[before:]
	if len(added) != 1 {
		t.Fatalf("len(added effects) = %d; want exactly 1", len(added))
	}
	e := added[0]
	if e.FallbackX != 300 || e.FallbackY != 400 {
		t.Fatalf("effect position = (%v,%v); want ImpactPosition (300,400)", e.FallbackX, e.FallbackY)
	}
}

// TestPlayPresentation_AtPoint_UnrecognizedKey_FallsBackToCastPoint locks the
// documented "unrecognized/empty key -> CastPoint" default for a hand-authored
// at-point config that omits/misspells position.
func TestPlayPresentation_AtPoint_UnrecognizedKey_FallsBackToCastPoint(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)

	cfg := playPresentationAtPointConfig{Asset: "shatter", Position: ContextRef{Key: "not_a_real_key"}}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	ctx := &RuntimeAbilityContext{
		CasterID:  caster.ID,
		CastPoint: protocol.Vec2{X: 55, Y: 66},
		Named:     map[string]ContextValue{},
		Trace:     &AbilityExecutionTrace{},
	}
	a := &AbilityActionDef{ID: "vfx", Type: ActionPlayPresentation, Config: json.RawMessage(b)}

	before := len(s.activeEffects)
	s.executeActionLocked(ctx, a, "trig")

	added := s.activeEffects[before:]
	if len(added) != 1 {
		t.Fatalf("len(added effects) = %d; want exactly 1", len(added))
	}
	e := added[0]
	if e.FallbackX != 55 || e.FallbackY != 66 {
		t.Fatalf("effect position = (%v,%v); want CastPoint fallback (55,66)", e.FallbackX, e.FallbackY)
	}
}

// TestPlayPresentation_AtPoint_EmptyAsset_StillTraces locks the documented
// "trace must fire even when the effect helper no-ops" contract: an empty
// Asset means playEffectAtPointLocked is a no-op (fail-safe, see
// ability_cast.go), but the presentation_played trace event must still be
// emitted.
func TestPlayPresentation_AtPoint_EmptyAsset_StillTraces(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)

	cfg := playPresentationAtPointConfig{Position: ContextRef{Key: "castPoint"}}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	tr := &AbilityExecutionTrace{}
	ctx := &RuntimeAbilityContext{
		CasterID:  caster.ID,
		CastPoint: protocol.Vec2{X: 1, Y: 2},
		Named:     map[string]ContextValue{},
		Trace:     tr,
	}
	a := &AbilityActionDef{ID: "vfx", Type: ActionPlayPresentation, Config: json.RawMessage(b)}

	before := len(s.activeEffects)
	s.executeActionLocked(ctx, a, "trig")

	if len(s.activeEffects) != before {
		t.Fatalf("len(activeEffects) = %d; want unchanged %d (empty asset no-ops)", len(s.activeEffects), before)
	}
	if !traceHas(tr, "presentation_played") {
		t.Fatalf("missing presentation_played trace event even though asset was empty: %+v", tr.Events)
	}
}

// ── play_presentation: on-target ────────────────────────────────────────────

func TestPlayPresentation_OnTarget_QueuesPerUnit(t *testing.T) {
	s := setupHostileTargetingPair(t)
	defer s.mu.Unlock()

	caster := teamCombatUnit(t, s, "p1", 0, 0)
	ally1 := teamCombatUnit(t, s, "p1", 10, 0)
	ally2 := teamCombatUnit(t, s, "p1", 20, 0)

	cfg := playPresentationOnTargetConfig{Asset: "healing_glow", OncePerTarget: true}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	tr := &AbilityExecutionTrace{}
	ctx := &RuntimeAbilityContext{
		CasterID: caster.ID,
		Named: map[string]ContextValue{
			"healTargets": {Kind: ctxUnitSet, UnitIDs: []int{ally1.ID, ally2.ID}},
		},
		Trace: tr,
	}
	a := &AbilityActionDef{
		ID:     "vfx",
		Type:   ActionPlayPresentation,
		Input:  map[string]ContextRef{"attach": {Key: "healTargets"}},
		Config: json.RawMessage(b),
	}

	before := len(s.activeEffects)
	s.executeActionLocked(ctx, a, "trig")

	added := s.activeEffects[before:]
	if len(added) != 2 {
		t.Fatalf("len(added effects) = %d; want 2 (one per target)", len(added))
	}
	wantAnchors := map[int]bool{ally1.ID: true, ally2.ID: true}
	seen := map[int]bool{}
	for _, e := range added {
		if e.Name != "healing_glow" {
			t.Fatalf("effect.Name = %q; want %q", e.Name, "healing_glow")
		}
		if !wantAnchors[e.AnchorUnitID] {
			t.Fatalf("effect.AnchorUnitID = %d; want one of %v", e.AnchorUnitID, wantAnchors)
		}
		seen[e.AnchorUnitID] = true
	}
	if len(seen) != 2 {
		t.Fatalf("effects anchored to %v; want exactly one per unit %v", seen, wantAnchors)
	}
	if !traceHas(tr, "presentation_played") {
		t.Fatalf("missing presentation_played trace event: %+v", tr.Events)
	}
}
