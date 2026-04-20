package game

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestObstaclesHydratedFromDef verifies that when a map is loaded, every
// obstacle in the returned MapConfig carries the id, capabilities, and
// (for trees) resource fields from its matching obstacle def. Regression
// guard for the trees-as-obstacles migration — without hydration the
// client would see bare {x, y, obstacle} tiles and couldn't select them.
func TestObstaclesHydratedFromDef(t *testing.T) {
	m := GetMapConfigByID("grenland-small")
	if len(m.Obstacles) == 0 {
		t.Fatal("expected obstacles in grenland-small")
	}

	var sawTree, sawRock bool
	for _, o := range m.Obstacles {
		if o.ID == "" {
			t.Errorf("obstacle %s at (%d,%d) missing id", o.Obstacle, o.X, o.Y)
		}
		switch o.Obstacle {
		case "tree":
			sawTree = true
			if !contains(o.Capabilities, "resource-source") {
				t.Errorf("tree %s missing resource-source capability: %v", o.ID, o.Capabilities)
			}
			if !contains(o.Capabilities, "selectable") {
				t.Errorf("tree %s missing selectable capability: %v", o.ID, o.Capabilities)
			}
			if o.ResourceType != "wood" {
				t.Errorf("tree %s ResourceType=%q, want wood", o.ID, o.ResourceType)
			}
			if o.ResourceAmount <= 0 {
				t.Errorf("tree %s ResourceAmount=%d, want >0", o.ID, o.ResourceAmount)
			}
		case "rock":
			sawRock = true
			if !contains(o.Capabilities, "selectable") {
				t.Errorf("rock %s missing selectable capability: %v", o.ID, o.Capabilities)
			}
			if o.MaxHp <= 0 {
				t.Errorf("rock %s MaxHp=%f, want >0", o.ID, o.MaxHp)
			}
		}
	}

	if !sawTree {
		t.Error("grenland-small should include at least one tree obstacle")
	}
	if !sawRock {
		t.Error("grenland-small should include at least one rock obstacle")
	}

	// Round-trip through JSON so we catch any field that would be dropped
	// on the wire (e.g. missing struct tag, Go-side private field).
	encoded, err := json.Marshal(m.Obstacles[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"id"`) {
		t.Errorf("serialized obstacle missing id field: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"capabilities"`) {
		t.Errorf("serialized obstacle missing capabilities field: %s", encoded)
	}
}

func contains(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}
