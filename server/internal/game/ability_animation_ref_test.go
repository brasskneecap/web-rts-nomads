package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestParseAnimationRefScheme covers the create_zone visual scheme parser that
// backs both the presentation routing and the visible-zone snapshot split.
func TestParseAnimationRefScheme(t *testing.T) {
	cases := []struct {
		in                          string
		wantSrc, wantRef, wantState string
	}{
		{"effect:explosion", "effect", "explosion", ""},
		{"projectile:frost_bolt", "projectile", "frost_bolt", ""},
		{"beam:siphon_life", "beam", "siphon_life", ""},
		{"object:caltrops", "object", "caltrops", ""},
		{"object:caltrops@electrified", "object", "caltrops", "electrified"},
		{"image:my_ability", "image", "my_ability", ""},
		// Bare legacy values: no scheme prefix → source "", ref = whole input.
		{"marker_trap", "", "marker_trap", ""},
		{"explosion", "", "explosion", ""},
		// Unknown prefix is treated as bare, not a scheme.
		{"bogus:thing", "", "bogus:thing", ""},
	}
	for _, c := range cases {
		src, ref, state := parseAnimationRefScheme(c.in)
		if src != c.wantSrc || ref != c.wantRef || state != c.wantState {
			t.Errorf("parseAnimationRefScheme(%q) = (%q,%q,%q); want (%q,%q,%q)",
				c.in, src, ref, state, c.wantSrc, c.wantRef, c.wantState)
		}
	}
}

// TestVisibleZoneSnapshot_SchemeResolution pins how a zone's sprite scheme maps
// onto the TrapSnapshot: an object scheme splits into a bare Type + Variant (so
// the existing object renderer handles it), every other scheme passes through
// verbatim as Type (so the client's decal branch resolves it).
func TestVisibleZoneSnapshot_SchemeResolution(t *testing.T) {
	s := NewGameStateWithSeed(GetMapConfigByID(DefaultMapID()), 1)
	s.AbilityZones = []*AbilityZone{
		{ID: "z1", Sprite: "object:caltrops@electrified", Center: protocol.Vec2{X: 1, Y: 2}},
		{ID: "z2", Sprite: "effect:explosion", Center: protocol.Vec2{X: 3, Y: 4}},
		{ID: "z3", Sprite: "marker_trap", Center: protocol.Vec2{X: 5, Y: 6}},
	}
	snaps := s.visibleZoneSnapshotsLocked()
	byID := map[string]protocol.TrapSnapshot{}
	for _, sn := range snaps {
		byID[sn.ID] = sn
	}

	if got := byID["z1"]; got.Type != "caltrops" || got.Variant != "electrified" {
		t.Errorf("object scheme: Type=%q Variant=%q; want caltrops/electrified", got.Type, got.Variant)
	}
	if got := byID["z2"]; got.Type != "effect:explosion" || got.Variant != "" {
		t.Errorf("effect scheme: Type=%q Variant=%q; want effect:explosion/empty", got.Type, got.Variant)
	}
	if got := byID["z3"]; got.Type != "marker_trap" || got.Variant != "" {
		t.Errorf("bare id: Type=%q Variant=%q; want marker_trap/empty", got.Type, got.Variant)
	}
}
